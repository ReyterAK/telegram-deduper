//
// notice_test.go
// Antidubl — notice-mode text builders and per-chat migration
//

package main

import (
	"errors"
	"testing"
)

func TestNoticeText(t *testing.T) {
	cases := []struct {
		name string
		typ  DupType
		mode string
		want string
	}{
		{"full link", DupTypeLink, NoticeModeFull, "Удален дубль ссылки — Иван"},
		{"full message", DupTypeMessage, NoticeModeFull, "Удален дубль сообщения — Иван"},
		{"short link", DupTypeLink, NoticeModeShort, "Удален дубль ссылки"},
		{"short message", DupTypeMessage, NoticeModeShort, "Удален дубль сообщения"},
		{"ephemeral link", DupTypeLink, NoticeModeEphemeral, "Удален дубль ссылки"},
		{"ephemeral message", DupTypeMessage, NoticeModeEphemeral, "Удален дубль сообщения"},
	}
	for _, c := range cases {
		if got := noticeText(c.typ, "Иван", c.mode); got != c.want {
			t.Errorf("noticeText(%s, Иван, %s) = %q, want %q", c.typ, c.mode, got, c.want)
		}
	}
	// empty author in full mode: no dangling dash
	if got := noticeText(DupTypeMessage, "", NoticeModeFull); got != "Удален дубль сообщения" {
		t.Errorf("noticeText without name = %q", got)
	}
}

func TestBanNoticeText(t *testing.T) {
	ws := WarningSettings{BanType: "readonly", BanDays: 3}
	cases := []struct {
		mode string
		want string
	}{
		{NoticeModeFull, "Участник Иван: бан (только чтение) на 3 суток"},
		{NoticeModeShort, "Участник ограничен (только чтение) на 3 суток"},
		{NoticeModeEphemeral, "Бан: только чтение на 3 суток"},
	}
	for _, c := range cases {
		if got := banNoticeText("Иван", c.mode, ws); got != c.want {
			t.Errorf("banNoticeText(%s) = %q, want %q", c.mode, got, c.want)
		}
	}

	wsKick := WarningSettings{BanType: "kick", BanDays: 7}
	if got := banNoticeText("Петя", NoticeModeShort, wsKick); got != "Участник ограничен (удаление из чата) на 7 суток" {
		t.Errorf("kick short = %q", got)
	}
}

func TestBanFailNoticeText(t *testing.T) {
	err := errors.New("can't remove chat owner")
	cases := []struct {
		mode string
		want string
	}{
		{NoticeModeFull, "Не удалось применить бан для Иван: can't remove chat owner"},
		{NoticeModeShort, "Не удалось применить бан: can't remove chat owner"},
		{NoticeModeEphemeral, "Бан не применён: can't remove chat owner"},
	}
	for _, c := range cases {
		if got := banFailNoticeText("Иван", c.mode, err); got != c.want {
			t.Errorf("banFailNoticeText(%s) = %q, want %q", c.mode, got, c.want)
		}
	}
}

// TestNoticeModeMigration covers the per-chat settings migration:
// stored JSON without notice_mode must yield "full" (like
// forward_matching/freshness_minutes migrations), while keys that
// ARE present are preserved.
func TestNoticeModeMigration(t *testing.T) {
	// a stored per-chat settings blob from before notice_mode existed
	raw := `{"retention_days":10,"reactions":{"link":{"same_participant":"delete","diff_participant":"delete"},"message":{"same_participant":"delete","diff_participant":"ignore"}},"warnings":{"same_participant":{"threshold":3,"lifetime_days":10,"ban_type":"readonly","ban_days":1},"diff_participant":{"threshold":0,"lifetime_days":10,"ban_type":"readonly","ban_days":1}},"auto_delete_hours":1,"photo_mode":"perceptual","video_mode":"perceptual","doc_mode":"perceptual","forward_matching":"source_only","deleted_original_policy":"strict","freshness_minutes":5}`
	s := DefaultConfig().asSettings()
	if err := unmarshalSettings([]byte(raw), s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.NoticeMode != NoticeModeFull {
		t.Errorf("migrated notice_mode = %q, want %q", s.NoticeMode, NoticeModeFull)
	}
	// a key that IS present is preserved, not overwritten
	if s.ForwardMatching != "source_only" {
		t.Errorf("forward_matching = %q, want preserved %q", s.ForwardMatching, "source_only")
	}

	// blob without forward_matching at all → default "all"
	rawOld := `{"retention_days":10,"auto_delete_hours":0}`
	s2 := DefaultConfig().asSettings()
	if err := unmarshalSettings([]byte(rawOld), s2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s2.ForwardMatching != ForwardMatchingAll {
		t.Errorf("migrated forward_matching = %q, want %q", s2.ForwardMatching, ForwardMatchingAll)
	}
	if s2.NoticeMode != NoticeModeFull {
		t.Errorf("migrated notice_mode = %q, want %q", s2.NoticeMode, NoticeModeFull)
	}
	if s2.FreshnessMinutes != DefaultFreshnessMinutes {
		t.Errorf("migrated freshness = %d, want %d", s2.FreshnessMinutes, DefaultFreshnessMinutes)
	}
}
