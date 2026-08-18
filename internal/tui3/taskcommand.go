package tui3

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
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
	if mode != "" {
		return a.startTaskDoor(door, mode, brief, "")
	}
	a.note("sizing it up…")
	ctx := a.ctx
	return func() tea.Msg {
		parallel, parts, why := door.JudgeDecomposable(ctx, brief)
		return taskSizedMsg{brief, parallel, parts, why}
	}
}

func (a *app) startTaskDoor(door taskCommandAgent, mode, brief, hint string) tea.Cmd {
	ctx := a.ctx
	return func() tea.Msg {
		if mode == "adaptive" {
			id, title, err := door.StartPlannerRun(ctx, brief, hint)
			return taskStartedMsg{mode, id, title, err}
		}
		id, title, err := door.StartTask(ctx, brief)
		return taskStartedMsg{"single", strconv.FormatUint(id, 10), title, err}
	}
}

func (a *app) settleSizing() {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryNote && a.entries[i].text == "sizing it up…" {
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
