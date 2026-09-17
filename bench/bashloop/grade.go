package main

// grade.go — the verdicts, taken by code.
//
// bench/README.md's standing doctrine: the counts, not the harness's
// self-assessment, are the verdict, and no LLM judges anything. Each cell's
// grader is the check the design names for it — the fixture's own suite, a
// mechanical diff, a document's presence and coverage. A grader that cannot
// run says so in its detail and fails; it never guesses.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// grade is one cell's outcome.
type grade struct {
	Pass   bool
	Detail string
}

// fixtureFiles is a fixture's own files, read out of the driver's embedded
// copy: the pristine state every tests-were-not-weakened check is made
// against.
type fixtureFiles map[string][]byte

// gradeCell grades one landed invocation. workDir is the task's own working
// copy — the tree under the session folder, which is where the work actually
// landed; the ground is only the seed that copy was cut from. pristine is the
// fixture as it started, embedded in the driver. An empty workDir is a run
// that never prepared a copy, and the row says so in as many words.
func gradeCell(c cell, workDir string, r readings, pristine fixtureFiles) grade {
	if workDir == "" {
		return grade{Pass: false, Detail: "the task never prepared a working copy"}
	}
	switch c.id {
	case "c1":
		return gradeC1(workDir, pristine)
	case "c2":
		return gradeC2(workDir, pristine)
	case "c3":
		return gradeC3(workDir, pristine)
	case "c4":
		return gradeC4(workDir)
	case "c5":
		return gradeC5(workDir, r)
	case "c6":
		return gradeC6(workDir)
	default:
		return grade{Detail: fmt.Sprintf("no grader for cell %s", c.id)}
	}
}

// ── shared helpers ──────────────────────────────────────────────────────────

// gradeSuiteGreen runs the fixture's own suite. Its counts are the verdict.
func gradeSuiteGreen(dir, what string) grade {
	out, err := goTest(dir)
	if err != nil {
		return grade{Detail: fmt.Sprintf("suite not green: %v — %s", err, tail(out, 4))}
	}
	return grade{Pass: true, Detail: what}
}

// fileUnchanged says whether a fixture file still holds the fixture's own
// bytes.
func fileUnchanged(dir, name string, pristine fixtureFiles) bool {
	body, ok := pristine[name]
	if !ok {
		return true
	}
	have, err := os.ReadFile(filepath.Join(dir, name))
	return err == nil && string(have) == string(body)
}

// tail is the last n lines of a command's output, for a detail line.
func tail(out string, n int) string {
	lines := splitLines(strings.TrimSpace(out))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// ── c1: small fix ───────────────────────────────────────────────────────────

// The suite goes green, and the test file is byte-for-byte the fixture's own.
func gradeC1(workDir string, pristine fixtureFiles) grade {
	if g := gradeSuiteGreen(workDir, "suite green"); !g.Pass {
		return g
	}
	if !fileUnchanged(workDir, "clamp_test.go", pristine) {
		return grade{Detail: "the suite is green but the test file was changed"}
	}
	return grade{Pass: true, Detail: "suite green, tests untouched"}
}

// ── c2: feature ─────────────────────────────────────────────────────────────

// The suite is green, a histogram package exists with tests of its own, and
// the stats package the fixture started with is untouched.
func gradeC2(workDir string, pristine fixtureFiles) grade {
	if g := gradeSuiteGreen(workDir, "suite green"); !g.Pass {
		return g
	}
	entries, err := os.ReadDir(filepath.Join(workDir, "histogram"))
	if err != nil {
		return grade{Detail: "no histogram package directory"}
	}
	hasTest := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_test.go") {
			hasTest = true
		}
	}
	if !hasTest {
		return grade{Detail: "histogram exists but carries no tests of its own"}
	}
	for _, name := range []string{"stats.go", "stats_test.go"} {
		if !fileUnchanged(workDir, name, pristine) {
			return grade{Detail: name + " was changed"}
		}
	}
	return grade{Pass: true, Detail: "suite green, histogram added, stats untouched"}
}

// ── c3: multi-file refactor ─────────────────────────────────────────────────

// taxLiteral is the duplicated calculation's own constant: three copies in
// the seed fixture, one after the refactor.
const taxLiteral = "0.0825"

// Suite green, one copy of the tax math, and the public surface byte-for-byte
// what the fixture started with.
func gradeC3(workDir string, pristine fixtureFiles) grade {
	if g := gradeSuiteGreen(workDir, "suite green"); !g.Pass {
		return g
	}
	if info, err := os.Stat(filepath.Join(workDir, "tax.go")); err != nil || info.IsDir() {
		return grade{Detail: "no tax.go"}
	}
	copies, err := countTaxCopies(workDir)
	if err != nil {
		return grade{Detail: fmt.Sprintf("count the tax copies: %v", err)}
	}
	if copies != 1 {
		return grade{Detail: fmt.Sprintf("the tax math still lives in %d places, want 1", copies)}
	}
	if diff, err := compareAPI(workDir, pristine); err != nil {
		return grade{Detail: fmt.Sprintf("read the public surface: %v", err)}
	} else if diff != "" {
		return grade{Detail: diff}
	}
	return grade{Pass: true, Detail: "suite green, one copy of the tax math, public surface unchanged"}
}

// countTaxCopies counts the non-test files still carrying the pasted tax
// math.
func countTaxCopies(workDir string) (int, error) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return 0, err
	}
	copies := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(workDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(body), taxLiteral) {
			copies++
		}
	}
	return copies, nil
}

// compareAPI regenerates the fixture's public surface the way `go doc` sees
// it, from a pristine copy of the same fixture on this machine with the same
// toolchain, and compares it against the work's. It answers "" when the two
// agree and names the first difference when they do not.
func compareAPI(workDir string, pristine fixtureFiles) (string, error) {
	scratch, err := os.MkdirTemp("", "bashloop-api-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	for name, body := range pristine {
		if err := os.WriteFile(filepath.Join(scratch, name), body, 0o600); err != nil {
			return "", err
		}
	}
	before, err := goDoc(scratch)
	if err != nil {
		return "", err
	}
	after, err := goDoc(workDir)
	if err != nil {
		return "", err
	}
	if before == after {
		return "", nil
	}
	left, right := splitLines(before), splitLines(after)
	for i := 0; i < len(left) || i < len(right); i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			return fmt.Sprintf("the public surface changed: %q became %q", l, r), nil
		}
	}
	return "the public surface changed", nil
}

// ── c4: report ──────────────────────────────────────────────────────────────

// REPORT.md exists, carries the four sections with something under each,
// numbers at least three recommendations, and is grounded in the corpus.
func gradeC4(workDir string) grade {
	body, err := os.ReadFile(filepath.Join(workDir, "REPORT.md"))
	if err != nil {
		return grade{Detail: "no REPORT.md at the top level"}
	}
	return gradeC4Text(string(body))
}

// gradeC4Text is the c4 grade over the report's text, split from the read so
// a test can hand it a report without a tree.
func gradeC4Text(text string) grade {
	for _, name := range []string{"Summary", "Findings", "Risks", "Recommendations"} {
		under, ok := sectionBody(text, name)
		if !ok {
			return grade{Detail: fmt.Sprintf("no %s section", name)}
		}
		if len(strings.TrimSpace(under)) < 20 {
			return grade{Detail: fmt.Sprintf("the %s section is empty", name)}
		}
	}
	recommendations, ok := sectionBody(text, "Recommendations")
	if !ok {
		return grade{Detail: "no Recommendations section"}
	}
	if counted := countNumbered(recommendations); counted < 3 {
		return grade{Detail: fmt.Sprintf("%d numbered recommendations, want at least 3", counted)}
	}
	found := 0
	for _, sentinel := range []string{"47", "34", "61", "3.4", "$4,210", "41"} {
		if strings.Contains(text, sentinel) {
			found++
		}
	}
	if found < 2 {
		return grade{Detail: fmt.Sprintf("the report quotes %d of the corpus's own figures, want at least 2", found)}
	}
	return grade{Pass: true, Detail: "four sections, numbered recommendations, grounded in the corpus"}
}

// sectionBody answers what a markdown section says, or false when the
// document does not carry it: the heading naming it, and the text between
// that heading and the next heading of any level.
func sectionBody(text, name string) (string, bool) {
	// A SECTION ENDS AT THE NEXT HEADING OF ITS OWN LEVEL OR HIGHER, not at
	// the next heading of any level: a Findings section written as one
	// sub-heading per document has its whole body under those sub-headings,
	// and a reader that stopped at the first one graded every such report
	// as empty (both arms failed c4 that way on 2026-09-17).
	headings := regexp.MustCompile(`(?m)^(#{1,6})\s.*$`)
	locs := headings.FindAllStringSubmatchIndex(text, -1)
	for i, loc := range locs {
		if !strings.Contains(text[loc[0]:loc[1]], name) {
			continue
		}
		level := loc[3] - loc[2]
		start := loc[1]
		end := len(text)
		for _, next := range locs[i+1:] {
			if next[3]-next[2] <= level {
				end = next[0]
				break
			}
		}
		return text[start:end], true
	}
	return "", false
}

func countNumbered(body string) int {
	items := regexp.MustCompile(`(?m)^\s*\d+\.\s`)
	return len(items.FindAllString(body, -1))
}

// ── c5: wide job ────────────────────────────────────────────────────────────

// Children landed and the integrated result present: the module's suite is
// green, the integration note exists, and the graph's own record shows at
// least two of the root task's parts landing as work of their own.
func gradeC5(workDir string, r readings) grade {
	if g := gradeSuiteGreen(workDir, "the module's suite is green"); !g.Pass {
		return g
	}
	if _, err := os.Stat(filepath.Join(workDir, "INTEGRATION.md")); err != nil {
		return grade{Detail: "no INTEGRATION.md at the top level"}
	}
	if r.ChildrenDone < 2 {
		return grade{Detail: fmt.Sprintf("the graph shows %d children landed, want at least 2 — the work was done in one pair of hands", r.ChildrenDone)}
	}
	return grade{Pass: true, Detail: fmt.Sprintf("suite green, %d of %d parts landed as their own work, integration note written", r.ChildrenDone, r.ChildrenTotal)}
}

// ── c6: kept-tool image ─────────────────────────────────────────────────────

// The image exists in the fixture and ARCHITECTURE.md references it by name.
func gradeC6(workDir string) grade {
	body, err := os.ReadFile(filepath.Join(workDir, "ARCHITECTURE.md"))
	if err != nil {
		return grade{Detail: "no ARCHITECTURE.md at the top level"}
	}
	refs := imageReferences(string(body))
	if len(refs) == 0 {
		return grade{Detail: "ARCHITECTURE.md references no image"}
	}
	for _, ref := range refs {
		if _, err := os.Stat(filepath.Join(workDir, filepath.FromSlash(ref))); err == nil {
			return grade{Pass: true, Detail: "image exists and is referenced: " + ref}
		}
	}
	return grade{Detail: fmt.Sprintf("ARCHITECTURE.md references %v, and none of them exist", refs)}
}

// imageReferences pulls markdown image targets out of a document, relative
// paths only.
func imageReferences(text string) []string {
	matches := regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`).FindAllStringSubmatch(text, -1)
	var out []string
	for _, m := range matches {
		target := strings.TrimSpace(m[1])
		if target == "" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			continue
		}
		if i := strings.IndexAny(target, " \t"); i >= 0 {
			target = target[:i]
		}
		if strings.HasSuffix(target, ".png") || strings.HasSuffix(target, ".jpg") ||
			strings.HasSuffix(target, ".jpeg") || strings.HasSuffix(target, ".webp") ||
			strings.HasSuffix(target, ".svg") || strings.HasSuffix(target, ".gif") {
			out = append(out, target)
		}
	}
	return out
}

// splitLines answers a string's lines, without the line endings.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			end := i
			if end > start && s[end-1] == '\r' {
				end--
			}
			out = append(out, s[start:end])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// firstLine is a report's own first line, for a one-line reading.
func firstLine(s string) string {
	lines := splitLines(s)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}
