// Command memorigo-eval runs the deterministic Memorigo evaluation suite.
package main

import (
	"context"
	"encoding/json"
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

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := eval.Run(ctx, eval.Options{Scenarios: scenarios})
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
	if report.Failed > 0 {
		os.Exit(1)
	}
}
