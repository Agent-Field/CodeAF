package verify

// HOW MUCH of a project one reading covers, and why that is decided before the
// clock is consulted rather than after.
//
// textual's whole-repository reading collects 3,422 tests and takes 793 seconds,
// measured in its own task image. The wall it was given afforded 5m30s. So the
// reading was started, killed at its ceiling, named nothing, and cost an eighth
// of the run — and every round of that job inherited the same refusal. The
// budget was doing exactly what it was written to do; what was missing is that
// NOTHING HAD DECIDED WHAT TO MEASURE BEFORE DECIDING HOW LONG TO MEASURE IT.
//
// A reading exists to answer two questions about ONE piece of work: which checks
// exist near it, and which of them this work broke. Neither question is asked of
// the four hundred test files the work never went near. So the ladder is scoped
// first and bounded second: the checks adjacent to the change, then — only if
// there is still budget for it — the whole suite. A scoped reading of the right
// forty checks is a better answer than a whole reading that was killed, and it
// is a strictly better answer than no reading at all, which is what the whole
// rung actually returned.
//
// THE SCOPE IS PART OF THE READING'S IDENTITY. Two readings only subtract when
// they are readings of the same thing: a before reading of the whole suite minus
// an after reading of three files is a hundred checks that "disappeared" and a
// finding about nothing. The scope therefore rides on Strategy, which is pinned
// on the first reading and re-used verbatim for the second — see RunReading —
// and it is journaled, so an autopsy reading a roster of forty knows whether it
// is looking at a small project or at a scoped reading of a large one.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Focus is what this job is about, as paths: what the person's request named,
// and what the record shows the work touched.
//
// It is supplied by the caller because only the caller knows which of those it
// holds — a first leaf has the request and no diff, a continuation and the
// delivery gate have both. An empty Focus is a job that named nothing, and its
// reading is of the whole project, which is what every reading here was before
// this existed.
type Focus []string

// Within is this focus as a reading taken inside dir would see it: the entries
// that fall under dir, with dir taken off the front.
//
// It exists because a focus is recorded once, against the workspace, and read in
// several places — the root and every package a monorepo declares. A path that
// does not fall under dir is not dropped silently; it is simply not part of that
// package's change, which is the fact the scoped reading there is a reading of.
func (f Focus) Within(dir string) Focus {
	dir = strings.Trim(filepath.ToSlash(strings.TrimSpace(dir)), "/")
	if dir == "" || dir == "." {
		return f
	}
	prefix := dir + "/"
	within := make(Focus, 0, len(f))
	for _, entry := range f {
		clean := filepath.ToSlash(strings.TrimSpace(entry))
		if after, cut := strings.CutPrefix(clean, prefix); cut {
			within = append(within, after)
		}
	}
	return within
}

// namedPath matches a token that reads as a path: a stem, a dot, and a
// two-to-eight character alphanumeric extension opening with a letter. The
// extension's shape is what keeps prose out — "e.g.", "i.e.", "vs." and version
// numbers all fail it — and the stem's character class is what lets a path
// through, because "src/textual/_rich_log.py" names a file exactly as
// "report.md" does.
//
// It is the shape rule for "this text names a file", and it lives here because
// two questions turn out to be this question: which files a request is about,
// which decides where a reading is taken, and which files a request named, which
// the delivery record settles against the disk. revision.NamedFiles reads this
// one rather than keeping a second copy of it.
var namedPath = regexp.MustCompile(`[\w.\-/]*\w\.[A-Za-z][A-Za-z0-9]{1,7}\b`)

// NamedPaths lists, in order and without repeats, the paths a piece of text
// names.
func NamedPaths(text string) []string {
	var names []string
	seen := map[string]bool{}
	for _, match := range namedPath.FindAllString(text, -1) {
		clean := strings.Trim(strings.TrimSpace(match), "/")
		clean = strings.TrimPrefix(clean, "./")
		if clean == "" || seen[strings.ToLower(clean)] {
			continue
		}
		seen[strings.ToLower(clean)] = true
		names = append(names, clean)
	}
	return names
}

// ScopeWhole is what a reading of everything the entrypoint covers calls itself.
// It is a word rather than an empty string because a scope that says nothing is
// indistinguishable from a reading taken before scopes existed, and those are
// the two things the comparison rule most needs to tell apart.
const ScopeWhole = "whole"

const (
	// scopeScanLimit bounds the walk that finds the checks adjacent to a
	// change. It is producedScanLimit's sibling — internal/exec bounds its own
	// tree walks at 6000 entries for the same reason — and it runs once per
	// reading rather than once per tool call, so the cheaper bound is the one
	// that matters: past it the answer is "no adjacent checks found", the
	// ladder falls to the whole rung, and nothing is wrong except that a very
	// large repository paid for a scoped reading it did not get.
	scopeScanLimit = 6000

	// scopeReadBudget is the total bytes this will read to find the test files
	// that IMPORT what the work touched. A test file that names the changed
	// module is a check adjacent to the change even when it lives nowhere near
	// it, which is the common shape in a repository with one tests/ directory —
	// and finding it costs reading test files, so the reading is bounded. Two
	// mebibytes is a few hundred test files; past it the selection is whatever
	// was found, which is a narrower answer and never a wrong one.
	scopeReadBudget = 2 << 20

	// scopeSelectionLimit bounds how many files one scoped reading names. A
	// command line with four hundred paths on it is a command line the shell
	// refuses, and a selection that large is not a selection — it is the whole
	// suite spelled the long way, which the ladder already has a rung for.
	scopeSelectionLimit = 40
)

// testFileMarkers are what a test file's own name looks like, in every
// ecosystem this program has met. It is the same kind of shape rule
// checkDeclarationPatterns is, applied to the filename rather than to the body,
// and it is deliberately the loose half of the pair: a file this admits and that
// declares no check costs a path on a command line, and the runner ignores it.
var testFileMarkers = []func(name string) bool{
	func(name string) bool { return strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py") },
	func(name string) bool { return strings.HasSuffix(name, "_test.py") },
	func(name string) bool { return strings.HasSuffix(name, "_test.go") },
	func(name string) bool { return strings.HasSuffix(name, "_test.rs") },
	func(name string) bool { return strings.HasSuffix(name, "_test.exs") },
	func(name string) bool { return strings.HasSuffix(name, "_spec.rb") },
	func(name string) bool { return strings.HasSuffix(name, "Test.java") },
	func(name string) bool { return strings.HasSuffix(name, "Tests.cs") },
	func(name string) bool { return hasTestInfix(name, ".test.") },
	func(name string) bool { return hasTestInfix(name, ".spec.") },
}

// hasTestInfix is the JavaScript and TypeScript shape — foo.test.ts,
// foo.spec.tsx — where the marker sits between the stem and the extension.
func hasTestInfix(name, infix string) bool {
	index := strings.Index(name, infix)
	return index > 0 && strings.LastIndexByte(name, '.') > index
}

// testFileName says a file's own name declares it a test file.
func testFileName(name string) bool {
	lowered := strings.ToLower(name)
	for _, marker := range testFileMarkers {
		if marker(name) || marker(lowered) {
			return true
		}
	}
	return false
}

// testDirName says a directory is where a project keeps its checks. It is the
// second half of the same shape rule: a repository that keeps every check in
// tests/ names none of its files after the module they exercise.
func testDirName(name string) bool {
	switch strings.ToLower(name) {
	case "test", "tests", "__tests__", "spec", "specs", "testing":
		return true
	}
	return false
}

// Adjacent is the checks that sit next to a change: the test files the focus
// names outright, the test files beside what it touched, and the test files that
// name what it touched.
//
// Paths come back relative to root — which for a member rung is the package's
// own directory, so the selection drops straight onto a command run there — in
// stable order, bounded at scopeSelectionLimit.
//
// ok is false when the focus names nothing, when nothing adjacent was found, or
// when the walk hit its bound before finding anything. All three mean the same
// thing to the caller: THERE IS NO SCOPED READING TO TAKE HERE, take the whole
// one. A scoped reading that guessed would be a reading of a suite nobody chose.
func Adjacent(root string, focus Focus) (paths []string, ok bool) {
	dirs, stems := focusShape(root, focus)
	if len(dirs) == 0 && len(stems) == 0 {
		return nil, false
	}
	var named, beside, importing []string
	visited, budget := 0, scopeReadBudget
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if visited++; visited > scopeScanLimit {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			if path != root && skipBuilt(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !testFileName(entry.Name()) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		slashed := filepath.ToSlash(relative)
		switch {
		case stems[stemOf(entry.Name())]:
			// The change is IN this test file, or the test file is named after
			// what the change touched. Either way it is the first thing to run.
			named = append(named, slashed)
		case adjacentDir(slashed, dirs):
			beside = append(beside, slashed)
		default:
			// The last and most expensive question: does this check name the
			// thing that changed? It is asked only of files the first two rules
			// did not already take, and only while the read budget lasts.
			if budget <= 0 {
				return nil
			}
			body, read := readSmallFile(path)
			budget -= len(body)
			if read && namesAny(body, stems) {
				importing = append(importing, slashed)
			}
		}
		return nil
	})
	// The three groups keep their order and each is sorted inside itself. The
	// order is what survives the cap: a selection cut at its limit keeps the
	// test files the change is IN before the ones merely beside it, and those
	// before the ones that only mention it.
	selection := dedupe(append(append(sortedUnique(named), sortedUnique(beside)...), sortedUnique(importing)...))
	if len(selection) == 0 {
		return nil, false
	}
	if len(selection) > scopeSelectionLimit {
		selection = selection[:scopeSelectionLimit]
	}
	return selection, true
}

// focusShape reduces a focus to the two things the selection asks of it: the
// directories the work touched, and the stems of the files it touched.
//
// A stem is the file name without its extension, which is the one spelling a
// module is imported by in every ecosystem here: `_rich_log` in Python, `Igel`
// in a Java import, `feature_schema` in a `from igel.feature_schema import`.
// Names too short to be a module — one or two characters, `index`, `main` — are
// dropped, because a stem that matches every file selects every file.
func focusShape(root string, focus Focus) (dirs map[string]bool, stems map[string]bool) {
	dirs, stems = map[string]bool{}, map[string]bool{}
	for _, entry := range focus {
		clean := strings.TrimSpace(entry)
		if clean == "" {
			continue
		}
		if filepath.IsAbs(clean) {
			relative, err := filepath.Rel(root, clean)
			if err != nil || strings.HasPrefix(relative, "..") {
				continue
			}
			clean = relative
		}
		clean = filepath.ToSlash(filepath.Clean(clean))
		if clean == "." || strings.HasPrefix(clean, "..") {
			continue
		}
		base := clean
		if index := strings.LastIndexByte(clean, '/'); index >= 0 {
			dirs[clean[:index]] = true
			base = clean[index+1:]
		} else {
			dirs["."] = true
		}
		if stem := stemOf(base); stem != "" {
			stems[stem] = true
		}
	}
	return dirs, stems
}

// commonStems are the file names that name nothing: every package has an index
// and a main, and a stem that matches everything selects everything.
var commonStems = map[string]bool{
	"index": true, "main": true, "mod": true, "init": true, "lib": true,
	"types": true, "utils": true, "util": true, "test": true, "tests": true,
}

// stemOf is a file name reduced to the module it is about: its extensions taken
// off, its test affixes taken off, and its word separators taken out.
//
// All three reductions are the same rule — TWO SPELLINGS OF ONE NAME ARE ONE
// NAME — and each of them is a way a project spells the link between a module
// and the check for it. `foo.test.ts` checks `foo.ts`; `test_rich_log.py`
// checks `_rich_log.py`, where the leading underscore is Python's own mark for
// a private module and belongs to neither name; and `RichLog` in an import is
// `rich_log` on disk, which is one name written in the two conventions the same
// project uses at once. A reader that compared these literally found nothing
// adjacent to anything, which is a scoped reading that never happens.
func stemOf(name string) string {
	if index := strings.IndexByte(name, '.'); index > 0 {
		name = name[:index]
	}
	name = strings.ToLower(name)
	for _, affix := range []string{"test_", "spec_"} {
		name = strings.TrimPrefix(name, affix)
	}
	for _, affix := range []string{"_test", "_spec", "test", "spec"} {
		if trimmed := strings.TrimSuffix(name, affix); trimmed != "" {
			name = trimmed
		}
	}
	name = separators.ReplaceAllString(name, "")
	if len(name) < 3 || commonStems[name] {
		return ""
	}
	return name
}

// separators are the word marks a name is spelled with in one convention and
// without in another: rich_log, rich-log, RichLog. Taking them out is what lets
// the three be recognised as one name.
var separators = regexp.MustCompile(`[^a-z0-9]+`)

// adjacentDir says a test file sits beside the change: in a directory the work
// touched, or in that directory's own tests/ directory, or in a tests/
// directory the touched directory sits under.
func adjacentDir(file string, dirs map[string]bool) bool {
	dir := pathDir(file)
	for touched := range dirs {
		if dir == touched || strings.HasPrefix(dir, touched+"/") {
			return true
		}
		// A repository that keeps its checks in one tree names that tree's
		// directories after the source tree's. `src/widgets/x.py` is checked by
		// `tests/widgets/`, and the shared tail is what says so.
		if testDirName(firstSegment(dir)) && strings.HasSuffix(dir, "/"+lastSegment(touched)) {
			return true
		}
	}
	return false
}

func firstSegment(slashed string) string {
	if index := strings.IndexByte(slashed, '/'); index >= 0 {
		return slashed[:index]
	}
	return slashed
}

func lastSegment(slashed string) string {
	if index := strings.LastIndexByte(slashed, '/'); index >= 0 {
		return slashed[index+1:]
	}
	return slashed
}

// namesAny says this body of source mentions one of the stems the work touched.
// It is a mention rather than a parsed import on purpose: every ecosystem spells
// an import differently and all of them spell the module's own name, and the
// cost of over-matching here is one more file on a command line.
func namesAny(body string, stems map[string]bool) bool {
	// Read in the same spelling stemOf reduces a name to, so an import of
	// `RichLog` and a file called `_rich_log.py` are one name here as well.
	flattened := separators.ReplaceAllString(strings.ToLower(body), "")
	for stem := range stems {
		if strings.Contains(flattened, stem) {
			return true
		}
	}
	return false
}

// sortedUnique orders one group of the selection, so the same tree answers the
// same way twice — a selection that kept a different subset on every read would
// make two readings of one project disagree about what they measured.
func sortedUnique(names []string) []string {
	kept := dedupe(names)
	sort.Strings(kept)
	return kept
}

// dedupe keeps the first sighting of each name and the order they arrived in.
func dedupe(names []string) []string {
	seen := make(map[string]bool, len(names))
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		kept = append(kept, name)
	}
	return kept
}
