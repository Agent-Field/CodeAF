package tui3

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE MEMORIES TAB'S READING: every line codeaf keeps, grouped by where it
// holds, with who kept it and when.
//
// The groups are the OWNERS (internal/store's memory_owner.go), because the
// owner is what decides which conversations a line is put in front of:
//
//	you, everywhere          — in front of every conversation
//	this machine             — every conversation on this machine
//	this project · codeaf    — every conversation in this project
//	<other project>          — that project's lines, by its name
//	couldn't tell which project — lines an earlier build could not place
//	let go                   — forgotten or replaced, and put-back-able
//
// IT IS DRAWN THE WAY THE ACTIVITY TAB DRAWS ITS TREE, so a person who has
// learned one has learned both. A group is a row of its own, said in the
// heading ink with its fold mark after it, and `→` opens it and `←` folds it
// back up (placekeys.go's `right` and `left` arms, [placeMemory.fold]); its
// lines hang under it on the tree's own connectors, and every row answers the
// same two questions — who kept it, how old it is — in the same two columns,
// headed on the filter row the way the Activity tab heads `project` and `age`.
// `enter` never opens a group: it was the key that did, and a group heading
// that looked like one more line and opened on the key that opened a line was
// the page people could not read.
//
// EVERY LINE WEIGHS THE SAME. There is no "helped" count and no rule flag: what
// is under the first three groups is exactly what a conversation here is told,
// and the head row says so — and says how many lines did not fit, when any did
// not ([store.FitMemories], the store's own choice of what rides).

// The words the groups are headed with, spelled once.
const (
	memoryShelfYou     = "you, everywhere"
	memoryShelfMachine = "this machine"
	memoryShelfHere    = "this project"
	memoryShelfLost    = "couldn't tell which project"
	memoryShelfLetGo   = "let go"
	memoryAddWord      = "remember something"
	memoryFilterWord   = "type to filter"
	// The two columns, headed on the filter row.
	memoryKeptLabel = "kept by"
	memoryAgeLabel  = "age"
)

// The tab's own measurements, written down once.
const (
	// memoryKeptCells is the `kept by` column: the longest word it says —
	// `the model`, `forgotten` — and the air before the next column.
	memoryKeptCells = 11
	// memoryAgeCells is the `age` column, right-aligned because an age is a
	// figure and figures are read down their last digit.
	memoryAgeCells = 5
	// memoryKeptFloor is the narrowest list that still draws the `kept by`
	// column. Under it the column goes and the age stays, because a line that
	// has spent its cells on who kept it has stopped saying what it is.
	memoryKeptFloor = 56
	// memoryColumnAir is the one cell between the last column and the edge.
	memoryColumnAir = 1
	// memoryKinStep is the indent a line hangs under its group by, so its
	// connector stands under the group's name the way a task's stands under its
	// conversation's on the Activity tab.
	memoryKinStep = "  "
)

type memoryReadingKind uint8

const (
	memoryReadingProse memoryReadingKind = iota
	memoryReadingHeader
	memoryReadingControl
	memoryReadingAdd
	memoryReadingShelf
	memoryReadingMemory
)

// memoryStop is what the cursor can stand on: the add row, a group, or one
// line.
type memoryStop struct {
	add   bool
	shelf string
	line  *store.Memory
}

type memoryReadingLine struct {
	kind memoryReadingKind
	// shelf is the group key this row belongs to (an owner, or the let-go key).
	shelf string
	// label is the row's IDENTITY — the group's name, the line's own words —
	// and it is whole, or the row is pointless.
	label string
	// count and open are a group row's: how many lines it holds, and whether
	// they are drawn under it.
	count int
	open  bool
	// last says a line is the last one under its group, which is the one that
	// hangs from `└─` rather than `├─`.
	last bool
	// kept and age are a line's two columns.
	kept   string
	age    string
	memory *store.Memory
}

type memoryReading struct {
	typed   string
	held    int
	letGo   int
	outside int
	lines   []memoryReadingLine
}

// memoryLetGoKey is the let-go group's key in the open map. No owner starts
// with a NUL, so it cannot collide with one.
const memoryLetGoKey = "\x00letgo"

// memoryShelfName is a group's heading in a person's words. here is the owner
// this conversation keeps project lines under ("" when it has none).
func memoryShelfName(shelf store.MemoryShelf, here string) string {
	switch {
	case shelf.Owner == store.OwnerUser:
		return memoryShelfYou
	case shelf.Owner == store.OwnerMachine:
		return memoryShelfMachine
	case shelf.Owner == store.OwnerLegacyProject:
		return memoryShelfLost
	case shelf.Owner == here && here != "":
		if shelf.ProjectName != "" {
			return memoryShelfHere + " · " + shelf.ProjectName
		}
		return memoryShelfHere
	case shelf.ProjectName != "":
		return shelf.ProjectName
	}
	return "another project"
}

// memoryHereShelf says whether a group's lines are put in front of a
// conversation here: the person's, this machine's, and this project's.
func memoryHereShelf(owner, here string) bool {
	return owner == store.OwnerUser || owner == store.OwnerMachine || (here != "" && owner == here)
}

// memoryOriginWord is who kept a line, as its row says it.
func memoryOriginWord(origin string) string {
	switch origin {
	case store.OriginPerson:
		return "you"
	case store.OriginModel:
		return "the model"
	case store.OriginNoticed:
		return "noticed"
	}
	return ""
}

// memoryStatusWord is what happened to a let-go line.
func memoryStatusWord(status string) string {
	switch status {
	case store.MemoryForgotten:
		return "forgotten"
	case store.MemorySuperseded:
		return "replaced"
	}
	return ""
}

// readMemory resolves the filter and the open groups once, so drawing remains
// a pure measurement of already-known words. The caller supplies now because a
// redraw must not silently turn into a clock read.
//
// open is what a person has opened with `→`; closed is what they folded back
// up with `←` while a filter was typed, which opens every group with a match —
// a match hidden under a shut group is a match nobody can see, and a group the
// person shut on purpose stays shut until the filter changes.
func readMemory(shelves store.MemoryShelves, here string, open, closed map[string]bool, filter string, now time.Time) memoryReading {
	query := strings.ToLower(strings.TrimSpace(filter))
	terms := fuzzy.Terms(query)
	r := memoryReading{held: shelves.Held, letGo: shelves.LetGoCount, typed: strings.TrimSpace(filter)}

	// WHAT A CONVERSATION HERE IS TOLD, in the order it is told it — the
	// person's, the machine's, the project's — so the head row can say how many
	// lines did not fit in front of the model.
	var hereLines []store.Memory
	for _, rank := range []func(string) bool{
		func(o string) bool { return o == store.OwnerUser },
		func(o string) bool { return o == store.OwnerMachine },
		func(o string) bool { return here != "" && o == here },
	} {
		for _, shelf := range shelves.Shelves {
			if rank(shelf.Owner) {
				hereLines = append(hereLines, shelf.Memories...)
			}
		}
	}
	_, r.outside = store.FitMemories(hereLines)

	// A MACHINE THAT HAS REMEMBERED NOTHING HAS NO LINES AT ALL: the place
	// draws its heading and whisper instead, and the whisper names /remember.
	// No head row saying "0 kept" — the emptiness law applied to a count.
	if r.bare() {
		return r
	}
	r.lines = append(r.lines,
		memoryReadingLine{kind: memoryReadingHeader},
		memoryReadingLine{kind: memoryReadingControl},
		memoryReadingLine{kind: memoryReadingProse})
	if query == "" {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingAdd, label: memoryAddWord})
	}
	matches := func(memory store.Memory, shelfName string) bool {
		if query == "" {
			return true
		}
		text := strings.ToLower(strings.Join([]string{memory.Text, shelfName, memoryOriginWord(memory.Origin), memoryStatusWord(memory.Status)}, " "))
		_, ok := fuzzy.Score(text, terms)
		return ok
	}
	opened := func(key string) bool {
		if query != "" {
			return !closed[key]
		}
		return open[key]
	}
	shown := 0
	for _, shelf := range shelves.Shelves {
		name := memoryShelfName(shelf, here)
		var kept []store.Memory
		for _, memory := range shelf.Memories {
			if matches(memory, name) {
				kept = append(kept, memory)
			}
		}
		if len(kept) == 0 {
			continue
		}
		shown++
		count := shelf.Held
		if query != "" {
			count = len(kept)
		}
		r.addShelf(shelf.Owner, name, count, kept, opened(shelf.Owner), now)
	}
	var letGo []store.Memory
	for _, memory := range shelves.LetGo {
		if matches(memory, memoryShelfLetGo) {
			letGo = append(letGo, memory)
		}
	}
	if len(letGo) > 0 {
		shown++
		count := shelves.LetGoCount
		if query != "" {
			count = len(letGo)
		}
		r.addShelf(memoryLetGoKey, memoryShelfLetGo, count, letGo, opened(memoryLetGoKey), now)
	}
	if query != "" && shown == 0 {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingProse, label: fmt.Sprintf("nothing remembered says %q", query)})
	}
	return r
}

// addShelf appends one group's row and, when it is open, its lines.
func (r *memoryReading) addShelf(key, name string, count int, lines []store.Memory, opened bool, now time.Time) {
	r.lines = append(r.lines, memoryReadingLine{
		kind: memoryReadingShelf, shelf: key, label: name, count: count, open: opened,
	})
	if !opened {
		return
	}
	for i, memory := range lines {
		copy := memory
		// A LET-GO LINE SAYS WHAT HAPPENED TO IT where a kept one says who kept
		// it: on that group the status is the fact a person is reading for.
		kept := memoryOriginWord(memory.Origin)
		if word := memoryStatusWord(memory.Status); word != "" {
			kept = word
		}
		r.lines = append(r.lines, memoryReadingLine{
			kind: memoryReadingMemory, shelf: key, memory: &copy,
			label: memory.Text, kept: kept, age: sinceAt(memory.UpdatedAt, now),
			last: i == len(lines)-1,
		})
	}
}

// memoryOpenByDefault is which groups a page opens with: the ones whose lines
// a conversation here is told. Other projects, the unplaced and the let-go stay
// folded until somebody opens them.
func memoryOpenByDefault(shelves store.MemoryShelves, here string) map[string]bool {
	open := map[string]bool{}
	for _, shelf := range shelves.Shelves {
		if memoryHereShelf(shelf.Owner, here) {
			open[shelf.Owner] = true
		}
	}
	return open
}

// memoryCounts is the head row.
func memoryCounts(held, letGo, outside int) string {
	parts := []string{groupedInt(held) + " kept"}
	if outside > 0 {
		parts = append(parts, groupedInt(outside)+" don't fit in front of the model · forget some")
	}
	if letGo > 0 {
		parts = append(parts, groupedInt(letGo)+" "+memoryShelfLetGo)
	}
	return strings.Join(parts, " · ")
}

// memoryColumns is the cells of one list width every row spends the same way:
// the `kept by` column (zero under [memoryKeptFloor]), the `age` column, and
// what is left for the name. One arithmetic, so the filter row's labels stand
// over the cells their rows fill at every width.
func memoryColumns(width int) (kept, age, name int) {
	age = memoryAgeCells
	if width >= memoryKeptFloor {
		kept = memoryKeptCells
	}
	name = max(width-kept-age-memoryColumnAir, 1)
	return kept, age, name
}

// memoryPaint is what a draw needs beyond the reading: which rows are lit, and
// the words being written when a line's editor is open — drawn in the row it
// belongs to, so a person can see what they are typing.
type memoryPaint struct {
	lit    func(row int) bool
	editID string
	edit   *editor
}

// rows paints only through the palette.
func (r memoryReading) rows(width int, pal palette) []string {
	return r.paint(width, pal, memoryPaint{})
}

// paint draws every row of the reading at one list width. Every row hangs from
// the body's own column, one cell in, where every place hangs its rows.
func (r memoryReading) paint(width int, pal palette, opts memoryPaint) []string {
	rows := make([]string, 0, len(r.lines))
	room := width - len(placeLead)
	for _, line := range r.lines {
		on := opts.lit != nil && opts.lit(len(rows))
		switch line.kind {
		case memoryReadingProse:
			rows = append(rows, placeLead+pal.dim(fit(line.label, room)))
		case memoryReadingHeader:
			rows = append(rows, placeLead+placeHeading(fit(memoryCounts(r.held, r.letGo, r.outside), room), pal))
		case memoryReadingControl:
			row, _ := memoryControlRow(r.typed, width, pal)
			rows = append(rows, row)
		case memoryReadingAdd:
			row, _ := memoryAddRow(width, pal, on, opts.editor(memoryAddID))
			rows = append(rows, row)
		case memoryReadingShelf:
			rows = append(rows, memoryShelfRow(line, width, pal))
		case memoryReadingMemory:
			var box *editor
			if line.memory != nil {
				box = opts.editor(line.memory.ID)
			}
			row, _ := memoryLineRow(line, width, pal, on, box)
			rows = append(rows, row)
		}
	}
	return rows
}

// editor is the box open on one row, or nil.
func (opts memoryPaint) editor(id string) *editor {
	if opts.edit != nil && opts.editID == id {
		return opts.edit
	}
	return nil
}

// memoryControlRow is the filter row: the box under the head, and the two
// column labels over their own cells. It answers the column the caret sits
// in, on the Activity tab's terms ([tasksControlRow]): the cell after what is
// typed, or the box's first cell when nothing is.
func memoryControlRow(query string, width int, pal palette) (string, int) {
	kept, age, name := memoryColumns(width)
	mark := pal.glyph(tokens.GFilter)
	boxCells := max(name-len(placeLead)-ansi.StringWidth(mark)-1, 1)
	column := len(placeLead) + ansi.StringWidth(mark) + 1
	said, ink := fit(memoryFilterWord, boxCells), pal.dim
	if query != "" {
		said, ink = fit(query, boxCells), pal.ink
		column += ansi.StringWidth(said)
	}
	out := placeLead + pal.dim(mark) + " " + ink(said) + pad(boxCells-ansi.StringWidth(said))
	if kept > 0 {
		label := fit(memoryKeptLabel, kept-1)
		out += pal.dim(label) + pad(kept-ansi.StringWidth(label))
	}
	label := fit(memoryAgeLabel, age)
	out += pad(age-ansi.StringWidth(label)) + pal.dim(label)
	return out + pad(memoryColumnAir), column
}

// memoryAddRow is `+ remember something`, or — while it is being written — the
// new line itself after the `+`, with the column the caret sits in.
func memoryAddRow(width int, pal palette, lit bool, box *editor) (string, int) {
	lead := placeLead + "+ "
	room := max(width-len(lead)-memoryColumnAir, 1)
	if box != nil {
		shown, caret := memoryBoxWindow(box, room)
		return lead + pal.ink(shown), len(lead) + caret
	}
	return placeFactInk(lit, pal)(lead + fit(memoryAddWord, room)), 0
}

// memoryShelfRow is one group: its name in the heading ink, how many lines it
// holds, and its fold mark after them — the Activity tab's spelling of a row
// that opens.
func memoryShelfRow(line memoryReadingLine, width int, pal palette) string {
	mark := pal.glyph(tokens.GCollapsed)
	if line.open {
		mark = pal.glyph(tokens.GExpanded)
	}
	count := " · " + groupedInt(line.count)
	room := max(width-len(placeLead)-ansi.StringWidth(count)-1-ansi.StringWidth(mark)-memoryColumnAir, 1)
	return placeLead + placeHeading(fit(line.label, room), pal) + pal.dim(count) + " " + pal.dim(mark)
}

// memoryLineRow is one line under its group: the connector, the words in what
// the columns leave, then who kept it and how old it is. While its editor is
// open the words are the ones being written, with the column the caret sits
// in.
func memoryLineRow(line memoryReadingLine, width int, pal palette, lit bool, box *editor) (string, int) {
	keptCells, ageCells, nameCells := memoryColumns(width)
	branch := tokens.GTreeBranch
	if line.last {
		branch = tokens.GTreeLast
	}
	connector := pal.glyph(branch) + pal.glyph(tokens.GTreeDash) + " "
	lead := placeLead + memoryKinStep + pal.dim(connector)
	leadCells := len(placeLead) + len(memoryKinStep) + ansi.StringWidth(connector)
	textCells := max(nameCells-leadCells-memoryColumnAir, 1)
	ink := placeSubjectInk(lit, pal)
	if line.memory != nil && line.memory.Status != store.MemoryActive {
		// History recedes: there is no strike paint in this palette.
		ink = pal.dim
	}
	said, caret := fit(line.label, textCells), 0
	if box != nil {
		said, caret = memoryBoxWindow(box, textCells)
		ink = pal.ink
	}
	facts := placeFactInk(lit, pal)
	out := lead + ink(said) + pad(max(nameCells-leadCells, 0)-ansi.StringWidth(said))
	if keptCells > 0 {
		word := fit(line.kept, keptCells-1)
		out += facts(word) + pad(keptCells-ansi.StringWidth(word))
	}
	age := fit(line.age, ageCells)
	out += pad(ageCells-ansi.StringWidth(age)) + facts(age) + pad(memoryColumnAir)
	return out, leadCells + caret
}

// memoryBoxWindow is the part of a box's words a row has room for, and the
// cell the caret sits at within it. Words that fit are drawn whole; words that
// do not are drawn from wherever keeps the caret on the row, because the caret
// is where the person is typing and a row that cut it off would be a box with
// no cursor in it.
func memoryBoxWindow(box *editor, room int) (string, int) {
	value, at := box.value, min(max(box.cursor, 0), len(box.value))
	if ansi.StringWidth(string(value)) <= room {
		return string(value), ansi.StringWidth(string(value[:at]))
	}
	start := 0
	for start < at && ansi.StringWidth(string(value[start:at])) > room-1 {
		start++
	}
	return fit(string(value[start:]), room), ansi.StringWidth(string(value[start:at]))
}

func (r memoryReading) at(i int) (memoryStop, bool) {
	if i < 0 || i >= len(r.lines) {
		return memoryStop{}, false
	}
	line := r.lines[i]
	switch line.kind {
	case memoryReadingAdd:
		return memoryStop{add: true}, true
	case memoryReadingShelf:
		return memoryStop{shelf: line.shelf}, true
	case memoryReadingMemory:
		return memoryStop{shelf: line.shelf, line: line.memory}, true
	}
	return memoryStop{}, false
}

// bare says nothing is remembered at all and no filter is typed: the place
// then draws its whisper and nothing else.
func (r memoryReading) bare() bool { return r.held == 0 && r.letGo == 0 && r.typed == "" }
