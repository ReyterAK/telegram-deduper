package main

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		hasURL, fwd  bool
		want         DupType
	}{
		{false, false, DupTypeMessage},
		{true, false, DupTypeLink},
		{false, true, DupTypeLink},
		{true, true, DupTypeLink},
	}
	for _, c := range cases {
		if got := classify(c.hasURL, c.fwd); got != c.want {
			t.Errorf("classify(%v,%v)=%v, want %v", c.hasURL, c.fwd, got, c.want)
		}
	}
}

func TestForwardOriginExternal(t *testing.T) {
	chat := int64(-100123)

	// forward from another channel → external
	m := &tgbotapi.Message{ForwardFromChat: &tgbotapi.Chat{ID: -100999}}
	if !forwardOriginExternal(m, chat) {
		t.Error("forward from another channel must be external")
	}

	// forward from our own chat → internal
	m = &tgbotapi.Message{ForwardFromChat: &tgbotapi.Chat{ID: chat}}
	if forwardOriginExternal(m, chat) {
		t.Error("forward from our chat must NOT be external")
	}

	// forward from a user (privacy on: ForwardSenderName) → external
	m = &tgbotapi.Message{ForwardSenderName: "Скрытый"}
	if !forwardOriginExternal(m, chat) {
		t.Error("hidden-user forward must be external")
	}

	// forward from a user (ForwardFrom) → external
	m = &tgbotapi.Message{ForwardFrom: &tgbotapi.User{ID: 777}}
	if !forwardOriginExternal(m, chat) {
		t.Error("user forward must be external")
	}

	// no forward → not external
	if forwardOriginExternal(&tgbotapi.Message{}, chat) {
		t.Error("plain message must not be external")
	}
}

func TestMediaUniqueID(t *testing.T) {
	// photo: largest size wins
	m := &tgbotapi.Message{
		Photo: []tgbotapi.PhotoSize{
			{FileID: "small", FileUniqueID: "uid-small"},
			{FileID: "large", FileUniqueID: "uid-large"},
		},
	}
	if got := mediaUniqueID(m); got != "uid-large" {
		t.Fatalf("photo uid = %q, want uid-large", got)
	}

	m = &tgbotapi.Message{Document: &tgbotapi.Document{FileUniqueID: "uid-doc"}}
	if got := mediaUniqueID(m); got != "uid-doc" {
		t.Fatalf("document uid = %q", got)
	}

	if got := mediaUniqueID(&tgbotapi.Message{}); got != "" {
		t.Fatalf("empty message uid = %q", got)
	}
}

func TestExtractContent(t *testing.T) {
	m := &tgbotapi.Message{
		MessageID: 42,
		Chat:      &tgbotapi.Chat{ID: -100123, Type: "supergroup"},
		From:      &tgbotapi.User{ID: 7},
		Text:      "Смотри: https://EXAMPLE.com/News?UTM_SOURCE=x",
	}
	c := extractContent(m, -100123)
	if c.NormText != "смотри: https://example.com/news" {
		t.Fatalf("norm text = %q", c.NormText)
	}
	if !c.HasURL {
		t.Error("URL not flagged")
	}

	m2 := &tgbotapi.Message{
		MessageID: 43,
		Chat:      &tgbotapi.Chat{ID: -100123, Type: "supergroup"},
		From:      &tgbotapi.User{ID: 7},
		Caption:   "Подпись к фото",
		Photo:     []tgbotapi.PhotoSize{{FileUniqueID: "uid-p"}},
	}
	c2 := extractContent(m2, -100123)
	if c2.MediaUID != "uid-p" {
		t.Fatalf("media uid = %q", c2.MediaUID)
	}
	if c2.NormText != "подпись к фото" {
		t.Fatalf("caption norm = %q", c2.NormText)
	}
}

func TestExtractContentForwardSourceLink(t *testing.T) {
	m := &tgbotapi.Message{
		MessageID:           50,
		Chat:                &tgbotapi.Chat{ID: -100123, Type: "supergroup"},
		From:                &tgbotapi.User{ID: 7},
		ForwardFromChat:     &tgbotapi.Chat{ID: -100999, UserName: "sourcechan"},
		ForwardFromMessageID: 321,
		Text:                "пост из канала",
	}
	c := extractContent(m, -100123)
	if !c.ForwardExternal {
		t.Error("forward must be external")
	}
	if c.SourceLink != "https://t.me/sourcechan/321" {
		t.Fatalf("source link = %q", c.SourceLink)
	}
	if c.FwdSource != "fwd:-100999:321" {
		t.Fatalf("fwd source = %q", c.FwdSource)
	}

	// channel without username → c/ link
	m2 := &tgbotapi.Message{
		MessageID:           51,
		Chat:                &tgbotapi.Chat{ID: -100123, Type: "supergroup"},
		From:                &tgbotapi.User{ID: 7},
		ForwardFromChat:     &tgbotapi.Chat{ID: -100999},
		ForwardFromMessageID: 321,
	}
	c2 := extractContent(m2, -100123)
	if c2.SourceLink != "https://t.me/c/100999/321" {
		t.Fatalf("source link = %q", c2.SourceLink)
	}

	// no source message id → no link
	m3 := &tgbotapi.Message{
		MessageID:       52,
		Chat:            &tgbotapi.Chat{ID: -100123, Type: "supergroup"},
		From:            &tgbotapi.User{ID: 7},
		ForwardFromChat: &tgbotapi.Chat{ID: -100999},
	}
	c3 := extractContent(m3, -100123)
	if c3.SourceLink != "" {
		t.Fatalf("expected empty source link, got %q", c3.SourceLink)
	}
}

func TestDeletedMessageText(t *testing.T) {
	if got := deletedMessageText(DupTypeLink, "https://t.me/c/100/5"); got != "Удален дубль ссылки в сообщении https://t.me/c/100/5" {
		t.Fatalf("with link: %q", got)
	}
	if got := deletedMessageText(DupTypeLink, ""); got != "Удалена пересылка из внешнего источника" {
		t.Fatalf("without link: %q", got)
	}
}

func TestMessageLink(t *testing.T) {
	if got := messageLink(-100123456789, 5, "mychat"); got != "https://t.me/mychat/5" {
		t.Fatalf("username link = %q", got)
	}
	if got := messageLink(-100123456789, 5, ""); got != "https://t.me/c/100123456789/5" {
		t.Fatalf("c link = %q", got)
	}
}
