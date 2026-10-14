package tui3

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE MEMORY PLACE'S READING: every line codeaf keeps, shelved by where it
// holds, with who kept it and when.
//
// The shelves are the OWNERS (internal/store's memory_owner.go), because the
// owner is what decides which conversations a line is put in front of:
//
//	you, everywhere          — in front of every conversation
//	this machine             — every conversation on this machine
//	this project · codeaf    — every conversation in this project
//	<other project>          — that project's lines, by its name
//	couldn't tell which project — lines an earlier build could not place
//	let go                   — forgotten or replaced, and put-back-able
//
// EVERY LINE WEIGHS THE SAME. There is no "helped" count and no rule flag: what
// is on the first three shelves is exactly what a conversation here is told,
// and the head row says so — and says how many lines did not fit, when any did
// not ([session.MemoryFits]).

// The words the shelves are headed with, spelled once.
const (
	memoryShelfYou     = "you, everywhere"
	memoryShelfMachine = "this machine"
	memoryShelfHere    = "this project"
	memoryShelfLost    = "couldn't tell which project"
	memoryShelfLetGo   = "let go"
	memoryAddWord      = "remember something"
	memoryFilterWord   = "type to filter"
	// memoryGutter is the least air between a row's identity and the facts at
	// the other end of it.
	memoryGutter = 2
)

type memoryReadingKind uint8

const (
	memoryReadingProse memoryReadingKind = iota
	memoryReadingHeader
	memoryReadingAdd
	memoryReadingShelf
	memoryReadingMemory
)

// memoryStop is what the cursor can stand on: the add row, a shelf heading, or
// one line.
type memoryStop struct {
	add   bool
	shelf string
	line  *store.Memory
}

type memoryReadingLine struct {
	kind memoryReadingKind
	// shelf is the shelf key this row belongs to (an owner, or the let-go key).
	shelf string
	// label is the row's IDENTITY — the shelf's name and count, the line's own
	// words — and it is whole, or the row is pointless.
	label  string
	facts  []rowField
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

// memoryLetGoKey is the let-go shelf's key in the open map. No owner starts
// with a NUL, so it cannot collide with one.
const memoryLetGoKey = "\x00letgo"

// memoryShelfName is a shelf's heading in a person's words. here is the owner
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

// memoryHereShelf says whether a shelf's lines are put in front of a
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

// readMemory resolves the filter and the open shelves once, so drawing remains
// a pure measurement of already-known words. The caller supplies now because a
// redraw must not silently turn into a clock read.
func readMemory(shelves store.MemoryShelves, here string, open map[string]bool, filter string, now time.Time) memoryReading {
	query := strings.ToLower(strings.TrimSpace(filter))
	terms := fuzzy.Terms(query)
	r := memoryReading{held: shelves.Held, letGo: shelves.LetGoCount, typed: strings.TrimSpace(filter)}

	// WHAT A CONVERSATION HERE IS TOLD, in the order it is told it — the
	// person's, the machine's, the project's — so the head row can say how many
	// lines did not fit in front of the model.
	var hereTexts []string
	for _, rank := range []func(string) bool{
		func(o string) bool { return o == store.OwnerUser },
		func(o string) bool { return o == store.OwnerMachine },
		func(o string) bool { return here != "" && o == here },
	} {
		for _, shelf := range shelves.Shelves {
			if rank(shelf.Owner) {
				for _, memory := range shelf.Memories {
					hereTexts = append(hereTexts, memory.Text)
				}
			}
		}
	}
	r.outside = len(hereTexts) - session.MemoryFits(hereTexts)

	// A MACHINE THAT HAS REMEMBERED NOTHING HAS NO LINES AT ALL: the place
	// draws its heading and whisper instead, and the whisper names /remember.
	// No head row saying "0 kept" — the emptiness law applied to a count.
	if r.bare() {
		return r
	}
	r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingHeader})
	if query == "" {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingAdd, label: "+ " + memoryAddWord})
	}
	matches := func(memory store.Memory, shelfName string) bool {
		if query == "" {
			return true
		}
		text := strings.ToLower(strings.Join([]string{memory.Text, shelfName, memoryOriginWord(memory.Origin), memoryStatusWord(memory.Status)}, " "))
		_, ok := fuzzy.Score(text, terms)
		return ok
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
		r.addShelf(shelf.Owner, name, shelf.Held, kept, open, query != "", now)
	}
	var letGo []store.Memory
	for _, memory := range shelves.LetGo {
		if matches(memory, memoryShelfLetGo) {
			letGo = append(letGo, memory)
		}
	}
	if len(letGo) > 0 {
		shown++
		r.addShelf(memoryLetGoKey, memoryShelfLetGo, shelves.LetGoCount, letGo, open, query != "", now)
	}
	if query != "" && shown == 0 {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingProse, label: fmt.Sprintf("nothing remembered says %q", query)})
	}
	return r
}

// addShelf appends one shelf's heading and, when it is open, its lines. A
// filter opens every shelf that has a match, because a match hidden under a
// closed heading is a match the person cannot see.
func (r *memoryReading) addShelf(key, name string, count int, lines []store.Memory, open map[string]bool, filtering bool, now time.Time) {
	opened := open[key] || filtering
	mark := tokens.GlyphCollapsed
	if opened {
		mark = tokens.GlyphExpanded
	}
	r.lines = append(r.lines, memoryReadingLine{
		kind: memoryReadingShelf, shelf: key,
		label: mark + " " + name + " · " + groupedInt(count),
	})
	if !opened {
		return
	}
	for _, memory := range lines {
		copy := memory
		var facts []rowField
		if word := memoryStatusWord(memory.Status); word != "" {
			facts = append(facts, rowSay(word))
		}
		if word := memoryOriginWord(memory.Origin); word != "" {
			facts = append(facts, rowSay(word))
		}
		r.lines = append(r.lines, memoryReadingLine{
			kind: memoryReadingMemory, shelf: key, memory: &copy,
			label: tokens.GlyphProseBullet + " " + memory.Text,
			facts: facts, age: sinceAt(memory.UpdatedAt, now),
		})
	}
}

// memoryOpenByDefault is which shelves a page opens with: the ones whose lines
// a conversation here is told. Other projects, the unplaced and the let-go stay
// rolled up until somebody asks.
func memoryOpenByDefault(shelves store.MemoryShelves, here string) map[string]bool {
	open := map[string]bool{}
	for _, shelf := range shelves.Shelves {
		if memoryHereShelf(shelf.Owner, here) {
			open[shelf.Owner] = true
		}
	}
	return open
}

// memoryCounts is the head row's left half.
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

// rows paints only through the palette.
func (r memoryReading) rows(width int, pal palette) []string { return r.paint(width, pal, nil) }

// paint is [memoryReading.rows] with the rows the cursor or the pointer is on
// lit; a nil lit lights nothing. Every row hangs from the body's own column,
// one cell in, where every place hangs its rows.
func (r memoryReading) paint(width int, pal palette, lit func(row int) bool) []string {
	rows := make([]string, 0, len(r.lines))
	room := width - 1
	for _, line := range r.lines {
		on := lit != nil && lit(len(rows))
		switch line.kind {
		case memoryReadingProse:
			rows = append(rows, " "+pal.dim(fit(line.label, room)))
		case memoryReadingHeader:
			left := memoryCounts(r.held, r.letGo, r.outside)
			right, plainRight := pal.dim(memoryFilterWord), memoryFilterWord
			if r.typed != "" {
				right, plainRight = pal.ink(r.typed), r.typed
			}
			rows = append(rows, " "+memoryJoin(placeHeading(left, pal), right, plainRight, room))
		case memoryReadingAdd:
			rows = append(rows, " "+placeFactInk(on, pal)(fit(line.label, room)))
		case memoryReadingShelf:
			rows = append(rows, " "+memoryRow(placeSubjectInk(on, pal), line, on, pal, room))
		case memoryReadingMemory:
			paintLabel := placeSubjectInk(on, pal)
			if line.memory != nil && line.memory.Status != store.MemoryActive {
				// History recedes: there is no strike paint in this palette.
				paintLabel = pal.dim
			}
			rows = append(rows, " "+memoryRow(paintLabel, line, on, pal, room))
		}
	}
	return rows
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

// memoryRow is ONE LIST ROW: the identity whole, then the facts as a ranked
// prefix joined by the one separator, then the age at the right. A fact that
// will not fit is dropped whole.
func memoryRow(paintLabel func(string) string, line memoryReadingLine, lit bool, pal palette, width int) string {
	facts := placeFactInk(lit, pal)
	room := width
	age := ansi.StringWidth(line.age)
	if age > 0 {
		room -= age + memoryGutter
	}
	name, cut := rowTrim(line.label, room, false)
	tail := ""
	if !cut && len(line.facts) > 0 {
		tail = rowTail(line.facts, room-ansi.StringWidth(name)-ansi.StringWidth(rowSep))
	}
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
