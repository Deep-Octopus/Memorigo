// Command memorigo-eval runs the deterministic Memorigo evaluation suite.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Deep-Octopus/Memorigo/eval"
	"github.com/Deep-Octopus/Memorigo/internal/version"
)

func main() {
	filter := flag.String("filter", "", "only run scenarios whose name contains this string")
	asJSON := flag.Bool("json", false, "emit JSON instead of a table")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall timeout")
	showVersion := flag.Bool("version", false, "print version and exit")
	external := flag.String("external", eval.DefaultExternalPath, "LongMemEval JSON path; a missing file does not fail the builtin suite")
	externalLimit := flag.Int("external-limit", 0, "sample at most this many external questions; 0 does not score them")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}

	scenarios := eval.Builtin()
	if *filter != "" {
		var selected []eval.Scenario
		for _, sc := range scenarios {
			if strings.Contains(strings.ToLower(sc.Name), strings.ToLower(*filter)) {
				selected = append(selected, sc)
			}
		}
		if len(selected) == 0 {
			fmt.Fprintf(os.Stderr, "no scenario matches %q\n", *filter)
			os.Exit(2)
		}
		scenarios = selected
	}

	judge, err := eval.JudgeFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := eval.Run(ctx, eval.Options{Scenarios: scenarios, Judge: judge})
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval failed:", err)
		os.Exit(2)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Print(report)
	}
	limit := *externalLimit
	if report.Failed > 0 {
		limit = 0
	}
	extCode := reportExternal(judge, *external, limit)
	if report.Failed > 0 {
		os.Exit(1)
	}
	if extCode != 0 {
		os.Exit(extCode)
	}
}

func reportExternal(judge eval.Judge, path string, limit int) int {
	questions, err := eval.LoadLongMemEval(path)
	if errors.Is(err, eval.ErrDatasetAbsent) {
		fmt.Fprintf(os.Stderr, "external dataset not present: %s\n", path)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if limit <= 0 {
		fmt.Fprintf(os.Stderr, "external dataset present: %d questions, not scored (pass -external-limit to sample; this is not a LongMemEval score)\n", len(questions))
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	sample, err := eval.RunExternal(ctx, questions, limit, judge)
	if err != nil {
		fmt.Fprintln(os.Stderr, "external sample failed:", err)
		return 2
	}
	fmt.Fprintln(os.Stderr, sample)
	return 0
}
