package memori

import (
	"hash/fnv"
)

// embedText is a simple deterministic, offline embedding.
// It is NOT a real semantic embedding, but provides a fixed-size vector
// for similarity computations without external services.
func embedText(text string) []float32 {
	const dim = 64
	v := make([]float32, dim)
	if text == "" {
		return v
	}

	h := fnv.New64a()
	for i, r := range text {
		h.Reset()
		_, _ = h.Write([]byte(string(r)))
		val := int64(h.Sum64())
		idx := i % dim
		v[idx] += float32(val%1000) / 1000.0
	}
	return v
}


