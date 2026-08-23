package tui3

import (
	"context"
	"fmt"
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
	TaskPlannerModel() string
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

// hint is the sketch the sizing call produced, in the one spelling both readers
// of it use — the chooser's adaptive row and the planner's supporting context.
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

type taskChooser struct {
	open         bool
	cursor       int
	brief, hint  string
	parts        []string
	why, planner string
}

func (p *taskChooser) close()         { *p = taskChooser{} }
func (p *taskChooser) move(delta int) { p.cursor = (p.cursor + delta + 2) % 2 }
func (p *taskChooser) height() int {
	if p.open {
		return 4
	}
	return 0
}

func (p *taskChooser) rows(width, n int, pal palette, hover int) []string {
	if !p.open || n <= 0 {
		return nil
	}
	adaptive := fmt.Sprintf("adaptive · ~%d parts · planner %s", len(p.parts), p.planner)
	if len(p.parts) == 0 {
		adaptive = "adaptive · planner " + p.planner
	}
	sketch := strings.Join(p.parts, " · ")
	if p.why != "" {
		sketch += " · " + p.why
	}
	labels := []string{adaptive, "single · one worker, no planner"}
	out := []string{pal.dim("this parallelizes — how should it run?")}
	for i, label := range labels {
		lead := "  "
		if i == p.cursor {
			lead = "› "
		}
		head := lead + fit(label, width-2)
		if i == p.cursor {
			head = pal.band(pal.ink(head), width)
		} else if hover == len(out) {
			head = pal.hover(pal.dim(head), width)
		} else {
			head = pal.dim(head)
		}
		out = append(out, head)
		if i == 0 {
			tail := "    " + fit(sketch, width-4)
			if p.cursor == 0 {
				tail = pal.band(pal.dim(tail), width)
			} else if hover == len(out) {
				tail = pal.hover(pal.dim(tail), width)
			} else {
				tail = pal.dim(tail)
			}
			out = append(out, tail)
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
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
	// And where they have said in advance that one worker is what they want, the
	// sizing call is not made: its only product is the chooser and the adaptive
	// sketch, and paying a model to answer a question already answered would be
	// spending on an answer that is going to be ignored.
	preset := config.TaskStartAt(a.profileDir)
	if preset == config.TaskStartSingle {
		return a.startTaskDoor(door, "single", brief, "")
	}
	a.beginPreflight(taskSizingNote)
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
// seconds would read as a command that had not registered. The note says the one
// true thing about the pause in the same voice `sizing it up…` says its own, and
// [app.settleShaping] takes it away the moment the task lands — including when
// nothing shaped it, because the note was about the attempt.
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
	a.beginPreflight(taskShapingNote)
	return func() tea.Msg {
		if mode == "adaptive" {
			id, title, err := door.StartPlannerRun(ctx, brief, hint)
			return taskStartedMsg{mode, id, title, err}
		}
		id, title, err := door.StartTask(ctx, brief)
		return taskStartedMsg{"single", strconv.FormatUint(id, 10), title, err}
	}
}

// The two waiting notes. They are named rather than typed at their two ends
// because each is written once and REMOVED by matching the same text: a note
// whose spelling drifted between the writer and the remover is a line that
// stays on screen for the rest of the session.
const (
	taskSizingNote  = "sizing it up…"
	taskShapingNote = "shaping the brief…"
)

// preflight is the wait a task command is standing in: which of the two notes
// above is on screen, and the moment it went up.
//
// THE DEFECT THIS FIXES: both notes were plain notes, and a note is the lane
// this surface says FINISHED things in — `exported · …`, `⟲ 135.7k cached ·
// saved $0.0069`. Dim, static, and cached like every other note (render.go's
// [app.entryRows]), so `shaping the brief…` was a still photograph for the
// twenty-five seconds the shaping call is allowed (internal/session's
// task_shape.go), sitting in a stack of post-hoc telemetry with nothing to
// distinguish it from the lines above it that were about work already over.
// Worse, no frame was even being ASKED for while it was up: the seven reasons
// this surface keeps painting with the model idle ([app.paint]) did not include a
// command's pre-flight, so the surface genuinely stopped. From where the person sat, a command they had just typed
// had done nothing and then kept doing nothing.
//
// So the wait is drawn the way every other genuinely in-flight thing on this
// surface is drawn — the braille spinner and the count-up the tool lines and a
// running compaction already wear (render.go's [app.compactRow]) — and the frame
// keeps turning while it is up. It borrows both rather than inventing an
// animation, for [app.compactRow]'s stated reason: somebody who has learned that
// a spinner means "this is happening right now" has learned it here too, and
// both turn on the same [spinnerStep] grid so two moving rows never beat against
// each other.
type preflight struct {
	note string
	at   time.Time
}

// live reports whether a wait is up. It is the frame's eighth reason to paint.
func (p preflight) live() bool { return p.note != "" }

// begin puts the wait on screen and starts its clock.
func (a *app) beginPreflight(note string) {
	a.wait = preflight{note: note, at: a.now()}
	a.note(note)
}

// endPreflight stops the clock and takes the line away. The two halves are one
// call because they are one fact — this is no longer happening — and a surface
// that dropped the note while leaving the clock running would keep asking for
// frames forever on behalf of a row nobody can see.
func (a *app) endPreflight(note string) {
	if a.wait.note == note {
		a.wait = preflight{}
	}
	a.dropNote(note)
}

func (a *app) settleSizing()  { a.endPreflight(taskSizingNote) }
func (a *app) settleShaping() { a.endPreflight(taskShapingNote) }

// waiting reports whether this note is the wait that is in flight right now,
// rather than one of the finished facts the same lane carries.
func (a *app) waiting(e *entry) bool {
	return a.wait.live() && e.kind == entryNote && e.text == a.wait.note
}

// preflightRows draws the wait: the spinner, the note's own words, and how long
// it has been going.
//
//	⠙ shaping the brief… · 6s
//
// It keeps the note lane's hanging indent — the spinner stands exactly where the
// `· ` would, two cells, and every continuation lines up under the words — so a
// wait that wraps at a narrow width is still one block rather than a ragged
// clump. The count-up is [countUpWord], which floors under a second: a wait that
// has only just started says nothing about its length, by the emptiness law.
func (a *app) preflightRows(e *entry, width int) []string {
	// The linear tier's objection to a spinner is the one it makes on a tool
	// line: a claim repeated thirty times a second is heard thirty times a second
	// by a surface being read aloud. A still mark makes it once.
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear {
		mark = glyphRunASCII
	}
	line := e.text
	if word := countUpWord(a.now().Sub(a.wait.at)); word != "" {
		line += " · " + word
	}
	body := wrap(line, width-2)
	out := make([]string, 0, len(body))
	for i, row := range body {
		lead := mark + " "
		if i > 0 {
			lead = "  "
		}
		out = append(out, a.pal.dim(lead+row))
	}
	return out
}

// dropNote takes the most recent note with this exact text back off the
// transcript. It is how a line that said what was happening leaves when it has
// stopped being true, rather than being rewritten in place — a note is a thing
// the conversation said, and the honest end of "sizing it up…" is that it is no
// longer sizing anything up.
func (a *app) dropNote(text string) {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryNote && a.entries[i].text == text {
			a.entries = append(a.entries[:i], a.entries[i+1:]...)
			return
		}
	}
}

func (a *app) taskChooserKey(msg tea.KeyPressMsg) tea.Cmd {
	door, ok := a.agent.(taskCommandAgent)
	if !ok {
		a.taskPick.close()
		return nil
	}
	switch msg.String() {
	case "up", "ctrl+p":
		a.taskPick.move(-1)
	case "down", "ctrl+n":
		a.taskPick.move(1)
	case "enter":
		p := a.taskPick
		a.taskPick.close()
		if p.cursor == 0 {
			return a.startTaskDoor(door, "adaptive", p.brief, p.hint)
		}
		return a.startTaskDoor(door, "single", p.brief, "")
	case "esc":
		p := a.taskPick
		a.taskPick.close()
		return a.startTaskDoor(door, "single", p.brief, "")
	}
	a.touch()
	return nil
}
