package embed

import (
	"context"
	"hash/fnv"
)

// HashEmbedder 简单的哈希嵌入器，用于离线/演示用途
type HashEmbedder struct {
	dimension int
}

func NewHashEmbedder() *HashEmbedder {
	return &HashEmbedder{
		dimension: 64, // 与原来的实现保持一致
	}
}

func (e *HashEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	return e.embedText(text), nil
}

func (e *HashEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		result[i] = e.embedText(text)
	}
	return result, nil
}

func (e *HashEmbedder) embedText(text string) []float32 {
	v := make([]float32, e.dimension)
	if text == "" {
		return v
	}

	h := fnv.New64a()
	for i, r := range text {
		h.Reset()
		_, _ = h.Write([]byte(string(r)))
		val := int64(h.Sum64())
		idx := i % e.dimension
		v[idx] += float32(val%1000) / 1000.0
	}
	return v
}

func (e *HashEmbedder) Dimension() int {
	return e.dimension
}

func (e *HashEmbedder) Provider() string {
	return "hash"
}
