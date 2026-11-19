package eval_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/eval"
)

type fixedJudge struct {
	score float64
	err   error
	calls int
}

func (f *fixedJudge) Score(context.Context, eval.JudgeInput) (eval.JudgeResult, error) {
	f.calls++
	if f.err != nil {
		return eval.JudgeResult{}, f.err
	}
	return eval.JudgeResult{Score: f.score}, nil
}

func TestJudgeDoesNotChangePassFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	judge := &fixedJudge{score: 0}
	var selected []eval.Scenario
	for _, sc := range eval.Builtin() {
		if sc.Name == "basic-preference" {
			selected = append(selected, sc)
		}
	}
	report, err := eval.Run(ctx, eval.Options{Scenarios: selected, Judge: judge})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || report.Passed == 0 || report.JudgeScored != report.Passed || judge.calls == 0 {
		t.Fatalf("report = %+v calls %d", report, judge.calls)
	}
	for _, result := range report.Results {
		if !result.Passed || result.JudgeScore == nil || *result.JudgeScore != 0 {
			t.Fatalf("result = %+v", result)
		}
	}
	if !strings.Contains(report.String(), "does not change pass/fail") {
		t.Fatalf("report text missing judge line:\n%s", report)
	}
}

func TestJudgeHTTPErrorDoesNotFailSuite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "favorite color") {
			t.Errorf("body = %s", body)
		}
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var selected []eval.Scenario
	for _, sc := range eval.Builtin() {
		if sc.Name == "basic-preference" {
			selected = append(selected, sc)
		}
	}
	report, err := eval.Run(ctx, eval.Options{
		Scenarios: selected,
		Judge:     eval.NewHTTPJudge(srv.URL, "secret", "test-model"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || report.JudgeErrors == 0 || report.JudgeScored != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestJudgeFromEnvDefaultOff(t *testing.T) {
	t.Setenv("MEMORI_EVAL_JUDGE", "")
	judge, err := eval.JudgeFromEnv()
	if err != nil || judge != nil {
		t.Fatalf("judge = %v err %v", judge, err)
	}
	t.Setenv("MEMORI_EVAL_JUDGE", "off")
	judge, err = eval.JudgeFromEnv()
	if err != nil || judge != nil {
		t.Fatalf("off judge = %v err %v", judge, err)
	}
	t.Setenv("MEMORI_EVAL_JUDGE", "http")
	t.Setenv("MEMORI_EVAL_JUDGE_URL", "")
	if _, err := eval.JudgeFromEnv(); err == nil {
		t.Fatal("expected URL error")
	}
	t.Setenv("MEMORI_EVAL_JUDGE_URL", "http://127.0.0.1:9/judge")
	judge, err = eval.JudgeFromEnv()
	if err != nil || judge == nil {
		t.Fatalf("http judge = %v err %v", judge, err)
	}
}
