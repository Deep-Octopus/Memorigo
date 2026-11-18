package embed

import (
	"container/list"
	"context"
	"sync"
)

// CachedEmbedder is a small concurrency-safe LRU cache in front of another
// embedder. Embedding calls are often the most expensive part of a memory
// round-trip; repeated queries and re-augmented messages benefit directly.
type CachedEmbedder struct {
	inner Embedder
	max   int

	mu      sync.Mutex
	ll      *list.List
	entries map[string]*list.Element
}

type cacheEntry struct {
	key string
	vec []float32
}

func NewCachedEmbedder(inner Embedder, max int) *CachedEmbedder {
	if max <= 0 {
		max = 1024
	}
	return &CachedEmbedder{
		inner:   inner,
		max:     max,
		ll:      list.New(),
		entries: make(map[string]*list.Element, max),
	}
}

func (c *CachedEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	key := text
	if vec := c.get(key); vec != nil {
		return vec, nil
	}
	vec, err := c.inner.EmbedText(ctx, text)
	if err != nil {
		return nil, err
	}
	c.put(key, vec)
	return vec, nil
}

func (c *CachedEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	missing := make([]int, 0, len(texts))
	missingTexts := make([]string, 0, len(texts))

	for i, text := range texts {
		if vec := c.get(text); vec != nil {
			out[i] = vec
			continue
		}
		missing = append(missing, i)
		missingTexts = append(missingTexts, text)
	}
	if len(missing) == 0 {
		return out, nil
	}

	vectors, err := c.inner.EmbedTexts(ctx, missingTexts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(missing) {
		return nil, ErrEmbeddingDimensionMismatch
	}
	for j, idx := range missing {
		out[idx] = vectors[j]
		c.put(missingTexts[j], vectors[j])
	}
	return out, nil
}

func (c *CachedEmbedder) get(key string) []float32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return nil
	}
	c.ll.MoveToFront(el)
	return el.Value.(*cacheEntry).vec
}

func (c *CachedEmbedder) put(key string, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		el.Value.(*cacheEntry).vec = vec
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&cacheEntry{key: key, vec: vec})
	c.entries[key] = el
	if c.ll.Len() > c.max {
		last := c.ll.Back()
		if last != nil {
			c.ll.Remove(last)
			delete(c.entries, last.Value.(*cacheEntry).key)
		}
	}
}

func (c *CachedEmbedder) Dimension() int { return c.inner.Dimension() }

func (c *CachedEmbedder) Provider() string { return c.inner.Provider() }

func (c *CachedEmbedder) Model() string { return c.inner.Model() }
