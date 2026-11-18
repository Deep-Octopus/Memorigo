package textutil

import (
	"reflect"
	"testing"
)

func TestTokenizeMixedText(t *testing.T) {
	got := Tokenize("My favorite programming language is Go, 我喜欢用 Go 写代码。")
	want := []string{
		"favorite", "programming", "language", "go",
		"我喜", "喜欢", "欢用", "go", "写代", "代码",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokenize() = %#v, want %#v", got, want)
	}
}

func TestTokenizeDropsStopwords(t *testing.T) {
	got := Tokenize("this is a test of the tokenizer")
	want := []string{"test", "tokenizer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokenize() = %#v, want %#v", got, want)
	}
}

func TestTokenizeKeepsSingleCharacterValues(t *testing.T) {
	got := Tokenize("My favorite number is 7, language C")
	want := []string{"favorite", "number", "7", "language", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokenize() = %#v, want %#v", got, want)
	}
	if Key("My favorite number is 7") == Key("My favorite number is 8") {
		t.Fatal("distinct single-character values must not share a dedup key")
	}
}

func TestKeyNormalizesCasePunctuationAndStopwords(t *testing.T) {
	a := Key("My favorite color is blue.")
	b := Key("my favorite color: blue")
	if a != b {
		t.Fatalf("Key mismatch: %q vs %q", a, b)
	}
	if Key("Hello!!!") == "" {
		t.Fatal("Key should not be empty for non-empty input")
	}
}

func TestNormalizeCollapsesWhitespace(t *testing.T) {
	if got := Normalize("  Hello \n\t World  "); got != "hello world" {
		t.Fatalf("Normalize() = %q", got)
	}
}

func TestIsQuestion(t *testing.T) {
	cases := map[string]bool{
		"What is your name?": true,
		"where do you live":  true,
		"你叫什么名字？":            true,
		"你喜欢猫吗":              true,
		"My name is Alice":   false,
		"我喜欢猫":               false,
		"I live in Shanghai": false,
		"Can you help me":    true,
		"这是我的猫":              false,
	}
	for input, want := range cases {
		if got := IsQuestion(input); got != want {
			t.Errorf("IsQuestion(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestSplitSentences(t *testing.T) {
	got := SplitSentences("My name is Alice. I live in Shanghai! 我喜欢猫。你呢？")
	want := []string{"My name is Alice", "I live in Shanghai", "我喜欢猫", "你呢"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitSentences() = %#v, want %#v", got, want)
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	if got := Truncate("你好世界", 3); got != "你好…" {
		t.Fatalf("Truncate() = %q", got)
	}
	if got := Truncate("short", 10); got != "short" {
		t.Fatalf("Truncate() = %q", got)
	}
}
