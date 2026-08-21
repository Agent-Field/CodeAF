package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE PHONE'S STATUS DECK ─────────────────────────────────────────────────
//
// The HUD's status is ONE ROW that carries eleven different facts, and it holds
// them by dropping whichever ones do not fit ([dropOrder]). At forty-four
// columns that ladder has nothing left to give: the identity alone is wider
// than the frame, and every number a person opened the terminal to read has
// been dropped before the first segment is drawn.
//
// So at [tierPhone] the row becomes a DECK OF TWO, and the two rows carry the
// two questions a phone-width frame can actually answer at a glance:
//
//	 Fix the nil-map crash          $0.31 · 12%
//	 deepseek-v4-flash        ⏺ 2 running ▸
//
//	row 1   WHAT THIS IS, and WHAT IT HAS COST — the session's name against its
//	        spend and how full it is
//	row 2   WHAT IS ANSWERING, and WHAT IS STILL MOVING — the model chip against
//	        the ambient counts and the state word
//
// The cluster law is the wide row's own: identity left, telemetry right, the
// gap between them as the only separator. Nothing crosses.
//
// AND NOTHING IS DROPPED — it is MOVED. Everything the wide row can carry that
// these two cannot (the contextual key hints, the watch and queue counts, the
// session's diffstat, the cache share, the burn rate, the compaction forecast,
// the workspace and its branch) is one tap away in a fullscreen sheet that
// lists every one of them, one per line. A narrow ladder that silently deletes
// facts teaches a person the numbers are unreliable; a ladder that relocates
// them teaches them where to look.
//
// THE DECK IS TWO ROWS ALWAYS, deterministically, and never one or three. The
// wide row's second row is WRAP-DRIVEN — it appears only when the clusters
// would collide — and a chrome height that has to run the layout to find out
// how tall it is has one more way to disagree with the frame. Here the tier
// decides it: [layoutTier] says phone, the deck is [deckHeight] rows, and
// [app.statusHeight] answers from the tier without laying anything out.

const (
	// deckHeight is what the deck costs the frame. It is a CONSTANT and not a
	// measurement, which is the whole point of the deck — see above.
	deckHeight = 2
	// deckGap is the smallest barrier the two clusters on a deck row will stand
	// next to each other across. It is smaller than the wide row's [hudGap]
	// because at forty-four columns three cells of nothing is seven percent of
	// the frame.
	deckGap = 2
	// deckPad is the one cell every deck row is inset by — the input block's own
	// inset ([inputPad]), so the bottom of the frame has one left margin rather
	// than two.
	deckPad = " "
	// deckTouch is the smallest a tappable target on the deck is allowed to be.
	// A finger is not a pointer: a two-cell chip is a chip that is missed, so a
	// short model name's target is widened to this even where its name is not.
	deckTouch = 4
)

// deckMore is the affordance on the end of row 1: there is more here, and this
// is where it is. It is one glyph rather than a word because the row it sits on
// is the one with a name on it, and a name is what gets cut when a word is
// added beside it.
const (
	deckMore      = "▸"
	deckMoreASCII = ">"
)

// deckRunMark is the glyph on the running-tasks count. It is the roster's own
// running mark ([app.railGlyph]) at rest — the count is a fact about work that
// is turning, and the deck is redrawn on the frame tick that turns it.
const (
	deckRunGlyph = "⏺"
	deckRunASCII = "*"
)

// statusDeck is the phone tier's status: exactly [deckHeight] rows, and the
// only place the model chip's columns are recorded at this width.
//
// It is called from [app.statusRows], which every width resolves through, so
// the frame, the chrome height and the hit-testing cannot disagree about what
// is on these rows or how many of them there are.
func (a *app) statusDeck(width int) []string {
	a.modelSpan = hudSpan{}
	if width < 1 {
		return []string{"", ""}
	}
	// The change clocks are stamped from the FULL segment set rather than from
	// the two the deck draws, for the reason [app.statusLayout] stamps them
	// before applying width pressure: a number that moved has moved whether or
	// not this frame had room to say so — and here the room it has is the sheet,
	// where the same clocks paint the same segments.
	a.freshen(a.telemetry(hudWide))
	rows := []string{a.deckTopRow(width), a.deckModelRow(width)}
	// THE ROW UNDER THE POINTER LIGHTS WHOLE, which is this deck's own bargain read
	// back off [app.deckPress]: every cell of these two rows opens something — the
	// chip its picker, every other cell the sheet — so there is no part of either
	// row a band would be promising a door it does not have (hover.go's law). It is
	// the row and not the chip for the same reason the press takes the whole row: a
	// gap that fell through is a gap that punishes a finger for being a finger.
	for i := range rows {
		if a.hoveringDeck(i) {
			rows[i] = a.hoverRow(rows[i], width)
		}
	}
	return rows
}

// deckTopRow is row 1: the session's name against its spend and its context.
//
// THE WHOLE ROW IS THE DOOR to the sheet, and the ▸ on its right end is what
// says so. A tap target that is one glyph wide is a target that is missed, and
// the alternative — a row where only the mark opens the sheet — would make the
// name beside it a place where missing costs you nothing and teaches you
// nothing.
func (a *app) deckTopRow(width int) string {
	right, plainRight := a.deckSpend()
	mark := a.linearMark(deckMore, deckMoreASCII)
	right, plainRight = right+" "+a.pal.dim(mark), plainRight+" "+mark

	left := a.deckTitle()
	// THE CLUSTER GOES ACCENT IN A ROOM, which is the wide row's own law
	// ([app.statusRows]): the chip is a statement about which page the keyboard
	// is pointed at, and it wears the accent at both ends of the frame.
	paint := a.pal.dim
	if a.roomOpen() {
		paint = a.pal.accent
	}
	return deckJoin(paint, left, right, plainRight, width)
}

// deckModelRow is row 2: what is answering, against what is still moving.
//
// The model is its BASENAME and it sheds its rider here — "via deepinfra · 92
// tok/s" is nine cells this frame does not have, and the sheet carries it whole
// along with the full routing address (see [app.identity] for the same trade at
// every other width).
func (a *app) deckModelRow(width int) string {
	right, plainRight := a.deckAmbient(width)
	chip := modelBase(a.model)
	// A ROOM RENAMES THIS ROW TOO, which is the wide row's own law at phone width
	// (render.go's [app.identityParts]): row 1 has already renamed itself to the
	// task, and a row 2 still naming the session's model would be the deck's half
	// of the same lie — the person is looking at a task's page and reading the
	// conversation's engine. It carries the same "task" lead, the same basename,
	// and the same silence when the node published no model of its own
	// (room.go's [app.roomModelWord] states all three).
	if a.roomOpen() {
		chip = a.roomModelWord()
	}
	if chip == "" {
		// A session that has not been told what is answering has nothing to press
		// and says nothing rather than saying "no model" — see [app.identityParts]
		// for the same silence at every other width.
		return deckJoin(a.pal.dim, "", right, plainRight, width)
	}
	room := width - len(deckPad) - ansi.StringWidth(plainRight) - deckGap
	chip = fit(chip, room)
	if a.roomOpen() {
		// AND IT IS A FACT, NOT A DOOR, while that room is open: no columns are
		// recorded, so the press falls through to the row's other answer — the
		// sheet, which names the conversation's model and the task's on two
		// labelled lines ([app.deckPress], [app.deckItems]) — rather than to a
		// picker that would move a dial this chip does not name. The law and its
		// reasons are the wide row's ([app.identityParts]).
		return deckJoin(a.pal.dim, chip, right, plainRight, width)
	}
	// THE CHIP'S COLUMNS ARE RECORDED WHERE THE ROW IS LAID OUT, which is what
	// keeps the press and the paint in step ([app.statusLayout] states the law).
	// The target is widened to [deckTouch] where the name is shorter than a
	// finger, and it is never widened past what the row has room for.
	to := len(deckPad) + ansi.StringWidth(chip)
	if reach := len(deckPad) + deckTouch; to < reach && reach <= width {
		to = reach
	}
	if ansi.StringWidth(chip) > 0 {
		a.modelSpan = hudSpan{from: len(deckPad), to: to}
	}
	return deckJoin(a.pal.dim, chip, right, plainRight, width)
}

// deckJoin lays one deck row: the identity inset by one cell on the left, the
// telemetry hard against the right edge, and the gap between them as the only
// separator. The left is cut to whatever the right left it.
func deckJoin(paint func(string) string, left, right, plainRight string, width int) string {
	room := width - len(deckPad) - ansi.StringWidth(plainRight) - deckGap
	if room < 1 {
		// Nothing fits beside the numbers: the telemetry is the half that
		// survives, for the reason it survives on the wide row — what is HAPPENING
		// outranks what it is called.
		return rightAlign(right, plainRight, width)
	}
	left = fit(left, room)
	gap := width - len(deckPad) - ansi.StringWidth(left) - ansi.StringWidth(plainRight)
	if gap < 1 {
		return fit(deckPad+paint(left), width)
	}
	return deckPad + paint(left) + strings.Repeat(" ", gap) + right
}

// deckTitle is row 1's left: where you are. A room renames it and nothing else
// on the deck, which is the wide row's law again ([app.identityParts]) — the
// telemetry beside it is still the session's, because a room is a view over one
// body region and not a second session.
func (a *app) deckTitle() string {
	if a.roomOpen() {
		return a.roomChip()
	}
	if name := a.sessionName(); name != "" {
		return name
	}
	return a.place
}

// deckSpend is row 1's right: the bill and the meter, painted by the same two
// clocks the wide row paints them with ([app.paintPart]) — so a figure that
// just moved is as loud here as it is at two hundred columns, and a settled one
// is as quiet.
//
// These two are the segments the phone tier keeps because they are the two a
// person cannot recover by looking at anything else on the screen. The percent
// is the meter's percent alone: "12.4k/128k · 12%" is twenty cells, and at
// forty-four the fraction is what the sheet is for.
func (a *app) deckSpend() (string, string) {
	var parts []hudPart
	if cost := dollars(a.cost); cost != "" {
		parts = append(parts, hudPart{kind: segCost, text: cost})
	}
	if pct, ok := a.ctxPercent(); ok {
		parts = append(parts, hudPart{kind: segCtx, text: itoa(pct) + "%"})
	}
	return a.paintParts(parts)
}

// deckAmbient is row 2's right: what is still moving.
//
//	⏺ 2 running · 1 job · ⠹ working · 4s
//
// THE STATE WORD IS LAST AND IT IS THE LAST TO GO, which is the wide row's own
// order and its own reason ([app.telemetry]): it is the one segment that is
// true of the whole line, and it is why a person is looking at the line at all.
// The counts ahead of it drop from the left when the row runs out — a count of
// background work is a thing you act on later, and "is this alive" is a thing
// you act on now.
func (a *app) deckAmbient(width int) (string, string) {
	var parts []hudPart
	if n := a.deckRunning(); n > 0 {
		parts = append(parts, hudPart{
			kind: segAmbient,
			text: a.linearMark(deckRunGlyph, deckRunASCII) + " " + itoa(n) + " running",
		})
	}
	if jobs := a.hudStats().jobs; jobs > 0 {
		parts = append(parts, hudPart{kind: segAmbient, text: itoa(jobs) + plural(" job", jobs)})
	}
	if word, _ := a.stateSegment(); word != "" {
		parts = append(parts, hudPart{kind: segState, text: word})
	}
	// The budget is the row minus its inset and the smallest name the chip beside
	// it is worth drawing at all.
	room := width - len(deckPad) - deckTouch - deckGap
	for len(parts) > 1 && hudWidth(parts) > room {
		parts = parts[1:]
	}
	return a.paintParts(parts)
}

// deckRunning is how many child agents are working right now — the roster's own
// running group, counted rather than listed (task.go's [railRunning]).
//
// IT ASKS THE ROSTER'S OWN QUESTION and does not re-derive one from the state,
// which is what it used to do. A design waiting on somebody to answer its card is
// `running` on the wire for as long as that card is up, and this line said so —
// "⏺ 1 running", pinned to the status row, about a page that had been sitting
// still on screen since before the person went for coffee (task.go's
// [taskAwaitsPerson] states the whole case).
func (a *app) deckRunning() int {
	n := 0
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil && a.railGroupOf(node) == railRunning {
			n++
		}
	}
	return n
}

// ── THE SHEET: EVERY ITEM, ONE PER LINE ─────────────────────────────────────
//
// The deck is what fits; the sheet is what there IS. It is fullscreen for the
// reason the settings panel is (settings.go's [app.sheetFrame]): a sheet drawn
// into a viewport at forty-four columns is a sheet you read past, and the frame
// under it has nothing on it that a person opening this wanted to keep looking
// at.
//
// Every line is a LABEL and a FACT, in that order, in one column each — the
// same shape the settings panel's rows have, so the two fullscreen surfaces on
// this program are read the same way. The rows a tap can act on carry the same
// ▸ that opened the sheet.

// deckSheet is the sheet's whole state. The zero value is closed and costs the
// frame nothing.
type deckSheet struct {
	open bool
	// cursor is the row the keyboard is on, top the first row drawn, and hot the
	// row the pointer is over (-1 for none, which is the zero value's ONE
	// dishonesty — see [app.openStatusSheet], which sets it).
	cursor int
	top    int
	hot    int
}

// deckAct is what pressing one line of the sheet does.
type deckAct uint8

const (
	// deckActNone is a fact you can read and nothing else, which is most of them.
	deckActNone deckAct = iota
	// deckActModel is the model line: the picker, the same one the chip on the
	// deck opens and the same one /model opens.
	deckActModel
)

// deckItem is one line of the sheet.
type deckItem struct {
	label string
	value string
	act   deckAct
}

// deckHitKind is what one screen row of the sheet IS, for the pointer.
type deckHitKind uint8

const (
	// deckHitNone is the chrome — the title, the rules, the keys, and the empty
	// rows under a short list. A press on any of them is a press OUTSIDE the
	// list, which closes the sheet.
	deckHitNone deckHitKind = iota
	// deckHitItem is one item's row; index is its position in [app.deckItems].
	deckHitItem
)

type deckHit struct {
	kind  deckHitKind
	index int
}

// deckShowing reports whether the sheet is up. It asks the TIER as well as the
// flag: the sheet is the phone form of a row that every wider frame draws in
// full, so a terminal that grew while it was open has already answered the
// question the sheet was opened to answer. [app.frame] is what closes it.
func (a *app) deckShowing() bool {
	if !a.deck.open {
		return false
	}
	width, _ := a.size()
	return layoutTier(width) == tierPhone
}

func (a *app) openStatusSheet() {
	a.deck = deckSheet{open: true, hot: -1}
	a.touch()
}

func (a *app) closeStatusSheet() {
	a.deck = deckSheet{}
	a.touch()
}

// deckItems is EVERY fact the status line can carry, in the order the wide row
// carries them: the identity first, then the telemetry in [app.telemetry]'s own
// order, then the two the legend holds — where you are, and which keys work
// right now.
//
// The list is built from the same functions the row is built from, and that is
// the whole of its correctness: a sheet that assembled its own version of the
// context meter would be a second meter to keep in step with the first.
func (a *app) deckItems() []deckItem {
	items := make([]deckItem, 0, 14)
	add := func(label, value string, act deckAct) {
		if strings.TrimSpace(value) != "" {
			items = append(items, deckItem{label: label, value: value, act: act})
		}
	}

	name := a.sessionName()
	if name == "" {
		name = a.place
	}
	add("session", name, deckActNone)
	if a.roomOpen() && a.room != nil {
		// The room is a SECOND identity rather than a replacement for the first:
		// the deck's row 1 renames itself while one is open, and a person reading
		// the sheet is owed both — which page they are on, and which session it
		// belongs to.
		add("task", a.room.title, deckActNone)
	}
	// The model is its FULL routing address here, not its basename: the sheet is
	// where the thing is recorded and where it is chosen, which is exactly where
	// [app.identity] says the whole id belongs.
	model := a.model
	if level := a.reasoningFor(a.model); level != "" && model != "" {
		model += ":" + level
	}
	add("model", model, deckActModel)
	// AND THE ROOM'S MODEL IS A SECOND LINE RATHER THAN A REPLACEMENT, the way
	// the task's title is a second identity above: the deck's row 2 has room for
	// one model and says the one the page is about, while the sheet is where
	// things are RECORDED and can afford to say both — what the conversation runs
	// on, and what this node ran on — each under its own label, whole address and
	// all. Only the conversation's is a door, because the picker moves the
	// conversation's dial and nothing else (render.go's [app.identityParts]).
	if node := a.roomNode(); node != nil {
		add("task model", strings.TrimSpace(node.model), deckActNone)
	}
	add("served", strings.TrimPrefix(a.servedRider(), " · "), deckActNone)

	for _, part := range a.telemetry(hudWide) {
		if int(part.kind) < len(deckSegWords) {
			add(deckSegWords[part.kind], part.text, deckActNone)
		}
	}
	add("tasks", a.deckTaskWord(), deckActNone)
	// phone lane: AND WHETHER ANYTHING IS KEEPING WATCH WITH NO WINDOW OPEN. It
	// is the one fact here that is not a status-line segment at any width — the
	// segment above says how MANY things are standing, and this says whether they
	// are still looked at once every terminal is closed (homestanding.go's
	// [app.watchLine]). A seam with no answer adds no line. It was /status's
	// alone; the phone's sheet is the other half of that surface and was the one
	// place a person could not reach it (statusnote.go says why the two lists are
	// one list).
	if word, ok := a.watchLine(); ok {
		add(homeWatchLabel, word, deckActNone)
	}

	// WHERE YOU ARE LIVES HERE NOW. The legend under the input used to carry the
	// path and gave the slot up to the conversation's own name (render.go's
	// [app.legendLeft]), so this row and /status are the two places the workspace
	// is written down — which is the right home for it either way: a path is a
	// thing a person copies into another program, and this is a page rather than
	// a border.
	add("place", dotted(a.placePath(0), a.branchWord()), deckActNone)
	// The hint slot's own words, and its own fallback: the keys that work right
	// now, or the input's two affordances when no state has any of its own
	// ([app.legendRight]). At phone width the legend drops this slot entirely,
	// which is what makes it a thing the sheet has to carry.
	keys := a.hintWord()
	if keys == "" {
		keys = microcopy
	}
	add("keys", keys, deckActNone)
	return items
}

// deckSegWords is what each telemetry segment is CALLED, which the wide row
// never has to say: a figure with three cells of context around it is read from
// its shape ("$0.14" is money), and a figure alone on a line is read from its
// label. The words are the ones the code comments already use for them.
var deckSegWords = [segCount]string{
	segAmbient: "background",
	// phone lane: the standing side had NO word at all here, so its segment came
	// out of the loop above with an empty label and hung in the value column
	// under nothing (homestanding.go's [app.keepingSegment] writes the fact).
	segKeeping: "watching",
	segDelta:   "changes",
	segCost:    "spend",
	segCtx:     "context",
	segCache:   "cache",
	segBurn:    "rate",
	segETA:     "compaction",
	segYolo:    "approvals",
	segState:   "state",
}

// deckTaskWord is the roster in one line: what is running, what is waiting, and
// what needs a person. It is the "watch and queue counts" the deck's second row
// has no cells for, and the roster is one ctrl+t away for the detail.
func (a *app) deckTaskWord() string {
	members := a.railMembers()
	var parts []string
	for _, g := range []railGroup{railAttention, railRunning, railIdle, railParked} {
		if n := len(members[g]); n > 0 {
			parts = append(parts, itoa(n)+" "+railGroupWords[g])
		}
	}
	return strings.Join(parts, " · ")
}

// ── the sheet's frame ───────────────────────────────────────────────────────

// deckSheetFrame is the whole screen while the sheet is open: exactly height
// rows, what each of them answers to the pointer, and where the caret sits.
//
// It is ONE function for the reason [app.chrome] and [app.sheetFrame] are: the
// frame draws these rows and the pointer resolves against them, and two answers
// to "which row is the model on" is a tap that opens the picker from the line
// above it.
func (a *app) deckSheetFrame(width, height int) ([]string, []deckHit, int, int) {
	items := a.deckItems()
	a.deck.cursor = clampDeckCursor(a.deck.cursor, len(items))

	lines := make([]string, 0, height)
	hits := make([]deckHit, 0, height)
	add := func(text string, hit deckHit) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	add(deckSheetTitle(width, a.pal), deckHit{})
	add(a.pal.dim(rule(width)), deckHit{})

	// The foot is two rows and it is spoken for before the list is: a rule, and
	// the keys under it.
	const foot = 2
	room := height - len(lines) - foot
	if room < 1 {
		room = 1
	}
	a.deck.top = listTop(a.deck.cursor, a.deck.top, len(items), room)

	label := deckLabelWidth(items)
	for i := 0; i < room; i++ {
		at := a.deck.top + i
		if at >= len(items) {
			add("", deckHit{})
			continue
		}
		add(a.deckItemRow(items[at], at, label, width), deckHit{kind: deckHitItem, index: at})
	}

	add(a.pal.dim(rule(width)), deckHit{})
	add(deckPad+a.pal.dim(fit(a.deckKeysLine(items), width-2)), deckHit{})

	// A terminal too short for the whole sheet keeps its head and its foot: what
	// this is, and how to leave — the settings panel's own trim.
	if len(lines) > height && height > 1 {
		lines = append(lines[:1], lines[len(lines)-(height-1):]...)
		hits = append(hits[:1], hits[len(hits)-(height-1):]...)
	}
	return lines, hits, 0, 0
}

// deckSheetTitle is the head: what this is on the left, how to leave on the
// right — the settings panel's own head, said about this sheet.
func deckSheetTitle(width int, pal palette) string {
	left := deckPad + pal.bold(pal.ink("status"))
	plainLeft := deckPad + "status"
	right := "esc close"
	gap := width - ansi.StringWidth(plainLeft) - len(right) - 1
	if gap < 1 {
		return fit(left, width)
	}
	return left + strings.Repeat(" ", gap) + pal.dim(right)
}

// deckLabelWidth is the label column: the longest label plus a gap, so the
// facts line up in one column and the eye reads down them.
func deckLabelWidth(items []deckItem) int {
	widest := 0
	for _, item := range items {
		if w := ansi.StringWidth(item.label); w > widest {
			widest = w
		}
	}
	return widest + 2
}

// deckItemRow draws one line: the label dim, the fact in ink, and the ▸ on the
// end of the ones a tap can act on.
//
// THE ROW UNDER THE KEYBOARD IS THE ONLY PAINTED ONE, and it is painted by its
// LABEL rather than by a bar across the whole line: a sheet where the selection
// is a block of colour is a sheet where the selection outshouts the facts it
// was opened to show.
func (a *app) deckItemRow(item deckItem, at, label, width int) string {
	selected := at == a.deck.cursor || at == a.deck.hot
	pad := label - ansi.StringWidth(item.label)
	if pad < 1 {
		pad = 1
	}
	name := a.pal.dim(item.label)
	if selected {
		name = a.pal.accent(item.label)
	}
	mark, plainMark := "", ""
	if item.act != deckActNone {
		glyph := a.linearMark(deckMore, deckMoreASCII)
		mark, plainMark = " "+a.pal.dim(glyph), " "+glyph
	}
	room := width - len(deckPad) - ansi.StringWidth(item.label) - pad - ansi.StringWidth(plainMark)
	if room < 1 {
		return fit(deckPad+name, width)
	}
	return deckPad + name + strings.Repeat(" ", pad) + a.pal.ink(fit(item.value, room)) + mark
}

// deckKeysLine names the keys that work on this sheet, and it names the one
// that opens the picker ONLY while the cursor is on a line it would open — a
// hint for a key that does nothing where the person is standing is the failure
// [app.hintWord] exists to prevent.
func (a *app) deckKeysLine(items []deckItem) string {
	line := "esc close · ↑↓ move"
	if at := a.deck.cursor; at >= 0 && at < len(items) && items[at].act == deckActModel {
		line += " · enter model"
	}
	return line
}

func clampDeckCursor(at, count int) int {
	if count == 0 || at < 0 {
		return 0
	}
	if at >= count {
		return count - 1
	}
	return at
}

// ── the sheet's keyboard and pointer ────────────────────────────────────────

// deckSheetKey routes one keypress while the sheet is up. It is modal for the
// reason the settings panel is: there is nothing else on the screen to send a
// key to.
func (a *app) deckSheetKey(msg tea.KeyPressMsg) {
	items := a.deckItems()
	switch msg.String() {
	case "esc", "q":
		a.closeStatusSheet()
	case "up", "k":
		a.deck.cursor = clampDeckCursor(a.deck.cursor-1, len(items))
		a.touch()
	case "down", "j":
		a.deck.cursor = clampDeckCursor(a.deck.cursor+1, len(items))
		a.touch()
	case "enter":
		a.deckActivate(a.deck.cursor, items)
	default:
		a.touch()
	}
}

// deckActivate does what one item's line does.
func (a *app) deckActivate(at int, items []deckItem) {
	if at < 0 || at >= len(items) {
		return
	}
	switch items[at].act {
	case deckActModel:
		// The sheet closes first: the picker takes the input line's place at the
		// bottom of the frame, and a picker under a fullscreen sheet is a picker
		// nobody can see.
		a.closeStatusSheet()
		a.openPicker()
	default:
		a.touch()
	}
}

// deckSheetPress is a click inside the sheet: an item's line selects and — on
// the second press, or on a line that acts — answers, and anything OUTSIDE the
// list closes. Tap-outside is the pointer's esc, and on a surface with no
// window edges the chrome and the empty rows under a short list are what
// "outside" means.
func (a *app) deckSheetPress(x, y int) {
	width, height := a.size()
	items := a.deckItems()
	_, hits, _, _ := a.deckSheetFrame(width, height)
	if y < 0 || y >= len(hits) || hits[y].kind != deckHitItem {
		a.closeStatusSheet()
		return
	}
	at := hits[y].index
	// A press SELECTS, and a press on the row already selected answers it — the
	// settings panel's own two-step ([app.sheetPress]), and for its own reason: a
	// control that fired the moment a finger landed on it is a control that acts
	// on a person who was only reading.
	if a.deck.cursor != at || items[at].act == deckActNone {
		a.deck.cursor = at
		a.touch()
		return
	}
	a.deckActivate(at, items)
}

// deckSheetHover records which line the pointer is over, repainting only when
// the answer changed (hover.go's rule, applied to this sheet).
func (a *app) deckSheetHover(y int) {
	width, height := a.size()
	_, hits, _, _ := a.deckSheetFrame(width, height)
	next := -1
	if y >= 0 && y < len(hits) && hits[y].kind == deckHitItem {
		next = hits[y].index
	}
	if next == a.deck.hot {
		return
	}
	a.deck.hot = next
	a.touch()
}

// deckMove walks the cursor by n, which is what the wheel does to this sheet.
func (a *app) deckMove(n int) {
	a.deck.cursor = clampDeckCursor(a.deck.cursor+n, len(a.deckItems()))
	a.touch()
}

// ── the deck's pointer ──────────────────────────────────────────────────────

// deckPress resolves a click on one of the deck's two rows, and it takes EVERY
// press that lands on them.
//
//	row 2, the model chip   the picker
//	anything else           the sheet
//
// It takes the whole deck rather than only its two targets because of what is
// under it: the deck is the last two rows of the frame, the input block is
// directly above them, and a press that fell through the gap between the name
// and the numbers would put the caret in a sentence a person was not typing.
// A gap that falls through is a gap that punishes a finger for being a finger.
//
// The row is [app.chrome]'s index within the status block, resolved by the
// caller before the column — laying the rows out is what writes the chip's
// columns, and reading them first would be reading the frame before this one
// ([app.statusPress]).
func (a *app) deckPress(x, row int) bool {
	if row == 1 && a.modelSpan.holds(x) {
		a.openPicker()
		return true
	}
	a.openStatusSheet()
	return true
}
