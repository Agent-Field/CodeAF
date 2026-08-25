package tui3

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

const (
	memoryTeachBelow   = 8
	memoryShelfShown   = 3
	memoryShelvesShown = 5
)

var memoryShelfNames = map[string]string{
	store.MemoryScopeUser:    "you",
	store.MemoryScopeProject: "this project",
	store.MemoryScopeEnv:     "this machine",
}

var memoryTeaching = []string{
	"What I hold true about you and this machine.",
	"I put a line in here when it looked like it would matter later, and I only carry it into a chat it bears on.",
	"Corrections are the point — a wrong line here is wrong in every chat.",
}

type memoryVerb struct {
	key  rune
	word string
}

type memoryStop struct {
	shelf string
	line  *store.Memory
}

type memoryReading struct {
	held    int
	letGo   int
	total   int
	shelves int
	filter  string
	teach   bool
	lines   []memoryReadingLine
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
	memoryReadingFooter
)

type memoryReadingLine struct {
	kind   memoryReadingKind
	shelf  string
	label  string
	note   string
	help   string
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
		held: shelves.Held, letGo: shelves.LetGo + shelves.Superseded,
		total: shelves.Total, shelves: len(shelves.Shelves), filter: query,
		teach: shelves.Total < memoryTeachBelow,
	}
	if r.teach {
		for _, sentence := range memoryTeaching {
			r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingProse, label: sentence})
		}
	} else {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingHeader})
	}

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
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingSection, label: "shelves · biggest first", note: memoryTypeLegend(ranked)})
	}
	shownShelves := min(len(ranked), memoryShelvesShown)
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
			note:  memoryShelfNote(shelf.shelf, newToday), age: sinceAt(newest, now),
		})
		if !open[key] {
			continue
		}
		shown := min(len(shelf.lines), memoryShelfShown)
		for j := 0; j < shown; j++ {
			memory := shelf.lines[j]
			copy := memory
			r.lines = append(r.lines, memoryReadingLine{
				kind: memoryReadingMemory, shelf: key, memory: &copy,
				label: tokens.GlyphProseBullet + " " + memory.Title,
				note:  memory.Type, help: memoryHelp(memory, now), age: sinceAt(memory.UpdatedAt, now),
			})
		}
		if more := len(shelf.lines) - shown; more > 0 {
			r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingFold, label: foldLine(more, "on this shelf")})
		}
	}
	if more := len(ranked) - shownShelves; more > 0 {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingFold, label: foldLine(more, "shelves")})
	}
	if r.teach {
		r.lines = append(r.lines, memoryReadingLine{kind: memoryReadingFooter})
	}
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

func memoryTypeLegend(shelves []rankedMemoryShelf) string {
	counts := map[string]int{}
	for _, shelf := range shelves {
		for kind, count := range shelf.shelf.ByType {
			counts[kind] += count
		}
	}
	var parts []string
	for _, kind := range []string{store.MemoryFact, store.MemoryPreference, store.MemoryDecision, store.MemoryCorrection, store.MemoryProjectState} {
		if counts[kind] > 0 {
			parts = append(parts, kind+" "+groupedInt(counts[kind]))
		}
	}
	return strings.Join(parts, " · ")
}

func memoryShelfNote(shelf store.MemoryShelf, newToday int) string {
	kinds := store.MemoryShelfTypes(shelf)
	parts := []string{}
	if len(kinds) > 0 {
		kind := kinds[0]
		for _, candidate := range kinds[1:] {
			if shelf.ByType[candidate] > shelf.ByType[kind] {
				kind = candidate
			}
		}
		if shelf.ByType[kind] != 1 {
			kind = memoryTypePlural(kind)
		}
		parts = append(parts, "mostly "+kind)
	}
	if newToday > 0 {
		parts = append(parts, groupedInt(newToday)+" new today")
	}
	return strings.Join(parts, " · ")
}

func memoryTypePlural(kind string) string {
	if kind == store.MemoryProjectState {
		return "project states"
	}
	return kind + "s"
}

func memoryHelp(memory store.Memory, now time.Time) string {
	switch memory.Status {
	case store.MemoryForgotten:
		// The store keeps no reason or author for this transition, so "you
		// corrected it" would turn an unknown origin into a person-facing fact.
		return "let go"
	case store.MemorySuperseded:
		return "let go"
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
func (r memoryReading) rows(width int, pal palette) []string {
	rows := make([]string, 0, len(r.lines))
	for _, line := range r.lines {
		switch line.kind {
		case memoryReadingProse:
			rows = append(rows, pal.dim(fit(line.label, width)))
		case memoryReadingBlank:
			if len(rows) > 0 && rows[len(rows)-1] != "" {
				rows = append(rows, "")
			}
		case memoryReadingHeader:
			left := memoryCounts(r.held, r.shelves, r.letGo)
			rows = append(rows, memoryJoin(pal.ink(left), pal.dim("type to filter"), "type to filter", width))
		case memoryReadingSection:
			rows = append(rows, memoryJoin(pal.muted(line.label), pal.dim(line.note), line.note, width))
		case memoryReadingShelf:
			middle := line.note
			if middle != "" {
				middle = "  " + middle
			}
			rows = append(rows, memoryThree(pal.muted(line.label), pal.dim(middle), pal.dim(line.age), line.label, middle, line.age, width))
		case memoryReadingMemory:
			note := line.note
			if width >= 80 && line.help != "" {
				note += "  " + line.help
			}
			paintLabel := pal.ink
			if line.memory != nil && line.memory.Status != store.MemoryActive {
				// There is no strike paint in this palette, so history takes the
				// documented fallback and recedes instead of borrowing a raw style.
				paintLabel = pal.dim
			}
			rows = append(rows, memoryThree(paintLabel(line.label), pal.dim(note), pal.dim(line.age), line.label, note, line.age, width))
		case memoryReadingFold:
			rows = append(rows, pal.dim(fit(line.label, width)))
		case memoryReadingFooter:
			text := memoryCounts(r.held, 0, r.letGo)
			if text != "" {
				text += " · "
			}
			text += "nothing here is a setting, all of it is editable"
			rows = append(rows, pal.dim(fit(text, width)))
		}
	}
	return rows
}

func memoryCounts(held, shelves, letGo int) string {
	var parts []string
	if held > 0 {
		parts = append(parts, groupedInt(held)+" held")
	}
	if shelves > 0 {
		parts = append(parts, groupedInt(shelves)+" shelves")
	}
	if letGo > 0 {
		parts = append(parts, groupedInt(letGo)+" let go")
	}
	return strings.Join(parts, " · ")
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

func memoryThree(paintedLeft, paintedMiddle, paintedRight, plainLeft, plainMiddle, plainRight string, width int) string {
	rightWidth := ansi.StringWidth(plainRight)
	if rightWidth > 0 && rightWidth+2 < width {
		bodyWidth := width - rightWidth - 2
		body := fit(paintedLeft+paintedMiddle, bodyWidth)
		gap := width - ansi.StringWidth(body) - rightWidth
		return body + strings.Repeat(" ", max(1, gap)) + paintedRight
	}
	return fit(paintedLeft+paintedMiddle, width)
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
	}
	return memoryStop{}, false
}

func (r memoryReading) verbs(i int) []memoryVerb {
	stop, ok := r.at(i)
	if !ok {
		return nil
	}
	if stop.line == nil {
		return []memoryVerb{{key: '\r', word: "enter open"}}
	}
	return []memoryVerb{{key: 'e', word: "e fix the wording"}, {key: 'f', word: "f forget it"}}
}

// THERE IS ONE EMPTY STATE HERE AND THE READING ITSELF IS IT.
//
// This file used to carry a `memoryTeach` that said "nothing is remembered yet"
// and then repeated the command surface's `memory is off for this session` note
// under it. Both halves were wrong on a PLACE. A machine that has remembered
// nothing meets [memoryTeaching] — the three sentences that say what this is
// for, which is what a nearly-empty page is worth — and a machine with memory
// switched off never reaches a body at all: [app.openMemory] refuses to open
// the place and says so on the transcript's own note line ([memoryOffNote]).
// So an empty page teaching that memory might be off would be a page guessing
// at a state the door has already ruled out.
