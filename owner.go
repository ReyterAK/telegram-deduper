//
// owner.go
// Antidubl — owner commands: manage the chat allowlist from Telegram
//
// /chats         — list allowed chats (id + title)
// /allow <id>    — add a chat to the allowlist
// /deny <id>     — remove a chat from the allowlist
//
// Owner = the user id in config (owner_user_id). Commands work in
// the private chat with the bot and in any allowed chat. Changes are
// persisted to config.json and apply immediately (no restart).
//

package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// isOwner reports whether the user is the configured owner.
func (d *Detector) isOwner(userID int64) bool {
	return d.cfg.OwnerUserID != 0 && userID == d.cfg.OwnerUserID
}

// chatTitle returns the cached title of a chat ("" when unknown).
func (d *Detector) chatTitle(chatID int64) string {
	if t, ok := d.chatTitleCache[chatID]; ok {
		return t
	}
	title := ""
	if chat, err := d.bot.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}); err == nil {
		title = chat.Title
	} else {
		log.Printf("[owner] имя чата %d: %v", chatID, err)
	}
	d.chatTitleCache[chatID] = title
	return title
}

func (d *Detector) showChats(m *tgbotapi.Message) {
	if !d.isOwner(m.From.ID) {
		return
	}
	if len(d.cfg.AllowedChats) == 0 {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
			"Список разрешённых чатов пуст — бот работает во всех чатах, где он админ."))
		return
	}
	text := "Разрешённые чаты:\n"
	for _, id := range d.cfg.AllowedChats {
		text += fmt.Sprintf("%d — %s\n", id, d.chatTitle(id))
	}
	text += "\nДобавить: /allow <id>\nУдалить: /deny <id>"
	_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, text))
}

func (d *Detector) allowChat(m *tgbotapi.Message) {
	if !d.isOwner(m.From.ID) {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(m.CommandArguments()), 10, 64)
	if err != nil {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /allow <id чата>"))
		return
	}
	for _, c := range d.cfg.AllowedChats {
		if c == id {
			_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("Чат %d уже в списке.", id)))
			return
		}
	}
	d.cfg.AllowedChats = append(d.cfg.AllowedChats, id)
	d.persistAllowlist()
	_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
		fmt.Sprintf("Чат %d (%s) разрешён.", id, d.chatTitle(id))))
}

func (d *Detector) denyChat(m *tgbotapi.Message) {
	if !d.isOwner(m.From.ID) {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(m.CommandArguments()), 10, 64)
	if err != nil {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /deny <id чата>"))
		return
	}
	out := d.cfg.AllowedChats[:0]
	found := false
	for _, c := range d.cfg.AllowedChats {
		if c == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("Чат %d не в списке.", id)))
		return
	}
	d.cfg.AllowedChats = out
	d.persistAllowlist()
	_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, fmt.Sprintf("Чат %d удалён из разрешённых.", id)))
}

// persistAllowlist saves the config (token and allowlist included).
func (d *Detector) persistAllowlist() {
	if err := writeConfig(d.configPath, d.cfg); err != nil {
		log.Printf("[owner] сохранение конфига: %v", err)
	}
}
