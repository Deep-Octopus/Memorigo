package memori_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Deep-Octopus/Memorigo/embed"
	"github.com/Deep-Octopus/Memorigo/memori"
)

type preferReranker struct {
	needle string
	calls  int
	fail   error
}

func (p *preferReranker) Rerank(_ context.Context, _ string, documents []string) ([]float64, error) {
	p.calls++
	if p.fail != nil {
		return nil, p.fail
	}
	out := make([]float64, len(documents))
	for i, doc := range documents {
		if strings.Contains(strings.ToLower(doc), p.needle) {
			out[i] = 0.99
			continue
		}
		out[i] = 0.01
	}
	return out, nil
}

func TestRerankReordersRecallAndCanBeSkipped(t *testing.T) {
	reranker := &preferReranker{needle: "stamps"}
	m := newTestMemori(t,
		memori.WithReranker(reranker),
		memori.WithConfig(func(c *memori.Config) {
			c.Recall.RerankTopN = 5
			c.Recall.MinScore = 0
		}),
	)
	m.Attribution("user", "proc")
	if _, err := m.Remember("I like jazz records"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I collect stamps from Portugal"); err != nil {
		t.Fatal(err)
	}

	facts, err := m.Recall("records stamps", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) < 2 || !strings.Contains(facts[0].Content, "stamps") {
		t.Fatalf("rerank order = %#v", facts)
	}
	if facts[0].Score <= facts[1].Score {
		t.Fatalf("rerank scores are not ordered: %#v", facts)
	}
	if reranker.calls != 1 {
		t.Fatalf("calls = %d", reranker.calls)
	}

	reranker.fail = context.Canceled
	facts, err = m.Recall("records stamps", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 || !strings.Contains(strings.ToLower(facts[0].Content), "record") && !strings.Contains(strings.ToLower(facts[0].Content), "stamp") {
		t.Fatalf("fallback recall = %#v", facts)
	}
}

func TestRerankTopNZeroDoesNotCall(t *testing.T) {
	reranker := &preferReranker{needle: "stamps"}
	m := newTestMemori(t, memori.WithReranker(reranker))
	m.Attribution("user", "proc")
	if _, err := m.Remember("I like jazz records"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I collect stamps from Portugal"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recall("records stamps", 2); err != nil {
		t.Fatal(err)
	}
	if reranker.calls != 0 {
		t.Fatalf("calls = %d", reranker.calls)
	}
}

func TestRerankConfigDefaultsOff(t *testing.T) {
	m := memori.New()
	if m.Config.Rerank.Provider != "off" || m.Config.Recall.RerankTopN != 0 || m.Reranker != nil {
		t.Fatalf("rerank default = %#v top %d reranker %#v", m.Config.Rerank, m.Config.Recall.RerankTopN, m.Reranker)
	}
}

func TestRerankHTTPConfigInstallsClient(t *testing.T) {
	t.Setenv("MEMORI_RERANK", "HTTP")
	t.Setenv("MEMORI_RERANK_URL", "http://127.0.0.1:9/rerank")
	t.Setenv("MEMORI_RERANK_MODEL", "bge-reranker-v2-m3")
	m := memori.New()
	if m.Config.Rerank.Provider != "http" || m.Config.Recall.RerankTopN != 20 || m.Reranker == nil {
		t.Fatalf("provider %q top %d reranker %#v", m.Config.Rerank.Provider, m.Config.Recall.RerankTopN, m.Reranker)
	}
	if _, ok := m.Reranker.(*embed.HTTPReranker); !ok {
		t.Fatalf("reranker type = %T", m.Reranker)
	}
}
