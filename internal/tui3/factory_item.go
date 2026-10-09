package tui3

import (
	"context"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE ITEM PAGE ───────────────────────────────────────────────────────────
//
// One item, whole, over the floor: `enter` on a row opens it and `esc` puts
// the floor back with the cursor on the row it was opened from. It is the
// full width at every width, which is what makes it the way to see detail on a
// terminal too narrow for the peek.
//
//	#1551 filters lost on compact · codeaf · priya · running 26m      $1.42 / $5
//	gate ship [t] · cap $5 [c] · effort — [e] · places: codeaf
//
//	▤ issue             │ review · chat · until clean · max 2 · fanout per-finding
//	● plan              │ read it as a stranger would
//	● write ×3          │
//	● test              │ 1 task · round 1/2
//	◐ review 1/2        │ 09:16 ◎ go test ./internal/tui3/ · 12/12
//	○ neaten            │
//	○ proof             │ s steer · p pause · x stop
//
// IT IS THE PEEK'S LADDER AT THE SCALE OF A PAGE (factory_pane.go): the head
// is two rows and a blank, and every pane on the right is blocks with ONE
// BLANK ROW BETWEEN THEM. The rail on the left starts with the `issue` row,
// whose pane is the whole issue — the body wrapped at [factoryPageProseW] and
// scrollable with `J` and `K`, then the read and the facts — then `talk` when
// the item has a conversation, then the stages. A stage's pane is its knobs,
// dim; its ask, in ink; a blank; what its state has to say; a blank; and the
// keys that work on the item, pinned to the pane's last row.
//
// THE CURSOR LANDS WHERE THE ITEM IS MOVING: the stage waiting on the person,
// else the running one, else the proof of a landed item, else the issue
// ([app.factoryStageFor]). An item nothing is happening to opens on what it
// says.
//
// THE STATE LIVES ON a.fp (open, stage, scroll) AND NOWHERE ELSE, so the
// floor's own cursor, window and narrowings are untouched while the page
// stands over them. THE PAGE READS THE SNAPSHOT AND NOTHING ELSE, like the rest
// of the factory (the framedisk law).
//
// ITS KEYS ARE NAMED ON THE PLACE'S HINT LINE, not on a line of its own
// (place_factory.go's [app.factoryItemHint]): walking the rail, `enter` on a
// stage, `J` and `K`, and `esc` are this file's; every verb is factory_keys.go's
// and keeps working on the item while the page is open. THE VERBS' ROWS, the
// typing row and the habit offer, stand at the bottom of the pane, as they
// stand at the bottom of the peek on the floor.

// factoryStageView is one stage as the item page draws it: the stage, how it
// stands on this item, and the phase that ran it when one has.
type factoryStageView struct {
	stage   factory.Stage
	state   factory.PhaseState // pending when nothing has run it
	phase   factory.Phase
	ran     bool // a phase of the stream stands behind it
	off     bool // switched off on this item
	skipped bool // its condition does not fit the item
	// kind is how the phase behind it draws (factory_marks.go): held,
	// stopped and passed over are told apart from running, failed and still
	// to come. A stage nothing ran is pending, or passed over when the run
	// went on past it.
	kind factoryMarkKind
	// elapsed is how long a running phase has been seen running, one short
	// word (factory_run.go's [app.factoryPhaseElapsed]), and "" otherwise.
	elapsed string
}

// factoryItemStages is the item's stages in order, each matched by name to the
// first phase of its stream not already matched, so a recipe that runs a
// stage twice is drawn twice.
func (a *app) factoryItemStages(it factory.Item) []factoryStageView {
	stages := factoryStages(a.fp.snap, it)
	var phases []factory.Phase
	if it.Stream != nil {
		phases = it.Stream.Phases
	}
	used := make([]bool, len(phases))
	out := make([]factoryStageView, 0, len(stages))
	for _, st := range stages {
		v := factoryStageView{stage: st, state: factory.PhasePending, off: !st.On, skipped: st.On && !factory.Fits(st, it)}
		for i, ph := range phases {
			if !used[i] && ph.Name == st.Name {
				used[i], v.phase, v.state, v.ran = true, ph, ph.State, true
				v.kind = factoryPhaseKind(it, i)
				if v.kind == factoryMarkRunning {
					v.elapsed = a.factoryPhaseElapsed(it.ID, i, ph.Name)
				}
				break
			}
		}
		out = append(out, v)
	}
	// A STAGE THE RUN WENT PAST IS SKIPPED, whether a pending phase stands
	// behind it or none does: a stage still drawn to come after a later one
	// has started, or on an item that has landed, is a promise the run will
	// not keep (factory_marks.go).
	moved := it.State == factory.StateLanded || it.State == factory.StateShipped
	for i := len(out) - 1; i >= 0; i-- {
		v := &out[i]
		if v.ran && v.state != factory.PhasePending {
			moved = true
			continue
		}
		if moved && it.Stream != nil && !v.off && !v.skipped {
			v.kind = factoryMarkSkipped
		}
	}
	return out
}

// factoryPageKind is what one row of the item page's left column stands for:
// ONE FACET OF THE ITEM'S DATA MODEL (owner decision, 2026-10-08, "the item
// page is the issue's map"), or one of the item's own actions under them.
type factoryPageKind int

const (
	factoryPageIssue    factoryPageKind = iota // what it says and what codeaf read, always first
	factoryPageManager                         // the manager's chat, where the seam has the Talk door
	factoryPageSteps                           // the run's steps, each nested under it
	factoryPageStage                           // one step of its recipe, under steps
	factoryPageLog                             // the stream's log, when it has one
	factoryPageResult                          // the sheet, the diff and the checks, once something came out
	factoryPageSettings                        // ask me at, thinking, budget and the steps on or off
	factoryPageAction                          // open on github, refresh, dismiss: one action each
)

// factoryPageRow is one row of the item page's left column: what it stands
// for, for a stage the stage as it stands on the item and where it is among
// them, and for an action the verb it does.
type factoryPageRow struct {
	kind factoryPageKind
	view factoryStageView
	at   int // the stage's place in [app.factoryItemStages]; -1 for the others
	verb factoryVerbRow
}

// factoryItemRows is the left column's rows top to bottom (owner's layout,
// 2026-10-09):
//
//	issue              always
//	manager            the manager's chat, where the seam has the Talk door
//	steps              the run's steps, where there are stages or a stream
//	  ✓ plan           each step, nested under steps, its loop in one line
//	log                the stream's log, once it has said anything
//	result             the sheet, the diff and the checks, once something came out
//	settings           always
//
//	open on github     the item's own actions, each where its door is
//	refresh
//	dismiss
//
// A ROW WITH NOTHING BEHIND IT IS NOT IN THE COLUMN (the emptiness law): an
// item that never ran has no log row and no result row, and an action whose
// door is absent is no row. EVERY ROW IS A STOP: the arrows walk all of them,
// the actions included, and skip nothing.
func (a *app) factoryItemRows(it factory.Item) []factoryPageRow {
	rows := []factoryPageRow{{kind: factoryPageIssue, at: -1}}
	if a.factory.Has("talk") {
		rows = append(rows, factoryPageRow{kind: factoryPageManager, at: -1})
	}
	stages := a.factoryItemStages(it)
	if len(stages) > 0 || it.Stream != nil {
		rows = append(rows, factoryPageRow{kind: factoryPageSteps, at: -1})
	}
	for i, v := range stages {
		rows = append(rows, factoryPageRow{kind: factoryPageStage, view: v, at: i})
	}
	if it.Stream != nil && len(it.Stream.Log) > 0 {
		rows = append(rows, factoryPageRow{kind: factoryPageLog, at: -1})
	}
	if factoryHasResult(it) {
		rows = append(rows, factoryPageRow{kind: factoryPageResult, at: -1})
	}
	rows = append(rows, factoryPageRow{kind: factoryPageSettings, at: -1})
	for _, v := range a.factoryItemActions() {
		rows = append(rows, factoryPageRow{kind: factoryPageAction, at: -1, verb: v})
	}
	return rows
}

// factoryItemActions is the item's own actions under the facets, in the
// order the column draws them, each only where its door is and it acts on
// the item where it stands: the `?` sheet's `also` group read for these
// three ([app.factorySheet]), so the column and the sheet cannot disagree.
func (a *app) factoryItemActions() []factoryVerbRow {
	var sheet []factorySheetRow
	for _, g := range a.factorySheet() {
		if g.name == wordGroupAlso {
			sheet = g.rows
		}
	}
	var out []factoryVerbRow
	for _, word := range []string{wordOpenGitHub, wordRefresh, wordDismiss} {
		for _, r := range sheet {
			if r.word == word {
				out = append(out, factoryVerbRow{word: word, key: r.key})
				break
			}
		}
	}
	return out
}

// factoryHasResult says whether something came out of the item's run: a
// proof sheet, or a diff the run made. A pull request's own diff, read from
// the forge before anything ran, is the issue's, not a result.
func factoryHasResult(it factory.Item) bool {
	return len(it.Proof)+len(it.Policy) > 0 || (it.Stream != nil && strings.TrimSpace(it.Diff) != "")
}

// factoryPageRowAt is the row under the item page's cursor, and false when
// the cursor stands on none.
func (a *app) factoryPageRowAt(it factory.Item) (factoryPageRow, bool) {
	rows := a.factoryItemRows(it)
	if a.fp.stage < 0 || a.fp.stage >= len(rows) {
		return factoryPageRow{}, false
	}
	return rows[a.fp.stage], true
}

// factoryStageFor is the row the page opens on: the stage waiting on the
// person, else the running one, else a landed item's result (its proof
// stage, else its last stage, when nothing came out), else the issue.
func (a *app) factoryStageFor(it factory.Item) int {
	rows := a.factoryItemRows(it)
	find := func(ok func(factoryPageRow) bool) int {
		for i, r := range rows {
			if r.kind == factoryPageStage && ok(r) {
				return i
			}
		}
		return -1
	}
	if at := find(func(r factoryPageRow) bool { return r.view.state == factory.PhaseWaiting }); at >= 0 {
		return at
	}
	if at := find(func(r factoryPageRow) bool { return r.view.state == factory.PhaseRunning }); at >= 0 {
		return at
	}
	if it.State == factory.StateLanded {
		last := -1
		for i, r := range rows {
			if r.kind == factoryPageResult {
				return i
			}
		}
		for i, r := range rows {
			if r.kind == factoryPageStage && r.view.stage.Name == "proof" {
				return i
			}
			if r.kind == factoryPageStage {
				last = i
			}
		}
		if last >= 0 {
			return last
		}
	}
	return 0
}

// factoryOpenItem is `enter` on a row: the item page opens over the floor on
// the row [app.factoryStageFor] names, with the issue read from its top. It
// answers false with no item under the cursor, and the command that lets the
// manager read the issue when the item is owed that ([app.factoryShapeOnOpen]).
func (a *app) factoryOpenItem() (tea.Cmd, bool) {
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil, false
	}
	a.fp.open, a.fp.stage, a.fp.said = true, a.factoryStageFor(it), false
	a.fp.scroll, a.fp.scrollID = 0, it.ID
	a.fp.hot, a.fp.box, a.fp.leftTop = factoryItemHot{}, false, -1
	a.pageMsg = ""
	a.touch()
	return a.factoryShapeOnOpen(it), true
}

// ── THE MANAGER READS THE ISSUE WHEN YOU OPEN IT ────────────────────────────
//
// The owner's decision of 2026-10-08. The first time this window opens the
// page of an item the manager has never shaped, the floor's Shape door
// ([factory.Seam.Shape]) gives the manager its shaping turn at once, off the
// loop and off the ordered line (a turn waits on a model, and a key pressed
// meanwhile must not wait behind it). The story shows the manager thinking
// ([app.factoryManagerThinking]) until the door answers; the floor is then
// read, so the stages redraw as the manager set them. The note line says the
// door's line only when the turn failed.
//
// ONCE PER ITEM PER WINDOW, and never for an item only seen on the floor. The
// door itself decides the rest (an item whose conversation the person spoke in
// is left alone) and answers [factory.ShapeAlready], which says nothing.

// factoryShapeOnOpen asks the Shape door for it when this window has not yet,
// and answers nil when there is no door or nothing is owed.
func (a *app) factoryShapeOnOpen(it factory.Item) tea.Cmd {
	shape := a.factory.Shape
	if shape == nil || a.fp.shapeAsked[it.ID] || factoryShapedAlready(it) {
		return nil
	}
	if a.fp.shapeAsked == nil {
		a.fp.shapeAsked = map[int]bool{}
	}
	if a.fp.shaping == nil {
		a.fp.shaping = map[int]bool{}
	}
	id := it.ID
	a.fp.shapeAsked[id] = true
	a.fp.shaping[id] = true
	a.factoryManagerThinking(id, true)
	load := a.factory.Load
	return a.besideLine(func() func(bool) tea.Cmd {
		line, err := shape(context.Background(), id)
		var snap factory.Snapshot
		var lerr error
		if load != nil {
			snap, lerr = load()
		}
		return func(bool) tea.Cmd {
			delete(a.fp.shaping, id)
			a.factoryManagerThinking(id, false)
			switch {
			case lerr != nil:
				a.fp.err = lerr
			case load != nil:
				a.factoryFold(snap)
			}
			if err != nil && strings.TrimSpace(line) != factory.ShapeAlready {
				said := strings.TrimSpace(line)
				if said == "" {
					said = strings.TrimSpace(err.Error())
				}
				a.pageMsg = said
			}
			a.touch()
			return nil
		}
	})
}

// factoryShapedAlready says what the floor already shows about an item that
// is owed no shaping turn: it ran, or the manager set its stages or kept its
// recipe. The door holds the same rule and the person's lines besides.
func factoryShapedAlready(it factory.Item) bool {
	if s := it.Stream; s != nil && (!s.Started.IsZero() || len(s.Phases) > 0) {
		return true
	}
	for _, st := range it.Stages {
		if st.By == factory.ByManager {
			return true
		}
	}
	for _, l := range it.Adapted {
		if strings.HasPrefix(strings.TrimSpace(l), factory.ByManager+" ") {
			return true
		}
	}
	return false
}

// factoryCloseItem is `esc` on the item page: the floor comes back with its
// cursor on the row the page was opened from, because nothing moved it.
func (a *app) factoryCloseItem() {
	a.fp.open, a.fp.said = false, false
	a.fp.crumbHover = factoryCrumbNone
	a.fp.scroll = 0
	a.fp.hot, a.fp.box = factoryItemHot{}, false
	a.fp.host = factoryHost{}
	a.pageMsg = ""
	a.touch()
}

// factoryStageMove walks the rail's cursor by delta and lets go of the note
// the last stage put on the place's note line. The issue is read from its top
// again each time it is arrived at.
func (a *app) factoryStageMove(delta int) {
	it, ok := a.factoryCursorItem()
	if !ok {
		return
	}
	a.factoryStageSelect(moveCursor(a.fp.stage, delta, len(a.factoryItemRows(it))))
}

// factoryStageSelect puts the rail's cursor on row at.
func (a *app) factoryStageSelect(at int) {
	if at != a.fp.stage {
		a.fp.scroll = 0
	}
	a.fp.stage = at
	a.fp.said = false
	a.fp.box = false
	a.fp.leftTop = -1
	a.pageMsg = ""
	a.touch()
}

// factoryLayoutKey is the layout's own keys, read before the place's others:
// `z` turns the density, `enter` opens the item under the cursor, `{` `}` and
// `|` move the divider (factory_split.go), `J` and `K` (and `pgdn` and `pgup`)
// scroll the item's body; and while the item page is open it is the page's
// walk ([app.factoryItemKey]). It answers false for every other key, which
// goes on to mean what it meant before.
//
// `ENTER` ON A FLOOR ROW OPENS THE ITEM PAGE AND NEVER LAUNCHES: launching is
// `r`, `space` and `L` (factory_keys.go, factory_bar.go).
//
// IT STANDS ASIDE for the map, the tab bar's cursor, the words box and a
// verb's typing row, each of which has the keyboard while it is up.
func (a *app) factoryLayoutKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.mapShowing || a.bar.on || a.fp.typing || a.fp.act.ask != nil || !a.factoryConnected() {
		return nil, false
	}
	k := msg.String()
	if a.fp.open {
		return a.factoryItemKey(msg)
	}
	if took := a.factoryScrollKey(k); took {
		return nil, true
	}
	if cmd, took := a.factorySplitKey(k); took {
		return cmd, true
	}
	switch k {
	case "h":
		// `h` is the handover's height, wherever the floor is showing.
		if !a.factoryFloorHas() {
			return nil, false
		}
		return a.factoryToggleHead(), true
	case "z":
		if !a.factoryFloorHas() {
			return nil, false
		}
		a.factoryRefocus(func() { a.fp.comfy = !a.fp.comfy })
		return nil, true
	case "enter":
		return a.factoryOpenItem()
	}
	return nil, false
}

// factoryItemKey is a key on the item page while the keys walk it (the
// center's box, when it has them, is factory_host.go's):
//
//	↑ ↓          walk the left column, every row in order, the actions too
//	→ or tab     put the keys in the center's box: the chat the center
//	             hosts, or the manager's box before the manager has a chat
//	←            stays in the left column, where it already is
//	enter        act on the row ([app.factoryItemEnter])
//	space        the top bar's control: run, pause or continue
//	esc          back to the floor
//
// THE KEYBOARD MOVES THE CURSOR AND THE POINTER MOVES THE HOVER, and the two
// never fight: a key lets the hover go, so the one ground on the column is
// the cursor's. Every other key goes on to the verbs (factory_keys.go).
func (a *app) factoryItemKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if a.fp.hot != (factoryItemHot{}) {
		a.fp.hot = factoryItemHot{}
		a.touch()
	}
	switch k {
	case "esc":
		a.factoryCloseItem()
		return nil, true
	case "up", "ctrl+p":
		a.factoryStageMove(-1)
		return nil, true
	case "down", "ctrl+n":
		a.factoryStageMove(1)
		return nil, true
	case "left":
		return nil, true
	case "right", "tab":
		if a.factoryFocusCenter() {
			return nil, true
		}
		if k == "right" {
			return nil, true
		}
		return nil, false
	case keyControl:
		if cmd, took := a.factoryControlPress(); took {
			return cmd, true
		}
		return nil, false
	case "enter":
		return a.factoryItemEnter(), true
	}
	if took := a.factoryScrollKey(k); took {
		return nil, true
	}
	return nil, false
}

// factoryFocusCenter puts the keys in the center's box, and says whether
// there was one: the hosted chat's own box, or the manager's box on the
// manager row while the manager has no chat yet. A center that is read and
// never typed into (the issue, the log, the settings) has none.
func (a *app) factoryFocusCenter() bool {
	if a.factoryHosting() {
		a.fp.box = true
		a.fp.hot = factoryItemHot{}
		a.touch()
		return true
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return false
	}
	if r, ok := a.factoryPageRowAt(it); ok && r.kind == factoryPageManager && a.factory.Has("talk") {
		a.pageMsg = ""
		a.factoryTLOpenBox(it)
		return true
	}
	return false
}

// factoryIssueDoor is what `enter` on the issue row does on this seam.
type factoryIssueDoor int

const (
	factoryIssueNone  factoryIssueDoor = iota // nothing to open yet
	factoryIssueTalk                          // the manager's chat, in the center
	factoryIssueForge                         // the item's page on github, as `g`
)

// factoryIssueEnter is the door `enter` on the issue row opens: the
// manager's chat when the seam has the Talk door, else `g`'s door when the
// item has a page on its forge, else none. The hint and the key both ask
// it, so they cannot disagree.
func (a *app) factoryIssueEnter(it factory.Item) factoryIssueDoor {
	switch {
	case a.factory.Has("talk"):
		return factoryIssueTalk
	case it.URL != "" && it.Origin != factory.OriginTerminal && a.factory.Has("open"):
		return factoryIssueForge
	}
	return factoryIssueNone
}

// factoryNothingToOpen is the action line's sentence for `enter` on the issue
// row when the seam has neither the Talk door nor `g`'s: `r runs it` only
// where `r` does.
func (a *app) factoryNothingToOpen(it factory.Item) string {
	words := "nothing to open yet"
	if a.factoryCanRun() && (it.State == factory.StateNew || it.State == factory.StateDismissed) {
		words += rowSep + factoryHintClause(keyRun, wordRun)
	}
	return words
}

// factoryItemEnter is `enter` on the item page, and a double press on a row
// of its left column. THE CHAT STAYS IN THE CENTER: `enter` on the issue
// walks to the manager's row and puts the keys in its box (or opens the item
// on github where there is no chat door); on the manager and on a step whose
// chat the center hosts it puts the keys in the box; on an action it does the
// action. The proof of a landed item answers as its sheet, and a step with a
// chat the center cannot host (a terminal too narrow for the column) opens
// that chat the way a room is walked into. Every other step says why it has
// none; the steps, the log, the result and the settings do nothing.
func (a *app) factoryItemEnter() tea.Cmd {
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	r, ok := a.factoryPageRowAt(it)
	if !ok {
		return nil
	}
	switch r.kind {
	case factoryPageAction:
		a.pageMsg = ""
		return a.factoryVerbPress(r.verb)
	case factoryPageIssue:
		switch a.factoryIssueEnter(it) {
		case factoryIssueTalk:
			a.pageMsg = ""
			if !a.factoryCanHost() {
				return a.factoryTalk(it)
			}
			return a.factoryTalkHere(it)
		case factoryIssueForge:
			a.pageMsg = ""
			return a.factoryOpenForge(it)
		}
		a.fp.said = true
		a.touch()
		return nil
	case factoryPageManager:
		if a.factoryHosting() {
			a.fp.box = true
			a.touch()
			return nil
		}
		if !a.factoryCanHost() {
			return a.factoryTalk(it)
		}
		return a.factoryTalkHere(it)
	case factoryPageStage:
	default:
		return nil
	}
	// A SHEET WITH NO DOOR FOR ITS `enter` (the still fixture) says the
	// stage's note like any other stage, rather than nothing.
	if a.factoryOnProof(it) {
		a.pageMsg = ""
		if cmd, took := a.factoryLandedKey(it, "enter"); took {
			return cmd
		}
	}
	if factoryRoomOf(r) != "" {
		if a.factoryFocusCenter() {
			return nil
		}
		if !a.factoryCanHost() {
			a.pageMsg = ""
			return a.factoryOpenRoom(it, r.view)
		}
		return nil
	}
	// `ENTER` ON A STAGE WITH NO ROOM SAYS WHY on the pane's last line
	// ([app.factoryPagePane]).
	a.fp.said = true
	a.touch()
	return nil
}

// ── the pointer ─────────────────────────────────────────────────────────────

// factoryHotKind is what the pointer rests on, on the item page.
type factoryHotKind int

const (
	factoryHotNone    factoryHotKind = iota
	factoryHotControl                // the top bar's control
	factoryHotCrumb                  // a crumb of the trail
	factoryHotRow                    // a row of the left column
	factoryHotBox                    // the manager's box, before the manager has a chat
)

// factoryItemHot is the one thing the pointer rests on: which kind, and the
// row or the crumb.
type factoryItemHot struct {
	kind  factoryHotKind
	row   int
	crumb factoryCrumb
}

// factoryItemGeo is where the last draw put the item page's parts, in
// screen cells: the bar's row and the control's cells on it, the left
// column's first row, its width, the row each of its drawn lines stands for
// (-1 for the blank before the actions) and the first line of the whole
// column it drew, and the center's first cell. hosted
// says the center was the chat itself (factory_host.go). boxY is the screen
// row of the manager's box, -1 when none was drawn.
type factoryItemGeo struct {
	barY         int
	ctlX0, ctlX1 int
	leftY        int
	leftW        int
	lines        []int
	first        int
	centerX      int
	centerY      int
	boxY         int
	hosted       bool
}

// factoryItemHotAt is what the last draw put at screen cell (x, y).
func (a *app) factoryItemHotAt(x, y int) factoryItemHot {
	g := &a.fp.geo
	if y == g.barY {
		if g.ctlX0 >= 0 && x >= g.ctlX0 && x < g.ctlX1 {
			return factoryItemHot{kind: factoryHotControl}
		}
		for _, h := range a.fp.crumbHits {
			if x >= h.x0 && x < h.x1 {
				return factoryItemHot{kind: factoryHotCrumb, crumb: h.crumb}
			}
		}
		return factoryItemHot{}
	}
	if g.leftW > 0 && x < g.leftW && y >= g.leftY && y-g.leftY < len(g.lines) {
		if at := g.lines[y-g.leftY]; at >= 0 {
			return factoryItemHot{kind: factoryHotRow, row: at}
		}
		return factoryItemHot{}
	}
	if g.boxY >= 0 && y == g.boxY && x >= g.centerX {
		return factoryItemHot{kind: factoryHotBox}
	}
	return factoryItemHot{}
}

// factoryItemHover is the pointer resting at (x, y): the one thing under it
// takes the pointer's ground, and everything else gives it up. It reports
// whether the pointer is on something.
func (a *app) factoryItemHover(x, y int) bool {
	next := a.factoryItemHotAt(x, y)
	if next != a.fp.hot {
		a.fp.hot = next
		a.fp.crumbHover = next.crumb
		a.touch()
	}
	return next.kind != factoryHotNone
}

// factoryItemPress is a left press at screen cell (x, y) on the item page:
// exactly what the key does. The control is `space`, a crumb its road, a row
// of the left column is the cursor landing on it (and a second press on the
// same row is `enter`; an action acts on the first), and the manager's box
// takes the keys. A press anywhere else moves nothing.
func (a *app) factoryItemPress(x, y int) tea.Cmd {
	hot := a.factoryItemHotAt(x, y)
	switch hot.kind {
	case factoryHotControl:
		cmd, _ := a.factoryControlPress()
		return cmd
	case factoryHotCrumb:
		return a.factoryCrumbPress(hot.crumb)
	case factoryHotBox:
		a.factoryFocusCenter()
		return nil
	case factoryHotRow:
		it, ok := a.factoryCursorItem()
		if !ok {
			return nil
		}
		rows := a.factoryItemRows(it)
		if hot.row >= len(rows) {
			return nil
		}
		again := hot.row == a.fp.stage
		a.factoryStageSelect(hot.row)
		a.fp.leftTop = a.fp.geo.first
		if rows[hot.row].kind == factoryPageAction {
			return a.factoryItemEnter()
		}
		if a.countClick(x, y) >= 2 && again {
			return a.factoryItemEnter()
		}
	}
	return nil
}

// factoryItemWheel is the wheel at screen cell (x, y) on the item page: over
// the left column it moves the column's window when the column is longer
// than its room, and over the issue it scrolls the issue. It answers false
// elsewhere, and the hosted chat takes its own wheel (factory_host.go).
func (a *app) factoryItemWheel(x, y, delta int) bool {
	g := &a.fp.geo
	if g.leftW > 0 && x < g.leftW && y >= g.leftY {
		it, ok := a.factoryCursorItem()
		if !ok {
			return false
		}
		lines := len(a.factoryColumnLines(a.factoryItemRows(it)))
		room := len(g.lines)
		top := a.fp.leftTop
		if top < 0 {
			top = g.first
		}
		a.fp.leftTop = max(min(top+delta, lines-room), 0)
		a.touch()
		return true
	}
	return a.factoryScrollBy(delta)
}

// factoryScrollKey is `J` and `K`, one row down and up the item's body, and
// `pgdn` and `pgup`, a page: on the floor the peek's body, and on the item
// page the issue while the rail stands on it. It answers false for every other
// key, and takes these four wherever there is an item, so a body with nothing
// below it does not hand them on to mean something else.
func (a *app) factoryScrollKey(k string) bool {
	step := 0
	switch k {
	case "J", "shift+j":
		step = 1
	case "K", "shift+k":
		step = -1
	case "pgdown":
		step = max(a.fp.scrollPage, 1)
	case "pgup":
		step = -max(a.fp.scrollPage, 1)
	default:
		return false
	}
	if _, ok := a.factoryCursorItem(); !ok || a.fp.pick != nil || a.fp.recipe != nil {
		return false
	}
	a.factoryScrollBy(step)
	return true
}

// factoryScrollBy moves the item's body step rows, down for a positive step,
// within what the last draw measured: THE ONE SCROLL every road to the rest
// of the body shares — `J` and `K` and the page keys, the wheel over the peek
// or the issue, and a press on `▾ more` ([app.factoryPointer]). It answers
// false where there is no body to move: no item, a settings page standing,
// or an item page whose rail stands on a row other than the issue.
func (a *app) factoryScrollBy(step int) bool {
	it, ok := a.factoryCursorItem()
	if !ok || a.fp.pick != nil || a.fp.recipe != nil {
		return false
	}
	if a.fp.open {
		if r, ok := a.factoryPageRowAt(it); !ok || r.kind != factoryPageIssue {
			return false
		}
	}
	if a.fp.scrollID != it.ID {
		a.fp.scrollID, a.fp.scroll = it.ID, 0
	}
	from := max(min(a.fp.scroll, a.fp.scrollMax), 0)
	next := max(min(from+step, a.fp.scrollMax), 0)
	if next != a.fp.scroll {
		a.fp.scroll = next
		a.touch()
	}
	return true
}

// factoryOnProof says whether the item page stands on the proof of a landed
// item, the one stage whose `enter` is the sheet's: the stage named proof, or
// the last stage of a recipe that has no sheet at all, which is the row the
// page opens a landed item on ([app.factoryStageFor]). The result row's
// `enter` does nothing: the sheet's keys are `s`, `e` and `B` there.
func (a *app) factoryOnProof(it factory.Item) bool {
	if !a.fp.open || it.State != factory.StateLanded {
		return false
	}
	r, ok := a.factoryPageRowAt(it)
	if !ok || r.kind != factoryPageStage {
		return false
	}
	return r.view.stage.Name == "proof" || a.fp.stage == a.factoryStageFor(it)
}

// ── drawing ─────────────────────────────────────────────────────────────────

// factoryItemBody is the item page as exactly room rows of exactly width
// cells, when the center is not a hosted chat (factory_host.go draws that
// one): the top bar's rows (factory_bar.go), then the left column beside the
// center, the pane of the row under the cursor, under a rule. Under
// [factoryStageFloor] the column is one line over the pane. No row is a hit;
// a press is read against what the draw placed ([app.factoryItemPress]).
func (a *app) factoryItemBody(it factory.Item, width, room int) []placeRow {
	rows := a.factoryItemRows(it)
	a.fp.stage = moveCursor(a.fp.stage, 0, len(rows))
	a.fp.pageRows, a.fp.bodyW = room, width
	a.fp.railTop, a.fp.railFirst, a.fp.railShown = 0, 0, 0
	a.fp.moreRow = -1
	g := &a.fp.geo
	*g = factoryItemGeo{barY: placeHeadRows, ctlX0: -1, ctlX1: -1, boxY: -1}
	lead := factoryMarginPad()
	lines := a.factoryBarRows(it, width)
	g.barY = placeHeadRows
	left := room - len(lines)
	switch {
	case left <= 0:
		lines = lines[:max(room, 0)]
	case width < factoryStageFloor:
		measure := max(width-factoryMargins, 0)
		strip := lead + a.factoryCellStrip(a.factoryPageCellsIn(it, rows, false), a.fp.stage, measure)
		a.fp.railTop, a.fp.railShown = len(lines), 1
		lines = append(lines, strip)
		pane := a.factoryPagePane(it, rows, measure, left-1)
		g.boxY = -1
		for _, line := range pane {
			if a.factoryHasMore(line) {
				a.fp.moreRow = len(lines)
			}
			lines = append(lines, factoryPad(lead+line, width))
		}
	default:
		paneW := width - factoryItemColW - factoryRuleW
		col := a.factoryItemColumn(it, rows, left)
		g.leftY, g.leftW = placeHeadRows+len(lines), factoryItemColW
		g.centerX, g.centerY = factoryItemColW+factoryRuleW, g.leftY
		a.fp.railTop, a.fp.railShown = len(lines), len(col)
		pane := a.factoryPagePane(it, rows, max(paneW-factoryMargins, 0), left)
		if a.fp.geo.boxY >= 0 {
			g.boxY += g.centerY
		}
		sep := a.pal.dim(a.linearMark("│", "|"))
		for i := 0; i < left; i++ {
			right := ""
			if pane[i] != "" {
				right = lead + pane[i]
			}
			if a.factoryHasMore(right) {
				a.fp.moreRow = len(lines)
			}
			lines = append(lines, col[i]+sep+factoryPad(right, paneW))
		}
	}
	out := make([]placeRow, room)
	for i := range out {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		out[i] = placeRow{text: factoryPad(text, width), hit: -1}
	}
	return out
}

// factoryColumnLines is the left column's lines as the row each stands for,
// top to bottom: every row, and one blank line (-1) before the first
// action, so the actions read as the item's own and not as a facet.
func (a *app) factoryColumnLines(rows []factoryPageRow) []int {
	out := make([]int, 0, len(rows)+1)
	for i, r := range rows {
		if r.kind == factoryPageAction && i > 0 && rows[i-1].kind != factoryPageAction {
			out = append(out, -1)
		}
		out = append(out, i)
	}
	return out
}

// factoryItemColumn is the left column as exactly room lines of
// [factoryItemColW] cells: its window over [app.factoryColumnLines], following
// the cursor unless the wheel moved it, the cursor's row on the selected
// ground and the row the pointer rests on, when it is another, on the
// pointer's. Where each line stands is kept for the pointer
// (factoryPage.geo).
func (a *app) factoryItemColumn(it factory.Item, rows []factoryPageRow, room int) []string {
	pal := a.pal
	cells := a.factoryPageCells(it, rows)
	all := a.factoryColumnLines(rows)
	cursorLine := 0
	for i, at := range all {
		if at == a.fp.stage {
			cursorLine = i
		}
	}
	top := a.fp.leftTop
	if top < 0 {
		top = placeTop(0, cursorLine, len(all), room)
	}
	top = max(min(top, len(all)-room), 0)
	g := &a.fp.geo
	g.first = top
	g.lines = g.lines[:0]
	out := make([]string, max(room, 0))
	for i := range out {
		at := top + i
		if at >= len(all) || room <= 0 {
			out[i] = factorySpaces(factoryItemColW)
			continue
		}
		g.lines = append(g.lines, all[at])
		r := all[at]
		if r < 0 {
			out[i] = factorySpaces(factoryItemColW)
			continue
		}
		c := cells[r]
		tailW := ansi.StringWidth(c.tailPlain)
		w := factoryItemColW - factoryMargin - c.indent - tailW
		text := ""
		if c.flat {
			text = c.paint(fit(c.rest, w)) + c.tail
		} else {
			text = c.markPaint(c.mark) + c.paint(fit(c.rest, w-ansi.StringWidth(c.mark))) + c.tail
		}
		line := factoryPad(factorySpaces(factoryMargin+c.indent)+text, factoryItemColW)
		switch {
		case r == a.fp.stage:
			line = pal.selected(line, factoryItemColW)
		case a.fp.hot.kind == factoryHotRow && a.fp.hot.row == r:
			line = pal.cursor(line, factoryItemColW)
		}
		out[i] = line
	}
	return out
}

// factoryCrumb is one button of the item page's trail.
type factoryCrumb int

const (
	factoryCrumbNone    factoryCrumb = iota
	factoryCrumbFactory              // `Factory`: the floor, as `esc`
	factoryCrumbRepo                 // the repo: the floor narrowed to it
	factoryCrumbRef                  // the ref: the item on github, as `g`
)

// factoryCrumbHit is a crumb as the last draw placed it on the page's first
// row: the cells from x0 up to x1, and which crumb.
type factoryCrumbHit struct {
	x0, x1 int
	crumb  factoryCrumb
}

// factoryItemCrumbsAt is the trail up to the item's ref, painted, starting
// at screen column x0 of the bar, with each crumb's cells kept for the
// pointer: `Factory › codeaf › #1551`. The crumb the pointer rests on wears
// the pointer's ground. THE REF IS A BUTTON ONLY WHERE `g` OPENS IT
// ([app.factoryRefOpens]), and a repo the trail does not name is no crumb.
func (a *app) factoryItemCrumbsAt(it factory.Item, x0, measure int) string {
	pal := a.pal
	sep := " " + a.linearMark("›", ">") + " "
	a.fp.crumbHits = a.fp.crumbHits[:0]
	x := x0
	add := func(word string, crumb factoryCrumb, paint func(string) string) string {
		w := ansi.StringWidth(word)
		if crumb != factoryCrumbNone && x-factoryMargin < measure {
			a.fp.crumbHits = append(a.fp.crumbHits, factoryCrumbHit{x0: x, x1: min(x+w, factoryMargin+measure), crumb: crumb})
		}
		x += w
		if crumb != factoryCrumbNone && a.fp.hot.kind == factoryHotCrumb && crumb == a.fp.hot.crumb {
			return pal.cursor(word, w)
		}
		return paint(word)
	}
	out := add(wordFloorCrumb, factoryCrumbFactory, pal.dim) + add(sep, factoryCrumbNone, pal.dim)
	if r := factoryRepoShort(it.Repo); r != "" {
		out += add(r, factoryCrumbRepo, pal.dim) + add(sep, factoryCrumbNone, pal.dim)
	}
	ref := factoryCrumbNone
	if a.factoryRefOpens(it) {
		ref = factoryCrumbRef
	}
	return out + a.factoryRefLink(it, add(it.Ref(), ref, pal.ink))
}

// factoryRefOpens says whether `g` opens the item on github: it has a page
// there, it did not start on this machine, and the seam has the door.
func (a *app) factoryRefOpens(it factory.Item) bool {
	return it.URL != "" && it.Origin != factory.OriginTerminal && a.factory.Has("open")
}

// factoryCrumbPress is a press on a crumb: `Factory` puts the floor back as
// `esc` does, the repo puts it back narrowed to that repo as `[ ]` would,
// and the ref opens the item on github through `g`'s own door.
func (a *app) factoryCrumbPress(c factoryCrumb) tea.Cmd {
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	switch c {
	case factoryCrumbFactory:
		a.factoryCloseItem()
	case factoryCrumbRepo:
		a.factoryCloseItem()
		for i, r := range a.fp.snap.Repos {
			if r.Name == it.Repo {
				a.factoryRefocus(func() { a.fp.repo = i + 1 })
				break
			}
		}
	case factoryCrumbRef:
		a.pageMsg = ""
		return a.factoryOpenForge(it)
	}
	return nil
}

// factoryCrumbs is the trail above a page on the floor, dim, up to the page's
// own name: `Factory › codeaf › `. A repo the page does not name is no crumb.
func (a *app) factoryCrumbs(repo string) string {
	sep := " " + a.linearMark("›", ">") + " "
	trail := wordFloorCrumb + sep
	if r := factoryRepoShort(repo); r != "" {
		trail += r + sep
	}
	return a.pal.dim(trail)
}

// factoryItemChips is the head's second row: the gate, the cap and the
// effort with the key that turns each, and the OTHER repos the item touches.
//
// THE REPO IT ARRIVED ON IS THE CRUMB AND NOTHING ELSE: the trail above
// already says `Factory › codeaf ›`, and a `places  codeaf` under it said the
// same word twice with a label to read first. An item the plan spread over
// more repos names the others, `also harness, agentfield`, dim.
func (a *app) factoryItemChips(it factory.Item, measure int) string {
	line := a.factoryChipRow(it, true, measure)
	if others := a.factoryItemOthers(it); others != "" {
		line += factorySpaces(factoryFactGap) + others
	}
	return fit(line, measure)
}

// factoryItemOthers is the other repos the item touches, dim, `also harness,
// agentfield`, and nothing for an item on one repo (the emptiness law). With
// the verbs on the right it is all the head's second row says, because the
// chips stand in the column's `set` group.
func (a *app) factoryItemOthers(it factory.Item) string {
	home := factoryRepoShort(it.Repo)
	var others []string
	for _, p := range it.Places {
		if short := factoryRepoShort(p); short != "" && short != home {
			others = append(others, short)
		}
	}
	if len(others) == 0 {
		return ""
	}
	return a.pal.dim("also " + strings.Join(others, ", "))
}

// factoryStageLabel is one stage as the rail names it, plain, and its paint:
// its mark and its name, the tasks it split into when done, its round over its
// most while running, and `· skipped` on a stage whose condition does not fit.
// A stage switched off or skipped is dim with the pending mark.
func (a *app) factoryStageLabel(v factoryStageView) (string, func(string) string) {
	pal := a.pal
	if v.off || v.skipped || v.kind == factoryMarkSkipped {
		// A STAGE THAT WILL NOT RUN WEARS THE SKIP STROKE AND SAYS SO, switched
		// off, its condition missed or passed over alike: the pending ring
		// it used to wear promised a stage still to come (factory_marks.go).
		mark, _ := a.factoryKindMark(factoryMarkSkipped)
		return mark + " " + v.stage.Name + rowSep + "skipped", pal.dim
	}
	mark, _ := a.factoryStageMark(v)
	words := v.stage.Name
	if v.ran {
		words = factoryPhaseWords(factory.Phase{Name: v.stage.Name, State: v.state, Round: v.phase.Round, Tasks: v.phase.Tasks}, max(v.stage.Max, 1))
	} else if v.stage.Max > 1 {
		words += " ×" + strconv.Itoa(v.stage.Max)
	}
	switch v.kind {
	case factoryMarkPaused:
		words += rowSep + "paused"
	case factoryMarkStopped:
		words += rowSep + "stopped"
	}
	paint := pal.muted
	switch {
	case v.kind == factoryMarkPaused:
	case v.state == factory.PhaseRunning:
		paint = pal.ink
	case v.state == factory.PhasePending:
		paint = pal.dim
	}
	return mark + " " + words, paint
}

// factoryPendingMark is the pending stage's mark, the one a stage still to
// come wears.
func (a *app) factoryPendingMark() string {
	mark, _ := a.factoryPhaseMark(factory.PhasePending)
	return mark
}

// factoryStageMark is a stage's mark on the item page's rail and its paint:
// the mark of the phase behind it ([app.phaseMark]'s mapping), and A RUNNING
// PHASE WEARS THE TRANSCRIPT'S SPINNER where its mark stands, because a shape
// that moves says it is moving (PRESENCE OVER LABELS). A held phase does not
// move, so it wears the pause mark and never the spinner.
func (a *app) factoryStageMark(v factoryStageView) (string, func(string) string) {
	kind := v.kind
	if !v.ran && kind != factoryMarkSkipped {
		kind = factoryMarkPending
	}
	mark, paint := a.factoryKindMark(kind)
	if kind == factoryMarkRunning {
		mark = a.factorySpin()
	}
	return mark, paint
}

// factoryRailCell is one row of a rail as it is drawn: its mark and the paint
// the mark wears, the words after it and theirs, and flat when the whole row
// is one paint (a stage switched off or skipped).
type factoryRailCell struct {
	mark      string
	markPaint func(string) string
	rest      string
	paint     func(string) string
	flat      bool
	// tail is what follows the words, already painted, and tailPlain its
	// plain spelling: a stage room's conversation mark and a running
	// phase's elapsed. THE WORDS ARE CUT BEFORE THE TAIL IS, so the room's
	// mark is never what a narrow rail loses.
	tail, tailPlain string
	// indent is the air before the row past the margin: [factoryNestW] for
	// a row nested under another (the stages and the log under `run`).
	indent int
}

// label is the cell's plain words, mark and all.
func (c factoryRailCell) label() string {
	if c.flat {
		return c.rest + c.tailPlain
	}
	return c.mark + c.rest + c.tailPlain
}

// factoryStageCell is one stage as a rail cell: its mark, its words, and
// after them, dim, the conversation's mark when the stage has a room and how
// long a running one has run. The elapsed is the first thing a narrow rail
// drops.
func (a *app) factoryStageCell(v factoryStageView) factoryRailCell {
	return a.factoryStageCellIn(v, 0)
}

// factoryStageCellIn is [app.factoryStageCell] indented by indent cells, the
// room it measures against narrowed by as much.
func (a *app) factoryStageCellIn(v factoryStageView, indent int) factoryRailCell {
	room := factoryRailW - factoryMargin - indent
	label, paint := a.factoryStageLabel(v)
	if v.off || v.skipped || v.kind == factoryMarkSkipped {
		// A NAME TOO LONG FOR THE RAIL BESIDE ITS WORD KEEPS THE NAME WHOLE and
		// lets the word go: the stroke still says skipped, and `security ·
		// skip…` said less than `security` does.
		if ansi.StringWidth(label) > room {
			mark, _ := a.factoryKindMark(factoryMarkSkipped)
			label = mark + " " + v.stage.Name
		}
		return factoryRailCell{rest: label, paint: paint, markPaint: paint, flat: true, indent: indent}
	}
	mark, markPaint := a.factoryStageMark(v)
	c := factoryRailCell{mark: mark, markPaint: markPaint, rest: strings.TrimPrefix(label, mark), paint: paint, indent: indent}
	if v.ran && strings.TrimSpace(v.phase.Chat) != "" {
		room := " " + a.icon(tokens.GActionCommunicate)
		c.tail, c.tailPlain = a.pal.dim(room), room
	}
	if v.elapsed != "" {
		e := rowSep + v.elapsed
		if ansi.StringWidth(c.label())+ansi.StringWidth(e) <= room {
			c.tail, c.tailPlain = c.tail+a.pal.dim(e), c.tailPlain+e
		}
	}
	return c
}

// factoryPageCells is the item page's left column as cells: each facet by
// its word, in ink, with no mark of its own; the stages and the log nested
// under `run` by [factoryNestW], each stage with its mark and its loop in
// columns ([app.factoryStageRowCell]); and the run's facts after its word,
// dim ([app.factoryRunFacts]).
func (a *app) factoryPageCells(it factory.Item, rows []factoryPageRow) []factoryRailCell {
	return a.factoryPageCellsIn(it, rows, true)
}

// factoryPageCellsIn is [app.factoryPageCells], its stages' facts in the
// rail's columns when grid is set and run on after the name when it is not,
// for the one-line strip a narrow terminal draws.
func (a *app) factoryPageCellsIn(it factory.Item, rows []factoryPageRow, grid bool) []factoryRailCell {
	pal := a.pal
	cells := make([]factoryRailCell, 0, len(rows))
	facet := func(word string, indent int) factoryRailCell {
		return factoryRailCell{rest: word, paint: pal.ink, markPaint: pal.ink, flat: true, indent: indent}
	}
	var views []factoryStageView
	for _, r := range rows {
		if r.kind == factoryPageStage {
			views = append(views, r.view)
		}
	}
	cols := a.factoryStageColumns(it, views, grid)
	for _, r := range rows {
		switch r.kind {
		case factoryPageIssue:
			cells = append(cells, facet(wordFacetIssue, 0))
		case factoryPageManager:
			cells = append(cells, facet(wordFacetManager, 0))
		case factoryPageSteps:
			c := facet(wordFacetSteps, 0)
			if facts := a.factoryRunFacts(it, factoryItemColW-factoryMargin-ansi.StringWidth(wordFacetSteps)-factoryGutter); facts != "" {
				c.tail, c.tailPlain = factorySpaces(factoryGutter)+pal.dim(facts), factorySpaces(factoryGutter)+facts
			}
			cells = append(cells, c)
		case factoryPageLog:
			cells = append(cells, facet(wordFacetLog, 0))
		case factoryPageAction:
			cells = append(cells, factoryRailCell{rest: r.verb.word, paint: pal.muted, markPaint: pal.muted, flat: true})
		case factoryPageResult:
			cells = append(cells, facet(wordFacetResult, 0))
		case factoryPageSettings:
			cells = append(cells, facet(wordFacetSettings, 0))
		default:
			cells = append(cells, a.factoryStageRowCell(it, views, r.at, cols))
		}
	}
	return cells
}

// factoryStageCols is the item page's stage columns as one rail draws them:
// the rounds column's width, 0 when no stage has rounds, whether the glyph
// column is drawn, and grid, unset for the one-line strip, where the facts run
// on after the name instead of standing in columns.
type factoryStageCols struct {
	rounds int
	glyph  bool
	grid   bool
}

// factoryStageColumns is the columns the stages draw: A COLUMN NO STAGE HAS
// ANYTHING IN IS NOT DRAWN (the emptiness law), and the rounds column is
// [factoryStageRoundsW] unless a stage's rounds are wider.
func (a *app) factoryStageColumns(it factory.Item, views []factoryStageView, grid bool) factoryStageCols {
	cols := factoryStageCols{grid: grid}
	for i, v := range views {
		if factoryStageOut(v) {
			continue
		}
		if r := factoryStageRounds(v); r != "" {
			cols.rounds = max(cols.rounds, factoryStageRoundsW, ansi.StringWidth(r))
		}
		if g, _ := a.factoryStageGlyph(it, views, i); g != "" {
			cols.glyph = true
		}
	}
	return cols
}

// factoryStageOut says whether a stage will not run on the item: switched
// off, its condition missed, or passed over by the run.
func factoryStageOut(v factoryStageView) bool {
	return v.off || v.skipped || v.kind == factoryMarkSkipped
}

// factoryStageRounds is a stage's rounds as its row says them: `×2`, the most
// it may take, before it runs, and `1/2`, the round it is on over its most,
// once it has (factory_run.go's [factoryRoundWords]). A stage of one round
// says nothing.
func factoryStageRounds(v factoryStageView) string {
	most := v.stage.Max
	if v.ran && v.phase.Round > 0 {
		return factoryRoundWords(v.phase.Round, max(most, 1))
	}
	if most > 1 {
		return "×" + strconv.Itoa(most)
	}
	return ""
}

// factoryStageGlyph is the one glyph a stage row carries and its paint: `?`
// where the run stops to ask you ([factoryStageAsks]), else `+` where someone
// other than the recipe set the stage, and nothing otherwise. Both are dim:
// the stage waiting on you now says so with its own amber mark.
func (a *app) factoryStageGlyph(it factory.Item, views []factoryStageView, at int) (string, func(string) string) {
	v := views[at]
	switch {
	case factoryStageOut(v):
		return "", a.pal.dim
	case factoryStageAsks(it, views, at):
		return a.factoryAsksMark(), a.pal.dim
	case stageSetByOther(v.stage):
		return a.factoryAddedMark(), a.pal.dim
	}
	return "", a.pal.dim
}

// factoryStageAsks says whether the run stops at the stage at to ask the
// person, read the way the runner reads it (internal/factory/run's loop): a
// gate stage is a person; a stage whose own gate applies to the item asks
// before it runs; `ask me at plan` asks after the plan stage; and every item
// whose ask-me-at is not never is signed off after its last stage that runs.
func factoryStageAsks(it factory.Item, views []factoryStageView, at int) bool {
	v := views[at]
	if factoryStageOut(v) {
		return false
	}
	st := v.stage
	if st.Kind == factory.StageGate || (st.Gate != factory.GateShip && factory.GateApplies(st, it)) {
		return true
	}
	first, last := -1, -1
	for i, w := range views {
		if factoryStageOut(w) {
			continue
		}
		if first < 0 && w.stage.Name == wordGatePlan {
			first = i
		}
		last = i
	}
	if it.Gate == factory.GatePlan && first == at {
		return true
	}
	return it.Gate != factory.GateNone && last == at
}

// factoryStageRowCell is the step at as a row of the item page's left
// column, IN ONE LINE (owner's layout, 2026-10-09): its mark, its one-word
// name, and after it its loop, `↻ 1/3` once it runs and `↻ 3` before, what
// it loops until, how long a running step has run, and `+` where the manager
// (or a person, or the plan) added it:
//
//	⠋ review ↻ 1/2 until clean
//	○ neaten +
//	? approve waiting for you
//
// A STEP THAT HOLDS THE RUN FOR YOU (an approve step, a gate step waiting)
// says so in the ask's ink, and says nothing else. THE NAME IS NEVER CUT
// for the rest: the elapsed goes first, then `until`, then the mark, then
// the loop. A step that will not run keeps its one dim line, `– neaten ·
// skipped`. The one-line strip under [factoryStageFloor] reads the same
// cell; cols is kept for the recipe page's columns.
func (a *app) factoryStageRowCell(it factory.Item, views []factoryStageView, at int, cols factoryStageCols) factoryRailCell {
	v := views[at]
	if factoryStageOut(v) {
		return a.factoryStageCellIn(v, factoryNestW)
	}
	pal := a.pal
	room := factoryItemColW - factoryMargin - factoryNestW
	mark, markPaint := a.factoryStageMark(v)
	_, paint := a.factoryStageLabel(v)
	type part struct {
		plain string
		paint func(string) string
		keep  int // the higher, the later it is dropped
	}
	var parts []part
	if a.factoryStageHeld(it, v) {
		mark, markPaint = a.icon(tokens.GNeedsHuman), pal.ask
		parts = append(parts, part{wordWaitingForYou, pal.ask, 9})
	} else {
		switch v.kind {
		case factoryMarkPaused:
			parts = append(parts, part{wordPause + "d", pal.muted, 8})
		case factoryMarkStopped:
			parts = append(parts, part{"stopped", pal.muted, 8})
		}
		if loop := a.factoryStageLoop(v); loop != "" {
			parts = append(parts, part{loop, paint, 7})
		}
		if u := strings.TrimSpace(v.stage.Until); u != "" && v.stage.Max > 1 {
			parts = append(parts, part{wordUntil + " " + u, pal.dim, 3})
		}
		if v.ran && strings.TrimSpace(v.phase.Chat) != "" {
			parts = append(parts, part{a.icon(tokens.GActionCommunicate), pal.dim, 6})
		}
		if v.elapsed != "" {
			parts = append(parts, part{v.elapsed, pal.dim, 1})
		}
		if glyph, glyphPaint := a.factoryStageGlyph(it, views, at); glyph != "" {
			parts = append(parts, part{glyph, glyphPaint, 5})
		}
	}
	width := func() int {
		w := ansi.StringWidth(mark) + 1 + ansi.StringWidth(v.stage.Name)
		for _, p := range parts {
			w += 1 + ansi.StringWidth(p.plain)
		}
		return w
	}
	for len(parts) > 0 && width() > room {
		low := 0
		for i, p := range parts {
			if p.keep < parts[low].keep {
				low = i
			}
		}
		parts = append(parts[:low], parts[low+1:]...)
	}
	var tail, tailPlain string
	for _, p := range parts {
		tail += " " + p.paint(p.plain)
		tailPlain += " " + p.plain
	}
	return factoryRailCell{mark: mark, markPaint: markPaint, rest: " " + v.stage.Name, paint: paint, tail: tail, tailPlain: tailPlain, indent: factoryNestW}
}

// factoryStageHeld says whether step v is the one holding item it's run for
// a person: a gate step (an approve step) waiting.
func (a *app) factoryStageHeld(it factory.Item, v factoryStageView) bool {
	if !v.ran || v.state != factory.PhaseWaiting {
		return false
	}
	at := a.factoryHeldAt(it)
	return at >= 0 && it.Stream.Phases[at].Name == v.stage.Name
}

// factoryStageLoop is a step's loop in one cell, `↻ 1/3` while it runs
// (the round it is on over its most) and `↻ 3` before, and nothing for a
// step of one round.
func (a *app) factoryStageLoop(v factoryStageView) string {
	most := v.stage.Max
	if most <= 1 {
		return ""
	}
	if v.ran && v.phase.Round > 0 {
		return a.icon(tokens.GLoop) + " " + factoryRoundWords(v.phase.Round, most)
	}
	return a.icon(tokens.GLoop) + " " + strconv.Itoa(most)
}

// factoryRunFacts is the run row's facts, plain: where it stands, how long it
// has run and what it has spent, `running 4m · $0.31`, the spend and then
// the time dropped until it fits in room, and nothing for an item that
// never ran (the emptiness law).
func (a *app) factoryRunFacts(it factory.Item, room int) string {
	s := it.Stream
	if s == nil {
		return ""
	}
	state := factoryStateWord(it)
	timed := state
	if e := factoryElapsed(s.Started, factoryEnd(s, a.fp.snap.Now)); e != "" && !factoryPaused(it) {
		timed += " " + e
	}
	for _, line := range []string{strings.Join(nonEmpty([]string{timed, factoryMoney(s.Spent)}), rowSep), timed, state} {
		if ansi.StringWidth(line) <= room {
			return line
		}
	}
	return ""
}

// factoryStageRail is the recipe page's stage rail (factory_settings.go): the
// stages as rail cells under the stage cursor.
func (a *app) factoryStageRail(views []factoryStageView, room int) []string {
	cells := make([]factoryRailCell, 0, len(views))
	for _, v := range views {
		cells = append(cells, a.factoryStageCell(v))
	}
	rail, _ := a.factoryCellRail(cells, a.fp.stage, room)
	return rail
}

// factoryStageStrip is the recipe page's stage rail as one line.
func (a *app) factoryStageStrip(views []factoryStageView, measure int) string {
	cells := make([]factoryRailCell, 0, len(views))
	for _, v := range views {
		cells = append(cells, a.factoryStageCell(v))
	}
	return a.factoryCellStrip(cells, a.fp.stage, measure)
}

// factoryCellRail is a rail: one row per cell, [factoryRailW] wide, the
// window following the cursor, and the cursor's row on the cursor ground and
// nothing else changed on it. The mark carries the row's colour; the words
// wear the reading tiers. It answers the rows and the first cell drawn.
func (a *app) factoryCellRail(cells []factoryRailCell, cursor, room int) ([]string, int) {
	pal := a.pal
	top := placeTop(0, cursor, len(cells), room)
	out := make([]string, room)
	for i := range out {
		at := top + i
		if at >= len(cells) {
			out[i] = factorySpaces(factoryRailW)
			continue
		}
		c := cells[at]
		text := ""
		tailW := ansi.StringWidth(c.tailPlain)
		room := factoryRailW - factoryMargin - c.indent - tailW
		if c.flat {
			text = c.paint(fit(c.rest, room)) + c.tail
		} else {
			text = c.markPaint(c.mark) + c.paint(fit(c.rest, room-ansi.StringWidth(c.mark))) + c.tail
		}
		row := factoryPad(factorySpaces(factoryMargin+c.indent)+text, factoryRailW)
		if at == cursor {
			row = pal.cursorRow(row, factoryRailW)
		}
		out[i] = row
	}
	return out, top
}

// factoryCellStrip is a rail as one line, for a terminal under
// [factoryStageFloor]: the cells left to right, the cursor's on its ground,
// as many WHOLE cells as fit from the cursor's side. THE CURSOR'S CELL IS
// ALWAYS ON THE LINE: cells before it are dropped from the left until it fits.
func (a *app) factoryCellStrip(cells []factoryRailCell, cursor, measure int) string {
	var segs, plains []string
	for i, c := range cells {
		label := c.label()
		seg := c.paint(strings.TrimSuffix(label, c.tailPlain)) + c.tail
		if i == cursor {
			seg = a.pal.cursorRow(seg, ansi.StringWidth(label))
		}
		segs = append(segs, seg)
		plains = append(plains, label)
	}
	from := 0
	for from < cursor && cursor < len(cells) {
		w := 0
		for i := from; i <= cursor; i++ {
			w += ansi.StringWidth(plains[i]) + factoryGutter
		}
		if w <= measure {
			break
		}
		from++
	}
	return factoryJoinWhole(segs[from:], plains[from:], factorySpaces(factoryGutter), measure)
}

// factoryPagePane is the center of the row under the cursor, when the
// center is not a hosted chat, as exactly room lines of at most measure
// cells:
//
//	issue      the whole issue, scrolled with `J` and `K`
//	manager    the manager's box, before the manager has a chat
//	steps      every step as the recipe and the manager wrote it
//	a step     its knobs, its ask and what its state has to say, or, for a
//	           step with a chat, the chat coming in (factory_host.go)
//	log        the stream's log
//	result     the sheet, the diff and the checks
//	settings   the knobs and the steps on or off
//	an action  the issue, which the action is about
func (a *app) factoryPagePane(it factory.Item, rows []factoryPageRow, measure, room int) []string {
	out := make([]string, max(room, 0))
	if room <= 0 || a.fp.stage >= len(rows) {
		return out
	}
	r := rows[a.fp.stage]
	var lines []string
	switch r.kind {
	case factoryPageIssue, factoryPageAction:
		lines = a.factoryIssuePane(it, measure, room)
	case factoryPageManager:
		lines = a.factoryManagerPane(it, measure, room)
	case factoryPageSteps:
		lines = a.factoryStepsPane(it, measure, room)
	case factoryPageLog:
		lines = a.factoryLogPane(it, measure, room)
	case factoryPageResult:
		lines = a.factoryResultPane(it, measure, room)
	case factoryPageSettings:
		lines = a.factorySettingsPane(it, measure, room)
	default:
		if words := a.factoryHostWaitWords(); words != "" {
			lines = []string{a.pal.dim(fit(words, measure))}
			break
		}
		var views []factoryStageView
		for _, row := range rows {
			if row.kind == factoryPageStage {
				views = append(views, row.view)
			}
		}
		lines = a.factoryStagePane(it, views, r.at, measure, room)
	}
	copy(out, lines)
	return out
}

// factoryManagerPane is the center on the manager row while the manager has
// no chat to host yet: what the manager is for, and its box on the last
// row, `› enter or click to talk to the manager`, which a press or `enter`
// or `→` puts the keys in. The words go through the Say door
// ([app.factoryTimelineSend]); the chat they make is the center from then on.
func (a *app) factoryManagerPane(it factory.Item, measure, room int) []string {
	pal := a.pal
	if room <= 0 {
		return nil
	}
	var box string
	if ask := a.fp.act.ask; ask != nil && ask.kind == factoryAskManager {
		box = a.factoryAskLine(ask, measure)
	} else {
		box = pal.dim(fit(a.linearMark(tokens.GlyphPromptChat, ">")+" "+wordEnterOrClickToTalk+" "+wordToTheManager, measure))
		if a.fp.hot.kind == factoryHotBox {
			box = pal.cursor(box, measure)
		}
	}
	var body []string
	if words := a.factoryHostWaitWords(); words != "" {
		body = append(body, pal.dim(fit(words, measure)))
	}
	if a.factoryTLThinking(it) {
		body = append(body, a.factorySpin()+" "+pal.muted(fit(wordManager+" "+wordIsThinking, max(measure-factoryLeadW, 0))))
	}
	out := factoryPaneLadder([][]string{body}, box, room)
	a.fp.geo.boxY = room - 1
	return out
}

// factoryStepsPane is the center on the steps row: every step in order, its
// mark and name in ink, its knobs dim beside it, and its ask under it, as
// the recipe and the manager wrote them.
func (a *app) factoryStepsPane(it factory.Item, measure, room int) []string {
	pal := a.pal
	var blocks [][]string
	for _, v := range a.factoryItemStages(it) {
		mark, markPaint := a.factoryStageMark(v)
		if factoryStageOut(v) {
			mark, markPaint = a.factoryKindMark(factoryMarkSkipped)
		}
		head := markPaint(mark) + " " + pal.ink(v.stage.Name)
		knobs := strings.TrimPrefix(factoryKnobs(v.stage), v.stage.Name+rowSep)
		line := fit(head+factorySpaces(factoryGutter)+pal.dim(knobs), measure)
		block := []string{line}
		if ask := strings.TrimSpace(v.stage.Ask); ask != "" {
			for _, l := range wrap(ask, max(measure-factoryLeadW, 1)) {
				block = append(block, factorySpaces(factoryLeadW)+pal.muted(l))
			}
		}
		blocks = append(blocks, block)
	}
	return factoryPaneLadder(blocks, "", room)
}

// factoryPaneLadder lays blocks over room lines with the action line pinned
// to the last of them and a blank row above it: the peek's shape at the scale
// of the item page's pane. The blocks are cut from their foot when they do
// not fit; an action line with nothing to say leaves its rows to the blocks.
func factoryPaneLadder(blocks [][]string, action string, room int) []string {
	out := make([]string, max(room, 0))
	if room <= 0 {
		return out
	}
	avail := room
	if action != "" {
		avail = room - 1
		if room >= 3 {
			avail = room - factoryActionRows
		}
		out[room-1] = action
	}
	lines := factoryStack(blocks)
	if len(lines) > avail {
		lines = lines[:max(avail, 0)]
	}
	copy(out, lines)
	return out
}

// factoryPageAction is the item's keys on the item page's pane, dim: the
// verbs that work on it where it stands ([app.factoryVerbHint], the peek's
// strip without its `enter`, which the bottom line names), AT MOST
// [factoryStripMost] clauses with extra, and nothing when there are none.
// Every other key is on the `?` sheet.
func (a *app) factoryPageAction(it factory.Item, measure int, extra ...string) string {
	words := append(append([]string{}, extra...), a.factoryVerbHint(it)...)
	if len(words) > factoryStripMost {
		words = words[:factoryStripMost]
	}
	if len(words) == 0 {
		return ""
	}
	return a.pal.dim(fit(strings.Join(words, " · "), measure))
}

// factoryPaneAction is the pane's action line as the page draws it: the
// keys that work on what the center shows (`J K scroll` on a long issue),
// and nothing else. THE ITEM'S VERBS ARE NOT REPEATED UNDER EVERY PANE: the
// run's own is the top bar's control, the item's actions stand under the
// left column, and every key is on the `?` sheet (owner's layout,
// 2026-10-09).
func (a *app) factoryPaneAction(it factory.Item, measure int, extra ...string) string {
	if len(extra) == 0 {
		return ""
	}
	return a.pal.dim(fit(strings.Join(extra, " · "), measure))
}

// factoryIssuePane is the whole issue: its body wrapped at [factoryPageProseW],
// then the factory's read and the facts, then the questions it would put to
// the author when the item is thin — one document, scrolled with `J` and `K`
// from where they left it, its last row marked when more is below.
//
// THE BODY IS MARKDOWN, rendered (factory_forge.go's [app.factoryMarkdown]),
// and the forge's blocks follow the read, each comment whole. Over the read
// stands the line that says when it was made, `read 3m ago · u refresh`, which
// spins while a read is out ([app.factoryReadLine]).
func (a *app) factoryIssuePane(it factory.Item, measure, room int) []string {
	pal := a.pal
	w := max(min(measure, factoryPageProseW), 1)
	body := a.factoryMarkdown(it.Body, w)
	var read []string
	if line := a.factoryReadLine(it, w); line != "" {
		read = append(read, line)
	}
	for _, line := range wrap(strings.TrimSpace(it.Triage.Read), w) {
		if strings.TrimSpace(line) != "" {
			read = append(read, pal.ink(line))
		}
	}
	// WHAT CODEAF READ stands under its sentence, dim: the type, the size,
	// the estimate, the priority and its reason, each only where the read
	// said it (the emptiness law).
	if line := factoryReadFacts(it); line != "" {
		read = append(read, pal.dim(fit(line, w)))
	}
	var facts []string
	if f := factoryFacts(it); len(f) > 0 {
		facts = []string{pal.dim(factoryJoinWhole(f, f, factorySpaces(factoryFactGap), measure))}
	}
	// THE READ'S OPEN QUESTIONS ARE ON THE ISSUE whenever it has any: the
	// issue is the one place what codeaf read is read whole.
	var asks []string
	if qs := nonEmpty(it.Triage.Questions); len(qs) > 0 {
		for _, line := range wrap("it would ask "+factoryOr(it.Author, "the author")+" "+strings.Join(qs, " / "), w) {
			asks = append(asks, pal.muted(line))
		}
	}
	var adapted []string
	if line := a.factoryAdaptedRow(it, w); line != "" {
		adapted = []string{line}
	}
	doc := factoryStack(append([][]string{body, read, facts, asks, adapted}, a.factoryForgeBlocks(it, w, 0)...))
	if len(doc) == 0 {
		return make([]string, max(room, 0))
	}
	// THE DOCUMENT IS WINDOWED FIRST, so the action line knows whether `J`
	// and `K` have anywhere to go before it names them.
	window := room
	if room >= 3 {
		window = room - factoryActionRows
	}
	shown := a.factoryScrolled(it, doc, w, window)
	extra := []string{}
	if a.fp.scrollMax > 0 {
		extra = append(extra, factoryHintClause(keyScroll, wordScroll))
	}
	return factoryPaneLadder([][]string{shown}, a.factoryIssueAction(it, measure, extra...), room)
}

// factoryReadFacts is what codeaf read about the item on one line, plain:
// `bug · M · ~$3 · priority 2 · touches the meter`, each part only where the
// read said it, and nothing for an item not read yet.
func factoryReadFacts(it factory.Item) string {
	t := it.Triage
	parts := []string{strings.TrimSpace(t.Type), strings.TrimSpace(t.Size)}
	if est := factoryMoney(t.Est); est != "" {
		parts = append(parts, "~"+est)
	}
	if t.Priority > 0 {
		parts = append(parts, "priority "+strconv.Itoa(t.Priority))
	}
	parts = append(parts, strings.TrimSpace(t.Reason))
	return strings.Join(nonEmpty(parts), rowSep)
}

// factoryIssueAction is the action line under the issue and the manager row: the
// item's keys, or, after an `enter` that had nothing to open, the sentence
// that says so, until the cursor moves or another key is pressed.
func (a *app) factoryIssueAction(it factory.Item, measure int, extra ...string) string {
	if a.fp.said {
		return a.pal.dim(fit(a.factoryNothingToOpen(it), measure))
	}
	return a.factoryPaneAction(it, measure, extra...)
}

// factoryResultPane is what came out of the run: the proof sheet as the
// proof stage draws it, then the diff and the checks, each a block with its
// word over it; on a landed item the sign-off line is pinned where the keys
// stand, because on the sheet those ARE the keys ([app.factoryProofPane]).
func (a *app) factoryResultPane(it factory.Item, measure, room int) []string {
	pal := a.pal
	blocks := [][]string{a.factorySheetRows(it, measure)}
	action := a.factoryPaneAction(it, measure)
	if it.State == factory.StateLanded {
		action = a.factorySignOffLine(it, measure)
	} else if line := a.factorySignOffLine(it, measure); line != "" {
		blocks = append(blocks, []string{line})
	}
	if d := strings.TrimSpace(it.Diff); d != "" {
		blocks = append(blocks, []string{a.factoryBlockHead(wordDiff, measure), pal.ink(fit(d, measure))})
	}
	blocks = append(blocks, a.factoryChecksBlock(it, measure))
	return factoryPaneLadder(blocks, action, room)
}

// factorySettingsPane is the item's settings, the knobs that stood in the
// verbs' `set` group and on the head's chips, as a table:
//
//	ask me at  plan        t
//	thinking   —           e
//	budget     $3          c
//
//	1 plan     on
//	2 write    on
//	3 test     off
//
//	1-9 stages · s add a stage
//
// EACH KEY IS DRAWN ONLY WHERE IT ACTS on the item where it stands, the `?`
// sheet's own predicates ([app.factorySheet]): a running item's gate and
// budget are read here and turned nowhere. The stages carry their number only
// while `1-9` turns them, and the set group's other keys stand under them.
// The keys are the place's own (factory_keys.go): they act from every row of
// the page, and this pane is where a person reads what they turn.
func (a *app) factorySettingsPane(it factory.Item, measure, room int) []string {
	pal := a.pal
	acts := map[string]bool{}
	var more []string
	for _, g := range a.factorySheet() {
		if g.name != wordGroupSet {
			continue
		}
		for _, r := range g.rows {
			acts[r.key] = true
			switch r.key {
			case keyAskAt, keyThinking, keyBudget, keyWalk:
			default:
				more = append(more, factoryHintClause(r.key, r.word))
			}
		}
	}
	stages := factoryStages(a.fp.snap, it)
	labelW := factorySetLabelW
	for _, st := range stages {
		labelW = max(labelW, factorySetNumW+ansi.StringWidth(st.Name)+factoryLabelGap)
	}
	row := func(label, value, key string, valuePaint func(string) string) string {
		line := pal.muted(factoryPad(label, labelW))
		if key == "" {
			return fit(line+valuePaint(value), measure)
		}
		return fit(line+valuePaint(factoryPad(value, factorySetValueW))+pal.dim(key), measure)
	}
	// THE KNOBS STAND IN THE ORDER A PERSON TURNS THEM BEFORE A RUN: where it
	// stops to ask, how hard it thinks, what it may spend.
	chips := map[string]factoryChip{}
	for _, c := range a.factoryChipList(it) {
		chips[c.label] = c
	}
	var knobs []string
	for _, label := range []string{wordAskAt, wordThinking, wordBudget} {
		c := chips[label]
		key := ""
		if acts[c.key] {
			key = c.key
		}
		if strings.TrimSpace(c.value) == "" && key == "" {
			continue
		}
		knobs = append(knobs, row(c.label, c.value, key, pal.ink))
	}
	var lines []string
	numbered := acts[keyStages]
	for i, st := range stages {
		num := factorySpaces(factorySetNumW)
		if numbered && i < 9 {
			num = factoryPad(strconv.Itoa(i+1), factorySetNumW)
		}
		state, paint := wordOn, pal.ink
		if !st.On {
			state, paint = wordOff, pal.dim
		}
		// A STAGE THE RECIPE FIXES WEARS THE LOCK where its `on` stands, and
		// the whole row is dim: no key here switches it off
		// (factory_stagefixed.go).
		if stageLocked(st) {
			lines = append(lines, pal.dim(fit(factoryPad(num+st.Name, labelW)+a.icon(tokens.GLocked), measure)))
			continue
		}
		lines = append(lines, row(num+st.Name, state, "", paint))
	}
	var foot []string
	if len(more) > 0 {
		foot = []string{pal.dim(fit(strings.Join(more, rowSep), measure))}
	}
	return factoryPaneLadder([][]string{knobs, lines, foot}, a.factoryPaneAction(it, measure), room)
}

// factoryStagePane is the stage at views[at] as exactly room lines of at most
// measure cells: its knobs, dim; its ask, in ink; what its state has to say;
// and the item's keys on the last line, a blank row between each.
func (a *app) factoryStagePane(it factory.Item, views []factoryStageView, at, measure, room int) []string {
	if room <= 0 || at < 0 || at >= len(views) {
		return make([]string, max(room, 0))
	}
	pal := a.pal
	v := views[at]
	head := []string{pal.dim(fit(factoryKnobs(v.stage), measure))}
	if ask := strings.TrimSpace(v.stage.Ask); ask != "" {
		head = append(head, pal.ink(fit(ask, measure)))
	}
	if v.stage.Name == "proof" && len(it.Proof)+len(it.Policy) > 0 {
		return a.factoryProofPane(it, head, measure, room)
	}
	action := a.factoryPaneAction(it, measure)
	// `ENTER` ON A STAGE WITH NO ROOM SAYS WHY ON THIS LINE, where the keys
	// stand, until the cursor moves or another key is pressed.
	if a.fp.said && strings.TrimSpace(v.phase.Chat) == "" {
		action = pal.dim(fit(factoryNoRoomWords(v), measure))
	}
	tailRoom := room - len(head) - 1
	if action != "" {
		tailRoom -= factoryActionRows
	}
	tail := a.factoryStageTail(it, views, at, measure, max(tailRoom, 0))
	return factoryPaneLadder([][]string{head, tail}, action, room)
}

// factoryKnobs is the stage's structured words on one line: its name, its
// kind, until, its most rounds, its fanout and its condition. A knob with no
// value is left out, and the condition says `when always` when it has none.
func factoryKnobs(st factory.Stage) string {
	kind := string(st.Kind)
	if kind == "" {
		kind = string(factory.StageChat)
	}
	knobs := []string{st.Name, kind}
	if u := strings.TrimSpace(st.Until); u != "" {
		knobs = append(knobs, "until "+u)
	}
	if st.Max > 1 {
		knobs = append(knobs, "max "+strconv.Itoa(st.Max))
	}
	if f := strings.TrimSpace(st.Fanout); f != "" {
		knobs = append(knobs, "fanout "+f)
	}
	if e := strings.TrimSpace(st.Effort); e != "" {
		knobs = append(knobs, wordThinking+" "+e)
	}
	when := strings.TrimSpace(st.When)
	if when == "" {
		when = "always"
	}
	knobs = append(knobs, "when "+when)
	return strings.Join(nonEmpty(knobs), rowSep)
}

// factoryStageTail is what the stage at views[at] has to say about its own
// state, at most room lines.
//
//	proof     the claim rows, then the policy rows
//	off       that it is switched off on this item
//	skipped   the condition it missed, or that the run went on past it
//	paused    how long it has been held
//	stopped   that a person stopped it and the branch is kept
//	pending   the stage it runs after
//	running   the spinner, its task count, round and time, then the stream's
//	          newest log lines
//	waiting   the question, then its keys
//	done      its result and what it took
//	failed    what failed
func (a *app) factoryStageTail(it factory.Item, views []factoryStageView, at, measure, room int) []string {
	if room <= 0 || at < 0 || at >= len(views) {
		return nil
	}
	v := views[at]
	pal := a.pal
	var out []string
	switch {
	case v.off:
		out = append(out, pal.dim(fit("switched off on this item", measure)))
	case v.skipped:
		out = append(out, pal.dim(fit("skipped · not "+strings.TrimSpace(v.stage.When), measure)))
	case v.kind == factoryMarkSkipped:
		out = append(out, pal.dim(fit("skipped · the run went on past it", measure)))
	case v.kind == factoryMarkPaused:
		line := strings.Join(nonEmpty([]string{a.factoryPausedFor(it), factoryStageCounts(v)}), rowSep)
		out = append(out, pal.muted(fit(line, measure)))
	case v.kind == factoryMarkStopped:
		out = append(out, pal.muted(fit(factoryStoppedWords, measure)))
	case v.state == factory.PhaseRunning:
		// THE PANE'S RUNNING LINE LEADS WITH THE SAME SPINNER THE RAIL WEARS
		// ([app.factoryStageMark]), turning, and its time counts up every
		// second ([app.factoryWantsSecondBeat]).
		mark, markPaint := a.factoryStageMark(v)
		line := strings.Join(nonEmpty([]string{factoryStageCounts(v), v.elapsed}), rowSep)
		out = append(out, markPaint(mark)+" "+pal.muted(fit(line, max(measure-factoryLeadW, 0))))
		if s := it.Stream; s != nil {
			log := s.Log
			if left := room - len(out); len(log) > left {
				log = log[len(log)-max(left, 0):]
			}
			for _, l := range log {
				out = append(out, a.factoryLogRow(l, measure))
			}
		}
	case v.state == factory.PhaseWaiting && factoryIsShaping(it) && it.State == factory.StateNeedsYou:
		out = append(out, a.factoryShapingBlock(it, measure, room)...)
	case v.state == factory.PhaseWaiting:
		q := strings.TrimSpace(it.Question)
		if q == "" {
			q = strings.TrimSpace(v.phase.Note)
		}
		if q != "" {
			out = append(out, a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), q, pal.ink, measure)...)
		}
		if a.factory.Has("answer") {
			out = append(out, pal.dim(fit(factoryAnswerClauses(), measure)))
		}
		if note := strings.TrimSpace(v.phase.Note); note != "" && note != q && note != strings.TrimSpace(v.stage.Ask) {
			out = append(out, pal.dim(fit(note, measure)))
		}
	case v.state == factory.PhaseDone:
		result := strings.TrimSpace(v.phase.Note)
		if result == "" {
			result = "done"
		}
		out = append(out, pal.muted(fit(strings.Join(nonEmpty([]string{result, factoryStageCounts(v)}), rowSep), measure)))
	case v.state == factory.PhaseFailed:
		out = append(out, pal.dim(fit(factoryOr(v.phase.Note, "failed"), measure)))
	default:
		after := "runs first"
		for i := at - 1; i >= 0; i-- {
			if !views[i].off && !views[i].skipped {
				after = "runs after " + views[i].stage.Name
				break
			}
		}
		out = append(out, pal.dim(fit(after, measure)))
	}
	if len(out) > room {
		out = out[:room]
	}
	return out
}

// factoryStageCounts is what is countable about a stage that ran: the tasks it
// split into and its round, and nothing when it has neither.
func factoryStageCounts(v factoryStageView) string {
	var parts []string
	if n := v.phase.Tasks; n > 0 {
		parts = append(parts, strconv.Itoa(n)+" "+factoryPlural(n, "task", "tasks"))
	}
	if r := factoryRoundWords(v.phase.Round, v.stage.Max); r != "" {
		parts = append(parts, "round "+r)
	}
	return strings.Join(parts, rowSep)
}
