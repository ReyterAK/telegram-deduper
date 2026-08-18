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
	BotToken        string  `json:"bot_token"`
	ChatID          int64   `json:"chat_id"` // 0 = auto-lock to the first group seen
	RetentionDays   int     `json:"retention_days"`
	Reactions       Reactions `json:"reactions"`
	Warnings        Warnings  `json:"warnings"`
	AutoDeleteHours int     `json:"auto_delete_hours"` // 0 = off
	// PhotoMode: "exact" (file_unique_id), "perceptual" (dHash),
	// "off" (photos are not compared).
	PhotoMode string `json:"photo_mode"`
}

// Photo mode values.
const (
	PhotoModeExact       = "exact"
	PhotoModePerceptual  = "perceptual"
	PhotoModeOff         = "off"
)

// DefaultConfig returns the built-in defaults.
func DefaultConfig() *Config {
	return &Config{
		BotToken:      "",
		ChatID:        0,
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
	return nil
}

// LoadConfig reads configPath; a missing file creates defaults and
// writes them back. Parsing or validation failures are fatal.
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

// SaveChatID persists the auto-learned chat id.
func (c *Config) SaveChatID(path string, chatID int64) error {
	if c.ChatID == chatID {
		return nil
	}
	c.ChatID = chatID
	return writeConfig(path, c)
}
