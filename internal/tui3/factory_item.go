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
//	● plan              │ review · chat · until clean · max 2 · fanout per-finding · when always
//	● write ×3          │ read it as a stranger would
//	● test              │ ────────────────────────────────────────────────
//	◐ review 1/2        │ 1 task · round 1/2
//	○ neaten            │ 09:16 ◎ go test ./internal/tui3/ · 12/12
//	○ proof             │
//
// THE STATE LIVES ON a.fp (open, stage) AND NOWHERE ELSE, so the floor's
// own cursor, window and narrowings are untouched while the page stands over
// them. THE PAGE READS THE SNAPSHOT AND NOTHING ELSE, like the rest of the
// factory (the framedisk law).
//
// ITS KEYS ARE NAMED ON THE PLACE'S HINT LINE, not on a line of its own
// (place_factory.go's [app.factoryItemHint]): walking the stages, `enter` on
// one and `esc` are this file's; every verb is factory_keys.go's and keeps
// working on the item while the page is open. THE VERBS' ROWS, the typing row
// and the habit offer, stand at the bottom of the stage's pane, as they stand
// at the bottom of the peek on the floor.

// factoryStageCols is the stage rail's columns, the rule beside it not
// included.
const factoryStageCols = 22

// factoryStageFloor is the narrowest terminal that draws the stage rail beside
// the stage's pane. Under it the rail is one line of stages above the pane.
const factoryStageFloor = 72

// factoryStageNoteWords is what `enter` on a stage says until a stage's
// conversation can be walked into from here.
const factoryStageNoteWords = "the stage's conversation opens here once streams are conversations"

// factoryStageView is one stage as the item page draws it: the stage, how it
// stands on this item, and the phase that ran it when one has.
type factoryStageView struct {
	stage   factory.Stage
	state   factory.PhaseState // pending when nothing has run it
	phase   factory.Phase
	ran     bool // a phase of the stream stands behind it
	off     bool // switched off on this item
	skipped bool // its condition does not fit the item
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
				break
			}
		}
		out = append(out, v)
	}
	return out
}

// factoryStageFor is the stage the page opens on: the running stage of a
// running item, the waiting stage of an item that needs the person, the proof
// of a landed one, and the first stage of everything else.
func (a *app) factoryStageFor(it factory.Item) int {
	views := a.factoryItemStages(it)
	find := func(ok func(factoryStageView) bool) int {
		for i, v := range views {
			if ok(v) {
				return i
			}
		}
		return -1
	}
	at := -1
	switch it.State {
	case factory.StateRunning:
		at = find(func(v factoryStageView) bool { return v.state == factory.PhaseRunning })
	case factory.StateNeedsYou:
		at = find(func(v factoryStageView) bool { return v.state == factory.PhaseWaiting })
	case factory.StateLanded, factory.StateShipped:
		at = find(func(v factoryStageView) bool { return v.stage.Name == "proof" })
		if at < 0 {
			at = len(views) - 1
		}
	}
	return max(at, 0)
}

// factoryOpenItem is `enter` on a row: the item page opens over the floor on
// the stage [app.factoryStageFor] names. It answers false with no item under
// the cursor.
func (a *app) factoryOpenItem() bool {
	it, ok := a.factoryCursorItem()
	if !ok {
		return false
	}
	a.fp.open, a.fp.stage = true, a.factoryStageFor(it)
	a.pageMsg = ""
	a.touch()
	return true
}

// factoryCloseItem is `esc` on the item page: the floor comes back with its
// cursor on the row the page was opened from, because nothing moved it.
func (a *app) factoryCloseItem() {
	a.fp.open = false
	a.pageMsg = ""
	a.touch()
}

// factoryStageMove walks the stage rail's cursor by delta and lets go of the
// note the last stage said on the note line.
func (a *app) factoryStageMove(delta int) {
	it, ok := a.factoryCursorItem()
	if !ok {
		return
	}
	a.fp.stage = moveCursor(a.fp.stage, delta, len(a.factoryItemStages(it)))
	a.pageMsg = ""
	a.touch()
}

// factoryLayoutKey is the layout's own keys, read before the place's others:
// `z` turns the density, `enter` opens the item under the cursor, and while the
// item page is open the arrows walk its stages, `enter` says what a stage will
// open, and `esc` closes it. It answers false for every other key, which goes
// on to mean what it meant before.
//
// `ENTER` ON A FLOOR ROW OPENS THE ITEM PAGE AND NEVER LAUNCHES: launching is
// `r`, `p` and `L` (factory_keys.go). The one `enter` on the item page that
// acts is on the proof of a landed item, which IS the item's sheet, so the
// sheet's default key keeps its meaning there ([app.factoryLandedKey]): ship
// when every claim was shown, and the send-back row otherwise. Every other
// stage says, on the place's note line, what it will open.
//
// IT STANDS ASIDE for the map, the tab bar's cursor, the words box and a
// verb's typing row, each of which has the keyboard while it is up.
func (a *app) factoryLayoutKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.mapShowing || a.bar.on || a.fp.typing || a.fp.act.ask != nil || !a.factoryConnected() {
		return nil, false
	}
	k := msg.String()
	if a.fp.open {
		switch k {
		case "esc":
			a.factoryCloseItem()
		case "enter":
			// A SHEET WITH NO DOOR FOR ITS `enter` (the still fixture) says the
			// stage's note like any other stage, rather than nothing.
			if it, ok := a.factoryCursorItem(); ok && a.factoryOnProof(it) {
				a.pageMsg = ""
				if cmd, took := a.factoryLandedKey(it, "enter"); took {
					return cmd, true
				}
			}
			a.factorySay(factoryStageNoteWords)
		case "up", "ctrl+p", "left":
			a.factoryStageMove(-1)
		case "down", "ctrl+n", "right":
			a.factoryStageMove(1)
		default:
			return nil, false
		}
		return nil, true
	}
	switch k {
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

// factoryOnProof says whether the item page stands on the proof of a landed
// item, the one stage whose `enter` is the sheet's. It is the stage the page
// opens a landed item on ([app.factoryStageFor]): `proof`, or the last stage
// of a recipe that has none.
func (a *app) factoryOnProof(it factory.Item) bool {
	return a.fp.open && it.State == factory.StateLanded && a.fp.stage == a.factoryStageFor(it)
}

// ── drawing ─────────────────────────────────────────────────────────────────

// factoryItemBody is the item page as exactly room rows of exactly width
// cells: two head rows, a blank, and the stages beside the stage's pane (or
// one line of stages above it under [factoryStageFloor]). No row is a hit.
func (a *app) factoryItemBody(it factory.Item, width, room int) []placeRow {
	views := a.factoryItemStages(it)
	a.fp.stage = moveCursor(a.fp.stage, 0, len(views))
	measure := max(width-factoryPaneLead, 0)
	lines := []string{a.factoryItemTitle(it, measure), a.factoryItemChips(it, measure), ""}
	lead := strings.Repeat(" ", factoryPaneLead)
	for i, line := range lines {
		if line != "" {
			lines[i] = lead + line
		}
	}
	left := room - len(lines)
	switch {
	case left <= 0:
	case len(views) == 0:
	case width < factoryStageFloor:
		strip := lead + a.factoryStageStrip(views, measure)
		lines = append(lines, strip)
		for _, line := range a.factoryStagePane(it, views, measure, left-1) {
			lines = append(lines, factoryPad(lead+line, width))
		}
	default:
		paneW := width - factoryStageCols - 1
		rail := a.factoryStageRail(views, left)
		pane := a.factoryStagePane(it, views, max(paneW-factoryPaneLead, 0), left)
		sep := a.pal.dim(a.linearMark("│", "|"))
		for i := 0; i < left; i++ {
			right := ""
			if pane[i] != "" {
				right = lead + pane[i]
			}
			lines = append(lines, rail[i]+sep+factoryPad(right, paneW))
		}
	}
	rows := make([]placeRow, room)
	for i := range rows {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		rows[i] = placeRow{text: factoryPad(text, width), hit: -1}
	}
	return rows
}

// factoryItemTitle is the head's first row: the ref and the title in ink, the
// repo, the author and where the item stands with how long it has run, muted,
// and the spend over the cap right-aligned in money's ink.
func (a *app) factoryItemTitle(it factory.Item, measure int) string {
	pal := a.pal
	state := string(it.State)
	if s := it.Stream; s != nil {
		if e := factoryElapsed(s.Started, factoryEnd(s, a.fp.snap.Now)); e != "" {
			state += " " + e
		}
	}
	meta := nonEmpty([]string{factoryRepoShort(it.Repo), it.Author, state})
	left := pal.ink(it.Ref()+" "+it.Title) + pal.muted(rowSep+strings.Join(meta, rowSep))
	right := ""
	if spend := factorySpend(it.Stream, it.Cap); spend != "" {
		right = placeMoneyInk(pal)(spend)
	}
	return factorySpread(left, right, measure)
}

// factoryItemChips is the head's second row: the chips with the key that turns
// each, and the places the item touches.
func (a *app) factoryItemChips(it factory.Item, measure int) string {
	pal := a.pal
	places := it.Places
	if len(places) == 0 && it.Repo != "" {
		places = []string{it.Repo}
	}
	short := make([]string, 0, len(places))
	for _, p := range places {
		short = append(short, factoryRepoShort(p))
	}
	line := a.factoryChips(it, true)
	if len(short) > 0 {
		line += pal.dim(rowSep) + pal.muted("places:") + " " + pal.ink(strings.Join(short, ", "))
	}
	return fit(line, measure)
}

// factoryStageLabel is one stage as the rail names it, plain, and its paint:
// its mark and its name, the tasks it split into when done, its round over its
// most while running, and `· skipped` on a stage whose condition does not fit.
// A stage switched off or skipped is dim with the pending mark.
func (a *app) factoryStageLabel(v factoryStageView) (string, func(string) string) {
	pal := a.pal
	if v.off || v.skipped {
		label := a.factoryPendingMark() + " " + v.stage.Name
		if v.skipped {
			label += rowSep + "skipped"
		}
		return label, pal.dim
	}
	mark, _ := a.factoryPhaseMark(v.state)
	words := v.stage.Name
	if v.ran {
		words = factoryPhaseWords(factory.Phase{Name: v.stage.Name, State: v.state, Round: v.phase.Round, Tasks: v.phase.Tasks}, max(v.stage.Max, 1))
	} else if v.stage.Max > 1 {
		words += " ×" + strconv.Itoa(v.stage.Max)
	}
	paint := pal.muted
	switch v.state {
	case factory.PhaseRunning:
		paint = pal.ink
	case factory.PhasePending:
		paint = pal.dim
	}
	return mark + " " + words, paint
}

// factoryPendingMark is the pending stage's mark, the one an off stage wears.
func (a *app) factoryPendingMark() string {
	mark, _ := a.factoryPhaseMark(factory.PhasePending)
	return mark
}

// factoryStageRail is the stage rail: one row per stage, [factoryStageCols]
// wide, the window following the stage cursor, and the cursor's row on the
// cursor ground and nothing else changed on it. The mark carries the stage's
// colour; the name is ink while running and the reading tiers otherwise.
func (a *app) factoryStageRail(views []factoryStageView, room int) []string {
	pal := a.pal
	top := placeTop(0, a.fp.stage, len(views), room)
	out := make([]string, room)
	for i := range out {
		at := top + i
		if at >= len(views) {
			out[i] = strings.Repeat(" ", factoryStageCols)
			continue
		}
		v := views[at]
		label, paint := a.factoryStageLabel(v)
		mark, markPaint := a.factoryPhaseMark(v.state)
		text := ""
		if v.off || v.skipped {
			text = paint(fit(label, factoryStageCols-factoryPaneLead))
		} else {
			rest := strings.TrimPrefix(label, mark)
			text = markPaint(mark) + paint(fit(rest, factoryStageCols-factoryPaneLead-ansi.StringWidth(mark)))
		}
		row := factoryPad(strings.Repeat(" ", factoryPaneLead)+text, factoryStageCols)
		if at == a.fp.stage {
			row = pal.cursor(row, factoryStageCols)
		}
		out[i] = row
	}
	return out
}

// factoryStageStrip is the stage rail as one line, for a terminal under
// [factoryStageFloor]: the stages left to right, the cursor's on its ground,
// as many WHOLE stages as fit from the cursor's side.
func (a *app) factoryStageStrip(views []factoryStageView, measure int) string {
	var segs, plains []string
	for i, v := range views {
		label, paint := a.factoryStageLabel(v)
		seg := paint(label)
		if i == a.fp.stage {
			seg = a.pal.cursor(seg, ansi.StringWidth(label))
		}
		segs = append(segs, seg)
		plains = append(plains, label)
	}
	// THE CURSOR'S STAGE IS ALWAYS ON THE LINE: stages before it are dropped
	// from the left until it fits.
	from := 0
	for from < a.fp.stage {
		w := 0
		for i := from; i <= a.fp.stage; i++ {
			w += ansi.StringWidth(plains[i]) + 2
		}
		if w <= measure {
			break
		}
		from++
	}
	return factoryJoinWhole(segs[from:], plains[from:], "  ", measure)
}

// factoryStagePane is the stage under the rail's cursor as exactly room lines
// of at most measure cells: its knobs, its ask in ink, the rule, the tail its
// state draws, and the verbs' rows ([app.factoryFootRows]) on the last rows,
// so a typing row opened on the item page stands under the stage it is about.
func (a *app) factoryStagePane(it factory.Item, views []factoryStageView, measure, room int) []string {
	out := make([]string, max(room, 0))
	if room <= 0 || a.fp.stage >= len(views) {
		return out
	}
	pal := a.pal
	v := views[a.fp.stage]
	top := []string{
		pal.muted(fit(factoryKnobs(v.stage), measure)),
		pal.ink(fit(strings.TrimSpace(v.stage.Ask), measure)),
		pal.dim(strings.Repeat(a.linearMark("─", "-"), measure)),
	}
	foot := a.factoryFootRows(measure)
	if room <= len(foot) {
		copy(out, foot[len(foot)-room:])
		return out
	}
	body := room - len(foot)
	lines := append(top, a.factoryStageTail(it, views, v, measure, max(body-len(top), 0))...)
	for i := 0; i < body && i < len(lines); i++ {
		out[i] = lines[i]
	}
	copy(out[body:], foot)
	return out
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

// factoryStageTail is what a stage's own state draws under the rule, at most
// room lines.
//
//	proof     the claim rows, then the policy rows
//	off       that it is switched off on this item
//	skipped   the condition it missed
//	pending   the stage it runs after
//	running   its task count and round, then the stream's newest log lines
//	waiting   the question, then its keys
//	done      its result and what it took
//	failed    what failed
func (a *app) factoryStageTail(it factory.Item, views []factoryStageView, v factoryStageView, measure, room int) []string {
	if room <= 0 {
		return nil
	}
	pal := a.pal
	var out []string
	switch {
	case v.stage.Name == "proof" && len(it.Proof)+len(it.Policy) > 0:
		for _, c := range it.Proof {
			out = append(out, a.factoryClaimRow(c, "", measure))
		}
		for _, c := range it.Policy {
			out = append(out, a.factoryClaimRow(c, "policy", measure))
		}
	case v.off:
		out = append(out, pal.dim(fit("switched off on this item", measure)))
	case v.skipped:
		out = append(out, pal.dim(fit("skipped · not "+strings.TrimSpace(v.stage.When), measure)))
	case v.state == factory.PhaseRunning:
		if line := factoryStageCounts(v); line != "" {
			out = append(out, pal.muted(fit(line, measure)))
		}
		if s := it.Stream; s != nil {
			log := s.Log
			if left := room - len(out); len(log) > left {
				log = log[len(log)-max(left, 0):]
			}
			for _, l := range log {
				out = append(out, a.factoryLogLine(l, measure))
			}
		}
	case v.state == factory.PhaseWaiting:
		q := strings.TrimSpace(it.Question)
		if q == "" {
			q = strings.TrimSpace(v.phase.Note)
		}
		if q != "" {
			out = append(out, a.factoryLed(pal.ask(a.icon(tokens.GNeedsHuman)), q, pal.ask, measure)...)
		}
		out = append(out, pal.dim(fit(factoryAnswerKeys, measure)))
	case v.state == factory.PhaseDone:
		result := strings.TrimSpace(v.phase.Note)
		if result == "" {
			result = "done"
		}
		out = append(out, pal.muted(fit(strings.Join(nonEmpty([]string{result, factoryStageCounts(v)}), rowSep), measure)))
	case v.state == factory.PhaseFailed:
		out = append(out, pal.bad(fit(factoryOr(v.phase.Note, "failed"), measure)))
	default:
		after := "runs first"
		for i := a.fp.stage - 1; i >= 0; i-- {
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
	if r := v.phase.Round; r > 0 {
		parts = append(parts, "round "+strconv.Itoa(r)+"/"+strconv.Itoa(max(v.stage.Max, 1)))
	}
	return strings.Join(parts, rowSep)
}
