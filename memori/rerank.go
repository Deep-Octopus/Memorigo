package memori

import (
	"context"
	"sort"
	"strings"
)

// applyRerank reorders the head of an already ranked list. It is a no-op when
// no reranker is configured, the query is empty, or TopN is not positive.
// A reranker error leaves the hybrid order in place.
//
// When the pass runs, Fact.Score becomes a strictly decreasing rank in (0, 1]
// so later sorts and MMR follow the reranker. Similarity and lexical scores
// stay as the hybrid ranker computed them.
func (m *Memori) applyRerank(ctx context.Context, query string, ranked []rankedFact, topN int) []rankedFact {
	if m == nil || m.Reranker == nil || topN <= 0 || len(ranked) < 2 || strings.TrimSpace(query) == "" {
		return ranked
	}
	n := topN
	if n > len(ranked) {
		n = len(ranked)
	}
	docs := make([]string, n)
	for i := 0; i < n; i++ {
		docs[i] = ranked[i].fact.Content
	}
	scores, err := m.Reranker.Rerank(ctx, query, docs)
	if err != nil || len(scores) != n {
		if m.logger != nil {
			if err != nil {
				m.logger.Warn("rerank failed; keeping hybrid order", "error", err)
			} else {
				m.logger.Warn("rerank returned a different length; keeping hybrid order", "got", len(scores), "want", n)
			}
		}
		return ranked
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		if scores[order[a]] == scores[order[b]] {
			return order[a] < order[b]
		}
		return scores[order[a]] > scores[order[b]]
	})
	head := make([]rankedFact, n)
	for i, src := range order {
		head[i] = ranked[src]
	}
	out := make([]rankedFact, 0, len(ranked))
	out = append(out, head...)
	out = append(out, ranked[n:]...)
	for i := range out {
		out[i].score = 1 - float64(i)/float64(len(out)+1)
	}
	return out
}
