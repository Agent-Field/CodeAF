package tui3

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// taskCommandAgent is the whole of what /task needs from the session, and it is
// ONE call now. The planner door it used to carry is gone from the engine, along
// with the `/task adaptive` that was its only caller; the planner ENGINE is
// untouched and a person can still name a run outright in the conversation,
// which the session reads for itself. The sizing judge it carried beside
// StartTask is gone from this seam too (#936): the width of the work is read
// inside the engine, beside the worker, so there is nothing left for the surface
// to ask before the door opens. A door listed here that no command opens is an
// invitation to open it again.
//
// The bool is solo — the person saying the work is one worker's and asking for
// no reading of its width, by `/task solo` or by the standing `single` answer.
type taskCommandAgent interface {
	StartTask(context.Context, string, bool) (uint64, string, string, error)
}

type taskStartedMsg struct {
	kind, id, title string
	err             error
	// note is the engine's line about WHERE THE WORK STANDS — the ground
	// ladder's redirect, said when the work goes somewhere other than where it
	// was asked to go (internal/session's taskstands.go). Empty is every
	// ordinary start, and the absence law draws nothing for it.
	note string
	// brief is THE WORDS THE PERSON TYPED, carried back so the node can keep
	// them (taskbrief.go says what for). `/task` mints no proposal card — the
	// person typed the brief, so there was nothing to consent to — and the card
	// is the only place [app.taskUpdate] has ever read a contract from, so on
	// this road the surface knew the instruction, sent it, and then held nothing
	// that could say what the work was for.
	brief string
	// conv is the conversation that typed it, captured before the door was
	// opened. The answer comes back on a goroutine and the window may be sitting
	// somewhere else by then; the words belong to the conversation they were said
	// in and to no other ([app.adoptTypedBrief] enforces it).
	conv string
}

func (a *app) runTaskCommand(arg string) tea.Cmd {
	// The word was typed, bare or with a brief (notice.go).
	a.noticeEvent(eventTaskTyped)
	arg = strings.TrimSpace(arg)
	if arg == "" {
		// A BARE /task IS THE ROSTER AND NOT A USAGE LINE. The margin's `+ /task`
		// row types this command into the draft (margin.go), so the word arrives in
		// the box in front of somebody who has not said what the work is yet — and
		// a person who sends it as it stands is asking the only question the command
		// can answer with no brief behind it: what work is there. That is the page
		// /history opens ([app.openTaskPage]), and the two forms of the one command
		// are then the pair of errands a person has about tasks — start one, or go
		// and look at the ones that already ran.
		return a.openTaskPage()
	}
	door, ok := a.agent.(taskCommandAgent)
	if !ok {
		a.note("could not start the task · this session has no task door")
		return nil
	}
	// THE WHOLE VOCABULARY IS TWO FORMS: a brief, or `solo` and a brief. A first
	// word that is neither of those is simply the beginning of the brief, so the
	// split is taken once here and the brief defaults to everything typed.
	word, rest, _ := strings.Cut(arg, " ")
	rest = strings.TrimSpace(rest)
	brief, solo := arg, false
	switch word {
	case "solo":
		brief, solo = rest, true
	case "adaptive":
		// THE RETIRED WORD IS A WORD NOW AND NOT A ROAD. `/task adaptive` used to
		// open a planned graph over the brief, and ordinary task work does not go
		// that way any more. There are two things this surface must not do about
		// that. It must not GUESS, because guessing means editing somebody's
		// sentence — a brief that genuinely opens "adaptive rate limiting for the
		// api" would lose its first word to a shape nobody asked for — so the words
		// are kept whole and run the one road there is. And it must not stay QUIET,
		// because the person who did mean the old shape would then get something
		// other than what they typed with nothing on screen saying so, which is the
		// one outcome a retirement owes a line about. So: the brief is untouched,
		// and one line says the word steers nothing.
		a.note(taskAdaptiveRetiredNote)
		if rest == "" {
			// Nothing but the retired word is no brief at all, and starting a task
			// called "adaptive" would spend a worker on somebody's muscle memory.
			brief = ""
		}
	}
	if brief == "" {
		a.note("usage: /task <brief> · /task solo <brief>")
		return nil
	}
	// ONE WORKER'S WORK IS SAID TWO WAYS, and both are the person's own word:
	// `/task solo` says it outright for this brief, and a standing `single` in
	// `starting a task` says it in advance for every brief. Either way the engine
	// is told not to read the work for width, which is the only thing solo has
	// left to decide now that nothing is read before the door opens. The work can
	// still divide off what its own brief already enumerates (internal/splitgate),
	// which costs nothing at all.
	solo = solo || config.TaskStartAt(a.profileDir) == config.TaskStartSingle
	return a.startTaskDoor(door, brief, solo)
}

// startTaskDoor hands the work through, and there is no wait to name.
//
// WHAT WAS TRUE: the command waited on two model calls in series before the task
// existed — the sizing judge under `sizing it up…`, then the shaper under
// `shaping the brief…`, with the brief previewed as it was written — and on a
// thinking model both ran their windows out, so every `/task` stood in a forming
// block for twenty-eight seconds (measured 2026-09-11) and then started on the
// person's sentence anyway.
//
// WHAT IS TRUE NOW (#936): the door admits the task at once, and the brief is
// written and the width read BESIDE the task's first worker, inside the engine.
// The answer comes back in milliseconds, so no forming block is raised on this
// road — the task's own row and room are what a person sees next, and a block
// naming a pause that no longer exists would be the surface describing
// machinery rather than work.
func (a *app) startTaskDoor(door taskCommandAgent, brief string, solo bool) tea.Cmd {
	return a.startTaskDoorVia(brief, func(ctx context.Context) (uint64, string, string, error) {
		return door.StartTask(ctx, brief, solo)
	})
}

// taskDoorNotes says who else is in these files before either task door opens,
// and answers which conversation is speaking before the asynchronous answer
// lands. A program works in the folder itself, so only the ordinary task door
// adds the separate note about edits travelling into its copy.
func (a *app) taskDoorNotes(brief string) string {
	// WHICH CONVERSATION IS SAYING THIS, read HERE rather than when the answer
	// lands: the door is opened on a goroutine and the window may have moved on
	// by the time it answers ([app.adoptTypedBrief] is where that matters).
	conv := a.taskBriefConv()
	// WHO ELSE IS ALREADY IN THESE FILES, SAID BEFORE THE SPEND. `/task` shows no
	// proposal card — the person typed the brief, so there is nothing to consent
	// to — which means this note is the only place the fact can reach them, and
	// this is the last line before the door is opened and the work is paid
	// for. It is a note and NOT a gate: the very next statement hands the
	// work over regardless, because a claim another window wrote is evidence and
	// never an instruction (internal/session's taskpreflight.go).
	//
	// The local reading is cached, so this costs a readdir at most once every
	// three seconds (taskview.go's [app.elsewhere]). A HOSTED SURFACE SAYS
	// NOTHING: this seam cannot ask the far roster, and consulting the laptop
	// would describe another machine's work.
	if !a.hosted() {
		if line := session.PreflightNote(a.workspace, a.elsewhere(), brief); line != "" {
			a.note(line)
		}
	}
	return conv
}

// taskCopyDoorNotes adds the ordinary task's copy note after the shared
// preflight. A program's door does not call this: it has no copy to describe.
func (a *app) taskCopyDoorNotes(brief string) string {
	conv := a.taskDoorNotes(brief)
	// WHAT THIS PERSON'S OWN CHECKOUT IS ABOUT TO SEND. The task works in a
	// copy of the folder AS IT STANDS (internal/session's groundladder.go), so
	// half-finished edits go with the work — which is what almost everybody
	// wants and is worth one line for the person who was in the middle of
	// something experimental. Said in the same breath as the line above and for
	// the same reason: this is the last moment before the spend when knowing it
	// can still change what somebody does.
	//
	// It costs one `git status` per start and nothing at all per frame, and it is
	// silent on a clean tree (internal/session's taskpreflight.go).
	if line := session.UnsavedEditsNote(a.workspace); line != "" {
		a.note(line)
	}
	return conv
}

func (a *app) startTaskDoorVia(brief string, start func(context.Context) (uint64, string, string, error)) tea.Cmd {
	ctx := a.ctx
	conv := a.taskCopyDoorNotes(brief)
	return func() tea.Msg {
		id, title, note, err := start(ctx)
		return taskStartedMsg{
			kind: "single", id: strconv.FormatUint(id, 10), title: title,
			err: err, note: note, brief: brief, conv: conv,
		}
	}
}

// taskShapingNote is the forming block's phase word on the one road that still
// raises it: a proposal the person just approved ([app.beginProposalWait]). It
// is named rather than typed at its two ends because the state that begins the
// phase and the test that reads it must agree, and a spelling drift there would
// leave the wrong live phase on screen.
const taskShapingNote = "shaping the brief…"

// taskAdaptiveRetiredNote answers `/task adaptive`, and says both halves of what
// happened: the word no longer picks anything, and it was left exactly where the
// person typed it. It names the road that replaced it — one worker that divides
// off the material once it has read it (internal/session's task_divide.go) — so
// a person who meant the old shape learns what they got instead.
const taskAdaptiveRetiredNote = "/task adaptive retired · the word stays in your brief, and the work starts as one worker that can split as it goes"

// preflight is the one visible thing an approved proposal is becoming: its
// name, its phase, and the moment that phase began.
//
// THE WAIT DOES NOT ENTER THE NOTES LANE. Notes report facts that have landed;
// this scaffold exists only while the task has not yet appeared, and is drawn at
// the transcript tail as a live region. Its left hairline gives every row one
// owner, while the shared spinner and count-up say that owner is still
// changing. The first update for its task clears it before the task's own row
// is drawn, so collapse is one replacement frame rather than a second
// announcement.
type preflight struct {
	note string
	// name is the block's identity line: the approved card's own name, so the
	// block and the card that raised it can never call the work two things.
	name string
	// taskID owns the wait. A proposal's task simply starts existing on the
	// update lane, and the id is how that settle finds its own block and no
	// other ([app.settleProposalWait]).
	taskID uint64
	at     time.Time
}

// waiting reports whether ANY forming block is up. It is the predicate the paint
// clock and the frame ask, and it is a question about the list rather than about
// one wait: a second proposal approved while the first is still forming must not
// let the surface go still when the first one lands.
func (a *app) waiting() bool { return len(a.waits) > 0 }

// beginProposalWait raises the forming block for a proposal card the person just
// answered yes. Between that yes and the task's first update there is a pause
// with nothing yet to point at, and until this existed it was seconds of
// nothing — dead air after a spend the person had just agreed to. The card
// itself stays, because it is a spend gate and not a rendering.
func (a *app) beginProposalWait(card *taskCard) {
	name := card.name
	if name == "" {
		name = card.title
	}
	a.waits = append(a.waits, preflight{
		note: taskShapingNote, name: name, taskID: card.id, at: a.now(),
	})
	a.follow()
	a.touch()
}

// settleProposalWait collapses a proposal's forming block the moment its task
// exists at all. Any update for the id is that moment: queued and running mean
// admitted, and a failure is a fact the task's own machinery announces — the
// block was only ever about the pause before there was anything to point at.
func (a *app) settleProposalWait(id uint64) {
	if id == 0 {
		return
	}
	for i := range a.waits {
		if a.waits[i].taskID == id {
			a.waits = append(a.waits[:i], a.waits[i+1:]...)
			a.touch()
			return
		}
	}
}
