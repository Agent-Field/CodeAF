package tui3

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

	// hits are indexes into all, in rank order — the rows actually on offer.
	hits []int
	// cursor indexes hits, and top is the first hit drawn.
	cursor int
	top    int

	// current is the model in use when the picker opened. It is what the accent
	// marks, and it is deliberately a snapshot: the mark answers "what am I on",
	// which cannot change while a modal overlay owns the keyboard.
	current string

	filter editor
}

// start opens the picker over models with current marked.
func (p *picker) start(models []Model, current string) {
	*p = picker{open: true, all: models, current: current}
	p.lower = make([]string, len(models))
	for i, model := range models {
		p.lower[i] = strings.ToLower(model.ID)
	}
	p.score = make([]int, len(models))
	p.rank()
	// The cursor opens ON the model in use. A picker that opened on row zero
	// would make enter — the key a person presses to confirm — a model change
	// they did not ask for.
	for at, i := range p.hits {
		if p.all[i].ID == current {
			p.cursor = at
			break
		}
	}
	p.follow(pickerRows)
}

// close puts the picker away and forgets the filter. The next /model opens on
// the whole list, which is the only thing a person can predict; a picker that
// remembered last week's query would open onto a list with no explanation.
func (p *picker) close() { *p = picker{} }

// rank re-filters against the filter box: SUBSTRING, case-insensitive, and a
// model whose id begins with the query outranks one that merely contains it.
//
// The score is the offset the match was found at — zero for a prefix — so the
// prefix rule is not a special case bolted on top: "gpt" puts openai/gpt-4.1
// (offset 7, the shortest reach into the id) above a model that carries the
// letters further along, and an id that literally starts with the query beats
// both. Ties keep source order, which is the catalog's order and the built-in
// list's, so an empty box shows the list as it was handed over.
//
// Substring and not the fzf subsequence the v2 palette uses: model ids are
// slash-separated names a person types the middle of, and a subsequence match
// over six hundred of them answers "sonnet" with every id containing an s, an
// o, an n… somewhere.
func (p *picker) rank() {
	needle := strings.ToLower(strings.TrimSpace(p.filter.String()))
	p.hits = p.hits[:0]
	for i, id := range p.lower {
		if needle == "" {
			p.hits = append(p.hits, i)
			continue
		}
		at := strings.Index(id, needle)
		if at < 0 {
			continue
		}
		p.score[i] = at
		p.hits = append(p.hits, i)
	}
	if needle != "" {
		sort.SliceStable(p.hits, func(a, b int) bool { return p.score[p.hits[a]] < p.score[p.hits[b]] })
	}
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose.
	p.cursor, p.top = 0, 0
}

// move walks the list, clamping at both ends rather than wrapping: a list that
// wraps makes "hold ↓ until it stops" an infinite gesture.
func (p *picker) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.hits))
	p.follow(pickerRows)
}

// follow scrolls the window by the least that keeps the cursor inside it.
func (p *picker) follow(height int) { p.top = listTop(p.cursor, p.top, len(p.hits), height) }

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
// that all shout is a list nobody can read down. The note trails on the right,
// dim, and the label gives way before it does: a truncated name is still
// recognizable, and "164k" cut in half is a wrong number.
func overlayRow(label, note string, selected, marked bool, width int, pal palette) string {
	lead := "  "
	if selected {
		lead = pal.accent("› ")
	}
	room := width - 2
	if note != "" {
		room -= ansi.StringWidth(note) + 1
	}
	label = fit(label, room)

	var painted string
	switch {
	case marked:
		painted = pal.accent(label)
	case selected:
		painted = pal.ink(label)
	default:
		painted = pal.dim(label)
	}
	if selected {
		painted = pal.bold(painted)
	}
	if note == "" {
		return lead + painted
	}
	gap := width - 2 - ansi.StringWidth(label) - ansi.StringWidth(note)
	if gap < 1 {
		gap = 1
	}
	return lead + painted + strings.Repeat(" ", gap) + pal.dim(note)
}

// choice is the model under the cursor, and false when the filter matched
// nothing — enter on an empty list must change nothing at all.
func (p *picker) choice() (Model, bool) {
	if !p.open || p.cursor < 0 || p.cursor >= len(p.hits) {
		return Model{}, false
	}
	return p.all[p.hits[p.cursor]], true
}

// height is how many LIST rows the picker wants, not counting the filter box —
// the box sits in the input line's place and costs the frame nothing. One row
// is reserved for the "no model matches" line, because a filter that matches
// nothing has to say so where the list was.
func (p *picker) height() int {
	switch {
	case !p.open:
		return 0
	case len(p.hits) == 0:
		return 1
	case len(p.hits) < pickerRows:
		return len(p.hits)
	default:
		return pickerRows
	}
}

// rows draws exactly n list rows. n comes from [app.overlayHeight], which is
// this picker's own height clamped to what the terminal can give, so a short
// window shows fewer rows rather than a frame that does not fit.
func (p *picker) rows(width, n int, pal palette) []string {
	if n <= 0 {
		return nil
	}
	if len(p.hits) == 0 {
		return []string{pal.dim("  no model matches")}
	}
	p.follow(n)
	out := make([]string, 0, n)
	for at := p.top; at < len(p.hits) && len(out) < n; at++ {
		out = append(out, p.row(p.all[p.hits[at]], at == p.cursor, width, pal))
	}
	return out
}

// row is one model: the cursor mark, the id, and the window on the right. The
// model in use is the marked row — that is the mark, and it survives scrolling
// past it.
func (p *picker) row(model Model, selected bool, width int, pal palette) string {
	return overlayRow(model.ID, contextWord(model.ContextLength),
		selected, model.ID == p.current, width, pal)
}

// pickerHint is the placeholder in the empty filter box. It is the only place
// this overlay explains itself, and it costs no row of its own.
const pickerHint = "filter models · ↑↓ move · enter switch · esc cancel"

// ── the app's side of the overlay ───────────────────────────────────────────

// openPicker is /model with no argument.
func (a *app) openPicker() {
	a.pick.start(a.modelList(), a.model)
	a.touch()
}

// modelList is the source order stated in models.go, applied once here: the
// door's list (the catalog, when it can answer without a fetch), then the disk
// cache, then the built-ins. Each rung is tried only if the one above it came
// back empty, and none of them can block.
func (a *app) modelList() []Model {
	if a.models != nil {
		if list := a.models(); len(list) > 0 {
			return list
		}
	}
	if list := CachedModels(); len(list) > 0 {
		return list
	}
	return BuiltinModels()
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

// switchModel is the ONE road a model change takes, from the picker and from
// /model <slug> alike: swap it, learn its window, say so.
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
	a.note("model · " + a.model)
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
		a.pick.close()
		if ok {
			a.switchModel(chosen.ID, chosen.ContextLength)
		}

	case "up", "ctrl+p":
		a.pick.move(-1)
	case "down", "ctrl+n":
		a.pick.move(1)
	case "pgup":
		a.pick.move(-pickerRows)
	case "pgdown":
		a.pick.move(pickerRows)

	case "backspace":
		a.pick.filter.deleteBackward()
		a.pick.rank()
	case "delete":
		a.pick.filter.deleteForward()
		a.pick.rank()
	case "ctrl+u":
		a.pick.filter.killToStart()
		a.pick.rank()
	case "ctrl+w":
		a.pick.filter.deleteWord()
		a.pick.rank()
	case "left", "ctrl+b":
		a.pick.filter.left()
	case "right", "ctrl+f":
		a.pick.filter.right()
	case "home", "ctrl+a":
		a.pick.filter.home()
	case "end", "ctrl+e":
		a.pick.filter.end()

	default:
		if text := msg.Key().Text; text != "" {
			a.pick.filter.insert(text)
			a.pick.rank()
		}
	}
	a.touch()
}

// overlayHeight is how many rows the frame gives whichever list is open. It is
// that list's own want, clamped so the status line and the box always survive:
// an overlay that could take the whole frame is an overlay that can hide where
// you are and what you typed.
//
// Only one list is ever open — [app.closeLists] and the sync in [app.edited]
// see to that — so this is a switch and not a sum.
func (a *app) overlayHeight() int {
	var want int
	switch {
	case a.pick.open:
		want = a.pick.height()
	case a.menu.open:
		want = a.menu.height()
	case a.comp.open:
		want = a.comp.height()
	default:
		return 0
	}
	_, height := a.size()
	// The approval question and the follow-up count are spoken for before the
	// list is: both sit between the conversation and the box, and a list that
	// claimed their rows would push the status line off the top of the frame.
	if room := height - 2 - (a.inputHeight() - 1) - a.consentHeight() - a.followHeight(); want > room {
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
	switch {
	case a.pick.open:
		return a.pick.rows(width, n, a.pal)
	case a.menu.open:
		return a.menu.rows(width, n, a.pal)
	case a.comp.open:
		return a.comp.rows(width, n, a.pal)
	}
	return nil
}
