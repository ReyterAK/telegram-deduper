package main

import "testing"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestFindDuplicatesByText(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 1, UserID: 10, NormText: "привет мир", TS: now})
	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 2, UserID: 20, NormText: "другое сообщение", TS: now})

	dups, err := st.FindDuplicates(chat, now-86400, "привет мир", "", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 1 || dups[0].MsgID != 1 || dups[0].UserID != 10 {
		t.Fatalf("expected 1 duplicate by text, got %+v", dups)
	}
}

func TestFindDuplicatesByMedia(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 1, UserID: 10, MediaUID: "uid-abc", TS: now})

	// same media → duplicate
	dups, err := st.FindDuplicates(chat, now-86400, "", "uid-abc", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 1 {
		t.Fatalf("expected media duplicate, got %d", len(dups))
	}

	// different media → no duplicate
	dups, err = st.FindDuplicates(chat, now-86400, "", "uid-xyz", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 0 {
		t.Fatalf("unexpected media duplicate: %+v", dups)
	}
}

func TestFindDuplicatesByForwardSource(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 1, UserID: 10, NormText: "пост", FwdSource: "fwd:-100999:321", TS: now})

	// same source re-forward → duplicate (even with a different caption)
	dups, err := st.FindDuplicates(chat, now-86400, "другой текст", "", "fwd:-100999:321", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 1 {
		t.Fatalf("expected forward-source duplicate, got %d", len(dups))
	}

	// different source AND different text → no duplicate
	dups, err = st.FindDuplicates(chat, now-86400, "не похожий текст", "", "fwd:-100888:111", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 0 {
		t.Fatalf("unexpected source duplicate: %+v", dups)
	}

	// plain unique text with no source → no duplicate
	dups, err = st.FindDuplicates(chat, now-86400, "уникальный текст", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 0 {
		t.Fatalf("plain text must not match: %+v", dups)
	}
}

func TestFindDuplicatesOrderOldestFirst(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 5, UserID: 10, NormText: "дубль", TS: now - 200})
	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 9, UserID: 20, NormText: "дубль", TS: now - 100})

	dups, err := st.FindDuplicates(chat, now-86400, "дубль", "", "", 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 2 {
		t.Fatalf("expected 2 duplicates, got %d", len(dups))
	}
	if dups[0].MsgID != 5 { // original = oldest
		t.Fatalf("original must be the oldest, got msg_id=%d", dups[0].MsgID)
	}
}

func TestRetentionCleanup(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 1, UserID: 10, NormText: "старое", TS: now - 20*86400})
	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 2, UserID: 10, NormText: "свежее", TS: now})
	st.AddWarning(chat, 10, now-20*86400)

	if err := st.CleanupRetention(chat, 10); err != nil {
		t.Fatal(err)
	}

	dups, _ := st.FindDuplicates(chat, now-10*86400, "старое", "", "", 99)
	if len(dups) != 0 {
		t.Fatalf("old message survived cleanup: %+v", dups)
	}
	dups, _ = st.FindDuplicates(chat, now-10*86400, "свежее", "", "", 99)
	if len(dups) != 1 {
		t.Fatalf("fresh message lost: %+v", dups)
	}
	if n, _ := st.CountWarnings(chat, 10, now-10*86400); n != 0 {
		t.Fatalf("old warning survived cleanup: %d", n)
	}
}

func TestUpdateMessageAfterEdit(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 1, UserID: 10, NormText: "старый текст", TS: now})

	dups, _ := st.FindDuplicates(chat, now-86400, "старый текст", "", "", 9)
	if len(dups) != 1 {
		t.Fatal("expected match before edit")
	}

	// edit: the stored content changes to the new text
	if err := st.UpdateMessage(StoredMessage{ChatID: chat, MsgID: 1, NormText: "новый текст"}); err != nil {
		t.Fatal(err)
	}

	dups, _ = st.FindDuplicates(chat, now-86400, "старый текст", "", "", 9)
	if len(dups) != 0 {
		t.Fatal("old text must not match after edit")
	}
	dups, _ = st.FindDuplicates(chat, now-86400, "новый текст", "", "", 9)
	if len(dups) != 1 {
		t.Fatal("new text must match after edit")
	}
}

func TestFindPhotoDuplicate(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 1, UserID: 10, PhotoHash: "ffffffffffffffff", TS: now})

	// exact same hash → duplicate
	m, minDist, err := st.FindPhotoDuplicate(chat, now-86400, MediaTypePhoto, "ffffffffffffffff", PhotoHashThreshold)
	if err != nil || m == nil || m.MsgID != 1 {
		t.Fatalf("exact hash match failed: %+v err=%v", m, err)
	}
	if minDist != 0 {
		t.Fatalf("exact match must have distance 0, got %d", minDist)
	}
	// one bit off → still a duplicate (within threshold)
	m, _, err = st.FindPhotoDuplicate(chat, now-86400, MediaTypePhoto, "fffffffeffffffff", PhotoHashThreshold)
	if err != nil || m == nil {
		t.Fatalf("near hash must match: %+v err=%v", m, err)
	}
	// all bits different → no duplicate, closest distance reported
	m, minDist, err = st.FindPhotoDuplicate(chat, now-86400, MediaTypePhoto, "0000000000000000", PhotoHashThreshold)
	if err != nil || m != nil {
		t.Fatalf("far hash must not match: %+v err=%v", m, err)
	}
	if minDist != 64 {
		t.Fatalf("closest distance = %d, want 64", minDist)
	}
}

func TestFindPhotoDuplicateTypeFilter(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	// the same hash stored as a video must NOT match a photo query
	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 1, UserID: 10,
		MediaType: MediaTypeVideo, PhotoHash: "ffffffffffffffff", TS: now})

	m, _, err := st.FindPhotoDuplicate(chat, now-86400, MediaTypePhoto, "ffffffffffffffff", PhotoHashThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if m != nil {
		t.Fatalf("video hash must not match photo query: %+v", m)
	}
	// same type does match
	m, _, err = st.FindPhotoDuplicate(chat, now-86400, MediaTypeVideo, "ffffffffffffffff", MediaThumbThreshold)
	if err != nil || m == nil {
		t.Fatalf("video hash must match video query: %+v err=%v", m, err)
	}
	// legacy rows (media_type default 'photo') still match photos
	st.AddMessage(StoredMessage{ChatID: chat, MsgID: 2, UserID: 10, PhotoHash: "eeeeeeeeeeeeeeee", TS: now})
	m, _, err = st.FindPhotoDuplicate(chat, now-86400, MediaTypePhoto, "eeeeeeeeeeeeeeee", PhotoHashThreshold)
	if err != nil || m == nil || m.MsgID != 2 {
		t.Fatalf("legacy photo row must match: %+v err=%v", m, err)
	}
}

func TestWarningsLifecycle(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	if n, _ := st.CountWarnings(chat, 42, now-86400); n != 0 {
		t.Fatal("expected 0 warnings initially")
	}
	st.AddWarning(chat, 42, now)
	if n, _ := st.CountWarnings(chat, 42, now-86400); n != 1 {
		t.Fatalf("expected 1 warning, got %d", n)
	}
	// old warning outside lifetime does not count
	if n, _ := st.CountWarnings(chat, 42, now); n != 1 {
		t.Fatalf("warning from the future counted: %d", n)
	}
	st.ResetWarnings(chat, 42)
	if n, _ := st.CountWarnings(chat, 42, now-86400); n != 0 {
		t.Fatalf("expected 0 warnings after reset, got %d", n)
	}
}

func TestBotMessagesLifecycle(t *testing.T) {
	st := newTestStore(t)
	chat := int64(-100123)
	now := nowUnix()

	st.AddBotMessage(chat, 100, now-10)
	st.AddBotMessage(chat, 101, now+3600)

	due, err := st.DueBotMessages(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].MsgID != 100 {
		t.Fatalf("expected 1 due message, got %+v", due)
	}

	st.RemoveBotMessage(chat, 100)
	due, _ = st.DueBotMessages(now)
	if len(due) != 0 {
		t.Fatalf("removed message still due: %+v", due)
	}
}
