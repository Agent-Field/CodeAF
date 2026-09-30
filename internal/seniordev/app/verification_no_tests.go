//go:build !windows

package app

import (
	"regexp"
	"strings"
)

var (
	unitZeroTests  = regexp.MustCompile(`(?i)\bRan 0 tests?\b`)
	goNoTestFiles  = regexp.MustCompile(`(?m)^\?[ \t]+[^\n]+\[no test files\][ \t]*$`)
	goSomeTests    = regexp.MustCompile(`(?m)^(?:ok|FAIL)[ \t]+[^\n]+$`)
	cargoZeroTests = regexp.MustCompile(`(?i)\brunning 0 tests?\b`)
	cargoSomeTests = regexp.MustCompile(`(?i)\brunning [1-9][0-9]* tests?\b`)
	mavenZeroTests = regexp.MustCompile(`(?i)\bTests run: 0\b`)
	mavenSomeTests = regexp.MustCompile(`(?i)\bTests run: [1-9][0-9]*\b`)
)

// noTestsReported recognizes the empty-suite words of the runners discovery
// can choose, including wrappers such as make test. A mixed Go, unittest,
// Cargo, or Maven run with positive evidence is not called empty.
func noTestsReported(output string) bool {
	lower := strings.ToLower(output)
	switch {
	case unitZeroTests.MatchString(output):
		return true
	case strings.Contains(lower, "no tests ran"), strings.Contains(lower, "collected 0 items"):
		return true
	case strings.Contains(lower, "no tests found"), strings.Contains(lower, "no test files found"),
		strings.Contains(lower, "no test files matched"), strings.Contains(lower, "no test suites found"):
		return true
	case goNoTestFiles.MatchString(output) && !goSomeTests.MatchString(output):
		return true
	case cargoZeroTests.MatchString(output) && !cargoSomeTests.MatchString(output):
		return true
	case mavenZeroTests.MatchString(output) && !mavenSomeTests.MatchString(output):
		return true
	case strings.Contains(lower, "no test is available"), strings.Contains(lower, "no tests were found"):
		return true
	case strings.Contains(lower, "no tests to run"), strings.Contains(lower, ":test no-source"):
		return true
	}
	return false
}
