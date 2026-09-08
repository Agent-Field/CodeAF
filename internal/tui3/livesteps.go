package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// ── WHILE A TURN RUNS, THE CONVERSATION SHOWS THREE LINES OF WHAT IT IS DOING ─
//
// A completed turn out in the conversation is one chip: the question, the answer,
// and the machinery between them folded away (workfold.go). A RUNNING turn was
// the opposite — every thought, every call, every line of narration, drawn in
// full and scrolling under the reader's eye at the speed the model works. The
// person who asked the question is watching a machine's transcript of itself,
// and the answer they are waiting for is being pushed off the bottom of the
// frame by the work that is producing it.
//
// So the running turn draws what the finished one draws, one tense earlier: a
// compact rolling window of the last few STEPS, under the question, in the work
// column. A step is what caption.go already derives — "listing github issues",
// "reading 2 files in internal/tui3" — so nothing here invents a word about the
// work. There is no second vocabulary and no second derivation.
//
// WHAT THE WINDOW SAYS, AND WHAT IT REFUSES TO SAY.
//
//   - THE STEP TITLES, AND NOTHING ELSE. No reasoning, no tool names, no
//     arguments, no output, no counts. Every one of those is a fact about the
//     machinery, and the machinery is what a person delegated precisely so they
//     would not have to watch it. All of it is one gesture away and none of it is
//     lost — the window HIDES rows, it never drops them.
//   - IT NEVER MANUFACTURES A STEP. Before the first caption and between
//     finished calls, a separate Working state keeps the window alive. It is
//     the running turn's state, not a claim that another step has happened.
//     The same row opens hidden work even before there is a caption to click.
//   - A NEW CAPTION ARRIVES ONLY WITH WORK. Silence can change the activity
//     state, but never invents another semantic description.
//
// THE MOTION IS THE ONE THIS SURFACE ALREADY OWNS. The newest step shimmers
// while a call under it is in flight, or the Working row moves between calls
// (captionmotion.go's [app.shimmer] — the spinner,
// relocated), and the steps above it step down the fade ladder the thinking
// window uses (styles.go's [thoughtFade]): faint, then dim, then the live one.
// Nothing here starts a timer or a goroutine; every frame this block moves on is
// a frame the running turn was already asking for (app.go's frame clock).
//
// ONE KEY, ONE CLICK, AND THE SAME KEY BACK. The block is the running turn's
// chip before the turn has finished, so it is opened by the gesture that opens
// the finished one and it is keyed the same way — by the turn ([workfold.key]).
// A click anywhere on it, or `ctrl+e`, opens the outline: every step as a caption
// row with its own door onto its own calls ([app.captionCallsOpen] keeps the
// running step's calls open and the past ones shut, exactly as it does under a
// finished chip). An opened window keeps a door of its own so the same key shuts
// it again.
//
// AND COMPLETION ALWAYS COLLAPSES IT, whatever the reader chose while it ran
// ([app.collapseLiveWork], called at the settle). The expansion is a thing a
// person wanted WHILE the work was happening; a turn that is over is an exchange
// again — a question and an answer — and the machinery behind it goes back
// behind the chip. This is the owner's ruling and it is the one place on this
// surface where a reader's expansion does not outlive what it was about.
//
// WHAT IT MAY NEVER COVER is [liveWorkKeeps], and the list is the conversation's
// own: the person's words, a question waiting to be answered, a failed call, a
// notice this surface wrote, a seam, an answer. A window that swallowed one of
// those would be hiding the one row on the frame that needed a person.
//
// THE CONVERSATION AND NOWHERE ELSE. A task's page is the page somebody opened
// BECAUSE they want to watch the machinery (lens.go's [overseerLens]), and a
// node's transcript inside a run's page is two gestures deeper into the same
// request. Both keep every row they have; the gate is the lens's own fold style
// and not a page name.

// liveStepRows is THE COMPACT READING BUDGET, and it is ROWS rather than steps.
//
// Three is the number the thinking window arrived at for the same reason
// (thinking.go): where the work is now, and the two steps it took to get there.
// Counting ROWS is what makes the budget survive a narrow frame — a caption that
// wraps to two lines has spent two of the three, and the alternative is a block
// that is three lines wide on this terminal and six on that one.
const liveStepRows = 3

// liveWorkWord is what the open block calls itself. It is [app.workfoldLabel]'s
// "worked" in the present tense, because it is the same chip about the same work
// one tense earlier, and a second vocabulary for a running turn would be a second
// thing to keep in step.
const liveWorkWord = "working"

// liveWork is one run of a running turn's machinery, and the steps inside it.
//
// It is derived per layout and journaled nowhere, exactly like [workfold]: the
// same entries derive the same window, and the reader's expansion dies with the
// turn that owned it.
//
// A turn holds more than one of these when the person has said something into
// it. A correction is one of their own messages and a window may not cover it,
// so the run ends there and a new one opens under it — which leaves their
// sentence standing between two compact blocks, where they said it.
type liveWork struct {
	// turn is THE KEY, and it is the turn for [workfold.key]'s reason: out in
	// the conversation one turn holds one chip, and this block is that chip
	// before the turn ended. It is deliberately the key the finished chip will
	// take, so that opening the work and watching it settle is one control and
	// not two.
	turn int
	// start and end are the entries this block stands in place of.
	start, end int
	// steps are the captions inside it, in the order they happened.
	steps []caption
	// pending is the live frontier between calls. It is a state indicator, never
	// a caption claiming that a completed call is still doing work.
	pending bool
}

// deriveLiveWork is the windows one page draws, and it answers nothing at all on
// a page that is not the conversation.
//
// It reads [deck.captions], which [app.deckRows] has already derived over the
// same list — a second derivation would be a second answer to "what are the
// steps of this turn", and the two would drift.
func deriveLiveWork(d deck) map[int]liveWork {
	// AND THE READER WHO ASKED FOR EVERY CALL GETS EVERY CALL. `ctrl+o` is "show
	// me the rest of this" over the turn on screen (app.go's [app.unfold]), and a
	// window that went on standing over it would leave that key pressing against
	// a door it had already opened.
	//
	// IT IS A GATE ON THE DRAWING AND NOT ON THE RUNS. The block still EXISTS
	// while every call is showing — it is the same running work — so the gesture
	// that closes the whole of it goes on finding it ([app.liveWorkOf]) and takes
	// this override away as it shuts.
	if d.unfolded[d.runningTurn] {
		return nil
	}
	return liveWorkRuns(d)
}

// liveWorkRuns is the runs themselves, without the drawing gate above: whether
// this page HAS running work that a compact window covers.
func liveWorkRuns(d deck) map[int]liveWork {
	// ZERO MEANS NO RUNNING TURN (workfold.go says why replay can number a real
	// turn zero), and the fold style is the whole of the page test: the
	// conversation folds turns, a room folds phases, a node's transcript folds
	// nothing (lens.go).
	if d.runningTurn == 0 || d.lens.foldPast != foldTurns {
		return nil
	}
	es := d.entries
	keeps := liveWorkKeeps(d)
	out := make(map[int]liveWork)
	// THE CAPTIONS ARE WALKED ONCE, WITH A CURSOR. Both lists are in index order
	// — [deriveCaptions] appends as it walks the entries — so a run takes the
	// slice of steps that begins inside it and the next run carries on from
	// there. Asking every caption about every run is the same answer at the cost
	// of steps times runs, which a turn full of corrections pays twice over.
	step := 0
	for lo := 0; lo < len(es); lo++ {
		if es[lo].turn != d.runningTurn || keeps[lo] {
			continue
		}
		hi := lo + 1
		for hi < len(es) && es[hi].turn == d.runningTurn && !keeps[hi] {
			hi++
		}
		for step < len(d.captions) && d.captions[step].start < lo {
			step++
		}
		from := step
		for step < len(d.captions) && d.captions[step].start < hi {
			step++
		}
		w := liveWork{turn: d.runningTurn, start: lo, end: hi, steps: d.captions[from:step], pending: hi == len(es)}
		for _, c := range w.steps {
			if c.ended.IsZero() {
				w.pending = false
				break
			}
		}
		out[lo] = w
		lo = hi - 1
	}
	return out
}

// liveWorkKeeps marks, for every row of the list, whether this block may cover
// it. It is a table rather than a condition spelled into the walk above for
// [phaseKeeps]'s reason: the list IS the argument, and an argument written as
// control flow cannot be read.
//
// A STEP IS ATOMIC. The second pass is what makes that true: a caption whose
// batch holds a failed call is kept WHOLE — its narration, its calls, all of it —
// rather than half-compacted, because a step drawn twice (once as a line in the
// window and once as the machinery under it) is the same work claiming to be two
// things. ONLY FAILURE SPEAKS on this surface, and the row that was allowed to
// raise its voice may not then be filed away by a rolling window.
func liveWorkKeeps(d deck) []bool {
	es := d.entries
	keeps := make([]bool, len(es))
	for i := range es {
		keeps[i] = liveWorkKeepsRow(&es[i])
	}
	for _, c := range d.captions {
		held := false
		for i := c.start; i < c.end && i < len(es); i++ {
			held = held || keeps[i]
		}
		if !held {
			continue
		}
		for i := c.start; i < c.end && i < len(es); i++ {
			keeps[i] = true
		}
	}
	return keeps
}

// liveWorkKeepsRow is the per-row half of the table above.
func liveWorkKeepsRow(e *entry) bool {
	switch e.kind {
	case entryThinking:
		// PRIVATE MACHINERY, AND THE LOUDEST OF IT. A wall of dim italic scrolling
		// under the answer a person is waiting for is the defect this whole block
		// exists to end; the working is one keypress away while it streams, exactly
		// as it was before (thinking.go).
		//
		// AND AN OPEN ONE IS COVERED TOO. [entry.open] is written by the stream as
		// well as by the reader — a think opens itself as it arrives — so it is not
		// evidence that anybody asked for it, and a container that let its own
		// child decide whether it could be seen would not be a container. THE
		// CHILD'S EXPANSION IS NOT LOST: opening the whole work draws that block
		// exactly as the person left it, and shutting the work hides it again.
		return false
	case entryTool:
		// A CALL THAT FAILED IS NEVER COVERED, and neither is one waiting on a
		// person: a consent question is a thing the work could not decide alone
		// (consent.go), and hiding it would hide the only row on the frame that
		// needs a hand.
		//
		// AND NEITHER IS ONE THE PERSON ANSWERED. [entry.decision] is the receipt
		// of their own keypress — `allowed`, `denied`, and the `always · saved —
		// /permissions to change` slot beside it — and a window that swallowed the
		// row a moment after they pressed the key would be answering a decision
		// they made with a screen that says nothing happened. It is theirs, like
		// their words, and it stands.
		return e.status == toolFailed || e.status == toolConsent || e.decision != ""
	case entryAssistant:
		// NARRATION IS THE STEP TITLE ITSELF — its first line is lifted into the
		// caption this block draws (hierarchy.go's [stampCaptions]), so covering it
		// hides nothing the window is not already saying. THE ANSWER IS NEVER
		// COVERED: the trailing block of a running turn is provisionally the answer
		// (hierarchy.go), which is the thing the person is waiting for.
		return !e.demoted
	}
	// EVERYTHING ELSE STANDS. The person's own words and their corrections, a
	// proposal, a standing card, a sign-in, a sub-harness offer, a landed task, a
	// seam, a compaction with its own clock, a divider, and every note this
	// surface wrote in its own voice — each of them is either something said TO
	// the person or something they have to answer.
	return true
}

// liveWorkOf is THE ONE WINDOW a gesture acts on: whether this page has a
// running turn with a compact block on it at all, and the key that opens it.
//
// It derives the hierarchy and the captions the way [app.toggleCap] does,
// because a gesture arrives between two layouts and the list it names has to be
// the list the reader is looking at.
//
// IT ASKS FOR THE RUNS AND NOT FOR THE DRAWING ([liveWorkRuns]). A turn whose
// every call is showing because somebody pressed `ctrl+o` is still a turn with
// running work in it, and a key that stopped finding it there would be a
// disclosure with no way back — the state a reader is most likely to want out of
// is the one with the most on screen.
func (a *app) liveWorkOf(d deck) (int, bool) {
	if d.runningTurn == 0 || d.lens.foldPast != foldTurns {
		return 0, false
	}
	folds := a.deckFolds(d)
	stampHierarchy(d.entries, folds)
	d.captions = deriveCaptions(d.entries, d.runningTurn)
	if len(liveWorkRuns(d)) == 0 {
		return 0, false
	}
	return d.runningTurn, true
}

// collapseLiveWork is COMPLETION ALWAYS COLLAPSES THE WORK, and it is called
// once, at the settle (app.go's [app.settle]), beside the thought that collapses
// there for the same reason.
//
// It forgets the reader's expansion rather than writing a false into the map:
// the chip [deriveWorkfolds] is about to mint for this turn defaults shut, and a
// key left behind would be a second author of that default.
func (a *app) collapseLiveWork(turn int) {
	if _, open := a.workOpen[turn]; !open {
		return
	}
	delete(a.workOpen, turn)
	a.touch()
}

// liveStepBlock is the compact block itself: the last few steps, oldest faintest,
// the live one shimmering.
//
// THE NEWEST STEP IS NEVER CHOPPED, which is what the budget costs on a narrow
// frame. Rows are taken newest-first and a step is only admitted whole, so a
// caption that wraps to two lines pushes the oldest step out of the window
// rather than being cut to fit. A current caption that alone wraps past the
// budget is drawn in full. Between calls, the current activity takes priority:
// a finished caption too tall to fit beside it remains behind the disclosure. A step title is five to ten words that
// somebody has to be able to read; an ellipsis in the middle of one would be the
// surface saving a row at the cost of the only thing the row was for.
func (a *app) liveStepBlock(w liveWork, width int, es []entry) []row {
	// Hidden reasoning needs a visible door even before a call has supplied a
	// caption. The label claims only that the turn is working; no step is invented.
	if len(w.steps) == 0 {
		word := a.pal.dim("Work")
		if w.pending {
			word = a.shimmer("Working")
			if a.ellipsisShowing() {
				word = a.activityLine(word)
			}
		}
		return []row{{text: a.pal.dim(a.linearMark("▸ ", "> ")) + word + a.pal.dim(" · ctrl+e"),
			entry: -1, hit: hitWorkFold, turn: w.turn, activity: w.pending}}
	}
	room := width - workIndentCols(width) - actionGutter
	if room < 1 {
		room = 1
	}
	// Newest first, one whole step at a time, until the budget is spent.
	type step struct {
		lines    []string
		live     bool
		pending  bool
		category session.ActionCategory
	}
	picked := make([]step, 0, liveStepRows)
	used := 0
	if w.pending {
		text := "Working"
		// The compact state inherits the footer's useful wait information only
		// while the footer would own it. During streaming reasoning the status
		// line keeps the phase instead, so no frame says the same fact twice.
		if a.ellipsisShowing() {
			text = ansi.Strip(a.activityLine(text))
		}
		lines := wrap(text, room)
		picked = append(picked, step{lines: lines, live: true, pending: true, category: session.ActionWork})
		used += len(lines)
	}
	for at := len(w.steps) - 1; at >= 0; at-- {
		lines := wrap(captionText(w.steps[at]), room)
		if len(lines) == 0 {
			continue
		}
		if len(picked) > 0 && used+len(lines) > liveStepRows {
			break
		}
		picked = append(picked, step{lines: lines, live: w.steps[at].ended.IsZero(), category: stepCategory(w.steps[at], es)})
		used += len(lines)
		if used >= liveStepRows {
			break
		}
	}
	// The ramp is [thoughtFade]'s, oldest first, and the newest step always takes
	// the LAST stop — so a window holding one step opens at the tier it will keep
	// rather than starting faint and brightening (thinking.go says it there).
	first := len(thoughtFade) - len(picked)
	if first < 0 {
		first = 0
	}
	out := make([]row, 0, used)
	for at := len(picked) - 1; at >= 0; at-- {
		s := picked[at]
		stop := first + (len(picked) - 1 - at)
		for i, line := range s.lines {
			// ONLY THE STEP THAT IS RUNNING MOVES, and only on its first line: the
			// shimmer is one band travelling over one line, and a second band on
			// the wrap under it would be two answers to "what is happening now"
			// (caption.go's [app.shimmer]).
			painted := a.pal.fade(line, stop)
			if s.live && at == 0 {
				painted = a.pal.narr(line)
				if i == 0 {
					painted = a.shimmer(line)
					if s.pending && len(line) >= len("Working") {
						painted = a.shimmer("Working") + a.pal.dim(line[len("Working"):])
					}
				}
			}
			// The action icon remains still while its caption carries the sweep.
			lead := a.actionLead(s.category, i == 0)
			if s.live && at == 0 {
				lead = a.pal.narr(lead)
			} else {
				lead = a.pal.fade(lead, stop)
			}
			out = append(out, row{text: lead + painted, entry: -1, hit: hitWorkFold, turn: w.turn, activity: s.live && at == 0})
		}
	}
	return out
}

// liveWorkDoor is the way back out of an opened window, and it is drawn only
// while the window is open — the compact block IS the shut state, and a chip
// over the top of it would spend a fourth row saying what the three under it
// already say.
//
// It names its key for the reason every fold on this surface names its key:
// something hidden without a way to it is something deleted.
func (a *app) liveWorkDoor(w liveWork) row {
	mark := a.linearMark("▾ ", "v ")
	return row{
		text:  a.pal.dim(mark + liveWorkWord + " · ctrl+e"),
		entry: -1, hit: hitWorkFold, turn: w.turn,
	}
}
