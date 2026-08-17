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
	"strings"
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
	// t.me/c/<id>/<msg> — id without the leading minus.
	id := strings.TrimPrefix(strconv.FormatInt(chatID, 10), "-")
	return fmt.Sprintf("https://t.me/c/%s/%d", id, msgID)
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

// react executes the configured reaction for a detected duplicate.
// original is the first occurrence (empty for external forwards).
func (d *Detector) react(c MsgContent, typ DupType, cat DupCategory, original StoredMessage, now time.Time) {
	reaction := d.reactionFor(typ, cat)
	link := messageLink(c.ChatID, c.MsgID, d.chatUsername)
	origLink := messageLink(c.ChatID, original.MsgID, d.chatUsername)

	switch reaction {
	case ReactionIgnore:
		return

	case ReactionComment:
		text := fmt.Sprintf(commentTemplate(typ), link)
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
		text := fmt.Sprintf(deletedTemplate(typ), origLink)
		text = d.appendWarningLine(text, c.ChatID, c.UserID, cat, now)
		sent, err := d.bot.Send(tgbotapi.NewMessage(c.ChatID, text))
		if err != nil {
			log.Printf("[action] уведомление об удалении: %v", err)
			return
		}
		d.scheduleAutoDelete(sent)
	}

	// Warning accounting happens for comment/delete reactions only.
	d.warnAndMaybeBan(c.ChatID, c.UserID, cat, now)
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
