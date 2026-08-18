package main

import "testing"

func TestFindNumericField(t *testing.T) {
	for _, id := range []string{"retention", "freshness", "autodel",
		"warn:" + string(CatSameParticipant) + ":thr",
		"warn:" + string(CatSameParticipant) + ":life",
		"warn:" + string(CatSameParticipant) + ":days",
		"warn:" + string(CatDiffParticipant) + ":thr",
		"warn:" + string(CatDiffParticipant) + ":life",
		"warn:" + string(CatDiffParticipant) + ":days"} {
		if findNumericField(id) == nil {
			t.Fatalf("field %q not found", id)
		}
	}
	if findNumericField("bogus") != nil {
		t.Fatal("bogus field must not be found")
	}
}

func TestApplyTextValue(t *testing.T) {
	s := DefaultConfig().asSettings()

	if msg, ok := applyTextValue(s, "freshness", "15"); !ok || s.FreshnessMinutes != 15 {
		t.Fatalf("freshness 15: ok=%v msg=%q", ok, msg)
	}
	if msg, ok := applyTextValue(s, "freshness", "0"); !ok || s.FreshnessMinutes != 0 {
		t.Fatalf("freshness 0: ok=%v msg=%q", ok, msg)
	}
	if _, ok := applyTextValue(s, "freshness", "abc"); ok {
		t.Fatal("non-numeric must fail")
	}
	if msg, ok := applyTextValue(s, "freshness", "999"); ok || msg == "" {
		t.Fatalf("out of range must fail with message, got ok=%v msg=%q", ok, msg)
	}
	// negative values must be rejected by the range check
	if msg, ok := applyTextValue(s, "freshness", "-5"); ok || msg == "" {
		t.Fatalf("negative must fail with message, got ok=%v msg=%q", ok, msg)
	}
	if msg, ok := applyTextValue(s, "freshness", "+5"); !ok || s.FreshnessMinutes != 5 {
		t.Fatalf("+5 must apply: ok=%v msg=%q", ok, msg)
	}

	if msg, ok := applyTextValue(s, "retention", "5"); !ok || s.RetentionDays != 5 {
		t.Fatalf("retention 5: ok=%v msg=%q", ok, msg)
	}
	if msg, ok := applyTextValue(s, "retention", "31"); ok {
		t.Fatalf("retention 31 must fail, msg=%q", msg)
	}

	// auto-delete max depends on retention (now 5 → max 120)
	if msg, ok := applyTextValue(s, "autodel", "120"); !ok || s.AutoDeleteHours != 120 {
		t.Fatalf("autodel 120: ok=%v msg=%q", ok, msg)
	}
	if _, ok := applyTextValue(s, "autodel", "121"); ok {
		t.Fatal("autodel above retention*24 must fail")
	}

	thr := "warn:" + string(CatSameParticipant) + ":thr"
	if msg, ok := applyTextValue(s, thr, "10"); !ok || s.Warnings.SameParticipant.Threshold != 10 {
		t.Fatalf("warn thr 10: ok=%v msg=%q", ok, msg)
	}
	if _, ok := applyTextValue(s, thr, "11"); ok {
		t.Fatal("warn thr 11 must fail")
	}

	days := "warn:" + string(CatDiffParticipant) + ":days"
	if msg, ok := applyTextValue(s, days, "366"); !ok || s.Warnings.DiffParticipant.BanDays != 366 {
		t.Fatalf("ban days 366: ok=%v msg=%q", ok, msg)
	}
	if _, ok := applyTextValue(s, days, "367"); ok {
		t.Fatal("ban days 367 must fail")
	}

	if _, ok := applyTextValue(s, "bogus", "5"); ok {
		t.Fatal("unknown field must fail")
	}
}

func TestMenuPartsForField(t *testing.T) {
	parts := menuPartsForField("freshness")
	if parts[0] != "m" || parts[1] != "main" {
		t.Fatalf("freshness parts = %v", parts)
	}
	parts = menuPartsForField("warn:" + string(CatSameParticipant) + ":thr")
	if parts[1] != "warn" || parts[2] != string(CatSameParticipant) {
		t.Fatalf("warn same parts = %v", parts)
	}
	parts = menuPartsForField("warn:" + string(CatDiffParticipant) + ":life")
	if parts[2] != string(CatDiffParticipant) {
		t.Fatalf("warn diff parts = %v", parts)
	}
}
