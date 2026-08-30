package verify

// The third reading of the same tree: what a run did to a DEFINITION that kept
// its name, and what the rest of the project still does with it.
//
// The presence photograph beside this one answers "is the name still there".
// igel s12 is what a run does with the difference. That run replaced the module
// name `configs` — a plain dict — with an instance of a small class it wrote,
// and the name was there before and there after, so the photograph compared
// eight names, lost none, and said so. Every one of the twenty-four hidden tests
// then failed with `TypeError: 'Configs' object does not support item
// assignment`: the callers were still using it the way a dict is used, and the
// thing behind the name no longer answered to that.
//
// A NAME IS NOT A CONTRACT. The photograph reads presence, the check-level
// reading reads what somebody wrote a check for, and neither of them can see a
// definition whose SHAPE moved under the code that uses it. Nothing in this
// harness told the judge that a retained definition had been rewritten while its
// consumers still stood.
//
// So: the definitions this run's own diff TOUCHED, and the places elsewhere in
// the project that use them, with the syntactic shape of each use read off the
// token beside it. It is evidence and never a verdict — the shapes are a closed
// set read structurally, nothing here knows what a shape MEANS, and the judgment
// that `x["k"] = v` will not survive a class with no `__setitem__` is left to
// the reader that is paid to make judgements.
//
// IT IS READ OFF THE TWO PHOTOGRAPHS AND NEVER OFF A DIFF, and that is not a
// simplification — it is what makes it exist at all. A diff is written by one
// belt, for one leaf, under that leaf's own key; the node a gate judges is
// routinely a parent or a sibling, and on the belts a headless run actually uses
// no diff is derived at all. igel s14 proved it twice over: three grown leaves
// each measured the loss, and both of that job's gates were handed an account
// with no change text in it. The two surface readings, on the other hand, are
// taken by every belt, inherited across every round, and already remembered
// against the JOB — so a definition's own digest, compared between them, is a
// fact every judged node of every job can reach.
//
// It is FAILSAFE.md clause 1 and clause 2 both: read by structure rather than by
// a list of names, and sourced from the world — two readings of the tree —
// rather than from anything the component being checked says about itself. Every
// silence in it favours the work: no reader for the language, no baseline, no
// definition that moved, no consumer found, and nothing is said at all.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// consumerScanLimit bounds the walk that finds a name's usage sites. It is
	// scopeScanLimit's sibling and carries the same figure for the same reason:
	// a walk whose size is nobody's plan stops rather than going on forever, and
	// past it the answer is whatever was found — which is the narrower answer
	// and never a wrong one.
	consumerScanLimit = 6000
	// consumerReadBudget bounds the bytes one settlement reads looking for them.
	// Every candidate file has to be READ, because a usage site is a line and
	// not a filename, so this is the number that decides what the reading costs.
	// Two mebibytes is scopeReadBudget's figure and is a few hundred source
	// files; past it the walk stops and the consumers are partial, which can
	// only UNDERSTATE a count and never invent one.
	consumerReadBudget = 2 << 20
	// consumerSites bounds how many sites one name keeps. A name used in two
	// hundred places says everything it is going to say about the shape of its
	// use long before the cap, and the count a reader is shown is the count that
	// was kept — it says so.
	consumerSites = 200
	// consumerNamesWeighed bounds how many changed definitions one settlement
	// carries. It is surfaceNamesReported's sibling and the same size for the
	// same reason: a list is read to learn what SHAPE a change has, and eight
	// entries say that as well as eighty.
	consumerNamesWeighed = 8
)

// Span is a range of lines, 1-based and inclusive at both ends.
type Span struct {
	From int
	To   int
}

// Empty says this span names no line.
func (s Span) Empty() bool { return s.From <= 0 || s.To < s.From }

// Words is the span as a person reads it: "lines 12–40", or "line 12".
func (s Span) Words() string {
	if s.Empty() {
		return ""
	}
	if s.From == s.To {
		return "line " + strconv.Itoa(s.From)
	}
	return "lines " + strconv.Itoa(s.From) + "–" + strconv.Itoa(s.To)
}

// Site is one place the project uses a name, outside the files the run changed.
//
// Text is the whole line, trimmed, because the shape is a reading and the line
// is the evidence for it: a reader who disagrees with the shape can see what it
// was read off. Shape is the closed set below and nothing else.
type Site struct {
	File  string
	Line  int
	Text  string
	Shape string
}

// Where is the site as everything downstream spells it.
func (s Site) Where() string { return s.File + ":" + strconv.Itoa(s.Line) }

// ChangedDefinition is one public name this work REWROTE — the name is in both
// readings and the text of its declaration is not — with where it stood on
// either side of the work and what still uses it.
type ChangedDefinition struct {
	Name string
	File string
	// Before is where the declaration stood in the tree before the job's first
	// change, and After is where it stands now. Both come off the readings
	// themselves rather than off a diff's arithmetic, so both are the
	// declaration and not the neighbourhood it sits in.
	Before    Span
	After     Span
	Consumers []Site
}

// SiteGroup is one shape of use and the sites that have it, biggest group first.
type SiteGroup struct {
	Shape string
	Sites []Site
}

// Grouped is this definition's consumers by shape, largest group first and each
// group in file order. It is one reading shared by the block a judge is shown
// and the row the journal keeps, because two groupings of one list are two
// answers to one question.
func (d ChangedDefinition) Grouped() []SiteGroup {
	held := map[string][]Site{}
	for _, site := range d.Consumers {
		held[site.Shape] = append(held[site.Shape], site)
	}
	groups := make([]SiteGroup, 0, len(held))
	for shape, sites := range held {
		groups = append(groups, SiteGroup{Shape: shape, Sites: sites})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Sites) != len(groups[j].Sites) {
			return len(groups[i].Sites) > len(groups[j].Sites)
		}
		return groups[i].Shape < groups[j].Shape
	})
	return groups
}

// ChangedDefinitions is the public names this work REWROTE: present in the
// baseline reading and present now, under one file, with a different declaration
// digest.
//
// It is the same subtraction Removed is, over the same pair of readings, asking
// the other question. Removed asks which names went; this asks which of the ones
// that STAYED are no longer the same thing. Neither needs a runner, a diff or a
// model, and both are scoped to the run's own record for the same reason: a
// definition that moved in a file nobody touched moved some other way.
//
// A name declared twice under one file — an overload, a conditional
// re-definition — is passed over rather than guessed at. There is no way to say
// which of two declarations became which, and a reader that picked one would be
// reporting an arrangement it invented.
func ChangedDefinitions(root string, baseline Surface, record []string) []ChangedDefinition {
	if len(baseline) == 0 {
		return nil
	}
	files := ChangedSources(root, record)
	if len(files) == 0 {
		return nil
	}
	now := SurfaceOf(root, files)
	var changed []ChangedDefinition
	for _, file := range files {
		was, standing := soleDeclarations(baseline[file]), soleDeclarations(now[file])
		for name, after := range standing {
			before, known := was[name]
			if !known || before.Digest == after.Digest || after.Digest == 0 {
				continue
			}
			changed = append(changed, ChangedDefinition{Name: name, File: file,
				Before: Span{From: before.Line, To: before.End},
				After:  Span{From: after.Line, To: after.End}})
		}
	}
	// Ordered by file and then by where the declaration sits, so the same pair
	// of readings answers the same way twice and a reader meets the definitions
	// in the order the file spells them.
	sort.SliceStable(changed, func(i, j int) bool {
		if changed[i].File != changed[j].File {
			return changed[i].File < changed[j].File
		}
		return changed[i].After.From < changed[j].After.From
	})
	if len(changed) > consumerNamesWeighed {
		changed = changed[:consumerNamesWeighed]
	}
	return changed
}

// soleDeclarations keys one file's declarations by name, DROPPING every name the
// file declares more than once. See ChangedDefinitions for why.
func soleDeclarations(declared []Declaration) map[string]Declaration {
	held := make(map[string]Declaration, len(declared))
	twice := map[string]bool{}
	for _, declaration := range declared {
		if _, seen := held[declaration.Name]; seen {
			twice[declaration.Name] = true
			continue
		}
		held[declaration.Name] = declaration
	}
	for name := range twice {
		delete(held, name)
	}
	return held
}

// Consumers is where the project itself uses these names, OUTSIDE the lines this
// run changed, with the syntactic shape of each use.
//
// One walk for every name rather than one walk per name: the walk is what this
// costs, and a settlement weighing eight definitions must not read the tree
// eight times.
//
// A site is a WHOLE-IDENTIFIER occurrence — namesIdentifier's rule, the one that
// keeps `Log` out of `Logger` — of the name as the surface reader spells it, with
// an instance marker taken off, because `Igel().results_path` is reached by
// writing `.results_path` and the parentheses are this program's own notation. A
// dotted or scoped name is matched whole, which is deliberately strict: a caller
// that reaches a class attribute through `self` spells something this cannot
// recognise, and not recognising it costs a silence rather than a wrong finding.
//
// A LINE IN A FILE THIS RUN CHANGED IS NOT A CONSUMER. It is the work itself, or
// it sits beside the work in a file the run was editing, and counting either
// would report a run's own code as evidence against it. Excluding the whole file
// rather than the changed lines costs real consumers in a large edited file —
// which is a silence, and silence is the direction this reading is wrong in
// everywhere else.
func Consumers(root string, names []string, changed []string) map[string][]Site {
	root = strings.TrimSpace(root)
	wanted := map[string]string{}
	for _, name := range names {
		if needle := consumerNeedle(name); needle != "" {
			wanted[name] = needle
		}
	}
	if root == "" || len(wanted) == 0 {
		return nil
	}
	ours := make(map[string]bool, len(changed))
	for _, file := range changed {
		ours[filepath.ToSlash(filepath.Clean(strings.TrimSpace(file)))] = true
	}
	sites := map[string][]Site{}
	visited, budget := 0, consumerReadBudget
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if visited++; visited > consumerScanLimit || budget <= 0 {
			return filepath.SkipAll
		}
		if entry.IsDir() {
			if path != root && skipBuilt(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !consumerFile(entry.Name()) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		slashed := filepath.ToSlash(relative)
		if ours[slashed] {
			return nil
		}
		body, read := readSurfaceFile(path)
		budget -= len(body)
		if !read {
			return nil
		}
		readConsumers(slashed, body, wanted, sites)
		return nil
	})
	for name, found := range sites {
		if len(found) > consumerSites {
			sites[name] = found[:consumerSites]
		}
	}
	return sites
}

// readConsumers records one file's usage sites of every wanted name.
func readConsumers(file, body string, wanted map[string]string, into map[string][]Site) {
	code := codeLines(file, body)
	for index, line := range code {
		if line == "" {
			continue
		}
		number := index + 1
		for name, needle := range wanted {
			at := wholeIdentifierAt(line, needle)
			if at < 0 {
				continue
			}
			into[name] = append(into[name], Site{File: file, Line: number,
				Text: strings.TrimSpace(line), Shape: shapeAt(line, at, at+len(needle))})
		}
	}
}

// consumerNeedle is the name as a caller writes it: the surface reader's
// notation for an instance attribute taken off, and nothing else changed.
//
// An empty answer is a name this cannot look for, and a name it cannot look for
// has no consumers rather than the wrong ones.
func consumerNeedle(name string) string {
	needle := strings.TrimSpace(strings.Replace(name, "()", "", 1))
	if needle == "" || strings.ContainsAny(needle, " \t") {
		return ""
	}
	return needle
}

// wholeIdentifierAt is where this line names this identifier as an identifier
// rather than as a fragment of a longer one, or -1.
//
// It is namesIdentifier's rule — the characters either side of the match may not
// themselves be part of an identifier — answering WHERE rather than WHETHER,
// because the shape of a use is read from the token beside it. A dotted name is
// matched whole, so the boundary test applies to the ends of the whole spelling.
func wholeIdentifierAt(line, identifier string) int {
	if identifier == "" {
		return -1
	}
	for offset := 0; ; {
		index := strings.Index(line[offset:], identifier)
		if index < 0 {
			return -1
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
		// A dot before the match means this is somebody else's member of the
		// same name, which is a different thing entirely.
		if !identifierByte(before) && before != '.' && !identifierByte(after) {
			return at
		}
		offset = at + 1
	}
}

// ── The shapes ───────────────────────────────────────────────────────────────
//
// A CLOSED SET, READ FROM THE TOKEN BESIDE THE NAME AND NOTHING ELSE. Nothing
// here resolves a type, follows an import, or knows what any of these means; the
// whole of it is "what character comes next", which is the same register the
// surface readers work in and the same reason they can be trusted. An
// arrangement this cannot name is `use`, which says only that the name is there.

const (
	// ShapeUse is the answer for every arrangement this reader cannot name. It
	// is the honest one: the project mentions the name here and this program has
	// nothing structural to say about how.
	ShapeUse = "use"
	// ShapeCall is the name followed by an argument list.
	ShapeCall = "call"
	// ShapeSubscript is the name indexed, and ShapeSubscriptAssign is the name
	// indexed on the LEFT of an assignment — the one arrangement igel s12 broke
	// and the reason the two are told apart at all.
	ShapeSubscript       = "subscript"
	ShapeSubscriptAssign = "subscript-assign"
	// ShapeAttribute is the name with a member reached off it, carrying the
	// member: `attribute .get` and `attribute .results_path` are different facts
	// about what a caller expects to find behind a name.
	ShapeAttribute = "attribute "
	// ShapeIterate is the name iterated over, and ShapeInstantiate is the name
	// constructed with a language's own keyword for it.
	ShapeIterate     = "iterate"
	ShapeInstantiate = "instantiate"
)

// shapeAt is what this line does with the name that sits between at and end.
func shapeAt(line string, at, end int) string {
	before := strings.TrimRight(line[:at], " \t")
	if lastWord(before) == "new" {
		return ShapeInstantiate
	}
	rest := strings.TrimLeft(line[end:], " \t")
	switch {
	case strings.HasPrefix(rest, "("):
		return callShape(rest)
	case strings.HasPrefix(rest, "["):
		return subscriptShape(rest)
	case strings.HasPrefix(rest, "::"):
		if member := leadingIdentifier(rest[2:]); member != "" {
			return ShapeAttribute + "::" + member
		}
	case strings.HasPrefix(rest, "."):
		if member := leadingIdentifier(rest[1:]); member != "" {
			return ShapeAttribute + "." + member
		}
	}
	if lastWord(before) == "in" && strings.HasPrefix(strings.TrimSpace(line), "for") {
		return ShapeIterate
	}
	return ShapeUse
}

// callShape is a call and, where the argument list closes on this line, how many
// arguments it was given.
//
// An unclosed list is `call` with no count, because a count read off half a list
// would be a number that is wrong rather than a number that is missing.
func callShape(rest string) string {
	depth, arguments, spelled := 0, 0, false
	for index := 0; index < len(rest); index++ {
		switch rest[index] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth--; depth == 0 {
				if spelled {
					arguments++
				}
				return ShapeCall + "(" + strconv.Itoa(arguments) + " args)"
			}
		case ',':
			if depth == 1 {
				arguments++
			}
		default:
			if depth == 1 && rest[index] != ' ' && rest[index] != '\t' {
				spelled = true
			}
		}
	}
	return ShapeCall
}

// subscriptShape tells an index from an index that is being ASSIGNED INTO. The
// difference is one `=` that is not a comparison, and it is the whole of what
// igel s12 turned on.
func subscriptShape(rest string) string {
	depth := 0
	for index := 0; index < len(rest); index++ {
		switch rest[index] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth--; depth > 0 {
				continue
			}
			after := strings.TrimLeft(rest[index+1:], " \t")
			if strings.HasPrefix(after, "=") && !strings.HasPrefix(after, "==") &&
				!strings.HasPrefix(after, "=>") {
				return ShapeSubscriptAssign
			}
			return ShapeSubscript
		}
	}
	return ShapeSubscript
}

// lastWord is the identifier a span of text ends with, which is how a keyword
// standing in front of a name is recognised without tokenising anything.
func lastWord(text string) string {
	end := len(text)
	for end > 0 && identifierByte(text[end-1]) {
		end--
	}
	return text[end:]
}

// leadingIdentifier is the identifier a span of text begins with.
func leadingIdentifier(text string) string {
	end := 0
	for end < len(text) && identifierByte(text[end]) {
		end++
	}
	return text[:end]
}

// ── Which files are read, and which of their lines count ─────────────────────

// consumerFile says this file may hold a usage site: a source file in a language
// this program reads — checks included, because a check is a caller like any
// other — or a document whose fenced code blocks are.
func consumerFile(name string) bool {
	if surfaceLanguage(name) != "" {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

// codeLines is the file's lines, with everything that is not code blanked out.
//
// A source file is all code. A document is code only inside its fenced blocks:
// prose that happens to spell a name is not a caller, and a README sentence
// counted as a usage site would be a count nobody could act on. The lines are
// blanked rather than dropped so a site keeps the number the file's own editor
// would show.
func codeLines(file, body string) []string {
	lines := strings.Split(body, "\n")
	if surfaceLanguage(lastSegment(file)) != "" {
		return lines
	}
	fenced := false
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			lines[index] = ""
			continue
		}
		if !fenced {
			lines[index] = ""
		}
	}
	return lines
}
