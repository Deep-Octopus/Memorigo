package memori

import (
	"container/list"
	"sync"

	"github.com/Deep-Octopus/Memorigo/textutil"
)

// factTokenCache avoids re-tokenizing the same fact on every recall. Facts are
// immutable after insertion, so content is a safe cache key. The cache is
// bounded to keep memory predictable; eviction only costs a future
// tokenization.
type factTokenCache struct {
	mu    sync.Mutex
	max   int
	items map[string]*list.Element
	order *list.List
}

type factTokenEntry struct {
	key    string
	tokens []string
}

func newFactTokenCache(max int) *factTokenCache {
	if max <= 0 {
		max = 4096
	}
	return &factTokenCache{
		max:   max,
		items: make(map[string]*list.Element, max),
		order: list.New(),
	}
}

func (c *factTokenCache) tokenize(content string) []string {
	if content == "" {
		return nil
	}
	c.mu.Lock()
	if el, ok := c.items[content]; ok {
		c.order.MoveToFront(el)
		tokens := el.Value.(*factTokenEntry).tokens
		c.mu.Unlock()
		return tokens
	}
	c.mu.Unlock()

	tokens := textutil.Tokenize(content)

	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[content]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*factTokenEntry).tokens
	}
	entry := &factTokenEntry{key: content, tokens: tokens}
	el := c.order.PushFront(entry)
	c.items[content] = el
	for len(c.items) > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*factTokenEntry).key)
	}
	return tokens
}

var defaultFactTokens = newFactTokenCache(4096)
