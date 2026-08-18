package main

import "testing"

func TestClamp(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 1, 30, 5},
		{0, 1, 30, 1},
		{99, 1, 30, 30},
		{-3, 0, 720, 0},
	}
	for _, c := range cases {
		if got := clamp(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clamp(%d,%d,%d)=%d, want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}

func TestLabels(t *testing.T) {
	if got := reactionLabel(ReactionIgnore); got != "Игнорировать" {
		t.Fatalf("ignore = %q", got)
	}
	if got := reactionLabel(ReactionDelete); got != "Удалять" {
		t.Fatalf("delete = %q", got)
	}
	if got := autoDeleteLabel(0); got != "выкл" {
		t.Fatalf("off = %q", got)
	}
	if got := autoDeleteLabel(1); got != "1 ч" {
		t.Fatalf("1h = %q", got)
	}
	if got := thresholdLabel(0); got != "выкл" {
		t.Fatalf("thr off = %q", got)
	}
	if got := thresholdLabel(3); got != "3" {
		t.Fatalf("thr 3 = %q", got)
	}
	if got := banTypeLabelShort("kick"); got != "удаление из чата" {
		t.Fatalf("kick = %q", got)
	}
	if got := banTypeLabelShort("readonly"); got != "только чтение" {
		t.Fatalf("readonly = %q", got)
	}
	if got := deletedOriginalLabel(DeletedOriginalAllow); got != "пропускать" {
		t.Fatalf("allow = %q", got)
	}
	if got := deletedOriginalLabel(DeletedOriginalStrict); got != "строгая" {
		t.Fatalf("strict = %q", got)
	}
}

func TestSetReaction(t *testing.T) {
	rs := ReactionSettings{SameParticipant: ReactionDelete, DiffParticipant: ReactionDelete}
	setReaction(&rs, CatSameParticipant, ReactionComment)
	if rs.SameParticipant != ReactionComment || rs.DiffParticipant != ReactionDelete {
		t.Fatalf("same reaction not set: %+v", rs)
	}
	setReaction(&rs, CatDiffParticipant, ReactionIgnore)
	if rs.DiffParticipant != ReactionIgnore {
		t.Fatalf("diff reaction not set: %+v", rs)
	}
}

func TestApplyWarningChange(t *testing.T) {
	ws := WarningSettings{Threshold: 0, LifetimeDays: 10, BanType: "readonly", BanDays: 1}

	if !applyWarningChange(&ws, "thr", "+1", 10) || ws.Threshold != 1 {
		t.Fatalf("thr +1: %+v", ws)
	}
	if !applyWarningChange(&ws, "thr", "off", 10) || ws.Threshold != 0 {
		t.Fatalf("thr off: %+v", ws)
	}
	if !applyWarningChange(&ws, "life", "+1", 10) || ws.LifetimeDays != 10 {
		t.Fatalf("life must clamp to retention: %+v", ws)
	}
	if !applyWarningChange(&ws, "type", "kick", 10) || ws.BanType != "kick" {
		t.Fatalf("type kick: %+v", ws)
	}
	if applyWarningChange(&ws, "type", "nonsense", 10) {
		t.Fatal("invalid ban type must not apply")
	}
	if !applyWarningChange(&ws, "days", "+1", 10) || ws.BanDays != 2 {
		t.Fatalf("days +1: %+v", ws)
	}
	if applyWarningChange(&ws, "bogus", "x", 10) {
		t.Fatal("unknown field must not apply")
	}
}

// Menu rendering must not panic and must carry the config values.
func TestMenusRender(t *testing.T) {
	d := &Detector{cfg: DefaultConfig()}

	text, kb := d.mainMenu()
	if text == "" || len(kb.InlineKeyboard) == 0 {
		t.Fatal("main menu empty")
	}
	if _, kb := d.reactionsMenu(DupTypeLink); len(kb.InlineKeyboard) == 0 {
		t.Fatal("reactions menu empty")
	}
	if _, kb := d.warningsMenu(CatSameParticipant); len(kb.InlineKeyboard) == 0 {
		t.Fatal("warnings menu empty")
	}
}

// Regression: the warnings navigation must open the CORRECT category
// (short keys "same"/"diff" used to map onto "diff_participant").
func TestRouteMenuWarningsCategories(t *testing.T) {
	d := &Detector{cfg: DefaultConfig()}

	text, _ := d.routeMenu([]string{"m", "warn", string(CatSameParticipant)})
	if !contains(text, "одного участника") {
		t.Fatalf("same menu title wrong: %q", text)
	}
	text, _ = d.routeMenu([]string{"m", "warn", string(CatDiffParticipant)})
	if !contains(text, "разных участников") {
		t.Fatalf("diff menu title wrong: %q", text)
	}

	// reactions navigation
	text, _ = d.routeMenu([]string{"m", "react", "link"})
	if !contains(text, "ссылки") {
		t.Fatalf("link reactions menu title wrong: %q", text)
	}
	text, _ = d.routeMenu([]string{"m", "react", "message"})
	if !contains(text, "сообщения") {
		t.Fatalf("message reactions menu title wrong: %q", text)
	}

	// main menu on unknown data
	text, _ = d.routeMenu([]string{"ret", "view"})
	if !contains(text, "Период слежения") {
		t.Fatalf("main menu lost: %q", text)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
