package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DOOR IS DOUBLE CTRL+C, AND IT IS DOUBLE BECAUSE ONE PRESS WAS NEVER A
// DECISION.
//
// ctrl+c is read above every modal on this surface — leaving is never modal —
// and until this wave the very first press ended the session outright whenever
// no turn happened to be streaming. Four things went out of the window with it,
// all of them real:
//
//   - A DOUBLE-TAP MID-TURN QUIT. [app.interrupt] sets stateInterrupted on the
//     spot, so the second half of the two-tap every terminal habit teaches —
//     press it again, harder, because the first one did not seem to land — was
//     read as a press at rest and took the door.
//   - THE GAP AT THE END OF A TURN. session.EventTurnDone flips the state to
//     idle one Update cycle before streamClosedMsg drains what is parked
//     (park.go). ctrl+c inside that gap quit, and the parked messages went with
//     it in silence.
//   - WORK THAT RUNS WHILE THE SURFACE IS IDLE. Tasks, background jobs and the
//     ctrl+g roster all keep going with no turn in flight, and one keystroke
//     killed the lot of them with nothing on screen saying so first.
//   - A REAL SIGINT NEVER REACHED [app.quit] AT ALL — it was eaten by Bubble
//     Tea's own handler and the draft was never written (tui3.go's [Run] is
//     where that half is fixed).
//
// So the first press ARMS and says what the second one will cost; the second
// press inside the window leaves. Mid-turn nothing changes at all: ctrl+c is
// still the interrupt, exactly as it always was, and it does NOT arm — the
// press that stopped the model was aimed at the model.

// quitArmWindow is how long the first ctrl+c keeps the door warm.
//
// A SECOND AND A HALF, and it is three times [rewindArmWindow] on purpose. The
// esc-esc rewind is a CHORD: two keys of one gesture, made by a hand that has
// already decided, and half a second is generous for that. This is not a chord.
// The first press puts a sentence in the hint slot — what is about to stop, and
// which key stops it — and the window has to be long enough to READ that
// sentence and then decide. A window sized for a chord would be a window that
// asks a question and hangs up before the answer.
//
// It errs LONG for the reason rewind's errs short, which is that the two
// mistakes are different sizes here as well — the wrong way round. A window
// that lapsed too early costs one extra keystroke on the way out of a program
// somebody has decided to leave; a window that lapsed too late does nothing at
// all, because the only key that can act on it is the key the person would have
// had to press again on purpose.
const quitArmWindow = 1500 * time.Millisecond

// quitArmWord is what the hint slot says while the first press is still warm.
// It is the whole sentence when nothing is running; [app.quitHint] adds what
// the second press would stop when something is.
const quitArmWord = "ctrl+c again to quit"

// armQuit is the first press. It returns the command that keeps the frame clock
// turning, because the window has an end to reach and the hint has to leave the
// screen when it does.
func (a *app) armQuit() tea.Cmd {
	a.quitArm = a.now()
	a.touch()
	return a.wake()
}

// quitArmed reports whether the first press is still warm.
func (a *app) quitArmed() bool {
	return !a.quitArm.IsZero() && a.now().Sub(a.quitArm) < quitArmWindow
}

// disarmQuit forgets it. It is called from the top of [app.key] for every key
// that is not ctrl+c itself, and from the doors that start a turn or replace the
// conversation — a warm door is a promise about the NEXT keystroke, and every
// one of those events makes that promise untrue.
func (a *app) disarmQuit() {
	if a.quitArm.IsZero() {
		return
	}
	a.quitArm = time.Time{}
	a.touch()
}

// quitSweep runs the window down. It is called from [app.paint] and from nowhere
// else, for [app.rewindSweep]'s reason: this surface has exactly one clock, and
// a second one is a second wakeup per second and two states that disagree about
// the time.
func (a *app) quitSweep() {
	if !a.quitArm.IsZero() && !a.quitArmed() {
		a.disarmQuit()
	}
}

// ── WHAT THE SECOND PRESS WOULD COST ────────────────────────────────────────

// quitHint is the armed sentence, with what leaving would do to the work named
// on the end of it:
//
//	ctrl+c again to quit
//	ctrl+c again to quit · a task will stop
//	ctrl+c again to quit · a task keeps running
//	ctrl+c again to quit · 3 conversations · 2 tasks and a job will stop
//
// THE CLAUSE ORDER IS DELIBERATE: how many conversations, then what work. A
// person who has forgotten they left something open in another project needs the
// first number before the second one means anything.
//
// AND THE VERB IS THE TRUTH ABOUT THIS CONVERSATION. Work a host is running
// outlives the window, so a warning that said it would stop would be asking
// somebody not to close a terminal for a reason that is not real — and the day
// they believed it, they would leave the window open for an hour to protect work
// that was never at risk. Work this process is running does stop, and that
// sentence is unchanged (keeper.go's [workOutlivesExit] is the one test).
//
// EVERY CLAUSE IS ABSENT WHEN IT IS ZERO, which is the emptiness law: one
// conversation drops the first, nothing running drops the second, and a quiet
// single conversation reads exactly `ctrl+c again to quit`. A line saying
// "1 conversation · 0 tasks will stop" would be two facts of which both are the
// absence of a fact, in the slot a person reads most.
func (a *app) quitHint() string {
	word := quitArmWord
	if open := a.openCount(); open > 1 {
		word += " · " + itoa(open) + plural(" conversation", open)
	}
	for _, clause := range a.quitWorkClauses() {
		word += " · " + clause
	}
	return word
}

// quitWorkClauses names what this terminal has running and what leaving does to
// it, in a person's own words: one clause for the work that stops, one for the
// work that keeps going, and nothing at all when there is neither.
//
// IT COUNTS WHAT THE FRAME ALREADY TRUSTS rather than asking anything new. The
// running task nodes are the strip's own set (taskstrip.go), and the background
// jobs are the number the ambient segment draws when it is nonzero (render.go's
// [app.ambientSegment], off a cached sum). A second opinion computed here is how
// a warning and the screen above it come to disagree about what is alive.
//
// ADAPTIVE RUNS ARE NOT COUNTED, and deliberately: [app.orchLive] is the run
// this surface last HEARD FROM rather than a list of what is running, and a
// warning that named a run which had already finished would be worse than a
// warning that named nothing.
//
// AND IT COUNTS ACROSS EVERY CONVERSATION THIS TERMINAL HOLDS, on the keystroke
// and never on a frame (keeper.go's [app.behindTasks]) — a warning that named
// only the conversation on screen would be the one place this feature could
// cost somebody work they had forgotten about. The jobs are the front
// conversation's only, and that limit is stated where it is made.
func (a *app) quitWorkClauses() []string {
	stopping, keeping := a.quitWorkCounts()
	var clauses []string
	if word := quitWorkWord(stopping); word != "" {
		clauses = append(clauses, word+" will stop")
	}
	if word := quitWorkWord(keeping); word != "" {
		clauses = append(clauses, word+" "+quitKeepsVerb(keeping)+" running")
	}
	return clauses
}

// quitWorkCount is running work on one side of that line: nodes, and the
// background jobs of the conversation on screen.
type quitWorkCount struct{ tasks, jobs int }

// quitWorkCounts splits what is running by what leaving would do to it. The
// conversation on screen carries the jobs, because that is the only one this
// surface can count them for ([app.behindTasks] states the limit).
func (a *app) quitWorkCounts() (stopping, keeping quitWorkCount) {
	front := &stopping
	if a.agent != nil && workOutlivesExit(a.agent) {
		front = &keeping
	}
	for _, id := range a.taskOrder {
		if node := a.tasks[id]; node != nil && node.state == session.TaskRunning {
			front.tasks++
		}
	}
	front.jobs += a.hudStats().jobs
	for _, held := range a.behind {
		if held.conv.Agent == nil {
			continue
		}
		side := &stopping
		if workOutlivesExit(held.conv.Agent) {
			side = &keeping
		}
		side.tasks += runningTasks(held.conv.Agent)
	}
	return stopping, keeping
}

// quitWorkWord spells one side of the count — "a task", "2 tasks and a job" —
// and "" when that side has nothing on it.
func quitWorkWord(count quitWorkCount) string {
	var parts []string
	if count.tasks > 0 {
		parts = append(parts, quitCountWord(count.tasks, "task"))
	}
	if count.jobs > 0 {
		parts = append(parts, quitCountWord(count.jobs, "job"))
	}
	return strings.Join(parts, " and ")
}

// quitKeepsVerb agrees with what it is about: one thing keeps running, several
// keep running.
func quitKeepsVerb(count quitWorkCount) string {
	if count.tasks+count.jobs == 1 {
		return "keeps"
	}
	return "keep"
}

// quitCountWord spells one of those counts the way a person would say it out
// loud: "a task", "2 tasks". One is an ARTICLE rather than the digit, because
// "1 task will stop" is a sentence written by a machine and this one is a
// warning somebody has to read in a second and a half.
func quitCountWord(n int, unit string) string {
	if n == 1 {
		return "a " + unit
	}
	return itoa(n) + " " + plural(unit, n)
}

// ── WHAT LEAVES WITH YOU ────────────────────────────────────────────────────

// leavingDraft is what the draft file gets on the way out: the sentence in the
// box, and then every message still parked above it, each on its own line.
//
// A PARKED MESSAGE IS SOMETHING THE PERSON TYPED AND PRESSED ENTER ON (park.go).
// It waits for the answer that is streaming and goes as a turn of its own when
// that answer lands — so a session closed before the turn ended used to drop it
// on the floor without a word. Folding it back into the draft is the only place
// on this surface that can still hold it: the next launch restores that file
// into the box (draft.go), and the words come back instead of vanishing.
//
// The draft leads because it is the newest thing typed and the thing the cursor
// is in; the parked messages follow in the order they were parked, which is the
// order they would have been sent in.
//
// AND THE DRAFT IT MEANS IS MAIN'S (recipient.go). Both readers of this — the
// file written on the way out of the program, and the sentence that walks with
// the person through /new, /resume and a takeover — are about the CONVERSATION,
// and the parked messages under it are the conversation's too. Read off the
// screen, a window quit while standing in a task's page would keep that page's
// steering line as the conversation's unsent sentence and give it back at the
// next launch in a box pointed at the model.
func (a *app) leavingDraft() string {
	text := a.mainDraftText()
	if len(a.parks) == 0 {
		return text
	}
	lines := make([]string, 0, len(a.parks)+1)
	if strings.TrimSpace(text) != "" {
		lines = append(lines, text)
	}
	for _, p := range a.parks {
		if strings.TrimSpace(p.text) != "" {
			lines = append(lines, p.text)
		}
	}
	return strings.Join(lines, "\n")
}
