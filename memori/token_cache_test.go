package memori

import (
	"sync"
	"testing"
)

func TestFactTokenCacheEvicts(t *testing.T) {
	c := newFactTokenCache(2)
	if got := c.tokenize("My favorite color is blue"); len(got) == 0 {
		t.Fatal("expected tokens")
	}
	c.tokenize("I live in Shanghai")
	c.tokenize("I like tea")
	if _, ok := c.items["My favorite color is blue"]; ok {
		t.Fatal("oldest entry should be evicted")
	}
	if len(c.items) != 2 {
		t.Fatalf("cache size = %d, want 2", len(c.items))
	}
}

func TestFactTokenCacheConcurrent(t *testing.T) {
	c := newFactTokenCache(32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if got := c.tokenize("shared content " + string(rune('a'+i))); len(got) == 0 {
					t.Error("empty tokens")
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
