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
	if got := deletedOriginalLabel(DeletedOriginalStrict); got != "реагировать" {
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
	s := d.cfg.asSettings()

	text, kb := d.mainMenu(s)
	if text == "" || len(kb.InlineKeyboard) == 0 {
		t.Fatal("main menu empty")
	}
	if _, kb := d.reactionsMenu(DupTypeLink, s); len(kb.InlineKeyboard) == 0 {
		t.Fatal("reactions menu empty")
	}
	if _, kb := d.warningsMenu(CatSameParticipant, s); len(kb.InlineKeyboard) == 0 {
		t.Fatal("warnings menu empty")
	}
}

// Regression: the warnings navigation must open the CORRECT category
// (short keys "same"/"diff" used to map onto "diff_participant").
func TestRouteMenuWarningsCategories(t *testing.T) {
	d := &Detector{cfg: DefaultConfig()}
	s := d.cfg.asSettings()

	text, _ := d.routeMenu([]string{"m", "warn", string(CatSameParticipant)}, s)
	if !contains(text, "одного участника") {
		t.Fatalf("same menu title wrong: %q", text)
	}
	text, _ = d.routeMenu([]string{"m", "warn", string(CatDiffParticipant)}, s)
	if !contains(text, "разных участников") {
		t.Fatalf("diff menu title wrong: %q", text)
	}

	// reactions navigation
	text, _ = d.routeMenu([]string{"m", "react", "link"}, s)
	if !contains(text, "ссылки") {
		t.Fatalf("link reactions menu title wrong: %q", text)
	}
	text, _ = d.routeMenu([]string{"m", "react", "message"}, s)
	if !contains(text, "сообщения") {
		t.Fatalf("message reactions menu title wrong: %q", text)
	}

	// main menu on unknown data
	text, _ = d.routeMenu([]string{"ret", "view"}, s)
	if !contains(text, "Период слежения") {
		t.Fatalf("main menu lost: %q", text)
	}
}

// Per-chat settings: initialized from global defaults, independent
// between chats, persisted to the store.
func TestShouldReact(t *testing.T) {
	now := int64(1000000)
	s := &Settings{FreshnessMinutes: 5}

	// fresh message → react
	if !shouldReact(s, now-10, now) {
		t.Fatal("fresh message must react")
	}
	// within the freshness window (5 min = 300 s) → react
	if !shouldReact(s, now-299, now) {
		t.Fatal("message inside the window must react")
	}
	// older than the window → no reaction
	if shouldReact(s, now-301, now) {
		t.Fatal("message outside the window must not react")
	}
	// freshness=0 → react to everything
	s.FreshnessMinutes = 0
	if !shouldReact(s, now-100000, now) {
		t.Fatal("freshness 0 must force reactions")
	}
}

func TestHelpText(t *testing.T) {
	h := helpText()
	for _, key := range []string{"КАК РАБОТАЕТ", "РЕАКЦИИ", "ПРЕДУПРЕЖДЕНИЯ", "ПОЛИТИКИ", "/settings", "/status", "/help", "ПЕРВОЕ ВХОЖДЕНИЕ НИКОГДА НЕ УДАЛЯЕТСЯ"} {
		if !contains(h, key) {
			t.Fatalf("help missing section %q", key)
		}
	}
}

func TestIsBannedStatus(t *testing.T) {
	for _, s := range []string{"kicked", "restricted"} {
		if !isBannedStatus(s) {
			t.Fatalf("%q must be banned", s)
		}
	}
	for _, s := range []string{"administrator", "creator", "member", "left", ""} {
		if isBannedStatus(s) {
			t.Fatalf("%q must NOT be banned", s)
		}
	}
}

func TestFreshnessLabel(t *testing.T) {
	if got := freshnessLabel(0); got != "выкл" {
		t.Fatalf("0 = %q", got)
	}
	if got := freshnessLabel(5); got != "5 мин" {
		t.Fatalf("5 = %q", got)
	}
}

// Legacy chat settings (stored before freshness_minutes existed) must
// migrate to the default freshness instead of the zero value.
func TestSettingsFreshnessMigration(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-1001)
	if err := st.SaveChatSettings(chat, `{"retention_days":10,"react_to_old":false}`); err != nil {
		t.Fatal(err)
	}
	d := NewDetector(DefaultConfig(), st, nil, "")
	if got := d.settingsFor(chat).FreshnessMinutes; got != DefaultFreshnessMinutes {
		t.Fatalf("migrated freshness = %d, want %d", got, DefaultFreshnessMinutes)
	}
}

func TestIsAllowed(t *testing.T) {
	// empty allowlist → any chat allowed
	d := &Detector{cfg: DefaultConfig()}
	if !d.isAllowed(-1001) {
		t.Fatal("empty allowlist must allow any chat")
	}

	// non-empty allowlist → only listed chats
	cfg := DefaultConfig()
	cfg.AllowedChats = []int64{-1001, -1002}
	d = &Detector{cfg: cfg}
	if !d.isAllowed(-1001) || !d.isAllowed(-1002) {
		t.Fatal("listed chats must be allowed")
	}
	if d.isAllowed(-1003) {
		t.Fatal("unlisted chat must be denied")
	}
}

func TestIsOwner(t *testing.T) {
	cfg := DefaultConfig()
	d := &Detector{cfg: cfg}

	// unset owner → nobody is owner
	if d.isOwner(1) {
		t.Fatal("no owner configured, must not pass")
	}

	cfg.OwnerUserID = 873242843
	d = &Detector{cfg: cfg}
	if !d.isOwner(873242843) {
		t.Fatal("configured owner must pass")
	}
	if d.isOwner(1) {
		t.Fatal("other user must not be owner")
	}
}

func TestPerChatSettings(t *testing.T) {
	st := newTestStore(t)
	d := NewDetector(DefaultConfig(), st, nil, "")

	s1 := d.settingsFor(-1001)
	s2 := d.settingsFor(-1002)
	if s1 == s2 {
		t.Fatal("chats must have independent settings")
	}
	if s1.RetentionDays != 10 || s2.RetentionDays != 10 {
		t.Fatalf("defaults not applied: %d %d", s1.RetentionDays, s2.RetentionDays)
	}

	s1.RetentionDays = 3
	if s2.RetentionDays != 10 {
		t.Fatal("mutating chat1 must not affect chat2")
	}
	// the settings menu persists through persistSettings
	d.persistSettings(-1001, s1)

	// persistence: a fresh detector sees the stored value
	d2 := NewDetector(DefaultConfig(), st, nil, "")
	if got := d2.settingsFor(-1001).RetentionDays; got != 3 {
		t.Fatalf("chat1 settings not persisted: %d", got)
	}
	if got := d2.settingsFor(-1002).RetentionDays; got != 10 {
		t.Fatalf("chat2 defaults changed: %d", got)
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
