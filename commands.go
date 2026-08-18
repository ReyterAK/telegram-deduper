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
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// HandleCommand routes /settings, /status, /help.
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
	case "chats":
		d.showChats(m)
	case "allow":
		d.allowChat(m)
	case "deny":
		d.denyChat(m)
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

func helpText() string {
	return "Антидубль — защита чата от дублей сообщений.\n\n" +
		"КАК РАБОТАЕТ\n" +
		"Бот запоминает сообщения чата на период слежения (по умолчанию 10 суток) " +
		"и считает дублем повтор контента в этом окне:\n" +
		"• текст — сравнение регистронезависимое (регистр, пробелы, ё/е не важны);\n" +
		"• ссылки — нормализуются (utm-параметры и мусор отрезаются);\n" +
		"• картинки — по содержимому (dHash): ловит копии, пережатые и изменённые по размеру;\n" +
		"• видео — по содержимому (dHash миниатюры);\n" +
		"• документы — по содержимому файла (SHA-256): ловит переименованные копии,\n" +
		"  а по миниатюре — и пережатые/пересохранённые (PDF и офисные);\n" +
		"• пересылки — повтор из того же источника тоже дубль.\n\n" +
		"ПЕРВОЕ ВХОЖДЕНИЕ НИКОГДА НЕ УДАЛЯЕТСЯ — только повтор.\n\n" +
		"РЕАКЦИИ (настраиваются отдельно для ссылок и сообщений, а также для «от одного " +
		"участника» и «от разных участников»):\n" +
		"• Игнорировать — ничего не делать;\n" +
		"• Комментировать — ответить на дубль с именем автора;\n" +
		"• Удалять — удалить дубль и написать уведомление.\n\n" +
		"ПРЕДУПРЕЖДЕНИЯ (отдельно для «одного» и «разных»):\n" +
		"• порог N — после N дублей участник получает бан;\n" +
		"• срок жизни — предупреждения старше срока не считаются;\n" +
		"• тип бана — только чтение (restrict) или удаление из чата (kick) на N суток.\n" +
		"После бана счётчик обнуляется. Если бан невозможен (владелец чата и т.п.) — " +
		"бот сообщит причину и сбросит счётчик.\n\n" +
		"ПОЛИТИКИ\n" +
		"• Удалённый оригинал: реагировать — дубль удаляется, даже если первое сообщение " +
		"удалили; пропускать — повтор при удалённом оригинале проходит.\n" +
		"• Порог свежести — сообщения старше N минут (например, накопившиеся за простой бота) " +
		"только запоминаются, без удалений и предупреждений; выкл — реагировать на все.\n" +
		"• Автоудаление — сообщения бота (уведомления) удаляются через N часов.\n\n" +
		"КОМАНДЫ\n" +
		"/settings — настройки этого чата (только админы)\n" +
		"/status — состояние бота\n" +
		"/help — эта справка\n" +
		"Владелец: /chats — список разрешённых чатов, /allow <id> — разрешить, /deny <id> — запретить.\n\n" +
		"НАСТРОЙКИ КАЖДОГО ЧАТА НЕЗАВИСИМЫ."
}

func (d *Detector) showHelp(m *tgbotapi.Message) {
	_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, helpText()))
}

func (d *Detector) showStatus(m *tgbotapi.Message) {
	chatID := m.Chat.ID
	s := d.settingsFor(chatID)
	n := 0
	_ = d.st.db.QueryRow("SELECT COUNT(*) FROM messages WHERE chat_id = ?", chatID).Scan(&n)
	text := fmt.Sprintf("Антидубль\nЧат: %d\nПериод слежения: %d сут\nАвтоудаление: %s\nСообщений в базе: %d",
		chatID, s.RetentionDays, autoDeleteLabel(s.AutoDeleteHours), n)
	_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, text))
}

func (d *Detector) showSettingsMenu(m *tgbotapi.Message) {
	if !d.isAllowed(m.Chat.ID) {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, "Бот не активен в этом чате."))
		return
	}
	if !d.isAdmin(m.Chat.ID, m.From.ID) {
		_, _ = d.bot.Send(tgbotapi.NewMessage(m.Chat.ID, "Только для админов чата"))
		return
	}
	text, kb := d.mainMenu(d.settingsFor(m.Chat.ID))
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

func photoModeLabel(mode string) string {
	switch mode {
	case PhotoModeExact:
		return "по ID"
	case PhotoModePerceptual:
		return "по содержимому"
	case PhotoModeOff:
		return "выкл"
	}
	return mode
}

func deletedOriginalLabel(p string) string {
	if p == DeletedOriginalAllow {
		return "пропускать"
	}
	return "реагировать"
}

func freshnessLabel(m int) string {
	if m <= 0 {
		return "выкл"
	}
	return strconv.Itoa(m) + " мин"
}

func (d *Detector) mainMenu(s *Settings) (string, tgbotapi.InlineKeyboardMarkup) {
	text := "Настройки Антидубля (этот чат, применяются мгновенно)\n\n" +
		"Период слежения: " + strconv.Itoa(s.RetentionDays) + " сут — повтор сообщения\n" +
		"в течение этого срока считается дублем.\n" +
		"Картинки: " + photoModeLabel(s.PhotoMode) + "\n" +
		"Видео: " + photoModeLabel(s.VideoMode) + "\n" +
		"Документы: " + photoModeLabel(s.DocMode) + "\n" +
		"Удалённый оригинал: " + deletedOriginalLabel(s.DeletedOriginalPolicy) + "\n" +
		"Порог свежести: " + freshnessLabel(s.FreshnessMinutes) + "\n" +
		"Автоудаление сообщений бота: " + autoDeleteLabel(s.AutoDeleteHours)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn("Период слежения: "+strconv.Itoa(s.RetentionDays)+" сут", "ret:view"),
			btn("−", "ret:-1"),
			btn("+", "ret:+1"),
			btn("✏️", "in:retention"),
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
			btn("Картинки: "+photoModeLabel(s.PhotoMode), "photo:view"),
			btn("по ID", "photo:"+PhotoModeExact),
			btn("по содержимому", "photo:"+PhotoModePerceptual),
			btn("выкл", "photo:"+PhotoModeOff),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Видео: "+photoModeLabel(s.VideoMode), "video:view"),
			btn("по ID", "video:"+PhotoModeExact),
			btn("по содержимому", "video:"+PhotoModePerceptual),
			btn("выкл", "video:"+PhotoModeOff),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Документы: "+photoModeLabel(s.DocMode), "doc:view"),
			btn("по ID", "doc:"+PhotoModeExact),
			btn("по содержимому", "doc:"+PhotoModePerceptual),
			btn("выкл", "doc:"+PhotoModeOff),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Удалённый оригинал: "+deletedOriginalLabel(s.DeletedOriginalPolicy), "delpol:view"),
			btn("Реагировать", "delpol:"+DeletedOriginalStrict),
			btn("Пропускать", "delpol:"+DeletedOriginalAllow),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Порог свежести: "+freshnessLabel(s.FreshnessMinutes), "fresh:view"),
			btn("−5", "fresh:-1"),
			btn("+5", "fresh:+1"),
			btn("выкл", "fresh:off"),
			btn("✏️", "in:freshness"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Автоудаление: "+autoDeleteLabel(s.AutoDeleteHours), "ad:view"),
			btn("выкл", "ad:off"),
			btn("−", "ad:-1"),
			btn("+", "ad:+1"),
			btn("✏️", "in:autodel"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Закрыть", "close"),
		),
	)
	return text, kb
}

func (d *Detector) reactionsMenu(typ DupType, s *Settings) (string, tgbotapi.InlineKeyboardMarkup) {
	var rs ReactionSettings
	name := "сообщения"
	if typ == DupTypeLink {
		rs = s.Reactions.Link
		name = "ссылки"
	} else {
		rs = s.Reactions.Message
	}
	text := "Реакции на дубль " + name + "\n\n" +
		"От одного участника: " + reactionLabel(rs.SameParticipant) + "\n" +
		"От разных участников: " + reactionLabel(rs.DiffParticipant) + "\n\n" +
		"Удалять — удаляет дубль и пишет уведомление;\n" +
		"Комментировать — ответ с именем автора."

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

func (d *Detector) warningsMenu(cat DupCategory, s *Settings) (string, tgbotapi.InlineKeyboardMarkup) {
	var ws WarningSettings
	name := "одного участника"
	if cat == CatSameParticipant {
		ws = s.Warnings.SameParticipant
	} else {
		ws = s.Warnings.DiffParticipant
		name = "разных участников"
	}
	prefix := "w:" + string(cat)
	text := "Предупреждения (дубли от " + name + ")\n\n" +
		"Порог: " + thresholdLabel(ws.Threshold) + "\n" +
		"Срок жизни: " + strconv.Itoa(ws.LifetimeDays) + " сут\n" +
		"Тип бана: " + banTypeLabelShort(ws.BanType) + "\n" +
		"Срок бана: " + strconv.Itoa(ws.BanDays) + " сут\n\n" +
		"Порог — сколько дублей до бана; срок жизни — период действия предупреждений."

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn("Порог: "+thresholdLabel(ws.Threshold), "noop"),
			btn("выкл", prefix+":thr:off"),
			btn("−", prefix+":thr:-1"),
			btn("+", prefix+":thr:+1"),
			btn("✏️", "in:"+prefix+":thr"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("Срок жизни: "+strconv.Itoa(ws.LifetimeDays)+" сут", "noop"),
			btn("−", prefix+":life:-1"),
			btn("+", prefix+":life:+1"),
			btn("✏️", "in:"+prefix+":life"),
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
			btn("✏️", "in:"+prefix+":days"),
		),
		tgbotapi.NewInlineKeyboardRow(btn("← Назад", "m:main")),
	)
	return text, kb
}

// ---------------------------------------------------------------------
// callback handling
// ---------------------------------------------------------------------

// routeMenu returns the menu to render for a callback data prefix.
func (d *Detector) routeMenu(parts []string, s *Settings) (string, tgbotapi.InlineKeyboardMarkup) {
	switch {
	case len(parts) >= 3 && parts[0] == "m" && parts[1] == "react":
		return d.reactionsMenu(DupType(parts[2]), s)
	case len(parts) >= 3 && parts[0] == "m" && parts[1] == "warn":
		return d.warningsMenu(DupCategory(parts[2]), s)
	case len(parts) == 2 && parts[0] == "r":
		return d.reactionsMenu(DupType(parts[1]), s)
	case len(parts) == 2 && parts[0] == "w":
		return d.warningsMenu(DupCategory(parts[1]), s)
	default:
		return d.mainMenu(s)
	}
}

func (d *Detector) applyCallback(cq *tgbotapi.CallbackQuery) {
	chatID := cq.Message.Chat.ID
	msgID := cq.Message.MessageID
	parts := strings.Split(cq.Data, ":")
	s := d.settingsFor(chatID)

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
			s.RetentionDays = clamp(s.RetentionDays+1, MinRetentionDays, MaxRetentionDays)
			changed = true
		case "-1":
			s.RetentionDays = clamp(s.RetentionDays-1, MinRetentionDays, MaxRetentionDays)
			changed = true
		}
	case "ad":
		maxAuto := s.RetentionDays * 24
		switch parts[1] {
		case "off":
			s.AutoDeleteHours = 0
			changed = true
		case "+1":
			s.AutoDeleteHours = clamp(s.AutoDeleteHours+1, 0, maxAuto)
			changed = true
		case "-1":
			s.AutoDeleteHours = clamp(s.AutoDeleteHours-1, 0, maxAuto)
			changed = true
		}
	case "in":
		if len(parts) == 2 && findNumericField(parts[1]) != nil {
			d.pending[chatID] = pendingInput{
				userID:    cq.From.ID,
				field:     parts[1],
				menuMsgID: msgID,
				at:        time.Now(),
			}
			_, _ = d.bot.Send(tgbotapi.NewMessage(chatID, inputPrompt(s, parts[1])))
			_, _ = d.bot.Request(tgbotapi.NewCallback(cq.ID, ""))
			return
		}
	case "fresh":
		switch parts[1] {
		case "off":
			s.FreshnessMinutes = 0
			changed = true
		case "+1":
			s.FreshnessMinutes = clamp(s.FreshnessMinutes+5, 0, MaxFreshnessMinutes)
			changed = true
		case "-1":
			s.FreshnessMinutes = clamp(s.FreshnessMinutes-5, 0, MaxFreshnessMinutes)
			changed = true
		}
	case "delpol":
		if len(parts) == 2 {
			switch parts[1] {
			case DeletedOriginalAllow, DeletedOriginalStrict:
				s.DeletedOriginalPolicy = parts[1]
				changed = true
			}
		}
	case "photo":
		if len(parts) == 2 {
			switch parts[1] {
			case PhotoModeExact, PhotoModePerceptual, PhotoModeOff:
				s.PhotoMode = parts[1]
				changed = true
			}
		}
	case "video":
		if len(parts) == 2 {
			switch parts[1] {
			case PhotoModeExact, PhotoModePerceptual, PhotoModeOff:
				s.VideoMode = parts[1]
				changed = true
			}
		}
	case "doc":
		if len(parts) == 2 {
			switch parts[1] {
			case PhotoModeExact, PhotoModePerceptual, PhotoModeOff:
				s.DocMode = parts[1]
				changed = true
			}
		}
	case "r":
		// r:<type>:<cat>:<reaction>
		if len(parts) == 4 {
			typ, cat, reac := DupType(parts[1]), DupCategory(parts[2]), Reaction(parts[3])
			if validReaction(reac) {
				if typ == DupTypeLink {
					setReaction(&s.Reactions.Link, cat, reac)
				} else {
					setReaction(&s.Reactions.Message, cat, reac)
				}
				changed = true
			}
		}
	case "w":
		// w:<cat>:<field>:<value>
		if len(parts) == 4 {
			cat := DupCategory(parts[1])
			ws := warningSettingsPtr(s, cat)
			if ws != nil {
				changed = applyWarningChange(ws, parts[2], parts[3], s.RetentionDays)
			}
		}
	}

	if changed {
		d.persistSettings(chatID, s)
	}

	// re-render the current menu
	text, kb := d.routeMenu(parts, s)
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

func warningSettingsPtr(s *Settings, cat DupCategory) *WarningSettings {
	if cat == CatSameParticipant {
		return &s.Warnings.SameParticipant
	}
	return &s.Warnings.DiffParticipant
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
