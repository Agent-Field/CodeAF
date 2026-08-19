package tui3

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
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
		a.note("usage: /task <brief> · /task solo <brief> · /task adaptive <brief>")
		return nil
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
	a.note(taskSizingNote)
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
	a.note(taskShapingNote)
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

func (a *app) settleSizing()  { a.dropNote(taskSizingNote) }
func (a *app) settleShaping() { a.dropNote(taskShapingNote) }

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
