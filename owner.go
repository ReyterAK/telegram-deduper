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

// fullPermissions restores every send/admin-able permission a member
// can have — the "unrestricted" state (restrictChatMember with this
// object lifts a readonly restriction).
const fullPermissions = `{"can_send_messages":true,"can_send_audios":true,"can_send_documents":true,"can_send_photos":true,"can_send_videos":true,"can_send_video_notes":true,"can_send_voice_notes":true,"can_send_polls":true,"can_send_other_messages":true,"can_add_web_page_previews":true,"can_change_info":true,"can_invite_users":true,"can_pin_messages":true,"can_manage_topics":true}`

// parseUnbanArgs parses "/unban <user_id> [chat_id]". Without a chat
// id the command's own chat is used; in a private chat the id is
// required (there is no chat to default to).
func parseUnbanArgs(args string, cmdChatID int64, cmdChatType string) (int64, int64, error) {
	fields := strings.Fields(args)
	if len(fields) < 1 || len(fields) > 2 {
		return 0, 0, fmt.Errorf("нужны 1-2 аргумента")
	}
	userID, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	if len(fields) == 2 {
		chatID, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		return userID, chatID, nil
	}
	if cmdChatType == "private" {
		return 0, 0, fmt.Errorf("в личном чате нужен id чата")
	}
	return userID, cmdChatID, nil
}

// unbanUser is the owner-only emergency lift of a ban: restores
// full rights for a restricted member, or unbans a kicked one so
// they can rejoin. Works through the bot's own admin rights — the
// owner does not need to be an admin of the target chat.
func (d *Detector) unbanUser(m *tgbotapi.Message) {
	if !d.isOwner(m.From.ID) {
		return
	}
	userID, chatID, err := parseUnbanArgs(m.CommandArguments(), m.Chat.ID, m.Chat.Type)
	if err != nil {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, "Использование: /unban <user_id> [chat_id]"))
		return
	}

	member, err := d.bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: chatID, UserID: userID},
	})
	if err != nil {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
			fmt.Sprintf("Не удалось проверить статус %d в чате %d: %v", userID, chatID, err)))
		return
	}

	switch member.Status {
	case "restricted":
		if _, err := d.bot.MakeRequest("restrictChatMember", tgbotapi.Params{
			"chat_id":     strconv.FormatInt(chatID, 10),
			"user_id":     strconv.FormatInt(userID, 10),
			"permissions": fullPermissions,
		}); err != nil {
			_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
				fmt.Sprintf("Не удалось снять ограничение для %d: %v", userID, err)))
			return
		}
		log.Printf("[owner] чат %d: снято ограничение (readonly) пользователя %d", chatID, userID)
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
			fmt.Sprintf("Ограничение снято: %d может снова писать в чат %d.", userID, chatID)))
	case "kicked":
		if _, err := d.bot.MakeRequest("unbanChatMember", tgbotapi.Params{
			"chat_id": strconv.FormatInt(chatID, 10),
			"user_id": strconv.FormatInt(userID, 10),
		}); err != nil {
			_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
				fmt.Sprintf("Не удалось снять бан для %d: %v", userID, err)))
			return
		}
		log.Printf("[owner] чат %d: снят бан (kick) пользователя %d", chatID, userID)
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
			fmt.Sprintf("Бан снят: %d может вернуться в чат %d по ссылке.", userID, chatID)))
	case "member", "administrator", "creator":
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
			fmt.Sprintf("Пользователь %d в чате %d не ограничен (%s).", userID, chatID, member.Status)))
	default:
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID,
			fmt.Sprintf("Неизвестный статус %q — бан не менялся.", member.Status)))
	}
}

// persistAllowlist saves the config (token and allowlist included).
func (d *Detector) persistAllowlist() {
	if err := writeConfig(d.configPath, d.cfg); err != nil {
		log.Printf("[owner] сохранение конфига: %v", err)
	}
}
