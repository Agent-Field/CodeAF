package tui3

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The model palette: /model with nothing after it, and omp's picker opens.
//
// It is called picker and not palette because [palette] in styles.go is already
// this surface's colour table. The FILE is palette.go because the thing it
// holds is the palette gesture — a filter box you type into, a short list under
// it, arrows to move, enter to switch, esc to leave everything exactly as it
// was.
//
// Three properties are the whole design:
//
//   - It never fetches. The list was resolved before it opened (models.go), so
//     the first frame after /model is a list and never a spinner.
//   - It is bottom-anchored and takes the input line's place. The conversation
//     shrinks above it; nothing pops up over the middle of what somebody was
//     reading.
//   - It changes nothing until enter. esc restores the draft that was being
//     typed, the model in use, and the frame — the picker holds its own filter
//     text, and the person's half-written sentence is never in it.
const pickerRows = 12

// picker is the overlay's whole state. The zero value is closed.
type picker struct {
	open bool

	// all is the list as it was resolved, and lower the same ids folded once at
	// open: filtering is per keystroke over every row, and lowercasing a few
	// hundred ids on each of them is the one allocation this path cannot afford
	// to repeat.
	all   []Model
	lower []string
	// score is per-model scratch, indexed by the same index as all, reused
	// across keystrokes.
	score []int
	// shared are the slugs MORE THAN ONE model on offer carries — `kimi-k3`
	// where both `moonshotai/kimi-k3` and a mirror of it are listed. A narrow
	// frame drops a row's author first (rowfit.go), and it may only do that
	// where the slug left behind still names one row.
	//
	// IT IS TAKEN OVER THE WHOLE LIST AND NOT OVER THE FILTER'S HITS, once,
	// when the list opens. A name that grew an author back because a keystroke
	// narrowed the list would be a row changing its own identity while somebody
	// was reading it, and the filter's hits change on every key.
	shared map[string]bool

	// hits are indexes into all, in rank order — the models actually on offer.
	hits []int
	// list is what is DRAWN: every hit, with the lanes of an unfolded model
	// standing in place under it ([pickRow]). The cursor and the scroll walk
	// this and not the hits, because a lane row is a row a person stops on.
	list []pickRow
	// cursor indexes list, and top is the first row drawn.
	cursor int
	top    int

	// unfold is the model whose lanes are open, empty when none is. ONE AT A
	// TIME on purpose: the fold is a way of looking closer at one row, and a
	// list with four models open is a list with no shape left.
	unfold string
	// lanes is what was believed about that model's lanes at the moment it was
	// opened, best first, and first names the lane an `@` filter asked to see
	// at the top of them.
	lanes []laneView
	first string
	// auto is the lane the CHOOSER would send the next turn to, taken with the
	// views at the moment the fold opened. It is not [bestLane]'s answer and
	// must not be: this file's own sort orders the rows a person reads, and the
	// chooser decides where a request goes — and the `auto` row is a claim about
	// the second of those. Empty when nothing is believed, which draws no name.
	auto string
	// pin is the lane this conversation is held to, empty for auto. It is a
	// snapshot taken when the list opened, exactly as current is, and for the
	// same reason: it answers "what am I on", which cannot change while a modal
	// overlay owns the keyboard.
	pin string
	// guard is whether a slow answer may be rescued elsewhere. It rides here
	// because it is a fact about what `auto` PROMISES, and this list is where a
	// person decides whether to leave the choosing to it.
	guard bool
	// ascii is whether this terminal was refused box drawing, so the two marks
	// on the fold's own rows have a plain spelling. It is a snapshot like the
	// rest of them, and the list is closed long before a terminal could change
	// its mind about glyphs.
	ascii bool
	// laneSlot is the CONFIG SLOT whose lane row this list may write, and empty
	// when there is none.
	//
	// IT IS THE SUBJECT OF THE FOLD AND NOT THE NAME OF THE DOOR. This was a
	// `folds bool` set by /model alone, which made the settings panel's `your
	// model` row a plainer list than the overlay reached by a slash — the same
	// question, answered two ways, on one surface. It is not the door that
	// decides whether a fold means anything; it is whether enter inside it has
	// a row to write. /model and the panel's conversation row both answer
	// "which model, on which machine", so both carry [talkSlot] and both fold.
	// A media slot, a role, a task's model carry nothing: the registry has one
	// lane row ([config.LaneSlotTalk], and lanes.go's [laneSlotForRow] is the
	// whole map), so a fold under those would offer a gesture their enter could
	// not honour — and that is the emptiness law, not a door's permission.
	laneSlot string

	// current is the model in use when the picker opened. It is what the accent
	// marks, and it is deliberately a snapshot: the mark answers "what am I on",
	// which cannot change while a modal overlay owns the keyboard.
	current string

	// task is the NODE this list is being chosen for, and 0 is the conversation —
	// which is every /model, every press on the status row out in the thread, and
	// every settings row. It is set only by [app.openTaskPicker], and what it
	// changes is where enter goes: one list, two subjects, and the subject is
	// decided when the list is opened rather than guessed at when it closes
	// ([app.pickerKey]). It is on the picker rather than on the app so that
	// [picker.close] forgets it with everything else — a target left behind by a
	// cancelled list is the next /model retargeting a task nobody was looking at.
	task uint64

	filter editor
}

// startFor opens the picker over the rows ONE SLOT can take: the list, narrowed
// by that slot's own question (models.go's [modelFilter]), with current marked.
//
// It is the single door every slot comes through — /model and the conversation
// rows pass [chatModel], the "looking" row passes [inspectsImages] — so the answer
// to "which models does this slot offer" is one predicate named at the call
// site rather than a list assembled there.
func (p *picker) startFor(models []Model, current string, keep modelFilter) {
	p.start(keepModels(models, keep), current)
}

// start opens the picker over models with current marked.
func (p *picker) start(models []Model, current string) {
	*p = picker{open: true, all: models, current: current}
	p.shared = sharedSlugs(models)
	p.lower = make([]string, len(models))
	for i, model := range models {
		p.lower[i] = strings.ToLower(model.ID)
	}
	p.score = make([]int, len(models))
	p.rank()
	// The cursor opens ON the model in use. A picker that opened on row zero
	// would make enter — the key a person presses to confirm — a model change
	// they did not ask for.
	for at, row := range p.list {
		if row.lane == laneNone && p.all[p.hits[row.hit]].ID == current {
			p.cursor = at
			break
		}
	}
	p.follow(pickerRows)
}

// pickRow is one drawn row: which hit it belongs to, and which of that model's
// lanes it is.
//
// laneNone is the model's own row. The two ends of an unfolded block are the
// `auto` row and the `openrouter` row rather than lanes, because neither of
// them is a machine — they are the two ways of declining to name one.
type pickRow struct {
	hit  int
	lane int
}

const (
	laneNone   = -1
	laneAutoAt = -2
	laneRoutAt = -3
)

// relist rebuilds the drawn rows from the hits and the fold. It is called
// wherever either changes, and nowhere else: a cursor walking a list that no
// longer exists is the whole class of bug this one function prevents.
func (p *picker) relist() {
	p.list = p.list[:0]
	for at, hit := range p.hits {
		p.list = append(p.list, pickRow{hit: at, lane: laneNone})
		if p.unfold == "" || p.all[hit].ID != p.unfold {
			continue
		}
		p.list = append(p.list, pickRow{hit: at, lane: laneAutoAt})
		for i := range p.lanes {
			p.list = append(p.list, pickRow{hit: at, lane: i})
		}
		p.list = append(p.list, pickRow{hit: at, lane: laneRoutAt})
	}
}

// close puts the picker away and forgets the filter. The next /model opens on
// the whole list, which is the only thing a person can predict; a picker that
// remembered last week's query would open onto a list with no explanation.
func (p *picker) close() { *p = picker{} }

// rank re-filters against the filter box: case-insensitive, EVERY TOKEN MUST
// MATCH, and each token matches in one of three tiers — prefix, then substring,
// then subsequence.
//
// THE QUERY IS TOKENS AND NOT A PHRASE. A person hunting a model types the
// pieces they remember in the order they remember them, and the pieces are not
// adjacent in the id: "ds v4" is deepseek/deepseek-v4-flash, "claude 4.5" is
// anthropic/claude-sonnet-4.5. Whitespace splits, and the tokens are ANDed — a
// second word narrows a list, which is the only thing typing more can sensibly
// do.
//
// THE TIERS ARE A LADDER AND THE SCORE IS THEIR RUNG PLUS THE OFFSET, summed
// over the tokens. A prefix scores zero, so "gpt" still puts gpt-5-classic
// first; a substring scores its offset above every prefix, so openai/gpt-4.1
// (offset 7) comes before anthropic/claude-gpt-echo (offset 17); and a
// subsequence — the letters in order, gaps allowed — scores above every
// substring, so the fuzzy hits land at the BOTTOM of the list rather than mixed
// through it. That ordering is what makes the third tier affordable at all:
// "sonnet" over six hundred ids does match a lot of them loosely, and every one
// of those sits under the models that really carry the word. Ties keep source
// order, which is the catalog's, so an empty box shows the list as handed over.
func (p *picker) rank() {
	// THE QUERY IS SPLIT BEFORE IT IS SCORED. A token that parses as a question
	// about the machines behind a model (lanes.go's [parseLaneTerm]) is a
	// filter, and everything else is a word to be ranked exactly as it always
	// was — which is why a person who has never heard of any of this types the
	// same query and gets the same list.
	tokens, terms := splitQuery(p.filter.String())
	now := timeNow()
	p.hits = p.hits[:0]
	p.unfold, p.lanes, p.first, p.auto = "", nil, "", ""
	for i, id := range p.lower {
		if len(terms) > 0 && !keepsLanes(p.all[i], terms, now) {
			continue
		}
		if len(tokens) == 0 {
			p.hits = append(p.hits, i)
			continue
		}
		total, matched := 0, true
		for _, token := range tokens {
			score, hit := tokenScore(id, token)
			if !hit {
				matched = false
				break
			}
			total += score
		}
		if !matched {
			continue
		}
		p.score[i] = total
		p.hits = append(p.hits, i)
	}
	if len(tokens) > 0 {
		sort.SliceStable(p.hits, func(a, b int) bool { return p.score[p.hits[a]] < p.score[p.hits[b]] })
	}
	p.orderByLanes(terms, now)
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose.
	p.cursor, p.top = 0, 0
	// AN `@lane` QUERY OPENS THE ROW IT WAS ABOUT. Somebody who typed
	// `@cloudflare` asked a question about a machine, and answering it with a
	// list of model names they would then have to open one by one would be the
	// filter working and the surface not.
	if word, ok := laneTermWord(terms); ok && len(p.hits) > 0 {
		p.unfoldAt(0, word, now)
	}
	p.relist()
}

// splitQuery divides what is typed into the words that rank and the terms that
// filter. Every token that does not parse is a word, unchanged.
func splitQuery(query string) ([]string, []laneTerm) {
	var words []string
	var terms []laneTerm
	for _, token := range strings.Fields(strings.ToLower(query)) {
		if term, ok := parseLaneTerm(token); ok {
			terms = append(terms, term)
			continue
		}
		words = append(words, token)
	}
	return words, terms
}

// keepsLanes is every keeping term ANDed over one model.
func keepsLanes(model Model, terms []laneTerm, now time.Time) bool {
	views := laneViews(model.ID, now)
	for _, term := range terms {
		if !term.keeps(model, views) {
			return false
		}
	}
	return true
}

// orderByLanes is the two words that ORDER rather than keep. They come last, so
// `deep fast` is the deepseek rows soonest-first rather than the fastest rows
// that happen to say deep.
func (p *picker) orderByLanes(terms []laneTerm, now time.Time) {
	for _, term := range terms {
		switch term.kind {
		case termFast:
			sort.SliceStable(p.hits, func(a, b int) bool {
				return laneFeel(bestOf(p.all[p.hits[a]], now)) < laneFeel(bestOf(p.all[p.hits[b]], now))
			})
		case termCheap:
			sort.SliceStable(p.hits, func(a, b int) bool {
				return lanePrice(bestOf(p.all[p.hits[a]], now)) < lanePrice(bestOf(p.all[p.hits[b]], now))
			})
		}
	}
}

// bestOf is one model's best-believed lane, or a lane that knows nothing —
// which sorts last under both words rather than first under either.
func bestOf(model Model, now time.Time) laneView {
	best, _ := bestLane(laneViews(model.ID, now))
	return best
}

// lanePrice is what a million output tokens cost on a lane, and a very large
// number for a lane with no published tariff: `cheap` must not put the rows
// nobody knows the price of at the top of the list.
func lanePrice(view laneView) float64 {
	if view.PriceOut <= 0 {
		return math.Inf(1)
	}
	return view.PriceOut * 1_000_000
}

// laneTermWord is the first `@lane` word in a query, if there is one.
func laneTermWord(terms []laneTerm) (string, bool) {
	for _, term := range terms {
		if term.kind == termLane {
			return term.word, true
		}
	}
	return "", false
}

// The three rungs, far enough apart that no offset inside one can reach the
// next: an id is a few dozen characters, so a thousand is a wall.
const (
	rungPrefix      = 0
	rungSubstring   = 1_000
	rungSubsequence = 1_000_000
)

// tokenScore is how well one token matches one id, and whether it matches at
// all. Lower is better; the rungs are [rungPrefix] and friends.
func tokenScore(id, token string) (int, bool) {
	switch {
	case token == "" || strings.HasPrefix(id, token):
		return rungPrefix, true
	}
	if at := strings.Index(id, token); at >= 0 {
		return rungSubstring + at, true
	}
	if at, ok := subsequenceAt(id, token); ok {
		return rungSubsequence + at, true
	}
	return 0, false
}

// subsequenceAt reports whether every rune of token appears in id in order, and
// where the first of them sits — so "ds" over deepseek scores by how early the
// run starts, the same way a substring scores by its offset.
func subsequenceAt(id, token string) (int, bool) {
	first, at := -1, 0
	for _, want := range token {
		found := strings.IndexRune(id[at:], want)
		if found < 0 {
			return 0, false
		}
		if first < 0 {
			first = at + found
		}
		at += found + utf8.RuneLen(want)
	}
	if first < 0 {
		return 0, false
	}
	return first, true
}

// move walks the list, clamping at both ends rather than wrapping: a list that
// wraps makes "hold ↓ until it stops" an infinite gesture.
func (p *picker) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.list))
	p.follow(pickerRows)
}

// follow scrolls the window by the least that keeps the cursor inside it.
func (p *picker) follow(height int) { p.top = listTop(p.cursor, p.top, len(p.list), height) }

// ── THE LANES UNDER A MODEL ─────────────────────────────────────────────────
//
// `→` on a row opens the machines behind it, in place, indented under the name
// they serve. It is a fold and not a second overlay for the reason there is one
// list on this surface at all: the question "which of these" and the question
// "which machine behind this one" are the same question at two depths, and
// answering the second somewhere else would make a person leave the list to ask
// it and come back to find their filter gone.
//
// A MODEL WHOSE LANES NOBODY HAS MEASURED DOES NOT OPEN. There is nothing
// truthful to put under it, and a fold that opened onto one dim line saying so
// would be a gesture that punishes the person for trying it (design-law-v2 §16
// EMPTINESS).

// unfoldAt unfolds the model at hit `at`, with `first` — a lane an `@` filter asked
// about — lifted to the top of the lanes. It reports whether anything opened.
func (p *picker) unfoldAt(at int, first string, now time.Time) bool {
	if at < 0 || at >= len(p.hits) {
		return false
	}
	model := p.all[p.hits[at]]
	views := laneViews(model.ID, now)
	if len(views) == 0 {
		return false
	}
	if first != "" {
		if named, ok := laneNamed(views, first); ok {
			lifted := []laneView{named}
			for _, view := range views {
				if !strings.EqualFold(view.Name, named.Name) {
					lifted = append(lifted, view)
				}
			}
			views = lifted
		}
	}
	p.unfold, p.lanes, p.first, p.auto = model.ID, views, first, laneAuto(model.ID, views, now)
	return true
}

// fold closes whatever is open. It answers false when nothing was, so `←` on a
// folded row can fall through to the filter box's own left.
func (p *picker) fold() bool {
	if p.unfold == "" {
		return false
	}
	p.unfold, p.lanes, p.first, p.auto = "", nil, "", ""
	return true
}

// unfoldHere is `→` and `tab`: open the model the cursor is on, or — when the
// cursor is already inside an open block — leave it open and do nothing, which
// is what a person pressing the key again means.
func (p *picker) unfoldHere() bool {
	if p.laneSlot == "" || p.cursor < 0 || p.cursor >= len(p.list) {
		return false
	}
	row := p.list[p.cursor]
	if row.lane != laneNone {
		return false
	}
	if p.all[p.hits[row.hit]].ID == p.unfold {
		return false
	}
	if !p.unfoldAt(row.hit, "", timeNow()) {
		return false
	}
	p.relist()
	// THE CURSOR STAYS ON THE MODEL IT OPENED. The rows appeared under it, so
	// the row a person was looking at is still the row they are on, and `↓`
	// walks into what they just asked to see.
	for at, drawn := range p.list {
		if drawn.hit == row.hit && drawn.lane == laneNone {
			p.cursor = at
			break
		}
	}
	p.follow(pickerRows)
	return true
}

// foldHere is `←` and `tab` on an open block: close it and put the cursor back
// on the model it belonged to, wherever inside the block it had got to.
func (p *picker) foldHere() bool {
	if p.cursor < 0 || p.cursor >= len(p.list) {
		return false
	}
	hit := p.list[p.cursor].hit
	if !p.fold() {
		return false
	}
	p.relist()
	for at, drawn := range p.list {
		if drawn.hit == hit {
			p.cursor = at
			break
		}
	}
	p.follow(pickerRows)
	return true
}

// cursorToPin puts the cursor, inside an open fold, on the row the pin names —
// the lane itself when one is pinned, and the `auto` or `openrouter` row when
// none is.
//
// It is [picker.start]'s rule applied one level down: a list opened AT a
// setting opens ON that setting's value, so enter with nothing typed confirms
// rather than changes. The panel's `lane` row is the only caller — /model opens
// on the MODEL in use and walks into the fold from there.
func (p *picker) cursorToPin() {
	for at, row := range p.list {
		if row.lane != laneNone && p.marked(at) {
			p.cursor = at
			p.follow(pickerRows)
			return
		}
	}
}

// foldKey is `tab`, `→` and `←` over this list — the fold's whole key map, in
// one place because the list has two doors ([app.pickerKey] and settings.go's
// [app.sheetSelectKey]) and a gesture that opened the machines from one of them
// and did nothing from the other is the exact drift [picker.navigate] exists to
// prevent. It answers whether it took the key; false falls through to the walk.
//
// THE ONE COMPROMISE THE FOLD COSTS. `tab` opens and closes outright, because
// it means nothing else in a box you type into. `→` and `←` are the keys a
// person reaches for at a tree, and they are ALSO how the caret walks the
// filter text — so they open and close only from the END and the START of what
// is typed, where there is no character left to step over. With an empty box,
// which is where this list spends most of its life, that is every press.
func (p *picker) foldKey(name string) bool {
	switch name {
	case "tab":
		if !p.unfoldHere() {
			p.foldHere()
		}
		return true
	case "right":
		return p.filter.cursor >= len(p.filter.value) && p.unfoldHere()
	case "left":
		return p.filter.cursor <= 0 && p.foldHere()
	}
	return false
}

// laneUnder is the lane row the cursor is on: the view, and which of the three
// kinds of row it is. It is what enter reads before it writes anything.
func (p *picker) laneUnder() (pickRow, bool) {
	if p.cursor < 0 || p.cursor >= len(p.list) {
		return pickRow{}, false
	}
	row := p.list[p.cursor]
	if row.lane == laneNone {
		return pickRow{}, false
	}
	return row, true
}

// ── the overlay grammar, shared by every list this surface opens ────────────
//
// There is ONE bottom-anchored list on this surface and three things open it:
// /model (picker, above), a typed "/" (the command list, commands.go) and a
// typed "@" (the file completion, files.go). They share the three functions
// below — the cursor walk, the scroll, and the row — so that they cannot drift
// into three overlays that each look almost like the others. What differs
// between them is what they LIST, which is the only thing that should.

// moveCursor walks a list of count rows by delta, clamping at both ends.
func moveCursor(cursor, delta, count int) int {
	if count == 0 {
		return 0
	}
	cursor += delta
	if cursor < 0 {
		return 0
	}
	if cursor >= count {
		return count - 1
	}
	return cursor
}

// listTop scrolls a window of `height` rows by the least that keeps the cursor
// inside it.
func listTop(cursor, top, count, height int) int {
	if height <= 0 {
		return top
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+height {
		top = cursor - height + 1
	}
	if top > count-height {
		top = count - height
	}
	if top < 0 {
		return 0
	}
	return top
}

// overlayRow is ONE row of ONE overlay, and every list draws through it.
//
// Three tiers and no fourth. What the row is ABOUT — the model in use, and
// nothing else so far — is accent wherever it sits in the list. The row under
// the cursor is ink and bold, so it stays the brightest thing on a monochrome
// terminal too. Everything else is dim, because a list of six hundred names
// that all shout is a list nobody can read down. The note trails on the right
// and the label gives way before it does: a truncated name is still
// recognizable, and "164k" cut in half is a wrong number.
//
// THE EMPHASIZED ROW IS A GROUND, AND THE WHOLE LINE IS IN IT. Emphasis used to
// be a bold label and nothing else, which left the cursor row reading as half a
// row: the lead was accent, the name was bright, and the tail that carries the
// window, the price and the arena score stayed dim grey — the three facts a
// person is actually comparing, greyed out on the one row they were comparing
// them ON. So the emphasis now spans the line, lead to note, padded to the full
// width, and the note joins it in ink rather than staying behind in dim.
//
// WHICH STEP EACH ROW DRAWS IS THE GROUND LADDER'S ANSWER AND NOT THIS FILE'S.
// A list has up to three things going on at once and they are three different
// facts, so they take three different rungs:
//
//	the keyboard cursor   THE CURSOR STEP — where ↑/↓ has got to
//	the pointer's row     THE CURSOR STEP — the same rung, deliberately
//	the marked row        THE SELECTED STEP — the one this terminal is IN
//
// CURSOR AND HOVER ARE ONE STEP, NOT TWO. This surface used to draw them at two
// different weights, on the argument that a pointer crossing a list must not
// look like the cursor moving. The ladder refuses that argument: whether a
// person arrived at a row with the mouse or with `↓`, the row they are on is the
// row they are on, and it does not change appearance depending on which hand
// they used. What still tells the two apart is the LEAD — `›` where enter would
// act, `·` where the pointer is — which is a mark and not a rung.
//
// AND THE MARKED ROW IS THE ONE THAT WAS MISSING ITS GROUND. The conversation
// this terminal is actually in is the definition of the ladder's selected step —
// the chosen thing, persistent, still true when nobody is touching the list —
// and it used to be an accent label on the bare terminal background while the
// row a person was merely scrolling PAST wore the louder ground. That is the
// ladder upside down, and a roster where the session you are sitting in looks
// less chosen than the one under the cursor is a roster that answers "where am
// I" with the wrong row.
func overlayRow(label, note string, selected, marked, hovered bool, width int, pal palette) string {
	// The two-valued form every list but home draws: marked or not, which is
	// [markFront] or [markNone].
	return overlayRowTinted(label, note, nil, selected, markIf(marked), hovered, width, pal)
}

// markIf is the two-valued mark said in the three-valued type. Only home has a
// third state, because only home lists conversations this terminal is holding.
func markIf(marked bool) rowMark {
	if marked {
		return markFront
	}
	return markNone
}

// noteInk is how a row's trailing fact is painted, for the one list where the
// tail is not a fact but an ANSWER (connectcaps.go's capability rows: yes, ask
// first, off). Every other list wants the rule below — dim, and ink on the
// selected row — and passes nil to say so.
//
// It is a hook rather than a second row-drawing function because the row is the
// row: the lead, the band, the hover step and the two-line law at [tierPhone]
// are decided in one place for every list on this surface, and a list that drew
// its own would be a second grammar to keep in step.
type noteInk func(pal palette, note string, selected bool) string

// paintNote is the ordinary rule, and the hook where one was given.
func paintNote(tint noteInk, pal palette, note string, selected bool) string {
	if tint != nil {
		return tint(pal, note, selected)
	}
	if selected {
		return pal.ink(note)
	}
	return pal.dim(note)
}

// rowMark is how strongly a row is marked as THE ONE THIS TERMINAL IS IN. It is
// three-valued because a terminal can now hold several conversations: the one on
// screen, the ones open behind it, and everything else on the machine.
//
// IT IS A PAINT AND NOT A WORD, on purpose. Home's left column is forty-six
// cells wide and every column spent on furniture is a column taken from the name
// the row is about — which is the argument the short spelling of `another
// window` already makes one file over.
type rowMark uint8

const (
	// markNone is a row this terminal does not hold.
	markNone rowMark = iota
	// markOurs is a conversation this terminal has open behind the one on
	// screen: the same treatment as the front one, at the tier below it.
	markOurs
	// markFront is the CHOSEN ROW OF AN OVERLAY — the model in use, the
	// conversation a picker would re-open — and it keeps the ladder's selected
	// step: an overlay is a modal list with a visible cursor in it, and the
	// chosen row's band is the language those lists have always spoken.
	markFront
	// markHere is home's own conversation — the row esc drops back into — and
	// it is a SEPARATE mark because home is a dashboard, not an overlay: a
	// persistent band on a resting page read, every time, as a cursor nobody
	// had moved. It paints the label in the body ink, takes no ground at all,
	// and says what it is in a word instead (home.go's homeHereWord).
	markHere
)

// overlayNoteRoom is HOW MANY CELLS A ROW'S NOTE MAY HAVE, given the label in
// front of it and the width of the frame.
//
// It is a function rather than four lines inside [overlayRowTinted] because a
// list that RANKS ITS FACTS has to know the answer before it composes the note:
// rowfit.go drops whole facts rather than cutting one in half, and it can only
// do that if it is told the budget it is fitting into. The settings sheet's
// money rows are the first callers (settingspend.go) and the arithmetic is the
// one below, unchanged and in one place, so the budget a caller fits into is by
// construction the budget this row will hand it.
func overlayNoteRoom(label string, width int) int {
	room := width - 2
	floor := ansi.StringWidth(label)
	if half := room - room/2; floor > half {
		floor = half
	}
	return room - floor - rowGutter
}

func overlayRowTinted(label, note string, tint noteInk, oncursor bool, marked rowMark, hovered bool, width int, pal palette) string {
	lead := overlayLead(oncursor, hovered, pal)
	// THE NOTE IS CUT TO THE ROW BEFORE THE ROW IS BUDGETED AROUND IT. The label
	// absorbs whatever the note leaves and the gap below clamps at one cell, so a
	// note longer than the terminal used to be appended WHOLE to an empty label —
	// the row ran past the edge by however long the note was, and no amount of
	// squeezing the label could pull it back. What it may take is everything but
	// the lead and that one cell of gap. Settings' `tool exceptions` is the row
	// that found it: a value naming ten tools is 141 cells against a 60-cell
	// terminal, which is LAW 1 (a place takes exactly the frame) broken by a
	// value a person chose.
	room := width - 2
	if note != "" {
		// AND THE LABEL KEEPS A FLOOR UNDER IT. The label used to absorb
		// whatever the note left, which on a long note left it NOTHING: the row
		// drew a full-width value with no name in front of it, and a person
		// reading down the column could not tell which setting they were
		// looking at. So the note may take the row's second half and no more —
		// or all of it but the label's own width, when the label is the shorter
		// of the two — and the label gives way only inside what is left.
		//
		// EVERY LIST THAT RANKS ITS FACTS HANDS US A NOTE THAT ALREADY FITS
		// (rowfit.go drops whole facts rather than cutting one in half), so this
		// is the floor under the lists that pass a note they did not budget.
		note = fit(note, overlayNoteRoom(label, width))
	}
	if note != "" {
		room -= ansi.StringWidth(note) + rowGutter
	}
	label = fit(label, room)

	// lifted is whether this row wears a ground at all, which is the one thing
	// the note's ink turns on: dim grey on a raised ground is grey on grey.
	lifted := oncursor || hovered || marked == markFront

	var painted string
	switch {
	case marked == markFront:
		painted = pal.accent(label)
	case marked == markHere:
		// HOME'S OWN CONVERSATION IS A FACT, NOT A SELECTION. It wore the
		// front mark's accent-on-selected band for a wave, and on a resting
		// dashboard that band was the loudest thing in sight — read, every
		// time, as a cursor nobody had moved. Ground bands on home mean one
		// thing only: where a person's hands are. So the row says what it is
		// the way every identity on this surface is said — in words: ink for
		// the label (readable above its dim siblings, junior to nothing) and
		// `here` on the tail (homeNote), with no ground and no accent.
		painted = pal.ink(label)
	case marked == markOurs:
		// Open here, and not the one being drawn. One tier under the here
		// row's ink, so a person's eye reads "this terminal has these" as one
		// group rather than as two unrelated paints.
		painted = pal.muted(label)
	case oncursor:
		painted = pal.ink(label)
	default:
		painted = pal.dim(label)
	}
	if oncursor {
		painted = pal.bold(painted)
	}
	line := lead + painted
	if note != "" {
		gap := width - 2 - ansi.StringWidth(label) - ansi.StringWidth(note)
		if gap < rowGutter {
			gap = rowGutter
		}
		// THE NOTE IS INSIDE THE GROUND, so it is painted as part of it: dim ink
		// on a raised ground is grey on grey, and the tail is the half of the row
		// a person is reading when they stop on it.
		line += strings.Repeat(" ", gap) + paintNote(tint, pal, note, lifted)
	}
	// AN OVERLAY'S CHOSEN ROW OUTRANKS THE CURSOR ON THE ROW IT SHARES WITH IT
	// — both can be true of one row, and the louder step wins so the row never
	// gets quieter for being arrived at; the cursor is still said, on the lead.
	// Home's own conversation deliberately is not in this switch: on a
	// dashboard the ground is the hand's and only the hand's ([markHere]).
	switch {
	case marked == markFront:
		return pal.selected(line, width)
	case oncursor, hovered:
		return pal.cursor(line, width)
	}
	return line
}

// overlayLead is the two cells in front of every row: the cursor's mark, the
// pointer's, or nothing. It is its own function because a wrapped row draws it
// on the first line and pads to it on the second — the same two cells either
// way, so the label starts in the same column on both.
func overlayLead(selected, hovered bool, pal palette) string {
	switch {
	case selected:
		return pal.accent("› ")
	case hovered:
		return pal.accent("· ")
	}
	return "  "
}

// ── the two-line row, at tierPhone ──────────────────────────────────────────
//
// A row is a label and a dim tail of facts, and on a wide frame they share one
// line with the label giving way first ([overlayRow]). On a phone there is no
// width to share: an id and "200k · $3/$15 per M · elo 1300" cannot both be on
// a forty-four-cell line, and the row that came out of that arithmetic was a
// truncated name beside a truncated number — the two halves of the row both
// cut, neither readable.
//
//	› anthropic/claude-sonnet-4.5
//	    200k · $3/$15 per M · elo 1300
//
// So at [tierPhone] the tail takes a line of its own, indented under the label
// it belongs to. THE PAIR IS ONE ROW and everything downstream treats it as
// one: the selection band spans both lines, the pointer over either line is
// over the row, and the window never draws the first line of a pair whose
// second would not fit (see [overlayFill]).
//
// A row with no tail — most of the file completion's paths — stays one line.
// A blank second line under every path would spend half the screen saying
// nothing.

// overlayIndent is where a wrapped tail starts: the row's own two-cell lead,
// plus two more so the tail reads as hanging under the label rather than as a
// row of its own.
const overlayIndent = 4

// phoneList reports whether lists on a frame this wide wrap their tails.
func phoneList(width int) bool { return layoutTier(width) == tierPhone }

// overlayItemLines is how many SCREEN lines one row takes. It is the ONE
// answer, asked by the fill that draws the rows and by the height that reserves
// the frame's rows for them — two counts that must agree or the list is drawn
// into a block of the wrong size.
func overlayItemLines(width int, note string) int {
	if note != "" && phoneList(width) {
		return 2
	}
	return 1
}

// overlayLines is one row as the lines it takes: [overlayRow] everywhere, and
// the label/tail pair at [tierPhone].
func overlayLines(label, note string, selected, marked, hovered bool, width int, pal palette) []string {
	return overlayLinesTinted(label, note, nil, selected, marked, hovered, width, pal)
}

func overlayLinesTinted(label, note string, tint noteInk, selected, marked, hovered bool, width int, pal palette) []string {
	if overlayItemLines(width, note) == 1 {
		return []string{overlayRowTinted(label, note, tint, selected, markIf(marked), hovered, width, pal)}
	}
	head := overlayLead(selected, hovered, pal)
	painted := fit(label, width-2)
	switch {
	case marked:
		painted = pal.accent(painted)
	case selected:
		painted = pal.ink(painted)
	default:
		painted = pal.dim(painted)
	}
	if selected {
		painted = pal.bold(painted)
	}
	head += painted

	// The tail keeps the row's own ink rule: dim, and ink on the selected row,
	// because dim grey on the selection band is grey on grey — and the tail is
	// the half of the row a person stopped on the row to read.
	tail := strings.Repeat(" ", overlayIndent) +
		paintNote(tint, pal, fit(note, width-overlayIndent), selected)

	switch {
	case selected:
		return []string{pal.selected(head, width), pal.selected(tail, width)}
	case hovered:
		return []string{pal.cursor(head, width), pal.cursor(tail, width)}
	}
	return []string{head, tail}
}

// overlayWindow is how many lines the rows from top take, stopping at the
// ceiling the list was given. A row that would straddle the bottom edge is not
// counted, because it is not drawn ([overlayFill.add]).
//
// note answers what row i's tail is — the only thing the count needs, since the
// tail is what decides whether the row is one line or two.
func overlayWindow(width, top, count, ceiling int, note func(int) string) int {
	lines := 0
	for at := top; at < count && lines < ceiling; at++ {
		take := overlayItemLines(width, note(at))
		if lines+take > ceiling {
			break
		}
		lines += take
	}
	return lines
}

// overlayItems is the item-space window a list follows its cursor within, given
// the SCREEN rows the frame handed it. At [tierPhone] a row can be two lines, so
// half the rows is the count that cannot overflow — which is what makes the
// cursor's row always fit whole inside the window it is scrolled into.
func overlayItems(n, width int) int {
	if phoneList(width) {
		return n / 2
	}
	return n
}

// overlayFill accumulates one list's lines into exactly the n rows the frame
// reserved for it. Every list on this surface draws through it, so the two-line
// law, the pointer's row and the bottom edge are decided once.
type overlayFill struct {
	out   []string
	owner []int
	n     int
	width int
	pal   palette
	// hover is the pointer's row within the block, in SCREEN lines — which is
	// what the frame records (view.go's chromeOverlay) and not what the list
	// counts in. A row is hovered when the pointer is on EITHER of its lines.
	hover int
}

func newOverlayFill(width, n int, pal palette, hover int) *overlayFill {
	return &overlayFill{out: make([]string, 0, n), owner: make([]int, 0, n), n: n, width: width, pal: pal, hover: hover}
}

// room reports whether another line will fit.
func (f *overlayFill) room() bool { return len(f.out) < f.n }

// add draws one row, and reports whether it fit. A two-line row with one line of
// room left does NOT fit: half a row at the bottom of a list is a label whose
// facts are on the next screen, and a selection band with one end cut off.
//
// at is what the row belongs to — the index a pointer resolves back to — or -1
// for a line that answers to nothing.
func (f *overlayFill) add(at int, label, note string, selected, marked bool) bool {
	return f.addTinted(at, label, note, nil, selected, marked)
}

// addTinted is [overlayFill.add] with the note's own ink named ([noteInk]), for
// the lists whose tail is not a fact but an ANSWER — the capability rows, and
// the intake card's filled fields, where the value a person put in is the datum
// the row is about and steps to ink while the schema's prose beside it stays
// dim (docs/DESIGN-LANGUAGE.md's payload rule).
//
// It is the same function and not a second one, because the lead, the ground,
// the pointer's row and the two-line law at [tierPhone] are decided here for
// every list on this surface, and a list that drew its own would be a second
// grammar to keep in step.
func (f *overlayFill) addTinted(at int, label, note string, tint noteInk, selected, marked bool) bool {
	take, flat := overlayItemLines(f.width, note), false
	if len(f.out)+take > f.n {
		// EXCEPT ON A FRAME WITH ONE ROW TO GIVE. A list that answered a one-row
		// window with a blank would be an overlay that opened onto nothing,
		// which is worse than the truncation this whole surface is about: the
		// pair is a way of READING a row, and no row at all is not a better one.
		// So the first row of a window too short for a pair falls back to the
		// one line every wider frame draws.
		if len(f.out) > 0 || f.n < 1 {
			return false
		}
		take, flat = 1, true
	}
	hovered := f.hover >= len(f.out) && f.hover < len(f.out)+take
	lines := overlayLinesTinted(label, note, tint, selected, marked, hovered, f.width, f.pal)
	if flat {
		lines = []string{overlayRowTinted(label, note, tint, selected, markIf(marked), hovered, f.width, f.pal)}
	}
	for _, line := range lines {
		f.out = append(f.out, line)
		f.owner = append(f.owner, at)
	}
	return true
}

// plain adds a line that is not a row — a section rule, a "nothing matches" —
// already painted by its caller.
func (f *overlayFill) plain(line string) bool {
	if !f.room() {
		return false
	}
	f.out = append(f.out, line)
	f.owner = append(f.owner, -1)
	return true
}

// done closes the block: blanks under the last row where the items ran out
// before the frame's rows did.
//
// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED. The frame subtracts that
// height from the conversation before the list is drawn ([app.overlayHeight]),
// and a list that came back a line short would leave the frame a line short of
// the terminal. It can only happen at [tierPhone], where a row's height depends
// on the row; everywhere else the count and the rows agree exactly, so nothing
// is padded and the block is byte-for-byte the one this surface always drew.
func (f *overlayFill) done() ([]string, []int) {
	if phoneList(f.width) {
		for len(f.out) < f.n {
			f.out = append(f.out, "")
			f.owner = append(f.owner, -1)
		}
	}
	return f.out, f.owner
}

// choice is the model under the cursor, and false when the filter matched
// nothing — enter on an empty list must change nothing at all.
func (p *picker) choice() (Model, bool) {
	if !p.open || p.cursor < 0 || p.cursor >= len(p.list) {
		return Model{}, false
	}
	return p.all[p.hits[p.list[p.cursor].hit]], true
}

// height is how many LIST rows the picker wants, not counting the filter box —
// the box sits in the input line's place and costs the frame nothing. One row
// is reserved for the "no model matches" line, because a filter that matches
// nothing has to say so where the list was.
//
// THE CEILING IS IN LINES AND NOT IN MODELS, which is what keeps the overlay
// the same size on every frame: [pickerRows] rows of a phone are six models
// with their facts under them rather than twelve models with their facts cut
// off, and either way the list takes the same twelve rows from the screen.
func (p *picker) height(width int) int {
	switch {
	case !p.open:
		return 0
	case len(p.list) == 0:
		return 1
	}
	// The why line rides with the row it explains, so it is counted the same
	// way: one line, inside the ceiling, and never half of a pair.
	lines := 0
	for at := p.top; at < len(p.list) && lines < pickerRows; at++ {
		_, note := p.entryText(at, width, nil)
		take := overlayItemLines(width, note)
		if p.whyAt(at) != "" {
			take++
		}
		if lines+take > pickerRows {
			break
		}
		lines += take
	}
	return lines
}

// rows draws exactly n list rows. n comes from [app.overlayHeight], which is
// this picker's own height clamped to what the terminal can give, so a short
// window shows fewer rows rather than a frame that does not fit.
//
// level answers what a model has been dialled to; it is passed in rather than
// looked up here because the answer lives on the agent (see [app.reasoningFor])
// and the picker is a list, not a thing that holds a session.
func (p *picker) rows(width, n int, pal palette, hover int, level func(string) string) []string {
	lines, _ := p.rowsOwned(width, n, pal, hover, level)
	return lines
}

// rowsOwned is [picker.rows] with the hit each LINE belongs to, or -1. The
// settings panel puts this list inside its own frame and resolves clicks
// against it (settings.go's [sheet.selectLines]), and "the hit is the line's
// index from the top" stopped being true the moment a row could be two lines.
func (p *picker) rowsOwned(width, n int, pal palette, hover int, level func(string) string) ([]string, []int) {
	if n <= 0 {
		return nil, nil
	}
	if len(p.list) == 0 {
		return []string{pal.dim("  no model matches")}, []int{-1}
	}
	p.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := p.top; at < len(p.list) && fill.room(); at++ {
		label, note := p.entryText(at, width, level)
		if !fill.add(at, label, note, at == p.cursor, p.marked(at)) {
			break
		}
		// THE WHY LINE IS UNDER THE CURSOR AND NOWHERE ELSE. One sentence about
		// the row a person has stopped on is an explanation; the same sentence
		// under every row is a wall, and the numbers above it stop being read.
		if why := p.whyAt(at); why != "" {
			if !fill.plain(pal.dim(fit(strings.Repeat(" ", overlayIndent+2)+why, width))) {
				break
			}
		}
	}
	return fill.done()
}

// pinnedLane is the MACHINE this conversation is held to, and empty for every
// row that names none — which is both `auto` and `openrouter`.
//
// It exists because [picker.pin] is the settings row VERBATIM, which is what
// [picker.marked] needs to tell the three rungs of the fold apart, and is
// exactly the wrong thing to hand to anything that draws a lane's name: the row
// reading `auto` drew a model whose speed came `via auto`, a machine no router
// has ever heard of.
func (p *picker) pinnedLane() string {
	switch strings.ToLower(strings.TrimSpace(p.pin)) {
	case "", config.LaneAuto, config.LaneOpenRouter:
		return ""
	}
	return p.pin
}

// marked is the row an overlay's chosen band belongs to: the model in use, and
// — inside an open fold — the lane this conversation is actually held to, which
// is the pin when there is one and the `auto` row when there is not.
func (p *picker) marked(at int) bool {
	row := p.list[at]
	model := p.all[p.hits[row.hit]]
	switch {
	case row.lane == laneNone:
		return model.ID == p.current
	case model.ID != p.current:
		return false
	case row.lane == laneAutoAt:
		return p.pin == "" || strings.EqualFold(p.pin, config.LaneAuto)
	case row.lane == laneRoutAt:
		return strings.EqualFold(p.pin, config.LaneOpenRouter)
	}
	return strings.EqualFold(p.pin, p.lanes[row.lane].Name)
}

// entryText is one drawn row as the row's two halves. level may be nil, which
// is what the height ask passes: the effort rides on the label and the height
// only needs the tail.
func (p *picker) entryText(at int, width int, level func(string) string) (string, string) {
	row := p.list[at]
	model := p.all[p.hits[row.hit]]
	switch row.lane {
	case laneNone:
		dial := ""
		if level != nil {
			dial = level(model.ID)
		}
		return p.rowText(model, dial, width)
	case laneAutoAt:
		// THE NAME ON THIS ROW IS THE CHOOSER'S AND NOT THE SORT'S. "auto picks
		// the fastest lane each answer — coreweave now" is a claim about where
		// the NEXT REQUEST would go, and only the chooser answers that; the
		// order the rows are drawn in is this file's own reading of the same
		// beliefs and is allowed to differ.
		//
		// AND ON A NARROW FRAME THE SENTENCE BECOMES THE NAME. What the row is
		// FOR is a thing you read once; which machine it would send you to now
		// is the thing you came back to look at, so the short spelling keeps the
		// name and drops the explanation around it.
		sentence := laneAutoNote
		short := ""
		if p.auto != "" {
			sentence += " — " + strings.ToLower(p.auto) + " now"
			short = strings.ToLower(p.auto) + " now"
		}
		fields := []rowField{rowSay(sentence, short), rowSay("recommended")}
		// AND WHAT AUTO WILL NOT DO, said where the choice is made. With the
		// speed guard off, a lane that turns slow mid-answer is one you wait
		// out; that is a fact about this row and it belongs on it.
		if !p.guard {
			fields = append(fields, rowSay("no rescue"))
		}
		return rowHalves(rowPlan{primary: "  " + p.mark(true) + " auto", fields: fields}, width, 0)
	case laneRoutAt:
		return rowHalves(rowPlan{
			primary: "  " + p.mark(false) + " openrouter",
			fields:  []rowField{rowSay("let the router balance on price", "balances on price")},
		}, width, 0)
	}
	// A LANE SITS UNDER THE TWO WORDS THAT ARE NOT LANES. `auto` and
	// `openrouter` are the two ways of declining to name a machine, so they
	// stand at the block's own margin and the machines themselves are indented
	// past them — which is what makes the block read as a question with two
	// answers and a list, rather than as five things of the same kind.
	label, note := rowHalves(laneRowPlan(p.lanes[row.lane]), width, overlayIndent)
	return strings.Repeat(" ", overlayIndent) + label, note
}

// mark is the two lead glyphs of the fold's own rows — filled for the answer
// that chooses for you, hollow for the one that declines to. A terminal without
// box drawing gets the same distinction in the letters it does have, because a
// difference drawn in shape has to survive having no shape (styles.go).
func (p *picker) mark(filled bool) string {
	switch {
	case p.ascii && filled:
		return "*"
	case p.ascii:
		return "o"
	case filled:
		return "●"
	}
	return "○"
}

// laneAutoNote is what the auto row says it does. It is a sentence and not a
// word because it is the row a person will land on first and the one they will
// leave alone: what it is FOR has to be on it.
const laneAutoNote = "picks the fastest lane each answer"

// whyAt is the dim sentence under the cursor's lane row, empty everywhere else.
func (p *picker) whyAt(at int) string {
	if at != p.cursor || at < 0 || at >= len(p.list) {
		return ""
	}
	row := p.list[at]
	if row.lane < 0 {
		return ""
	}
	return laneWhy(p.lanes[row.lane])
}

// rowText is one model as the row's two halves: the id with whatever level it
// has been dialled to, and the dim tail of facts (models.go's [modelNote] —
// window, price, arena score). The model in use is the marked row — that is the
// mark, and it survives scrolling past it.
//
// The level goes with the ID and not into the tail, because it is the one thing
// on the row that is not a fact about the model: it is what THIS person asked
// for, it reads the same here as it does in the status line ("<model>:<level>"),
// and the tail stays what the catalog said.
func (p *picker) rowText(model Model, level string, width int) (string, string) {
	// THE PIN IS THIS CONVERSATION'S AND SO IT RIDES ON THIS CONVERSATION'S ROW.
	// A lane pinned while you were on one model is not a claim about the next
	// one, so every other row's `via` is what auto would do (lanes.go).
	pin := ""
	if model.ID == p.current {
		pin = p.pinnedLane()
	}
	plan := rowPlan{
		primary: model.ID,
		// THE AUTHOR IS DROPPABLE WHERE THE SLUG STILL NAMES ONE ROW. Which is
		// a question about the LIST and not about the model, so the list
		// answers it once when it opens ([picker.shared]).
		author: !p.shared[rowSlug(model.ID)],
		fields: modelFields(model, pin),
	}
	// THE LEVEL RIDES THE NAME AND IS NEVER CUT. It is the one thing on the row
	// that is not a fact about the model — it is what THIS person asked for, and
	// it reads the same here as it does in the status line ("<model>:<level>") —
	// so the fitter reserves it and fits the id into what is left (rowfit.go).
	if level != "" {
		plan.suffix = ":" + level
	}
	return rowHalves(plan, width, 0)
}

// ── reasoning strength, from the row it belongs to ──────────────────────────
//
// ctrl+t walks the model under the cursor through off → low → medium → high →
// off. The level is stored ON THE AGENT, per model id (internal/session's
// agent.go), which is what makes it survive the picker closing, a switch away
// and a switch back — and what makes /new forget it, since /new is a new agent.
//
// IT IS ctrl+t AND NOT t. The filter box takes every printable key, and a bare
// t would mean nobody could type "sonnet", "mistral" or "gpt" into a list whose
// whole purpose is being typed into. A modifier is the price of a type-to-filter
// overlay, and it is the cheaper half of that trade by a wide margin.
//
// The key does nothing on a model whose catalog row does not accept a reasoning
// knob ([Model.Reasoning]), and nothing is exactly what it should do: the level
// would be a 400 at the next turn, and refusing to offer it is how the surface
// declines to sell something the endpoint will not honour.

// reasoningCycle is the walk, in order. The empty level leads it because off is
// where every model starts and where the cycle comes back to.
var reasoningCycle = []string{"", "low", "medium", "high"}

// nextReasoning is the level after this one, wrapping.
func nextReasoning(level string) string {
	for at, step := range reasoningCycle {
		if step == level {
			return reasoningCycle[(at+1)%len(reasoningCycle)]
		}
	}
	// A level from somewhere this surface does not know about resolves to off,
	// which is the one answer that cannot surprise anybody.
	return ""
}

// The level a model is held at is read through [app.reasoningFor], which lives
// in reasoninglevel.go: the draw path asks it three times a frame and it must
// never touch the agent, because over a connection the agent is another machine.

// cycleReasoning is ctrl+t: the selected row's model moves one step round the
// cycle, or nothing happens because that model takes no reasoning knob.
//
// IT ASKS THE AGENT WHERE THE ROW STANDS WHEN NOBODY HAS ASKED YET, which is
// the one place the surface's held answer is not good enough: the cycle is
// RELATIVE, so starting it from "not told yet" would walk a model already
// dialled to high back down to low. This is a keystroke — the fourth of
// reasoninglevel.go's seeded moments, and waiting is what a keystroke may do.
//
// AND THE NEW LEVEL IS WRITTEN THROUGH, so the row under the cursor changes on
// the very next frame. The surface is the only thing that sets these, so what it
// just set is what is true.
func (a *app) cycleReasoning() {
	chosen, ok := a.pick.choice()
	if !ok || !chosen.Reasoning || a.agent == nil {
		return
	}
	if _, known := a.levels[session.ReasoningKey(chosen.ID)]; !known {
		a.learnLevel(chosen.ID)
	}
	next := nextReasoning(a.reasoningFor(chosen.ID))
	a.agent.SetReasoningFor(chosen.ID, next)
	a.keepLevel(chosen.ID, next)
}

// pickerHint is the placeholder in the empty filter box. It is the only place
// this overlay explains itself, and it costs no row of its own.
//
// THE LINE IS BUDGETED. It is drawn WHOLE inside the box on a sixty-cell frame
// — a hint cut off at "e…" is a hint that has to be guessed at — which leaves
// about fifty-five cells for it. The fold earned its seven, and what paid for
// them are the two verbs a modal list does not have to explain: enter commits
// and esc leaves, everywhere on this surface and in every other program.
//
// UNDER SIXTY IT IS THE SAME RANKED TAIL EVERY ROW IS (rowfit.go): the keys go
// from the right, whole, and the box never draws a key spelled `es…`. Which is
// why the written order is also the order they are given up in — `filter` is
// what the box IS and survives every width, and the two universal verbs at the
// end are the two a person already knows without being told.
const pickerHint = "filter · ↑↓ · → lanes · ctrl+t effort · enter · esc"

// pickerHintFields is that same line as the fields it is made of, ranked. The
// test that joins them and compares against [pickerHint] is what keeps the two
// spellings one (the one-source-of-truth law: a constant read by a person and a
// list read by the fitter would otherwise drift).
var pickerHintFields = []rowField{
	rowSay("filter"), rowSay("↑↓"), rowSay("→ lanes"),
	rowSay("ctrl+t effort"), rowSay("enter"), rowSay("esc"),
}

// pickerHintAt is the hint in the cells the box actually has.
func pickerHintAt(room int) string { return rowTail(pickerHintFields, room) }

// ── the app's side of the overlay ───────────────────────────────────────────

// openPicker is /model with no argument. It names the chat law out loud rather
// than leaning on the list having been filtered already: /model is a slot like
// any other, and every slot says which models may answer it (settings.go's
// [filterFor]).
func (a *app) openPicker() {
	a.pick.startFor(a.modelList(), a.model, chatModel)
	// THE PIN IS A SNAPSHOT, exactly as the model in use is: it is what marks a
	// row inside an open fold, and what the row in use says `via`, and neither
	// of those can change while a modal overlay owns the keyboard.
	a.armLanes(&a.pick, laneSlotFor(a.model))
	a.touch()
}

// openPickerFiltered is /model with words after it that are a QUESTION rather
// than a name — `/model deepseek <1s`. It is the same list, opened with the
// query already typed, so the next keystroke narrows it further instead of
// starting again.
func (a *app) openPickerFiltered(query string) {
	a.openPicker()
	a.pick.filter.setText(query)
	a.pick.rank()
	a.touch()
}

// openTaskPicker is the same list, pointed at ONE RUNNING NODE: the model word
// in a room's status line pressed, which is the only door onto it (app.go's
// [app.statusPress]).
//
// IT OFFERS THE SAME ROWS AS THE CONVERSATION'S, filtered by the same chat law,
// and that is what keeps the engine's ambiguity out of this gesture entirely: a
// row is one concrete catalog id, so the word handed over resolves to exactly one
// model and the shortlist a typed word can raise (internal/session's
// taskmodel.go) has nothing to raise here.
//
// THE MARK OPENS ON THE NODE'S OWN MODEL, not the session's, for [picker.start]'s
// stated reason: the cursor sits on what you are on, so enter confirms rather
// than changes. In here what you are on is what the task is running.
func (a *app) openTaskPicker(id uint64) {
	current := ""
	if node := a.tasks[id]; node != nil {
		current = node.model
	}
	a.pick.startFor(a.modelList(), current, chatModel)
	a.pick.task = id
	a.touch()
}

// modelList is the source order stated in models.go, applied once here: the
// door's list (the catalog, when it can answer without a fetch), then the disk
// cache, then the built-ins. Each rung is tried only if the one above it came
// back empty, and none of them can block.
//
// EVERY RUNG IS FILTERED THE SAME WAY ([chatModels]): a row on offer here is a
// model you can talk to. The filter sits at the join rather than on any one
// source because all three of them have carried a drawing model at some point —
// the door's catalog publishes them, the cache is a file the door wrote before
// this rule existed — and a rule enforced at two of three places is a rule with
// a way round it.
func (a *app) modelList() []Model { return a.modelsFor(chatModel) }

// modelsFor is that same source order, asked ONE SLOT'S question instead of the
// chat law's ([modelFilter], models.go).
//
// The filter is applied INSIDE the ladder rather than to whatever it returned,
// and that is the whole reason this exists as a function. A slot filtering
// [app.modelList]'s answer is filtering a list from which its own rows have
// already been removed — which is what the media slots were doing, and why the
// drawing row offered a picker that could not contain a drawing model. It also
// keeps the rung rule honest for every slot: a catalog that carries no speech
// model at all falls through to the cache, exactly as a catalog with no chat
// model falls through for /model.
func (a *app) modelsFor(keep modelFilter) []Model {
	if a.models != nil {
		if list := keepModels(a.models(), keep); len(list) > 0 {
			return list
		}
	}
	if list := keepModels(CachedModels(), keep); len(list) > 0 {
		return list
	}
	return keepModels(BuiltinModels(), keep)
}

// nonChatWarning is what `/model <slug>` says instead of switching, and it is
// empty for every slug that may be taken.
//
// THE OFFLINE LAW DECIDES WHO IS CHECKED. A slug no list this surface can reach
// carries is taken AS TYPED, exactly as it always was: the catalog may be cold,
// a person may be naming a model this build has never listed, and a surface that
// refused every unfamiliar name would be a surface that stops working the moment
// the network does. The check is only for a slug the catalog DOES carry, where
// "this one cannot hold a conversation" is a published fact and not a guess.
//
// One sentence, and it says what happens rather than what went wrong: the model
// is unchanged, which is the thing the person needs to know before they type
// their next message.
func (a *app) nonChatWarning(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, model := range a.modelsFor(nil) {
		if !strings.EqualFold(model.ID, id) {
			continue
		}
		if chatModel(model) {
			return ""
		}
		line := model.ID + " cannot hold a conversation"
		if words := ModalityWord(model.Input, model.Output); words != "" {
			line += " — it " + words
		}
		return line + ". Still on " + a.model + "."
	}
	return ""
}

// windowFor is the context length this surface knows for a model id, or zero.
// It is how /model <slug> — which carries no row with it — still tells the
// session what window it just switched to.
func (a *app) windowFor(id string) int {
	id = strings.TrimSpace(id)
	for _, model := range a.modelList() {
		if strings.EqualFold(model.ID, id) {
			return model.ContextLength
		}
	}
	return 0
}

// switchModel is the ONE road a model change takes, from the picker, from
// /model <slug> and from the settings sheet's talk row alike (settings.go's
// registry wires that row straight to here): swap it, learn its window, write
// it down, say so.
//
// window is the figure the caller already has (the picker's row); zero asks the
// list. Telling the session about the window is not decoration — compaction
// fires at a fraction of it, so a session that switched to a 1M model without
// saying so would keep compacting as if it were on the 128k one it started on.
func (a *app) switchModel(id string, window int) {
	a.agent.SetModel(id)
	a.model = a.agent.Model()
	if a.model == "" {
		a.model = id
	}
	// The new model's dial is learned HERE rather than left to the frame clock,
	// so the status row names it on the very frame the switch lands on
	// (reasoninglevel.go states the three moments that are seeded and why).
	a.learnLevel(a.model)
	if window <= 0 {
		window = a.windowFor(a.model)
	}
	if window > 0 {
		a.agent.SetContextWindow(window)
		// The surface keeps the figure it just handed over: session has no
		// getter for it, and the status line's meter is a percentage of exactly
		// this number (see [app.ctxPercent]).
		a.ctxWindow = window
	}
	a.rememberModel(a.model)
	// THE ID IS THE WHOLE OF THIS LINE (payload.go). `model ·` is a label a person
	// already knows they asked for; the id is the one thing here they cannot see
	// anywhere else at this moment, so it steps to ink and the label stays dim.
	a.noteFacts("model · "+a.model, a.model)
	a.noticeEvent(eventModelSwitched)
}

// rememberModel writes the choice down, so the NEXT launch opens on the model
// this one ended on (the door's seam is [Options.SaveModel], and the v3 door
// reads it back in v3TalkModel).
//
// It is here rather than at the picker because this is the one road every model
// change takes; a second write site would be the drift where /model persisted
// and the sheet's row did not.
//
// NIL IS A SURFACE THAT CANNOT REMEMBER, exactly as it is for the consent
// card's "always" — and that is the whole of the --host rule, kept in wiring
// rather than in a condition here: the far machine's engine reads the far
// machine's profile, so the hosted door hands over no seam and nothing about a
// remote model choice lands on this laptop.
//
// The write's error is dropped, and that is honest rather than lazy: nothing on
// screen claims the choice was saved. The note says "model · <id>", which is
// true of the running session whatever the disk did.
func (a *app) rememberModel(id string) {
	if a.saveModel == nil || strings.TrimSpace(id) == "" {
		return
	}
	_ = a.saveModel(id)
}

// pickerKey routes one keypress while the overlay owns the keyboard. The input
// box is suspended for the duration — its draft is untouched and comes back
// whole on esc or enter.
//
// esc means the overlay here and not the turn: a modal that cannot be dismissed
// by the dismiss key is a trap. A turn is still interruptible the moment the
// picker closes.
func (a *app) pickerKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		a.pick.close()

	case "enter":
		chosen, ok := a.pick.choice()
		// THE SUBJECT IS READ BEFORE THE LIST IS CLOSED, because closing it is what
		// forgets the subject ([picker.close] zeroes the whole struct).
		task := a.pick.task
		// AND SO IS THE LANE ROW, for the same reason.
		row, onLane := a.pick.laneUnder()
		var lanes []laneView
		if onLane {
			lanes = a.pick.lanes
		}
		a.pick.close()
		if ok && onLane && task == 0 {
			// ENTER ON A LANE IS TWO ANSWERS AT ONCE WHEN THE MODEL IS NOT THE
			// ONE IN USE: somebody who opened another model's lanes and chose
			// one of them asked for that model on that machine, and pinning a
			// lane under a model they are not talking to would be a setting
			// that took effect the next time they happened to switch.
			if chosen.ID != a.model {
				a.switchModel(chosen.ID, chosen.ContextLength)
			}
			a.applyLaneChoice(chosen.ID, row, lanes)
			a.touch()
			return
		}
		if ok {
			// One list, two subjects, decided where the list was opened: a node when
			// the model word in its room was pressed, and the conversation every
			// other time (palette.go's [app.openTaskPicker]).
			if task != 0 {
				a.retargetTask(task, chosen.ID)
			} else {
				a.switchModel(chosen.ID, chosen.ContextLength)
			}
		}

	// The reasoning cycle sits above the filter's default branch on purpose: it
	// is the one key here that is not about the list, and it changes the row
	// rather than the query.
	case "ctrl+t":
		a.cycleReasoning()

	// THE FOLD IS ITS OWN KEY MAP and it is the list's, not this door's
	// ([picker.foldKey]) — the settings panel's model row reads the very same
	// three keys, and a fold that opened from one door and not the other would
	// be two pickers again.
	default:
		if !a.pick.foldKey(msg.String()) {
			a.pick.navigate(msg)
		}
	}
	a.touch()
}

// navigate is EVERY KEY THE PICKER OWNS that is not a decision: the walk, the
// scroll and the filter box. enter, esc and ctrl+t are left to whoever opened
// the list, because what they mean is the caller's business — /model switches a
// session with them, the settings panel writes a registry row (settings.go).
//
// It exists so that the two entry points cannot drift into two pickers. There
// is one filterable model list on this surface; a slot row in the settings
// panel and /model are two doors onto it, not two lists that look alike.
func (p *picker) navigate(msg tea.KeyPressMsg) {
	listNavigate(msg, &p.filter, p.move, p.rank, pickerRows)
}

// listNavigate is that key map itself, held apart from the model list so the
// session picker can have exactly it (resume.go) rather than a second copy of
// it that answers ctrl+w and forgets pgdn. What a filterable overlay LISTS is
// its own; how a person walks and types into one is this surface's, once.
//
// move walks the rows and rank re-filters after an edit — a caller passes its
// own two, because the scoring is about what is being listed. page is how far
// pgup and pgdn jump, which is that list's own window.
func listNavigate(msg tea.KeyPressMsg, filter *editor, move func(int), rank func(), page int) {
	// THE WORD AND LINE JUMPS ARE THE SURFACE'S, NOT THIS LIST'S (editkeys.go).
	// They are read before the switch because they belong to every box on the
	// program and this one is only the busiest door onto them — twelve overlays
	// share this key map, and a jump added here has to be the same jump the
	// message box makes or a person learns two of them.
	if editorMotion(filter, msg.String()) {
		return
	}
	switch msg.String() {
	case "up", "ctrl+p":
		move(-1)
	case "down", "ctrl+n":
		move(1)
	case "pgup":
		move(-page)
	case "pgdown":
		move(page)

	case "backspace":
		filter.deleteBackward()
		rank()
	case "delete":
		filter.deleteForward()
		rank()
	// THE LINE AND WORD KILLS ANSWER TO EVERY NAME THEY SEND UNDER, exactly as
	// they do in the message box (input.go). A gesture that clears the filter in
	// the composer and does nothing in the model picker is a gesture a person
	// stops trusting anywhere — and `super+backspace` is what a hand on a Mac
	// keyboard does without being told, so it means kill-to-line-start here for
	// the same reason it means it there. It reaches this switch only on a terminal
	// that reports the super modifier at all, which costs nothing where none does.
	case "ctrl+u", "super+backspace":
		filter.killToStart()
		rank()
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		filter.deleteWord()
		rank()
	case "left", "ctrl+b":
		filter.left()
	case "right", "ctrl+f":
		filter.right()
	case "home":
		// `ctrl+a` is the same jump and is read above, with the rest of the
		// surface's line vocabulary (editkeys.go).
		filter.home()
	case "end", "ctrl+e":
		filter.end()

	default:
		if text := msg.Key().Text; text != "" {
			filter.insert(text)
			rank()
		}
	}
}

// overlayHeight is how many rows the frame gives whichever list is open. It is
// that list's own want, clamped so the status line and the box always survive:
// an overlay that could take the whole frame is an overlay that can hide where
// you are and what you typed.
//
// Only one list is ever open — [app.closeLists] and the sync in [app.edited]
// see to that — so this is a switch and not a sum.
func (a *app) overlayHeight() int {
	width, height := a.size()
	var want int
	switch {
	case a.pick.open:
		want = a.pick.height(width)
	case a.crewPick.open:
		want = a.crewPick.height()
	case a.effPick.open:
		want = a.effPick.height()
	case a.roster.open:
		want = a.roster.height(width)
	case a.shelf.open:
		want = a.shelf.height(width)
	case a.connPanel.open:
		want = a.connPanel.height(width)
	case a.harnPanel.open:
		want = a.harnPanel.height(width)
	case a.harnPick.open:
		want = a.harnPick.height(width)
	case a.permPanel.open:
		want = a.permPanel.height(width)
	case a.subPage.open:
		want = a.subPage.height(width)
	case a.menu.open:
		want = a.menu.height(width)
	case a.comp.open:
		want = a.comp.height(width)
	default:
		return 0
	}
	// The approval question, the follow-up count and a waiting message are spoken
	// for before the list is: all of them sit between the conversation and the
	// box, and a list that claimed their rows would push the status line off the
	// frame. The two reserved rows are the status line and one row of
	// conversation — a list that left neither would be a list that took the
	// screen.
	if room := height - 2 - a.inputHeight() - a.consentHeight() - a.connectAskHeight() -
		a.harnessAskHeight() - a.followHeight() - a.parkedHeight(); want > room {
		want = room
	}
	if want < 0 {
		return 0
	}
	return want
}

// overlayRows is the tail of the frame: the open list, drawn in exactly the
// rows [app.overlayHeight] handed out.
func (a *app) overlayRows(width, n int) []string {
	// The pointer's row within whichever list is open, or -1. One number for all
	// three, because there is only ever one list (hover.go).
	hover := -1
	if a.hot.kind == hoverOverlay {
		hover = a.hot.index
	}
	switch {
	case a.pick.open:
		return a.pick.rows(width, n, a.pal, hover, a.reasoningFor)
	case a.crewPick.open:
		return a.crewPick.rows(width, n, a.pal, hover, a)
	case a.effPick.open:
		return a.effPick.rows(width, n, a.pal, hover)
	case a.roster.open:
		return a.roster.rows(width, n, a.pal, hover)
	case a.shelf.open:
		return a.shelf.rows(width, n, a.pal, hover)
	case a.connPanel.open:
		return a.connPanel.draw(width, n, a.pal, hover)
	case a.harnPanel.open:
		return a.harnPanel.draw(width, n, a.pal, hover)
	case a.harnPick.open:
		return a.harnPick.draw(width, n, a.pal, hover)
	case a.permPanel.open:
		return a.permPanel.draw(width, n, a.pal, hover)
	case a.subPage.open:
		return a.subPage.draw(a, width, n, hover)
	case a.menu.open:
		return a.menu.rows(width, n, a.pal, hover)
	case a.comp.open:
		return a.comp.rows(width, n, a.pal, hover)
	}
	return nil
}
