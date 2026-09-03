package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// WHAT A NODE NAMES IS MEASURED AGAINST WHAT ONE WORKER CAN HOLD, AND WHAT IS
// MEASURED IS THE MATERIAL THE NODE WILL READ — NOT THE FILE ITS WORDS MENTION.
//
// Context pressure used to be a judgment. The sizing pass was asked whether
// "the node cannot be brought to an end inside what one worker can hold" — a
// model, guessing, about a quantity that is sitting on the disk in front of it.
// Measured on the brief in issue #384: three lanes of work over one 3,500-line
// file planned as ONE leaf, which then ran out of context reading its own
// subject, and the replan after the exhaustion drew the same one leaf again.
// Nothing in the run was wrong about the work. Everything in it was wrong about
// a number nobody had bothered to read.
//
// So it is read. But reading the number is only half of it, and the other half
// is which number. A first version of this pass read the file NAMES out of a
// node's sources, stat'ed each one whole, and summed the sizes; on a plan that
// had divided a wide brief correctly — one lane per block of one big file — it
// reported `2 files, 165.4 KB in all … 5.2 times what one worker can hold` on a
// node whose own sources said `register.txt: header lines, North block heading,
// 1,160 North records`. Every correctly divided lane was then stamped oversized
// and handed over whole, which is the exact state the measurement was added to
// prevent (issue #480). A node's source line carries its SCOPE, and the scope is
// the material:
//
//   - A bare file name is the whole file. "rewrite corpus.txt in three lanes"
//     names all of corpus.txt, and nothing about the sentence says otherwise.
//   - A file name followed by a scoping mark — a colon, a dash, a bracket — is
//     scoped by the words after it, and contributes only the share those words
//     name. See scopedShare for exactly which shapes of scope are read.
//   - A scope this pass cannot resolve to a share contributes NOTHING. That is
//     the pass's own standing rule for anything it cannot weigh: a guess about
//     unmeasured material is the thing this replaces, and it is no better a
//     guess for being an over-estimate.
//
// Everything here is silent when there is nothing to measure. No workspace, a
// goal that names no file that exists, or a scope nobody can resolve renders
// zero bytes and changes no verdict.

// Reach is what one worker holds at a time, and the workspace whose files the
// words of a goal are measured against.
//
// The two travel together because neither is a measurement on its own: bytes
// with no workspace has nothing to weigh, and a workspace with no window has
// nothing to weigh it against.
type Reach struct {
	Dir   string
	Bytes int
}

// ReachFor derives one reach from the workspace the terrain was drawn from and
// the window of the model the work will run on. An empty workspace or an
// unmeasurable window yields a reach that measures nothing, which is the whole
// of the compatibility story.
func ReachFor(workspace string, contextTokens int) Reach {
	return Reach{Dir: strings.TrimSpace(workspace), Bytes: ctxbudget.ObservationBytes(contextTokens)}
}

// known reports whether this reach can weigh anything at all.
func (r Reach) known() bool { return r.Dir != "" && r.Bytes > 0 }

// Measurement is one reading: how much the material named in some words weighs,
// and what one worker holds beside it. It is a value rather than a pair of
// numbers passed around because the two are only ever meaningful together.
type Measurement struct {
	Files int
	Bytes int
	Reach int
}

// Taken reports whether anything was measured. A goal that names no file that
// exists is not a small goal — it is a goal nothing was measured about — and
// every consumer below treats the two differently.
func (m Measurement) Taken() bool { return m.Files > 0 && m.Reach > 0 }

// Exceeds is the whole rule in one predicate: the named material is larger than
// what one worker holds.
func (m Measurement) Exceeds() bool { return m.Taken() && m.Bytes > m.Reach }

// Line is the measurement in a person's words, for the prompt block every pass
// that decides shape already shares. Nothing was measured renders nothing at
// all, down to the newline.
//
// It states both figures and their ratio and stops. There is no instruction in
// it, and that is deliberate: the passes that read it each have their own rule
// about what a measurement means for them, written in their own prompts, and a
// second instruction smuggled in beside the numbers would be that rule stated
// twice and drifting.
func (m Measurement) Line() string {
	if !m.Taken() {
		return ""
	}
	measured := fmt.Sprintf("MEASURED — the material this goal names by name is %s, %s in all. "+
		"One worker holds %s of material at a time, so what is named ",
		plural(m.Files, "file"), terrainSize(int64(m.Bytes)), terrainSize(int64(m.Reach)))
	if !m.Exceeds() {
		return measured + "fits inside one worker."
	}
	return measured + fmt.Sprintf("is %.1f times what one worker can hold.", float64(m.Bytes)/float64(m.Reach))
}

// namedCandidates bounds how many words of one text are worth a syscall. A goal
// is prose and a node's sources are a short list; past this the text is not
// naming material, it is prose that happens to contain dots, and the pass stops
// rather than walking the disk on its behalf.
const namedCandidates = 64

// shareReadCeiling bounds what the share reader will OPEN. A stat is free and a
// read is not, so a file past this size is never opened to resolve a scope —
// and, per the law above, a scope that could not be resolved contributes
// nothing. A bare name is unaffected: it is stat'ed and weighed whole at any
// size, which is what keeps the arithmetic guarantee on a genuinely over-large
// leaf (issue #384) exactly as strong as it was.
const shareReadCeiling = 8 << 20

// reading is one file some words name, and how much of that file they name.
//
// Weighed is the difference between nothing and nothing-that-could-be-weighed,
// and both consumers need it, in opposite directions. The sum takes only what
// was weighed, because an unresolvable scope may not contribute a guess. The
// sibling count takes every reading, weighed or not, because a lane whose scope
// happens to be unreadable still NAMES the file its siblings name, and the
// signature of a division is the naming.
type reading struct {
	Name    string
	Bytes   int
	Weighed bool
	// Scoped says the words named a PART of this file rather than the file. It
	// is what lets one node's several mentions of one file be reconciled: see
	// narrower.
	Scoped bool
}

// namedScope is one file name as some words spell it, together with the words
// that scope it — empty when the name stands bare.
type namedScope struct {
	Name  string
	Scope string
	// Marked says which of the two ways this scope was written, and the two are
	// not read the same way when they resolve to nothing. See namedScopes.
	Marked bool
}

// scopingMark is what turns a file name into a scoped reference: the name, then
// a colon, a dash or an opening bracket, then the words that say which part of
// the file is meant. It is deliberately a small, punctuated set rather than
// "any words that follow", because ANY WORDS THAT FOLLOW IS EVERY SENTENCE — a
// goal reading "rewrite corpus.txt in three lanes" names the whole of
// corpus.txt, and a reader that treated "in three lanes" as a scope would
// measure nothing anywhere. A plain dash must be followed by a space so that it
// is a separator and not the middle of a name.
var scopingMark = regexp.MustCompile(`^[ \t]*(?::|[—–]|-[ \t]|[(\[])[ \t]*`)

// namedScopes is THE reading of how a piece of text divides into file names and
// the scopes around them, and it is one function because a second copy of this
// rule is a second answer to "does this source name a region or a file".
//
// The names themselves come from the one reader the tree already has for that
// question, verify.NamedPaths, which knows the things a second copy would have
// had to learn again — that an extension's shape is what keeps "e.g." and a
// version number out of a listing of files. What is added here is only WHERE
// each name sits, so that the words between one name and the next can be read
// as the first one's scope.
func namedScopes(text string) []namedScope {
	names := verify.NamedPaths(text)
	scopes := make([]namedScope, 0, len(names))
	// Where each name ends, so that a name's scope can be closed at the start of
	// the next one. A name the cleaned spelling cannot be found by is recorded
	// with no scope, which reads it as bare — the reading this pass has always
	// given a name it knows nothing else about.
	ends := make([]int, 0, len(names))
	starts := make([]int, 0, len(names))
	cursor := 0
	for _, name := range names {
		offset := strings.Index(text[cursor:], name)
		if offset < 0 {
			scopes = append(scopes, namedScope{Name: name})
			starts = append(starts, -1)
			ends = append(ends, -1)
			continue
		}
		start := cursor + offset
		cursor = start + len(name)
		scopes = append(scopes, namedScope{Name: name})
		starts = append(starts, start)
		ends = append(ends, cursor)
	}
	// Where the words in front of a name begin: after the name before it, or at
	// the start of the text.
	at := 0
	for index := range scopes {
		if ends[index] < 0 {
			continue
		}
		stop := len(text)
		for next := index + 1; next < len(scopes); next++ {
			if starts[next] >= 0 {
				stop = starts[next]
				break
			}
		}
		rest := text[ends[index]:stop]
		mark := scopingMark.FindString(rest)
		if mark == "" {
			// NO MARK, SO THE WORDS BEFORE THE NAME ARE ITS SCOPE. A source
			// entry is a phrase and its name is often the last thing in it —
			// "The 30 rewritten '## Chapter N — Title' headings in HANDBOOK.md"
			// says exactly what it reads and says it all before the file it
			// reads it from. Measured at the plan door: read as a bare name that
			// lane was charged the whole 84.8 KB handbook for thirty heading
			// lines.
			//
			// The asymmetry with a marked scope is the safety and is deliberate.
			// A mark is somebody saying "here comes the part I mean", so a
			// marked scope that resolves to nothing leaves the file unweighed.
			// Words merely standing in front of a name say no such thing, so
			// when nothing weighable is in them the name is just a name and the
			// file is weighed whole — which is what keeps "rewrite corpus.txt in
			// three lanes" a statement about all of corpus.txt, and the leaf of
			// issue #384 corrected.
			before := strings.TrimSpace(text[at:starts[index]])
			if strings.ContainsFunc(before, isWordRune) {
				scopes[index].Scope = before
			}
			at = ends[index]
			continue
		}
		at = ends[index]
		// A MARK WITH NO WORDS AFTER IT IS NOT A SCOPE. "large.txt:" at the end
		// of a line, or "large.txt ()", says nothing at all about which part of
		// the file is meant, so there is nothing here to resolve and the name is
		// read as what it is: the file, bare. It is deliberately not read as an
		// unresolvable scope — an unresolvable scope weighs nothing, and letting
		// a stray colon delete a 144 KB file from the measurement would hand the
		// #384 leaf back its exemption for a piece of punctuation. Words is what
		// it takes, so a run of punctuation is measured as none.
		scope := strings.TrimRight(strings.TrimSpace(rest[len(mark):]), " \t)]")
		if !strings.ContainsFunc(scope, isWordRune) {
			continue
		}
		scopes[index].Scope, scopes[index].Marked = scope, true
		// AND A MARKED SCOPE IS NOT ALSO THE WORDS IN FRONT OF THE NEXT NAME.
		// This name has claimed the text between it and whatever comes next, so
		// there is nothing left standing in front of the next name — without
		// this, "small.md: lines 1-2; huge.md" read "lines 1-2" a second time as
		// huge.md's scope and weighed a whole over-large file as two lines of
		// somebody else's material.
		at = stop
	}
	return scopes
}

// isWordRune is what makes a scope words rather than punctuation.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Measure weighs the material these words name, once.
//
// It is deliberately stateless. The alternative is a cache on a value that is
// copied into every pass of the build, which is shared mutable state bought to
// save a handful of syscalls on paths the operating system has already cached.
func (r Reach) Measure(texts ...string) Measurement {
	return r.weigh(r.readings(texts...))
}

// weigh sums readings into the one value the rest of the planner reads.
func (r Reach) weigh(readings []reading) Measurement {
	if !r.known() {
		return Measurement{}
	}
	measurement := Measurement{Reach: r.Bytes}
	for _, one := range readings {
		if !one.Weighed {
			continue
		}
		measurement.Files++
		measurement.Bytes += one.Bytes
	}
	return measurement
}

// readings is the measurement itself: what these words name, and how much of
// each name they name.
//
// Each name is looked up where the terrain was drawn from, and a regular file
// that is really there contributes either its whole size — the name stood bare —
// or the share its scope resolves to. A name that resolves to nothing is not a
// reading at all; a scope that resolves to no share is a reading of a file that
// nothing could weigh, which is not the same thing and is not the same zero.
//
// A FILE MENTIONED MORE THAN ONCE IN ONE NODE IS RECONCILED BY narrower, and
// the mention order never matters. Two scoped mentions give the larger, because
// the node reads both. A scoped mention beside a bare one gives the SCOPED one,
// because a node that writes the file's name and then the parts of it it reads
// has said what it reads — the bare name is the file being named, not a claim
// to all of it.
func (r Reach) readings(texts ...string) []reading {
	if !r.known() {
		return nil
	}
	var readings []reading
	at := map[string]int{}
	budget := namedCandidates
	for _, text := range texts {
		for _, named := range namedScopes(text) {
			if budget == 0 {
				return readings
			}
			budget--
			if !insideWorkspace(named.Name) {
				continue
			}
			path := filepath.Join(r.Dir, named.Name)
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			mention := reading{Name: named.Name, Bytes: int(info.Size()), Weighed: true}
			if named.Scope != "" {
				share, resolved := scopedShare(path, info.Size(), named.Scope)
				switch {
				case resolved:
					mention = reading{Name: named.Name, Bytes: share, Weighed: true, Scoped: true}
				case named.Marked:
					// A marked scope nobody could resolve leaves the file
					// unweighed. Words merely standing in front of the name fall
					// back to the bare reading above.
					mention = reading{Name: named.Name}
				}
			}
			index, mentioned := at[named.Name]
			if !mentioned {
				at[named.Name] = len(readings)
				readings = append(readings, mention)
				continue
			}
			readings[index] = narrower(readings[index], mention)
		}
	}
	return readings
}

// narrower reconciles two mentions of one file inside one node.
//
// WITHIN ONE NODE, A SCOPED MENTION NARROWS A BARE MENTION OF THE SAME FILE.
// Measured at the plan door: a lane sourced `HANDBOOK.md ; The line '#
// Handbook' ; The 30 rewritten '## Chapter N — Title' headings in HANDBOOK.md`,
// and taking the largest mention charged it the whole 84.8 KB for the bare
// first one — a veto on a lane that reads one line and thirty headings. The
// node had already said what it reads. So a weighable scoped mention is the
// file's share, and a bare mention is the whole file only where the node has no
// weighable scoped mention of it.
//
// It is one file at a time and says nothing about a node's other files: a node
// sourcing a bare over-large `huge.txt` beside a scoped `shared.md: lines 1-10`
// is still weighed the whole of huge.txt, because nothing in it narrowed THAT
// name.
func narrower(standing, mention reading) reading {
	standingScoped := standing.Scoped && standing.Weighed
	mentionScoped := mention.Scoped && mention.Weighed
	switch {
	case mentionScoped && standingScoped:
		if mention.Bytes > standing.Bytes {
			return mention
		}
		return standing
	case mentionScoped:
		return mention
	case standingScoped:
		return standing
	case !standing.Weighed:
		if mention.Weighed {
			return mention
		}
		return standing
	case mention.Weighed && mention.Bytes > standing.Bytes:
		return mention
	}
	return standing
}

// insideWorkspace is the one thing this pass asks of a name that the shared
// reader does not.
//
// WHICH TOKENS OF A TEXT READ AS THE NAME OF A FILE IS ONE QUESTION AND IT HAS
// ONE ANSWER: verify.NamedPaths, which the revision record and the resident's
// focus already read. What is left over is not about reading text at all. It is
// about this package's own boundary: a plan measured against a path that climbs
// out of the directory the terrain was drawn from is measuring somebody else's
// material, so a name that leaves the workspace is dropped rather than resolved.
// The shared reader already trims a leading slash and a scheme away, so what
// reaches here is relative and only the climb is left to catch.
func insideWorkspace(name string) bool {
	return name != ".." && !strings.HasPrefix(name, "../") && !strings.Contains(name, "/../")
}

// THE SCOPE SHAPES THIS PASS READS, AND NOTHING ELSE IS A SCOPE.
//
// Every one of them is resolved from the file's OWN BYTES, by reading it. There
// is no model call here, no fuzzy match, and no number reported for material
// that was not read:
//
//   - A LINE RANGE — "lines 40-320", "lines 40 to 320". The bytes of exactly
//     those lines, which is exact.
//   - A COUNT OF LINE-SHAPED THINGS — "1,160 North records", "40 rows". The
//     count is exact and the file says what a line of it weighs, so the share is
//     that many of the file's own mean line. The source said HOW MANY lines and
//     not WHICH, and this is the file answering the half it can answer.
//   - A COUNT OF HEADINGS — "all 30 `## chapter N: …` heading lines". Read
//     exactly rather than averaged, because it can be: the file says which of
//     its lines are headings, so these are the bytes of the lines themselves.
//   - A NAMED BLOCK OR HEADING — "the North block heading", "the `## chapter 3`
//     section". A heading line of the file whose label the scope spells, and
//     the extent from it to the next heading at or above its rank.
//
// Where a scope names more than one of these the LARGEST resolved share is the
// answer: they are several descriptions of one region — the North block heading
// and the 1,160 records under it are the same forty-eight kilobytes said twice —
// and the largest is the one that spans the region rather than a part of it.
//
// ANYTHING ELSE IS NOT A SCOPE THIS PASS CAN RESOLVE, and its file is not
// measured at all. "CONVENTIONS.md: rulings on date format and scope" names a
// region of a file in words no arithmetic reaches, so it weighs nothing here.
//
// THE KNOWN EDGE OF THAT RULE, so that nobody rediscovers it as a bug: a scope
// can say the whole file in words, and then the whole file goes unweighed. One
// node sourcing `register.txt (all three blocks, every byte except the dates
// unchanged)` was measured at the plan door and came back unmeasured, so the
// sizer's own "borderline" stood — for a node that will in fact read all 144 KB
// of the register. That is the law as written and not an oversight: an
// over-estimate is still a guess, and this pass's standing rule is that it does
// not guess. The sizer keeps such a node, as it kept every node before any of
// this existed.
func scopedShare(path string, size int64, scope string) (int, bool) {
	if size <= 0 || size > shareReadCeiling {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	text := string(data)
	lines := strings.SplitAfter(text, "\n")
	if count := len(lines); count > 0 && lines[count-1] == "" {
		lines = lines[:count-1]
	}
	if len(lines) == 0 {
		return 0, false
	}
	share, resolved := 0, false
	for _, candidate := range []int{
		lineRangeShare(lines, scope),
		lineCountShare(lines, len(text), scope),
		namedBlockShare(lines, len(text), scope),
	} {
		if candidate <= 0 {
			continue
		}
		resolved = true
		if candidate > share {
			share = candidate
		}
	}
	if !resolved {
		return 0, false
	}
	if share > len(text) {
		share = len(text)
	}
	return share, true
}

// lineRangeShare reads "lines 40-320" and returns what those lines weigh. The
// range is clamped to the file rather than refused when it runs off the end: a
// source that overshoots by a line has still scoped a region, and the file is
// the authority on how far it goes.
func lineRangeShare(lines []string, scope string) int {
	match := lineRange.FindStringSubmatch(scope)
	if match == nil {
		return 0
	}
	first, second := countIn(match[1]), countIn(match[2])
	if first <= 0 || second < first {
		return 0
	}
	if second > len(lines) {
		second = len(lines)
	}
	bytes := 0
	for index := first - 1; index < second; index++ {
		bytes += len(lines[index])
	}
	return bytes
}

// lineCountShare reads "1,160 North records" or "all 30 heading lines" and
// returns what that many of this file's lines weigh. The unit words are the
// line-shaped ones on purpose: a record, a row, an entry, an item and a line
// are all one line of a file, while a "section" or a "chapter" is a count of
// regions and says nothing about their size.
//
// A count of HEADINGS is read exactly, because it can be: the file says which
// of its lines are headings, so the share is the bytes of that many of them and
// not an estimate at all. That distinction is worth the branch — a handbook's
// thirty heading lines are a thousand bytes of an eighty-five-kilobyte file,
// and the file's mean line is five times too generous about them.
//
// Every other count is the file's own mean line, that many times over. It is an
// average and it is named as one: the source said HOW MANY lines and not WHICH,
// so the file cannot be asked which bytes are meant, and the mean is taken over
// lines this pass did read — the whole file — rather than assumed. The count is
// clamped to the file, so the answer can never be a statement about lines the
// file does not have.
func lineCountShare(lines []string, size int, scope string) int {
	match := lineCount.FindStringSubmatch(scope)
	if match == nil {
		return 0
	}
	count := countIn(match[1])
	if count <= 0 {
		return 0
	}
	if headingCount.MatchString(scope) {
		if bytes := headingLinesShare(lines, count, headingRankIn(scope)); bytes > 0 {
			return bytes
		}
	}
	if count > len(lines) {
		count = len(lines)
	}
	return count * (size / len(lines))
}

// headingLinesShare is the bytes of the file's first count heading lines of the
// given rank, or of all of them when it has fewer than that. Nothing is
// estimated here: these are the lines themselves.
//
// The rank is what keeps it to the headings the scope actually named. A source
// that writes its pattern out — "all 30 `## chapter N: …` heading lines" —
// spells the rank in the pattern's own marks, and taking the first thirty
// headings of ANY rank would have charged that node a `# Handbook` line it never
// mentioned. A scope that spells no rank takes headings of every rank, which is
// all a scope that did not say can ask for.
func headingLinesShare(lines []string, count, rank int) int {
	bytes, found := 0, 0
	for _, line := range lines {
		lineRank, _, ok := headingLine(line)
		if !ok || (rank > 0 && lineRank != rank) {
			continue
		}
		bytes += len(line)
		if found++; found == count {
			break
		}
	}
	return bytes
}

// headingRankIn reads the heading rank a scope spells inside a backticked
// pattern — the `##` of "`## chapter N: …`" — and zero when it spells none. It
// is the pattern's literal marks and never an inference from its words.
func headingRankIn(scope string) int {
	for _, pattern := range spelledPatterns(scope) {
		if marks := len(pattern) - len(strings.TrimLeft(pattern, "#")); marks > 0 && marks <= 6 {
			return marks
		}
	}
	return 0
}

// spelledPatterns are the spans a scope writes a pattern or a heading out in.
// Backticks are the convention a source list is usually written with and single
// quotes are the one a model reaches for when it is writing prose — "the 30
// rewritten '## Chapter N — Title' headings" — and reading only the first left
// that scope with no rank, which charged the lane the file's `# Handbook` title
// beside twenty-nine of the thirty headings it actually named.
func spelledPatterns(scope string) []string {
	var patterns []string
	for _, span := range []*regexp.Regexp{backticked, singleQuoted} {
		for _, match := range span.FindAllStringSubmatch(scope, -1) {
			patterns = append(patterns, strings.TrimSpace(match[1]))
		}
	}
	return patterns
}

// namedBlockShare finds the heading the scope spells and returns what the scope
// named of it. The candidates are the things a scope spells a heading with — a
// backticked span, or a capitalised word — and each is resolved by EQUALITY
// against a heading line of the file, never by containment: a scope that
// mentions "North" measures the North block or nothing, and never a line that
// happens to have the word in it.
//
// Two laws bound what a matched heading is worth, and both were written after
// the same lane was refused at the plan door for the second time.
//
// A HEADING NAMED AS A LINE WEIGHS THAT LINE. The lane that builds a contents
// section sourced `HANDBOOK.md (the '# Handbook' line and the list of all
// chapter headings to construct contents)`. It will read one line and thirty
// headings. This pass matched `Handbook` against the file's `# Handbook`
// heading and handed back its SECTION, and a person who writes "the '# Handbook'
// line" has said which of the two they meant.
//
// A HEADING WHOSE SECTION IS THE WHOLE FILE IS NOT A SHARE. `# Handbook` is a
// rank-one title over thirty rank-two chapters, so its section runs to the end
// of the file: 84.8 KB, the whole document, charged to a lane that touches one
// line of it. A scope that resolves to the entire file has said nothing narrower
// than the file, so it is no reading at all and this shape declines it — the
// name is left unmeasured rather than charged whole through a scope. THE BARE
// NAME IS UNAFFECTED: a source that says `HANDBOOK.md` and stops still weighs
// every byte of it, which is what keeps the guarantee from issue #384.
func namedBlockShare(lines []string, size int, scope string) int {
	// Each label with the two ways the scope may have named it. They are kept
	// apart rather than reduced to one flag because a scope may do both — "the
	// North section and the North heading line" — and a node that names both
	// will read both, so the section is what it weighs.
	labels := map[string]mention{}
	note := func(label string) {
		if label == "" {
			return
		}
		asLine, plain := mentionsOf(scope, label)
		was := labels[label]
		labels[label] = mention{line: was.line || asLine, section: was.section || plain}
	}
	for _, pattern := range spelledPatterns(scope) {
		note(headingKey(pattern))
	}
	for _, match := range capitalised.FindAllString(scope, -1) {
		note(headingKey(match))
	}
	if len(labels) == 0 {
		return 0
	}
	largest := 0
	for index, line := range lines {
		rank, label, ok := headingLine(line)
		if !ok {
			continue
		}
		named, ok := labels[label]
		if !ok {
			continue
		}
		bytes := 0
		if named.line {
			bytes = len(line)
		}
		if named.section {
			section := len(line)
			for reach := index + 1; reach < len(lines); reach++ {
				if next, _, ok := headingLine(lines[reach]); ok && next <= rank {
					break
				}
				section += len(lines[reach])
			}
			// A section that is the whole document is not a share of it, so it
			// contributes nothing — and a line reading beside it still stands.
			if section < size && section > bytes {
				bytes = section
			}
		}
		if bytes > largest {
			largest = bytes
		}
	}
	return largest
}

// mention is the two ways a scope can name one heading, kept apart because a
// scope may do both and the readings differ by three orders of magnitude.
type mention struct {
	line    bool
	section bool
}

// mentionsOf reads how a scope named this heading: as a LINE — the label, then
// quotes or marks, then the word "line" — or plainly, which names the section
// under it. A scope that does both gets both, and the caller takes the larger,
// because a node that names both will read both.
//
// The line marker is deliberately tight: the word has to follow the label, not
// merely appear somewhere in the scope, because "header lines, North block
// heading, 1,160 North records" names the North SECTION and mentions lines in
// the same breath about something else entirely.
func mentionsOf(scope, label string) (asLine, plain bool) {
	quoted := regexp.QuoteMeta(label)
	every, err := regexp.Compile(`(?i)` + quoted)
	if err != nil {
		return false, true
	}
	marked, err := regexp.Compile(`(?i)` + quoted + "[`'\"\\s#:,.\\-]{0,6}(?:heading\\s+)?lines?\\b")
	if err != nil {
		return false, true
	}
	all := len(every.FindAllString(scope, -1))
	lines := len(marked.FindAllString(scope, -1))
	// A label the scope spells but this reader cannot find again — the trimming
	// that made the key was not reversible — is read the way every label was
	// read before line-naming existed: as its section.
	if all == 0 {
		return false, true
	}
	return lines > 0, all > lines
}

// headingLine reads one line as a heading and says at what rank. Markdown marks
// give the rank directly; a line that is nothing but a short bare label — no
// sentence punctuation, no columns — is a heading of the top rank, which is how
// a plain register writes `North` over its block. Everything else is content.
func headingLine(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return 0, "", false
	}
	if marks := len(trimmed) - len(strings.TrimLeft(trimmed, "#")); marks > 0 {
		if marks > 6 {
			return 0, "", false
		}
		label := headingKey(strings.TrimLeft(trimmed, "#"))
		if label == "" {
			return 0, "", false
		}
		return marks, label, true
	}
	if !bareLabel.MatchString(trimmed) {
		return 0, "", false
	}
	return 1, headingKey(trimmed), true
}

// headingKey is the one spelling a heading and the scope that names it are
// compared in: trimmed of its marks and its trailing punctuation, and cased
// down. It is equality on this key and nothing looser.
func headingKey(text string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(text), " \t#:*_-"))
}

// countIn reads a written count — "1,160" — as a number.
func countIn(text string) int {
	value, err := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(text), ",", ""))
	if err != nil {
		return 0
	}
	return value
}

var (
	lineRange = regexp.MustCompile(`(?i)\blines?\s+(\d[\d,]*)\s*(?:-|–|—|to|through|\.\.)\s*(\d[\d,]*)\b`)
	// The unit noun may sit a few words after the count — "1,160 North records",
	// "all 30 `## chapter N: …` heading lines" — so a bounded, ungreedy run of
	// words is allowed between them and the nearest unit wins.
	lineCount    = regexp.MustCompile(`(?i)\b(\d[\d,]*)(?:\s+\S+){0,6}?\s+(?:records?|rows?|entries|entry|lines?|items?|headings?)\b`)
	headingCount = regexp.MustCompile(`(?i)\bheadings?\b`)
	backticked   = regexp.MustCompile("`([^`]{1,80})`")
	singleQuoted = regexp.MustCompile(`'([^']{1,80})'`)
	capitalised  = regexp.MustCompile(`\b[A-Z][A-Za-z0-9'-]{2,}\b`)
	bareLabel    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _'-]{0,60}$`)
)

// admissible drops the spine samples the measurement has already ruled out.
//
// It is a levelling step and not a second vote, which is the distinction that
// matters. levelled turns what a sample SAYS into the shape the rest of the
// planner reads; this says which shapes are available to be read at all. When
// the material a goal names is larger than one worker holds, "one stage" is not
// a shape this goal has — one stage is one worker holding all of it — so a
// sample that came back with one is set aside before the medoid rather than
// argued with inside it, and the medoid then picks the most typical of the
// answers that are actually available exactly as it always did.
//
// THE MEASUREMENT IT READS IS THE GOAL'S AND NEVER A NODE'S. A goal that names
// more than one worker holds is a true statement about the whole plan — there
// are no nodes yet when the spine runs, and nothing here is a verdict about one
// of them. The per-node question has one answer and one place that computes it,
// correctBeyondReach, which weighs a node against its siblings and stores what
// it finds on Node.BeyondReach; this pass never asks it and could not, because
// a division does not exist until the spine has drawn the stages it is made of.
//
// If every sample said one stage, every sample is kept. The spine cannot be
// made to invent a gate it did not find, and the measurement has one more place
// downstream — the undivided shortcut — where the same fact about the same goal
// is applied to the shape of the whole build.
func admissible(candidates [][]Stage, named Measurement) [][]Stage {
	if !named.Exceeds() {
		return candidates
	}
	staged := make([][]Stage, 0, len(candidates))
	for _, candidate := range candidates {
		if len(candidate) > 1 {
			staged = append(staged, candidate)
		}
	}
	if len(staged) == 0 {
		return candidates
	}
	return staged
}

// correctBeyondReach is the measurement overruling the judgment, on the one
// pass whose judgment it can check — and, in its second half, the measurement
// declining to.
//
// The sizing model is asked whether a node can be brought to an end inside what
// one worker holds. Where the node names its own material and that material has
// been weighed, the answer is not the model's to give: a node whose named
// sources are larger than one worker's window may not be atomic, whatever the
// ruler said about the breadth of its subject. The verdict is corrected to
// oversized — which is the word the rest of the planner already routes on — and
// the reason is journaled on the node, where a splice clears it if the node does
// divide and a reader finds it if it does not.
//
// AND IT NEVER OVERRULES A DIVISION. A node the ruler called atomic, whose
// sources scope a REGION of a file that a sibling node also scopes a region of,
// is one lane of a division somebody has already made correctly: three lanes
// over one 144 KB register, each owning one 48 KB block, are three nodes that
// each name more than one worker holds and are nonetheless exactly right. There
// is nothing left for this pass to correct there — expansion cannot divide a
// lane over one file any further, so the correction only converts a good plan
// into a refusal (issue #480). A node that names a whole file larger than one
// worker's window has no such division behind it and is corrected as it always
// was.
//
// THE EXEMPTION ASKS FOR ATOMIC AND NOT MERELY FOR A SIZING, which is the
// issue's own wording and is worth stating because it has a live consequence: a
// lane the ruler called BORDERLINE over a file its siblings share is still
// corrected to oversized here. That is not obviously wrong — borderline is the
// ruler saying the node is already past the size it would like, and a division
// it half-doubted is a weaker signature than three confident atomics — but it is
// a live question rather than a settled one, and it is the owner's to answer. It
// is written down here rather than quietly widened.
func correctBeyondReach(graph *Graph) {
	reach := graph.reach()
	if !reach.known() {
		return
	}
	// The material each work node names, read once. Two passes want it — the
	// sum for the node itself, and how many nodes name each file — and reading
	// a file twice per pass to answer both would be the same bytes read for
	// nothing.
	named := make([][]reading, len(graph.Nodes))
	// How many work nodes UNDER THE SAME PARENT name each file. Siblings and
	// not simply other nodes: a division is drawn in one place, so its lanes sit
	// beside each other, and counting the whole graph would let a node in some
	// other subtree — or a node's own children — vouch for a leaf that owns a
	// whole file by itself.
	siblingsNaming := map[int]map[string]int{}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Kind != KindWork {
			continue
		}
		named[index] = reach.readings(node.Sources...)
		among := siblingsNaming[node.Parent]
		if among == nil {
			among = map[string]int{}
			siblingsNaming[node.Parent] = among
		}
		for _, one := range named[index] {
			among[one.Name]++
		}
	}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Kind != KindWork || node.State.Frozen() {
			continue
		}
		// THE VERDICT IS WRITTEN DOWN BEFORE IT IS ACTED ON, and it is written
		// for every node rather than only for the ones being corrected. The
		// expansion pass asks the same question about the same node a moment
		// later and cannot see a sibling from where it stands, so what it reads
		// has to be this answer and not a second reading of the disk. See
		// Node.BeyondReach.
		node.BeyondReach = reach.weigh(named[index]).Exceeds() &&
			!(node.Size == SizeAtomic && isALaneOfADivision(named[index], siblingsNaming[node.Parent], reach))
		if !node.BeyondReach || node.Size == SizeOversized {
			continue
		}
		node.Size = SizeOversized
		JournalRefusal(node, RefusalBeyondReach)
	}
}

// isALaneOfADivision reports whether these readings are one lane of a division:
// they name a file that a sibling work node names too.
//
// THE SHARING IS THE SIGNATURE, WHETHER THE READING IS SCOPED OR BARE. That is
// the correction to a first version of this test which demanded that every
// reading be scoped, and it was measured wrong at the plan door on the very
// brief the law was written for: asked for three lanes over one handbook, the
// model sized each lane atomic and wrote each lane's source as the bare name
// `HANDBOOK.md`, with the lane's share said in the summary instead — "Format
// all chapter headings per spec.", "Insert contents section after Handbook
// line.", "Convert see-also lines to formatted links." All three were then
// charged 84.8 KB, corrected to oversized, refused as one piece and handed over
// whole, which is the whole of issue #480 happening again through the source
// line's punctuation. THREE ATOMIC SIBLINGS NAMING ONE FILE CANNOT EACH BE
// HOLDING THE WHOLE OF IT — and the sizer read each of their summaries before
// it called them atomic, which is precisely the judgment this pass has no
// business overruling.
//
// The sharing is also what keeps the guarantee from issue #384, and it keeps it
// per FILE rather than per node: material this node names that NO sibling names
// is nobody else's share, so it is still weighed against the window on its own.
// A node alone in naming a whole 144 KB register is corrected exactly as it
// always was, and it stays corrected when a shared scope is sitting beside the
// register in its source list.
func isALaneOfADivision(readings []reading, siblingsNaming map[string]int, reach Reach) bool {
	shared, alone := false, 0
	for _, one := range readings {
		if siblingsNaming[one.Name] > 1 {
			shared = true
			continue
		}
		alone += one.Bytes
	}
	return shared && alone <= reach.Bytes
}
