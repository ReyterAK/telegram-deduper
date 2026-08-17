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

func deletedTemplate(typ DupType) string {
	if typ == DupTypeLink {
		return "Удален дубль ссылки в сообщении %s"
	}
	return "Удален дубль сообщения %s"
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
func (d *Detector) reactionFor(typ DupType, cat DupCategory) Reaction {
	var rs ReactionSettings
	if typ == DupTypeLink {
		rs = d.cfg.Reactions.Link
	} else {
		rs = d.cfg.Reactions.Message
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
func (d *Detector) react(c MsgContent, typ DupType, cat DupCategory, original StoredMessage, now time.Time) {
	reaction := d.reactionFor(typ, cat)
	link := messageLink(c.ChatID, c.MsgID, d.chatUsername)

	// Link for the delete notice: the in-chat original when one
	// exists; otherwise fall back to the forward's source post;
	// with no link the notice degrades to plain text.
	origLink := c.SourceLink
	if original.MsgID > 0 {
		origLink = messageLink(c.ChatID, original.MsgID, d.chatUsername)
	}

	switch reaction {
	case ReactionIgnore:
		return

	case ReactionComment:
		text := withAuthor(fmt.Sprintf(commentTemplate(typ), link), c.AuthorName)
		text = d.appendWarningLine(text, c.ChatID, c.UserID, cat, now)
		msg := tgbotapi.NewMessage(c.ChatID, text)
		msg.ReplyToMessageID = c.MsgID
		sent, err := d.bot.Send(msg)
		if err != nil {
			log.Printf("[action] комментарий: %v", err)
			return
		}
		d.scheduleAutoDelete(sent)

	case ReactionDelete:
		if _, err := d.bot.Request(tgbotapi.NewDeleteMessage(c.ChatID, c.MsgID)); err != nil {
			log.Printf("[action] удаление дубля: %v", err)
			return
		}
		text := withAuthor(deletedShortText(typ), c.AuthorName)
		text = d.appendWarningLine(text, c.ChatID, c.UserID, cat, now)

		// Post the notice as a REPLY to the original message, so the
		// quoted content shows which message was duplicated.
		msg := tgbotapi.NewMessage(c.ChatID, text)
		msg.ReplyToMessageID = original.MsgID
		sent, err := d.bot.Send(msg)
		if err != nil && original.MsgID > 0 {
			// Reply failed (original gone?) → standalone with a link.
			log.Printf("[action] ответ на оригинал не прошёл (%v), шлю со ссылкой", err)
			fallback := withAuthor(deletedMessageText(typ, origLink), c.AuthorName)
			fallback = d.appendWarningLine(fallback, c.ChatID, c.UserID, cat, now)
			sent, err = d.bot.Send(tgbotapi.NewMessage(c.ChatID, fallback))
		}
		if err != nil {
			log.Printf("[action] уведомление об удалении: %v", err)
			return
		}
		d.scheduleAutoDelete(sent)
	}

	// Warning accounting happens for comment/delete reactions only.
	d.warnAndMaybeBan(c.ChatID, c.UserID, cat, now, c.AuthorName)
}

// deletedShortText is the reply-form delete notice (no link — the
// quoted original serves as the reference).
func deletedShortText(typ DupType) string {
	if typ == DupTypeLink {
		return "Удален дубль ссылки"
	}
	return "Удален дубль сообщения"
}

// deletedMessageText builds the delete notice; without a link the
// message degrades to a plain statement.
func deletedMessageText(typ DupType, link string) string {
	if link == "" {
		return "Удалена пересылка из внешнего источника"
	}
	return fmt.Sprintf(deletedTemplate(typ), link)
}

// appendWarningLine appends the current warning count when warnings
// are enabled for the category.
func (d *Detector) appendWarningLine(base string, chatID, userID int64, cat DupCategory, now time.Time) string {
	ws := d.warningSettingsFor(cat)
	if ws.Threshold <= 0 {
		return base
	}
	lifetimeSec := int64(ws.LifetimeDays) * 86400
	count, err := d.st.CountWarnings(chatID, userID, now.Unix()-lifetimeSec)
	if err != nil {
		log.Printf("[action] подсчёт предупреждений: %v", err)
		return base
	}
	return base + fmt.Sprintf("\nПредупреждение %d/%d", count+1, ws.Threshold)
}
