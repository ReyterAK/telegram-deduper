//
// actions.go
// Antidubl — reactions on duplicates: ignore / comment (reply) / delete
//
// Reaction texts:
//   comment: «Дубль ссылки в сообщении <link>» / «Дубль сообщения <link>»
//   delete:  «Удален дубль ссылки в сообщении <link>» / «Удален дубль сообщения <link>»
// The link in a comment points to the duplicate (it is alive, it is
// the reply target); the link in a delete notice points to the
// ORIGINAL message (the duplicate is gone).
// When warnings are enabled, the current warning count is appended.
//

package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func commentTemplate(typ DupType) string {
	if typ == DupTypeLink {
		return "Дубль ссылки в сообщении %s"
	}
	return "Дубль сообщения %s"
}

// messageLink builds a clickable t.me link to a message.
func messageLink(chatID int64, msgID int, chatUsername string) string {
	if chatUsername != "" {
		return fmt.Sprintf("https://t.me/%s/%d", chatUsername, msgID)
	}
	return fmt.Sprintf("https://t.me/c/%s/%d", chatLinkID(chatID), msgID)
}

// chatLinkID converts a Telegram chat id to the form used in
// t.me/c/ links: for supergroups/channels the "-100" marker is
// dropped (t.me/c/4307533132/60, not .../1004307533132/60);
// otherwise just the leading minus.
func chatLinkID(chatID int64) string {
	if chatID <= -1000000000000 { // supergroup/channel
		return strconv.FormatInt(-(chatID + 1000000000000), 10)
	}
	return strconv.FormatInt(-chatID, 10)
}

// chatMessageLink builds a link to a message in an arbitrary chat
// (used for the source post of an external forward); "" when the
// message id is unknown.
func chatMessageLink(chatID int64, username string, msgID int) string {
	if msgID <= 0 {
		return ""
	}
	return messageLink(chatID, msgID, username)
}

// reactionFor resolves the configured reaction for type×category.
func reactionFor(s *Settings, typ DupType, cat DupCategory) Reaction {
	var rs ReactionSettings
	if typ == DupTypeLink {
		rs = s.Reactions.Link
	} else {
		rs = s.Reactions.Message
	}
	if cat == CatSameParticipant {
		return rs.SameParticipant
	}
	return rs.DiffParticipant
}

// withAuthor appends the author's name to a reaction text.
func withAuthor(text, name string) string {
	if name == "" {
		return text
	}
	return text + " — " + name
}

// react executes the configured reaction for a detected duplicate.
// original is the first occurrence (empty for external forwards).
func (d *Detector) react(c MsgContent, typ DupType, cat DupCategory, original StoredMessage, now time.Time, s *Settings) {
	reaction := reactionFor(s, typ, cat)
	link := messageLink(c.ChatID, c.MsgID, d.chatNameFor(c.ChatID))

	switch reaction {
	case ReactionIgnore:
		return

	case ReactionComment:
		text := withAuthor(fmt.Sprintf(commentTemplate(typ), link), c.AuthorName)
		text = appendWarningLine(d.st, s, text, c.ChatID, c.UserID, cat, now)
		msg := tgbotapi.NewMessage(c.ChatID, text)
		msg.ReplyToMessageID = c.MsgID
		sent, err := d.bot.Send(msg)
		if err != nil {
			log.Printf("[action] комментарий: %v", err)
			return
		}
		d.scheduleAutoDelete(sent, s.AutoDeleteHours)

	case ReactionDelete:
		text := withAuthor(deletedShortText(typ), c.AuthorName)
		text = appendWarningLine(d.st, s, text, c.ChatID, c.UserID, cat, now)

		// 1) Post the notice as a REPLY to the original first: the
		// quoted content shows which message was duplicated.
		notice := tgbotapi.NewMessage(c.ChatID, text)
		notice.ReplyToMessageID = original.MsgID
		sent, err := d.bot.Send(notice)
		if err != nil {
			// Original is gone (message to be replied not found) or a
			// transient error. Policy decides whether the repeat stands.
			if s.DeletedOriginalPolicy != DeletedOriginalStrict {
				log.Printf("[action] ответ на оригинал не прошёл (%v) — повтор разрешён (allow)", err)
				return
			}
			log.Printf("[action] ответ на оригинал не прошёл (%v) — строгая политика: дубль удаляется", err)
			if _, derr := d.bot.Request(tgbotapi.NewDeleteMessage(c.ChatID, c.MsgID)); derr != nil {
				log.Printf("[action] удаление дубля: %v", derr)
				return
			}
			// Standalone notice without a reply (no dead link).
			if s2, err2 := d.bot.Send(tgbotapi.NewMessage(c.ChatID, text)); err2 == nil {
				d.scheduleAutoDelete(s2, s.AutoDeleteHours)
			}
			warnAndMaybeBan(d, c.ChatID, c.UserID, cat, now, c.AuthorName, s)
			return
		}
		d.scheduleAutoDelete(sent, s.AutoDeleteHours)

		// 2) Delete the duplicate.
		if _, err := d.bot.Request(tgbotapi.NewDeleteMessage(c.ChatID, c.MsgID)); err != nil {
			log.Printf("[action] удаление дубля: %v", err)
			return
		}
	}

	// Warning accounting happens for comment/delete reactions only.
	warnAndMaybeBan(d, c.ChatID, c.UserID, cat, now, c.AuthorName, s)
}

// deletedShortText is the reply-form delete notice (no link — the
// quoted original serves as the reference).
func deletedShortText(typ DupType) string {
	if typ == DupTypeLink {
		return "Удален дубль ссылки"
	}
	return "Удален дубль сообщения"
}

// appendWarningLine appends the current warning count when warnings
// are enabled for the category.
func appendWarningLine(st *Store, s *Settings, base string, chatID, userID int64, cat DupCategory, now time.Time) string {
	ws := warningSettingsFor(s, cat)
	if ws.Threshold <= 0 {
		return base
	}
	lifetimeSec := int64(ws.LifetimeDays) * 86400
	count, err := st.CountWarnings(chatID, userID, now.Unix()-lifetimeSec)
	if err != nil {
		log.Printf("[action] подсчёт предупреждений: %v", err)
		return base
	}
	return base + fmt.Sprintf("\nПредупреждение %d/%d", count+1, ws.Threshold)
}
