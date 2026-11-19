// Package eval provides a deterministic, self-contained evaluation harness for
// Memorigo. It is intentionally dependency-free: scenarios run against an
// in-memory SQLite database with the offline hash embedder, so CI can execute
// them without API keys or downloaded datasets.
package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Deep-Octopus/Memorigo/memori"
)

// QueryCase is one assertion against recall or context assembly.
type QueryCase struct {
	Query   string
	Limit   int
	Want    []string
	Avoid   []string
	Context *ContextCheck
	// BaselineBudget caps the full-context and naive-RAG packs for this
	// query. Zero uses the context budget, or 800 tokens.
	BaselineBudget int
	// ExpectEmpty asserts that recall returns no facts (e.g. after Forget).
	ExpectEmpty bool
}

// ContextCheck asserts on BuildContext output. When set, ContextCheck replaces
// the plain Recall assertion for this query.
type ContextCheck struct {
	Budget       int
	FactLimit    int
	IncludeRaw   bool
	RawLimit     int
	WantText     []string
	WantKinds    []string
	MaxTokens    int
	WantProfile  []string
	AvoidProfile []string

	IncludeProfile        bool
	ProfileBudget         int
	ProfileLimit          int
	IncludeDynamicProfile bool
	MMRLambda             float64
	DisableDiversity      bool
	RawBudgetShare        float64
	DisableMiddleOrder    bool
	MinCoverage           float64
	WantInsufficient      bool
}

// Scenario seeds a Memori instance and then runs query assertions.
type Scenario struct {
	Name         string
	Description  string
	NoExtraction bool
	Seed         func(ctx context.Context, m *memori.Memori) error
	Queries      []QueryCase
}

// QueryResult is the outcome of one query assertion.
type QueryResult struct {
	Scenario     string        `json:"scenario"`
	Query        string        `json:"query"`
	Passed       bool          `json:"passed"`
	Reason       string        `json:"reason,omitempty"`
	Latency      time.Duration `json:"latency_ns"`
	Facts        int           `json:"facts"`
	Profile      int           `json:"profile,omitempty"`
	Tokens       int           `json:"tokens,omitempty"`
	Coverage     float64       `json:"coverage,omitempty"`
	Insufficient bool          `json:"insufficient,omitempty"`
	Truncated    bool          `json:"truncated,omitempty"`
	// BaselineCompared reports a want-string check against a recent-window
	// full-context pack and a hash-cosine naive RAG pack. It is not an
	// LLM-judge score and it does not change Passed.
	BaselineCompared bool `json:"baseline_compared,omitempty"`
	FullContextHit   bool `json:"full_context_hit,omitempty"`
	NaiveRAGHit      bool `json:"naive_rag_hit,omitempty"`
	// JudgeScore is set only when Options.Judge is configured. It does not
	// change Passed.
	JudgeScore *float64 `json:"judge_score,omitempty"`
	JudgeError string   `json:"judge_error,omitempty"`
}

// Report aggregates scenario results.
type Report struct {
	Results         []QueryResult `json:"results"`
	Passed          int           `json:"passed"`
	Failed          int           `json:"failed"`
	AvgLatency      time.Duration `json:"avg_latency_ns"`
	P95Latency      time.Duration `json:"p95_latency_ns"`
	AvgTokens       int           `json:"avg_tokens"`
	Duration        time.Duration `json:"duration_ns"`
	BaselineQueries int           `json:"baseline_queries,omitempty"`
	FullContextHits int           `json:"full_context_hits,omitempty"`
	NaiveRAGHits    int           `json:"naive_rag_hits,omitempty"`
	JudgeScored     int           `json:"judge_scored,omitempty"`
	JudgeErrors     int           `json:"judge_errors,omitempty"`
}

// Options configures a run.
type Options struct {
	Scenarios []Scenario
	// Judge, when set, scores each query after the assertion. It does not
	// change Passed. Nil keeps the run offline.
	Judge Judge
}

var runCounter atomic.Int64

// Run executes every scenario in an isolated in-memory SQLite database.
func Run(ctx context.Context, opts Options) (Report, error) {
	scenarios := opts.Scenarios
	if len(scenarios) == 0 {
		scenarios = Builtin()
	}
	started := time.Now()
	report := Report{Results: make([]QueryResult, 0, len(scenarios)*2)}

	for _, sc := range scenarios {
		results, err := runScenario(ctx, sc, opts.Judge)
		if err != nil {
			return report, fmt.Errorf("scenario %s: %w", sc.Name, err)
		}
		report.Results = append(report.Results, results...)
	}

	var totalLatency time.Duration
	var totalTokens int
	var latencies []time.Duration
	for _, r := range report.Results {
		if r.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
		totalLatency += r.Latency
		latencies = append(latencies, r.Latency)
		totalTokens += r.Tokens
		if r.BaselineCompared {
			report.BaselineQueries++
			if r.FullContextHit {
				report.FullContextHits++
			}
			if r.NaiveRAGHit {
				report.NaiveRAGHits++
			}
		}
		if r.JudgeScore != nil {
			report.JudgeScored++
		}
		if r.JudgeError != "" {
			report.JudgeErrors++
		}
	}
	if len(report.Results) > 0 {
		report.AvgLatency = totalLatency / time.Duration(len(report.Results))
		report.AvgTokens = totalTokens / len(report.Results)
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		idx := int(float64(len(latencies))*0.95) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(latencies) {
			idx = len(latencies) - 1
		}
		report.P95Latency = latencies[idx]
	}
	report.Duration = time.Since(started)
	return report, nil
}

func runScenario(ctx context.Context, sc Scenario, judge Judge) ([]QueryResult, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:eval_%s_%d?mode=memory&cache=shared",
		sanitize(sc.Name), runCounter.Add(1)))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	m := memori.New(
		memori.WithStorageConn(db),
		memori.WithConfig(func(c *memori.Config) {
			c.LogLevel = "off"
			c.AutoInject = false
			c.Embedding.Provider = "hash"
			c.Profile.Budget = 200
			c.Profile.Limit = 40
			c.Profile.IncludeDynamic = false
			if sc.NoExtraction {
				c.Extraction.Provider = "off"
			} else {
				c.Extraction.Provider = "heuristic"
			}
		}),
	)
	defer m.Close()
	if err := m.Err(); err != nil {
		return nil, err
	}
	if err := m.TryAttribution("eval-user", "eval-process"); err != nil {
		return nil, err
	}
	if sc.Seed != nil {
		if err := sc.Seed(ctx, m); err != nil {
			return nil, err
		}
	}
	if err := m.Flush(ctx); err != nil {
		return nil, err
	}

	results := make([]QueryResult, 0, len(sc.Queries))
	for _, q := range sc.Queries {
		start := time.Now()
		result := QueryResult{Scenario: sc.Name, Query: q.Query}
		var observed string
		if q.Context != nil {
			observed = checkContext(ctx, m, q, &result)
		} else {
			observed = checkRecall(ctx, m, q, &result)
		}
		result.Latency = time.Since(start)
		applyJudge(ctx, judge, q, observed, &result)
		results = append(results, result)
	}
	if err := annotateBaselines(ctx, m, sc.Queries, results); err != nil {
		return nil, err
	}
	return results, nil
}

func checkRecall(ctx context.Context, m *memori.Memori, q QueryCase, result *QueryResult) string {
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	facts, err := m.Recall(q.Query, limit)
	if err != nil {
		result.Reason = "recall error: " + err.Error()
		return ""
	}
	result.Facts = len(facts)
	text := joinFacts(facts)
	if q.ExpectEmpty {
		if len(facts) != 0 {
			result.Reason = fmt.Sprintf("expected no facts, got %d: %q", len(facts), text)
			return text
		}
		result.Passed = true
		return text
	}
	reason := assertContains(text, q.Want) + assertAvoids(text, q.Avoid)
	if reason != "" {
		result.Reason = reason
		return text
	}
	result.Passed = true
	return text
}

func checkContext(ctx context.Context, m *memori.Memori, q QueryCase, result *QueryResult) string {
	check := q.Context
	pkg, err := m.BuildContext(ctx, q.Query, memori.ContextOptions{
		TokenBudget:           check.Budget,
		FactLimit:             check.FactLimit,
		IncludeRaw:            check.IncludeRaw,
		RawLimit:              check.RawLimit,
		IncludeProfile:        check.IncludeProfile,
		ProfileBudget:         check.ProfileBudget,
		ProfileLimit:          check.ProfileLimit,
		IncludeDynamicProfile: check.IncludeDynamicProfile,
		MMRLambda:             check.MMRLambda,
		DisableDiversity:      check.DisableDiversity,
		RawBudgetShare:        check.RawBudgetShare,
		DisableMiddleOrder:    check.DisableMiddleOrder,
		MinCoverage:           check.MinCoverage,
	})
	if err != nil {
		result.Reason = "context error: " + err.Error()
		return ""
	}
	result.Facts = len(pkg.Facts)
	result.Profile = pkg.ProfileCount
	result.Tokens = pkg.Tokens
	result.Coverage = pkg.Coverage
	result.Insufficient = pkg.InsufficientEvidence
	result.Truncated = pkg.Truncated
	if pkg.Tokens > pkg.Budget {
		result.Reason = fmt.Sprintf("tokens %d exceed budget %d", pkg.Tokens, pkg.Budget)
		return pkg.Text
	}
	if check.MaxTokens > 0 && pkg.Tokens > check.MaxTokens {
		result.Reason = fmt.Sprintf("tokens %d exceed max %d", pkg.Tokens, check.MaxTokens)
		return pkg.Text
	}
	if check.WantInsufficient && !pkg.InsufficientEvidence {
		result.Reason = fmt.Sprintf("expected insufficient evidence, coverage=%.2f", pkg.Coverage)
		return pkg.Text
	}
	reason := assertContains(pkg.Text, check.WantText)
	reason += assertContains(pkg.ProfileText, check.WantProfile)
	reason += assertAvoids(pkg.ProfileText, check.AvoidProfile)
	for _, kind := range check.WantKinds {
		found := false
		for _, c := range pkg.Citations {
			if c.Kind == kind {
				found = true
				break
			}
		}
		if !found {
			reason += fmt.Sprintf("missing citation kind %q; ", kind)
		}
	}
	if reason != "" {
		result.Reason = reason
		return pkg.Text
	}
	result.Passed = true
	return pkg.Text
}

func assertContains(text string, wants []string) string {
	for _, want := range wants {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			return fmt.Sprintf("missing %q in %q; ", want, text)
		}
	}
	return ""
}

func assertAvoids(text string, avoids []string) string {
	for _, avoid := range avoids {
		if strings.Contains(strings.ToLower(text), strings.ToLower(avoid)) {
			return fmt.Sprintf("forbidden %q present in %q; ", avoid, text)
		}
	}
	return ""
}

func joinFacts(facts []memori.Fact) string {
	parts := make([]string, 0, len(facts))
	for _, f := range facts {
		parts = append(parts, f.Content)
	}
	return strings.Join(parts, "\n")
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// String renders a human-readable report.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-26s %-28s %-6s %-5s %-8s %s\n", "SCENARIO", "QUERY", "RESULT", "FACTS", "TOKENS", "LATENCY")
	for _, res := range r.Results {
		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "%-26s %-28s %-6s %-5d %-8d %s\n",
			truncate(res.Scenario, 26), truncate(res.Query, 28), status, res.Facts, res.Tokens, res.Latency.Round(time.Microsecond))
		if !res.Passed && res.Reason != "" {
			fmt.Fprintf(&b, "  reason: %s\n", res.Reason)
		}
	}
	total := r.Passed + r.Failed
	accuracy := 0.0
	if total > 0 {
		accuracy = float64(r.Passed) / float64(total) * 100
	}
	fmt.Fprintf(&b, "\naccuracy: %d/%d (%.1f%%)  avg latency: %s  p95: %s  avg tokens: %d  duration: %s\n",
		r.Passed, total, accuracy, r.AvgLatency.Round(time.Microsecond),
		r.P95Latency.Round(time.Microsecond), r.AvgTokens, r.Duration.Round(time.Millisecond))
	if r.BaselineQueries > 0 {
		fmt.Fprintf(&b, "baselines (want-string hit rate, hash embedder, no LLM judge): full-context %d/%d, naive-rag %d/%d\n",
			r.FullContextHits, r.BaselineQueries, r.NaiveRAGHits, r.BaselineQueries)
	}
	if r.JudgeScored > 0 || r.JudgeErrors > 0 {
		fmt.Fprintf(&b, "llm-judge (opt-in, does not change pass/fail): scored %d, errors %d\n", r.JudgeScored, r.JudgeErrors)
	}
	return b.String()
}

// WriteJSON writes a machine-readable report.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
