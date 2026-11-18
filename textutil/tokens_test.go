package textutil

import "testing"

func TestEstimateTokens(t *testing.T) {
	cases := map[string]int{
		"":              0,
		"hello world":   4,
		"Hello, world!": 6,
		"我喜欢用 Go 写代码":   8, // 6 CJK + "go" (1) + … punctuation none
		"你好，世界":         5, // 4 CJK + 1 punctuation
		"12345678":      2,
		"a":             1,
	}
	for input, want := range cases {
		if got := EstimateTokens(input); got != want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestTruncateToTokens(t *testing.T) {
	if got, truncated := TruncateToTokens("short", 10); got != "short" || truncated {
		t.Fatalf("got %q truncated=%v", got, truncated)
	}
	if got, truncated := TruncateToTokens("我喜欢用 Go 写代码", 3); !truncated || EstimateTokens(got) > 3 {
		t.Fatalf("got %q tokens=%d truncated=%v", got, EstimateTokens(got), truncated)
	}
	if got, truncated := TruncateToTokens("hello world", 0); got != "" || !truncated {
		t.Fatalf("got %q truncated=%v", got, truncated)
	}
	// Rune safety: never return invalid UTF-8.
	got, _ := TruncateToTokens("你好世界", 2)
	if got != "你好" {
		t.Fatalf("got %q", got)
	}
}
