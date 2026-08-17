//
// autodelete.go
// Antidubl — scheduled deletion of the bot's own messages
//
// The bot cleans up after itself: comments and delete notices are
// removed N hours after being sent (config auto_delete_hours, off
// by default). The bot can always delete its own messages, so the
// 48-hour limit does not apply here.
//

package main

import (
	"context"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// scheduleAutoDelete registers a bot message for deletion.
func (d *Detector) scheduleAutoDelete(sent tgbotapi.Message) {
	if d.cfg.AutoDeleteHours <= 0 {
		return
	}
	deleteAt := time.Now().Add(time.Duration(d.cfg.AutoDeleteHours) * time.Hour).Unix()
	if err := d.st.AddBotMessage(sent.Chat.ID, sent.MessageID, deleteAt); err != nil {
		log.Printf("[autodel] регистрация удаления: %v", err)
	}
}

// purgeBotMessages deletes all due bot messages.
func (d *Detector) purgeBotMessages() {
	due, err := d.st.DueBotMessages(time.Now().Unix())
	if err != nil {
		log.Printf("[autodel] запрос должников: %v", err)
		return
	}
	for _, bm := range due {
		if _, err := d.bot.Request(tgbotapi.NewDeleteMessage(bm.ChatID, bm.MsgID)); err != nil {
			log.Printf("[autodel] удаление сообщения %d/%d: %v", bm.ChatID, bm.MsgID, err)
			continue
		}
		if err := d.st.RemoveBotMessage(bm.ChatID, bm.MsgID); err != nil {
			log.Printf("[autodel] снятие из очереди: %v", err)
		}
	}
}

// autoDeleteLoop runs the periodic purge while ctx is active.
func (d *Detector) autoDeleteLoop(ctx context.Context) {
	if d.cfg.AutoDeleteHours <= 0 {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.purgeBotMessages()
		}
	}
}
