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

// isBannedStatus reports whether a chat member status means the user
// cannot send messages (banned or restricted).
func isBannedStatus(status string) bool {
	return status == "kicked" || status == "restricted"
}

// userBanned reports whether the user is currently banned/muted in
// the chat. Banned users get no warning accounting — duplicates are
// still deleted with a notice, but the counter does not grow and no
// second ban is issued.
func (d *Detector) userBanned(chatID, userID int64) bool {
	member, err := d.bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: userID,
		},
	})
	if err != nil {
		log.Printf("[warn] getChatMember (%d) чат %d: %v", userID, chatID, err)
		return false
	}
	return isBannedStatus(member.Status)
}

// warnAndMaybeBan records a warning event and bans when the
// threshold is reached. authorName is shown in the ban notice.
func warnAndMaybeBan(d *Detector, chatID, userID int64, cat DupCategory, now time.Time, authorName string, s *Settings) {
	ws := warningSettingsFor(s, cat)
	if ws.Threshold <= 0 {
		return
	}
	// Already banned/muted: no warnings, no repeated bans.
	if d.userBanned(chatID, userID) {
		log.Printf("[warn] чат %d: пользователь %d уже ограничен — предупреждение не выносится", chatID, userID)
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
		log.Printf("[warn] чат %d: бан пользователя %d (%s %d сут): %v", chatID, userID, ws.BanType, ws.BanDays, err2)
		// Unbannable target (chat owner, missing rights, API error):
		// inform (per notice_mode) and reset the counter so it does
		// not accumulate forever against an unbannable user.
		name := authorName
		if name == "" {
			name = strconv.FormatInt(userID, 10)
		}
		notice := banFailNoticeText(name, s.NoticeMode, err2)
		d.postNotice(chatID, userID, notice, s)
		if err := d.st.ResetWarnings(chatID, userID); err != nil {
			log.Printf("[warn] сброс предупреждений после ошибки бана: %v", err)
		}
		return
	}

	if err := d.st.ResetWarnings(chatID, userID); err != nil {
		log.Printf("[warn] сброс предупреждений: %v", err)
	}
	log.Printf("[warn] чат %d: пользователь %d: бан %s на %d суток (%d/%d предупреждений)",
		chatID, userID, ws.BanType, ws.BanDays, count, ws.Threshold)

	// Ban notice with the offender's name (or without, per mode).
	name := authorName
	if name == "" {
		name = strconv.FormatInt(userID, 10)
	}
	notice := banNoticeText(name, s.NoticeMode, ws)
	d.postNotice(chatID, userID, notice, s)
}

// postNotice delivers a moderation notice per the chat's notice_mode:
// publicly (with auto-delete tracking) or ephemeral to the offender.
func (d *Detector) postNotice(chatID, userID int64, text string, s *Settings) {
	if s.NoticeMode == NoticeModeEphemeral {
		d.sendEphemeral(chatID, userID, text, nil)
		return
	}
	sent, err := d.bot.Send(tgbotapi.NewMessage(chatID, text))
	if err != nil {
		log.Printf("[warn] уведомление: %v", err)
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

// banNoticeText builds the public/offender ban notice per
// notice_mode: "full" names the offender, "short" does not
// (deterrent without a "донос"), "ephemeral" addresses only the
// offender (delivered via sendEphemeral).
func banNoticeText(authorName, mode string, ws WarningSettings) string {
	switch mode {
	case NoticeModeFull:
		return fmt.Sprintf("Участник %s: бан (%s) на %d суток",
			authorName, banTypeLabel(ws.BanType), ws.BanDays)
	case NoticeModeEphemeral:
		return fmt.Sprintf("Бан: %s на %d суток", banTypeLabel(ws.BanType), ws.BanDays)
	default: // short
		return fmt.Sprintf("Участник ограничен (%s) на %d суток",
			banTypeLabel(ws.BanType), ws.BanDays)
	}
}

// banFailNoticeText builds the ban-failure notice per notice_mode.
func banFailNoticeText(authorName, mode string, err error) string {
	switch mode {
	case NoticeModeFull:
		return fmt.Sprintf("Не удалось применить бан для %s: %v", authorName, err)
	case NoticeModeEphemeral:
		return fmt.Sprintf("Бан не применён: %v", err)
	default: // short
		return fmt.Sprintf("Не удалось применить бан: %v", err)
	}
}
