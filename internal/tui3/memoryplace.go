package tui3

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

const (
	memoryShelfShown   = 3
	memoryShelvesShown = 5
)

var memoryShelfNames = map[string]string{
	store.MemoryScopeUser:    "you",
	store.MemoryScopeProject: "this project",
	store.MemoryScopeEnv:     "this machine",
}

type memoryStop struct {
	shelf string
	line  *store.Memory
	// fold is the key a fold line opens and shuts in the place's open map
	// ([memoryFoldKey], [memoryShelvesFold]); empty on every other stop.
	fold string
}

// memoryFoldKey is the open-map key for the fold under one shelf's lines, and
// memoryShelvesFold the key for the fold under the shelves themselves. They sit
// in the map the shelves' own open state lives in because they are the same
// kind of fact — what this visit has unrolled — and a scope is never empty,
// so neither can collide with one.
func memoryFoldKey(scope string) string { return "\x00" + scope }

const memoryShelvesFold = "\x00"

type memoryReading struct {
	held int
	// letGo and replaced are the two ways a memory stops being held, and they
	// are counted apart because they are not the same event
	// ([memoryHelp] says which word each row wears). One was asked for; the
	// other happened to you.
	letGo    int
	replaced int
	total    int
	shelves  int
	filter   string
	lines    []memoryReadingLine
}

type memoryReadingKind uint8

const (
	memoryReadingProse memoryReadingKind = iota
	memoryReadingBlank
	memoryReadingHeader
	memoryReadingSection
	memoryReadingShelf
	memoryReadingMemory
	memoryReadingFold
)

type memoryReadingLine struct {
	kind  memoryReadingKind
	shelf string
	// label is the row's IDENTITY — the shelf's name and count, the memory's
	// own title — and rowfit's law 1 is about this string: it is whole, or the
	// row is pointless.
	label string
	// facts are what is known about that identity, highest first, joined by the
	// one separator this surface joins facts with ([rowSep]). They used to be a
	// pair of pre-joined strings pushed onto the label behind two spaces, so one
	// row carried two separator grammars — `· Ships on Fridays  fact  let go` —
	// and nothing on it said where the memory's own words stopped.
	facts  []rowField
	age    string
	memory *store.Memory
}

type rankedMemoryShelf struct {
	shelf store.MemoryShelf
	lines []store.Memory
	score int
	count int
}

// readMemory resolves filtering, ranking and folds once so drawing remains a
// pure measurement of already-known words. The caller supplies now because a
// redraw must not silently turn into a clock read.
func readMemory(shelves store.MemoryShelves, open map[string]bool, filter string, now time.Time) memoryReading {
	query := strings.ToLower(strings.TrimSpace(filter))
	r := memoryReading{
		held: shelves.Held, letGo: shelves.LetGo, replaced: shelves.Superseded,
		total: shelves.Total, shelves: len(shelves.Shelves), filter: query,
	}
	// A MACHINE THAT HAS REMEMBERED NOTHING HAS NO LINES AT ALL, and the place
	// draws its heading and its whisper instead ([placeWhisper]). Every other
	// machine gets the head row over its shelves from the first line on — the
	// page used to teach in three paragraphs until it held eight lines and then
	// swap them for the head row in one frame (PLACES-AUDIT.md finding 4).
	if r.bare() {
		return r
	}
	r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingHeader})

	var ranked []rankedMemoryShelf
	for _, shelf := range shelves.Shelves {
		candidate := rankedMemoryShelf{shelf: shelf, count: shelf.Held + shelf.LetGo + shelf.Superseded}
		shelfText := strings.ToLower(memoryShelfName(shelf))
		shelfScore, shelfMatch := memoryWordsMatch(shelfText, query)
		for _, memory := range shelf.Memories {
			text := strings.ToLower(strings.Join([]string{memory.Title, memory.Text, memory.Type, memory.Status, strings.Join(memory.Tags, " ")}, " "))
			score, match := memoryWordsMatch(text, query)
			if query == "" || shelfMatch || match {
				candidate.lines = append(candidate.lines, memory)
				candidate.score = max(candidate.score, max(shelfScore, score))
			}
		}
		if query == "" || len(candidate.lines) > 0 {
			ranked = append(ranked, candidate)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if query != "" && ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].count > ranked[j].count
	})
	if query != "" && len(ranked) == 0 {
		r.lines = []memoryReadingLine{{kind: memoryReadingProse, label: fmt.Sprintf("nothing on a shelf says %q", query)}}
		return r
	}

	// No memory status asks for a look: active is held, while forgotten and
	// superseded are history. Drawing that section would invent a fourth state.
	//
	// AND THE SECTION LINE IS DRAWN ONLY OVER SHELVES. A machine that has
	// remembered nothing meets the three teaching sentences and nothing else; a
	// heading with no rows under it is furniture over an absence, which is the
	// emptiness law applied to a label instead of to a number.
	if len(ranked) > 0 {
		if len(r.lines) > 0 {
			r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingBlank})
		}
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingSection, label: memorySectionWord, facts: memoryTypeLegend(ranked)})
	}
	shownShelves := min(len(ranked), memoryShelvesShown)
	if open[memoryShelvesFold] {
		shownShelves = len(ranked)
	}
	for i := 0; i < shownShelves; i++ {
		shelf := ranked[i]
		key := shelf.shelf.Scope
		newToday := 0
		var newest time.Time
		for _, memory := range shelf.lines {
			if memory.UpdatedAt.After(newest) {
				newest = memory.UpdatedAt
			}
			if !memory.UpdatedAt.IsZero() && sameDay(memory.UpdatedAt, now) {
				newToday++
			}
		}
		mark := tokens.GlyphCollapsed
		if open[key] {
			mark = tokens.GlyphExpanded
		}
		r.lines = append(r.lines, memoryReadingLine{
			kind: memoryReadingShelf, shelf: key,
			label: mark + " " + memoryShelfName(shelf.shelf) + " · " + groupedInt(shelf.count),
			facts: memoryShelfFacts(shelf.shelf, newToday), age: sinceAt(newest, now),
		})
		if !open[key] {
			continue
		}
		shown := min(len(shelf.lines), memoryShelfShown)
		if open[memoryFoldKey(key)] {
			shown = len(shelf.lines)
		}
		for j := 0; j < shown; j++ {
			memory := shelf.lines[j]
			copy := memory
			r.lines = append(r.lines, memoryReadingLine{
				kind: memoryReadingMemory, shelf: key, memory: &copy,
				label: tokens.GlyphProseBullet + " " + memory.Title,
				facts: []rowField{rowSay(memoryTypeWord(memory.Type)), rowSay(memoryHelp(memory, now))},
				age:   sinceAt(memory.UpdatedAt, now),
			})
		}
		// THE FOLD LINES ARE DOORS BOTH WAYS: `▸ 37 more, on this shelf`
		// unrolls the rest of the shelf where it stands and `▾ 37 fewer` rolls
		// it back, on `enter` or a click ([foldDoor]).
		if hidden := len(shelf.lines) - memoryShelfShown; hidden > 0 {
			fold := memoryFoldKey(key)
			r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingFold, shelf: fold,
				label: foldDoor(open[fold], hidden, "on this shelf")})
		}
	}
	if hidden := len(ranked) - memoryShelvesShown; hidden > 0 {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingFold, shelf: memoryShelvesFold,
			label: foldDoor(open[memoryShelvesFold], hidden, "shelves")})
	}
	return r
}

// wrapped is this reading with its TEACHING PROSE laid out for a frame of this
// width: one line per drawn row, held to the same reading measure every other
// place's teaching is held to ([teachMeasure], placebodies.go).
//
// THE PROSE USED TO BE CUT WITH AN ELLIPSIS AND NOT WRAPPED. At 80 columns the
// second sentence drew `I put a line in here when it looked like it would matter
// later, and I only carr…` and its other half was simply gone — while tasks,
// standing and spend all wrap at 76 cells and never cut. A sentence about what
// this place is FOR is the one thing on an empty page, and half of it is worse
// than none.
//
// IT IS A STEP OF ITS OWN AND NOT PART OF [readMemory] because the reading is
// what the store said and this is what the frame can hold: one line of the
// reading stays one drawn row, which is the law the cursor, the hit map and the
// scroll are all built on ([memoryReading.at]).
func (r memoryReading) wrapped(width int) memoryReading {
	measure := width - 2
	if measure > teachMeasure {
		measure = teachMeasure
	}
	if measure < 1 {
		return r
	}
	out := make([]memoryReadingLine, 0, len(r.lines))
	for _, line := range r.lines {
		if line.kind != memoryReadingProse {
			out = append(out, line)
			continue
		}
		for _, part := range wrap(line.label, measure) {
			out = append(out, memoryReadingLine{kind: memoryReadingProse, label: part})
		}
	}
	r.lines = out
	return r
}

func memoryWordsMatch(text, query string) (int, bool) {
	if query == "" {
		return 0, true
	}
	total := 0
	for _, word := range strings.Fields(query) {
		score, ok := session.MatchQuality(text, word)
		if !ok {
			return 0, false
		}
		total += score
	}
	return total, true
}

func memoryShelfName(shelf store.MemoryShelf) string {
	if word := memoryShelfNames[shelf.Scope]; word != "" {
		return word
	}
	return shelf.Label
}

// memorySectionWord names the shelves, and it is the IDENTITY of that row: the
// two words that say what everything under them is. It is never the half that
// is cut.
const memorySectionWord = "shelves · biggest first"

// memoryTypeLegend is what the shelves are made of, as RANKED FACTS rather than
// one joined string.
//
// THE HEADING WAS LOSING TO ITS OWN LEGEND. The row was laid out by reserving
// every cell the legend asked for and fitting the heading into what was left, so
// at 80 columns it read `shelves · …` beside `fact 7 · preference 2 · decision 2
// · correction 1 · project state 1` — the identity spent to keep a count of
// correction memories, which is rowfit's law 1 inverted. As fields the kinds
// fall off the end one at a time, biggest first, and the heading never loses a
// cell.
func memoryTypeLegend(shelves []rankedMemoryShelf) []rowField {
	counts := map[string]int{}
	for _, shelf := range shelves {
		for kind, count := range shelf.shelf.ByType {
			counts[kind] += count
		}
	}
	kinds := []string{store.MemoryFact, store.MemoryPreference, store.MemoryDecision, store.MemoryCorrection, store.MemoryProjectState}
	// BIGGEST FIRST, which is what the heading beside it promises. The kind
	// order above is the store's own and breaks a tie, so two kinds with the
	// same count are always drawn in the same order.
	sort.SliceStable(kinds, func(i, j int) bool { return counts[kinds[i]] > counts[kinds[j]] })
	fields := make([]rowField, 0, len(kinds))
	for _, kind := range kinds {
		if counts[kind] > 0 {
			fields = append(fields, rowSay(memoryTypeWord(kind)+" "+groupedInt(counts[kind])))
		}
	}
	return fields
}

// memoryShelfFacts is what is known about one shelf, ranked: what it is mostly
// made of, and how much of it arrived today.
func memoryShelfFacts(shelf store.MemoryShelf, newToday int) []rowField {
	fields := make([]rowField, 0, 2)
	if kinds := store.MemoryShelfTypes(shelf); len(kinds) > 0 {
		kind := kinds[0]
		for _, candidate := range kinds[1:] {
			if shelf.ByType[candidate] > shelf.ByType[kind] {
				kind = candidate
			}
		}
		if shelf.ByType[kind] != 1 {
			kind = memoryTypePlural(kind)
		}
		fields = append(fields, rowSay("mostly "+memoryTypeWord(kind)))
	}
	if newToday > 0 {
		fields = append(fields, rowSay(groupedInt(newToday)+" new today"))
	}
	return fields
}

// memoryTypeWord is a memory's kind as a PERSON reads it. The store spells one
// of the five with an underscore, which is a column name and not a word — no
// machinery vocabulary in anything a person reads (CLAUDE.md's law) — so the
// bar goes and nothing else changes.
func memoryTypeWord(kind string) string {
	return strings.ReplaceAll(kind, "_", " ")
}

func memoryTypePlural(kind string) string {
	if kind == store.MemoryProjectState {
		return "project states"
	}
	return kind + "s"
}

// memoryHelp is HOW ONE MEMORY HAS DONE, in the words this page uses for it.
//
// `let go` AND `replaced` ARE TWO DIFFERENT THINGS AND SAID SO. Both statuses
// answered `let go`, and the count on the head line added them together, so one
// phrase carried two facts on one screen: a line somebody asked to be forgotten,
// and a line the machine retired on its own because something newer contradicted
// it. The second is the riskier of the two and the manual has a whole section
// about it ("Why did it say superseded?" — a memory **replaced by one that
// contradicts it**), which is where this word comes from. The head counts them
// apart for the same reason ([memoryCounts]).
func memoryHelp(memory store.Memory, now time.Time) string {
	switch memory.Status {
	case store.MemoryForgotten:
		// The store keeps no reason or author for this transition, so "you
		// corrected it" would turn an unknown origin into a person-facing fact.
		return memoryLetGoWord
	case store.MemorySuperseded:
		return memoryReplacedWord
	}
	if memory.UseCount == 0 && memory.MissCount == 0 {
		if age := sinceAt(memory.UpdatedAt, now); age != "" {
			return "new, learned " + age
		}
		return "new"
	}
	parts := []string{}
	if memory.UseCount > 0 {
		if memory.MissCount == 0 {
			word := "time"
			if memory.UseCount != 1 {
				word = "times"
			}
			parts = append(parts, "helped "+groupedInt(memory.UseCount)+" "+word)
		} else {
			parts = append(parts, "helped "+groupedInt(memory.UseCount))
		}
	}
	if memory.MissCount > 0 {
		parts = append(parts, "bore on "+groupedInt(memory.MissCount))
	}
	return strings.Join(parts, " · ")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.In(b.Location()).Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// rows paints only through the palette and fits every completed line with the
// same cell-width ruler the rest of the surface uses.
func (r memoryReading) rows(width int, pal palette) []string { return r.paint(width, pal, nil) }

// paint is [memoryReading.rows] with the rows the cursor or the pointer is on
// lit (placeprose.go's THE FIVE-LEVEL SCALE); a nil lit lights nothing.
//
// EVERY ROW HANGS FROM THE BODY'S OWN COLUMN, one cell in, where every place and
// home hang theirs: a shelf's `▸` and a line's `·` stand in the lead, and the
// words start two cells after them (PLACES-AUDIT.md finding 12). This page
// started at column 0, so walking the bar left to right the body stepped aside.
func (r memoryReading) paint(width int, pal palette, lit func(row int) bool) []string {
	rows := make([]string, 0, len(r.lines))
	room := width - 1
	for _, line := range r.lines {
		on := lit != nil && lit(len(rows))
		switch line.kind {
		case memoryReadingProse:
			rows = append(rows, " "+pal.dim(fit(line.label, room)))
		case memoryReadingBlank:
			if len(rows) > 0 && rows[len(rows)-1] != "" {
				rows = append(rows, "")
			}
		case memoryReadingHeader:
			// THE HEAD COUNTS WHAT IS HELD AND IS NOT A SUBJECT, so it wears the
			// heading's ink: it was the reading ink, the one heading on the bar
			// lit like a row (PLACES-AUDIT.md finding 9).
			left := memoryCounts(r.held, r.shelves, r.letGo, r.replaced)
			rows = append(rows, " "+memoryJoin(placeHeading(left, pal), pal.dim(memoryFilterWord), memoryFilterWord, room))
		case memoryReadingSection:
			// THE HEADING IS WHOLE AND THE LEGEND IS A PREFIX OF ITSELF. The
			// legend is fitted to what the heading leaves rather than the other
			// way round, so a kind falls off the end before the two words that
			// say what the section is lose a cell (rowfit's law 1).
			legend := rowTail(line.facts, room-ansi.StringWidth(line.label)-memoryGutter)
			rows = append(rows, " "+memoryJoin(placeHeading(line.label, pal), pal.dim(legend), legend, room))
		case memoryReadingShelf:
			rows = append(rows, " "+memoryRow(placeSubjectInk(on, pal), line, on, pal, room))
		case memoryReadingMemory:
			paintLabel := placeSubjectInk(on, pal)
			if line.memory != nil && line.memory.Status != store.MemoryActive {
				// There is no strike paint in this palette, so history takes the
				// documented fallback and recedes instead of borrowing a raw style.
				paintLabel = pal.dim
			}
			rows = append(rows, " "+memoryRow(paintLabel, line, on, pal, room))
		case memoryReadingFold:
			rows = append(rows, " "+placeFactInk(on, pal)(fit(line.label, room)))
		}
	}
	return rows
}

// memoryFilterWord is the head row's right field, and it is a key rather than a
// fact — the one thing typing on this page does.
const memoryFilterWord = "type to filter"

// memoryLetGoWord and memoryReplacedWord are the two words for a memory that is
// no longer held, and they are constants because the COUNT on the head line and
// the TAG on a row have to be the same word for the same thing. They were not:
// the head added the two states together under `let go` while a row wore `let
// go` for either of them, so one phrase meant two things on one screen.
const (
	memoryLetGoWord    = "let go"
	memoryReplacedWord = "replaced"
)

// memoryGutter is the least air between a row's identity and the facts drawn at
// the other end of it. It is two cells and not one, because this page draws its
// pairs far apart and a single space between them reads as a sentence.
const memoryGutter = 2

func memoryCounts(held, shelves, letGo, replaced int) string {
	var parts []string
	if held > 0 {
		parts = append(parts, groupedInt(held)+" held")
	}
	if shelves > 0 {
		parts = append(parts, groupedInt(shelves)+" "+memoryShelfWord(shelves))
	}
	if letGo > 0 {
		parts = append(parts, groupedInt(letGo)+" "+memoryLetGoWord)
	}
	if replaced > 0 {
		parts = append(parts, groupedInt(replaced)+" "+memoryReplacedWord)
	}
	return strings.Join(parts, " · ")
}

// memoryShelfWord is the noun a count of shelves takes. `1 shelves` stood on the
// head row of every page with one shelf on it (PLACES-AUDIT.md finding 14), and
// [plural] would say `shelfs`.
func memoryShelfWord(n int) string {
	if n == 1 {
		return "shelf"
	}
	return "shelves"
}

func memoryJoin(left, paintedRight, plainRight string, width int) string {
	if left == "" {
		return rightAlign(paintedRight, plainRight, width)
	}
	room := width - ansi.StringWidth(plainRight) - 2
	if room < 1 {
		return fit(left, width)
	}
	left = fit(left, room)
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(plainRight)
	return left + strings.Repeat(" ", max(1, gap)) + paintedRight
}

// memoryRow is ONE LIST ROW of this place, laid out by rowfit's law rather than
// by hand.
//
// WHAT IT REPLACES. The row was three fields glued together — the label, then
// two spaces, then a joined note, then two more spaces, then the help — while
// every clause built for those same rows joined with ` · `. So one line carried
// two separator grammars and the only separator on it meant two different
// things: reading `· Ships on Fridays  fact  let go`, nobody can tell where the
// memory's own words stop and the machine's facts about it start. The two spaces
// were themselves the fix for a worse run-together (`not tabscorrection`), so the
// column idea was right and the grammar simply was not carried through.
//
// NOW IT IS THE IDENTITY WHOLE, then the facts as a ranked prefix joined by the
// one separator, then the age at the right. A fact that will not fit is dropped
// whole — which is also how the help clause stopped needing a `width >= 80` of
// its own.
func memoryRow(paintLabel func(string) string, line memoryReadingLine, lit bool, pal palette, width int) string {
	facts := placeFactInk(lit, pal)
	room := width
	age := ansi.StringWidth(line.age)
	if age > 0 {
		room -= age + memoryGutter
	}
	name, tail := memoryHalves(line.label, line.facts, room)
	painted, spent := paintLabel(name), ansi.StringWidth(name)
	if tail != "" {
		painted += facts(rowSep + tail)
		spent += ansi.StringWidth(rowSep) + ansi.StringWidth(tail)
	}
	if line.age == "" {
		return painted
	}
	return painted + strings.Repeat(" ", max(1, width-spent-age)) + facts(line.age)
}

// memoryHalves is [rowPlan.fit] with this page's own gutter: the identity, cut
// only where the row cannot hold it alone, and then as many facts as the rest of
// the room takes. A name that HAD to be cut takes the whole row, because a row
// whose title is already an ellipsis has spent the one thing it was drawn to
// say.
func memoryHalves(label string, facts []rowField, room int) (string, string) {
	name, cut := rowTrim(label, room, false)
	if cut || len(facts) == 0 {
		return name, ""
	}
	return name, rowTail(facts, room-ansi.StringWidth(name)-ansi.StringWidth(rowSep))
}

func (r memoryReading) at(i int) (memoryStop, bool) {
	if i < 0 || i >= len(r.lines) {
		return memoryStop{}, false
	}
	line := r.lines[i]
	switch line.kind {
	case memoryReadingShelf:
		return memoryStop{shelf: line.shelf}, true
	case memoryReadingMemory:
		return memoryStop{shelf: line.shelf, line: line.memory}, true
	case memoryReadingFold:
		return memoryStop{fold: line.shelf}, true
	}
	return memoryStop{}, false
}

// bare says this page has NOTHING ON IT — no shelf, no line, nothing to open,
// filter or walk — which is when the place draws its whisper instead of a body
// and takes the shelf keys off the foot (place_memory.go's hint).
func (r memoryReading) bare() bool { return r.total == 0 }

// THERE IS ONE EMPTY STATE HERE AND THE READING ITSELF IS IT.
//
// This file used to carry a `memoryTeach` that said "nothing is remembered yet"
// and then repeated the command surface's `memory is off for this session` note
// under it. Both halves were wrong on a PLACE, and they are wrong in the same
// way: the reading is the empty state. A machine that has remembered nothing
// meets the place's heading and whisper ([placeWhisper]), and a machine with
// memory switched off meets exactly the same two lines, with [memoryOffNote]
// said ONCE on the note under them ([app.openMemory] puts it there). Neither
// state is a second body, and neither is a refusal: the place opens on both.
