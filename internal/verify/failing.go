package verify

// Reading a test runner's failures, and subtracting one reading from another.
//
// This is the half of the law that used to live unexported inside
// internal/swepro/codeaf/baseline.go, where exactly one worker could reach it.
// Nothing here knows what language the workspace is in. It does not run the
// tests — the caller already knows how to do that — it only reads the runners'
// own failure vocabulary, which is small, stable, and shared across every
// ecosystem this program has met.

import (
	"regexp"
	"sort"
	"strings"
)

// failingTestPatterns is the failure vocabulary of the runners this program
// meets, each pattern capturing one identity that is stable between two runs of
// the same suite: no durations, no line numbers that move, no counts.
//
// A pattern that over-matches is safe in one direction only, and that is the
// direction it is written for: a phantom name read out of BOTH runs cancels,
// and a phantom read out of the AFTER run alone is scored as a new failure,
// which fails the run. Nothing here can turn a real regression green.
var failingTestPatterns = []*regexp.Regexp{
	// go test
	regexp.MustCompile(`(?m)^\s*--- FAIL:\s+([^\s(]+)`),
	regexp.MustCompile(`(?m)^FAIL\s+(\S+)\s`),
	// pytest
	regexp.MustCompile(`(?m)^(?:FAILED|ERROR)\s+(\S+::\S+)`),
	regexp.MustCompile(`(?m)^(?:FAILED|ERROR)\s+(\S+\.py)\s*$`),
	// python unittest
	regexp.MustCompile(`(?m)^(?:FAIL|ERROR):\s+([\w.]+\s*\([\w.]+\))`),
	// jest / vitest / mocha
	regexp.MustCompile(`(?m)^\s*[✕✗×]\s+(.+?)\s*$`),
	regexp.MustCompile(`(?m)^\s*●\s+(.+?)\s*$`),
	// cargo test
	regexp.MustCompile(`(?m)^test\s+(\S+)\s+\.\.\.\s+FAILED`),
	// maven surefire / gradle
	regexp.MustCompile(`(?m)^\[ERROR\]\s+(\S+)\s+Time elapsed`),
	regexp.MustCompile(`(?m)^\s*(\S+)\s+>\s+\S+\s+FAILED\s*$`),
	// dotnet test / xunit
	regexp.MustCompile(`(?m)^\s*(?:Failed|X)\s+(\S+)\s`),
	// rspec
	regexp.MustCompile(`(?m)^rspec\s+(\./\S+:\d+)`),
	// ctest
	regexp.MustCompile(`(?m)^\s*\d+\s+-\s+(\S+)\s+\(Failed\)`),
	// TAP
	regexp.MustCompile(`(?m)^not ok\s+\d+\s+-?\s*(.+?)\s*$`),
}

var (
	ansiEscape     = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	trailingTiming = regexp.MustCompile(`\s*[\(\[]\s*[\d.,]+\s*(?:ms|s|sec|secs|seconds)?\s*[\)\]]\s*$`)
	digitRun       = regexp.MustCompile(`\d+`)
	spaceRun       = regexp.MustCompile(`\s+`)
)

// FailingTests reads every test identity a runner named as failing, sorted and
// deduplicated so two runs of one suite compare as sets rather than as
// transcripts.
func FailingTests(output string) []string {
	clean := ansiEscape.ReplaceAllString(output, "")
	seen := map[string]bool{}
	var names []string
	for _, pattern := range failingTestPatterns {
		for _, match := range pattern.FindAllStringSubmatch(clean, -1) {
			name := normalizeTestName(match[1])
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func normalizeTestName(raw string) string {
	name := strings.TrimSpace(trailingTiming.ReplaceAllString(strings.TrimSpace(raw), ""))
	name = spaceRun.ReplaceAllString(name, " ")
	name = strings.Trim(name, ":.,")
	// A "name" that is a count, a bare verb or a punctuation run is a false
	// read of a summary line, and carrying it would make two identical runs
	// disagree with each other.
	if len(name) < 2 || len(name) > 200 {
		return ""
	}
	if digitRun.ReplaceAllString(name, "") == "" {
		return ""
	}
	switch strings.ToLower(name) {
	case "console", "failures", "failed", "error", "errors", "test", "tests":
		return ""
	}
	return name
}

// NewFailures names the checks that were green before this work and are red
// after it.
//
// It is set subtraction and nothing else: order-stable in `after`'s own order,
// deduped, and pure. A repository that arrives already red is the repository's
// problem — a correct one-line fix to spf13/cobra was thrown away once because
// a gate read a suite's ABSOLUTE state as a verdict on the change, and the 2 in
// `make all exited 2` came from a test that had been failing before the harness
// ever opened the directory. Red before and red after subtracts to nothing. Red
// only after is the change's doing, and it is the one signal a leaf's own new
// tests cannot carry, because the leaf wrote them.
//
// WHAT THIS FUNCTION DELIBERATELY DOES NOT DECIDE: a red result with NO
// parseable names is never acquitted by a baseline that also had no names. Two
// empty readings subtract to nothing here, which is arithmetic, not an
// acquittal — "the whole suite was red before, so its being red now proves
// nothing" is the broadest possible acquittal and it is exactly wrong on the
// run whose whole job was to turn that red suite green. The CALLER weighs the
// exit statuses and decides; this function only subtracts.
func NewFailures(before, after []string) []string { return Subtract(after, before) }

// Subtract is that arithmetic with the meaning left out: the names in `from`
// that `remove` does not hold, order-stable in `from`'s own order, deduped, and
// pure.
//
// It is exported and separate because three questions in this system turn out to
// be one subtraction — which checks this work turned red, which checks stopped
// being reported, and which check declarations a diff only takes away — and
// three copies of a four-line loop is how they come to disagree about the empty
// case. Every caller states the meaning; this states none.
func Subtract(from, remove []string) []string {
	known := make(map[string]bool, len(remove))
	for _, name := range remove {
		known[name] = true
	}
	seen := make(map[string]bool, len(from))
	var rest []string
	for _, name := range from {
		if known[name] || seen[name] {
			continue
		}
		seen[name] = true
		rest = append(rest, name)
	}
	return rest
}
