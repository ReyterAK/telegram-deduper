//
// warnings.go
// Antidubl — warning counter and ban
//
// One counter per user (shared across duplicate types). Warnings
// older than the configured lifetime do not count. When the count
// reaches the threshold the user is banned (read-only via
// restrictChatMember, or kicked via banChatMember, both with an
// until_date) and the counter is reset.
//

package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// readOnlyPermissions denies every send capability. Sent as a JSON
// string: MakeRequest encodes params as form data, and the Bot API
// accepts JSON-encoded objects there (v5.5.1 ChatPermissions cannot
// express explicit false because of bool+omitempty).
const readOnlyPermissions = `{"can_send_messages":false,"can_send_media_messages":false,"can_send_polls":false,"can_send_other_messages":false,"can_add_web_page_previews":false}`

func warningSettingsFor(s *Settings, cat DupCategory) WarningSettings {
	if cat == CatSameParticipant {
		return s.Warnings.SameParticipant
	}
	return s.Warnings.DiffParticipant
}

// warnAndMaybeBan records a warning event and bans when the
// threshold is reached. authorName is shown in the ban notice.
func warnAndMaybeBan(d *Detector, chatID, userID int64, cat DupCategory, now time.Time, authorName string, s *Settings) {
	ws := warningSettingsFor(s, cat)
	if ws.Threshold <= 0 {
		return
	}

	if err := d.st.AddWarning(chatID, userID, now.Unix()); err != nil {
		log.Printf("[warn] запись предупреждения: %v", err)
		return
	}

	lifetimeSec := int64(ws.LifetimeDays) * 86400
	count, err := d.st.CountWarnings(chatID, userID, now.Unix()-lifetimeSec)
	if err != nil {
		log.Printf("[warn] подсчёт предупреждений: %v", err)
		return
	}
	if count < ws.Threshold {
		return
	}

	until := now.Add(time.Duration(ws.BanDays) * 24 * time.Hour).Unix()
	var err2 error
	if ws.BanType == "readonly" {
		_, err2 = d.bot.MakeRequest("restrictChatMember", tgbotapi.Params{
			"chat_id":     strconv.FormatInt(chatID, 10),
			"user_id":     strconv.FormatInt(userID, 10),
			"until_date":  strconv.FormatInt(until, 10),
			"permissions": readOnlyPermissions,
		})
	} else {
		cfg := tgbotapi.BanChatMemberConfig{
			ChatMemberConfig: tgbotapi.ChatMemberConfig{ChatID: chatID, UserID: userID},
			UntilDate:        until,
		}
		_, err2 = d.bot.Request(cfg)
	}
	if err2 != nil {
		log.Printf("[warn] бан пользователя %d (%s %d сут): %v", userID, ws.BanType, ws.BanDays, err2)
		// Unbannable target (chat owner, missing rights, API error):
		// inform the chat and reset the counter so it does not
		// accumulate forever against an unbannable user.
		name := authorName
		if name == "" {
			name = strconv.FormatInt(userID, 10)
		}
		notice := fmt.Sprintf("Не удалось применить бан для %s: %v", name, err2)
		if sent, err := d.bot.Send(tgbotapi.NewMessage(chatID, notice)); err == nil {
			d.scheduleAutoDelete(sent, s.AutoDeleteHours)
		}
		if err := d.st.ResetWarnings(chatID, userID); err != nil {
			log.Printf("[warn] сброс предупреждений после ошибки бана: %v", err)
		}
		return
	}

	if err := d.st.ResetWarnings(chatID, userID); err != nil {
		log.Printf("[warn] сброс предупреждений: %v", err)
	}
	log.Printf("[warn] пользователь %d: бан %s на %d суток (%d/%d предупреждений)",
		userID, ws.BanType, ws.BanDays, count, ws.Threshold)

	// Public ban notice with the offender's name.
	name := authorName
	if name == "" {
		name = strconv.FormatInt(userID, 10)
	}
	notice := fmt.Sprintf("Участник %s: бан (%s) на %d суток",
		name, banTypeLabel(ws.BanType), ws.BanDays)
	sent, err := d.bot.Send(tgbotapi.NewMessage(chatID, notice))
	if err != nil {
		log.Printf("[warn] уведомление о бане: %v", err)
		return
	}
	d.scheduleAutoDelete(sent, s.AutoDeleteHours)
}

func banTypeLabel(banType string) string {
	if banType == "kick" {
		return "удаление из чата"
	}
	return "только чтение"
}
