//
// input.go
// Antidubl — text input for numeric settings
//
// The settings menu offers a "✏️" button next to numeric fields.
// Pressing it asks the user to type a value; the next text message
// from the same user in the same chat is parsed, validated, applied
// and the open menu is refreshed with the new value.
//

package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// numericField describes a numeric setting editable by text.
type numericField struct {
	id    string
	label string
	min   func(s *Settings) int
	max   func(s *Settings) int
	set   func(s *Settings, v int)
	// value formats the current value (for confirmations).
	value func(s *Settings) string
}

var numericFields = []numericField{
	{
		id:    "retention",
		label: "Период слежения (сут)",
		min:   func(s *Settings) int { return MinRetentionDays },
		max:   func(s *Settings) int { return MaxRetentionDays },
		set:   func(s *Settings, v int) { s.RetentionDays = v },
		value: func(s *Settings) string { return strconv.Itoa(s.RetentionDays) + " сут" },
	},
	{
		id:    "freshness",
		label: "Порог свежести (мин, 0 = выкл)",
		min:   func(s *Settings) int { return 0 },
		max:   func(s *Settings) int { return MaxFreshnessMinutes },
		set:   func(s *Settings, v int) { s.FreshnessMinutes = v },
		value: func(s *Settings) string { return freshnessLabel(s.FreshnessMinutes) },
	},
	{
		id:    "autodel",
		label: "Автоудаление сообщений бота (ч, 0 = выкл)",
		min:   func(s *Settings) int { return 0 },
		max:   func(s *Settings) int { return s.RetentionDays * 24 },
		set:   func(s *Settings, v int) { s.AutoDeleteHours = v },
		value: func(s *Settings) string { return autoDeleteLabel(s.AutoDeleteHours) },
	},
}

func warnFields(cat DupCategory) []numericField {
	catLabel := "один участник"
	if cat == CatDiffParticipant {
		catLabel = "разные участники"
	}
	prefix := "warn:" + string(cat)
	return []numericField{
		{
			id:    prefix + ":thr",
			label: "Порог предупреждений (" + catLabel + ", 0 = выкл)",
			min:   func(s *Settings) int { return 0 },
			max:   func(s *Settings) int { return 10 },
			set: func(s *Settings, v int) {
				warningSettingsPtr(s, cat).Threshold = v
			},
			value: func(s *Settings) string { return thresholdLabel(warningSettingsPtr(s, cat).Threshold) },
		},
		{
			id:    prefix + ":life",
			label: "Срок жизни предупреждений (" + catLabel + ", сут)",
			min:   func(s *Settings) int { return 1 },
			max:   func(s *Settings) int { return s.RetentionDays },
			set: func(s *Settings, v int) {
				warningSettingsPtr(s, cat).LifetimeDays = v
			},
			value: func(s *Settings) string { return strconv.Itoa(warningSettingsPtr(s, cat).LifetimeDays) + " сут" },
		},
		{
			id:    prefix + ":days",
			label: "Срок бана (" + catLabel + ", сут)",
			min:   func(s *Settings) int { return 1 },
			max:   func(s *Settings) int { return MaxBanDays },
			set: func(s *Settings, v int) {
				warningSettingsPtr(s, cat).BanDays = v
			},
			value: func(s *Settings) string { return strconv.Itoa(warningSettingsPtr(s, cat).BanDays) + " сут" },
		},
	}
}

func allNumericFields() []numericField {
	out := append([]numericField{}, numericFields...)
	out = append(out, warnFields(CatSameParticipant)...)
	out = append(out, warnFields(CatDiffParticipant)...)
	return out
}

func findNumericField(id string) *numericField {
	for i := range allNumericFields() {
		if allNumericFields()[i].id == id {
			return &allNumericFields()[i]
		}
	}
	return nil
}

// applyTextValue parses and applies a typed value for a numeric
// field. Returns an error message ("" on success) and whether the
// value was applied.
func applyTextValue(s *Settings, fieldID, text string) (string, bool) {
	f := findNumericField(fieldID)
	if f == nil {
		return "Неизвестное поле", false
	}
	v, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return "Введите целое число", false
	}
	min, max := f.min(s), f.max(s)
	if v < min || v > max {
		return fmt.Sprintf("Допустимый диапазон: %d–%d", min, max), false
	}
	f.set(s, v)
	return "", true
}

// inputPrompt returns the message asking for a value.
func inputPrompt(s *Settings, fieldID string) string {
	f := findNumericField(fieldID)
	if f == nil {
		return "Неизвестное поле"
	}
	return fmt.Sprintf("Введите значение для «%s»: %d–%d", f.label, f.min(s), f.max(s))
}

// menuPartsForField returns the callback parts that render the menu
// the field belongs to (for refreshing after text input).
func menuPartsForField(fieldID string) []string {
	switch {
	case strings.HasPrefix(fieldID, "warn:"+string(CatSameParticipant)):
		return []string{"m", "warn", string(CatSameParticipant)}
	case strings.HasPrefix(fieldID, "warn:"+string(CatDiffParticipant)):
		return []string{"m", "warn", string(CatDiffParticipant)}
	default:
		return []string{"m", "main"}
	}
}

// HandleTextInput consumes a text message as a value for a pending
// numeric-field request. Returns true when the message was consumed
// (not a regular chat message).
func (d *Detector) HandleTextInput(m *tgbotapi.Message) bool {
	p, ok := d.pending[m.Chat.ID]
	if !ok || p.userID != m.From.ID || m.Text == "" {
		return false
	}
	delete(d.pending, m.Chat.ID)
	chatID := m.Chat.ID

	if time.Since(p.at) > 5*time.Minute {
		_, _ = d.bot.Send(tgbotapi.NewMessage(chatID, "Время на ввод истекло — нажмите «✏️» ещё раз."))
		return true
	}

	s := d.settingsFor(chatID)
	errMsg, ok := applyTextValue(s, p.field, m.Text)
	if !ok {
		_, _ = d.bot.Send(tgbotapi.NewMessage(chatID, errMsg))
		return true
	}
	d.persistSettings(chatID, s)

	f := findNumericField(p.field)
	_, _ = d.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Готово: %s = %s", f.label, f.value(s))))

	// refresh the open menu with the new value
	text, kb := d.routeMenu(menuPartsForField(p.field), s)
	_, _ = d.bot.Request(tgbotapi.NewEditMessageText(chatID, p.menuMsgID, text))
	_, _ = d.bot.Request(tgbotapi.NewEditMessageReplyMarkup(chatID, p.menuMsgID, kb))
	return true
}
