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
		{"Текст с  разными   пробелами.", "текст с разными пробелами."},
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
