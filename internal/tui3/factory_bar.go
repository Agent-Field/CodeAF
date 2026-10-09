package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE ITEM PAGE'S TOP BAR ─────────────────────────────────────────────────
//
// The owner's layout (2026-10-09): one bar across the whole page, over the
// left column and the center alike.
//
//	▶ run   Factory › codeaf › #1551 filters lost on compact      review · round 1/2 · $1.42 / $5
//	? plan is ready · go, or change it?                            y yes · n no · a in words
//
// ITS ONE CONTROL says what a press does where the run stands
// ([app.factoryControlOf]): `run` on an item that has not run, and on a run
// that is paused; `pause` on a run that is moving; `continue` on a run held at
// an approve step, a step of kind gate (`a person`) waiting on you. It is a
// button (a press is the key) and a key, `space`, from anywhere on the page
// while the keys walk it. An item the control can do nothing for draws none
// (the emptiness law): a queued item, a landed one, a question that is not a
// hold.
//
// THE CRUMBS FOLLOW IT, each a button as before (factory_item.go), and AT THE
// RIGHT the step the run stands on, its round, and the spend over the cap.
//
// UNDER IT, ONLY WHILE THEY HAVE SOMETHING TO SAY: the question the run waits
// on with the keys that answer it, then the verbs' typing row or an offer,
// then one blank row. These are the bar's rows, and the left column and the
// center start under them on every item.

// factoryControl is what the top bar's control does now.
type factoryControl int

const (
	factoryControlNone     factoryControl = iota
	factoryControlRun                     // start the run
	factoryControlPause                   // hold a moving run
	factoryControlResume                  // go on with a paused run, in the same chat
	factoryControlContinue                // go past the approve step the run is held at
)

// factoryHeldAt is the phase of item it held at a step that waits for a
// person (a gate step: the approve step), and -1 when the run is held at
// none. It reads the phase's own kind, and the recipe's stage of that name
// where the phase does not carry one.
func (a *app) factoryHeldAt(it factory.Item) int {
	s := it.Stream
	if s == nil || it.State != factory.StateNeedsYou {
		return -1
	}
	stages := factoryStages(a.fp.snap, it)
	for i, ph := range s.Phases {
		if ph.State != factory.PhaseWaiting {
			continue
		}
		if ph.Kind == factory.StageGate {
			return i
		}
		if st, ok := factoryStageNamed(stages, ph.Name); ok && st.Kind == factory.StageGate {
			return i
		}
	}
	return -1
}

// factoryControlOf is what the control does for item it on this seam.
func (a *app) factoryControlOf(it factory.Item) factoryControl {
	switch {
	case a.factoryHeldAt(it) >= 0 && a.factory.Has("answer"):
		return factoryControlContinue
	case it.State == factory.StateRunning && a.factory.Has("pause"):
		if it.Stream != nil && it.Stream.Paused {
			return factoryControlResume
		}
		return factoryControlPause
	case (it.State == factory.StateNew || it.State == factory.StateDismissed) && a.factoryCanRun():
		return factoryControlRun
	}
	return factoryControlNone
}

// factoryControlLabel is the control as it reads, its icon and its word:
// `▶ run`, `= pause`, `▶ continue`. "" for none.
func (a *app) factoryControlLabel(c factoryControl) string {
	switch c {
	case factoryControlRun, factoryControlResume:
		return a.icon(tokens.GRun) + " " + wordRun
	case factoryControlPause:
		return a.icon(tokens.GPaused) + " " + wordPause
	case factoryControlContinue:
		return a.icon(tokens.GRun) + " " + wordContinue
	}
	return ""
}

// factoryControlPress is a press on the control, or its key: what the label
// said, through the door that does it.
func (a *app) factoryControlPress() (tea.Cmd, bool) {
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil, false
	}
	a.pageMsg, a.fp.said = "", false
	switch a.factoryControlOf(it) {
	case factoryControlRun:
		return a.factoryRunHere(it), true
	case factoryControlPause, factoryControlResume:
		return a.factoryPauseKey()
	case factoryControlContinue:
		id := it.ID
		return a.factoryVerb(id, func(s factory.Seam) error { return s.Answer(id, true, "") }, func(it factory.Item) string {
			return "answered " + it.Ref() + rowSep + wordContinue
		}), true
	}
	return nil, false
}

// factoryBarRows is the bar's rows at width, each already led by the page's
// margin and exactly width cells: the bar itself, the question the run
// waits on, the item's typing row and offers, and a blank row. The
// control's cells and the crumbs' are kept for the pointer, on the bar's
// own row (factoryPage.geo).
func (a *app) factoryBarRows(it factory.Item, width int) []string {
	measure := max(width-factoryMargins, 0)
	lead := factoryMarginPad()
	rows := []string{a.factoryBarLine(it, measure)}
	if q := a.factoryBarQuestion(it, measure); q != "" {
		rows = append(rows, q)
	}
	rows = append(rows, a.factoryFootRowsWhere(measure, false)...)
	rows = append(rows, a.factoryFootRowsWhere(measure, true)...)
	out := make([]string, 0, len(rows)+1)
	for _, r := range rows {
		out = append(out, factoryPad(lead+r, width))
	}
	return append(out, factorySpaces(width))
}

// factoryBarCount is how many rows [app.factoryBarRows] draws for item it,
// asked by the frame's height arithmetic without drawing them.
func (a *app) factoryBarCount(it factory.Item) int {
	n := 2
	if a.factoryBarQuestion(it, 200) != "" {
		n++
	}
	return n + len(a.factoryFootRowsWhere(200, false)) + len(a.factoryFootRowsWhere(200, true))
}

// factoryBarLine is the bar's first row, measure cells, without the margin:
// the control, the crumbs and the title, and at the right where the run
// stands.
func (a *app) factoryBarLine(it factory.Item, measure int) string {
	pal := a.pal
	g := &a.fp.geo
	g.ctlX0, g.ctlX1 = -1, -1
	x := factoryMargin
	left := ""
	if label := a.factoryControlLabel(a.factoryControlOf(it)); label != "" {
		button := " " + label + " "
		w := ansi.StringWidth(button)
		g.ctlX0, g.ctlX1 = x, x+w
		if a.fp.hot.kind == factoryHotControl {
			left = pal.cursor(pal.ink(button), w)
		} else {
			left = pal.accent(button)
		}
		left += factorySpaces(factoryGutter)
		x += w + factoryGutter
	}
	trail := a.factoryItemCrumbsAt(it, x, measure)
	left += trail + pal.ink(" "+it.Title)
	right := ""
	if facts := a.factoryBarFacts(it); facts != "" {
		right = pal.muted(facts)
	}
	if spend := factorySpend(it.Stream, it.Cap); spend != "" && it.Stream != nil {
		if right != "" {
			right += pal.dim(rowSep)
		}
		right += placeMoneyInk(pal)(spend)
	}
	return factorySpread(left, right, measure)
}

// factoryBarFacts is where the run stands, plain: the step it is on, held,
// paused or waiting, and its round, `review · round 1/2`; `landed` once it
// came out; nothing for an item that never ran (the emptiness law).
func (a *app) factoryBarFacts(it factory.Item) string {
	s := it.Stream
	if s == nil {
		return ""
	}
	stages := factoryStages(a.fp.snap, it)
	for i, ph := range s.Phases {
		if ph.State != factory.PhaseRunning && ph.State != factory.PhaseWaiting {
			continue
		}
		parts := []string{ph.Name}
		switch kind := factoryPhaseKind(it, i); {
		case kind == factoryMarkPaused:
			parts = append(parts, a.factoryPausedFor(it))
		case a.factoryHeldAt(it) == i:
			parts = append(parts, wordWaitingForYou)
		}
		most := 1
		if st, ok := factoryStageNamed(stages, ph.Name); ok {
			most = max(st.Max, 1)
		}
		if most > 1 && ph.Round > 0 {
			parts = append(parts, wordRound+" "+factoryRoundWords(ph.Round, most))
		}
		return strings.Join(parts, rowSep)
	}
	return factoryStateWord(it)
}

// factoryBarQuestion is the bar's second row while the run waits on the
// person, measure cells: the question in ink after the ask mark, and at the
// right the keys that answer it. A hold at an approve step says the step and
// that it waits for you, and its key is the control's.
func (a *app) factoryBarQuestion(it factory.Item, measure int) string {
	if at := a.factoryHeldAt(it); at >= 0 {
		pal := a.pal
		q := strings.TrimSpace(it.Question)
		if q == "" {
			q = it.Stream.Phases[at].Name + rowSep + wordWaitingForYou
		}
		left := pal.ask(a.icon(tokens.GNeedsHuman)) + " " + pal.ink(q)
		right := ""
		if a.factory.Has("answer") {
			right = pal.dim(factoryHintClause(keyControl, wordContinue))
		}
		if right != "" && ansi.StringWidth(left)+factoryGutter+ansi.StringWidth(right) <= measure {
			return factorySpread(left, right, measure)
		}
		return fit(left, measure)
	}
	return a.factoryItemQuestion(it, measure)
}

// factoryAnswerClauses is the keys that answer item it's question, as
// clauses: `y yes · n no · a in words`, and at an approve step
// `y continue · n send back · a in words`, the bar's own word.
func factoryAnswerClauses(it factory.Item) string {
	yes, no := factoryYesNo(it)
	return strings.Join([]string{
		factoryHintClause(keyYes, yes),
		factoryHintClause(keyNo, no),
		factoryHintClause(keyInWords, wordInWords),
	}, rowSep)
}

// factoryYesNo is what `y` and `n` do for item it, in words: continue and
// send back at an approve step, yes and no on every other question.
func factoryYesNo(it factory.Item) (yes, no string) {
	if it.QKind == factory.QKindApprove {
		return wordContinue, wordSendBack
	}
	return wordYes, wordNo
}
