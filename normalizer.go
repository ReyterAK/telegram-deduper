//
// normalizer.go
// Antidubl — text and URL normalization for duplicate comparison
//
// Duplicate detection compares FULL normalized message text:
// case-folded, ё→е, whitespace collapsed, and every URL replaced
// by its canonical form (tracking parameters stripped). Two
// messages are duplicates only if the whole normalized text is
// identical — different surrounding text is NOT a duplicate.
//

package main

import (
	"regexp"
	"strings"
)

// urlRe matches http(s) URLs up to the first whitespace/quote;
// an opening "(" directly before the scheme is part of the token
// ("(https://…)" — the closing paren is trimmed as punctuation).
var urlRe = regexp.MustCompile(`(?i)\(?https?://[^\s<>"']+`)

// isTrackingParam reports whether a query parameter is tracking
// noise (utm_* or t.me deep-link params) and must be dropped.
func isTrackingParam(name string) bool {
	if strings.HasPrefix(name, "utm_") {
		return true
	}
	switch name {
	case "start", "startapp", "text", "comment":
		return true
	}
	return false
}

// NormalizeURL canonicalizes a raw URL token: lowercase, fragment
// and tracking params stripped, trailing punctuation removed.
func NormalizeURL(raw string) string {
	u := strings.ToLower(raw)
	u = strings.TrimLeft(u, "(")
	u = strings.TrimRight(u, trailingPunct)

	if i := strings.IndexByte(u, '#'); i >= 0 {
		u = u[:i]
	}

	if i := strings.IndexByte(u, '?'); i >= 0 {
		base, query := u[:i], u[i+1:]
		keep := make([]string, 0, 4)
		for _, p := range strings.Split(query, "&") {
			name := p
			if j := strings.IndexByte(p, '='); j >= 0 {
				name = p[:j]
			}
			if isTrackingParam(name) {
				continue
			}
			keep = append(keep, p)
		}
		if len(keep) > 0 {
			u = base + "?" + strings.Join(keep, "&")
		} else {
			u = base
		}
	}
	return u
}

// trailingPunct is stripped from the end of a normalized text (and
// from URL tokens): a trailing ")" or "..." is the cheapest way to
// bypass exact duplicate matching, and carries no meaning.
const trailingPunct = ".,;:!?…()\"„«»"

// NormalizeText folds case and whitespace, and replaces every URL
// with its canonical form.
func NormalizeText(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "ё", "е")
	s = urlRe.ReplaceAllStringFunc(s, NormalizeURL)
	return strings.TrimRight(strings.Join(strings.Fields(s), " "), trailingPunct)
}

// HasURL reports whether the (normalized) text contains a URL.
func HasURL(normText string) bool {
	return urlRe.MatchString(normText)
}
