package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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
// Scoped is kept beside the bytes because two passes need it and they need it
// for opposite reasons: the sum does not care how a share was arrived at, while
// correctBeyondReach cares about nothing else — a node scoping a region of a
// file its siblings also scope is a division, not an over-large leaf.
type reading struct {
	Name   string
	Bytes  int
	Scoped bool
}

// namedScope is one file name as some words spell it, together with the words
// that scope it — empty when the name stands bare.
type namedScope struct {
	Name  string
	Scope string
}

// scopingMark is what turns a file name into a scoped reference: the name, then
// a colon, a dash or an opening bracket, then the words that say which part of
// the file is meant. It is deliberately a small, punctuated set rather than
// "any words that follow", because ANY WORDS THAT FOLLOW IS EVERY SENTENCE — a
// goal reading "rewrite corpus.txt in three lanes" names the whole of
// corpus.txt, and a reader that treated "in three lanes" as a scope would
// measure nothing anywhere. A plain dash must be followed by a space so that it
// is a separator and not the middle of a name.
var scopingMark = regexp.MustCompile(`^[ \t]*(?::|[—–]|-[ \t]|\()[ \t]*`)

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
			continue
		}
		scopes[index].Scope = strings.TrimSpace(rest[len(mark):])
	}
	return scopes
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
// or the share its scope resolves to. A name that resolves to nothing, and a
// scope that resolves to no share, contribute NOTHING rather than an estimate.
// The same file named twice is weighed once, by its first mention.
func (r Reach) readings(texts ...string) []reading {
	if !r.known() {
		return nil
	}
	var readings []reading
	seen := map[string]bool{}
	budget := namedCandidates
	for _, text := range texts {
		for _, named := range namedScopes(text) {
			if budget == 0 {
				return readings
			}
			budget--
			if seen[named.Name] || !insideWorkspace(named.Name) {
				continue
			}
			seen[named.Name] = true
			path := filepath.Join(r.Dir, named.Name)
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if named.Scope == "" {
				readings = append(readings, reading{Name: named.Name, Bytes: int(info.Size())})
				continue
			}
			share, resolved := scopedShare(path, info.Size(), named.Scope)
			if !resolved {
				continue
			}
			readings = append(readings, reading{Name: named.Name, Bytes: share, Scoped: true})
		}
	}
	return readings
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
//   - A COUNT OF LINE-SHAPED THINGS — "1,160 North records", "30 heading
//     lines", "40 rows". The count is exact and the file says what a line of it
//     weighs, so the share is that many of the file's own mean line. The source
//     said HOW MANY lines and not WHICH, and this is the file answering the
//     half it can answer.
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
		namedBlockShare(lines, scope),
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
func lineCountShare(lines []string, size int, scope string) int {
	match := lineCount.FindStringSubmatch(scope)
	if match == nil {
		return 0
	}
	count := countIn(match[1])
	if count <= 0 {
		return 0
	}
	if count > len(lines) {
		count = len(lines)
	}
	return count * (size / len(lines))
}

// namedBlockShare finds the heading the scope spells and returns the extent of
// the block under it. The candidates are the things a scope spells a heading
// with — a backticked span, or a capitalised word — and each is resolved by
// EQUALITY against a heading line of the file, never by containment: a scope
// that mentions "North" measures the North block or nothing, and never a line
// that happens to have the word in it.
func namedBlockShare(lines []string, scope string) int {
	labels := map[string]bool{}
	for _, match := range backticked.FindAllStringSubmatch(scope, -1) {
		labels[headingKey(match[1])] = true
	}
	for _, match := range capitalised.FindAllString(scope, -1) {
		labels[headingKey(match)] = true
	}
	delete(labels, "")
	if len(labels) == 0 {
		return 0
	}
	largest := 0
	for index, line := range lines {
		rank, label, ok := headingLine(line)
		if !ok || !labels[label] {
			continue
		}
		bytes := 0
		for reach := index; reach < len(lines); reach++ {
			if reach > index {
				if next, _, ok := headingLine(lines[reach]); ok && next <= rank {
					break
				}
			}
			bytes += len(lines[reach])
		}
		if bytes > largest {
			largest = bytes
		}
	}
	return largest
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
	lineCount   = regexp.MustCompile(`(?i)\b(\d[\d,]*)(?:\s+\S+){0,6}?\s+(?:records?|rows?|entries|entry|lines?|items?)\b`)
	backticked  = regexp.MustCompile("`([^`]{1,80})`")
	capitalised = regexp.MustCompile(`\b[A-Z][A-Za-z0-9'-]{2,}\b`)
	bareLabel   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _'-]{0,60}$`)
)

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
func correctBeyondReach(graph *Graph) {
	reach := graph.reach()
	if !reach.known() {
		return
	}
	// The material each work node names, read once. Two passes want it — the
	// sum for the node itself, and which files are scoped by more than one node
	// — and reading a file twice per pass to answer both would be the same
	// bytes read for nothing.
	named := make([][]reading, len(graph.Nodes))
	scoped := map[string]int{}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Kind != KindWork {
			continue
		}
		named[index] = reach.readings(node.Sources...)
		for _, one := range named[index] {
			if one.Scoped {
				scoped[one.Name]++
			}
		}
	}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Kind != KindWork || node.Size == SizeOversized || node.State.Frozen() {
			continue
		}
		if !reach.weigh(named[index]).Exceeds() {
			continue
		}
		if node.Size == SizeAtomic && sharesAScopedFile(named[index], scoped) {
			continue
		}
		node.Size = SizeOversized
		JournalRefusal(node, RefusalBeyondReach)
	}
}

// sharesAScopedFile reports whether these readings scope a region of a file
// some other node scopes a region of too — the signature of a lane in a
// division rather than of a leaf over a whole subject.
func sharesAScopedFile(readings []reading, scoped map[string]int) bool {
	for _, one := range readings {
		if one.Scoped && scoped[one.Name] > 1 {
			return true
		}
	}
	return false
}
