package memori

import (
	"context"
	"testing"
)

func TestHeuristicExtractorEnglishPatterns(t *testing.T) {
	cases := []struct {
		text      string
		wantSlot  string
		exclusive bool
	}{
		{"My name is Alice", "profile.name", true},
		{"My favorite color is blue", "favorite.color", true},
		{"My birthday is March 3", "profile.birthday", true},
		{"I live in Shanghai", "profile.location", true},
		{"I'm from Canada", "profile.origin", true},
		{"I work at Google", "profile.employer", true},
		{"I work in Berlin", "profile.workplace", true},
		{"I'm a software engineer", "profile.role", true},
		{"I love Go", "sentiment.go", true},
		{"I'm learning Spanish", "skill.spanish", false},
		{"I know Python", "skill.python", false},
		{"I'm allergic to peanuts", "health.allergy.peanuts", true},
		{"Remember that my passport is in the drawer", "", false},
	}
	for _, tc := range cases {
		cs := extractCandidatesFromSentence(tc.text)
		if len(cs) != 1 {
			t.Errorf("%q: got %d candidates, want 1", tc.text, len(cs))
			continue
		}
		if cs[0].Slot != tc.wantSlot {
			t.Errorf("%q: slot = %q, want %q", tc.text, cs[0].Slot, tc.wantSlot)
		}
		if cs[0].Exclusive != tc.exclusive {
			t.Errorf("%q: exclusive = %v, want %v", tc.text, cs[0].Exclusive, tc.exclusive)
		}
	}
}

func TestHeuristicExtractorChinesePatterns(t *testing.T) {
	cases := []struct {
		text     string
		wantSlot string
	}{
		{"我叫小明", "profile.name"},
		{"我的名字是李雷", "profile.name"},
		{"我最喜欢的颜色是蓝色", "favorite.颜色"},
		{"我住在北京", "profile.location"},
		{"我来自四川", "profile.origin"},
		{"我在字节跳动工作", "profile.workplace"},
		{"我的生日是五月三号", "profile.birthday"},
		{"我是程序员", "profile.role"},
		{"我喜欢猫", "sentiment.猫"},
		{"我对花生过敏", "health.allergy.花生"},
	}
	for _, tc := range cases {
		cs := extractCandidatesFromSentence(tc.text)
		if len(cs) != 1 {
			t.Errorf("%q: got %d candidates, want 1 (%#v)", tc.text, len(cs), cs)
			continue
		}
		if cs[0].Slot != tc.wantSlot {
			t.Errorf("%q: slot = %q, want %q", tc.text, cs[0].Slot, tc.wantSlot)
		}
	}
}

func TestHeuristicExtractorSkipsQuestionsNoiseAndAssistant(t *testing.T) {
	if cs := extractCandidatesFromSentence("What is my favorite color?"); len(cs) != 0 {
		t.Fatalf("question produced %#v", cs)
	}
	if cs := extractCandidatesFromSentence("I am fine"); len(cs) != 0 {
		t.Fatalf("noise produced %#v", cs)
	}
	if cs := extractCandidatesFromSentence("我很好"); len(cs) != 0 {
		t.Fatalf("chinese noise produced %#v", cs)
	}

	e := &HeuristicExtractor{maxFacts: 10}
	res, err := e.Extract(context.Background(), ExtractionInput{
		Messages: []Message{
			{Role: "assistant", Content: "You love Go"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Facts) != 0 {
		t.Fatalf("assistant-only extraction = %#v", res.Facts)
	}
}

func TestHeuristicExtractorGeneralFirstPersonFallback(t *testing.T) {
	cases := []string{
		"I have two cats",
		"我家有三只猫",
		"My sister lives in Paris",
	}
	for _, c := range cases {
		cs := extractCandidatesFromSentence(c)
		if len(cs) != 1 || cs[0].Content == "" {
			t.Errorf("%q: general fallback failed: %#v", c, cs)
		}
	}
}

func TestHeuristicExtractorDeduplicatesAndCaps(t *testing.T) {
	e := &HeuristicExtractor{maxFacts: 2}
	res, err := e.Extract(context.Background(), ExtractionInput{
		Messages: []Message{{Role: "user", Content: "I love Go. I love Go! I live in Shanghai. My name is Alice."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Facts) != 2 {
		t.Fatalf("maxFacts not enforced: %#v", res.Facts)
	}
	if res.Summary == "" {
		t.Fatal("summary should not be empty")
	}
}

func TestCandidateCleanVersusRaw(t *testing.T) {
	if cs := extractCandidatesFromText("  Hello   world  "); len(cs) != 0 {
		t.Fatalf("non first-person text should not be a fact: %#v", cs)
	}
	cs := extractCandidatesFromText("My name is Bob. My name is Bob.")
	if len(cs) != 1 {
		t.Fatalf("dedupe failed: %#v", cs)
	}
	if cs[0].Content != "My name is Bob" {
		t.Fatalf("content = %q", cs[0].Content)
	}
}

func TestSanitizeSlotAndExclusive(t *testing.T) {
	if got := sanitizeSlot("favorite.color"); got != "favorite.color" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeSlot("profile.name; DROP TABLE"); got != "" {
		t.Fatalf("invalid slot accepted: %q", got)
	}
	if !isExclusiveSlot("sentiment.go") || !isExclusiveSlot("profile.name") || !isExclusiveSlot("favorite.color") {
		t.Fatal("exclusive slots not recognized")
	}
	if isExclusiveSlot("skill.python") || isExclusiveSlot("") {
		t.Fatal("non-exclusive slot misclassified")
	}
}

func TestPlausibleRole(t *testing.T) {
	if plausibleRole("bit tired") || plausibleRole("little sad") || plausibleRole("really happy") {
		t.Fatal("adjectives accepted as roles")
	}
	if !plausibleRole("software engineer") || !plausibleRole("程序员") || !plausibleRole("学生") {
		t.Fatal("valid roles rejected")
	}
}

func TestBuildTranscript(t *testing.T) {
	got := buildTranscript(ExtractionInput{
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
		Response: &Message{Role: "assistant", Content: "bye"},
	}, 10, 100)
	if got != "user: hello\nassistant: hi\nassistant: bye\n" {
		t.Fatalf("transcript = %q", got)
	}
}
