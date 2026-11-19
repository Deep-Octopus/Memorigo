package textutil

import "strings"

// SearchTokens returns the unique lexical tokens used both to index a fact
// and to query that index. Order follows the first occurrence in the text.
func SearchTokens(s string) []string {
	raw := Tokenize(s)
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

// SearchDocument is the space-separated form stored for full-text indexing.
// An empty result means the text contained only stopwords or no tokens.
func SearchDocument(s string) string {
	return strings.Join(SearchTokens(s), " ")
}
