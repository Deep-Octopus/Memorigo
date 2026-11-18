// Package textutil provides small, dependency-free text helpers shared by the
// embedding and recall layers.
package textutil

import (
	"strings"
	"unicode"
)

var englishStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {},
	"but": {}, "by": {}, "can": {}, "could": {}, "did": {}, "do": {}, "does": {},
	"for": {}, "from": {}, "had": {}, "has": {}, "have": {}, "he": {}, "her": {},
	"him": {}, "his": {}, "how": {}, "i": {}, "if": {}, "in": {}, "is": {},
	"it": {}, "its": {}, "just": {}, "me": {}, "my": {}, "of": {}, "on": {},
	"or": {}, "our": {}, "she": {}, "so": {}, "that": {}, "the": {}, "their": {},
	"them": {}, "then": {}, "there": {}, "these": {}, "they": {}, "this": {},
	"those": {}, "to": {}, "was": {}, "we": {}, "were": {}, "what": {}, "when": {},
	"where": {}, "which": {}, "who": {}, "will": {}, "with": {}, "would": {},
	"you": {}, "your": {},
}

var chineseStopBigrams = map[string]struct{}{
	"我们": {}, "你们": {}, "他们": {}, "她们": {}, "我的": {}, "你的": {}, "他的": {},
	"这个": {}, "那个": {}, "这些": {}, "那些": {}, "就是": {}, "不是": {}, "可以": {},
	"什么": {}, "怎么": {}, "因为": {}, "所以": {}, "但是": {}, "然后": {}, "如果": {},
	"已经": {}, "还是": {}, "只是": {}, "现在": {}, "一个": {}, "一下": {}, "没有": {},
	"觉得": {}, "知道": {}, "时候": {}, "非常": {},
}

// Normalize lowercases text and collapses all whitespace runs into single spaces.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// SplitSentences splits free-form text on common sentence terminators.
func SplitSentences(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		part := strings.TrimSpace(b.String())
		b.Reset()
		if part != "" {
			out = append(out, part)
		}
	}
	for _, r := range s {
		switch r {
		case '.', '!', '?', '。', '！', '？', ';', '；', '\n', '\r':
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// Tokenize returns lexical tokens suitable for BM25-style scoring and feature
// hashing: lowercased Latin words plus CJK character bigrams. Chinese text has
// no whitespace, so bigrams are the standard cheap segmentation strategy.
func Tokenize(s string) []string {
	s = strings.ToLower(s)
	tokens := make([]string, 0, 16)

	var latin strings.Builder
	var cjk []rune

	flushLatin := func() {
		t := latin.String()
		latin.Reset()
		// Keep single-character terms: they can be meaningful values ("C",
		// "R", digits) and dropping them makes distinct facts collide in the
		// content-hash dedup key.
		if len(t) >= 1 {
			if _, stop := englishStopwords[t]; !stop {
				tokens = append(tokens, t)
			}
		}
	}
	flushCJK := func() {
		if len(cjk) == 1 {
			tokens = append(tokens, string(cjk[0]))
		}
		for i := 0; i+1 < len(cjk); i++ {
			bg := string(cjk[i : i+2])
			if _, stop := chineseStopBigrams[bg]; !stop {
				tokens = append(tokens, bg)
			}
		}
		cjk = cjk[:0]
	}

	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin.WriteRune(r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return tokens
}

// Key builds a stable deduplication key from lexical tokens. It intentionally
// drops casing, punctuation, whitespace runs and common stopwords so that
// "I like Go." and "i like go" collapse to the same key.
func Key(s string) string {
	key := strings.Join(Tokenize(s), " ")
	if key == "" {
		return Normalize(s)
	}
	return key
}

// Truncate cuts s to at most n runes, appending an ellipsis when truncated.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return string(runes[:1])
	}
	return string(runes[:n-1]) + "…"
}

// IsQuestion reports whether a sentence looks like a question. Questions are
// not long-term facts about the user.
func IsQuestion(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return false
	}
	if strings.HasSuffix(s, "?") || strings.HasSuffix(s, "？") {
		return true
	}
	for _, suffix := range []string{"吗", "呢", "么"} {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	prefixes := []string{
		"what ", "who ", "when ", "where ", "why ", "how ", "which ", "whose ",
		"can ", "could ", "do ", "does ", "did ", "is ", "are ", "will ",
		"would ", "should ", "may ", "might ", "shall ", "am ",
		"什么", "怎么", "为什么", "哪", "谁", "多少", "是否", "能不能", "可不可以",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
