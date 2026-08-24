package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RetentionDays != 10 {
		t.Fatalf("retention = %d, want 10", cfg.RetentionDays)
	}
	if cfg.Reactions.Link.SameParticipant != ReactionDelete ||
		cfg.Reactions.Link.DiffParticipant != ReactionDelete ||
		cfg.Reactions.Message.SameParticipant != ReactionDelete ||
		cfg.Reactions.Message.DiffParticipant != ReactionIgnore {
		t.Fatalf("unexpected default reactions: %+v", cfg.Reactions)
	}
	if cfg.Warnings.SameParticipant.Threshold != 0 {
		t.Fatalf("warnings must be off by default: %+v", cfg.Warnings)
	}
	if cfg.AutoDeleteHours != 0 {
		t.Fatalf("auto-delete must be off by default")
	}
	if cfg.PhotoMode != PhotoModePerceptual ||
		cfg.VideoMode != PhotoModePerceptual ||
		cfg.DocMode != PhotoModePerceptual {
		t.Fatalf("content comparison must default to perceptual: %+v",
			[]string{cfg.PhotoMode, cfg.VideoMode, cfg.DocMode})
	}
	if cfg.ForwardMatching != ForwardMatchingAll {
		t.Fatalf("forward matching must default to all: %q", cfg.ForwardMatching)
	}
}

func TestConfigValidationClamps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RetentionDays = 99 // > 30
	cfg.Warnings.SameParticipant.LifetimeDays = 99
	cfg.Warnings.DiffParticipant.BanDays = 9999
	cfg.AutoDeleteHours = -5
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.RetentionDays != 10 {
		t.Errorf("retention not clamped: %d", cfg.RetentionDays)
	}
	if cfg.Warnings.SameParticipant.LifetimeDays != 10 {
		t.Errorf("lifetime not clamped: %d", cfg.Warnings.SameParticipant.LifetimeDays)
	}
	if cfg.Warnings.DiffParticipant.BanDays != 1 {
		t.Errorf("ban_days not clamped: %d", cfg.Warnings.DiffParticipant.BanDays)
	}
	if cfg.AutoDeleteHours != 0 {
		t.Errorf("auto_delete not clamped: %d", cfg.AutoDeleteHours)
	}
	cfg.VideoMode = "bogus"
	cfg.DocMode = "weird"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.VideoMode != PhotoModePerceptual || cfg.DocMode != PhotoModePerceptual {
		t.Errorf("unknown media modes must be reset to perceptual: %q %q", cfg.VideoMode, cfg.DocMode)
	}
	cfg.ForwardMatching = "bogus"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.ForwardMatching != ForwardMatchingAll {
		t.Errorf("unknown forward_matching must reset to all, got %q", cfg.ForwardMatching)
	}
}

func TestConfigValidationErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Reactions.Link.SameParticipant = Reaction("ban")
	if err := cfg.validate(); err == nil {
		t.Error("invalid reaction must fail validation")
	}
	cfg = DefaultConfig()
	cfg.Warnings.SameParticipant.BanType = "whatever"
	if err := cfg.validate(); err == nil {
		t.Error("invalid ban_type must fail validation")
	}
	cfg = DefaultConfig()
	if cfg.PhotoMode != PhotoModePerceptual {
		t.Fatalf("photo mode default = %q", cfg.PhotoMode)
	}
	cfg.PhotoMode = "bogus"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.PhotoMode != PhotoModePerceptual {
		t.Fatalf("invalid photo mode must fall back to perceptual, got %q", cfg.PhotoMode)
	}
	cfg = DefaultConfig()
	if cfg.DeletedOriginalPolicy != DeletedOriginalStrict {
		t.Fatalf("deleted-original policy default = %q", cfg.DeletedOriginalPolicy)
	}
	cfg.DeletedOriginalPolicy = "bogus"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.DeletedOriginalPolicy != DeletedOriginalStrict {
		t.Fatalf("invalid policy must fall back to strict, got %q", cfg.DeletedOriginalPolicy)
	}
	cfg = DefaultConfig()
	if cfg.FreshnessMinutes != DefaultFreshnessMinutes {
		t.Fatalf("freshness default = %d", cfg.FreshnessMinutes)
	}
	cfg.FreshnessMinutes = 999
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.FreshnessMinutes != DefaultFreshnessMinutes {
		t.Fatalf("invalid freshness must clamp to default, got %d", cfg.FreshnessMinutes)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// missing file → defaults written
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(missing): %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default config not written: %v", err)
	}

	// re-read matches
	cfg2, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(existing): %v", err)
	}
	if cfg2.RetentionDays != cfg.RetentionDays {
		t.Errorf("round-trip mismatch")
	}
}
