package textutil

import "testing"

func TestSearchDocumentKeepsCJKBigrams(t *testing.T) {
	doc := SearchDocument("我喜欢喝美式咖啡")
	for _, want := range []string{"美式", "咖啡"} {
		if !containsToken(doc, want) {
			t.Fatalf("document %q missing %q", doc, want)
		}
	}
}

func TestSearchTokensDedupes(t *testing.T) {
	got := SearchTokens("blue blue")
	if len(got) != 1 || got[0] != "blue" {
		t.Fatalf("tokens = %#v", got)
	}
}

func containsToken(doc, token string) bool {
	for _, part := range splitFields(doc) {
		if part == token {
			return true
		}
	}
	return false
}

func splitFields(s string) []string {
	return SearchTokens(s) // already unique; used only to scan the document tokens
}
