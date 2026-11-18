package memori

import (
	"testing"
	"time"
)

func TestConfigEnvParsing(t *testing.T) {
	t.Setenv("MEMORI_RECALL_LIMIT", "9")
	t.Setenv("MEMORI_AUTO_INJECT", "false")
	t.Setenv("MEMORI_INJECT_LIMIT", "3")
	t.Setenv("MEMORI_OP_TIMEOUT", "2s")
	t.Setenv("MEMORI_LOG_LEVEL", "DEBUG")
	t.Setenv("MEMORI_EMBEDDING_PROVIDER", "openai")
	t.Setenv("MEMORI_EMBEDDING_MODEL", "text-embedding-3-large")
	t.Setenv("MEMORI_EMBEDDING_DIMENSION", "1536")
	t.Setenv("MEMORI_EXTRACTION_PROVIDER", "LLM")
	t.Setenv("MEMORI_EXTRACTION_MAX_FACTS", "7")
	t.Setenv("MEMORI_EXTRACTION_MERGE_THRESHOLD", "0.9")
	t.Setenv("MEMORI_EXTRACTION_SUMMARY", "off")
	t.Setenv("MEMORI_RECALL_SCAN_LIMIT", "500")
	t.Setenv("MEMORI_RECALL_MIN_SCORE", "0.2")
	t.Setenv("MEMORI_RECALL_WEIGHT_SIMILARITY", "0.5")
	t.Setenv("MEMORI_RECALL_RECENCY_HALF_LIFE", "48h")

	c := newConfig()
	if c.RecallLimit != 9 || c.AutoInject || c.InjectLimit != 3 {
		t.Fatalf("recall/autoinject config = %#v", c)
	}
	if c.OpTimeout != 2*time.Second || c.LogLevel != "debug" {
		t.Fatalf("timeout/log = %v %q", c.OpTimeout, c.LogLevel)
	}
	if c.Embedding.Provider != "openai" || c.Embedding.Model != "text-embedding-3-large" || c.Embedding.Dimension != 1536 {
		t.Fatalf("embedding config = %#v", c.Embedding)
	}
	if c.Extraction.Provider != "llm" || c.Extraction.MaxFacts != 7 || c.Extraction.MergeThreshold != 0.9 || c.Extraction.SummaryEnabled {
		t.Fatalf("extraction config = %#v", c.Extraction)
	}
	if c.Recall.ScanLimit != 500 || c.Recall.MinScore != 0.2 || c.Recall.WeightSimilarity != 0.5 || c.Recall.RecencyHalfLife != 48*time.Hour {
		t.Fatalf("recall config = %#v", c.Recall)
	}
}

func TestConfigInvalidEnvFallsBack(t *testing.T) {
	t.Setenv("MEMORI_RECALL_LIMIT", "not-a-number")
	t.Setenv("MEMORI_AUTO_INJECT", "maybe")
	t.Setenv("MEMORI_OP_TIMEOUT", "soon")
	c := newConfig()
	if c.RecallLimit != 5 || !c.AutoInject || c.OpTimeout != 20*time.Second {
		t.Fatalf("fallbacks not applied: %#v", c)
	}
}

func TestConfigSnapshotIsIndependent(t *testing.T) {
	c := newConfig()
	c.EntityID = "before"
	snap := c.snapshot()
	c.EntityID = "after"
	c.Recall.WeightSimilarity = 0.1
	if snap.EntityID != "before" || snap.Recall.WeightSimilarity == 0.1 {
		t.Fatalf("snapshot aliased config: %#v", snap)
	}
}
