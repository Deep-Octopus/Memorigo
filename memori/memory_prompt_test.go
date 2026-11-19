package memori

import (
	"strings"
	"testing"
)

func TestRenderPackageBlockIncludesProfile(t *testing.T) {
	pkg := ContextPackage{
		ProfileText: "<memory_profile>\nIdentity:\n- [fact:1] My name is Alice\n</memory_profile>",
		Facts:       []Fact{{ID: 2, Content: "My favorite color is blue"}},
	}
	block := renderPackageBlock(DefaultMemoryPrompt, pkg)
	if !strings.Contains(block, "<memory_profile>") || !strings.Contains(block, "Alice") {
		t.Fatalf("profile missing: %q", block)
	}
	if !strings.Contains(block, "- My favorite color is blue") {
		t.Fatalf("facts missing: %q", block)
	}
	if strings.Contains(block, "{{profile}}") || strings.Contains(block, "{{facts}}") {
		t.Fatalf("unrendered placeholder: %q", block)
	}
	if strings.Index(block, "<memory_profile>") > strings.Index(block, "favorite color") {
		t.Fatalf("profile should precede facts: %q", block)
	}
}

func TestRenderPackageBlockBackfillsOldTemplate(t *testing.T) {
	pkg := ContextPackage{
		ProfileText: "<memory_profile>\nIdentity:\n- Alice\n</memory_profile>",
		Facts:       []Fact{{Content: "blue"}},
	}
	block := renderPackageBlock("FACTS:\n{{facts}}", pkg)
	if !strings.Contains(block, "<memory_profile>") || !strings.Contains(block, "blue") {
		t.Fatalf("profile not backfilled: %q", block)
	}
}

func TestRenderMemoryBlockStripsProfilePlaceholder(t *testing.T) {
	block := renderMemoryBlock(DefaultMemoryPrompt, []Fact{{Content: "blue"}})
	if strings.Contains(block, "{{profile}}") {
		t.Fatalf("literal placeholder leaked: %q", block)
	}
	if !strings.Contains(block, "- blue") {
		t.Fatalf("facts missing: %q", block)
	}
}
