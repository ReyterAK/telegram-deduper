package main

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestDocumentMediaType(t *testing.T) {
	cases := []struct {
		mime string
		want string
	}{
		{"video/mp4", MediaTypeVideo},
		{"video/x-matroska", MediaTypeVideo},
		{"image/png", MediaTypePhoto},
		{"image/jpeg", MediaTypePhoto},
		{"application/pdf", MediaTypeDocument},
		{"application/zip", MediaTypeDocument},
		{"", MediaTypeDocument},
	}
	for _, c := range cases {
		if got := documentMediaType(&tgbotapi.Document{MimeType: c.mime}); got != c.want {
			t.Errorf("documentMediaType(%q)=%q, want %q", c.mime, got, c.want)
		}
	}
}

func TestExtractContentDocumentAsFile(t *testing.T) {
	chat := int64(-100123)
	// a video delivered as a document ("send as file") must be
	// classified as a video, so it matches stored videos
	m := &tgbotapi.Message{
		MessageID: 42,
		Chat:      &tgbotapi.Chat{ID: chat, Type: "supergroup"},
		From:      &tgbotapi.User{ID: 7, FirstName: "Иван"},
		Document: &tgbotapi.Document{
			FileID:       "doc-file-id",
			FileUniqueID: "doc-uid-42",
			FileName:     "clip.mp4",
			MimeType:     "video/mp4",
		},
	}
	c := extractContent(m, chat)
	if c.MediaType != MediaTypeVideo {
		t.Fatalf("video sent as file must be classified as video, got %q", c.MediaType)
	}
	if c.MediaUID == "" {
		t.Fatal("document file_unique_id must be extracted")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		hasURL, fwd bool
		want        DupType
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
		MessageID:            50,
		Chat:                 &tgbotapi.Chat{ID: -100123, Type: "supergroup"},
		From:                 &tgbotapi.User{ID: 7},
		ForwardFromChat:      &tgbotapi.Chat{ID: -100999, UserName: "sourcechan"},
		ForwardFromMessageID: 321,
		Text:                 "пост из канала",
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
		MessageID:            51,
		Chat:                 &tgbotapi.Chat{ID: -100123, Type: "supergroup"},
		From:                 &tgbotapi.User{ID: 7},
		ForwardFromChat:      &tgbotapi.Chat{ID: -100999},
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

func TestMessageLink(t *testing.T) {
	if got := messageLink(-1004307533132, 5, "mychat"); got != "https://t.me/mychat/5" {
		t.Fatalf("username link = %q", got)
	}
	// supergroup: the "-100" marker is dropped from c/ links
	if got := messageLink(-1004307533132, 5, ""); got != "https://t.me/c/4307533132/5" {
		t.Fatalf("supergroup c link = %q", got)
	}
	// basic group: only the minus is dropped
	if got := messageLink(-123456789, 5, ""); got != "https://t.me/c/123456789/5" {
		t.Fatalf("basic group c link = %q", got)
	}
}

func TestChatLinkID(t *testing.T) {
	cases := []struct {
		id   int64
		want string
	}{
		{-1004307533132, "4307533132"},
		{-1001234567890, "1234567890"},
		{-123456789, "123456789"},
		{-100, "100"},
	}
	for _, c := range cases {
		if got := chatLinkID(c.id); got != c.want {
			t.Errorf("chatLinkID(%d) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := displayName(&tgbotapi.User{UserName: "ivan"}); got != "@ivan" {
		t.Fatalf("username only = %q", got)
	}
	if got := displayName(&tgbotapi.User{UserName: "ivan", FirstName: "Иван", LastName: "Петров"}); got != "@ivan (Иван Петров)" {
		t.Fatalf("username + name = %q", got)
	}
	if got := displayName(&tgbotapi.User{FirstName: "Иван", LastName: "Петров"}); got != "Иван Петров" {
		t.Fatalf("full name = %q", got)
	}
	if got := displayName(&tgbotapi.User{FirstName: "Иван"}); got != "Иван" {
		t.Fatalf("first name = %q", got)
	}
	if got := displayName(nil); got != "" {
		t.Fatalf("nil name = %q", got)
	}
}

func TestWithAuthor(t *testing.T) {
	if got := withAuthor("Удален дубль сообщения", "@ivan (Иван Петров)"); got != "Удален дубль сообщения — @ivan (Иван Петров)" {
		t.Fatalf("with author = %q", got)
	}
	if got := withAuthor("Удален дубль сообщения", ""); got != "Удален дубль сообщения" {
		t.Fatalf("empty author = %q", got)
	}
}

func TestDeletedShortText(t *testing.T) {
	if got := deletedShortText(DupTypeLink); got != "Удален дубль ссылки" {
		t.Fatalf("link short = %q", got)
	}
	if got := deletedShortText(DupTypeMessage); got != "Удален дубль сообщения" {
		t.Fatalf("message short = %q", got)
	}
}

func TestForwardMatchKeys(t *testing.T) {
	base := MsgContent{NormText: "текст", MediaUID: "uid-1", FwdSource: "fwd:-100:5"}

	// a plain message (not a forward) always keeps all keys,
	// regardless of the policy
	c := base
	c.FwdSource = ""
	text, uid, fwd := forwardMatchKeys(c, ForwardMatchingSourceOnly)
	if text != "текст" || uid != "uid-1" || fwd != "" {
		t.Errorf("non-forward must keep all keys, got %q %q %q", text, uid, fwd)
	}

	// all: a forward keeps every key (historical behavior)
	text, uid, fwd = forwardMatchKeys(base, ForwardMatchingAll)
	if text != "текст" || uid != "uid-1" || fwd != "fwd:-100:5" {
		t.Errorf("all mode must keep all keys, got %q %q %q", text, uid, fwd)
	}

	// source_only: only the forward source survives
	text, uid, fwd = forwardMatchKeys(base, ForwardMatchingSourceOnly)
	if text != "" || uid != "" || fwd != "fwd:-100:5" {
		t.Errorf("source_only must keep only the source, got %q %q %q", text, uid, fwd)
	}

	// ignore: nothing survives
	text, uid, fwd = forwardMatchKeys(base, ForwardMatchingIgnore)
	if text != "" || uid != "" || fwd != "" {
		t.Errorf("ignore must drop all keys, got %q %q %q", text, uid, fwd)
	}
}

func TestPerceptualAllowed(t *testing.T) {
	fwd := MsgContent{FwdSource: "fwd:-100:5"}
	plain := MsgContent{}

	if !perceptualAllowed(plain, ForwardMatchingSourceOnly) {
		t.Error("non-forward must allow perceptual matching under source_only")
	}
	if perceptualAllowed(fwd, ForwardMatchingSourceOnly) {
		t.Error("forward must skip perceptual matching under source_only")
	}
	if perceptualAllowed(fwd, ForwardMatchingIgnore) {
		t.Error("forward must skip perceptual matching under ignore")
	}
	if !perceptualAllowed(fwd, ForwardMatchingAll) {
		t.Error("forward must allow perceptual matching under all")
	}
}
