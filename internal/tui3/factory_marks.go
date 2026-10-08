package tui3

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── ONE OUTPUT, ONE MEANING ─────────────────────────────────────────────────
//
// EVERY MARK ON THE FLOOR SAYS ONE THING, and a state that is not the state it
// looks like gets a mark of its own. The review of 2026-10-08 found four places
// where one drawing carried two meanings, each of which a person would act on
// wrongly:
//
//   - A PAUSED ITEM looked running: its lead and its strip kept the working
//     mark and its peek kept `13m left · editing …`. It wears the pause mark
//     ([tokens.GPaused]) on its lead and on the phase it holds, and says
//     `paused 13m`, counted from when the floor first saw it held.
//   - A SKIPPED STAGE looked to come: switched off, its condition not met, or
//     passed over by a run that went on past it, it wore the pending ring. It
//     wears the skip stroke ([tokens.GSkipped]), dim, and the word `skipped`.
//   - A STOPPED ITEM looked failed: the phase a person stopped wore the red
//     cross and the runner's last note, `TestRelay failed`. It wears the stop
//     square ([tokens.GStopped]) and says `stopped · branch kept`, because
//     nobody found anything about work somebody stopped.
//   - A ROW WAITING ITS TURN TO BE READ looked like one being read: a whole
//     floor's re-read spun every row it would reach. Only the row the floor
//     says is `reading` or `refreshing` spins; one it says is `waiting` wears a
//     still dim dot and `waiting to read` ([app.factoryRowSpins]).
//
// THIS FILE IS THE ONE MAPPING. The row's strip, the peek's strip and the item
// page's rail all ask [app.phaseMark] and [phaseWord] for a phase, and the row
// asks [app.itemLeadMark] for its lead, so the four meanings cannot drift
// apart between the three places a phase is drawn.

// factoryMarkKind is what a phase is drawing as, which is its state with the
// three states the runner does not write folded in: held, ended by a person,
// and passed over.
type factoryMarkKind int

const (
	factoryMarkPending factoryMarkKind = iota
	factoryMarkRunning
	factoryMarkDone
	factoryMarkWaiting
	factoryMarkFailed
	factoryMarkPaused
	factoryMarkStopped
	factoryMarkSkipped
)

// factoryPaused says whether a person has paused the item's stream: it is on
// a bench, or waiting for one, and held.
func factoryPaused(it factory.Item) bool {
	s := it.Stream
	return s != nil && s.Paused && (it.State == factory.StateRunning || it.State == factory.StateQueued)
}

// factoryStopped says whether a person stopped the item's run: it is back
// among the new work with the stream it had, the stream has ended, and its
// last phase never finished. A run that finished and was sent back is not a
// stop.
func factoryStopped(it factory.Item) bool {
	s := it.Stream
	if s == nil || s.Ended.IsZero() || len(s.Phases) == 0 {
		return false
	}
	if it.State != factory.StateNew && it.State != factory.StateDismissed {
		return false
	}
	return s.Phases[len(s.Phases)-1].State != factory.PhaseDone
}

// factoryStoppedAt is the phase a stopped run stopped on: the first one that
// had not finished. -1 when the item is not stopped.
func factoryStoppedAt(it factory.Item) int {
	if !factoryStopped(it) {
		return -1
	}
	for i, ph := range it.Stream.Phases {
		if ph.State != factory.PhaseDone {
			return i
		}
	}
	return -1
}

// factoryPhaseKind is what the phase at index at of the item's stream draws
// as. A PENDING PHASE THE RUN WENT PAST IS SKIPPED: one with a phase after it
// that has started, or one on an item that has landed or shipped, will not
// run, and drawing it as still to come is a promise nobody will keep.
func factoryPhaseKind(it factory.Item, at int) factoryMarkKind {
	s := it.Stream
	if s == nil || at < 0 || at >= len(s.Phases) {
		return factoryMarkPending
	}
	if at == factoryStoppedAt(it) {
		return factoryMarkStopped
	}
	ph := s.Phases[at]
	switch ph.State {
	case factory.PhaseRunning:
		if factoryPaused(it) {
			return factoryMarkPaused
		}
		return factoryMarkRunning
	case factory.PhaseDone:
		return factoryMarkDone
	case factory.PhaseWaiting:
		return factoryMarkWaiting
	case factory.PhaseFailed:
		return factoryMarkFailed
	}
	if it.State == factory.StateLanded || it.State == factory.StateShipped {
		return factoryMarkSkipped
	}
	for _, later := range s.Phases[at+1:] {
		if later.State != factory.PhasePending {
			return factoryMarkSkipped
		}
	}
	return factoryMarkPending
}

// factoryKindMark is one kind's mark and the paint it wears. The held and the
// ended wear muted ink, never the accent and never the alarm, because neither
// is moving and neither is wrong; the passed-over stroke is dim, quieter than
// a stage still to come.
func (a *app) factoryKindMark(k factoryMarkKind) (string, func(string) string) {
	pal := a.pal
	switch k {
	case factoryMarkRunning:
		return a.icon(tokens.GStepRunning), pal.accent
	case factoryMarkDone:
		return a.icon(tokens.GStepDone), pal.muted
	case factoryMarkWaiting:
		return a.icon(tokens.GNeedsHuman), pal.ask
	case factoryMarkFailed:
		return a.icon(tokens.GFailed), pal.bad
	case factoryMarkPaused:
		return a.icon(tokens.GPaused), pal.muted
	case factoryMarkStopped:
		return a.icon(tokens.GStopped), pal.muted
	case factoryMarkSkipped:
		return a.icon(tokens.GSkipped), pal.dim
	}
	return a.icon(tokens.GStepPending), pal.dim
}

// phaseMark is the mark the phase at index at of the item's stream wears, and
// its paint: the ONE mapping the row's strip, the peek's strip and the item
// page's rail draw a phase with.
func (a *app) phaseMark(it factory.Item, at int) (string, func(string) string) {
	return a.factoryKindMark(factoryPhaseKind(it, at))
}

// phaseWord is the phase at index at as its words, where most is the most
// rounds its stage may take: [factoryPhaseWords] for a phase that is what its
// state says, and for the three it is not, its name with what it is instead —
// `write ×3 · paused`, `test 2/2 · stopped`, `update manual · skipped`. A
// HELD OR ENDED PHASE HAS NO MINUTES LEFT, so it never says any.
func phaseWord(it factory.Item, at, most int) string {
	s := it.Stream
	if s == nil || at < 0 || at >= len(s.Phases) {
		return ""
	}
	ph := s.Phases[at]
	switch factoryPhaseKind(it, at) {
	case factoryMarkPaused:
		ph.Left = 0
		return factoryPhaseWords(ph, most) + rowSep + "paused"
	case factoryMarkStopped:
		ph.Left, ph.State = 0, factory.PhaseDone
		return factoryPhaseWords(ph, most) + rowSep + "stopped"
	case factoryMarkSkipped:
		return ph.Name + rowSep + "skipped"
	}
	return factoryPhaseWords(ph, most)
}

// factoryPhaseCell is the phase at index at as the peek's strip draws one
// cell, painted and plain, before the strip pads it to its length class: the
// running cell all in the accent, a done, waiting, failed or stopped cell its
// mark in its paint and its words in ink, and a held cell its words muted.
// A cell still to come, or passed over, is dim whole.
func (a *app) factoryPhaseCell(it factory.Item, at, most int) (string, string) {
	pal := a.pal
	kind := factoryPhaseKind(it, at)
	mark, paint := a.factoryKindMark(kind)
	words := phaseWord(it, at, most)
	plain := mark + " " + words
	switch kind {
	case factoryMarkRunning:
		return pal.accent(plain), plain
	case factoryMarkDone, factoryMarkWaiting, factoryMarkFailed, factoryMarkStopped:
		return paint(mark) + " " + pal.ink(words), plain
	case factoryMarkPaused:
		return paint(mark) + " " + pal.muted(words), plain
	}
	return pal.dim(plain), plain
}

// itemLeadMark is an item's one-cell lead, painted, and "" for a new item at
// rest, which draws nothing in its lead.
//
//	needs you  ? in the asking colour
//	running    the working mark in accent
//	paused     the pause mark, muted
//	queued     the queued ring, dim
//	new        nothing; a red CI run wears the failed cross
//	stopped    the stop square, muted
//	landed     the settled check
//	shipped    a dim dot
//	marked     the dot, or the state's own mark, in accent
func (a *app) itemLeadMark(it factory.Item) string {
	pal := a.pal
	glyph, paint := "", pal.muted
	switch {
	case factoryPaused(it):
		glyph = a.icon(tokens.GPaused)
	case factoryStopped(it):
		glyph = a.icon(tokens.GStopped)
	case it.State == factory.StateNeedsYou:
		glyph, paint = a.icon(tokens.GNeedsHuman), pal.ask
	case it.State == factory.StateRunning:
		glyph, paint = a.icon(tokens.GWorking), pal.accent
	case it.State == factory.StateQueued:
		glyph, paint = a.icon(tokens.GQueued), pal.dim
	case it.State == factory.StateNew:
		if it.Kind == factory.KindCI {
			glyph, paint = a.icon(tokens.GFailed), pal.bad
		}
	case it.State == factory.StateLanded:
		glyph = a.icon(tokens.GSettled)
	case it.State == factory.StateShipped:
		glyph, paint = a.icon(tokens.GDoneCell), pal.dim
	}
	if a.factoryMarked(it) {
		if glyph == "" {
			glyph = a.icon(tokens.GDoneCell)
		}
		paint = pal.accent
	}
	if glyph == "" {
		return ""
	}
	return paint(glyph)
}

// factoryStateWord is where an item stands as one word for the item page's
// head: the state, or `paused` and `stopped` where the state word would say
// running and new.
func factoryStateWord(it factory.Item) string {
	switch {
	case factoryPaused(it):
		return "paused"
	case factoryStopped(it):
		return "stopped"
	}
	return string(it.State)
}

// factoryStoppedWords is what a stopped item says about itself, on its row
// and under its strip.
const factoryStoppedWords = "stopped · branch kept"

// factoryPausedKey names an item's hold for [factoryPage.phaseSince], beside
// the running phases it already times ([app.factoryFoldPhases]).
func factoryPausedKey(id int) string { return "paused/" + itoa(id) }

// factoryPausedFor is how long the floor has seen the item held, as `paused
// 13m`, and `paused` alone before a minute has passed or when the floor never
// saw it begin. It counts from the first snapshot that said paused, on the
// floor's own clock, the trade the running phase's elapsed makes.
func (a *app) factoryPausedFor(it factory.Item) string {
	since, ok := a.fp.phaseSince[factoryPausedKey(it.ID)]
	if !ok || since.IsZero() || a.fp.snap.Now.IsZero() {
		return "paused"
	}
	if span := factoryElapsed(since, a.fp.snap.Now); span != "" {
		return "paused " + span
	}
	return "paused"
}

// factoryPausedLine is the line a held item says under its strip, where a
// running one says what it is doing: `paused 13m · write ×3`. "" for an item
// that is not paused.
func (a *app) factoryPausedLine(it factory.Item) string {
	if !factoryPaused(it) {
		return ""
	}
	words := a.factoryPausedFor(it)
	for _, ph := range it.Stream.Phases {
		if ph.State == factory.PhaseRunning {
			ph.Left = 0
			stages := factoryStages(a.fp.snap, it)
			words += rowSep + factoryPhaseWords(ph, factoryStageMax(stages, ph.Name))
			break
		}
	}
	return words
}

// factoryStateLine is the one line an item's own state adds under its strip
// when that state is one this file names: a held item's `paused 13m · write
// ×3`, and a stopped one's `test 2/2 · stopped · branch kept`. "" otherwise,
// and the peek then says what it always said.
func (a *app) factoryStateLine(it factory.Item) string {
	if line := a.factoryPausedLine(it); line != "" {
		return line
	}
	if at := factoryStoppedAt(it); at >= 0 {
		return it.Stream.Phases[at].Name + rowSep + factoryStoppedWords
	}
	return ""
}

// ── a row waiting its turn ──────────────────────────────────────────────────

// factoryRowSpins says whether the row of the item with id is being read
// RIGHT NOW: the floor's busy word for it is `reading` or `refreshing`. It is
// the one question the row drawer asks before it spins the priority cell. A
// row the floor says is `waiting` does not spin ([app.factoryRowWaits]).
func (a *app) factoryRowSpins(id int) bool {
	word, busy := a.factoryBusy(id)
	return busy && (word == factory.BusyReading || word == factory.BusyRefreshing)
}

// factoryRowWaits says whether the row of the item with id is queued for a
// read the floor has not reached yet: its busy word is anything but reading.
// Such a row wears a still dim dot in the priority cell and `waiting to read`
// as its last fact.
func (a *app) factoryRowWaits(id int) bool {
	_, busy := a.factoryBusy(id)
	return busy && !a.factoryRowSpins(id)
}

// factoryWaitWords is a waiting row's fact.
const factoryWaitWords = "waiting to read"

// factoryFlightFact is the row's in-flight fact, plain: `refreshing…` or
// `reading…` while it is read, `waiting to read` while it waits, and "" when
// nothing is in flight.
func (a *app) factoryFlightFact(id int) string {
	if a.factoryRowWaits(id) {
		return factoryWaitWords
	}
	if word, busy := a.factoryBusy(id); busy {
		return word + "…"
	}
	return ""
}

// ── the item's keys, one list at two widths ─────────────────────────────────

// factoryVerbRail is the keys that work on the item under the cursor, in ONE
// ORDER for every place that names them: the peek's key line and the floor's
// bottom key line both draw this list, and both drop from its right end as
// they narrow, so the two lose THE SAME KEYS IN THE SAME ORDER. `enter open`
// leads, because on the floor `enter` opens the item page; the item page's own
// line takes [app.factoryVerbHint], which is this list without it.
func (a *app) factoryVerbRail(it factory.Item) []string {
	return append([]string{"enter open"}, a.factoryVerbHint(it)...)
}

// factoryVerbLine is [app.factoryVerbRail] in at most measure cells, plain,
// whole clauses kept from the left: the peek's key line. A clause is never
// cut in half.
func (a *app) factoryVerbLine(it factory.Item, measure int) string {
	return factoryVerbsWithin(a.factoryVerbRail(it), measure)
}

// factoryVerbsWithin keeps as many whole clauses of verbs from the left as
// fit in measure cells, joined by the dim middle dot's plain spelling.
func factoryVerbsWithin(verbs []string, measure int) string {
	out, used := "", 0
	for i, v := range verbs {
		w := ansi.StringWidth(v)
		if i > 0 {
			w += ansi.StringWidth(rowSep)
		}
		if used+w > measure {
			break
		}
		if i > 0 {
			out += rowSep
		}
		out += v
		used += w
	}
	return out
}

// factoryNotWaiting is the note a key that answers says on an item that is
// asking nothing: `#12 is not waiting on you`.
func factoryNotWaiting(it factory.Item) string {
	return it.Ref() + " is not waiting on you"
}

// factoryFlightKeep is the cells the facts column keeps so a row being read
// can say so at the floor's widest: the widest at-rest state fact (a new,
// queued, landed or shipped item's) with `· refreshing…` after it.
//
// WHY THE FLIGHT FACT NEVER FIT. The give-back in [app.factoryGridAt] lets the
// titles take the facts' cells down to the widest state fact, so at 160
// columns the facts column was exactly as wide as `pr · ci ✓` and the flight
// fact behind it had no cell to stand in, at any width. THE RESERVE IS
// PERMANENT, not raised while a read is out, so the grid does not move every
// time an arrival is read; and it is spent only by the give-back, never by
// the tight floor's rule, so a narrow floor's titles are not starved for a
// fact that is the first a narrow row drops. A running row's strip is too
// wide for it to share, and so is a stopped row's sentence; there the spinner
// alone says it.
func (a *app) factoryFlightKeep() int {
	flight := ansi.StringWidth(rowSep + factory.BusyRefreshing + "…")
	w := 0
	for _, it := range a.fp.snap.Items {
		if !factoryOnFloor(it) || it.State == factory.StateNeedsYou || it.State == factory.StateRunning || factoryStopped(it) {
			continue
		}
		if st, ok := a.factoryStateFact(it); ok {
			w = max(w, ansi.StringWidth(st.plain)+flight)
		}
	}
	return w
}
