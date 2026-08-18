//
// detector.go
// Antidubl — duplicate detection and classification
//
// A message is a duplicate when, within the retention window, the
// store contains:
//   - the same normalized FULL text (media-less messages), or
//   - the same media file_unique_id (media messages; caption text
//     alone is NOT compared — different pictures with the same
//     comment are not duplicates), or
//   - the message is forwarded from another chat (always).
//
// Classification:
//   type     = link (contains URL or external forward) | message
//   category = same_participant (same author repeated) | diff_participant
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

// MsgContent is the comparable essence of an incoming message.
type MsgContent struct {
	ChatID          int64
	MsgID           int
	UserID          int64
	AuthorName      string
	NormText        string
	HasURL          bool
	MediaUID        string
	ForwardExternal bool
	// FwdSource identifies the origin of an external forward
	// ("fwd:<chat_id>:<msg_id>", "" when unknown/not a forward).
	FwdSource string
	// SourceLink points at the original post for external forwards
	// ("" when the source is unknown).
	SourceLink string
	// PhotoHash is the perceptual dHash of a photo message ("" when
	// not computed).
	PhotoHash string
}

// Detector wires store + telegram into the duplicate pipeline.
type Detector struct {
	cfg          *Config
	st           *Store
	bot          *tgbotapi.BotAPI
	configPath   string
	chatID       int64
	chatUsername string
}

func NewDetector(cfg *Config, st *Store, bot *tgbotapi.BotAPI, configPath string) *Detector {
	return &Detector{
		cfg:        cfg,
		st:         st,
		bot:        bot,
		configPath: configPath,
		chatID:     cfg.ChatID,
	}
}

// extractContent builds the comparable content of a message.
func extractContent(m *tgbotapi.Message, chatID int64) MsgContent {
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	norm := NormalizeText(text)
	mediaUID := mediaUniqueID(m)
	forwardExternal := forwardOriginExternal(m, chatID)
	fwdSource := ""
	sourceLink := ""
	if m.ForwardFromChat != nil {
		fwdSource = fmt.Sprintf("fwd:%d:%d", m.ForwardFromChat.ID, m.ForwardFromMessageID)
		sourceLink = chatMessageLink(m.ForwardFromChat.ID, m.ForwardFromChat.UserName, m.ForwardFromMessageID)
	}
	return MsgContent{
		ChatID:          chatID,
		MsgID:           m.MessageID,
		UserID:          userIDOf(m),
		AuthorName:      displayName(m.From),
		NormText:        norm,
		HasURL:          norm != "" && HasURL(norm),
		MediaUID:        mediaUID,
		ForwardExternal: forwardExternal,
		FwdSource:       fwdSource,
		SourceLink:      sourceLink,
	}
}

func userIDOf(m *tgbotapi.Message) int64 {
	if m.From != nil {
		return m.From.ID
	}
	return 0
}

// displayName renders a user for reaction texts: "@username
// (Имя Фамилия)" when both are known, "@username" or "Имя Фамилия"
// otherwise.
func displayName(u *tgbotapi.User) string {
	if u == nil {
		return ""
	}
	realName := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.UserName != "" {
		if realName != "" {
			return "@" + u.UserName + " (" + realName + ")"
		}
		return "@" + u.UserName
	}
	return realName
}

// mediaUniqueID returns the content fingerprint for media messages
// ("" when there is no comparable media). For photos the largest
// size is used — Telegram assigns the same file_unique_id to
// byte-identical uploads.
func mediaUniqueID(m *tgbotapi.Message) string {
	switch {
	case len(m.Photo) > 0:
		return m.Photo[len(m.Photo)-1].FileUniqueID
	case m.Document != nil:
		return m.Document.FileUniqueID
	case m.Video != nil:
		return m.Video.FileUniqueID
	case m.Audio != nil:
		return m.Audio.FileUniqueID
	case m.Voice != nil:
		return m.Voice.FileUniqueID
	case m.Animation != nil:
		return m.Animation.FileUniqueID
	case m.VideoNote != nil:
		return m.VideoNote.FileUniqueID
	case m.Sticker != nil:
		return m.Sticker.FileUniqueID
	}
	return ""
}

// forwardOriginExternal reports whether the message was forwarded
// from outside our chat: another channel/chat (ForwardFromChat) or
// a user (ForwardFrom / hidden sender — ForwardSenderName).
func forwardOriginExternal(m *tgbotapi.Message, chatID int64) bool {
	if m.ForwardFromChat != nil {
		return m.ForwardFromChat.ID != chatID
	}
	return m.ForwardFrom != nil || m.ForwardSenderName != ""
}

// classify determines the duplicate type.
func classify(hasURL, forwardExternal bool) DupType {
	if forwardExternal || hasURL {
		return DupTypeLink
	}
	return DupTypeMessage
}

// Process handles one incoming message: store, detect, react.
func (d *Detector) Process(m *tgbotapi.Message) {
	// Only group/supergroup messages from identifiable users.
	if m.From == nil {
		return
	}
	if m.From.ID == d.bot.Self.ID {
		return
	}
	if !m.Chat.IsGroup() && !m.Chat.IsSuperGroup() {
		return
	}

	chatID := m.Chat.ID

	// Auto-lock to the first group seen when chat_id is not set.
	if d.chatID == 0 {
		d.chatID = chatID
		if err := d.cfg.SaveChatID(d.configPath, chatID); err != nil {
			log.Printf("[detect] не удалось сохранить chat_id=%d: %v", chatID, err)
		}
		log.Printf("[detect] chat_id не задан, зафиксирован чат %d", chatID)
	}
	if chatID != d.chatID {
		// The bot is locked to one chat; other chats are ignored.
		// Notify the owner once per foreign chat for visibility.
		if !foreignChats[chatID] {
			foreignChats[chatID] = true
			name := m.Chat.Title
			if name == "" {
				name = strconv.FormatInt(chatID, 10)
			}
			log.Printf("[detect] бота добавили в чужой чат %q (%d) — игнорируется", name, chatID)
			_, err := d.bot.Send(tgbotapi.NewMessage(d.chatID,
				fmt.Sprintf("Бота добавили в чужой чат «%s» (id %d).\nБот его игнорирует.", name, chatID)))
			if err != nil {
				log.Printf("[detect] уведомление о чужом чате: %v", err)
			}
		}
		return
	}

	c := extractContent(m, chatID)
	if c.NormText == "" && c.MediaUID == "" && c.FwdSource == "" {
		return // service message without comparable content
	}

	now := time.Now()
	window := now.Add(-time.Duration(d.cfg.RetentionDays) * 24 * time.Hour).Unix()

	// The first occurrence of any content (including the first
	// forward from an external source) is NOT a duplicate — only
	// repeats within the window are. Forwards are stored and matched
	// like regular messages: by text, by media, or by their source.
	dups, err := d.st.FindDuplicates(chatID, window, c.NormText, c.MediaUID, c.FwdSource, c.MsgID)
	if err != nil {
		log.Printf("[detect] поиск дублей: %v", err)
	}

	// Perceptual photo match: when the exact file match found
	// nothing and photo comparison by content is enabled, download
	// the image and compare its dHash against the window.
	if len(dups) == 0 && d.cfg.PhotoMode == PhotoModePerceptual &&
		c.MediaUID != "" && len(m.Photo) > 0 {
		if ph, err := d.photoHash(m); err != nil {
			log.Printf("[photo] хеш фотографии: %v", err)
		} else {
			c.PhotoHash = ph
			if pm, err := d.st.FindPhotoDuplicate(chatID, window, ph, PhotoHashThreshold); err != nil {
				log.Printf("[photo] поиск по содержимому: %v", err)
			} else if pm != nil {
				log.Printf("[photo] фото совпало по содержимому с msg %d (hamming ≤ %d)", pm.MsgID, PhotoHashThreshold)
				dups = []StoredMessage{*pm}
			}
		}
	}

	d.store(c, now.Unix())

	if len(dups) == 0 {
		return
	}

	cat := CatDiffParticipant
	for _, dup := range dups {
		if dup.UserID == c.UserID {
			cat = CatSameParticipant
			break
		}
	}
	d.react(c, classify(c.HasURL, c.ForwardExternal), cat, dups[0], now)
}

// ProcessEdited refreshes the stored content of an edited message
// so later duplicate checks use the CURRENT text: a primary that was
// edited to different content no longer matches the old text. Edited
// messages do not themselves trigger duplicate detection.
func (d *Detector) ProcessEdited(m *tgbotapi.Message) {
	if m == nil || m.Chat == nil {
		return
	}
	chatID := m.Chat.ID
	if chatID != d.chatID {
		return
	}
	c := extractContent(m, chatID)
	if err := d.st.UpdateMessage(StoredMessage{
		ChatID:          chatID,
		MsgID:           m.MessageID,
		NormText:        c.NormText,
		HasURL:          c.HasURL,
		MediaUID:        c.MediaUID,
		ForwardExternal: c.ForwardExternal,
		FwdSource:       c.FwdSource,
	}); err != nil {
		log.Printf("[detect] обновление изменённого сообщения: %v", err)
	}
}

func (d *Detector) store(c MsgContent, ts int64) {
	if err := d.st.AddMessage(StoredMessage{
		ChatID:          c.ChatID,
		MsgID:           c.MsgID,
		UserID:          c.UserID,
		NormText:        c.NormText,
		HasURL:          c.HasURL,
		MediaUID:        c.MediaUID,
		ForwardExternal: c.ForwardExternal,
		FwdSource:       c.FwdSource,
		PhotoHash:       c.PhotoHash,
		TS:              ts,
	}); err != nil {
		log.Printf("[detect] запись сообщения: %v", err)
	}
}
