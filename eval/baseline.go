package eval

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Deep-Octopus/Memorigo/embed"
	"github.com/Deep-Octopus/Memorigo/memori"
	"github.com/Deep-Octopus/Memorigo/textutil"
)

const (
	defaultBaselineBudget = 800
	defaultNaiveK         = 5
	maxNaiveK             = 20
)

// chunk is one comparable piece of a subject's own memory. Full-context takes
// the newest chunks that fit the budget. Naive RAG takes the top hash-cosine
// chunks. Neither path calls an LLM.
type chunk struct {
	text string
	at   time.Time
	id   int64
}

func annotateBaselines(ctx context.Context, m *memori.Memori, queries []QueryCase, results []QueryResult) error {
	doc, err := m.Export(memori.ExportOptions{})
	if err != nil {
		return err
	}
	chunks := corpus(doc)
	emb := embed.NewHashEmbedder()
	for i := range queries {
		wants := baselineWants(queries[i])
		if len(wants) == 0 || i >= len(results) {
			continue
		}
		budget := baselineBudget(queries[i])
		full := packStop(sortNewest(chunks), budget)
		naive, err := packNaive(ctx, emb, queries[i].Query, chunks, budget, naiveK(queries[i]))
		if err != nil {
			return err
		}
		results[i].BaselineCompared = true
		results[i].FullContextHit = containsAll(full, wants)
		results[i].NaiveRAGHit = containsAll(naive, wants)
	}
	return nil
}

func baselineWants(q QueryCase) []string {
	if q.ExpectEmpty {
		return nil
	}
	raw := q.Want
	if q.Context != nil && len(q.Context.WantText) > 0 {
		raw = q.Context.WantText
	}
	out := make([]string, 0, len(raw))
	for _, want := range raw {
		// Citation and profile markup belong to Memorigo's renderer.
		// The baselines only have to surface the underlying evidence text.
		if strings.Contains(want, "[fact:") || strings.Contains(want, "[msg:") || strings.Contains(want, "<memory_") {
			continue
		}
		out = append(out, want)
	}
	return out
}

func baselineBudget(q QueryCase) int {
	if q.BaselineBudget > 0 {
		return q.BaselineBudget
	}
	if q.Context != nil && q.Context.Budget > 0 {
		return q.Context.Budget
	}
	return defaultBaselineBudget
}

func naiveK(q QueryCase) int {
	k := q.Limit
	if k <= 0 {
		k = defaultNaiveK
	}
	if k > maxNaiveK {
		k = maxNaiveK
	}
	return k
}

func corpus(doc memori.SubjectExport) []chunk {
	out := make([]chunk, 0, len(doc.Facts)+len(doc.Messages))
	for _, fact := range doc.Facts {
		if fact.Status != "" && fact.Status != "active" {
			continue
		}
		text := strings.TrimSpace(fact.Content)
		if text == "" {
			continue
		}
		out = append(out, chunk{text: text, at: fact.DateLastTime, id: fact.ID})
	}
	for _, msg := range doc.Messages {
		text := strings.TrimSpace(msg.Role + ": " + msg.Content)
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		out = append(out, chunk{text: text, at: msg.DateCreated, id: msg.ID})
	}
	return out
}

func sortNewest(chunks []chunk) []chunk {
	out := append([]chunk(nil), chunks...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].at.Equal(out[j].at) {
			return out[i].at.After(out[j].at)
		}
		return out[i].id > out[j].id
	})
	return out
}

// packStop appends chunks in the given order until the next one would exceed
// the budget. The first chunk is truncated so a window is never empty when
// any text exists.
func packStop(chunks []chunk, budget int) string {
	if budget <= 0 {
		budget = defaultBaselineBudget
	}
	var b strings.Builder
	used := 0
	for i, c := range chunks {
		cost := textutil.EstimateTokens(c.text)
		if cost == 0 {
			continue
		}
		if used+cost > budget {
			if i == 0 || used == 0 {
				cut, _ := textutil.TruncateToTokens(c.text, budget-used)
				b.WriteString(cut)
			}
			break
		}
		if used > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(c.text)
		used += cost
	}
	return b.String()
}

func packNaive(ctx context.Context, emb embed.Embedder, query string, chunks []chunk, budget, k int) (string, error) {
	if len(chunks) == 0 {
		return "", nil
	}
	qv, err := emb.EmbedText(ctx, query)
	if err != nil {
		return "", err
	}
	type scored struct {
		c     chunk
		score float64
	}
	ranked := make([]scored, 0, len(chunks))
	for _, c := range chunks {
		cv, err := emb.EmbedText(ctx, c.text)
		if err != nil {
			return "", err
		}
		ranked = append(ranked, scored{c: c, score: embed.CosineSimilarity(qv, cv)})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].c.id > ranked[j].c.id
	})
	if k > len(ranked) {
		k = len(ranked)
	}
	top := make([]chunk, k)
	for i := 0; i < k; i++ {
		top[i] = ranked[i].c
	}
	return packStop(top, budget), nil
}

func containsAll(text string, wants []string) bool {
	lower := strings.ToLower(text)
	for _, want := range wants {
		if !strings.Contains(lower, strings.ToLower(want)) {
			return false
		}
	}
	return true
}
