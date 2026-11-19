package eval_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/eval"
)

func TestBuiltinScenariosPass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := eval.Run(ctx, eval.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Failed != 0 {
		t.Fatalf("builtin scenarios failed:\n%s", report)
	}
	if report.Passed < len(eval.Builtin()) {
		t.Fatalf("expected at least one assertion per scenario, got %d", report.Passed)
	}
}

func TestOldFactFallsOutOfFullContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var selected []eval.Scenario
	for _, sc := range eval.Builtin() {
		if sc.Name == "old-fact-budget" {
			selected = append(selected, sc)
		}
	}
	report, err := eval.Run(ctx, eval.Options{Scenarios: selected})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || report.Passed != 1 {
		t.Fatalf("memory assertion:\n%s", report)
	}
	if report.BaselineQueries != 1 || report.FullContextHits != 0 || report.NaiveRAGHits != 1 {
		t.Fatalf("baselines = full %d/%d naive %d/%d\n%s",
			report.FullContextHits, report.BaselineQueries, report.NaiveRAGHits, report.BaselineQueries, report)
	}
}

func TestReportJSON(t *testing.T) {
	report := eval.Report{Passed: 1}
	var b strings.Builder
	if err := report.WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"passed": 1`) {
		t.Fatalf("json = %s", b.String())
	}
}
