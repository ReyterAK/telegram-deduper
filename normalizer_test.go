package main

import "testing"

func TestNormalizeTextBasic(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Привет Мир", "привет мир"},
		{"ЁЛКА и ёжик", "елка и ежик"},
		{"Много   пробелов\nи табуляций\tтут", "много пробелов и табуляций тут"},
		{"", ""},
		{"   ", ""},
		// trailing punctuation is stripped (cheap duplicate bypass)
		{"Текст с  разными   пробелами.", "текст с разными пробелами"},
		{"Привет!!!", "привет"},
		{"куда поедешь)", "куда поедешь"},
		{"ну и что...", "ну и что"},
		{"Ахаха))", "ахаха"},
		{"в скобках)", "в скобках"},
		{"Только знаки...", "только знаки"},
		{"...", ""},
		{"?!?!", ""},
	}
	for _, c := range cases {
		if got := NormalizeText(c.in); got != c.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeTextURLs(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			"Смотри https://example.com/news",
			"смотри https://example.com/news",
		},
		{
			"https://example.com/news?utm_source=x&utm_medium=y",
			"https://example.com/news",
		},
		{
			"https://example.com/news?utm_source=x&id=42",
			"https://example.com/news?id=42",
		},
		{
			"https://t.me/SomeChannel/123?start=abc",
			"https://t.me/somechannel/123",
		},
		{
			"https://example.com/Page?utm_campaign=spam&utm_term=a",
			"https://example.com/page",
		},
		{
			"Точка в конце: https://example.com/x.",
			"точка в конце: https://example.com/x",
		},
		{
			"РЕГИСТР https://EXAMPLE.com/NEWS?UTM_SOURCE=1",
			"регистр https://example.com/news",
		},
	}
	for _, c := range cases {
		if got := NormalizeText(c.in); got != c.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Same URL with different tracking must be equal; different
// surrounding text must not.
func TestDuplicateSemantics(t *testing.T) {
	a := NormalizeText("Смотри: https://example.com/news?utm_source=1")
	b := NormalizeText("Смотри: https://example.com/news?utm_campaign=2")
	if a != b {
		t.Errorf("tracking params must not break equality: %q vs %q", a, b)
	}

	c := NormalizeText("Другое окружение: https://example.com/news")
	if a == c {
		t.Errorf("different surrounding text must NOT be a duplicate: %q", a)
	}

	// Different URLs of the SAME domain must stay different (the
	// normalized form keeps the full path — only identical links
	// after tracking/punctuation cleanup match).
	d := NormalizeText("Смотри: https://example.com/other")
	if a == d {
		t.Errorf("different paths on one domain must NOT be duplicates: %q", a)
	}
	e := NormalizeText("Смотри: https://example.com/news?ref=123")
	if a == e {
		t.Errorf("different non-tracking query must NOT be stripped: %q", a)
	}

	// The trailing-paren bypass: identical text, one ends with ")".
	x := NormalizeText("одна и та же фраза")
	y := NormalizeText("одна и та же фраза)")
	if x != y {
		t.Errorf("trailing paren must not break equality: %q vs %q", x, y)
	}
	// ...and the same with a URL wrapped in parentheses.
	u1 := NormalizeText("смотри (https://example.com/news)")
	u2 := NormalizeText("смотри https://example.com/news")
	if u1 != u2 {
		t.Errorf("parens around URL must not break equality: %q vs %q", u1, u2)
	}
}

func TestHasURL(t *testing.T) {
	if !HasURL(NormalizeText("ссылка https://example.com")) {
		t.Error("URL not detected")
	}
	if HasURL(NormalizeText("просто текст без ссылок")) {
		t.Error("false URL detection")
	}
	if !HasURL(NormalizeText("https://t.me/channel/5")) {
		t.Error("t.me URL not detected")
	}
}
