package memori

import (
	"math"
	"strings"
	"unicode"

	"github.com/Deep-Octopus/Memorigo/textutil"
)

const defaultAdaptiveCoverage = 0.85

// greetingPhrases are matched only at the start of a query, longest first.
// A following letter or digit keeps the word ("history" is not "hi").
var greetingPhrases = []string{
	"good morning",
	"good afternoon",
	"good evening",
	"good night",
	"hello there",
	"hey there",
	"hi there",
	"thank you",
	"thanks",
	"hello",
	"hiya",
	"howdy",
	"hey",
	"hi",
	"早上好",
	"下午好",
	"晚上好",
	"你好",
	"您好",
	"谢谢你",
	"谢谢",
	"多谢",
	"哈喽",
	"嗨",
}

// RecallDecision is a deterministic retrieval gate. It never calls a model.
type RecallDecision struct {
	// Skip means fact recall should not run. Profile assembly is unaffected.
	Skip bool
	// Reason is "empty", "greeting", or "sufficient" when Skip is set.
	Reason string
	// Query is the string to embed and rank. It is the original query when
	// adaptive retrieval is off.
	Query string
}

// DecideRecall skips an empty query, a greeting, or a query whose tokens are
// already present in existing text. A leading greeting is removed so
// "hi, what's my favorite color" searches the question. existing is caller
// text such as earlier turns; it is not itself searched. coverageMin <= 0 or
// greater than 1 uses 0.85. Rank scores are not multiplied by confidence.
func DecideRecall(query, existing string, enabled bool, coverageMin float64) RecallDecision {
	if !enabled {
		return RecallDecision{Query: query}
	}
	rewritten, _ := rewriteQuery(query)
	if strings.TrimSpace(rewritten) == "" {
		if strings.TrimSpace(query) == "" {
			return RecallDecision{Skip: true, Reason: "empty", Query: ""}
		}
		return RecallDecision{Skip: true, Reason: "greeting", Query: ""}
	}
	if strings.TrimSpace(existing) != "" && queryTokenCoverage(rewritten, existing) >= normalizeAdaptiveCoverage(coverageMin) {
		return RecallDecision{Skip: true, Reason: "sufficient", Query: rewritten}
	}
	return RecallDecision{Query: rewritten}
}

func normalizeAdaptiveCoverage(v float64) float64 {
	if v <= 0 || v > 1 || math.IsNaN(v) {
		return defaultAdaptiveCoverage
	}
	return v
}

// ExistingTurns joins earlier user and assistant turns and omits the latest
// user message. System text is left out so an instruction or a previous memory
// block is not treated as coverage. The result is capped at 8000 runes.
func ExistingTurns(messages []ChatMessage) string {
	lastUser := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			lastUser = i
			break
		}
	}
	var b strings.Builder
	for i, msg := range messages {
		if i == lastUser {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(content)
	}
	return textutil.Truncate(b.String(), 8000)
}

func rewriteQuery(s string) (string, bool) {
	s = strings.TrimSpace(s)
	strippedAny := false
	for i := 0; i < 4; i++ {
		next, stripped := stripLeadingGreeting(s)
		if !stripped || next == s {
			break
		}
		strippedAny = true
		s = next
		if s == "" {
			break
		}
	}
	return s, strippedAny
}

func stripLeadingGreeting(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	lowerRunes := []rune(strings.ToLower(s))
	runes := []rune(s)
	for _, phrase := range greetingPhrases {
		g := []rune(phrase)
		if len(lowerRunes) < len(g) {
			continue
		}
		if string(lowerRunes[:len(g)]) != phrase {
			continue
		}
		if len(lowerRunes) == len(g) {
			return "", true
		}
		next := lowerRunes[len(g)]
		if unicode.IsLetter(next) || unicode.IsDigit(next) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimLeftFunc(string(runes[len(g):]), func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsPunct(r)
		}))
		return rest, true
	}
	return s, false
}

// queryTokenCoverage is the fraction of usable query tokens that also appear
// in existing. Tokens shorter than two runes are ignored so a stray "s" from
// "what's" cannot keep a covered question in the search path. No usable query
// tokens means no skip.
func queryTokenCoverage(query, existing string) float64 {
	wanted := make(map[string]struct{})
	for _, token := range textutil.Tokenize(query) {
		if len([]rune(token)) < 2 {
			continue
		}
		wanted[token] = struct{}{}
	}
	if len(wanted) == 0 {
		return 0
	}
	covered := make(map[string]struct{}, len(wanted))
	for _, token := range textutil.Tokenize(existing) {
		if _, ok := wanted[token]; ok {
			covered[token] = struct{}{}
		}
	}
	return float64(len(covered)) / float64(len(wanted))
}
