package eval_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/eval"
)

func TestFixtureExternalSample(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	questions, err := eval.LoadLongMemEval("datasets/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 2 || questions[0].Answer != "blue" || !strings.HasSuffix(questions[1].ID, "_abs") {
		t.Fatalf("fixture = %+v", questions)
	}
	report, err := eval.RunExternal(ctx, questions, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Ran != 2 || report.Abstentions != 1 || report.Scored != 1 || report.AnswerHits != 1 {
		t.Fatalf("sample = %+v", report)
	}
	if report.JudgeScored != 0 {
		t.Fatalf("judge ran while disabled: %+v", report)
	}
	idle, err := eval.RunExternal(ctx, questions, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if idle.Ran != 0 || idle.Questions != 2 {
		t.Fatalf("limit 0 = %+v", idle)
	}
}

func TestExternalDatasetAbsent(t *testing.T) {
	_, err := eval.LoadLongMemEval("datasets/not-downloaded.json")
	if !errors.Is(err, eval.ErrDatasetAbsent) {
		t.Fatalf("err = %v", err)
	}
}

func TestNumericAnswerDecodes(t *testing.T) {
	raw := []byte(`[{"question_id":"n","question_type":"temporal-reasoning","question":"How many?","answer":3,"haystack_sessions":[]}]`)
	// LoadLongMemEval needs a file. decode is unexported, so round-trip through a temp file.
	path := t.TempDir() + "/numeric.json"
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	questions, err := eval.LoadLongMemEval(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 1 || questions[0].Answer != "3" {
		t.Fatalf("questions = %+v", questions)
	}
}

func TestChecksumPinnedInScript(t *testing.T) {
	script, err := os.ReadFile("datasets/download.sh")
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile("datasets/checksums.txt")
	if err != nil {
		t.Fatal(err)
	}
	const sum = "821a2034d219ab45846873dd14c14f12cfe7776e73527a483f9dac095d38620c"
	if !bytes.Contains(sums, []byte(sum+"  longmemeval_oracle.json")) {
		t.Fatalf("checksums.txt = %s", sums)
	}
	for _, needle := range []string{"checksums.txt", "longmemeval_oracle.json", "huggingface.co/datasets/xiaowu0162/longmemeval-cleaned"} {
		if !bytes.Contains(script, []byte(needle)) {
			t.Fatalf("download.sh missing %s", needle)
		}
	}
}

func TestOracleChecksumWhenPresent(t *testing.T) {
	path := "datasets/longmemeval_oracle.json"
	info, err := os.Stat(path)
	if err != nil {
		t.Skip("external dataset not present")
	}
	if info.Size() < 1000 {
		t.Fatalf("oracle file is unexpectedly small: %d", info.Size())
	}
	questions, err := eval.LoadLongMemEval(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 500 {
		t.Fatalf("oracle questions = %d", len(questions))
	}
}
