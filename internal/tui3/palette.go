package tui3

import (
	"sort"
	"strings"
	"unicode/utf8"

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

// startFor opens the picker over the rows ONE SLOT can take: the list, narrowed
// by that slot's own question (models.go's [modelFilter]), with current marked.
//
// It is the single door every slot comes through — /model and the conversation
// rows pass [chatModel], the "looking" row passes [seesImages] — so the answer
// to "which models does this slot offer" is one predicate named at the call
// site rather than a list assembled there.
func (p *picker) startFor(models []Model, current string, keep modelFilter) {
	p.start(keepModels(models, keep), current)
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
	tokens := strings.Fields(strings.ToLower(p.filter.String()))
	p.hits = p.hits[:0]
	for i, id := range p.lower {
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
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose.
	p.cursor, p.top = 0, 0
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
// that all shout is a list nobody can read down. The note trails on the right
// and the label gives way before it does: a truncated name is still
// recognizable, and "164k" cut in half is a wrong number.
//
// THE SELECTED ROW IS A BAND, AND THE WHOLE LINE IS IN IT. Selection used to be
// a bold label and nothing else, which left the cursor row reading as half a
// row: the lead was accent, the name was bright, and the tail that carries the
// window, the price and the arena score stayed dim grey — the three facts a
// person is actually comparing, greyed out on the one row they were comparing
// them ON. So the emphasis now spans the line, lead to note, padded to the full
// width, and the note joins it in ink rather than staying behind in dim.
//
// Hover is the fourth thing a row can be and it is not a tier: it is the
// background under whichever of the three the row already was, plus a brighter
// lead — the pointer saying "this one", not the list saying "this matters".
// The two backgrounds are deliberately different weights ([palette.band] versus
// [palette.hover]): a pointer crossing a list must never look like the cursor
// moving, so hover stays one step off the terminal's own black and selection is
// the stronger band above it.
func overlayRow(label, note string, selected, marked, hovered bool, width int, pal palette) string {
	lead := "  "
	switch {
	case selected:
		lead = pal.accent("› ")
	case hovered:
		lead = pal.accent("· ")
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
	line := lead + painted
	if note != "" {
		gap := width - 2 - ansi.StringWidth(label) - ansi.StringWidth(note)
		if gap < 1 {
			gap = 1
		}
		// THE NOTE IS INSIDE THE BAND, so it is painted as part of it: dim ink
		// on the selection background is grey on grey, and the tail is the half
		// of the row a person is reading when they stop on it.
		tail := pal.dim(note)
		if selected {
			tail = pal.ink(note)
		}
		line += strings.Repeat(" ", gap) + tail
	}
	switch {
	case selected:
		return pal.band(line, width)
	case hovered:
		return pal.hover(line, width)
	}
	return line
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
//
// level answers what a model has been dialled to; it is passed in rather than
// looked up here because the answer lives on the agent (see [app.reasoningFor])
// and the picker is a list, not a thing that holds a session.
func (p *picker) rows(width, n int, pal palette, hover int, level func(string) string) []string {
	if n <= 0 {
		return nil
	}
	if len(p.hits) == 0 {
		return []string{pal.dim("  no model matches")}
	}
	p.follow(n)
	out := make([]string, 0, n)
	for at := p.top; at < len(p.hits) && len(out) < n; at++ {
		model := p.all[p.hits[at]]
		out = append(out, p.row(model, level(model.ID), at == p.cursor, len(out) == hover, width, pal))
	}
	return out
}

// row is one model: the cursor mark, the id with whatever level it has been
// dialled to, and the dim tail of facts on the right (models.go's [modelNote] —
// window, price, arena score). The model in use is the marked row — that is the
// mark, and it survives scrolling past it.
//
// The level goes with the ID and not into the tail, because it is the one thing
// on the row that is not a fact about the model: it is what THIS person asked
// for, it reads the same here as it does in the status line ("<model>:<level>"),
// and the tail stays what the catalog said.
func (p *picker) row(model Model, level string, selected, hovered bool, width int, pal palette) string {
	note := modelNote(model)
	label := model.ID
	if level != "" {
		// THE LEVEL SURVIVES THE TRUNCATION AND THE NAME GIVES WAY. The row's
		// own law is that the label yields before the note does (see
		// [overlayRow]), and inside the label the same rule applies once more:
		// a clipped id is still recognizable, while a level clipped off the end
		// is a knob that looks like it did nothing. So the id is fitted to what
		// is left AFTER the suffix is reserved, using the same arithmetic the
		// row does — the two cells of the lead, the note, and the gap before it.
		suffix := ":" + level
		room := width - 2 - ansi.StringWidth(suffix)
		if note != "" {
			room -= ansi.StringWidth(note) + 1
		}
		label = fit(model.ID, room) + suffix
	}
	return overlayRow(label, note, selected, model.ID == p.current, hovered, width, pal)
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

// reasoningFor is the level held for a model id, "" when none is or when there
// is no agent to ask (a headless frame).
func (a *app) reasoningFor(id string) string {
	if a.agent == nil {
		return ""
	}
	return a.agent.ReasoningFor(id)
}

// cycleReasoning is ctrl+t: the selected row's model moves one step round the
// cycle, or nothing happens because that model takes no reasoning knob.
func (a *app) cycleReasoning() {
	chosen, ok := a.pick.choice()
	if !ok || !chosen.Reasoning || a.agent == nil {
		return
	}
	a.agent.SetReasoningFor(chosen.ID, nextReasoning(a.reasoningFor(chosen.ID)))
}

// pickerHint is the placeholder in the empty filter box. It is the only place
// this overlay explains itself, and it costs no row of its own.
const pickerHint = "filter · ↑↓ · ctrl+t effort · enter switch · esc cancel"

// ── the app's side of the overlay ───────────────────────────────────────────

// openPicker is /model with no argument. It names the chat law out loud rather
// than leaning on the list having been filtered already: /model is a slot like
// any other, and every slot says which models may answer it (settings.go's
// [filterFor]).
func (a *app) openPicker() {
	a.pick.startFor(a.modelList(), a.model, chatModel)
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

	// The reasoning cycle sits above the filter's default branch on purpose: it
	// is the one key here that is not about the list, and it changes the row
	// rather than the query.
	case "ctrl+t":
		a.cycleReasoning()

	default:
		a.pick.navigate(msg)
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
	switch msg.String() {
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-pickerRows)
	case "pgdown":
		p.move(pickerRows)

	case "backspace":
		p.filter.deleteBackward()
		p.rank()
	case "delete":
		p.filter.deleteForward()
		p.rank()
	case "ctrl+u":
		p.filter.killToStart()
		p.rank()
	case "ctrl+w":
		p.filter.deleteWord()
		p.rank()
	case "left", "ctrl+b":
		p.filter.left()
	case "right", "ctrl+f":
		p.filter.right()
	case "home", "ctrl+a":
		p.filter.home()
	case "end", "ctrl+e":
		p.filter.end()

	default:
		if text := msg.Key().Text; text != "" {
			p.filter.insert(text)
			p.rank()
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
	// claimed their rows would push the status line off the frame. The two
	// reserved rows are the status line and one row of conversation — a list
	// that left neither would be a list that took the screen.
	if room := height - 2 - a.inputHeight() - a.consentHeight() - a.followHeight(); want > room {
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
	case a.menu.open:
		return a.menu.rows(width, n, a.pal, hover)
	case a.comp.open:
		return a.comp.rows(width, n, a.pal, hover)
	}
	return nil
}
