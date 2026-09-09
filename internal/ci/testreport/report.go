// Package testreport turns go test's JSON event stream into a durable timing report.
package testreport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

type Event struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

type Test struct {
	Package string  `json:"package"`
	Name    string  `json:"name"`
	Action  string  `json:"action"`
	Seconds float64 `json:"seconds"`
}

type Package struct {
	Name    string  `json:"name"`
	Action  string  `json:"action"`
	Seconds float64 `json:"seconds"`
	Cached  bool    `json:"cached,omitempty"`
}

type Report struct {
	Schema          int       `json:"schema"`
	GeneratedAt     time.Time `json:"generated_at"`
	Events          int       `json:"events"`
	TestsPassed     int       `json:"tests_passed"`
	TestsFailed     int       `json:"tests_failed"`
	TestsSkipped    int       `json:"tests_skipped"`
	PackageFailures int       `json:"package_failures"`
	Incomplete      []string  `json:"incomplete_packages,omitempty"`
	Packages        []Package `json:"packages"`
	Tests           []Test    `json:"tests"`
}

// Read consumes exactly one JSON object per line. It rejects empty and malformed
// streams because a timing artifact that silently omitted a compiler failure is worse
// than no artifact.
func Read(r io.Reader, progress io.Writer, now time.Time) (Report, error) {
	report := Report{Schema: 1, GeneratedAt: now.UTC(), Packages: []Package{}, Tests: []Test{}}
	started := map[string]bool{}
	finished := map[string]bool{}
	cached := map[string]bool{}
	s := bufio.NewScanner(r)
	// Build errors can contain generated lines much larger than Scanner's default.
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		var event Event
		if err := json.Unmarshal(s.Bytes(), &event); err != nil {
			return report, fmt.Errorf("go test JSON event %d: %w", report.Events+1, err)
		}
		report.Events++
		if event.Package != "" {
			started[event.Package] = true
		}
		if event.Output != "" && containsCached(event.Output) {
			cached[event.Package] = true
		}
		if event.Test != "" && terminal(event.Action) {
			t := Test{Package: event.Package, Name: event.Test, Action: event.Action, Seconds: event.Elapsed}
			report.Tests = append(report.Tests, t)
			switch event.Action {
			case "pass":
				report.TestsPassed++
			case "fail":
				report.TestsFailed++
			case "skip":
				report.TestsSkipped++
			}
			if event.Elapsed >= 1 {
				fmt.Fprintf(progress, "test-report: %s %s (%.3fs)\n", event.Action, event.Test, event.Elapsed)
			}
		}
		if event.Test == "" && terminal(event.Action) && event.Package != "" {
			finished[event.Package] = true
			report.Packages = append(report.Packages, Package{Name: event.Package, Action: event.Action, Seconds: event.Elapsed, Cached: cached[event.Package]})
			if event.Action == "fail" {
				report.PackageFailures++
			}
			fmt.Fprintf(progress, "test-report: package %s %s (%.3fs)\n", event.Package, event.Action, event.Elapsed)
		}
	}
	if err := s.Err(); err != nil {
		return report, fmt.Errorf("read go test JSON: %w", err)
	}
	if report.Events == 0 {
		return report, fmt.Errorf("go test produced no JSON events")
	}
	for pkg := range started {
		if !finished[pkg] {
			report.Incomplete = append(report.Incomplete, pkg)
		}
	}
	sort.Strings(report.Incomplete)
	sort.SliceStable(report.Tests, func(i, j int) bool { return report.Tests[i].Seconds > report.Tests[j].Seconds })
	return report, nil
}

func terminal(action string) bool { return action == "pass" || action == "fail" || action == "skip" }
func containsCached(s string) bool {
	for i := 0; i+8 <= len(s); i++ {
		if s[i:i+8] == "(cached)" {
			return true
		}
	}
	return false
}
