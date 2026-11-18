package memori

import (
	"math"
	"sort"
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
func rankFacts(query string, queryEmbedding []float32, facts []storage.FactResult, cfg RecallConfig, now time.Time) []rankedFact {
	if len(facts) == 0 {
		return nil
	}

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
