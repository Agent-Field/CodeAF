package tui3

import (
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
// page is the issue's map").
type factoryPageKind int

const (
	factoryPageIssue    factoryPageKind = iota // what it says and what codeaf read, always first
	factoryPageManager                         // the item's conversation, where the seam has the Talk door
	factoryPageRun                             // the run, with its stages and its log nested under it
	factoryPageStage                           // one stage of its recipe, under run
	factoryPageLog                             // the stream's log, last under run, when it has one
	factoryPageResult                          // the sheet, the diff and the checks, once something came out
	factoryPageSettings                        // ask me at, thinking, budget and the stages on or off, always last
)

// factoryPageRow is one row of the item page's left column: what it stands
// for, and for a stage, the stage as it stands on the item and where it is
// among them.
type factoryPageRow struct {
	kind factoryPageKind
	view factoryStageView
	at   int // the stage's place in [app.factoryItemStages]; -1 for the others
}

// factoryItemRows is the left column's rows top to bottom:
//
//	issue              always
//	manager            the item's conversation, where the seam has the Talk door
//	run                the run, where there are stages or a stream
//	  ✓ plan  2m       each stage, nested under run
//	  log              the stream's log, nested last, once it has said anything
//	result             the sheet, the diff and the checks, once something came out
//	settings           always
//
// A ROW WITH NOTHING BEHIND IT IS NOT IN THE COLUMN (the emptiness law): an
// item that never ran has no log row and no result row.
func (a *app) factoryItemRows(it factory.Item) []factoryPageRow {
	rows := []factoryPageRow{{kind: factoryPageIssue, at: -1}}
	if a.factory.Has("talk") {
		rows = append(rows, factoryPageRow{kind: factoryPageManager, at: -1})
	}
	stages := a.factoryItemStages(it)
	if len(stages) > 0 || it.Stream != nil {
		rows = append(rows, factoryPageRow{kind: factoryPageRun, at: -1})
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
	return append(rows, factoryPageRow{kind: factoryPageSettings, at: -1})
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

// factoryOnTimeline says whether a row's center is the run's story
// (factory_timeline.go): the manager, the run and every stage.
func factoryOnTimeline(r factoryPageRow) bool {
	return r.kind == factoryPageManager || r.kind == factoryPageRun || r.kind == factoryPageStage
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
// answers false with no item under the cursor.
func (a *app) factoryOpenItem() bool {
	it, ok := a.factoryCursorItem()
	if !ok {
		return false
	}
	a.fp.open, a.fp.stage, a.fp.said = true, a.factoryStageFor(it), false
	a.fp.scroll, a.fp.scrollID = 0, it.ID
	a.pageMsg = ""
	a.touch()
	return true
}

// factoryCloseItem is `esc` on the item page: the floor comes back with its
// cursor on the row the page was opened from, because nothing moved it.
func (a *app) factoryCloseItem() {
	a.fp.open, a.fp.said = false, false
	a.fp.crumbHover = factoryCrumbNone
	a.fp.scroll = 0
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
	a.pageMsg = ""
	a.factoryTLFollowRow()
	a.touch()
}

// factoryLayoutKey is the layout's own keys, read before the place's others:
// `z` turns the density, `enter` opens the item under the cursor, `{` `}` and
// `|` move the divider (factory_split.go), `J` and `K` (and `pgdn` and `pgup`)
// scroll the item's body; and while the item page is open the arrows walk its
// rail, `enter` walks into a stage's conversation (or says why it has none),
// and `esc` closes it. It answers false for every other key, which goes on to mean
// what it meant before.
//
// `ENTER` ON A FLOOR ROW OPENS THE ITEM PAGE AND NEVER LAUNCHES: launching is
// `r`, `p` and `L` (factory_keys.go). On the item page `enter` on the issue
// and on the manager row opens the item's own conversation, as `T` does; on a
// stage it is the run's story's first (factory_timeline.go), and then a
// stage that ran as a conversation is a ROOM it walks into (factory_run.go's
// [app.factoryOpenRoom]), the proof stage of a landed item answers as the
// sheet ([app.factoryLandedKey]), and every other stage says why it has none
// on the note line. `enter` on the log, the result and the settings does
// nothing ([app.factoryItemEnter]).
//
// IT STANDS ASIDE for the map, the tab bar's cursor, the words box and a
// verb's typing row, each of which has the keyboard while it is up.
func (a *app) factoryLayoutKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.mapShowing || a.bar.on || a.fp.typing || a.fp.act.ask != nil || !a.factoryConnected() {
		return nil, false
	}
	k := msg.String()
	if a.fp.open {
		// A DIVE INTO A STAGE hears `esc` first: it climbs back to the story
		// (factory_timeline.go), and only the story's `esc` leaves the page.
		if a.fp.tl.diving && k == "esc" {
			if it, ok := a.factoryCursorItem(); ok {
				if cmd, took := a.factoryTimelineKey(it, msg); took {
					return cmd, true
				}
			}
		}
		switch k {
		case "esc":
			a.factoryCloseItem()
			return nil, true
		case "up", "ctrl+p", "left":
			a.factoryStageMove(-1)
			return nil, true
		case "down", "ctrl+n", "right":
			a.factoryStageMove(1)
			return nil, true
		}
		// `TAB` REACHES THE MANAGER'S BOX from the story (factory_timeline.go):
		// on a row whose center is the run's story the box is the place after
		// the center, and the next `tab`, from inside the box, walks on to
		// the next place as it always did.
		if k == "tab" {
			if it, ok := a.factoryCursorItem(); ok {
				if r, ok := a.factoryPageRowAt(it); ok && factoryOnTimeline(r) && a.factory.Has("talk") && !a.fp.tl.diving {
					a.factoryTLOpenBox(it)
					return nil, true
				}
			}
			return nil, false
		}
		// THE RUN'S STORY HEARS A KEY FIRST on the rows whose center it is
		// (factory_timeline.go), the walk and `esc` excepted, and a key it
		// does not take goes on to mean what it meant. `enter` asks it in
		// [app.factoryItemEnter], so a double press asks it the same way.
		if it, ok := a.factoryCursorItem(); ok && k != "enter" {
			if r, ok := a.factoryPageRowAt(it); ok && factoryOnTimeline(r) {
				if cmd, took := a.factoryTimelineKey(it, msg); took {
					return cmd, true
				}
			}
		}
		if k == "enter" {
			return a.factoryItemEnter(), true
		}
		if took := a.factoryScrollKey(k); took {
			return nil, true
		}
		return nil, false
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
		return nil, a.factoryOpenItem()
	}
	return nil, false
}

// factoryIssueDoor is what `enter` on the issue row does on this seam.
type factoryIssueDoor int

const (
	factoryIssueNone  factoryIssueDoor = iota // nothing to open yet
	factoryIssueTalk                          // the item's own conversation, as `T`
	factoryIssueForge                         // the item's page on github, as `g`
)

// factoryIssueEnter is the door `enter` on the issue row opens: the Talk door
// when the seam has it, else `g`'s door when the item has a page on its forge,
// else none. The hint and the key both ask it, so they cannot disagree.
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

// factoryItemEnter is `enter` on the item page, and a double press on a row of
// its left column: the issue opens the item's own conversation and the
// manager row puts the keys in the manager's box (factory_timeline.go); on
// the run and on a stage the run's story is asked first
// (factory_timeline.go); then the proof stage of a landed item answers as its
// sheet, a stage with a room opens it, and every other stage says why it has
// none. The log, the result and the settings do nothing here.
func (a *app) factoryItemEnter() tea.Cmd {
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	r, ok := a.factoryPageRowAt(it)
	if !ok {
		return nil
	}
	// `ENTER` ON THE MANAGER ROW PUTS THE KEYS IN THE MANAGER'S BOX
	// (owner decision, 2026-10-08): a person who starts typing to the
	// manager types, and `T chat` stays the way to the whole conversation.
	if r.kind == factoryPageManager && a.factory.Has("talk") {
		a.pageMsg = ""
		a.factoryTLOpenBox(it)
		return nil
	}
	// `ENTER` ON THE ISSUE OPENS THE ITEM'S OWN CONVERSATION, exactly as `T`
	// does (factory_talk.go): a new item is started by talking it through,
	// and `enter` is the key a person tries first. With no Talk door it opens
	// the item on github through `g`'s door, and with neither the action line
	// says there is nothing to open yet.
	if r.kind == factoryPageIssue || r.kind == factoryPageManager {
		switch a.factoryIssueEnter(it) {
		case factoryIssueTalk:
			a.pageMsg = ""
			return a.factoryTalk(it)
		case factoryIssueForge:
			a.pageMsg = ""
			return a.factoryOpenForge(it)
		}
		a.fp.said = true
		a.touch()
		return nil
	}
	if r.kind == factoryPageRun || r.kind == factoryPageStage {
		if cmd, took := a.factoryTimelineKey(it, tea.KeyPressMsg{Code: tea.KeyEnter}); took {
			return cmd
		}
	}
	if r.kind != factoryPageStage {
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
		a.pageMsg = ""
		return a.factoryOpenRoom(it, r.view)
	}
	// `ENTER` ON A STAGE WITH NO ROOM SAYS WHY on the pane's last line
	// ([app.factoryPagePane]).
	a.fp.said = true
	a.touch()
	return nil
}

// factoryItemPress is a press on the item page at screen cell (x, y), body row
// row: a press on the center of a row whose center is the run's story is the
// story's (factory_timeline.go), a press on a row of the left column selects
// it, and a second press on the same row is `enter` on it. Every other press
// on the page moves nothing.
func (a *app) factoryItemPress(x, y, row int) tea.Cmd {
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	if r, ok := a.factoryPageRowAt(it); ok && factoryOnTimeline(r) {
		if px, py, ok := a.factoryPaneAt(x, row); ok {
			cmd, _ := a.factoryTimelinePress(it, px, py)
			return cmd
		}
	}
	at := row - a.fp.railTop
	if at < 0 || at >= a.fp.railShown {
		return nil
	}
	if a.fp.bodyW >= factoryStageFloor && x >= factoryRailW {
		return nil
	}
	// Under the stage floor the rail is one line; a press on it selects
	// nothing, because which word is where is the line's own arithmetic.
	if a.fp.bodyW < factoryStageFloor {
		return nil
	}
	pick := a.fp.railFirst + at
	if pick >= len(a.factoryItemRows(it)) {
		return nil
	}
	again := pick == a.fp.stage
	a.factoryStageSelect(pick)
	if a.countClick(x, y) >= 2 && again {
		return a.factoryItemEnter()
	}
	return nil
}

// factoryPaneAt is screen column x on body row row as the center pane's own
// cell, counted from the pane's first cell past its margin and its first
// line, and false off the pane: on the left column, its rule, the verbs'
// column or the head. Under [factoryStageFloor] the pane is the rows under
// the left column's one line.
func (a *app) factoryPaneAt(x, row int) (int, int, bool) {
	if a.fp.railShown == 0 {
		return 0, 0, false
	}
	top, left := a.fp.railTop, factoryRailW+factoryRuleW+factoryMargin
	if a.fp.bodyW < factoryStageFloor {
		top, left = a.fp.railTop+1, factoryMargin
	}
	py := row - top
	if py < 0 || row >= a.fp.pageRows || x < left {
		return 0, 0, false
	}
	if a.factoryVerbsDrawn() && x >= a.fp.verbX-factoryRuleW {
		return 0, 0, false
	}
	return x - left, py, true
}

// factoryPaneHover is the pointer resting at (x, y) over the center of a row
// whose center is the run's story: the story's to answer.
func (a *app) factoryPaneHover(x, y int) bool {
	it, ok := a.factoryCursorItem()
	if !ok {
		return false
	}
	r, ok := a.factoryPageRowAt(it)
	if !ok || !factoryOnTimeline(r) {
		return false
	}
	px, py, ok := a.factoryPaneAt(x, y-placeHeadRows)
	return ok && a.factoryTimelineHover(it, px, py)
}

// factoryStageHover is the pointer resting at (x, y) on the item page: over
// the stage rail it selects the stage under it, as a press does without the
// second press's walk in ([app.factoryItemPress]), and anywhere else it moves
// nothing. It reports whether the rail took the motion.
func (a *app) factoryStageHover(x, y int) bool {
	at := y - placeHeadRows - a.fp.railTop
	if at < 0 || at >= a.fp.railShown || a.fp.bodyW < factoryStageFloor || x >= factoryRailW {
		return false
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return false
	}
	pick := a.fp.railFirst + at
	if pick >= len(a.factoryItemRows(it)) {
		return false
	}
	if pick != a.fp.stage {
		a.factoryStageSelect(pick)
	}
	return true
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
// cells: two head rows (three when plan changed the stages), a blank, and the
// rail beside the pane of the row under its cursor (or one line of the rail
// above it under [factoryStageFloor]). No row is a hit; a press is read
// against the rail the draw placed ([app.factoryItemPress]).
func (a *app) factoryItemBody(it factory.Item, width, room int) []placeRow {
	rows := a.factoryItemRows(it)
	a.fp.stage = moveCursor(a.fp.stage, 0, len(rows))
	a.fp.pageRows, a.fp.bodyW = room, width
	a.fp.railTop, a.fp.railFirst, a.fp.railShown = 0, 0, 0
	a.fp.verbX, a.fp.verbHits = 0, nil
	// THE VERBS STAND ON THE RIGHT on a page wide enough for them
	// (factory_verbs.go), and the chips they carry leave the head.
	// AN ITEM WITH NO VERB AT ALL DRAWS NO COLUMN (the emptiness law), and
	// its page is drawn as a narrow one is.
	verbs := width >= factoryVerbRailMinW && width >= factoryStageFloor && len(a.factoryVerbGroups(it)) > 0
	// THE PAGE STOPS [factoryMargin] BEFORE THE FRAME'S EDGE (factory_grid.go's
	// THE RIGHT MARGIN), the head and the pane alike.
	measure := max(width-factoryMargins, 0)
	// THE HEAD IS TWO ROWS AND A BLANK ON EVERY ITEM (owner ruling,
	// 2026-10-08): the crumbs on row 0, the gate, cap and effort on row 1, and the rail and
	// its pane from row 3, so the page's regions start on the same rows
	// whatever the item. What the plan stage changed is the issue pane's,
	// beside the stages it changed.
	// A PARKED ITEM'S SECOND ROW IS ITS QUESTION (factory_run.go's
	// [app.factoryItemQuestion]), where the gate, cap and effort stand on every other item.
	second := a.factoryItemQuestion(it, measure)
	switch {
	case second != "":
	case verbs:
		second = fit(a.factoryItemOthers(it), measure)
	default:
		second = a.factoryItemChips(it, measure)
	}
	lines := []string{a.factoryItemTitle(it, measure), second, ""}
	lead := factoryMarginPad()
	for i, line := range lines {
		if line != "" {
			lines[i] = lead + line
		}
	}
	left := room - len(lines)
	cells := a.factoryPageCells(it, rows)
	switch {
	case left <= 0:
	case width < factoryStageFloor:
		strip := lead + a.factoryCellStrip(a.factoryPageCellsIn(it, rows, false), a.fp.stage, measure)
		a.fp.railTop, a.fp.railShown = len(lines), 1
		lines = append(lines, strip)
		for _, line := range a.factoryPagePane(it, rows, measure, left-1) {
			if a.factoryHasMore(line) {
				a.fp.moreRow = len(lines)
			}
			lines = append(lines, factoryPad(lead+line, width))
		}
	default:
		paneW := width - factoryRailW - factoryRuleW
		var verbLines []string
		if verbs {
			// THE VERBS' COLUMN TAKES ITS WIDTH, ITS RULE AND THE PAGE'S RIGHT
			// MARGIN FROM THE PANE, and is placed before the pane is drawn,
			// so the pane knows to leave its action line to the column.
			paneW -= factoryRuleW + factoryVerbRailW + factoryMargin
			a.fp.verbX = factoryRailW + factoryRuleW + paneW + factoryRuleW
			var hits []factoryVerbHit
			verbLines, hits = a.factoryVerbLines(it, left)
			for _, h := range hits {
				a.fp.verbHits = append(a.fp.verbHits, factoryVerbHit{row: len(lines) + h.row, verb: h.verb})
			}
		}
		rail, first := a.factoryCellRail(cells, a.fp.stage, left)
		a.fp.railTop, a.fp.railFirst, a.fp.railShown = len(lines), first, min(len(cells)-first, left)
		pane := a.factoryPagePane(it, rows, max(paneW-factoryMargins, 0), left)
		sep := a.pal.dim(a.linearMark("│", "|"))
		for i := 0; i < left; i++ {
			right := ""
			if pane[i] != "" {
				right = lead + pane[i]
			}
			if a.factoryHasMore(right) {
				a.fp.moreRow = len(lines)
			}
			line := rail[i] + sep + factoryPad(right, paneW)
			if verbs {
				verb := ""
				if i < len(verbLines) {
					verb = verbLines[i]
				}
				line += sep + factoryPad(verb, factoryVerbRailW)
			}
			lines = append(lines, line)
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

// factoryItemTitle is the head's first row, a TRAIL OF CRUMBS (owner ruling,
// 2026-10-08): `Factory › codeaf › #1551 filters lost on compact`, the crumbs
// dim and the item's own ref and title in ink, and at the right where the item
// stands with how long it has run, muted, and the spend over the cap in
// money's ink. `esc` climbs one crumb, back to the floor.
//
// THE CRUMBS ARE BUTTONS ([app.factoryCrumbPress]): `Factory` is the floor,
// the repo the floor narrowed to that repo, and the ref the item on github
// where `g` has a door. Where each stands is kept for the pointer
// ([factoryPage.crumbHits]), the page's left margin included.
func (a *app) factoryItemTitle(it factory.Item, measure int) string {
	pal := a.pal
	state := factoryStateWord(it)
	if factoryPaused(it) {
		state = a.factoryPausedFor(it)
	} else if s := it.Stream; s != nil {
		if e := factoryElapsed(s.Started, factoryEnd(s, a.fp.snap.Now)); e != "" {
			state += " " + e
		}
	}
	trail := a.factoryItemCrumbs(it, measure)
	left := trail + pal.ink(" "+it.Title)
	// ON A STAGE WITH A ROOM THE TRAIL GOES ONE CRUMB DEEPER, the way `enter`
	// will take: `Factory › codeaf › #12 › review`, the item's title after it,
	// muted. `esc` from the room climbs back to this page.
	if r, ok := a.factoryRoomRow(it); ok {
		sep := " " + a.linearMark("›", ">") + " "
		left = trail + pal.dim(sep) + pal.ink(r.view.stage.Name) + factorySpaces(factoryGutter) + pal.muted(it.Title)
	}
	// THE MONEY AT THE RIGHT IS SPEND OVER THE CAP, so an item with no stream
	// draws none: its cap is on the row under it, and saying it twice
	// is a second number to read for one fact.
	right := pal.muted(state)
	if spend := factorySpend(it.Stream, it.Cap); spend != "" && it.Stream != nil {
		right += factorySpaces(factoryGutter) + placeMoneyInk(pal)(spend)
	}
	return factorySpread(left, right, measure)
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

// factoryItemCrumbs is the trail up to the item's ref, painted, with each
// crumb's cells kept for the pointer: `Factory › codeaf › #1551`. The crumb
// the pointer rests on wears the pointer's ground, as the verbs' rows do. THE
// REF IS A BUTTON ONLY WHERE `g` OPENS IT ([app.factoryRefOpens]), and a repo
// the trail does not name is no crumb.
func (a *app) factoryItemCrumbs(it factory.Item, measure int) string {
	pal := a.pal
	sep := " " + a.linearMark("›", ">") + " "
	a.fp.crumbHits = a.fp.crumbHits[:0]
	x := factoryMargin
	add := func(word string, crumb factoryCrumb, paint func(string) string) string {
		w := ansi.StringWidth(word)
		if crumb != factoryCrumbNone && x-factoryMargin < measure {
			a.fp.crumbHits = append(a.fp.crumbHits, factoryCrumbHit{x0: x, x1: min(x+w, factoryMargin+measure), crumb: crumb})
		}
		x += w
		if crumb != factoryCrumbNone && crumb == a.fp.crumbHover {
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

// factoryCrumbAt is the crumb the last draw put at screen column x on body
// row row, and none off the trail.
func (a *app) factoryCrumbAt(x, row int) factoryCrumb {
	if !a.fp.open || row != 0 {
		return factoryCrumbNone
	}
	for _, h := range a.fp.crumbHits {
		if x >= h.x0 && x < h.x1 {
			return h.crumb
		}
	}
	return factoryCrumbNone
}

// factoryCrumbHover is the pointer resting at (x, y): the crumb under it
// takes the pointer's ground and every other gives it up. It reports whether
// the pointer is on a crumb.
func (a *app) factoryCrumbHover(x, y int) bool {
	next := a.factoryCrumbAt(x, y-placeHeadRows)
	if next != a.fp.crumbHover {
		a.fp.crumbHover = next
		a.touch()
	}
	return next != factoryCrumbNone
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
		case factoryPageRun:
			c := facet(wordFacetRun, 0)
			if facts := a.factoryRunFacts(it, factoryRailW-factoryMargin-ansi.StringWidth(wordFacetRun)-factoryGutter); facts != "" {
				c.tail, c.tailPlain = factorySpaces(factoryGutter)+pal.dim(facts), factorySpaces(factoryGutter)+facts
			}
			cells = append(cells, c)
		case factoryPageLog:
			cells = append(cells, facet(wordFacetLog, factoryNestW))
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

// factoryStageRowCell is the stage at as a row of the item page's rail: its
// mark, its one-word name, and after it its loop in columns, each cell of
// air apart (owner decision, 2026-10-08, "the left column shows the loop of
// each stage in one word each"):
//
//	⠋ review   1/2 ?
//	○ arch         +
//
// the rounds right-aligned in [factoryStageRoundsW], then the one glyph in
// [factoryStageGlyphW]. The conversation's mark and a running stage's elapsed
// follow the name where they fit whole, and are the first things a narrow
// rail drops; THE NAME IS CUT ONLY AS A LAST RESORT. A stage that will not
// run keeps its one dim line, `⊘ neaten · skipped`.
func (a *app) factoryStageRowCell(it factory.Item, views []factoryStageView, at int, cols factoryStageCols) factoryRailCell {
	v := views[at]
	if factoryStageOut(v) {
		return a.factoryStageCellIn(v, factoryNestW)
	}
	room := factoryRailW - factoryMargin - factoryNestW
	mark, markPaint := a.factoryStageMark(v)
	_, paint := a.factoryStageLabel(v)
	rounds := factoryStageRounds(v)
	glyph, glyphPaint := a.factoryStageGlyph(it, views, at)
	var tail, tailPlain string
	add := func(painted, plain string) {
		tail, tailPlain = tail+" "+painted, tailPlain+" "+plain
	}
	switch {
	case cols.grid:
		// A ROW WITH NO ROUNDS LENDS ITS ROUNDS CELL TO THE NAME, so a
		// one-round stage keeps its elapsed beside it; the glyph column
		// stands in one cell on every row either way.
		if cols.rounds > 0 && rounds != "" {
			cell := factorySpaces(cols.rounds-ansi.StringWidth(rounds)) + rounds
			add(paint(cell), cell)
		}
		if cols.glyph {
			cell := glyph + factorySpaces(factoryStageGlyphW-ansi.StringWidth(glyph))
			add(glyphPaint(cell), cell)
		}
	default:
		if rounds != "" {
			add(paint(rounds), rounds)
		}
		if glyph != "" {
			add(glyphPaint(glyph), glyph)
		}
	}
	// THE NAME'S ROOM is what the mark, its space and the columns leave, a
	// lent rounds cell included.
	nameW := max(room-ansi.StringWidth(mark)-1-ansi.StringWidth(tailPlain), 0)
	words := v.stage.Name
	extra := func(w string) {
		if ansi.StringWidth(words)+ansi.StringWidth(w) <= nameW {
			words += w
		}
	}
	switch v.kind {
	case factoryMarkPaused:
		extra(rowSep + "paused")
	case factoryMarkStopped:
		extra(rowSep + "stopped")
	}
	plainWords := words
	var after string
	if v.ran && strings.TrimSpace(v.phase.Chat) != "" {
		w := " " + a.icon(tokens.GActionCommunicate)
		if ansi.StringWidth(plainWords)+ansi.StringWidth(w) <= nameW {
			plainWords, after = plainWords+w, after+a.pal.dim(w)
		}
	}
	if v.elapsed != "" {
		w := rowSep + v.elapsed
		if ansi.StringWidth(plainWords)+ansi.StringWidth(w) <= nameW {
			plainWords, after = plainWords+w, after+a.pal.dim(w)
		}
	}
	if !cols.grid {
		return factoryRailCell{mark: mark, markPaint: markPaint, rest: " " + words, paint: paint, tail: after + tail, tailPlain: strings.TrimPrefix(plainWords, words) + tailPlain, indent: factoryNestW}
	}
	pad := factorySpaces(max(nameW-ansi.StringWidth(plainWords), 0))
	return factoryRailCell{mark: mark, markPaint: markPaint, rest: " " + fit(words, nameW), paint: paint, tail: after + pad + tail, tailPlain: strings.TrimPrefix(plainWords, words) + pad + tailPlain, indent: factoryNestW}
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

// factoryPagePane is the pane of the rail row under the cursor as exactly room
// lines of at most measure cells, the verbs' rows ([app.factoryFootRows]) on
// its last lines, so a typing row opened on the item page stands under what
// it is about.
func (a *app) factoryPagePane(it factory.Item, rows []factoryPageRow, measure, room int) []string {
	out := make([]string, max(room, 0))
	if room <= 0 || a.fp.stage >= len(rows) {
		return out
	}
	foot := a.factoryFootRows(measure)
	if room <= len(foot) {
		copy(out, foot[len(foot)-room:])
		return out
	}
	body := room - len(foot)
	r := rows[a.fp.stage]
	var lines []string
	switch r.kind {
	case factoryPageIssue:
		lines = a.factoryIssuePane(it, measure, body)
	case factoryPageLog:
		lines = a.factoryLogPane(it, measure, body)
	case factoryPageResult:
		lines = a.factoryResultPane(it, measure, body)
	case factoryPageSettings:
		lines = a.factorySettingsPane(it, measure, body)
	default:
		// THE MANAGER, THE RUN AND EVERY STAGE ARE THE RUN'S STORY
		// (factory_timeline.go), which reads the row under the cursor itself.
		// `ENTER` ON A STAGE WITH NO ROOM SAYS WHY on the pane's last line,
		// where the item's keys stand, until the cursor moves or another key
		// is pressed.
		if r.kind == factoryPageStage && a.fp.said && strings.TrimSpace(r.view.phase.Chat) == "" && body >= factoryActionRows {
			why := a.pal.dim(fit(factoryNoRoomWords(r.view), measure))
			lines = factoryPaneLadder([][]string{a.factoryTimelinePane(it, measure, body-factoryActionRows)}, why, body)
		} else {
			lines = a.factoryTimelinePane(it, measure, body)
		}
	}
	copy(out, lines)
	copy(out[body:], foot)
	return out
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
// item's keys ([app.factoryPageAction]), and NOTHING WHILE THE VERBS STAND ON
// THE RIGHT (factory_verbs.go), because the column says them and the pane
// does not say them a second time. Under [factoryVerbRailMinW] the line is
// back.
func (a *app) factoryPaneAction(it factory.Item, measure int, extra ...string) string {
	if a.factoryVerbsDrawn() {
		return ""
	}
	return a.factoryPageAction(it, measure, extra...)
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
	case v.state == factory.PhaseWaiting:
		q := strings.TrimSpace(it.Question)
		if q == "" {
			q = strings.TrimSpace(v.phase.Note)
		}
		if q != "" {
			out = append(out, a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), q, pal.ink, measure)...)
		}
		if a.factory.Has("answer") {
			out = append(out, pal.dim(fit(factoryAnswerKeys, measure)))
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
