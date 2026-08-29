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
// It is FAILSAFE.md clause 1 and clause 2 both: read by structure rather than by
// a list of names, and sourced from the world — the run's own diff and the files
// on disk — rather than from anything the component being checked says about
// itself. Every silence in it favours the work: no reader for the language, no
// declaration the diff overlaps, no consumer found, and nothing is said at all.

import (
	"os"
	"path/filepath"
	"regexp"
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

// Hunk is one span of one file that a diff replaced: where those lines stood in
// the tree before the work, and where they stand in the tree after it.
type Hunk struct {
	File   string
	Before Span
	After  Span
}

// Site is one place the project uses a name, outside the lines the run changed.
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

// ChangedDefinition is one public name whose DECLARATION this run's diff
// overlaps, with where it stood on either side of the work and what still uses
// it.
type ChangedDefinition struct {
	Name string
	File string
	// Before is the lines of the file, as it was, that the diff replaced where
	// this declaration stands. It is the hunk's own old-side range rather than a
	// reading of a tree nobody kept: the finished tree is on disk and the tree
	// before the work is not, so what can be said honestly about the old side is
	// what the diff itself says.
	Before Span
	// After is where the declaration stands in the finished tree, read by the
	// surface reader that also decided the name is public.
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

// hunkHeader is a unified diff's own account of which lines it replaced.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Hunks is every span a unified diff touched, keyed by the file it touched.
//
// It reads the diff's own headers and nothing else. A file the diff CREATES has
// no old side and a file it DELETES has no new side; both are read as what they
// are, because a created file's declarations are all new — nothing was reshaped
// under anybody — and a deleted file's names are the presence photograph's
// business, which already reports every one of them lost.
func Hunks(diff string) []Hunk {
	var hunks []Hunk
	file, live := "", false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			file, live = "", false
		case strings.HasPrefix(line, "+++ "):
			target := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if at := strings.IndexAny(target, "\t"); at >= 0 {
				target = target[:at]
			}
			// `/dev/null` on the new side is a deletion, and a deletion has no
			// finished file to read a declaration out of.
			if target == "/dev/null" {
				file, live = "", false
				continue
			}
			file, live = strings.TrimPrefix(strings.TrimPrefix(target, "b/"), "./"), true
		case live && strings.HasPrefix(line, "@@ "):
			match := hunkHeader.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			hunks = append(hunks, Hunk{File: filepath.ToSlash(file),
				Before: diffSpan(match[1], match[2]), After: diffSpan(match[3], match[4])})
		}
	}
	return hunks
}

// diffSpan is one side of a hunk header. A count the header omits is one line,
// which is the unified format's own rule; a count of zero is an insertion point
// rather than a range, and the line before it is the line it sits after.
func diffSpan(start, count string) Span {
	from, err := strconv.Atoi(start)
	if err != nil {
		return Span{}
	}
	length := 1
	if count != "" {
		if parsed, err := strconv.Atoi(count); err == nil {
			length = parsed
		}
	}
	if length == 0 {
		return Span{From: from, To: from}
	}
	return Span{From: from, To: from + length - 1}
}

// ChangedDefinitions is the public names whose DECLARATION this run touched: the
// declarations of the finished tree that a hunk of the run's own diff overlaps.
//
// It is scoped to the run's own record for the same reason the presence
// photograph is: a definition that moved in a file nobody touched moved some
// other way, and reporting it would hand a leaf a finding about something it
// never did. It is scoped to SOURCES rather than to checks because the question
// is what the work did to things other code uses, and a project's checks are
// used by the runner alone.
//
// Nothing is reported for a file with no reader, a file that is not on disk, or
// a declaration no hunk overlaps. Each of those is a silence that leaves the
// judge seeing exactly what it saw before this existed.
func ChangedDefinitions(root string, record []string, diff string) []ChangedDefinition {
	root = strings.TrimSpace(root)
	if root == "" || strings.TrimSpace(diff) == "" {
		return nil
	}
	sources := map[string]bool{}
	for _, path := range ChangedSources(root, record) {
		sources[path] = true
	}
	if len(sources) == 0 {
		return nil
	}
	touched := map[string][]Hunk{}
	for _, hunk := range Hunks(diff) {
		if sources[hunk.File] {
			touched[hunk.File] = append(touched[hunk.File], hunk)
		}
	}
	var changed []ChangedDefinition
	for file, hunks := range touched {
		if surfaceLanguage(lastSegment(file)) == "" {
			continue
		}
		body, read := readSurfaceFile(filepath.Join(root, filepath.FromSlash(file)))
		if !read {
			continue
		}
		for _, declaration := range DeclarationsIn(file, body) {
			before := Span{}
			for _, hunk := range hunks {
				if !declaration.Overlaps(hunk.After.From, hunk.After.To) {
					continue
				}
				before = widen(before, hunk.Before)
			}
			if before.Empty() {
				continue
			}
			changed = append(changed, ChangedDefinition{
				Name: declaration.Name, File: file, Before: before,
				After: Span{From: declaration.Line, To: declaration.End}})
		}
	}
	// Ordered by file and then by where the declaration sits, so the same tree
	// answers the same way twice and a reader meets the definitions in the order
	// the file spells them.
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

// widen is the union of two spans, treating an empty one as nothing.
func widen(held, add Span) Span {
	if add.Empty() {
		return held
	}
	if held.Empty() {
		return add
	}
	return Span{From: min(held.From, add.From), To: max(held.To, add.To)}
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
// Lines INSIDE the run's own hunks are not consumers. They are the work itself,
// and counting them would report the run's own new code as evidence against it.
func Consumers(root string, names []string, touched []Hunk) map[string][]Site {
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
	changed := map[string][]Span{}
	for _, hunk := range touched {
		if !hunk.After.Empty() {
			changed[hunk.File] = append(changed[hunk.File], hunk.After)
		}
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
		body, read := readSurfaceFile(path)
		budget -= len(body)
		if !read {
			return nil
		}
		readConsumers(slashed, body, wanted, changed[slashed], sites)
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
func readConsumers(file, body string, wanted map[string]string,
	skip []Span, into map[string][]Site,
) {
	code := codeLines(file, body)
	for index, line := range code {
		if line == "" {
			continue
		}
		number := index + 1
		if inAnySpan(number, skip) {
			continue
		}
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

// inAnySpan says this line is inside one of the ranges the run changed.
func inAnySpan(line int, spans []Span) bool {
	for _, span := range spans {
		if !span.Empty() && line >= span.From && line <= span.To {
			return true
		}
	}
	return false
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
