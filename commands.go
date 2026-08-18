//
// commands.go
// Antidubl — Telegram settings UI
//
// /settings — full configuration menu (admin-only): retention,
// reactions per type×category, warnings per category, auto-delete.
// /status   — brief bot state. /help — short usage text.
//
// All changes are persisted to config.json and applied live
// (the running detector reads the config on every event).
//

package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// foreignChats tracks chats (other than the locked one) where the
// bot was added, so the owner is notified only once per chat.
var foreignChats = map[int64]bool{}

func reactionLabel(r Reaction) string {
	switch r {
	case ReactionIgnore:
		return "Игнорировать"
	case ReactionComment:
		return "Комментировать"
	case ReactionDelete:
		return "Удалять"
	}
	return string(r)
}

func autoDeleteLabel(h int) string {
	if h <= 0 {
		return "выкл"
	}
	return fmt.Sprintf("%d ч", h)
}

func thresholdLabel(t int) string {
	if t <= 0 {
		return "выкл"
	}
	return strconv.Itoa(t)
}

func banTypeLabelShort(bt string) string {
	if bt == "kick" {
		return "удаление из чата"
	}
	return "только чтение"
}

// HandleCommand routes /settings, /status, /help.
func (d *Detector) HandleCommand(m *tgbotapi.Message) {
	switch m.Command() {
	case "settings":
		d.showSettingsMenu(m)
	case "status":
		d.showStatus(m)
	default:
		d.showHelp(m)
	}
}

// HandleCallback routes inline-button presses.
func (d *Detector) HandleCallback(cq *tgbotapi.CallbackQuery) {
	if cq.Message == nil {
		_, _ = d.bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка"))
		return
	}
	if !d.isAdmin(cq.Message.Chat.ID, cq.From.ID) {
		_, _ = d.bot.Request(tgbotapi.NewCallback(cq.ID, "Только для админов чата"))
		return
	}
	d.applyCallback(cq)
}

// isAdmin reports whether the user is an administrator/creator of
// the given chat (required to change settings).
func (d *Detector) isAdmin(chatID, userID int64) bool {
	member, err := d.bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: userID,
		},
	})
	if err != nil {
		log.Printf("[cmd] getChatMember: %v", err)
		return false
	}
	return member.IsAdministrator() || member.IsCreator()
}

// ---------------------------------------------------------------------
// command handlers
// ---------------------------------------------------------------------

func (d *Detector) showHelp(m *tgbotapi.Message) {
	text := "Антидубль — защита чата от дублей.\n\n" +
		"/settings — настройки (только для админов)\n" +
		"/status — состояние бота\n" +
		"/help — эта справка"
	_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, text))
}

func (d *Detector) showStatus(m *tgbotapi.Message) {
	n := 0
	_ = d.st.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&n)
	text := fmt.Sprintf("Антидубль\nЧат: %d\nПериод слежения: %d сут\nАвтоудаление: %s\nСообщений в базе: %d",
		d.chatID, d.cfg.RetentionDays, autoDeleteLabel(d.cfg.AutoDeleteHours), n)
	_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, text))
}

func (d *Detector) showSettingsMenu(m *tgbotapi.Message) {
	if m.Chat.ID != d.chatID {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, "Настройки доступны только в основном чате"))
		return
	}
	if !d.isAdmin(m.Chat.ID, m.From.ID) {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, "Только для админов чата"))
		return
	}
	text, kb := d.mainMenu()
	msg := tgbotapi.NewMessage(m.Chat.ID, text)
	msg.ReplyMarkup = kb
	_, _ = d.bot.Send(msg)
}

// ---------------------------------------------------------------------
// menus
// ---------------------------------------------------------------------

func btn(text, data string) tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardButtonData(text, data)
}

func (d *Detector) mainMenu() (string, tgbotapi.InlineKeyboardMarkup) {
	c := d.cfg
	text := "Настройки Антидубля\n\n" +
		"Период слежения: " + strconv.Itoa(c.RetentionDays) + " сут — повтор сообщения\n" +
		"в течение этого срока считается дублем.\n" +
		"Автоудаление сообщений бота: " + autoDeleteLabel(c.AutoDeleteHours)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn("Период слежения: "+strconv.Itoa(c.RetentionDays)+" сут", "ret:view"),
			btn("−", "ret:-1"),
			btn("+", "ret:+1"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Реакции: ссылка", "m:react:link"),
			btn("Реакции: сообщение", "m:react:message"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Предупреждения: один участник", "m:warn:"+string(CatSameParticipant)),
			btn("Предупреждения: разные", "m:warn:"+string(CatDiffParticipant)),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Автоудаление: "+autoDeleteLabel(c.AutoDeleteHours), "ad:view"),
			btn("выкл", "ad:off"),
			btn("−", "ad:-1"),
			btn("+", "ad:+1"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Закрыть", "close"),
		),
	)
	return text, kb
}

func (d *Detector) reactionsMenu(typ DupType) (string, tgbotapi.InlineKeyboardMarkup) {
	var rs ReactionSettings
	name := "сообщения"
	if typ == DupTypeLink {
		rs = d.cfg.Reactions.Link
		name = "ссылки"
	} else {
		rs = d.cfg.Reactions.Message
	}
	text := "Реакции на дубль " + name + "\n\n" +
		"От одного участника: " + reactionLabel(rs.SameParticipant) + "\n" +
		"От разных участников: " + reactionLabel(rs.DiffParticipant)

	row := func(cat DupCategory) []tgbotapi.InlineKeyboardButton {
		return tgbotapi.NewInlineKeyboardRow(
			btn("Игнорировать", "r:"+string(typ)+":"+string(cat)+":"+string(ReactionIgnore)),
			btn("Комментировать", "r:"+string(typ)+":"+string(cat)+":"+string(ReactionComment)),
			btn("Удалять", "r:"+string(typ)+":"+string(cat)+":"+string(ReactionDelete)),
		)
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(btn("От одного участника: "+reactionLabel(rs.SameParticipant), "noop")),
		row(CatSameParticipant),
		tgbotapi.NewInlineKeyboardRow(btn("От разных участников: "+reactionLabel(rs.DiffParticipant), "noop")),
		row(CatDiffParticipant),
		tgbotapi.NewInlineKeyboardRow(btn("← Назад", "m:main")),
	)
	return text, kb
}

func (d *Detector) warningsMenu(cat DupCategory) (string, tgbotapi.InlineKeyboardMarkup) {
	var ws WarningSettings
	name := "одного участника"
	if cat == CatSameParticipant {
		ws = d.cfg.Warnings.SameParticipant
	} else {
		ws = d.cfg.Warnings.DiffParticipant
		name = "разных участников"
	}
	prefix := "w:" + string(cat)
	text := "Предупреждения (дубли от " + name + ")\n\n" +
		"Порог: " + thresholdLabel(ws.Threshold) + "\n" +
		"Срок жизни: " + strconv.Itoa(ws.LifetimeDays) + " сут\n" +
		"Тип бана: " + banTypeLabelShort(ws.BanType) + "\n" +
		"Срок бана: " + strconv.Itoa(ws.BanDays) + " сут"

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn("Порог: "+thresholdLabel(ws.Threshold), "noop"),
			btn("выкл", prefix+":thr:off"),
			btn("−", prefix+":thr:-1"),
			btn("+", prefix+":thr:+1"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Срок жизни: "+strconv.Itoa(ws.LifetimeDays)+" сут", "noop"),
			btn("−", prefix+":life:-1"),
			btn("+", prefix+":life:+1"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Тип бана: "+banTypeLabelShort(ws.BanType), "noop"),
			btn("Только чтение", prefix+":type:readonly"),
			btn("Удаление", prefix+":type:kick"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Срок бана: "+strconv.Itoa(ws.BanDays)+" сут", "noop"),
			btn("−", prefix+":days:-1"),
			btn("+", prefix+":days:+1"),
		),
		tgbotapi.NewInlineKeyboardRow(btn("← Назад", "m:main")),
	)
	return text, kb
}

// ---------------------------------------------------------------------
// callback handling
// ---------------------------------------------------------------------

// routeMenu returns the menu to render for a callback data prefix.
func (d *Detector) routeMenu(parts []string) (string, tgbotapi.InlineKeyboardMarkup) {
	switch {
	case len(parts) >= 3 && parts[0] == "m" && parts[1] == "react":
		return d.reactionsMenu(DupType(parts[2]))
	case len(parts) >= 3 && parts[0] == "m" && parts[1] == "warn":
		return d.warningsMenu(DupCategory(parts[2]))
	case len(parts) == 2 && parts[0] == "r":
		return d.reactionsMenu(DupType(parts[1]))
	case len(parts) == 2 && parts[0] == "w":
		return d.warningsMenu(DupCategory(parts[1]))
	default:
		return d.mainMenu()
	}
}

func (d *Detector) applyCallback(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID
	parts := strings.Split(cq.Data, ":")

	changed := false
	switch parts[0] {
	case "m":
		// navigation
	case "noop", "close":
		if parts[0] == "close" {
			_, _ = d.bot.Request(tgbotapi.NewEditMessageText(chatID, msgID, "Настройки закрыты"))
		}
		_, _ = d.bot.Request(tgbotapi.NewCallback(cq.ID, ""))
		return
	case "ret":
		switch parts[1] {
		case "+1":
			d.cfg.RetentionDays = clamp(d.cfg.RetentionDays+1, MinRetentionDays, MaxRetentionDays)
			changed = true
		case "-1":
			d.cfg.RetentionDays = clamp(d.cfg.RetentionDays-1, MinRetentionDays, MaxRetentionDays)
			changed = true
		}
	case "ad":
		maxAuto := d.cfg.RetentionDays * 24
		switch parts[1] {
		case "off":
			d.cfg.AutoDeleteHours = 0
			changed = true
		case "+1":
			d.cfg.AutoDeleteHours = clamp(d.cfg.AutoDeleteHours+1, 0, maxAuto)
			changed = true
		case "-1":
			d.cfg.AutoDeleteHours = clamp(d.cfg.AutoDeleteHours-1, 0, maxAuto)
			changed = true
		}
	case "r":
		// r:<type>:<cat>:<reaction>
		if len(parts) == 4 {
			typ, cat, reac := DupType(parts[1]), DupCategory(parts[2]), Reaction(parts[3])
			if validReaction(reac) {
				if typ == DupTypeLink {
					setReaction(&d.cfg.Reactions.Link, cat, reac)
				} else {
					setReaction(&d.cfg.Reactions.Message, cat, reac)
				}
				changed = true
			}
		}
	case "w":
		// w:<cat>:<field>:<value>
		if len(parts) == 4 {
			cat := DupCategory(parts[1])
			ws := d.warningSettingsPtr(cat)
			if ws != nil {
				changed = applyWarningChange(ws, parts[2], parts[3], d.cfg.RetentionDays)
			}
		}
	}

	if changed {
		if err := writeConfig(d.configPath, d.cfg); err != nil {
			log.Printf("[cmd] сохранение конфига: %v", err)
			_, _ = d.bot.Request(tgbotapi.NewCallback(cq.ID, "Ошибка сохранения"))
			return
		}
	}

	// re-render the current menu
	text, kb := d.routeMenu(parts)
	_, _ = d.bot.Request(tgbotapi.NewEditMessageText(chatID, msgID, text))
	_, _ = d.bot.Request(tgbotapi.NewEditMessageReplyMarkup(chatID, msgID, kb))
	_, _ = d.bot.Request(tgbotapi.NewCallback(cq.ID, ""))
}

func setReaction(rs *ReactionSettings, cat DupCategory, r Reaction) {
	if cat == CatSameParticipant {
		rs.SameParticipant = r
	} else {
		rs.DiffParticipant = r
	}
}

func (d *Detector) warningSettingsPtr(cat DupCategory) *WarningSettings {
	if cat == CatSameParticipant {
		return &d.cfg.Warnings.SameParticipant
	}
	return &d.cfg.Warnings.DiffParticipant
}

func applyWarningChange(ws *WarningSettings, field, value string, retention int) bool {
	switch field {
	case "thr":
		switch value {
		case "off":
			ws.Threshold = 0
			return true
		case "+1":
			ws.Threshold = clamp(ws.Threshold+1, 0, 10)
			return true
		case "-1":
			ws.Threshold = clamp(ws.Threshold-1, 0, 10)
			return true
		}
	case "life":
		switch value {
		case "+1":
			ws.LifetimeDays = clamp(ws.LifetimeDays+1, 1, retention)
			return true
		case "-1":
			ws.LifetimeDays = clamp(ws.LifetimeDays-1, 1, retention)
			return true
		}
	case "type":
		if value == "readonly" || value == "kick" {
			ws.BanType = value
			return true
		}
	case "days":
		switch value {
		case "+1":
			ws.BanDays = clamp(ws.BanDays+1, 1, MaxBanDays)
			return true
		case "-1":
			ws.BanDays = clamp(ws.BanDays-1, 1, MaxBanDays)
			return true
		}
	}
	return false
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
