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

// Locate resolves what a request SAYS into paths the workspace actually holds.
//
// It exists because happy-dom s8 took its whole reading at the repository root
// and never once looked at `packages/happy-dom`, where the work was. Nothing was
// wrong with the workspace declaration or with the nearest-manifest walk: THE
// FOCUS WAS EMPTY. A focus was derived only from paths a request spells out, and
// that request spells out none — it says "Implement `observe()`, `unobserve()`,
// `disconnect()` and `takeRecords()`", names `IntersectionObserver` and
// `IntersectionObserverEntry`, and never writes a single path. So no package was
// touched as far as the reader knew, the ladder had only root rungs, and the
// root's `npx vitest run` was killed at its ceiling naming nothing.
//
// A request that names a thing this repository has a FILE for is a request about
// that file, and that is a structural fact rather than a guess. Three ways a
// name resolves, all of them whole-name equality and none of them a substring:
//
//   - a path the workspace holds outright;
//   - a bare file name, resolved to wherever the workspace keeps it;
//   - a distinctive identifier whose spelling IS a source file's name under the
//     ecosystem's own convention — `IntersectionObserver` is
//     `IntersectionObserver.ts`, and `RichLog` is `_rich_log.py`, because
//     CamelCase and snake_case are two spellings of one name and a leading
//     underscore is Python's mark for a private module.
//
// Unresolved entries are kept as they were: they cost nothing and a caller may
// have handed a path this walk could not reach.
func Locate(root string, focus Focus) Focus {
	wanted := map[string][]int{}
	located := append(Focus{}, focus...)
	found := map[int][]string{}
	for index, entry := range focus {
		clean := strings.TrimSpace(entry)
		if clean == "" || strings.ContainsAny(clean, "/\\") || filepath.IsAbs(clean) {
			continue
		}
		if key := locateKey(clean); key != "" {
			wanted[key] = append(wanted[key], index)
		}
	}
	if len(wanted) == 0 {
		return located
	}
	visited := 0
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
		// A test file is where a name is CHECKED, never where it lives. Filing
		// the request's own subject under a test file would send the reading to
		// the package the check is in rather than to the package the work is in
		// — usually the same place, and when it is not, the wrong one.
		if testFileName(entry.Name()) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		slashed := filepath.ToSlash(relative)
		name := lastSegment(slashed)
		stem := name
		if index := strings.IndexByte(name, '.'); index > 0 {
			stem = name[:index]
		}
		for _, key := range []string{locateKey(name), locateKey(stem)} {
			for _, at := range wanted[key] {
				// EVERY FILE THAT CARRIES THE NAME, NOT THE FIRST ONE THE WALK
				// TRIPS OVER. This used to keep one sighting and drop the rest,
				// and a repository that documents itself shadows its own source:
				// textual holds `docs/examples/widgets/rich_log.py` beside
				// `src/textual/widgets/_rich_log.py`, `docs` sorts first, and
				// every scoped reading of the nemotron n1 run was aimed at the
				// documentation copy of the widget it was changing. Which of two
				// files of one name is the implementation is not a question this
				// can answer by looking at either of them — so it answers none
				// of it, hands both to Adjacent, and lets the structural ranking
				// there decide.
				if len(found[at]) >= locateLimit {
					continue
				}
				found[at] = append(found[at], slashed)
			}
		}
		return nil
	})
	// Spliced in where the name stood, so the order the request put its subjects
	// in is the order the selection is built in.
	resolved := make(Focus, 0, len(located)+len(found))
	for index, entry := range located {
		if hits := found[index]; len(hits) > 0 {
			resolved = append(resolved, hits...)
			continue
		}
		resolved = append(resolved, entry)
	}
	return resolved
}

// locateLimit bounds how many files one name may resolve to.
//
// A name is a name because it is distinctive, so a handful is the honest answer
// for a repository that keeps a source copy, a documentation copy and a stub of
// one thing; past that the name was not distinctive after all and the focus is
// better off short. It is memberLimit's size for the same reason — this is the
// same "how many candidates is a ladder rung worth" question, asked one level
// down.
const locateLimit = 4

// locateKey is the one spelling two names are compared in: lowercased, with the
// word marks a name is written with in one convention and without in another
// taken out, and with Python's private mark off the front.
//
// It is EQUALITY of whole names and never containment, which is the difference
// between this and the reader textual s8 replaced. A key shorter than four
// characters is dropped: `Log` and `App` and `Row` are names half a repository
// answers to, and a focus that resolves to half a repository is no focus.
func locateKey(name string) string {
	key := separators.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "")
	// Three, because a module named for one segment of a compound the request
	// also spells is a real name — textual's `Log` beside its `RichLog` — and
	// two is where a name stops being one. See standaloneSegments, which is the
	// only caller that reaches down here.
	if len(key) < 3 {
		return ""
	}
	return key
}

// separators are the word marks a name is spelled with in one convention and
// without in another: rich_log, rich-log, RichLog.
var separators = regexp.MustCompile(`[^a-z0-9]+`)

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

// subjectSpellings are the two ways a repository's own name for a thing is
// written inside a sentence: CamelCase with at least two segments, and
// snake_case with at least two segments. Both are DISTINCTIVE by construction —
// a word a person could have used by accident is one segment — and both are only
// ever resolved by whole-name equality against a file the workspace holds, so a
// spelling that names nothing costs nothing.
var subjectSpellings = []*regexp.Regexp{
	regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]+)+\b`),
	regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`),
}

// namedSubjectLimit bounds how many subjects one request contributes. A request
// naming more than this many distinct things is not describing a change, and the
// resolution walk is one walk whatever the count — this bounds the map and the
// focus that comes out of it, not the reading.
const namedSubjectLimit = 64

// NamedSubjects is what a request is ABOUT, as the names it uses: the paths it
// spells out, and the identifiers it names in the repository's own spelling.
//
// The second half is the repair for happy-dom s8, whose request names
// `IntersectionObserver`, `IntersectionObserverEntry`, `observe()` and
// `takeRecords()` and does not contain one path — so a focus built from paths
// alone was empty, no package was ever chosen, and the whole reading was taken
// at the repository root and killed at its ceiling.
//
// Nothing here decides anything on its own: a name is only a subject once
// [Locate] has matched it, whole, to a file the workspace holds.
func NamedSubjects(text string) []string {
	named := NamedPaths(text)
	seen := make(map[string]bool, len(named))
	for _, name := range named {
		seen[strings.ToLower(name)] = true
	}
	for _, spelling := range subjectSpellings {
		for _, match := range spelling.FindAllString(text, -1) {
			key := strings.ToLower(match)
			if seen[key] || locateKey(match) == "" {
				continue
			}
			seen[key] = true
			named = append(named, match)
			if len(named) >= namedSubjectLimit {
				return named
			}
			for _, part := range standaloneSegments(text, match) {
				if key := strings.ToLower(part); !seen[key] && locateKey(part) != "" {
					seen[key] = true
					named = append(named, part)
					if len(named) >= namedSubjectLimit {
						return named
					}
				}
			}
		}
	}
	return named
}

// segmentSpelling is one segment of a CamelCase name: a capital and the small
// letters and digits after it.
var segmentSpelling = regexp.MustCompile(`[A-Z][a-z0-9]*`)

// standaloneSegments are the segments of a compound name that the SAME TEXT also
// uses on their own.
//
// A REQUEST THAT SAYS BOTH `RichLog` AND `Log` HAS NAMED TWO THINGS. Only the
// compound one is spelled distinctively enough to be a subject on sight — a
// single capitalised word is as likely to be the first word of a sentence — so
// the short one was dropped, and textual's nemotron run never once resolved
// `Log` to `widgets/_log.py`: every reading it took was of the RichLog side of a
// change that touched both, and `tests/test_log.py` reached the selection only
// after the work, off the diff.
//
// What makes the short name admissible is not its length. It is that the text
// spells it BOTH ways — inside a compound this reader has already accepted, and
// again on its own as a whole word — which is a structural fact about the
// request and not a judgement about the word. `Rich` in "with current Rich"
// qualifies too, and correctly: if the repository holds a file of that name, the
// request is about it, and if it does not, Locate resolves nothing and the entry
// costs one map lookup.
//
// A segment shorter than three characters is not read. Below that a name is not
// a name — it is an initial, an article or a unit, and there is no repository
// where matching it whole means anything.
func standaloneSegments(text, compound string) []string {
	parts := segmentSpelling.FindAllString(compound, -1)
	if len(parts) < 2 {
		return nil
	}
	var standing []string
	for _, part := range parts {
		if len([]rune(part)) < 3 {
			continue
		}
		alone := regexp.MustCompile(`\b` + regexp.QuoteMeta(part) + `\b`)
		for _, at := range alone.FindAllStringIndex(text, -1) {
			// Inside the compound itself does not count: what is being looked
			// for is the request using the short name in its own right.
			if inCompound(text, at[0], at[1]) {
				continue
			}
			standing = append(standing, part)
			break
		}
	}
	return standing
}

// inCompound says this occurrence is part of a longer run of name characters —
// the `Log` inside `RichLog`, which is the compound already accepted rather than
// a mention of the short name.
func inCompound(text string, from, to int) bool {
	return (from > 0 && identifierByte(text[from-1])) || (to < len(text) && identifierByte(text[to]))
}

// OwnChecks is every check file in a record of what a run left behind, as
// workspace-relative paths in stable order.
//
// It is the world's own answer to "which of these are checks" — the record is
// the artifact registry settled against the filesystem, so a file is here
// because the tree gained or changed it, whatever wrote it — and a path is a
// check by the runner's own naming convention and by nothing else. It is
// deliberately not the worker's account of what it tested: that is a claim about
// checks the same worker wrote, which is the thing the whole gate exists not to
// weigh.
func OwnChecks(root string, record []string) Focus {
	return recordPaths(root, record, true)
}

// ChangedSources is the other half of the same record: every file the run left
// behind that is NOT a check.
//
// It is what the second reading is aimed at. The scope of the first is a reading
// of the REQUEST, settled before the work existed and inherited by every round —
// and a request is not a diff. textual s10 asked for `Log and RichLog`, which
// resolved to `_rich_log.py` and could not resolve `Log` at all, so both readings
// ran `tests/test_concurrency.py tests/test_textlog.py`; the change touched
// `_log.py` and `_rich_log.py`, and `tests/test_log.py` — which the repository
// already had, beside the file the work changed — was never read on either side.
// The diff is the one account of where the work actually went, and it exists by
// the time the second reading is taken.
func ChangedSources(root string, record []string) Focus {
	return recordPaths(root, record, false)
}

// recordPaths is the record read as workspace-relative paths the tree still
// holds, split by whether they are checks.
//
// STILL THERE is the condition both halves share. A record settled against the
// world should hold nothing else, but a scoped command is a list of paths and a
// runner handed one that has since been moved or deleted fails to collect
// ANYTHING — which would turn a widening meant to see more checks into a reading
// of none.
func recordPaths(root string, record []string, checks bool) Focus {
	held := make(Focus, 0, len(record))
	for _, entry := range record {
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
		if clean == "." || strings.HasPrefix(clean, "..") ||
			testFileName(lastSegment(clean)) != checks {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(clean))); err != nil || info.IsDir() {
			continue
		}
		held = append(held, clean)
	}
	return Focus(sortedUnique(held))
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

// Adjacent is the checks that sit next to a change, and it decides that BY
// STRUCTURE.
//
// It did not, once, and the cost is measured. textual s8 touched `_log.py`,
// `_rich_log.py`, `widget.py` and `messages.py`; the reader flattened every name
// to its letters and asked whether a test file's text CONTAINED one, so the stem
// `log` matched `dialog`, `catalog`, `logic` and `logging` wherever they
// appeared. The selection came back as forty files spanning tests/animations,
// command_palette, css, directory_tree, document, footer and input — a third of
// the suite, none of it about this change — and the reading was killed at its
// ceiling of 1m53s naming nothing at all. A SUBSTRING IS NOT A RELATIONSHIP.
//
// Two structural relationships, in this rank:
//
//  1. The test file NAMED AFTER the touched file, by the runner's own naming
//     convention — `test_<stem>.py`, `<stem>_test.go`, `<stem>.test.ts` — with
//     the stem compared whole and never as a fragment; and the test files
//     sitting in the same directory as the touched file.
//  2. The test files whose IMPORT STATEMENTS resolve to the touched module —
//     `from textual.widgets._rich_log import RichLog`, and equally
//     `from textual.widgets import RichLog`, because the package's own
//     `__init__.py` says that name comes from that file. For JavaScript a
//     relative specifier is resolved against the importing file's own
//     directory. Only import lines are read, and only whole identifiers match,
//     so `Log` never matches inside `Logger` and `log` never matches inside
//     `dialog`.
//
// Paths come back relative to root — which for a member rung is the package's
// own directory, so the selection drops straight onto a command run there — with
// rank 1 before rank 2 and each rank sorted inside itself. That order is what
// survives the cap, so a selection cut to fit keeps the checks the change is
// actually in.
//
// core is how many of them are rank 1 — the checks the change is IN, as opposed
// to the ones that merely import it. It is what a reading cut at its ceiling
// narrows back to, because that is the smallest selection that is still a
// reading of this change rather than of its neighbourhood.
//
// ok is false when the focus names nothing, when nothing adjacent was found, or
// when the walk hit its bound before finding anything. All three mean the same
// thing to the caller: THERE IS NO SCOPED READING TO TAKE HERE, take the whole
// one. A scoped reading that guessed would be a reading of a suite nobody chose.
func Adjacent(root string, focus Focus) (paths []string, core int, ok bool) {
	touched := focusShape(root, focus)
	if touched.empty() {
		return nil, 0, false
	}
	var own, named, importing []string
	visited, budget, suite := 0, scopeReadBudget, 0
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
		suite++
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		slashed := filepath.ToSlash(relative)
		switch {
		case touched.checks[slashed]:
			// A CHECK THE JOB ITSELF NAMED IS ALWAYS IN SCOPE, and it leads the
			// selection rather than joining the rank beside it. The focus carries
			// the record of what the run left behind, and a test file in that
			// record is a check this job WROTE — the one thing a scope decided
			// before the work could never have known about, and the one thing
			// the coverage mapping most needs to see. Everything below may be
			// cut back by the eighth rule or by a measured pace; this may not.
			own = append(own, slashed)
		case touched.namesAfter(entry.Name()) || touched.dirs[pathDir(slashed)]:
			named = append(named, slashed)
		default:
			// The second rank, and the only one that costs a read. It is asked
			// only of files the first rank did not already take, and only while
			// the read budget lasts.
			if budget <= 0 {
				return nil
			}
			body, read := readSmallFile(path)
			budget -= len(body)
			if read && touched.importedBy(slashed, body) {
				importing = append(importing, slashed)
			}
		}
		return nil
	})
	mine := sortedUnique(own)
	first := dedupe(append(mine, sortedUnique(named)...))
	selection := dedupe(append(first, sortedUnique(importing)...))
	if len(selection) == 0 {
		return nil, 0, false
	}
	if len(selection) > scopeSelectionLimit {
		selection = selection[:scopeSelectionLimit]
	}
	core = min(len(first), len(selection))
	// A SELECTION LARGER THAN AN EIGHTH OF THE SUITE IS NOT A SCOPE. Past that
	// it is a sample of the same order as the whole thing, and the whole rung
	// directly below it is a better reading for the same money — so the scoped
	// rung has stopped paying for itself and is cut back to the checks the
	// change is IN.
	//
	// The eighth is WallShare, spent on the other axis and for the same reason
	// it is spent on the first: an eighth is the share of a thing a measurement
	// may take before it has become the thing. textual s8 selected forty of its
	// 251 test files — a third of the suite — because `widget.py` and
	// `messages.py` are genuinely imported by three dozen tests. Every one of
	// those imports is real; the selection is still not a reading of this
	// change, and it was killed at its ceiling naming nothing.
	//
	// Where there is no core — nothing is named after what changed — the
	// neighbourhood is all there is, and it is cut to the eighth itself.
	// The comparison is only worth making on a suite big enough for a scope to
	// be worth having: one no larger than the most a scope may name at all is
	// one where the whole rung directly below costs about the same, and
	// trimming there buys nothing and loses roster.
	//
	// The floor under every cut is the run's OWN checks. They are not a sample
	// of the suite — they are the work, and a reading that dropped them is the
	// igel s8 reading that could not see forty of its own new checks.
	if share := suite / WallShare; suite > scopeSelectionLimit && len(selection) > share {
		keep := len(selection)
		switch {
		case core > 0 && core < keep:
			keep = core
		case share > 0 && share < keep:
			keep = share
		}
		selection = selection[:max(keep, min(len(mine), len(selection)))]
		core = min(core, len(selection))
	}
	return selection, core, true
}

// change is what a focus says about the tree, in the three forms the two
// adjacency rules ask for it.
type change struct {
	// dirs are the directories the work touched, so a check sitting beside it
	// is a check next to it.
	dirs map[string]bool
	// stems are the touched files' own names without their extension, compared
	// WHOLE. `_rich_log.py` contributes `_rich_log` and `rich_log`, because a
	// leading underscore is Python's mark for a private module and is not part
	// of the name the check is written under — `test_rich_log.py`.
	stems map[string]bool
	// identifiers are what an import statement would have to name to be naming
	// one of these files: the module's own tail, and every public name the
	// package's own `__init__.py` re-exports FROM that module. textual's
	// widgets package says `from textual.widgets._rich_log import RichLog`, so
	// a test that writes `from textual.widgets import RichLog` is importing the
	// touched file and there is no other way to know it.
	identifiers map[string]bool
	// files are the touched paths themselves, so a relative JavaScript
	// specifier can be resolved against them.
	files map[string]bool
	// checks are the touched paths that are THEMSELVES check files. They are
	// held apart because they are not adjacent to the change — they ARE it, and
	// a scope that left the run's own new tests out has nothing to map a
	// checklist against.
	checks map[string]bool
}

func (c change) empty() bool {
	return len(c.dirs) == 0 && len(c.stems) == 0 && len(c.identifiers) == 0 &&
		len(c.checks) == 0
}

// namesAfter says this test file is the one written for a touched file, under
// the runner's own naming convention.
//
// The convention is a table of AFFIXES, not a substring search: the file's name
// with its test marker removed must EQUAL a touched stem. `test_log.py` is the
// check for `_log.py`; `test_logger.py` is the check for `logger.py` and for
// nothing else, which is the distinction the flattened reader could not draw.
func (c change) namesAfter(name string) bool {
	for _, bare := range testFileStems(name) {
		if c.stems[bare] {
			return true
		}
	}
	return false
}

// testFileStems is a test file's name with each of the test conventions'
// affixes taken off, in every reading that applies. A name is usually one of
// them; returning all of them costs nothing and keeps the table declarative.
func testFileStems(name string) []string {
	lowered := strings.ToLower(name)
	trimmed := lowered
	if index := strings.IndexByte(lowered, '.'); index > 0 {
		trimmed = lowered[:index]
	}
	stems := []string{}
	add := func(stem string) {
		if len(stem) >= 2 {
			stems = append(stems, stem)
		}
	}
	// `foo.test.ts`, `foo.spec.tsx` — the marker sits between stem and
	// extension, so the stem is already what is in front of the first dot.
	if hasTestInfix(lowered, ".test.") || hasTestInfix(lowered, ".spec.") {
		add(trimmed)
	}
	for _, prefix := range []string{"test_", "spec_", "test"} {
		if after, cut := strings.CutPrefix(trimmed, prefix); cut {
			add(after)
		}
	}
	for _, suffix := range []string{"_test", "_spec", "test", "tests", "spec"} {
		if before, cut := strings.CutSuffix(trimmed, suffix); cut {
			add(before)
		}
	}
	return stems
}

// importedBy says this test file's IMPORT STATEMENTS name the touched module.
//
// Only import lines are read. A module's name appearing in the body of a check
// is the check talking about something; a module's name in an import is the
// check depending on it, and only the second is a relationship. That difference
// is the whole of textual s8: `log` in the word `dialog` in the body of a test
// about the command palette put that test in the reading.
func (c change) importedBy(file, body string) bool {
	dir := pathDir(file)
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !importLine.MatchString(trimmed) {
			continue
		}
		for identifier := range c.identifiers {
			if namesIdentifier(trimmed, identifier) {
				return true
			}
		}
		// A relative specifier is a path, and it resolves against the file that
		// wrote it. `import { Element } from '../src/nodes/Element'` in
		// test/nodes/X.test.ts is that file and nothing else.
		for _, specifier := range relativeSpecifier.FindAllStringSubmatch(trimmed, -1) {
			if c.resolves(dir, specifier[1]) {
				return true
			}
		}
	}
	return false
}

// resolves says a relative import specifier, read from a file in dir, names one
// of the touched files — trying the extensions and the index file a JavaScript
// resolver would try, because a specifier is written without them.
func (c change) resolves(dir, specifier string) bool {
	joined := filepath.ToSlash(filepath.Join(dir, specifier))
	if c.files[joined] {
		return true
	}
	for _, extension := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} {
		if c.files[joined+extension] || c.files[joined+"/index"+extension] {
			return true
		}
	}
	return false
}

var (
	// An import statement, in the two grammars this program meets. It is the
	// line's own shape and not a search for a word.
	importLine = regexp.MustCompile(
		`^(?:from[[:space:]]|import[[:space:]]|import\{|const[[:space:]].*=[[:space:]]*require\(|.*[[:space:]]require\()` +
			`|^import[[:space:]]*[{*'"]` + "|^export[[:space:]].*[[:space:]]from[[:space:]]")
	// A relative specifier inside one, in either quote.
	relativeSpecifier = regexp.MustCompile(`['"](\.[^'"]*)['"]`)
	// A leading underscore is Python's mark for a private module, and it is not
	// part of the name the check for it is written under.
	privateMark = regexp.MustCompile(`^_+`)
)

// namesIdentifier says this line names this identifier AS AN IDENTIFIER: not as
// a fragment of a longer one, and not inside a word.
//
// It is rule 3 of the repair, and it is the one that had to be a rule. `Log`
// must not match `Logger`; `log` must not match `dialog`; `_rich_log` must not
// match `_rich_log2`. The test is that the characters either side of the match
// cannot themselves be part of an identifier.
func namesIdentifier(line, identifier string) bool {
	if identifier == "" {
		return false
	}
	for offset := 0; ; {
		index := strings.Index(line[offset:], identifier)
		if index < 0 {
			return false
		}
		at := offset + index
		before := byte(' ')
		if at > 0 {
			before = line[at-1]
		}
		after := byte(' ')
		if end := at + len(identifier); end < len(line) {
			after = line[end]
		}
		if !identifierByte(before) && !identifierByte(after) {
			return true
		}
		offset = at + 1
	}
}

func identifierByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// focusShape reduces a focus to what the two adjacency rules ask of the tree.
func focusShape(root string, focus Focus) change {
	shape := change{
		dirs: map[string]bool{}, stems: map[string]bool{},
		identifiers: map[string]bool{}, files: map[string]bool{},
		checks: map[string]bool{},
	}
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
		dir, base := pathDir(clean), lastSegment(clean)
		if testFileName(base) {
			shape.checks[clean] = true
		}
		shape.dirs[dir] = true
		stem := base
		if index := strings.IndexByte(base, '.'); index > 0 {
			stem = base[:index]
		}
		if len(stem) < 2 || commonStems[strings.ToLower(stem)] {
			continue
		}
		shape.files[strings.TrimSuffix(clean, filepath.Ext(clean))] = true
		shape.stems[strings.ToLower(stem)] = true
		public := privateMark.ReplaceAllString(stem, "")
		if len(public) >= 2 {
			shape.stems[strings.ToLower(public)] = true
		}
		// The module's own tail is what a full-path import names.
		shape.identifiers[stem] = true
		for name := range reExportedNames(root, dir, stem) {
			shape.identifiers[name] = true
		}
	}
	return shape
}

// commonStems are the file names that name nothing in particular: every package
// has an index and a main, and a check named after one of them is named after
// the package rather than after the file.
var commonStems = map[string]bool{
	"index": true, "main": true, "mod": true, "init": true, "__init__": true,
}

// reExport is the line a package's own entry point writes to say that a public
// name comes from one of its modules: `from textual.widgets._rich_log import
// RichLog`, `from ._rich_log import RichLog`, `export { Element } from
// './nodes/Element'`.
var reExport = regexp.MustCompile(
	`(?m)^[[:space:]]*(?:from[[:space:]]+([\w.]+)[[:space:]]+import[[:space:]]+(.+)$` +
		`|export[[:space:]]*\{([^}]*)\}[[:space:]]*from[[:space:]]*['"]([^'"]+)['"])`)

// reExportedNames are the public names a package's entry point re-exports FROM
// one of its modules.
//
// It is what turns `from textual.widgets import RichLog` into a fact about
// `_rich_log.py`. Without it a test that imports a widget the way every user of
// the library imports it is invisible to the reader, and the only tests found
// are the ones that reached past the package's own front door.
//
// Read from the package's own entry point and from nowhere else: this is the
// file whose job is to say where its names come from, and a name it does not
// mention is a name it does not export.
func reExportedNames(root, dir, module string) map[string]bool {
	names := map[string]bool{}
	for _, entry := range []string{"__init__.py", "index.ts", "index.js", "index.tsx", "mod.rs"} {
		body, ok := readSmallFile(filepath.Join(root, filepath.FromSlash(dir), entry))
		if !ok {
			continue
		}
		for _, match := range reExport.FindAllStringSubmatch(body, -1) {
			source, exported := match[1], match[2]
			if source == "" {
				source, exported = match[4], match[3]
			}
			if lastSegment(strings.ReplaceAll(source, ".", "/")) != module {
				continue
			}
			for _, name := range strings.Split(exported, ",") {
				name = strings.TrimSpace(strings.Trim(strings.TrimSpace(name), "()"))
				if index := strings.Index(name, " as "); index > 0 {
					name = strings.TrimSpace(name[index+4:])
				}
				if name != "" && name != "*" {
					names[name] = true
				}
			}
		}
	}
	return names
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

// lastSegment is the final component of a slash-spelled path.
func lastSegment(slashed string) string {
	if index := strings.LastIndexByte(slashed, '/'); index >= 0 {
		return slashed[index+1:]
	}
	return slashed
}
