package verify

// Reading a suite's whole roster, and reading a diff for the checks it declares.
//
// failing.go answers "what is red". This answers the two questions the delivery
// gate needs that redness cannot: WHAT CHECKS EXIST AT ALL, and WHICH CHECKS
// THIS CHANGE ADDED OR REMOVED. Both were unanswerable, and the cost of that is
// measured: two graded runs shipped at exit 0 on deliverables claiming every
// test passed, over hidden failures in behaviours the request stated and the
// leaf's own test file never exercised (docs/design/gate/ACCEPTANCE.md).
//
// Nothing here knows what language the workspace is in, and nothing here is a
// gate. A check is recognised by SHAPE — the punctuation and keywords a check
// declaration is built out of in every ecosystem this program has met — never by
// a list of frameworks, which is the failure FAILSAFE's first clause is about.

import (
	"regexp"
	"sort"
	"strings"
)

// passingTestPatterns is the other half of the vocabulary failingTestPatterns
// spells: the lines a runner prints for a check that held. Together they are the
// roster — every identity the runner named, whichever way it went.
//
// The over-matching direction is the safe one here too, and it is the opposite
// of failing.go's. A phantom name read out of both rosters cancels; a phantom
// read out of the BEFORE roster alone is scored as a check that disappeared,
// which raises a finding and buys a repair round rather than passing anything.
// Nothing here can make a missing check look present.
var passingTestPatterns = []*regexp.Regexp{
	// go test
	regexp.MustCompile(`(?m)^\s*--- PASS:\s+([^\s(]+)`),
	regexp.MustCompile(`(?m)^ok\s+(\S+)\s`),
	// pytest, in both orders its reporters print
	regexp.MustCompile(`(?m)^PASSED\s+(\S+::\S+)`),
	regexp.MustCompile(`(?m)^(\S+::\S+)\s+PASSED`),
	// python unittest
	regexp.MustCompile(`(?m)^ok:\s+([\w.]+\s*\([\w.]+\))`),
	// jest / vitest / mocha
	regexp.MustCompile(`(?m)^\s*[✓√]\s+(.+?)\s*$`),
	// cargo test
	regexp.MustCompile(`(?m)^test\s+(\S+)\s+\.\.\.\s+ok\s*$`),
	// gradle
	regexp.MustCompile(`(?m)^\s*(\S+)\s+>\s+\S+\s+PASSED\s*$`),
	// dotnet test / xunit, whose names are fully qualified. Required rather than
	// assumed, for the reason failingTestPatterns states at its own copy of this
	// line: after an English word, `\S+` matches an English word.
	regexp.MustCompile(`(?m)^\s*Passed\s+([\w+]+(?:\.[\w+]+)+(?:\([^)]*\))?)(?:\s|$)`),
	// ctest
	regexp.MustCompile(`(?m)^\s*\d+\s+-\s+(\S+)\s+\(Passed\)`),
	// TAP
	regexp.MustCompile(`(?m)^ok\s+\d+\s+-?\s*(.+?)\s*$`),
}

// ReportedTests is every check identity a runner named, red or green, sorted and
// deduplicated so two runs of one suite compare as sets.
//
// It is a superset of FailingTests by construction — the same output read
// through both vocabularies — because a roster that omitted the red half would
// report every failing check as one that had disappeared.
func ReportedTests(output string) []string {
	clean := ansiEscape.ReplaceAllString(output, "")
	seen := map[string]bool{}
	var names []string
	collect := func(patterns []*regexp.Regexp) {
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllStringSubmatch(clean, -1) {
				name := normalizeTestName(match[1])
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	collect(passingTestPatterns)
	collect(failingTestPatterns)
	sort.Strings(names)
	return names
}

// checkDeclarationPatterns is what a check declaration looks like, by shape, in
// the ecosystems this program has met. Each captures the check's own name.
//
// This is deliberately narrower than "a line mentioning a test": it must be a
// DECLARATION, because the question it answers is which checks a change brought
// into existence and which it took out. A line that calls a helper, imports a
// fixture or renames a variable is not a check, and counting it as one would let
// a diff that touched a test file look like a diff that wrote tests.
var checkDeclarationPatterns = []*regexp.Regexp{
	// jest / vitest / mocha / jasmine / bun, including the .each, .only and
	// .skip suffixes — a skipped check is a check that has stopped running,
	// which is exactly the case the removal half exists to catch.
	regexp.MustCompile(`(?:^|\W)(?:it|test|bench)(?:\.\w+)*\s*(?:\(|` + "`" + `)\s*(?:'([^']{2,200})'|"([^"]{2,200})"|` + "`" + `([^` + "`" + `]{2,200})` + "`" + `)`),
	// xit / xtest / fit — the same, spelled as a prefix
	regexp.MustCompile(`(?:^|\W)[xf](?:it|test)\s*\(\s*(?:'([^']{2,200})'|"([^"]{2,200})")`),
	// pytest / unittest / nose
	regexp.MustCompile(`(?:^|\W)(?:async\s+)?def\s+(test_\w+)\s*\(`),
	// go test
	regexp.MustCompile(`(?:^|\W)func\s+((?:Test|Benchmark|Fuzz|Example)\w*)\s*\(`),
	// rust
	regexp.MustCompile(`(?:^|\W)fn\s+(\w*test\w*)\s*\(`),
	// junit / testng — the annotation names the method on the following line,
	// so the method is what is captured wherever the two share one.
	regexp.MustCompile(`(?:^|\W)(?:public|private|protected)?\s*void\s+(test\w+)\s*\(`),
	// rspec / minitest
	regexp.MustCompile(`(?:^|\W)(?:it|specify)\s+(?:'([^']{2,200})'|"([^"]{2,200})")\s+do`),
}

// DeclaredChecks names every check a body of text declares, in the order it
// declares them, without repeats.
//
// It reads source rather than output, so it is the one reader here that works on
// a change nobody has run — which is what makes a diff answer the coverage
// question at all.
func DeclaredChecks(source string) []string {
	seen := map[string]bool{}
	var names []string
	for _, pattern := range checkDeclarationPatterns {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			// One pattern, several alternative capture groups: the quoted name
			// in whichever quotation mark the author used. Exactly one of them
			// is ever non-empty.
			for _, captured := range match[1:] {
				name := normalizeTestName(captured)
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

// PatchChecks reads a unified diff and names the checks it ADDS and the checks
// it REMOVES.
//
// A line is read for its content and never for its file, because a diff carries
// no reliable statement about which files are test files: a check moved between
// two files is added and removed in the same patch and cancels here, which is
// the honest reading of a move. A check that is only removed is a check that
// stopped existing, and that is the whole finding
// docs/design/gate/ACCEPTANCE.md §3 is about — deleting the failing test is the
// cheapest way there is to make a suite green.
//
// The diff's own headers are skipped: "+++ b/test/foo.test.ts" begins with a
// plus and declares nothing.
func PatchChecks(patch string) (added, removed []string) {
	var plus, minus strings.Builder
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			plus.WriteString(line[1:])
			plus.WriteString("\n")
		case strings.HasPrefix(line, "-"):
			minus.WriteString(line[1:])
			minus.WriteString("\n")
		}
	}
	wrote, took := DeclaredChecks(plus.String()), DeclaredChecks(minus.String())
	// A check present on both sides was edited, moved or re-indented, not
	// removed. Subtracting here rather than at every caller is what keeps the
	// removal half from convicting a worker for reformatting a test file.
	return Subtract(wrote, took), Subtract(took, wrote)
}
