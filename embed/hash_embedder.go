package embed

import (
	"context"
	"hash/fnv"
	"math"

	"github.com/Deep-Octopus/Memorigo/textutil"
)

// HashEmbedder is a deterministic, dependency-free feature-hashing embedder.
// It captures lexical overlap (unigrams + token bigrams, including CJK
// bigrams), not deep semantics. It exists so the SDK has a zero-cost offline
// path and a stable fallback for tests.
type HashEmbedder struct {
	dimension int
}

func NewHashEmbedder() *HashEmbedder {
	return NewHashEmbedderWithDim(256)
}

func NewHashEmbedderWithDim(dimension int) *HashEmbedder {
	if dimension <= 0 {
		dimension = 256
	}
	return &HashEmbedder{dimension: dimension}
}

func (e *HashEmbedder) EmbedText(_ context.Context, text string) ([]float32, error) {
	return e.embedText(text), nil
}

func (e *HashEmbedder) EmbedTexts(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = e.embedText(text)
	}
	return out, nil
}

func (e *HashEmbedder) embedText(text string) []float32 {
	v := make([]float32, e.dimension)
	tokens := textutil.Tokenize(text)
	if len(tokens) == 0 {
		normalized := textutil.Normalize(text)
		if normalized == "" {
			return v
		}
		addFeature(v, "raw:"+normalized, 1)
	} else {
		for i, token := range tokens {
			addFeature(v, "u:"+token, 1)
			if i+1 < len(tokens) {
				addFeature(v, "b:"+token+"\x00"+tokens[i+1], 0.5)
			}
		}
	}

	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	if norm == 0 {
		return v
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= scale
	}
	return v
}

func addFeature(v []float32, feature string, weight float32) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(feature))
	sum := h.Sum64()
	idx := int(sum % uint64(len(v)))
	// Signed hashing keeps collisions from systematically inflating similarity.
	if sum&(1<<63) != 0 {
		weight = -weight
	}
	v[idx] += weight
}

func (e *HashEmbedder) Dimension() int { return e.dimension }

func (e *HashEmbedder) Provider() string { return "hash" }

func (e *HashEmbedder) Model() string { return "feature-hash-v1" }
