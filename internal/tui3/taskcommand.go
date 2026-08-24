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

type taskCommandAgent interface {
	StartTask(context.Context, string) (uint64, string, error)
	StartPlannerRun(context.Context, string, string) (string, string, error)
	JudgeDecomposable(context.Context, string) (bool, []string, string)
}

type taskSizedMsg struct {
	brief    string
	parallel bool
	parts    []string
	why      string
	// preset is what the person's `starting a task` row said when the command
	// was typed ([config.TaskStartAt]), carried on the message rather than read
	// again on the way back. It is one answer to one command: a row changed while
	// the sizing call was in flight must not settle the command that was already
	// running under the old one.
	preset string
}

// hint is the sketch the sizing call produced, handed to the planner as
// supporting context on the one road that still opens a planner without being
// told to in so many words.
func (m taskSizedMsg) hint() string {
	hint := strings.Join(m.parts, " · ")
	if m.why != "" {
		if hint != "" {
			hint += " · "
		}
		hint += m.why
	}
	return hint
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
	mode, brief := "", arg
	if word, rest, found := strings.Cut(arg, " "); found && (word == "solo" || word == "adaptive") {
		mode, brief = word, strings.TrimSpace(rest)
	}
	if brief == "" {
		a.note("usage: /task <brief> · /task solo <brief> · /task adaptive <brief>")
		return nil
	}
	// AN EXPLICIT WORD IS THE LAST WORD. `/task solo` and `/task adaptive` say
	// the shape outright, so neither the sizing call nor the person's standing
	// answer to it has anything left to decide.
	if mode != "" {
		return a.startTaskDoor(door, mode, brief, "")
	}
	// And where they have said in advance that one worker is what they want and
	// that they do not want the brief read for width first, the sizing call is
	// not made. It is a small call, but it is a call, and its whole remaining
	// product is a road this person has said they would rather not pay to open:
	// the work can still divide off what its own brief already enumerates
	// (internal/splitgate), which costs nothing at all.
	preset := config.TaskStartAt(a.profileDir)
	if preset == config.TaskStartSingle {
		return a.startTaskDoor(door, "single", brief, "")
	}
	a.beginPreflight(taskSizingNote, brief)
	ctx := a.ctx
	return func() tea.Msg {
		parallel, parts, why := door.JudgeDecomposable(ctx, brief)
		return taskSizedMsg{brief: brief, parallel: parallel, parts: parts, why: why, preset: preset}
	}
}

// startTaskDoor hands the work through, and says so while it goes.
//
// THE WAIT IS NAMED BECAUSE IT IS NOT INSTANT ANY MORE. Both doors shape the
// brief before they admit anything (internal/session's task_shape.go), which is
// a model call of its own, and a command that appeared to do nothing for several
// seconds would read as a command that had not registered. The forming block
// says the one true thing about the pause in the same voice `sizing it up…` says
// its own, and [app.settleShaping] collapses it the moment the task lands —
// including when nothing shaped it, because the block was about the attempt.
func (a *app) startTaskDoor(door taskCommandAgent, mode, brief, hint string) tea.Cmd {
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
	a.beginPreflight(taskShapingNote, brief)
	return func() tea.Msg {
		if mode == "adaptive" {
			id, title, err := door.StartPlannerRun(ctx, brief, hint)
			return taskStartedMsg{mode, id, title, err}
		}
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
	at    time.Time
}

// live reports whether a wait is up. It is the frame's eighth reason to paint.
func (p preflight) live() bool { return p.note != "" }

// begin puts the wait on screen and starts its clock.
func (a *app) beginPreflight(note, brief string) {
	a.wait = preflight{note: note, brief: brief, at: a.now()}
	a.follow()
	a.touch()
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
	brief := wrap(strconv.Quote(a.wait.brief), room)
	if len(brief) > 2 {
		brief = brief[:2]
		brief[1] = fit(brief[1], room)
		if !strings.HasSuffix(brief[1], "…") {
			brief[1] = fit(brief[1]+"…", room)
		}
	}
	out := []string{a.pal.dim(rail + "task")}
	for _, row := range brief {
		out = append(out, a.pal.dim(rail+row))
	}
	out = append(out, a.pal.dim(rail+mark+" "+fit(line, room-2)))
	return out
}
