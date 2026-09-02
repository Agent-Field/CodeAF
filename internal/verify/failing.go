package verify

// Reading a test runner's failures, and subtracting one reading from another.
//
// This is the half of the law that reads a reading. It is exported, and in its
// own package, so that every caller can reach it rather than one.
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
	// A failure banner naming a file and the chain of names inside it. The
	// shape is the whole of the match — a path, then " > ", then the names the
	// check is nested under — and it is here because a runner that prints its
	// failures this way prints them NOWHERE ELSE: vitest's default reporter
	// names its red checks in this banner and its green ones only per file, so
	// a reading of a failing suite through the old vocabulary named nothing at
	// all. Measured on the ofetch s5 run, whose five red checks were invisible.
	regexp.MustCompile(`(?m)^\s+FAIL\s+(\S+\s+>\s+.+?)\s*$`),
	// cargo test
	regexp.MustCompile(`(?m)^test\s+(\S+)\s+\.\.\.\s+FAILED`),
	// maven surefire / gradle. Gradle's line is `com.example.ApiTest >
	// testHeaders FAILED`, and both halves are captured so that CheckIdentity
	// can keep the second: the method is the check, the class is only where it
	// lives, and the roster in roster.go captures the same shape.
	regexp.MustCompile(`(?m)^\[ERROR\]\s+(\S+)\s+Time elapsed`),
	regexp.MustCompile(`(?m)^\s*(\S+\s+>\s+\S+)\s+FAILED\s*$`),
	// dotnet test / xunit. The name is FULLY QUALIFIED — that is the runner's
	// own grammar, and it is required rather than assumed, because `\S+` after
	// an English word matches an English word. ofetch's nemotron n1 run read
	// vitest's collection failure — `Failed to load url ./circuit-breaker …` —
	// as one red check named `to`, subtracted it against a baseline of 28, and
	// failed the delivery with `This work broke checks that were passing before
	// it: to.` A NAME COMES FROM THE RUNNER'S OWN TEST-RECORD GRAMMAR AND NEVER
	// FROM A SENTENCE.
	regexp.MustCompile(`(?m)^\s*(?:Failed|X)\s+([\w+]+(?:\.[\w+]+)+(?:\([^)]*\))?)(?:\s|$)`),
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
	// goCheckIdentity is go's own grammar for naming a check inside a package
	// and a subtest inside a check: `example.com/pkg.TestThing/the_empty_case`.
	// The capture is the declaration a source reader can see — the function's
	// own name — with the import path in front of it and the subtest behind it.
	goCheckIdentity = regexp.MustCompile(`(?:^|[./])((?:Test|Benchmark|Fuzz|Example)\w*)(?:/|$)`)
	// qualifiedTail is the last segment of a dotted qualification, and it is a
	// plain identifier or it is not a qualification at all: rspec names a red
	// example by `./spec/api_spec.rb:12`, whose final dot is a file extension
	// with a line number behind it.
	qualifiedTail = regexp.MustCompile(`\.(\w+)$`)
	// sourceFileSuffix is the dot that is NOT a qualifier. `tests/api_test.py`
	// is a file a runner named because a whole file failed to collect, and a
	// file is not a check: reducing it the way a qualified name reduces would
	// leave every such reading identified as `py`.
	sourceFileSuffix = regexp.MustCompile(`(?i)\.(?:py|go|ts|tsx|js|jsx|mjs|cjs|rb|java|kt|kts|cs|rs|php|swift|scala|c|cc|cpp|h|hpp|m|mm|ex|exs|sh)$`)
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

// normalizeTestName is the one cleaning every reader in this package puts a
// name through: the duration a runner prints after it, the whitespace it padded
// it with, and the summary lines that are not checks at all.
//
// It says what a name IS. CheckIdentity, below, says which checks two names are
// two readings of.
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

// CheckIdentity is WHICH CHECK A NAME NAMES, and it is the only place in this
// program that decides that.
//
// A CHECK HAS ONE IDENTITY. The same check is read twice by different readers —
// out of a runner's output by ReportedTests and FailingTests, out of its own
// source by DeclaredChecks — and those two readings are put together: the gate
// unions the checks a run DECLARED with the checks the suite REPORTED to ask
// what the change covers, and unions the declarations a diff REMOVED with the
// names a roster stopped reporting to ask what it deleted
// (internal/revision/acceptance.go). Where the two readers spell one check two
// ways it is counted twice in both unions — the same test arriving as
// `test_headers` and as `tests/api_test.py::test_headers` tells the gate a
// behaviour is covered twice, and names one deletion as two.
//
// The identity is THE BARE NAME, because it is the only spelling BOTH readers
// can reach. A declaration reader sees a `def`, a `func`, a `void` or an
// `it(...)`; it never sees the import path, the class, the node-id path, the
// describe() nesting or the subtest suffix a runner prints around it, and no
// amount of reading source recovers them. So every qualification a runner adds
// is stripped here, in this one function, and nowhere else.
//
// WHAT IS NOT DONE HERE, deliberately: a roster keeps the runner's own
// qualified spelling. A name is two things — an identity and a LOCATION — and
// SplitReplaced reads the location out of it to tell a check that was rewritten
// under its own describe path from one that was deleted, which is a distinction
// that cost happy-dom's s13 run four repair rounds. Reducing every roster to
// bare names would take that reading away. So the identity is derived where two
// readers meet (UniqueChecks) rather than imposed on the readers themselves.
func CheckIdentity(name string) string {
	clean := normalizeTestName(name)
	if clean == "" {
		return ""
	}
	return normalizeTestName(bareCheckName(clean))
}

// UniqueChecks is a run of check names, read by whichever readers named them,
// with one entry left per check.
//
// It is the union every caller of the two readers needs and none of them can
// spell with Subtract, which compares strings: `test_headers` and
// `tests/api_test.py::test_headers` are two strings and one check. The first
// spelling of a check wins, and the order is the caller's own — the same
// contract Subtract states, so a list that holds no duplicate identities comes
// back exactly as it went in.
func UniqueChecks(names []string) []string {
	seen := make(map[string]bool, len(names))
	var kept []string
	for _, name := range names {
		identity := CheckIdentity(name)
		if identity == "" {
			// A NAME WHOSE IDENTITY CANNOT BE READ IS ITS OWN IDENTITY. This
			// union may collapse two readings of one check and it may never
			// lose one: a check dropped here is a check the gate stops seeing,
			// which is the one direction nothing in this file may move in.
			identity = strings.TrimSpace(name)
		}
		if identity == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		kept = append(kept, name)
	}
	return kept
}

// bareCheckName strips the qualification a runner prints around a check's own
// name. It is the one step of CheckIdentity that is not cleaning, and it has no
// other caller: A CHECK HAS ONE IDENTITY means one function decides it.
func bareCheckName(name string) string {
	// A nesting chain — vitest's failure banner prints `file > describe > the
	// check`, gradle prints `com.example.ApiTest > testHeaders` — names the
	// check itself in its last segment. The segments in front of it are the
	// file and the headings, and a heading is not a check.
	if cut := strings.LastIndex(name, " > "); cut >= 0 {
		name = strings.TrimSpace(name[cut+len(" > "):])
	}
	// A parenthesised tail on an unspaced name is the runner saying where the
	// check lives — unittest's `test_headers (tests.api.ApiCase)`, surefire's
	// `testHeaders(com.example.ApiTest)` — or with what arguments it ran, as
	// xunit's `ApiTest.Works(n: 1)` does. A written description that happens to
	// end in a parenthesis keeps it: what stands in front of that one is prose,
	// not an identifier, and prose is the whole of the name its author gave.
	if open := strings.LastIndex(name, "("); open > 0 && strings.HasSuffix(name, ")") {
		if head := strings.TrimSpace(name[:open]); head != "" && !strings.ContainsAny(head, " \t") {
			name = head
		}
	}
	// Everything below reads identifier grammar. A name with a space in it is a
	// sentence somebody wrote — jest, mocha, rspec, TAP — and a sentence is
	// already bare: it is spelled in source exactly as the runner prints it.
	if strings.ContainsAny(name, " \t") {
		return name
	}
	// A node id — pytest's `tests/api_test.py::ApiCase::test_headers`, cargo's
	// `parser::tests::commas` — names the check after its last separator.
	if cut := strings.LastIndex(name, "::"); cut >= 0 {
		name = name[cut+len("::"):]
	}
	// Go names a check by its package and a subtest by its parent, and the
	// parent is the `func` a source reader can see.
	if match := goCheckIdentity.FindStringSubmatch(name); match != nil {
		return match[1]
	}
	// A dotted qualifier — xunit's `Ns.ApiCase.Works`, surefire's
	// `ApiTest.headers` — names the check in its last segment. Only the segment
	// after the last slash is read for it, because a slash means a path and a
	// path's dots belong to a file name rather than to a class.
	tail := name[strings.LastIndex(name, "/")+1:]
	if match := qualifiedTail.FindStringSubmatch(tail); match != nil &&
		!sourceFileSuffix.MatchString(tail) {
		return match[1]
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
