package revision

// The second door the acceptance mapping goes through: a check must ASSERT the
// behaviour, not merely mention it.
//
// GroundMapping, beside this, asks whether the check SPELLS the names the
// behaviour spells. That question is answered by a file's whole text, setup and
// all, and textual s13 is what a run does with the difference. The request asked
// that `RichLog.write(..., expand=True)` honour expansion; the run wrote
// `test_rich_log_write_expand_preserves_full_width_justified`, which calls
// `rich_log.write("short", expand=True)` and then asserts `len(rich_log.lines) >
// 0` and `strip.cell_length >= 5`. Every name is there. Nothing is weighed. The
// gate had named that exact behaviour unexercised one round earlier, counted it
// exercised on the next, passed at exit 0, and the hidden check for it —
// `test_rich_log_expand_entries_reflow_after_min_width_change` — was red.
//
// So the door: A BEHAVIOUR IS EXERCISED BY A CHECK ONLY WHERE THE CHECK'S OWN
// ASSERTIONS NAME ONE OF THE BEHAVIOUR'S OBSERVABLES. The observables are read
// off the REQUEST — the identifiers, dotted names, keyword arguments and bound
// values the person themselves spelled — exactly as the entailment door reads
// symbols out of a citation, and the assertions are read off the file by shape
// (verify.AssertionsIn). No model has a say in either half.
//
// IT FIRES IN ONE DIRECTION AND EVERY SILENCE FAVOURS THE CHECK. A behaviour
// that names no observable is not judged here; a check this program cannot find
// or cannot parse is not judged here; and a single observable named in a single
// assertion clears the point outright. Being generous about what counts as an
// observable therefore makes the door quieter, not louder, which is why the
// reader below takes every shape it can recognise.

import (
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// observablesNamed bounds what one finding says out loud about a behaviour: the
// first few observables nothing asserted, and no count after them.
//
// It is regressionsNamed's smaller sibling and a different unit. That one bounds
// how many FINDINGS are listed, where a reader is learning the shape of a gap;
// this bounds a parenthetical inside one line of that list, where a reader is
// learning what to go and assert. Four names say which family of observables was
// skipped; sixteen say it no better and push the behaviour itself off the line.
const observablesNamed = 4

// Observables are the identifiers a stated behaviour names — what a check would
// have to weigh for the behaviour to be exercised rather than merely visited.
//
// Three shapes, and every one of them is a shape rather than a vocabulary:
//
//   - a name somebody spelled DISTINCTIVELY, which is symbolsIn's own rule and
//     the identical one the entailment door opens on: `is_following_end`,
//     `RichLog.write`, `max_scroll_y`, `min_width`, `#follow-log`.
//   - a name somebody BOUND to something, which is how code spells an argument
//     and prose does not: `expand=True`, `width=40`, `follow_end(animate: bool =
//     False)`. The name is the observable; the value is what makes it one.
//   - a name of the TREE somebody spelled IN WORDS. textual s16 asked that
//     normal scrolling still update "the visible viewport and vertical scrollbar
//     position"; the run asserted `scroll_y` and `max_scroll_y` a hundred and six
//     times and never touched `ScrollBar.position`, which is precisely what the
//     two hidden checks that failed assert. "vertical scrollbar position" names
//     `ScrollBar.position` and nothing in this program could see it.
//     verify.SurfaceIndex.Spoken is that reading, against the tree's own public
//     surface and only for names that have an owner.
//
// TWO SHAPES ARE THROWN AWAY, and both because they cannot be asserted ON.
//
// A hyphen is not an identifier character in any language this program reads, so
// a token whose only separator is one is an English compound: `full-width`,
// `half-open`, `pre-fetch`. It is re-admitted where the person wrote it as a
// selector (`#follow-log`) or where the tree itself declares it, because those
// are names. Without this the door would hold every request to a word nothing
// can ever assert.
//
// And a bare TYPE name the tree declares at the top level — `RichLog`, `Log` —
// is what a check constructs, never what it weighs; the values it weighs are
// that type's members. A symbol the surface confirms as an unqualified
// declaration is dropped for that reason, and one the surface does not know is
// kept, because the request may be asking for it to exist.
//
// A behaviour that yields none of the three names nothing a check could be asked
// about — "it must post only when the boolean actually changes" is a true
// sentence with no identifier in it — and the door below stays shut on it. That
// is the same asymmetry GroundMapping keeps, for the same reason: a floor that
// refuses everything is not a floor.
func Observables(text string, index verify.SurfaceIndex) []string {
	var found []string
	seen := map[string]bool{}
	admit := func(name string) {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" && !seen[name] {
			seen[name] = true
			found = append(found, name)
		}
	}
	selectors := selectorNames(text)
	for _, symbol := range symbolsIn(text) {
		if compoundOnly(symbol) && !selectors[symbol] {
			if _, held := index.Holds(symbol); !held {
				continue
			}
		}
		if name, held := index.Holds(symbol); held && !verify.Qualified(name) {
			// The tree says this is a type of its own. A check builds one; it
			// asserts on what one carries.
			continue
		}
		admit(symbol)
	}
	for _, name := range boundNames(text) {
		admit(name)
	}
	for _, name := range index.Spoken(text) {
		admit(name)
	}
	return found
}

// compoundOnly says this token is held together by hyphens and nothing else,
// which is how English writes a compound and how no language this program reads
// writes a name.
func compoundOnly(symbol string) bool {
	return strings.ContainsRune(symbol, '-') &&
		!strings.ContainsAny(symbol, "._/#") &&
		!strings.ContainsRune(symbol, '_')
}

// selectorNames are the hyphenated tokens the person wrote as a selector — the
// one place a hyphen IS part of a name. `#follow-log` and `.write-expanded` are
// names of things on a page; `full-width` is two words.
var selectorToken = regexp.MustCompile(`[#.]([A-Za-z_][A-Za-z0-9_-]*-[A-Za-z0-9_-]+)`)

func selectorNames(text string) map[string]bool {
	held := map[string]bool{}
	for _, match := range selectorToken.FindAllStringSubmatch(text, -1) {
		held[strings.ToLower(match[1])] = true
	}
	return held
}

var (
	// keywordArg is a name bound with no space around the sign, which is how
	// every language this program reads spells a keyword argument and how no
	// language spells a comparison.
	keywordArg = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*=`)
	// parameterName is one item of a parenthesised list that binds or annotates
	// a single name: `animate: bool = False`, `expand=True`, `y=0`. The name has
	// to be the whole of what precedes the sign, so `for example: three` — which
	// is prose and not a parameter — matches nothing.
	parameterName = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*[:=][^=]`)
)

// boundNames is the second shape: every name the text binds to a value, whether
// it was spelled distinctively or not.
//
// `expand` is an ordinary English word and `expand=True` is an argument. The
// difference is not in the letters, so it cannot be read out of the name — it is
// in what the person wrote beside it, which is what this looks at.
func boundNames(text string) []string {
	var names []string
	for _, match := range keywordArg.FindAllStringIndex(text, -1) {
		start, end := match[0], match[1]
		if start > 0 && nameByte(text[start-1]) {
			continue
		}
		// `a==b`, `a!=b`, `a>=b` bind nothing. The sign is the last byte of the
		// match, so what follows it decides.
		if end < len(text) && text[end] == '=' {
			continue
		}
		names = append(names, text[start:end-1])
	}
	for _, item := range parenthesisedItems(text) {
		if match := parameterName.FindStringSubmatch(strings.TrimSpace(item) + " "); match != nil {
			names = append(names, match[1])
		}
	}
	return names
}

// parenthesisedItems is every comma-separated item of every top-level
// parenthesised span, which is where a signature and a call both keep their
// arguments. Nested spans travel with the item that contains them, because
// `run_test(size=(80, 24))` binds `size` and nothing inside it.
func parenthesisedItems(text string) []string {
	var items []string
	depth, start := 0, 0
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '(':
			if depth++; depth == 1 {
				start = index + 1
			}
		case ',':
			if depth == 1 {
				items = append(items, text[start:index])
				start = index + 1
			}
		case ')':
			if depth == 0 {
				continue
			}
			if depth--; depth == 0 {
				items = append(items, text[start:index])
			}
		}
	}
	return items
}

// WeighAssertions marks each mapped pairing whose check asserts none of the
// behaviour's observables, and changes nothing else.
//
// It runs AFTER GroundMapping and never instead of it: a pairing the names door
// already emptied has no check to read, and the two doors answer different
// questions about the ones that survive it. The row keeps its check — the check
// is real, it does map, and hiding that would lose the evidence — and carries
// the observables nothing weighed, which is what makes the point weakly
// exercised downstream.
//
// ONLY THE CHECKS THIS RUN WROTE ARE READ. A check the repository already had is
// not this run's account of its own work, and holding a project's existing suite
// to a request it was written years before would fail every delivery that reuses
// one. The run's own record says which files are its (verify.OwnChecks), and a
// check outside them is left exactly as the mapping answered it.
func WeighAssertions(root, job string, record []string,
	points []plan.Point, mapping []store.ExercisedPoint,
) []store.ExercisedPoint {
	if len(mapping) == 0 || root == "" {
		return mapping
	}
	// THE VOCABULARY IS THE JOB'S BASELINE AND COSTS NOTHING NEW. The whole
	// tree's public surface is already read once per job, before its first
	// change; re-walking it per gate would spend verify's largest budget on
	// every verdict. Where a job has no baseline the index is empty and the
	// spoken half of Observables simply says nothing.
	var vocabulary verify.SurfaceIndex
	if held, ok := verify.BaselineFor(root, job); ok {
		vocabulary = verify.IndexSurface(held.Surface)
	}
	bodies := &checkBodies{root: root, budget: mappingBodyBudget, held: map[string]string{}}
	bodies.record = verify.OwnChecks(root, record)
	if len(bodies.record) == 0 {
		return mapping
	}
	own := make(map[string]bool, len(bodies.record))
	for _, path := range bodies.record {
		own[path] = true
	}
	weighed := make([]store.ExercisedPoint, len(mapping))
	copy(weighed, mapping)
	read := map[string]verify.Assertions{}
	for index := range weighed {
		check := strings.TrimSpace(weighed[index].Check)
		if check == "" || index >= len(points) {
			continue
		}
		observables := Observables(points[index].Quote, vocabulary)
		if len(observables) == 0 {
			continue
		}
		weighed[index].Observables = observables
		asserted, found := assertionText(check, own, bodies, read)
		if !found {
			continue
		}
		// EACH OBSERVABLE, NOT ANY ONE OF THEM. A behaviour names the things a
		// check would have to weigh, and weighing one of them says nothing about
		// the others. textual s16 asserted `scroll_y` and `max_scroll_y` a
		// hundred and six times against a behaviour that also named the
		// scrollbar's own `position`, nothing in the run ever read that
		// position, and the two hidden checks that failed assert exactly it.
		var missing []string
		for _, observable := range observables {
			if !assertionNames(asserted, observable) {
				missing = append(missing, observable)
			}
		}
		if len(missing) > observablesNamed {
			missing = missing[:observablesNamed]
		}
		weighed[index].Unasserted = missing
	}
	return weighed
}

// assertionNames says this check's assertions name the observable, whole, either
// as the person spelled it or as the member of it a check would actually read.
//
// A QUALIFIED NAME IS REACHED THROUGH AN INSTANCE. The request writes
// `ScrollBar.position` and `RichLog.write`; a check writes `bar.position` and
// `rich_log.write`, because the class is what made the object and the member is
// what it asks for. So the last part of a qualified observable satisfies it, and
// the whole of an unqualified one has to be there.
func assertionNames(asserted, observable string) bool {
	if namesSymbol(asserted, observable) {
		return true
	}
	if at := strings.LastIndexAny(observable, "./:"); at >= 0 && at+1 < len(observable) {
		return namesSymbol(asserted, observable[at+1:])
	}
	return false
}

// assertionText is what this check's own assertions say, and whether the check
// was found at all.
//
// Not found is the answer for a check whose file this run did not write, whose
// language has no reader, or whose declaration the reader could not locate — and
// all three leave the pairing alone. A door that treated "I could not read it"
// as "it asserts nothing" would empty every mapping in a language this program
// does not parse.
func assertionText(check string, own map[string]bool,
	bodies *checkBodies, read map[string]verify.Assertions,
) (string, bool) {
	leaf := checkLeaf(check)
	if leaf == "" {
		return "", false
	}
	for _, path := range bodies.filesFor(check) {
		if !own[path] {
			continue
		}
		assertions, seen := read[path]
		if !seen {
			assertions = verify.AssertionsIn(path, bodies.read(path))
			read[path] = assertions
		}
		if text, found := assertions.Named(leaf); found {
			return text, true
		}
	}
	return "", false
}

// checkLeaf is the DECLARATION a check identity names, with everything a runner
// wraps around it taken off.
//
// Every runner this program reads spells an identity as a path to the
// declaration and then the declaration: pytest's `tests/test_log.py::test_follow`,
// vitest's `test/log.test.ts > follow > restores`, go's `TestFollow/restores`.
// The leaf is the last of those segments, less a parametrisation — pytest's
// `[case-2]` — which names one running of the check and not another declaration.
//
// An identity that is only a file names no declaration and returns nothing,
// which shuts the door rather than opening it on a guess.
func checkLeaf(check string) string {
	leaf := strings.TrimSpace(check)
	for _, cut := range []string{"::", " > ", " › ", " | "} {
		if at := strings.LastIndex(leaf, cut); at >= 0 {
			leaf = leaf[at+len(cut):]
		}
	}
	leaf = strings.TrimSpace(leaf)
	if at := strings.IndexByte(leaf, '['); at > 0 {
		leaf = strings.TrimSpace(leaf[:at])
	}
	// An identity that is only a path names a FILE and no declaration in it, and
	// this is asked before the subtest rule below so `tests/test_log.py` is not
	// read as a check called `tests`.
	if leaf == "" || checkFile(leaf) != "" {
		return ""
	}
	// A go subtest is `Parent/child` and the declaration is the parent. Only a
	// name with no spaces in it is read that way, because a runner that prints
	// prose can put a slash anywhere.
	if !strings.ContainsAny(leaf, " \t") {
		if before, _, found := strings.Cut(leaf, "/"); found {
			leaf = before
		}
	}
	return leaf
}
