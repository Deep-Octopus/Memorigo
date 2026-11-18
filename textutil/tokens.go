package textutil

import (
	"unicode"
)

// EstimateTokens is a provider-independent token estimate for budgeting. It is
// deliberately conservative: CJK runes count as one token each, Latin words as
// one token per four characters, and punctuation as one token. It is not a
// replacement for a model tokenizer, but it keeps context assembly bounded
// without shipping a tokenizer.
func EstimateTokens(s string) int {
	tokens := 0
	latinRun := 0
	flushLatin := func() {
		if latinRun > 0 {
			tokens += (latinRun + 3) / 4
			latinRun = 0
		}
	}
	for _, r := range s {
		switch {
		case isCJK(r):
			flushLatin()
			tokens++
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			latinRun++
		case unicode.IsSpace(r):
			flushLatin()
		default:
			flushLatin()
			tokens++
		}
	}
	flushLatin()
	return tokens
}

// TruncateToTokens returns the longest rune prefix whose estimated token count
// does not exceed max. It never splits a UTF-8 rune. The boolean reports
// whether truncation occurred.
func TruncateToTokens(s string, max int) (string, bool) {
	if max <= 0 {
		return "", s != ""
	}
	if EstimateTokens(s) <= max {
		return s, false
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if EstimateTokens(string(runes[:mid])) <= max {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo]), true
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
