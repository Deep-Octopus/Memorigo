package memori

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Deep-Octopus/Memorigo/embed"
	"github.com/Deep-Octopus/Memorigo/storage"
	"github.com/Deep-Octopus/Memorigo/textutil"
)

type rankedFact struct {
	fact       storage.FactResult
	score      float64
	similarity float64
	lexical    float64
	recency    float64
	frequency  float64
}

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// rankFacts combines dense similarity, BM25 lexical overlap, recency decay and
// repeated-mention frequency. Hybrid scoring is important because offline hash
// embeddings are lexical by nature, while API embeddings can miss exact names
// and identifiers that BM25 catches.
//
// Fusion "weighted" (the default) mixes normalized signals with a linear
// combination. Fusion "rrf" uses weighted reciprocal rank fusion so a fact
// that is best on one signal is not buried by a mediocre score on another.
// An empty query always uses the weighted path and means "latest facts".
func rankFacts(query string, queryEmbedding []float32, facts []storage.FactResult, cfg RecallConfig, now time.Time) []rankedFact {
	if len(facts) == 0 {
		return nil
	}
	if cfg.useRRF() && strings.TrimSpace(query) != "" {
		return rankFactsRRF(query, queryEmbedding, facts, cfg, now)
	}
	return rankFactsWeighted(query, queryEmbedding, facts, cfg, now)
}

func rankFactsWeighted(query string, queryEmbedding []float32, facts []storage.FactResult, cfg RecallConfig, now time.Time) []rankedFact {

	wSim, wLex, wRec, wFreq := normalizedWeights(cfg)
	halfLife := cfg.RecencyHalfLife
	if halfLife <= 0 {
		halfLife = 7 * 24 * time.Hour
	}
	minSimilarity := cfg.MinSimilarity
	if minSimilarity < 0 {
		minSimilarity = 0
	}

	queryTokens := uniqueStrings(textutil.Tokenize(query))
	lexicalScores, maxLexical := bm25Scores(queryTokens, facts)

	maxLogFreq := 0.0
	for _, f := range facts {
		if v := math.Log1p(float64(f.NumTimes)); v > maxLogFreq {
			maxLogFreq = v
		}
	}

	out := make([]rankedFact, 0, len(facts))
	for i, f := range facts {
		sim := 0.0
		if len(queryEmbedding) > 0 && len(queryEmbedding) == len(f.Embedding) {
			sim = embed.CosineSimilarity(queryEmbedding, f.Embedding)
			if sim < 0 {
				sim = 0
			}
		}

		lexRaw := lexicalScores[i]
		lex := 0.0
		if maxLexical > 0 {
			lex = lexRaw / maxLexical
		}

		// Relevance gate: unrelated facts must not be recalled purely because
		// they are recent. An empty query intentionally means "latest facts".
		if len(queryTokens) > 0 && lexRaw == 0 && sim < minSimilarity {
			continue
		}

		recency := 0.0
		if !f.DateLastTime.IsZero() {
			age := now.Sub(f.DateLastTime)
			if age < 0 {
				age = 0
			}
			recency = math.Exp(-math.Ln2 * age.Hours() / halfLife.Hours())
		}
		frequency := 0.0
		if maxLogFreq > 0 {
			frequency = math.Log1p(float64(f.NumTimes)) / maxLogFreq
		}

		score := wSim*sim + wLex*lex + wRec*recency + wFreq*frequency
		if score < cfg.MinScore {
			continue
		}
		out = append(out, rankedFact{
			fact:       f,
			score:      score,
			similarity: sim,
			lexical:    lex,
			recency:    recency,
			frequency:  frequency,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if !a.fact.DateLastTime.Equal(b.fact.DateLastTime) {
			return a.fact.DateLastTime.After(b.fact.DateLastTime)
		}
		if a.fact.NumTimes != b.fact.NumTimes {
			return a.fact.NumTimes > b.fact.NumTimes
		}
		return a.fact.ID > b.fact.ID
	})
	return out
}

// rankFactsRRF fuses per-signal rankings with weighted reciprocal rank fusion:
//
//	score(d) = Σ_r  weight_r / (k + rank_r(d))
//
// The score is then divided by the best possible score so it stays in [0, 1]
// and MinScore keeps the same meaning as weighted fusion. A signal with a
// zero weight is left out of the fusion. Facts that fail the relevance gate
// never enter a ranking.
func rankFactsRRF(query string, queryEmbedding []float32, facts []storage.FactResult, cfg RecallConfig, now time.Time) []rankedFact {
	wSim, wLex, wRec, wFreq := normalizedWeights(cfg)
	halfLife := cfg.RecencyHalfLife
	if halfLife <= 0 {
		halfLife = 7 * 24 * time.Hour
	}
	minSimilarity := cfg.MinSimilarity
	if minSimilarity < 0 {
		minSimilarity = 0
	}
	k := float64(cfg.RRFK)
	if k <= 0 {
		k = 60
	}

	queryTokens := uniqueStrings(textutil.Tokenize(query))
	lexicalScores, _ := bm25Scores(queryTokens, facts)

	maxLogFreq := 0.0
	for _, f := range facts {
		if v := math.Log1p(float64(f.NumTimes)); v > maxLogFreq {
			maxLogFreq = v
		}
	}

	type item struct {
		fact   storage.FactResult
		sim    float64
		lexRaw float64
		lex    float64
		rec    float64
		freq   float64
	}
	items := make([]item, 0, len(facts))
	maxLexical := 0.0
	for _, raw := range lexicalScores {
		if raw > maxLexical {
			maxLexical = raw
		}
	}
	for i, f := range facts {
		sim := 0.0
		if len(queryEmbedding) > 0 && len(queryEmbedding) == len(f.Embedding) {
			sim = embed.CosineSimilarity(queryEmbedding, f.Embedding)
			if sim < 0 {
				sim = 0
			}
		}
		lexRaw := lexicalScores[i]
		if len(queryTokens) > 0 && lexRaw == 0 && sim < minSimilarity {
			continue
		}
		lex := 0.0
		if maxLexical > 0 {
			lex = lexRaw / maxLexical
		}
		recency := 0.0
		if !f.DateLastTime.IsZero() {
			age := now.Sub(f.DateLastTime)
			if age < 0 {
				age = 0
			}
			recency = math.Exp(-math.Ln2 * age.Hours() / halfLife.Hours())
		}
		frequency := 0.0
		if maxLogFreq > 0 {
			frequency = math.Log1p(float64(f.NumTimes)) / maxLogFreq
		}
		items = append(items, item{
			fact: f, sim: sim, lexRaw: lexRaw, lex: lex, rec: recency, freq: frequency,
		})
	}
	if len(items) == 0 {
		return nil
	}

	raw := make([]float64, len(items))
	addRanking := func(weight float64, value func(item) float64) {
		if weight <= 0 {
			return
		}
		order := make([]int, 0, len(items))
		for i, it := range items {
			if value(it) <= 0 {
				continue
			}
			order = append(order, i)
		}
		sort.SliceStable(order, func(a, b int) bool {
			ia, ib := order[a], order[b]
			va, vb := value(items[ia]), value(items[ib])
			if va != vb {
				return va > vb
			}
			if !items[ia].fact.DateLastTime.Equal(items[ib].fact.DateLastTime) {
				return items[ia].fact.DateLastTime.After(items[ib].fact.DateLastTime)
			}
			return items[ia].fact.ID > items[ib].fact.ID
		})
		for rank, idx := range order {
			raw[idx] += weight / (k + float64(rank+1))
		}
	}
	addRanking(wSim, func(it item) float64 { return it.sim })
	addRanking(wLex, func(it item) float64 { return it.lexRaw })
	addRanking(wRec, func(it item) float64 { return it.rec })
	addRanking(wFreq, func(it item) float64 { return it.freq })

	best := 0.0
	for _, w := range []float64{wSim, wLex, wRec, wFreq} {
		if w > 0 {
			best += w / (k + 1)
		}
	}
	if best <= 0 {
		return nil
	}

	out := make([]rankedFact, 0, len(items))
	for i, it := range items {
		score := raw[i] / best
		if score < cfg.MinScore {
			continue
		}
		out = append(out, rankedFact{
			fact:       it.fact,
			score:      score,
			similarity: it.sim,
			lexical:    it.lex,
			recency:    it.rec,
			frequency:  it.freq,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if !a.fact.DateLastTime.Equal(b.fact.DateLastTime) {
			return a.fact.DateLastTime.After(b.fact.DateLastTime)
		}
		if a.fact.NumTimes != b.fact.NumTimes {
			return a.fact.NumTimes > b.fact.NumTimes
		}
		return a.fact.ID > b.fact.ID
	})
	return out
}

func (cfg RecallConfig) useRRF() bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Fusion), "rrf")
}

func normalizedWeights(cfg RecallConfig) (sim, lex, rec, freq float64) {
	sim = clamp01(cfg.WeightSimilarity)
	lex = clamp01(cfg.WeightLexical)
	rec = clamp01(cfg.WeightRecency)
	freq = clamp01(cfg.WeightFrequency)
	sum := sim + lex + rec + freq
	if sum == 0 {
		return 0.60, 0.20, 0.10, 0.10
	}
	return sim / sum, lex / sum, rec / sum, freq / sum
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// factsFromRanked converts the internal ranking into the public Fact shape.
func factsFromRanked(ranked []rankedFact) []Fact {
	out := make([]Fact, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, Fact{
			ID:             r.fact.ID,
			Content:        r.fact.Content,
			Slot:           r.fact.Slot,
			Score:          r.score,
			Similarity:     r.similarity,
			LexicalScore:   r.lexical,
			NumTimes:       r.fact.NumTimes,
			DateLastTime:   r.fact.DateLastTime,
			ConversationID: r.fact.SourceConversationID,
			ValidFrom:      r.fact.ValidFrom,
			ValidTo:        r.fact.ValidTo,
			MessageID:      r.fact.SourceMessageID,
			Confidence:     r.fact.Confidence,
			AssertedBy:     r.fact.AssertedBy,
		})
	}
	return out
}

// selectDiverseRanked applies maximal marginal relevance (MMR) to the ranked
// pool: each pick maximizes relevance minus redundancy against what is already
// selected. This avoids filling a small budget with near-duplicate memories.
// lambda=1 is pure relevance and reproduces top-k behavior.
func selectDiverseRanked(ranked []rankedFact, limit int, lambda float64) []rankedFact {
	if limit <= 0 || len(ranked) == 0 {
		return nil
	}
	if len(ranked) <= limit || lambda >= 0.999 {
		if len(ranked) > limit {
			return ranked[:limit]
		}
		return ranked
	}
	lambda = clamp01(lambda)
	selected := make([]rankedFact, 0, limit)
	remaining := make([]rankedFact, len(ranked))
	copy(remaining, ranked)

	for len(selected) < limit && len(remaining) > 0 {
		bestIdx := 0
		bestValue := math.Inf(-1)
		for i, candidate := range remaining {
			maxSim := 0.0
			for _, chosen := range selected {
				if sim := factSimilarity(candidate.fact, chosen.fact); sim > maxSim {
					maxSim = sim
				}
			}
			value := lambda*candidate.score - (1-lambda)*maxSim
			if value > bestValue {
				bestValue = value
				bestIdx = i
			}
		}
		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}
	return selected
}

// factSimilarity prefers dense cosine similarity and falls back to token
// Jaccard when embeddings are unavailable or have different dimensions.
func factSimilarity(a, b storage.FactResult) float64 {
	if len(a.Embedding) > 0 && len(a.Embedding) == len(b.Embedding) {
		return clamp01(embed.CosineSimilarity(a.Embedding, b.Embedding))
	}
	return tokenJaccard(a.Content, b.Content)
}

func tokenJaccard(a, b string) float64 {
	at := uniqueStrings(defaultFactTokens.tokenize(a))
	bt := uniqueStrings(defaultFactTokens.tokenize(b))
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(at))
	for _, t := range at {
		set[t] = struct{}{}
	}
	inter := 0
	for _, t := range bt {
		if _, ok := set[t]; ok {
			inter++
		}
	}
	union := len(at) + len(bt) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// bm25Scores computes one BM25 score per fact and returns the maximum raw
// score so callers can normalize to [0, 1]. It only tracks term frequencies
// for the query terms, which keeps the per-fact working set small.
func bm25Scores(queryTokens []string, facts []storage.FactResult) ([]float64, float64) {
	scores := make([]float64, len(facts))
	if len(queryTokens) == 0 {
		return scores, 0
	}

	q := len(queryTokens)
	tfs := make([]float64, len(facts)*q)
	docLens := make([]int, len(facts))
	docFreq := make([]int, q)
	totalLen := 0

	for i, f := range facts {
		tokens := defaultFactTokens.tokenize(f.Content)
		docLens[i] = len(tokens)
		totalLen += len(tokens)

		base := i * q
		for j, qt := range queryTokens {
			count := 0
			for _, token := range tokens {
				if token == qt {
					count++
				}
			}
			tfs[base+j] = float64(count)
			if count > 0 {
				docFreq[j]++
			}
		}
	}

	avgLen := 0.0
	if len(facts) > 0 {
		avgLen = float64(totalLen) / float64(len(facts))
	}
	if avgLen == 0 {
		return scores, 0
	}

	n := float64(len(facts))
	idf := make([]float64, q)
	for j := range idf {
		df := float64(docFreq[j])
		idf[j] = math.Log(1 + (n-df+0.5)/(df+0.5))
	}

	maxScore := 0.0
	for i := range facts {
		dl := float64(docLens[i])
		if dl == 0 {
			continue
		}
		base := i * q
		score := 0.0
		for j := range queryTokens {
			freq := tfs[base+j]
			if freq == 0 {
				continue
			}
			denom := freq + bm25K1*(1-bm25B+bm25B*dl/avgLen)
			score += idf[j] * freq * (bm25K1 + 1) / denom
		}
		scores[i] = score
		if score > maxScore {
			maxScore = score
		}
	}
	return scores, maxScore
}
