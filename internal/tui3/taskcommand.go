package tui3

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// taskCommandAgent is the whole of what /task needs from the session, and it is
// two calls now that ordinary work has ONE ROAD. The planner door it used to
// carry is gone from the engine too, along with the `/task adaptive` that was its
// only caller; the planner ENGINE is untouched and a person can still name a run
// outright in the conversation, which the session reads for itself. Either way
// the verb is not in this surface's vocabulary — a door listed here that no
// command opens is an invitation to open it again.
type taskCommandAgent interface {
	StartTask(context.Context, string) (uint64, string, error)
	JudgeDecomposable(context.Context, string) (bool, []string, string)
}

// taskSizedMsg is the sizing call's answer on its way back to the surface: the
// brief that was read, and whether there was more than one job in it.
//
// IT CARRIES NEITHER A SKETCH NOR A ROW ANY MORE. The parts the judge named and
// its reason for naming them were supporting context for a planner, and the
// person's `starting a task` row decided which road the answer opened — and
// there is one road, so both are questions with no reader. What survives of the
// sketch is what it was always standing in for: a yes ARMS the worker to divide
// off the material itself (internal/session's task_divide.go), from evidence
// rather than from a guess made before anybody opened the files.
type taskSizedMsg struct {
	brief    string
	parallel bool
}

type taskStartedMsg struct {
	kind, id, title string
	err             error
}

func (a *app) runTaskCommand(arg string) tea.Cmd {
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
			// Nothing but the retired word is no brief at all, and shaping a task
			// called "adaptive" would spend a model call on somebody's muscle memory.
			brief = ""
		}
	}
	if brief == "" {
		a.note("usage: /task <brief> · /task solo <brief>")
		return nil
	}
	// AN EXPLICIT SOLO IS THE LAST WORD. `/task solo` says outright that this is
	// one worker and nothing is to be read for width first, so neither the sizing
	// call nor the person's standing answer to it has anything left to decide.
	if solo {
		return a.startTaskDoor(door, brief)
	}
	// And where they have said in advance that one worker is what they want and
	// that they do not want the brief read for width first, the sizing call is
	// not made. It is a small call, but it is a call, and its whole remaining
	// product is a road this person has said they would rather not pay to open:
	// the work can still divide off what its own brief already enumerates
	// (internal/splitgate), which costs nothing at all.
	preset := config.TaskStartAt(a.profileDir)
	if preset == config.TaskStartSingle {
		return a.startTaskDoor(door, brief)
	}
	a.beginPreflight(taskSizingNote, brief)
	ctx := a.ctx
	return func() tea.Msg {
		parallel, _, _ := door.JudgeDecomposable(ctx, brief)
		return taskSizedMsg{brief: brief, parallel: parallel}
	}
}

// startTaskDoor hands the work through, and says so while it goes.
//
// THE WAIT IS NAMED BECAUSE IT IS NOT INSTANT ANY MORE. The door shapes the
// brief before it admits anything (internal/session's task_shape.go), which is
// a model call of its own, and a command that appeared to do nothing for several
// seconds would read as a command that had not registered. The forming block
// says the one true thing about the pause in the same voice `sizing it up…` says
// its own, and [app.settleShaping] collapses it the moment the task lands —
// including when nothing shaped it, because the block was about the attempt.
func (a *app) startTaskDoor(door taskCommandAgent, brief string) tea.Cmd {
	ctx := a.ctx
	// WHO ELSE IS ALREADY IN THESE FILES, SAID BEFORE THE SPEND. `/task` shows no
	// proposal card — the person typed the brief, so there is nothing to consent
	// to — which means this note is the only place the fact can reach them, and
	// this is the last line before the door is opened and the shaping call is
	// paid for. It is a note and NOT a gate: the very next statement hands the
	// work over regardless, because a claim another window wrote is evidence and
	// never an instruction (internal/session's taskpreflight.go).
	//
	// The reading is the cached one the roster already keeps, so this costs a
	// readdir at most once every three seconds (taskview.go's [app.elsewhere]).
	if line := session.PreflightNote(a.workspace, a.elsewhere(), brief); line != "" {
		a.note(line)
	}
	// AND WHAT THIS PERSON'S OWN CHECKOUT IS ABOUT TO NOT SEND. The task works in
	// a copy cut from the last commit, so unsaved edits stay in front of the
	// person who made them — and "the task sees what I see" is what everybody
	// assumes until a worker reports the file as it was this morning. Said in the
	// same breath as the line above and for the same reason: this is the last
	// moment before the spend when knowing it can still change what somebody does.
	//
	// It costs one `git status` per start and nothing at all per frame, and it is
	// silent on a clean tree (internal/session's taskpreflight.go).
	if line := session.UnsavedEditsNote(a.workspace); line != "" {
		a.note(line)
	}
	a.beginPreflight(taskShapingNote, brief)
	return func() tea.Msg {
		id, title, err := door.StartTask(ctx, brief)
		return taskStartedMsg{"single", strconv.FormatUint(id, 10), title, err}
	}
}

// The two phase words are named rather than typed at their two ends because the
// state that begins a phase and the state that settles it must agree. A spelling
// drift there would leave the wrong live phase on screen or fail to clear it.
const (
	taskSizingNote  = "sizing it up…"
	taskShapingNote = "shaping the brief…"
)

// taskWideNote is what the sizing call's yes says now that it opens nothing.
//
// IT IS A FACT AND NOT A WAIT, so it is written once and never taken back: a
// question was asked about this brief before the work started, the answer was
// that there is more than one job in it, and that answer is what lets the worker
// hand the parts out later (internal/session's task_divide.go). The person is
// told because the roster is about to grow rows nobody typed a command for, and
// a task that quietly becomes four tasks is a surface doing something unannounced.
//
// It says only what is true at the moment it is written. The worker still has to
// open the material, find the width is real and find a free hand before anything
// is handed out, so the line promises a possibility — "can split" — rather than
// a plan, and the transcript's own words for the split (`split into 3 parts:`)
// are what say it happened.
const taskWideNote = "the work looks wide · one worker starts, and it can split as it goes"

// taskAdaptiveRetiredNote answers `/task adaptive`, and says both halves of what
// happened: the word no longer picks anything, and it was left exactly where the
// person typed it. It names the road that replaced it in the same words
// [taskWideNote] uses, because they are the same road and a person who reads
// both should not have to work out that they are.
const taskAdaptiveRetiredNote = "/task adaptive retired · the word stays in your brief, and the work starts as one worker that can split as it goes"

// preflight is the one visible thing a task command is becoming: the person's
// words, its present phase, and the moment that phase began.
//
// THE WAIT DOES NOT ENTER THE NOTES LANE. Notes report facts that have landed;
// this scaffold exists only while a command is in flight and is drawn at the
// transcript tail as a live region. Its left hairline gives every row one owner,
// while the shared spinner and count-up say that owner is still changing. When
// the door answers, the state is cleared before the ordinary settled task or
// error row is written, so collapse is one replacement frame rather than a
// second announcement.
type preflight struct {
	note  string
	brief string
	// name is the block's identity line where there are no person-words to
	// quote: a PROPOSAL the person just approved. It is the card's own name, so
	// the block and the card that raised it can never call the work two things.
	name string
	// taskID owns the wait on the proposal road, and zero is the typed command's
	// wait. The two roads settle differently — a command's door answers with a
	// message, a proposal's task simply starts existing on the update lane — and
	// the id is how the second settle finds its own block and no other.
	taskID uint64
	at     time.Time
}

// live reports whether a wait is up. It is the frame's eighth reason to paint.
func (p preflight) live() bool { return p.note != "" }

// begin puts the wait on screen and starts its clock.
func (a *app) beginPreflight(note, brief string) {
	a.wait = preflight{note: note, brief: brief, at: a.now()}
	a.follow()
	a.touch()
}

// beginProposalWait is the SAME forming block raised by the OTHER door: a
// proposal card the person just answered yes. The engine shapes the brief
// before the task exists (internal/session's task_shape.go), which is the same
// pause the typed command stands in — and until this existed the yes was
// followed by seconds of nothing, the exact dead air the block was built to
// end. One forming vocabulary, two lawful entrances; the card itself stays,
// because it is a spend gate and not a rendering.
func (a *app) beginProposalWait(card *taskCard) {
	name := card.name
	if name == "" {
		name = card.title
	}
	a.wait = preflight{note: taskShapingNote, name: name, taskID: card.id, at: a.now()}
	a.follow()
	a.touch()
}

// settleProposalWait collapses a proposal's forming block the moment its task
// exists at all. Any update for the id is that moment: queued and running mean
// admitted, and a failure is a fact the task's own machinery announces — the
// block was only ever about the pause before there was anything to point at.
func (a *app) settleProposalWait(id uint64) {
	if id != 0 && a.wait.taskID == id {
		a.wait = preflight{}
		a.touch()
	}
}

// endPreflight stops the clock and takes the line away. The two halves are one
// call because they are one fact — this is no longer happening — and a surface
// that dropped the note while leaving the clock running would keep asking for
// frames forever on behalf of a row nobody can see.
func (a *app) endPreflight(note string) {
	if a.wait.note == note {
		a.wait = preflight{}
	}
	a.touch()
}

func (a *app) settleSizing()  { a.endPreflight(taskSizingNote) }
func (a *app) settleShaping() { a.endPreflight(taskShapingNote) }

// preflightRows draws the forming block at the transcript tail.
//
//	▏ task
//	▏ "write the release notes"
//	▏ ⠙ shaping the brief… · 6s
//
// ONE HAIRLINE AND ONE SPACE IS THE WHOLE SCAFFOLD. The brief is quoted because
// it is the person's verbatim input, and is capped at two fitted rows so a long
// command cannot turn a transient wait into a transcript card. The count-up is
// [countUpWord], which floors under a second by the emptiness law.
func (a *app) preflightRows(width int) []string {
	if !a.wait.live() || width < 3 {
		return nil
	}
	// The linear tier's objection to a spinner is the one it makes on a tool
	// line: a claim repeated thirty times a second is heard thirty times a second
	// by a surface being read aloud. A still mark makes it once.
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear {
		mark = glyphRunASCII
	}
	line := a.wait.note
	if word := countUpWord(a.now().Sub(a.wait.at)); word != "" {
		line += " · " + word
	}
	rail := "▏ "
	room := width - 2
	// The identity line is the person's words when there are person's words —
	// quoted, because they are verbatim — and the task's own name when the block
	// was raised by an approved proposal, plain, because the name is the
	// surface's word and wearing quotes would claim somebody typed it.
	var identity []string
	switch {
	case a.wait.brief != "":
		identity = wrap(strconv.Quote(a.wait.brief), room)
		if len(identity) > 2 {
			identity = identity[:2]
			identity[1] = fit(identity[1], room)
			if !strings.HasSuffix(identity[1], "…") {
				identity[1] = fit(identity[1]+"…", room)
			}
		}
	case a.wait.name != "":
		identity = []string{fit(a.wait.name, room)}
	}
	out := []string{a.pal.dim(rail + "task")}
	for _, row := range identity {
		out = append(out, a.pal.dim(rail+row))
	}
	out = append(out, a.pal.dim(rail+mark+" "+fit(line, room-2)))
	return out
}
