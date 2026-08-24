//
// config.go
// Antidubl — Telegram anti-duplicate bot
//
// Settings model with defaults, JSON file storage
// and bound validation.
//

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const (
	MinRetentionDays = 1
	MaxRetentionDays = 30
	MaxBanDays       = 366

	DefaultRetentionDays = 10
)

// Reaction is the bot action on a detected duplicate.
type Reaction string

const (
	ReactionIgnore  Reaction = "ignore"
	ReactionComment Reaction = "comment"
	ReactionDelete  Reaction = "delete"
)

// DupType discriminates duplicate kinds.
type DupType string

const (
	DupTypeLink    DupType = "link"
	DupTypeMessage DupType = "message"
)

// DupCategory discriminates same-author vs cross-author duplicates.
type DupCategory string

const (
	CatSameParticipant DupCategory = "same_participant"
	CatDiffParticipant DupCategory = "diff_participant"
)

type ReactionSettings struct {
	SameParticipant Reaction `json:"same_participant"`
	DiffParticipant Reaction `json:"diff_participant"`
}

type Reactions struct {
	Link    ReactionSettings `json:"link"`
	Message ReactionSettings `json:"message"`
}

type WarningSettings struct {
	Threshold    int    `json:"threshold"`     // 0 = off, 1..10
	LifetimeDays int    `json:"lifetime_days"` // 1..retention_days
	BanType      string `json:"ban_type"`      // "readonly" | "kick"
	BanDays      int    `json:"ban_days"`      // 1..366
}

type Warnings struct {
	SameParticipant WarningSettings `json:"same_participant"`
	DiffParticipant WarningSettings `json:"diff_participant"`
}

type Config struct {
	BotToken        string    `json:"bot_token"`
	RetentionDays   int       `json:"retention_days"`
	Reactions       Reactions `json:"reactions"`
	Warnings        Warnings  `json:"warnings"`
	AutoDeleteHours int       `json:"auto_delete_hours"` // 0 = off
	// PhotoMode: "exact" (file_unique_id), "perceptual" (dHash),
	// "off" (photos are not compared).
	PhotoMode string `json:"photo_mode"`
	// VideoMode / DocMode: same enum, applied to video thumbnails
	// and document thumbnails respectively.
	VideoMode string `json:"video_mode"`
	DocMode   string `json:"doc_mode"`
	// ForwardMatching: how forwarded messages are matched
	// ("all" | "source_only" | "ignore").
	ForwardMatching string `json:"forward_matching"`
	// DeletedOriginalPolicy: what happens when a duplicate is found
	// but the original message is gone from the chat.
	// "allow"  — the repeat is allowed (becomes the new original).
	// "strict" — flagged content stays flagged for the window: the
	// repeat is deleted anyway.
	DeletedOriginalPolicy string `json:"deleted_original_policy"`
	// FreshnessMinutes: messages older than this (by send time) are
	// remembered but not reacted to (backlog after downtime).
	// 0 = react to everything.
	FreshnessMinutes int `json:"freshness_minutes"`
	// AllowedChats: chat ids the bot may serve. Empty = any chat
	// where the bot is an administrator. When non-empty, other chats
	// are ignored and the owner is notified.
	AllowedChats []int64 `json:"allowed_chats"`
	// OwnerUserID: Telegram user id of the owner. The owner manages
	// the allowlist via /chats, /allow and /deny (private chat with
	// the bot or any allowed chat); foreign-add notifications are
	// sent to this user. 0 = owner commands disabled.
	OwnerUserID int64 `json:"owner_user_id"`
}

// Freshness bounds.
const (
	DefaultFreshnessMinutes = 5
	MaxFreshnessMinutes     = 60
)

// Settings is the tunable subset of the config, stored per chat
// (chat_settings table). New chats are initialized from the global
// config values.
type Settings struct {
	RetentionDays         int       `json:"retention_days"`
	Reactions             Reactions `json:"reactions"`
	Warnings              Warnings  `json:"warnings"`
	AutoDeleteHours       int       `json:"auto_delete_hours"`
	PhotoMode             string    `json:"photo_mode"`
	VideoMode             string    `json:"video_mode"`
	DocMode               string    `json:"doc_mode"`
	ForwardMatching       string    `json:"forward_matching"`
	DeletedOriginalPolicy string    `json:"deleted_original_policy"`
	FreshnessMinutes      int       `json:"freshness_minutes"`
}

// asSettings returns the tunable subset of the global config —
// the defaults for newly seen chats.
func (c *Config) asSettings() *Settings {
	return &Settings{
		RetentionDays:         c.RetentionDays,
		Reactions:             c.Reactions,
		Warnings:              c.Warnings,
		AutoDeleteHours:       c.AutoDeleteHours,
		PhotoMode:             c.PhotoMode,
		VideoMode:             c.VideoMode,
		DocMode:               c.DocMode,
		ForwardMatching:       c.ForwardMatching,
		DeletedOriginalPolicy: c.DeletedOriginalPolicy,
		FreshnessMinutes:      c.FreshnessMinutes,
	}
}

// Photo mode values.
const (
	PhotoModeExact      = "exact"
	PhotoModePerceptual = "perceptual"
	PhotoModeOff        = "off"
)

// Deleted-original policy values.
const (
	DeletedOriginalAllow  = "allow"
	DeletedOriginalStrict = "strict"
)

// Forward-matching policy values: how forwarded messages are matched
// against the window. Each element of a multi-media forward is a
// separate message, so without a policy a single coinciding media
// among several can mark a forward from a DIFFERENT channel as a
// duplicate.
const (
	// ForwardMatchingAll — forwards are matched like any message:
	// by text, by media and by forward source (historical default).
	ForwardMatchingAll = "all"
	// ForwardMatchingSourceOnly — a forward is a duplicate only
	// when the SAME original post is re-forwarded (same forward
	// source). Coincidences of individual media/text with other
	// content are ignored.
	ForwardMatchingSourceOnly = "source_only"
	// ForwardMatchingIgnore — forwards are stored but never
	// considered duplicates.
	ForwardMatchingIgnore = "ignore"
)

// DefaultConfig returns the built-in defaults.
func DefaultConfig() *Config {
	return &Config{
		BotToken:      "",
		RetentionDays: DefaultRetentionDays,
		Reactions: Reactions{
			Link: ReactionSettings{
				SameParticipant: ReactionDelete,
				DiffParticipant: ReactionDelete,
			},
			Message: ReactionSettings{
				SameParticipant: ReactionDelete,
				// Text duplicates from DIFFERENT users are normal
				// conversation ("Привет" twice) — not moderated.
				DiffParticipant: ReactionIgnore,
			},
		},
		Warnings: Warnings{
			SameParticipant: WarningSettings{
				Threshold:    0, // off
				LifetimeDays: DefaultRetentionDays,
				BanType:      "readonly",
				BanDays:      1,
			},
			DiffParticipant: WarningSettings{
				Threshold:    0, // off
				LifetimeDays: DefaultRetentionDays,
				BanType:      "readonly",
				BanDays:      1,
			},
		},
		AutoDeleteHours: 0,
		PhotoMode:       PhotoModePerceptual,
		VideoMode:       PhotoModePerceptual,
		DocMode:         PhotoModePerceptual,
		ForwardMatching: ForwardMatchingAll,
		// Strict by default: once-flagged content stays flagged.
		DeletedOriginalPolicy: DeletedOriginalStrict,
		FreshnessMinutes:      DefaultFreshnessMinutes,
	}
}

// validReaction reports whether r is a known reaction.
func validReaction(r Reaction) bool {
	switch r {
	case ReactionIgnore, ReactionComment, ReactionDelete:
		return true
	}
	return false
}

// validate clamps out-of-range values to the allowed bounds and
// logs what was fixed. Returns an error only for values that
// cannot be fixed safely (unknown ban type, bad reaction).
func (c *Config) validate() error {
	if c.RetentionDays < MinRetentionDays || c.RetentionDays > MaxRetentionDays {
		log.Printf("[config] retention_days=%d вне диапазона, установлено %d",
			c.RetentionDays, DefaultRetentionDays)
		c.RetentionDays = DefaultRetentionDays
	}

	fixReactions := func(name string, rs *ReactionSettings) error {
		for _, cat := range []struct {
			label string
			r     *Reaction
		}{
			{"same_participant", &rs.SameParticipant},
			{"diff_participant", &rs.DiffParticipant},
		} {
			if !validReaction(*cat.r) {
				return fmt.Errorf("reactions.%s.%s: неизвестная реакция %q", name, cat.label, *cat.r)
			}
		}
		return nil
	}
	if err := fixReactions("link", &c.Reactions.Link); err != nil {
		return err
	}
	if err := fixReactions("message", &c.Reactions.Message); err != nil {
		return err
	}

	fixWarnings := func(name string, ws *WarningSettings) error {
		if ws.Threshold < 0 || ws.Threshold > 10 {
			log.Printf("[config] warnings.%s.threshold=%d вне диапазона, выключено", name, ws.Threshold)
			ws.Threshold = 0
		}
		if ws.LifetimeDays < 1 || ws.LifetimeDays > c.RetentionDays {
			log.Printf("[config] warnings.%s.lifetime_days=%d вне 1..%d, установлено %d",
				name, ws.LifetimeDays, c.RetentionDays, DefaultRetentionDays)
			ws.LifetimeDays = DefaultRetentionDays
		}
		if ws.BanType != "readonly" && ws.BanType != "kick" {
			return fmt.Errorf("warnings.%s.ban_type: неизвестный тип %q", name, ws.BanType)
		}
		if ws.BanDays < 1 || ws.BanDays > MaxBanDays {
			log.Printf("[config] warnings.%s.ban_days=%d вне 1..%d, установлено 1", name, ws.BanDays, MaxBanDays)
			ws.BanDays = 1
		}
		return nil
	}
	if err := fixWarnings("same_participant", &c.Warnings.SameParticipant); err != nil {
		return err
	}
	if err := fixWarnings("diff_participant", &c.Warnings.DiffParticipant); err != nil {
		return err
	}

	maxAuto := c.RetentionDays * 24
	if c.AutoDeleteHours < 0 || c.AutoDeleteHours > maxAuto {
		log.Printf("[config] auto_delete_hours=%d вне 0..%d, выключено", c.AutoDeleteHours, maxAuto)
		c.AutoDeleteHours = 0
	}
	switch c.PhotoMode {
	case PhotoModeExact, PhotoModePerceptual, PhotoModeOff:
	default:
		log.Printf("[config] photo_mode=%q неизвестен, установлено %q", c.PhotoMode, PhotoModePerceptual)
		c.PhotoMode = PhotoModePerceptual
	}
	switch c.VideoMode {
	case PhotoModeExact, PhotoModePerceptual, PhotoModeOff:
	default:
		log.Printf("[config] video_mode=%q неизвестен, установлено %q", c.VideoMode, PhotoModePerceptual)
		c.VideoMode = PhotoModePerceptual
	}
	switch c.DocMode {
	case PhotoModeExact, PhotoModePerceptual, PhotoModeOff:
	default:
		log.Printf("[config] doc_mode=%q неизвестен, установлено %q", c.DocMode, PhotoModePerceptual)
		c.DocMode = PhotoModePerceptual
	}
	switch c.DeletedOriginalPolicy {
	case DeletedOriginalAllow, DeletedOriginalStrict:
	default:
		log.Printf("[config] deleted_original_policy=%q неизвестна, установлено %q", c.DeletedOriginalPolicy, DeletedOriginalStrict)
		c.DeletedOriginalPolicy = DeletedOriginalStrict
	}
	switch c.ForwardMatching {
	case ForwardMatchingAll, ForwardMatchingSourceOnly, ForwardMatchingIgnore:
	default:
		log.Printf("[config] forward_matching=%q неизвестен, установлено %q", c.ForwardMatching, ForwardMatchingAll)
		c.ForwardMatching = ForwardMatchingAll
	}
	if c.FreshnessMinutes < 0 || c.FreshnessMinutes > MaxFreshnessMinutes {
		log.Printf("[config] freshness_minutes=%d вне 0..%d, установлено %d", c.FreshnessMinutes, MaxFreshnessMinutes, DefaultFreshnessMinutes)
		c.FreshnessMinutes = DefaultFreshnessMinutes
	}
	return nil
}

// LoadConfig reads configPath; a missing file creates defaults and
// writes them back. Parsing or validation failures are fatal.
// hasJSONKey reports whether a JSON object contains the key
// (used for schema migrations when adding new settings).
func hasJSONKey(data []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func LoadConfig(configPath string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		log.Printf("[config] %s не найден, создаю с настройками по умолчанию", configPath)
		if werr := writeConfig(configPath, cfg); werr != nil {
			return nil, fmt.Errorf("write default config: %w", werr)
		}
		return cfg, nil
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", configPath, err)
	}
	// migration: settings added after this config was written
	if !hasJSONKey(data, "freshness_minutes") {
		cfg.FreshnessMinutes = DefaultFreshnessMinutes
	}
	if !hasJSONKey(data, "video_mode") {
		cfg.VideoMode = PhotoModePerceptual
	}
	if !hasJSONKey(data, "doc_mode") {
		cfg.DocMode = PhotoModePerceptual
	}
	if !hasJSONKey(data, "forward_matching") {
		cfg.ForwardMatching = ForwardMatchingAll
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func writeConfig(configPath string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0o644)
}
