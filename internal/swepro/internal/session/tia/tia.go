// Test-impact analysis — port of src/session/tia.ts.
//
// Two layers: a pure reverse-reachability selector over a caller-supplied
// import graph (SelectImpactedTests / BuildTestCommand), and a bounded
// filesystem walk that builds the graph itself
// (ComputeImpactedTestsForWorkspace).
//
// Fidelity notes (deliberate, do not "fix"):
//   - Every `.sort()` in the TS is the DEFAULT Array.prototype.sort, i.e.
//     UTF-16 code-unit order, not byte order. sortDefault reproduces that with
//     sort.SliceStable + a utf16.Encode comparator; byte order would reorder a
//     non-BMP path against a U+E000..U+FFFF one.
//   - The TS `matches()` writes `pattern.lastIndex = 0` before every
//     `pattern.test()`. That reset is what makes a /…/g or /…/y pattern behave
//     statelessly across the repeated calls in the DFS and in the
//     allTestFiles filter. Go regexps are stateless, so TestFilePattern needs
//     no reset — the observable behaviour is identical, which is exactly why
//     the reset exists.
//   - isTestFile's `changed` short-circuit means a changed path that is listed
//     in allTestFiles is impacted even when it does not match the pattern;
//     kept verbatim.
//   - The walk pushes directories and pops them (LIFO), and its `files` order
//     is the raw readdir order — os.File.ReadDir(-1), NOT os.ReadDir, which
//     would sort. Order is observable through the maxFiles cutoff, because
//     `files.length >= maxFiles` is checked BEFORE the size filter that can
//     skip a file.
//   - Any error out of the directory walk returns nil (TS: `catch { return
//     null }`), while a per-file stat/read error only skips that file.
package tia

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/importgraph"
)

// TestFilePattern is the Go stand-in for the TS `RegExp` option. A JS RegExp
// can use constructs RE2 has no answer for (lookaround, backreferences), so
// the port takes a predicate instead of a *regexp.Regexp and lets the caller
// supply an RE2 translation or a hand-rolled scanner. A nil TestFilePattern is
// TS `undefined`: it is both the `!pattern` falsy case in isTestFile and the
// `pattern ?? DEFAULT_TEST_PATTERN` nullish case.
type TestFilePattern func(path string) bool

// Confidence is the TS `"exact" | "partial"` union.
type Confidence string

const (
	ConfidenceExact   Confidence = "exact"
	ConfidencePartial Confidence = "partial"
)

// Runner is the TS `"bun" | "vitest"` union.
type Runner string

const (
	RunnerBun    Runner = "bun"
	RunnerVitest Runner = "vitest"
)

// SelectImpactedTestsOptions mirrors the TS interface. TestFilePattern carries
// `json:"-"` because a JS RegExp serializes to `{}` and is never part of any
// output shape.
type SelectImpactedTestsOptions struct {
	ChangedFiles    []string                `json:"changedFiles"`
	Edges           importgraph.ImportGraph `json:"edges"`
	AllTestFiles    []string                `json:"allTestFiles"`
	TestFilePattern TestFilePattern         `json:"-"`
}

// ImpactedTestsResult mirrors the TS interface; field order is the object
// literal's key order so JSON.stringify parity holds.
type ImpactedTestsResult struct {
	Impacted   []string   `json:"impacted"`
	Confidence Confidence `json:"confidence"`
	Reason     string     `json:"reason"`
}

// codeExtensions is CODE_EXTENSIONS. Membership-only, never iterated, so a
// bare Go map is order-safe.
var codeExtensions = map[string]bool{
	".ts":  true,
	".tsx": true,
	".js":  true,
	".jsx": true,
	".mjs": true,
	".cjs": true,
}

// defaultTestPatternRE is DEFAULT_TEST_PATTERN. The TS `$` (no `m` flag) is
// end-of-input, which is also Go's `$` with the default flags — neither
// matches before a trailing newline.
var defaultTestPatternRE = regexp.MustCompile(`(?:\.test|\.spec)\.(?:ts|tsx|js|jsx|mjs|cjs)$`)

var defaultTestPattern TestFilePattern = defaultTestPatternRE.MatchString

// SelectImpactedTests selects tests that can reach a changed module through
// reverse import edges. The graph is supplied by the caller so this module
// remains pure and easy to use with a precomputed repository graph.
func SelectImpactedTests(opts SelectImpactedTestsOptions) ImpactedTestsResult {
	changedFiles, edges, allTestFiles, testFilePattern :=
		opts.ChangedFiles, opts.Edges, opts.AllTestFiles, opts.TestFilePattern
	changed := uniqueStrings(changedFiles)
	testFiles := stringSet(allTestFiles)
	impacted := newOrderedSet()
	visited := map[string]struct{}{}
	stack := reverseStrings(sortDefault(cloneStrings(changed)))

	for len(stack) > 0 {
		path := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, seen := visited[path]; seen {
			continue
		}
		visited[path] = struct{}{}

		if isTestFile(path, testFiles, testFilePattern, false) {
			impacted.add(path)
		}

		// `if (!dependents) continue` — an empty array is truthy in JS, but a
		// present-and-empty entry and a missing entry are indistinguishable
		// here: both skip the loop body.
		dependents, ok := edges.Dependents.Get(path)
		if !ok {
			continue
		}
		for _, dependent := range reverseStrings(sortDefault(cloneStrings(dependents))) {
			if _, seen := visited[dependent]; !seen {
				stack = append(stack, dependent)
			}
		}
	}

	// A changed test can be absent from the graph (for example, a newly added
	// test), so test membership is checked independently of reachability.
	for _, path := range changed {
		if isTestFile(path, testFiles, testFilePattern, true) {
			impacted.add(path)
		}
	}

	unknown := filterStrings(changed, func(path string) bool { return !hasGraphNode(edges, path) })
	nonCode := filterStrings(changed, func(path string) bool { return !isCodeFile(path) })
	partialReasons := []string{}
	if len(changed) == 0 {
		partialReasons = append(partialReasons, "no changed files")
	}
	if len(nonCode) > 0 {
		partialReasons = append(partialReasons,
			"non-code change(s): "+strings.Join(sortDefault(nonCode), ", "))
	}
	if len(unknown) > 0 {
		partialReasons = append(partialReasons,
			"unknown graph file(s): "+strings.Join(sortDefault(unknown), ", "))
	}

	confidence := ConfidenceExact
	if len(partialReasons) > 0 {
		confidence = ConfidencePartial
	}
	var reason string
	if confidence == ConfidencePartial {
		reason = "partial impact analysis: " + strings.Join(partialReasons, "; ") +
			"; run the full suite in the inner loop"
	} else {
		reason = "exact reverse reachability: " + jscompat.FormatNumber(float64(impacted.size())) +
			" impacted test(s) from " + jscompat.FormatNumber(float64(len(changed))) + " changed file(s)"
	}

	return ImpactedTestsResult{
		Impacted:   sortDefault(impacted.values()),
		Confidence: confidence,
		Reason:     reason,
	}
}

// BuildTestCommand is buildTestCommand; a nil return is the TS `null`.
func BuildTestCommand(impacted []string, runner Runner) *string {
	if len(impacted) == 0 {
		return nil
	}
	var out string
	if runner == RunnerBun {
		out = "bun test " + strings.Join(impacted, " ")
	} else {
		out = "npx vitest run " + strings.Join(impacted, " ")
	}
	return &out
}

func isTestFile(path string, allTestFiles map[string]struct{}, pattern TestFilePattern, changed bool) bool {
	if _, ok := allTestFiles[path]; ok {
		return changed || pattern == nil || matches(pattern, path)
	}
	if pattern == nil {
		return matches(defaultTestPattern, path)
	}
	return matches(pattern, path)
}

// matches is the TS `pattern.lastIndex = 0; return pattern.test(path)`. Go
// patterns hold no cursor, so the reset has no analogue and none is needed.
func matches(pattern TestFilePattern, path string) bool {
	return pattern(path)
}

func hasGraphNode(graph importgraph.ImportGraph, path string) bool {
	return graph.Deps.Has(path) || graph.Dependents.Has(path)
}

// isCodeFile slices on '/' and '.', both ASCII, so byte indices and the TS
// UTF-16 indices pick out the same substrings, and `dot > 0` means the same
// thing in either index space.
//
// Divergence (accepted): strings.ToLower is Unicode simple lowercasing while
// JS toLowerCase applies the unconditional SpecialCasing entries (U+0130 →
// "i̇") and Final_Sigma. Neither can turn a non-ASCII extension into one
// of ts/tsx/js/jsx/mjs/cjs — none of those contain an 'i' or 'k', the only
// letters non-ASCII code points lowercase into — so the Set lookup is
// unaffected.
func isCodeFile(path string) bool {
	base := path[strings.LastIndex(path, "/")+1:]
	dot := strings.LastIndex(base, ".")
	return dot > 0 && codeExtensions[strings.ToLower(base[dot:])]
}

// ---------------------------------------------------------------------------
// Workspace-level TIA: bounded fs walk -> import graph -> impacted tests.
// Pure filesystem work (no LLM); returns nil instead of guessing when the
// repo is too large to scan cheaply or the walk fails — callers fall back to
// their full-suite behavior, never to a truncated result.
// ---------------------------------------------------------------------------

var skipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"out":          true,
	"coverage":     true,
	"vendor":       true,
	".next":        true,
	".codeaf":      true,
}

// WorkspaceTiaResult mirrors the TS interface. The spread `{ ...selected,
// scannedFiles }` puts scannedFiles last, so the field order here is
// impacted, confidence, reason, scannedFiles.
type WorkspaceTiaResult struct {
	Impacted     []string   `json:"impacted"`
	Confidence   Confidence `json:"confidence"`
	Reason       string     `json:"reason"`
	ScannedFiles int        `json:"scannedFiles"`
}

// ComputeImpactedTestsForWorkspaceOptions is the TS inline options bag.
// MaxFiles/MaxBytes are pointers because `??` only defaults on null/undefined:
// an explicit 0 must stay 0.
type ComputeImpactedTestsForWorkspaceOptions struct {
	Workspace       string          `json:"workspace"`
	ChangedFiles    []string        `json:"changedFiles"`
	MaxFiles        *float64        `json:"maxFiles"`
	MaxBytes        *float64        `json:"maxBytes"`
	TestFilePattern TestFilePattern `json:"-"`
}

// ComputeImpactedTestsForWorkspace is computeImpactedTestsForWorkspace; a nil
// return is the TS `null`. The `require("node:fs")` try/catch has no Go
// analogue (os is always available), so that branch is unreachable here — the
// remaining nil returns are the maxFiles cutoff and the walk failure.
func ComputeImpactedTestsForWorkspace(opts ComputeImpactedTestsForWorkspaceOptions) *WorkspaceTiaResult {
	maxFiles := 4000.0
	if opts.MaxFiles != nil {
		maxFiles = *opts.MaxFiles
	}
	maxBytes := 512.0 * 1024
	if opts.MaxBytes != nil {
		maxBytes = *opts.MaxBytes
	}

	files := []importgraph.SourceFile{}
	stack := []string{opts.Workspace}
	for len(stack) > 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries, err := readdirWithFileTypes(dir)
		if err != nil {
			// TS: readdirSync throws → outer catch → return null.
			return nil
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if !skipDirs[entry.Name()] && !strings.HasPrefix(entry.Name(), ".") {
					stack = append(stack, filepath.Join(dir, entry.Name()))
				}
				continue
			}
			// Dirent.isFile() is d_type-based: symlinks are neither a
			// directory nor a file and fall out here.
			if !entry.Type().IsRegular() {
				continue
			}
			abs := filepath.Join(dir, entry.Name())
			rel := strings.ReplaceAll(pathRelative(opts.Workspace, abs), string(filepath.Separator), "/")
			if !isCodeFile(rel) {
				continue
			}
			if float64(len(files)) >= maxFiles {
				return nil
			}
			// Unreadable file: skip it rather than abort the whole scan.
			st, err := os.Stat(abs)
			if err != nil {
				continue
			}
			if float64(st.Size()) > maxBytes {
				continue
			}
			content, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			files = append(files, importgraph.SourceFile{Path: rel, Content: decodeUTF8Lossy(content)})
		}
	}

	edges := importgraph.BuildImportGraph(files, nil)
	pattern := opts.TestFilePattern
	if pattern == nil {
		pattern = defaultTestPattern
	}
	allTestFiles := []string{}
	for _, f := range files {
		if matches(pattern, f.Path) {
			allTestFiles = append(allTestFiles, f.Path)
		}
	}
	selected := SelectImpactedTests(SelectImpactedTestsOptions{
		ChangedFiles:    opts.ChangedFiles,
		Edges:           edges,
		AllTestFiles:    allTestFiles,
		TestFilePattern: opts.TestFilePattern,
	})
	return &WorkspaceTiaResult{
		Impacted:     selected.Impacted,
		Confidence:   selected.Confidence,
		Reason:       selected.Reason,
		ScannedFiles: len(files),
	}
}

// ---------------------------------------------------------------------------
// JS-shaped helpers
// ---------------------------------------------------------------------------

// readdirWithFileTypes is `fs.readdirSync(dir, { withFileTypes: true })`:
// entries in raw directory order. os.ReadDir sorts by name and would change
// which file trips the maxFiles cutoff.
func readdirWithFileTypes(dir string) ([]os.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// pathRelative is node:path's relative(): both sides are resolved against the
// cwd first. filepath.Rel can only fail here if one side cannot be made
// absolute, which node's resolve() never reports; the absolute path is
// returned in that unreachable case.
func pathRelative(from string, to string) string {
	absFrom, err := filepath.Abs(from)
	if err != nil {
		return to
	}
	absTo, err := filepath.Abs(to)
	if err != nil {
		return to
	}
	rel, err := filepath.Rel(absFrom, absTo)
	if err != nil {
		return absTo
	}
	return rel
}

// decodeUTF8Lossy is `fs.readFileSync(abs, "utf8")`: malformed sequences
// become U+FFFD, one per maximal subpart (the Unicode-recommended practice
// V8/node implement). Go's utf8.DecodeRune reports one bad byte at a time,
// which would emit three U+FFFD for a truncated 4-byte sequence instead of
// one.
//
// Divergence (accepted, unreachable in practice): node also lossily decodes
// FILE NAMES from readdir, so a file whose name is not valid UTF-8 fails to
// re-open and is skipped there while Go reads it fine.
func decodeUTF8Lossy(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 {
			sb.WriteRune(utf8.RuneError)
			i += maximalSubpart(b[i:])
			continue
		}
		sb.Write(b[i : i+size])
		i += size
	}
	return sb.String()
}

// maximalSubpart returns the length of the longest prefix of b that is a
// prefix of some well-formed UTF-8 sequence (at least 1).
func maximalSubpart(b []byte) int {
	c := b[0]
	var need int
	var lo, hi byte
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need, lo, hi = 1, 0x80, 0xBF
	case c == 0xE0:
		need, lo, hi = 2, 0xA0, 0xBF
	case c >= 0xE1 && c <= 0xEC:
		need, lo, hi = 2, 0x80, 0xBF
	case c == 0xED:
		need, lo, hi = 2, 0x80, 0x9F
	case c >= 0xEE && c <= 0xEF:
		need, lo, hi = 2, 0x80, 0xBF
	case c == 0xF0:
		need, lo, hi = 3, 0x90, 0xBF
	case c >= 0xF1 && c <= 0xF3:
		need, lo, hi = 3, 0x80, 0xBF
	case c == 0xF4:
		need, lo, hi = 3, 0x80, 0x8F
	default:
		return 1
	}
	n := 1
	for k := 0; k < need; k++ {
		if n >= len(b) {
			return n
		}
		l, h := byte(0x80), byte(0xBF)
		if k == 0 {
			l, h = lo, hi
		}
		if b[n] < l || b[n] > h {
			return n
		}
		n++
	}
	return n
}

// uniqueStrings is `Array.from(new Set(v))`: first occurrence wins, insertion
// order preserved.
func uniqueStrings(v []string) []string {
	set := newOrderedSet()
	for _, s := range v {
		set.add(s)
	}
	return set.values()
}

func stringSet(v []string) map[string]struct{} {
	out := make(map[string]struct{}, len(v))
	for _, s := range v {
		out[s] = struct{}{}
	}
	return out
}

func cloneStrings(v []string) []string {
	out := make([]string, len(v))
	copy(out, v)
	return out
}

func filterStrings(v []string, keep func(string) bool) []string {
	out := []string{}
	for _, s := range v {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

// reverseStrings is `.reverse()`: in place, returning the same slice.
func reverseStrings(v []string) []string {
	for i, j := 0, len(v)-1; i < j; i, j = i+1, j-1 {
		v[i], v[j] = v[j], v[i]
	}
	return v
}

// sortDefault is `Array.prototype.sort()` with no comparator: UTF-16
// code-unit order, in place, stable.
func sortDefault(v []string) []string {
	sort.SliceStable(v, func(i, j int) bool { return compareUTF16(v[i], v[j]) < 0 })
	return v
}

// compareUTF16 orders by UTF-16 code unit, which is what V8's default sort
// comparator does. It differs from byte order once a non-BMP code point
// (surrogate pair, leading unit 0xD800..0xDBFF) meets a BMP one at or above
// U+E000.
func compareUTF16(a string, b string) int {
	if a == b {
		return 0
	}
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	n := len(ua)
	if len(ub) < n {
		n = len(ub)
	}
	for i := 0; i < n; i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	if len(ua) < len(ub) {
		return -1
	}
	if len(ua) > len(ub) {
		return 1
	}
	return 0
}

// orderedSet is a JS Set of strings: insertion-ordered, first add wins.
type orderedSet struct {
	seen map[string]struct{}
	list []string
}

func newOrderedSet() *orderedSet {
	return &orderedSet{seen: map[string]struct{}{}, list: []string{}}
}

func (s *orderedSet) add(v string) {
	if _, ok := s.seen[v]; ok {
		return
	}
	s.seen[v] = struct{}{}
	s.list = append(s.list, v)
}

func (s *orderedSet) size() int { return len(s.list) }

func (s *orderedSet) values() []string {
	out := make([]string, len(s.list))
	copy(out, s.list)
	return out
}
