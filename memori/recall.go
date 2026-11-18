package memori

import "github.com/Deep-Octopus/Memorigo/embed"

// Recall is kept as a thin, stable façade for callers that constructed it
// directly. Ranking lives in scoring.go.
type Recall struct {
	m        *Memori
	embedder embed.Embedder
}

func NewRecall(m *Memori) *Recall {
	return &Recall{m: m, embedder: m.Embedder}
}

func (r *Recall) SearchFacts(query string, limit int) ([]Fact, error) {
	return r.m.Recall(query, limit)
}
