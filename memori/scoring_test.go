package memori

import (
	"context"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/embed"
	"github.com/Deep-Octopus/Memorigo/storage"
)

func makeFact(t *testing.T, id int64, content string, numTimes int64, age time.Duration) storage.FactResult {
	t.Helper()
	e := embed.NewHashEmbedder()
	vec, err := e.EmbedText(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	return storage.FactResult{
		ID:           id,
		Content:      content,
		Embedding:    vec,
		NumTimes:     numTimes,
		DateLastTime: time.Now().Add(-age),
	}
}

func TestRankFactsPrefersRelevantOverFrequentNoise(t *testing.T) {
	e := embed.NewHashEmbedder()
	query := "favorite color"
	queryVec, _ := e.EmbedText(context.Background(), query)

	facts := []storage.FactResult{
		makeFact(t, 1, "My favorite color is blue", 1, 48*time.Hour),
		makeFact(t, 2, "The weather is nice today", 50, 0),
	}
	ranked := rankFacts(query, queryVec, facts, RecallConfig{
		MinScore:         0,
		MinSimilarity:    0.1,
		WeightSimilarity: 0.6,
		WeightLexical:    0.2,
		WeightRecency:    0.1,
		WeightFrequency:  0.1,
		RecencyHalfLife:  7 * 24 * time.Hour,
	}, time.Now())

	if len(ranked) == 0 || ranked[0].fact.ID != 1 {
		t.Fatalf("expected relevant fact 1 to rank first, got %#v", ranked)
	}
	for _, r := range ranked[1:] {
		if r.score >= ranked[0].score {
			t.Fatalf("noise outranked relevant fact: %#v", ranked)
		}
	}
}

func TestRankFactsPrefersRecentWhenEquallyRelevant(t *testing.T) {
	query := "live in"
	queryVec, _ := embed.NewHashEmbedder().EmbedText(context.Background(), query)
	newest := makeFact(t, 2, "I live in Shanghai", 3, time.Hour)
	oldest := makeFact(t, 1, "I live in Shanghai", 3, 60*24*time.Hour)

	ranked := rankFacts(query, queryVec, []storage.FactResult{oldest, newest}, RecallConfig{
		MinScore:         0,
		MinSimilarity:    0,
		WeightSimilarity: 0.6,
		WeightLexical:    0.2,
		WeightRecency:    0.1,
		WeightFrequency:  0.1,
		RecencyHalfLife:  7 * 24 * time.Hour,
	}, time.Now())
	if len(ranked) != 2 {
		t.Fatalf("len = %d", len(ranked))
	}
	if ranked[0].fact.ID != 2 {
		t.Fatalf("recent fact should rank first: %#v", ranked)
	}
}

func TestRankFactsFrequencyBoost(t *testing.T) {
	query := "coffee"
	queryVec, _ := embed.NewHashEmbedder().EmbedText(context.Background(), query)
	frequent := makeFact(t, 1, "I like coffee", 20, time.Hour)
	rare := makeFact(t, 2, "I like coffee", 1, time.Hour)

	ranked := rankFacts(query, queryVec, []storage.FactResult{rare, frequent}, RecallConfig{
		MinScore:         0,
		MinSimilarity:    0,
		WeightSimilarity: 0.4,
		WeightLexical:    0.2,
		WeightRecency:    0.1,
		WeightFrequency:  0.3,
		RecencyHalfLife:  7 * 24 * time.Hour,
	}, time.Now())
	if len(ranked) != 2 || ranked[0].fact.ID != 1 {
		t.Fatalf("frequent fact should rank first: %#v", ranked)
	}
}

func TestRankFactsFiltersUnrelated(t *testing.T) {
	queryVec, _ := embed.NewHashEmbedder().EmbedText(context.Background(), "quantum physics")
	facts := []storage.FactResult{
		makeFact(t, 1, "My favorite color is blue", 1, time.Hour),
	}
	ranked := rankFacts("quantum physics", queryVec, facts, RecallConfig{
		MinScore:         0.05,
		MinSimilarity:    0.1,
		WeightSimilarity: 0.6,
		WeightLexical:    0.2,
		WeightRecency:    0.1,
		WeightFrequency:  0.1,
		RecencyHalfLife:  7 * 24 * time.Hour,
	}, time.Now())
	if len(ranked) != 0 {
		t.Fatalf("unrelated facts should be filtered: %#v", ranked)
	}
}

func TestRankFactsEmptyQueryReturnsRecent(t *testing.T) {
	facts := []storage.FactResult{
		makeFact(t, 1, "older", 1, 10*time.Hour),
		makeFact(t, 2, "newer", 1, time.Minute),
	}
	ranked := rankFacts("", nil, facts, RecallConfig{
		MinScore:        0,
		WeightRecency:   1,
		RecencyHalfLife: 24 * time.Hour,
	}, time.Now())
	if len(ranked) != 2 || ranked[0].fact.ID != 2 {
		t.Fatalf("empty query should rank by recency: %#v", ranked)
	}
}

func TestNormalizedWeights(t *testing.T) {
	sim, lex, rec, freq := normalizedWeights(RecallConfig{
		WeightSimilarity: 2,
		WeightLexical:    1,
		WeightRecency:    1,
		WeightFrequency:  0,
	})
	if sim+lex+rec+freq < 0.9999 || sim+lex+rec+freq > 1.0001 {
		t.Fatalf("weights do not sum to 1: %v", sim+lex+rec+freq)
	}
	sim, lex, rec, freq = normalizedWeights(RecallConfig{})
	if sim != 0.6 || lex != 0.2 || rec != 0.1 || freq != 0.1 {
		t.Fatalf("default weights = %v %v %v %v", sim, lex, rec, freq)
	}
}

func TestSelectDiverseRankedPrefersCoverage(t *testing.T) {
	ranked := []rankedFact{
		{fact: makeFact(t, 1, "My favorite color is blue", 1, time.Hour), score: 0.90},
		{fact: makeFact(t, 2, "My favorite color is blue and green", 1, time.Hour), score: 0.88},
		{fact: makeFact(t, 3, "I live in Shanghai", 1, time.Hour), score: 0.70},
	}
	pure := selectDiverseRanked(ranked, 2, 1)
	if pure[0].fact.ID != 1 || pure[1].fact.ID != 2 {
		t.Fatalf("lambda=1 should be pure relevance: %#v", pure)
	}
	diverse := selectDiverseRanked(ranked, 2, 0.3)
	if diverse[0].fact.ID != 1 || diverse[1].fact.ID != 3 {
		t.Fatalf("MMR should avoid the near-duplicate: %#v", diverse)
	}
}

func TestMiddleOrderFactsKeepsStrongestAtEdges(t *testing.T) {
	facts := []Fact{
		{ID: 1, Score: 0.9},
		{ID: 2, Score: 0.8},
		{ID: 3, Score: 0.7},
		{ID: 4, Score: 0.6},
	}
	ordered := middleOrderFacts(facts)
	got := []int64{ordered[0].ID, ordered[1].ID, ordered[2].ID, ordered[3].ID}
	want := []int64{1, 3, 4, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("middle order = %v, want %v", got, want)
		}
	}
	if len(middleOrderFacts(facts[:2])) != 2 {
		t.Fatal("short fact lists should pass through")
	}
}

func TestFactsFromRankedPreservesScores(t *testing.T) {
	ranked := []rankedFact{{
		fact:       makeFact(t, 7, "My favorite color is blue", 2, time.Hour),
		score:      0.75,
		similarity: 0.5,
		lexical:    0.25,
	}}
	facts := factsFromRanked(ranked)
	if len(facts) != 1 || facts[0].ID != 7 || facts[0].Score != 0.75 || facts[0].LexicalScore != 0.25 {
		t.Fatalf("unexpected facts: %#v", facts)
	}
}
