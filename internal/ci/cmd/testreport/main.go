package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/ci/testreport"
)

func main() {
	out := flag.String("out", "test-report.json", "structured report destination")
	flag.Parse()
	report, err := testreport.Read(os.Stdin, os.Stderr, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-report: %v\n", err)
		os.Exit(2)
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-report: create %s: %v\n", *out, err)
		os.Exit(2)
	}
	encErr := json.NewEncoder(f).Encode(report)
	closeErr := f.Close()
	if encErr != nil || closeErr != nil {
		if encErr == nil {
			encErr = closeErr
		}
		fmt.Fprintf(os.Stderr, "test-report: write %s: %v\n", *out, encErr)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "test-report: wrote %s; %d passed, %d failed, %d skipped\n", *out, report.TestsPassed, report.TestsFailed, report.TestsSkipped)
	if len(report.Incomplete) > 0 {
		fmt.Fprintln(os.Stderr, "test-report: packages that did not finish:")
		for _, pkg := range report.Incomplete {
			fmt.Fprintf(os.Stderr, "  %s\n", pkg)
		}
	}
	limit := 10
	if len(report.Tests) < limit {
		limit = len(report.Tests)
	}
	if limit > 0 {
		fmt.Fprintln(os.Stderr, "test-report: slowest completed tests:")
		for _, test := range report.Tests[:limit] {
			fmt.Fprintf(os.Stderr, "  %.3fs %s %s\n", test.Seconds, test.Package, test.Name)
		}
	}
	if report.Truncated {
		fmt.Fprintln(os.Stderr, "test-report: the go test stream ended mid-line; the run did not finish")
		os.Exit(2)
	}
}
