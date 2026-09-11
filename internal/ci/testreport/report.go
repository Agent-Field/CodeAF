// Package testreport turns go test's JSON event stream into a durable timing report.
package testreport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
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
	Truncated       bool      `json:"truncated,omitempty"`
	Packages        []Package `json:"packages"`
	Tests           []Test    `json:"tests"`
}

// Read consumes exactly one JSON object per line. It rejects empty and malformed
// streams because a timing artifact that silently omitted a compiler failure is worse
// than no artifact.
func Read(r io.Reader, progress io.Writer, now time.Time) (Report, error) {
	// Schema 2 adds truncated and narrows cached to Go's own package summary,
	// because readers must be able to distinguish both changes from schema 1.
	report := Report{Schema: 2, GeneratedAt: now.UTC(), Packages: []Package{}, Tests: []Test{}}
	started := map[string]bool{}
	finished := map[string]bool{}
	cached := map[string]bool{}
	// ReadString distinguishes a final cut line from a complete event and removes
	// Scanner's old 4 MB ceiling, which a long generated build error could cross.
	reader := bufio.NewReader(r)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if readErr == io.EOF {
				// A LAST LINE WITH NO NEWLINE IS A RUN THAT WAS CUT, NOT A BROKEN ONE.
				// go test writes one whole event per line, so anything left over when
				// the pipe closes is half an event somebody killed. Trailing whitespace
				// is not half an event and must not be read as one.
				if strings.TrimSpace(line) != "" {
					report.Truncated = true
				}
				break
			}
			return report, fmt.Errorf("read go test JSON: %w", readErr)
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return report, fmt.Errorf("go test JSON event %d: %w", report.Events+1, err)
		}
		report.Events++
		if event.Package != "" {
			started[event.Package] = true
		}
		if summarySaysCached(event) {
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

// GO ITSELF IS THE ONLY WITNESS TO THE BUILD CACHE. A package finishes with the
// summary line `ok  <package>  (cached)`, and that line is the whole evidence; the
// same characters inside a test's own log say nothing about whether anything ran.
// Scanning every output event marked a package that had just executed under
// -count=1 as cached, which is the kind of lie this package exists not to tell (#735).
func summarySaysCached(event Event) bool {
	if event.Test != "" || event.Package == "" {
		return false
	}
	fields := strings.Fields(event.Output)
	return len(fields) >= 3 && fields[0] == "ok" && fields[1] == event.Package && fields[2] == "(cached)"
}
