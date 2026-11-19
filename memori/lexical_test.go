package memori_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestRecallFindsFactOutsideRecencyWindow(t *testing.T) {
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_RECALL_SCAN_LIMIT": "2",
	})
	m.Attribution("user", "proc")

	if _, err := m.Remember("The secret project codename is zephyrquark"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := m.Remember(fmt.Sprintf("Unrelated weather note %d about sunshine", i)); err != nil {
			t.Fatal(err)
		}
	}

	facts, err := m.Recall("zephyrquark", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(facts, "zephyrquark") {
		t.Fatalf("lexical index missed the old fact: %#v", facts)
	}
}

func TestRecallLexicalChineseOutsideRecencyWindow(t *testing.T) {
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_RECALL_SCAN_LIMIT": "2",
	})
	m.Attribution("user", "proc")

	if _, err := m.Remember("我最喜欢的咖啡是美式咖啡"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := m.Remember(fmt.Sprintf("今天的天气记录 %d 晴朗", i)); err != nil {
			t.Fatal(err)
		}
	}

	facts, err := m.Recall("美式咖啡", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFact(facts, "美式咖啡") {
		t.Fatalf("chinese lexical index missed the old fact: %#v", facts)
	}
}

func TestRecallLexicalDoesNotCrossEntities(t *testing.T) {
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_RECALL_SCAN_LIMIT": "2",
	})
	m.Attribution("alice", "proc")
	if _, err := m.Remember("The secret project codename is zephyrquark"); err != nil {
		t.Fatal(err)
	}
	m.Attribution("bob", "proc")
	for i := 0; i < 4; i++ {
		if _, err := m.Remember(fmt.Sprintf("Bob weather note %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := m.Recall("zephyrquark", 5)
	if err != nil {
		t.Fatal(err)
	}
	if containsFact(facts, "zephyrquark") {
		t.Fatalf("bob recalled alice's fact: %#v", facts)
	}
}

func containsFact(facts []memori.Fact, needle string) bool {
	for _, fact := range facts {
		if strings.Contains(fact.Content, needle) {
			return true
		}
	}
	return false
}
