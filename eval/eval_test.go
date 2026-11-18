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
