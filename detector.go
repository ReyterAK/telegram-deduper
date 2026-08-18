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
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Media type constants (which part of a message is perceptually
// hashed).
const (
	MediaTypePhoto    = "photo"
	MediaTypeVideo    = "video"
	MediaTypeDocument = "document"
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
	MediaType       string
	ForwardExternal bool
	// FwdSource identifies the origin of an external forward
	// ("fwd:<chat_id>:<msg_id>", "" when unknown/not a forward).
	FwdSource string
	// SourceLink points at the original post for external forwards
	// ("" when the source is unknown).
	SourceLink string
	// PhotoHash is the perceptual dHash of the media (photo, or the
	// thumbnail of a video/document; "" when not computed).
	PhotoHash string
	// ContentSHA is the SHA-256 of a downloaded document's bytes
	// ("" when not computed). Catches renamed identical files that
	// file_unique_id (per-upload for documents) cannot.
	ContentSHA string
}

// Detector wires store + telegram into the duplicate pipeline.
// Multi-chat: settings are resolved per chat (initialized from the
// global config), and the bot engages only chats where it is an
// administrator. All update handling runs in the single poller
// goroutine, so the caches need no locking.
type Detector struct {
	cfg        *Config
	st         *Store
	bot        *tgbotapi.BotAPI
	configPath string

	settings  map[int64]*Settings
	chatName  map[int64]string
	chatAdmin map[int64]bool
	pending   map[int64]pendingInput
	// foreignNotified remembers chats that were reported to the owner.
	foreignNotified map[int64]bool
	// chatTitleCache caches chat titles for the /chats listing.
	chatTitleCache map[int64]string
}

// pendingInput remembers a text-value request until the user replies.
type pendingInput struct {
	userID    int64
	field     string
	menuMsgID int
	at        time.Time
}

func NewDetector(cfg *Config, st *Store, bot *tgbotapi.BotAPI, configPath string) *Detector {
	return &Detector{
		cfg:        cfg,
		st:         st,
		bot:        bot,
		configPath: configPath,
		settings:        map[int64]*Settings{},
		chatName:        map[int64]string{},
		chatAdmin:       map[int64]bool{},
		pending:         map[int64]pendingInput{},
		foreignNotified: map[int64]bool{},
		chatTitleCache:  map[int64]string{},
	}
}

// isAllowed reports whether the bot may serve this chat (allowlist).
// An empty allowlist permits every chat where the bot is an admin.
func (d *Detector) isAllowed(chatID int64) bool {
	if len(d.cfg.AllowedChats) == 0 {
		return true
	}
	for _, id := range d.cfg.AllowedChats {
		if id == chatID {
			return true
		}
	}
	return false
}

// notifyForeign tells the owner once per foreign chat that the bot
// was added there but is not enabled. Notification goes to the
// owner's private chat with the bot (or the first allowed chat when
// no owner is configured).
func (d *Detector) notifyForeign(chatID int64, title string) {
	if d.foreignNotified[chatID] {
		return
	}
	d.foreignNotified[chatID] = true
	name := title
	if name == "" {
		name = strconv.FormatInt(chatID, 10)
	}
	log.Printf("[chat] бота добавили в чат %q (%d) — не в списке разрешённых, игнорируется", name, chatID)

	var target int64
	if d.cfg.OwnerUserID != 0 {
		target = d.cfg.OwnerUserID
	} else if len(d.cfg.AllowedChats) > 0 {
		target = d.cfg.AllowedChats[0]
	}
	if target == 0 {
		return
	}
	_, err := d.bot.Send(tgbotapi.NewMessage(target,
		fmt.Sprintf("Бота добавили в чат «%s» (id %d).\nРазрешить: /allow %d", name, chatID, chatID)))
	if err != nil {
		log.Printf("[chat] уведомление владельцу: %v", err)
	}
}

// settingsFor returns the effective settings of a chat, initializing
// them from the global config on first contact and persisting them.
func (d *Detector) settingsFor(chatID int64) *Settings {
	if s, ok := d.settings[chatID]; ok {
		return s
	}
	s := d.cfg.asSettings()
	if raw, err := d.st.GetChatSettings(chatID); err == nil && raw != "" {
		if err := json.Unmarshal([]byte(raw), s); err != nil {
			log.Printf("[chat] чтение настроек чата %d: %v", chatID, err)
		}
		// migration: settings added after this chat was stored
		if !hasJSONKey([]byte(raw), "freshness_minutes") {
			s.FreshnessMinutes = DefaultFreshnessMinutes
		}
		if !hasJSONKey([]byte(raw), "video_mode") {
			s.VideoMode = PhotoModePerceptual
		}
		if !hasJSONKey([]byte(raw), "doc_mode") {
			s.DocMode = PhotoModePerceptual
		}
	} else if err == nil {
		// first contact — persist the defaults
		d.persistSettings(chatID, s)
	}
	d.settings[chatID] = s
	return s
}

func (d *Detector) persistSettings(chatID int64, s *Settings) {
	raw, err := json.Marshal(s)
	if err != nil {
		log.Printf("[chat] сериализация настроек: %v", err)
		return
	}
	if err := d.st.SaveChatSettings(chatID, string(raw)); err != nil {
		log.Printf("[chat] сохранение настроек чата %d: %v", chatID, err)
	}
}

// botAdmin reports whether the bot itself is an administrator (or
// creator) of the chat — required for deletions and bans.
func (d *Detector) botAdmin(chatID int64) bool {
	if v, ok := d.chatAdmin[chatID]; ok {
		return v
	}
	member, err := d.bot.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{ChatID: chatID, UserID: d.bot.Self.ID},
	})
	ok := err == nil && (member.IsAdministrator() || member.IsCreator())
	d.chatAdmin[chatID] = ok
	if !ok {
		log.Printf("[chat] бот не админ чата %d, сообщения игнорируются", chatID)
	}
	return ok
}

// chatNameFor returns the cached username of a chat (for message links).
func (d *Detector) chatNameFor(chatID int64) string {
	if n, ok := d.chatName[chatID]; ok {
		return n
	}
	name := ""
	if chat, err := d.bot.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatID}}); err == nil {
		name = chat.UserName
	} else {
		log.Printf("[chat] имя чата %d: %v", chatID, err)
	}
	d.chatName[chatID] = name
	return name
}

// extractContent builds the comparable content of a message.
func extractContent(m *tgbotapi.Message, chatID int64) MsgContent {
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	norm := NormalizeText(text)
	mediaUID := mediaUniqueID(m)
	mediaType := ""
	switch {
	case len(m.Photo) > 0:
		mediaType = MediaTypePhoto
	case m.Video != nil:
		mediaType = MediaTypeVideo
	case m.Document != nil:
		mediaType = MediaTypeDocument
	}
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
		MediaType:       mediaType,
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

// shouldReact reports whether a message deserves a reaction, based
// on its send time and the freshness threshold. freshness_minutes=0
// means react to everything.
func shouldReact(s *Settings, msgDate, now int64) bool {
	if s.FreshnessMinutes <= 0 {
		return true
	}
	return now-msgDate <= int64(s.FreshnessMinutes)*60
}

// MediaMode returns the comparison mode configured for a media
// type (photo/video/document); unknown types fall back to photo.
func (s *Settings) MediaMode(mediaType string) string {
	switch mediaType {
	case MediaTypeVideo:
		return s.VideoMode
	case MediaTypeDocument:
		return s.DocMode
	default:
		return s.PhotoMode
	}
}

// mediaThreshold returns the Hamming threshold for a media type:
// photos use the looser PhotoHashThreshold, thumbnails (video and
// documents) the stricter MediaThumbThreshold — two different
// videos/docs often share similar first frames, so they need a
// tighter bound to avoid false positives.
func mediaThreshold(mediaType string) int {
	switch mediaType {
	case MediaTypeVideo, MediaTypeDocument:
		return MediaThumbThreshold
	default:
		return PhotoHashThreshold
	}
}

// Process handles one incoming message: store, detect, react.
func (d *Detector) Process(m *tgbotapi.Message) {
	// Only group/supergroup messages from identifiable users.
	if m.From == nil {
		return
	}
	if m.From.IsBot {
		return // other bots' messages are not moderated
	}
	if m.From.ID == d.bot.Self.ID {
		return
	}
	if !m.Chat.IsGroup() && !m.Chat.IsSuperGroup() {
		return
	}

	chatID := m.Chat.ID

	// Serve only allowed chats (empty allowlist = any admin chat).
	if !d.isAllowed(chatID) {
		d.notifyForeign(chatID, m.Chat.Title)
		return
	}

	// Engage only chats where the bot is an administrator.
	if !d.botAdmin(chatID) {
		return
	}
	s := d.settingsFor(chatID)

	c := extractContent(m, chatID)
	if c.NormText == "" && c.MediaUID == "" && c.FwdSource == "" {
		return // service message without comparable content
	}

	now := time.Now()
	ts := int64(m.Date) // the message's real send time
	if ts <= 0 {
		ts = now.Unix()
	}

	// Backlog after downtime (bot offline, Telegram delivered the
	// missed updates late): remember the content with its real time,
	// but do not react — no retroactive deletions/notices/warnings.
	if !shouldReact(s, ts, now.Unix()) {
		if s.MediaMode(c.MediaType) == PhotoModePerceptual && c.MediaUID != "" {
			if ph, err := d.mediaHash(m, c.MediaType); err == nil {
				c.PhotoHash = ph
			}
			if c.MediaType == MediaTypeDocument {
				if sha, err := d.contentSHA(m); err == nil {
					c.ContentSHA = sha
				}
			}
		}
		d.store(c, ts)
		log.Printf("[detect] сообщение %d от %s старше порога свежести — только запомнено", c.MsgID, time.Unix(ts, 0).Format("15:04:05"))
		return
	}

	window := now.Add(-time.Duration(s.RetentionDays) * 24 * time.Hour).Unix()

	// The first occurrence of any content (including the first
	// forward from an external source) is NOT a duplicate — only
	// repeats within the window are. Forwards are stored and matched
	// like regular messages: by text, by media, or by their source.
	dups, err := d.st.FindDuplicates(chatID, window, c.NormText, c.MediaUID, c.FwdSource, c.MsgID)
	if err != nil {
		log.Printf("[detect] поиск дублей: %v", err)
	}

	// Perceptual content match: when the exact file match found
	// nothing and content comparison is enabled for this media type
	// (photo/video/document), download the image (a photo, or the
	// thumbnail of a video/document) and compare its dHash against
	// the window of the same media type. Documents additionally
	// compare a SHA-256 of the downloaded FILE — file_unique_id is
	// per-upload for documents, so renamed identical copies need
	// byte-level matching.
	if len(dups) == 0 && s.MediaMode(c.MediaType) == PhotoModePerceptual &&
		c.MediaUID != "" {
		threshold := mediaThreshold(c.MediaType)
		if ph, err := d.mediaHash(m, c.MediaType); err != nil {
			log.Printf("[media] хеш %s: %v", c.MediaType, err)
		} else {
			c.PhotoHash = ph
			if pm, minDist, err := d.st.FindPhotoDuplicate(chatID, window, c.MediaType, ph, threshold); err != nil {
				log.Printf("[media] поиск по содержимому: %v", err)
			} else if pm != nil {
				log.Printf("[media] %s совпал по содержимому с msg %d (hamming %d ≤ %d)", c.MediaType, pm.MsgID, minDist, threshold)
				dups = []StoredMessage{*pm}
			} else {
				log.Printf("[media] совпадений нет, ближайший хэш на расстоянии %d (порог %d)", minDist, threshold)
			}
		}
		if len(dups) == 0 && c.MediaType == MediaTypeDocument {
			if sha, err := d.contentSHA(m); err != nil {
				log.Printf("[media] sha документа: %v", err)
			} else {
				c.ContentSHA = sha
				if pm, err := d.st.FindContentDuplicate(chatID, window, sha); err != nil {
					log.Printf("[media] поиск по содержимому файла: %v", err)
				} else if pm != nil {
					log.Printf("[media] документ совпал по содержимому файла с msg %d", pm.MsgID)
					dups = []StoredMessage{*pm}
				}
			}
		}
	}

	d.store(c, ts)

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
	d.react(c, classify(c.HasURL, c.ForwardExternal), cat, dups[0], now, s)
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
	if !d.isAllowed(chatID) {
		return
	}
	if !d.botAdmin(chatID) {
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
		MediaType:       c.MediaType,
		ForwardExternal: c.ForwardExternal,
		FwdSource:       c.FwdSource,
		PhotoHash:       c.PhotoHash,
		ContentSHA:      c.ContentSHA,
		TS:              ts,
	}); err != nil {
		log.Printf("[detect] запись сообщения: %v", err)
	}
}
