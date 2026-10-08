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

// factoryPageKind is what one row of the item page's rail stands for.
type factoryPageKind int

const (
	factoryPageIssue factoryPageKind = iota // the issue itself, always first
	factoryPageTalk                         // the item's conversation, when it has one
	factoryPageStage                        // one stage of its recipe
	factoryPageProof                        // the sheet, when no stage is named proof
	factoryPageLog                          // the stream's log, when it has one
)

// factoryPageRow is one row of the item page's rail: what it stands for, and
// for a stage, the stage as it stands on the item and where it is among them.
type factoryPageRow struct {
	kind factoryPageKind
	view factoryStageView
	at   int // the stage's place in [app.factoryItemStages]; -1 for the others
}

// factoryItemRows is the rail's rows top to bottom: the issue, the talk row
// when the item has a conversation, its stages, a `proof` row when the item
// carries a sheet and no stage is named proof (a recipe without one, or a
// send-back's `prove`), and `log` when its stream has said anything. A ROW
// WITH NOTHING BEHIND IT IS NOT ON THE RAIL: an item that never ran has no log
// row (the emptiness law).
func (a *app) factoryItemRows(it factory.Item) []factoryPageRow {
	rows := []factoryPageRow{{kind: factoryPageIssue, at: -1}}
	if strings.TrimSpace(it.Talk) != "" {
		rows = append(rows, factoryPageRow{kind: factoryPageTalk, at: -1})
	}
	proofStage := false
	for i, v := range a.factoryItemStages(it) {
		rows = append(rows, factoryPageRow{kind: factoryPageStage, view: v, at: i})
		proofStage = proofStage || v.stage.Name == "proof"
	}
	if !proofStage && len(it.Proof)+len(it.Policy) > 0 {
		rows = append(rows, factoryPageRow{kind: factoryPageProof, at: -1})
	}
	if it.Stream != nil && len(it.Stream.Log) > 0 {
		rows = append(rows, factoryPageRow{kind: factoryPageLog, at: -1})
	}
	return rows
}

// factoryIsProofRow says whether a rail row is the item's sheet: the stage
// named proof, or the proof row that stands in for one.
func factoryIsProofRow(r factoryPageRow) bool {
	return r.kind == factoryPageProof || (r.kind == factoryPageStage && r.view.stage.Name == "proof")
}

// factoryStageFor is the rail row the page opens on: the stage waiting on the
// person, else the running one, else the proof of a landed item (its last
// stage when the recipe has no proof), else the issue.
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
			if factoryIsProofRow(r) {
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
// `r`, `p` and `L` (factory_keys.go). The one `enter` on the item page that
// acts is on the proof of a landed item, which IS the item's sheet, so the
// sheet's default key keeps its meaning there ([app.factoryLandedKey]): sign
// off when every claim was shown, and the send-back row otherwise. A STAGE
// THAT RAN AS A CONVERSATION IS A ROOM, and `enter` walks into it
// (factory_run.go's [app.factoryOpenRoom]); every other stage says why it has
// none on the pane's action line. `enter` on the issue, the talk row and the
// log does nothing here.
//
// IT STANDS ASIDE for the map, the tab bar's cursor, the words box and a
// verb's typing row, each of which has the keyboard while it is up.
func (a *app) factoryLayoutKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.mapShowing || a.bar.on || a.fp.typing || a.fp.act.ask != nil || !a.factoryConnected() {
		return nil, false
	}
	k := msg.String()
	if took := a.factoryScrollKey(k); took {
		return nil, true
	}
	if a.fp.open {
		switch k {
		case "esc":
			a.factoryCloseItem()
		case "enter":
			return a.factoryItemEnter(), true
		case "up", "ctrl+p", "left":
			a.factoryStageMove(-1)
		case "down", "ctrl+n", "right":
			a.factoryStageMove(1)
		default:
			return nil, false
		}
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

// factoryItemEnter is `enter` on the item page, and a double press on a row of
// its rail: the proof of a landed item answers as its sheet, a stage with a
// room opens it, every other stage says why it has none, and the issue, the
// talk row and the log do nothing here.
func (a *app) factoryItemEnter() tea.Cmd {
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	rows := a.factoryItemRows(it)
	if a.fp.stage < 0 || a.fp.stage >= len(rows) {
		return nil
	}
	r := rows[a.fp.stage]
	if r.kind != factoryPageStage && r.kind != factoryPageProof {
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
	if r.kind != factoryPageStage {
		return nil
	}
	a.fp.said = true
	a.touch()
	return nil
}

// factoryItemPress is a press on the item page at screen cell (x, y), body row
// row: a press on a row of the rail selects it, and a second press on the same
// row is `enter` on it. Every other press on the page moves nothing.
func (a *app) factoryItemPress(x, y, row int) tea.Cmd {
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
	it, ok := a.factoryCursorItem()
	if !ok {
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
	it, ok := a.factoryCursorItem()
	if !ok || a.fp.pick != nil || a.fp.recipe != nil {
		return false
	}
	if a.fp.open {
		rows := a.factoryItemRows(it)
		if a.fp.stage < 0 || a.fp.stage >= len(rows) || rows[a.fp.stage].kind != factoryPageIssue {
			return true
		}
	}
	if a.fp.scrollID != it.ID {
		a.fp.scrollID, a.fp.scroll = it.ID, 0
	}
	next := max(min(a.fp.scroll+step, a.fp.scrollMax), 0)
	if next != a.fp.scroll {
		a.fp.scroll = next
		a.touch()
	}
	return true
}

// factoryOnProof says whether the item page stands on the proof of a landed
// item, the one row whose `enter` is the sheet's: `proof`, or the last stage of
// a recipe that has no sheet at all, which is the row the page opens a landed
// item on ([app.factoryStageFor]).
func (a *app) factoryOnProof(it factory.Item) bool {
	if !a.fp.open || it.State != factory.StateLanded {
		return false
	}
	rows := a.factoryItemRows(it)
	if a.fp.stage < 0 || a.fp.stage >= len(rows) {
		return false
	}
	r := rows[a.fp.stage]
	return factoryIsProofRow(r) || (r.kind == factoryPageStage && a.fp.stage == a.factoryStageFor(it))
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
	if second == "" {
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
	cells := a.factoryPageCells(rows)
	switch {
	case left <= 0:
	case width < factoryStageFloor:
		strip := lead + a.factoryCellStrip(cells, a.fp.stage, measure)
		a.fp.railTop, a.fp.railShown = len(lines), 1
		lines = append(lines, strip)
		for _, line := range a.factoryPagePane(it, rows, measure, left-1) {
			lines = append(lines, factoryPad(lead+line, width))
		}
	default:
		paneW := width - factoryRailW - 1
		rail, first := a.factoryCellRail(cells, a.fp.stage, left)
		a.fp.railTop, a.fp.railFirst, a.fp.railShown = len(lines), first, min(len(cells)-first, left)
		pane := a.factoryPagePane(it, rows, max(paneW-factoryMargins, 0), left)
		sep := a.pal.dim(a.linearMark("│", "|"))
		for i := 0; i < left; i++ {
			right := ""
			if pane[i] != "" {
				right = lead + pane[i]
			}
			lines = append(lines, rail[i]+sep+factoryPad(right, paneW))
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
	ref := it.Ref()
	left := a.factoryCrumbs(it.Repo) + a.factoryRefLink(it, pal.ink(ref)) + pal.ink(" "+it.Title)
	// ON A STAGE WITH A ROOM THE TRAIL GOES ONE CRUMB DEEPER, the way `enter`
	// will take: `Factory › codeaf › #12 › review`, the item's title after it,
	// muted. `esc` from the room climbs back to this page.
	if r, ok := a.factoryRoomRow(it); ok {
		sep := " " + a.linearMark("›", ">") + " "
		left = a.factoryCrumbs(it.Repo) + a.factoryRefLink(it, pal.ink(ref)) + pal.dim(sep) + pal.ink(r.view.stage.Name) + factorySpaces(factoryGutter) + pal.muted(it.Title)
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

// factoryCrumbs is the trail above a page on the floor, dim, up to the page's
// own name: `Factory › codeaf › `. A repo the page does not name is no crumb.
func (a *app) factoryCrumbs(repo string) string {
	sep := " " + a.linearMark("›", ">") + " "
	trail := "Factory" + sep
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
	pal := a.pal
	home := factoryRepoShort(it.Repo)
	var others []string
	for _, p := range it.Places {
		if short := factoryRepoShort(p); short != "" && short != home {
			others = append(others, short)
		}
	}
	line := a.factoryChipRow(it, true, measure)
	if len(others) > 0 {
		line += factorySpaces(factoryFactGap) + pal.dim("also "+strings.Join(others, ", "))
	}
	return fit(line, measure)
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
	label, paint := a.factoryStageLabel(v)
	if v.off || v.skipped || v.kind == factoryMarkSkipped {
		// A NAME TOO LONG FOR THE RAIL BESIDE ITS WORD KEEPS THE NAME WHOLE and
		// lets the word go: the stroke still says skipped, and `security ·
		// skip…` said less than `security` does.
		if ansi.StringWidth(label) > factoryRailW-factoryMargin {
			mark, _ := a.factoryKindMark(factoryMarkSkipped)
			label = mark + " " + v.stage.Name
		}
		return factoryRailCell{rest: label, paint: paint, markPaint: paint, flat: true}
	}
	mark, markPaint := a.factoryStageMark(v)
	c := factoryRailCell{mark: mark, markPaint: markPaint, rest: strings.TrimPrefix(label, mark), paint: paint}
	if v.ran && strings.TrimSpace(v.phase.Chat) != "" {
		room := " " + a.icon(tokens.GActionCommunicate)
		c.tail, c.tailPlain = a.pal.dim(room), room
	}
	if v.elapsed != "" {
		e := rowSep + v.elapsed
		if ansi.StringWidth(c.label())+ansi.StringWidth(e) <= factoryRailW-factoryMargin {
			c.tail, c.tailPlain = c.tail+a.pal.dim(e), c.tailPlain+e
		}
	}
	return c
}

// factoryPageCells is the item page's rail as cells: the issue with the
// document's mark, the talk row with the conversation's, and the stages.
func (a *app) factoryPageCells(rows []factoryPageRow) []factoryRailCell {
	pal := a.pal
	cells := make([]factoryRailCell, 0, len(rows))
	for _, r := range rows {
		switch r.kind {
		case factoryPageIssue:
			cells = append(cells, factoryRailCell{mark: a.icon(tokens.GFileDocument), markPaint: pal.muted, rest: " issue", paint: pal.ink})
		case factoryPageTalk:
			cells = append(cells, factoryRailCell{mark: a.icon(tokens.GActionCommunicate), markPaint: pal.muted, rest: " talk", paint: pal.ink})
		case factoryPageProof:
			cells = append(cells, factoryRailCell{mark: a.icon(tokens.GActionTest), markPaint: pal.muted, rest: " proof", paint: pal.ink})
		case factoryPageLog:
			cells = append(cells, factoryRailCell{mark: a.icon(tokens.GShell), markPaint: pal.muted, rest: " log", paint: pal.ink})
		default:
			cells = append(cells, a.factoryStageCell(r.view))
		}
	}
	return cells
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
		if c.flat {
			text = c.paint(fit(c.rest, factoryRailW-factoryMargin-tailW)) + c.tail
		} else {
			text = c.markPaint(c.mark) + c.paint(fit(c.rest, factoryRailW-factoryMargin-ansi.StringWidth(c.mark)-tailW)) + c.tail
		}
		row := factoryPad(factorySpaces(factoryMargin)+text, factoryRailW)
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
	case factoryPageTalk:
		lines = a.factoryTalkPane(it, measure, body)
	case factoryPageProof:
		lines = a.factoryProofPane(it, nil, measure, body)
	case factoryPageLog:
		lines = a.factoryLogPane(it, measure, body)
	default:
		views := a.factoryItemStages(it)
		lines = a.factoryStagePane(it, views, r.at, measure, body)
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
// verbs that work on it where it stands ([app.factoryVerbHint]), and nothing
// when there are none.
func (a *app) factoryPageAction(it factory.Item, measure int, extra ...string) string {
	words := append(append([]string{}, extra...), a.factoryVerbHint(it)...)
	if len(words) == 0 {
		return ""
	}
	return a.pal.dim(fit(strings.Join(words, " · "), measure))
}

// factoryIssuePane is the whole issue: its body wrapped at [factoryPageProseW],
// then the factory's read and the facts, then the questions it would put to
// the author when the item is thin — one document, scrolled with `J` and `K`
// from where they left it, its last row marked when more is below.
//
// THE BODY IS MARKDOWN, rendered (factory_forge.go's [app.factoryMarkdown]),
// and the forge's blocks follow the read, each comment whole. Over the read
// stands the line that says when it was made, `read 3m ago · u again`, which
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
	var facts []string
	if f := factoryFacts(it); len(f) > 0 {
		facts = []string{pal.dim(factoryJoinWhole(f, f, factorySpaces(factoryFactGap), measure))}
	}
	var asks []string
	if r := it.Triage.Readiness; r > 0 && r < factory.ThinReadiness {
		if qs := nonEmpty(it.Triage.Questions); len(qs) > 0 {
			for _, line := range wrap("it would ask "+factoryOr(it.Author, "the author")+" "+strings.Join(qs, " / "), w) {
				asks = append(asks, pal.muted(line))
			}
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
		extra = append(extra, "J K scroll")
	}
	return factoryPaneLadder([][]string{shown}, a.factoryPageAction(it, measure, extra...), room)
}

// factoryTalkPane is the item's conversation as the item page shows it: its
// row, and the item's keys.
func (a *app) factoryTalkPane(it factory.Item, measure, room int) []string {
	return factoryPaneLadder([][]string{{a.factoryTalkRow(it, measure)}}, a.factoryPageAction(it, measure), room)
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
	action := a.factoryPageAction(it, measure)
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
		knobs = append(knobs, "effort "+e)
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
//	running   its task count and round, then the stream's newest log lines
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
		if line := strings.Join(nonEmpty([]string{factoryStageCounts(v), v.elapsed}), rowSep); line != "" {
			out = append(out, pal.muted(fit(line, measure)))
		}
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
