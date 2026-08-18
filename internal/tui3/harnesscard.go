package tui3

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// harnessCard is the design thread's one feed object. Live fields are replaced
// by the page when it lands; settled fields remain after the answer.
type harnessCard struct {
	id                         uint64
	goal, phase, hint, thought string
	model                      string
	task                       uint64
	attempt, attempts, bytes   int
	stalled                    bool
	began                      time.Time
	ended                      time.Time
	page                       *subharness.Harness
	state                      string
	buttonRow                  string
}

func (a *app) harnessCardOf(id uint64) (*harnessCard, int) {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if c := a.entries[i].harness; c != nil && c.id == id {
			return c, i
		}
	}
	return nil, -1
}

func (a *app) beginHarnessCard(ev session.Event) {
	a.closeLive()
	c := &harnessCard{id: ev.ID, goal: ev.Text, phase: "designing", hint: "thinking", model: ev.Model, began: time.Now()}
	if ev.Task != nil {
		c.task = ev.Task.ID
	}
	a.entries = append(a.entries, entry{kind: entryHarness, turn: a.turn, harness: c})
	a.follow()
	a.touch()
}

func (a *app) progressHarnessCard(ev session.Event) {
	c, i := a.harnessCardOf(ev.ID)
	if c == nil {
		a.beginHarnessCard(ev)
		c, i = a.harnessCardOf(ev.ID)
	}
	c.goal, c.phase, c.hint, c.thought = ev.Goal, ev.Phase, ev.Hint, ev.ThoughtTail
	c.attempt, c.attempts, c.bytes, c.stalled = ev.Attempt, ev.Attempts, ev.Bytes, ev.Stalled
	a.entries[i].stale = true
	a.follow()
	a.touch()
}

// progressHarnessRoom gives a design's own room the same live edge as its feed
// card. The feed card owns the mapping from a design call to its task node; an
// unknown node or a closed room therefore has nowhere honest to draw and is a
// no-op. This row is never appended to entries, so it can never reach a journal.
func (a *app) progressHarnessRoom(ev session.Event) {
	c, _ := a.harnessCardOf(ev.ID)
	if c == nil || c.task == 0 || a.tasks[c.task] == nil || a.room == nil || a.room.id != c.task {
		return
	}
	parts := []string{"harness · " + firstNonEmpty(ev.Phase, "designing")}
	if ev.Attempts > 1 {
		parts = append(parts, fmt.Sprintf("attempt %d/%d", ev.Attempt, ev.Attempts))
	}
	tail := strings.TrimSpace(ev.ThoughtTail)
	if tail != "" {
		tail = firstLineOf(tail)
	} else {
		tail = strings.TrimSpace(ev.Hint)
	}
	if tail != "" {
		parts = append(parts, tail)
	}
	a.room.harnessProgress = strings.Join(parts, " · ")
	a.roomTouched()
}

func (a *app) finishHarnessCard(ev session.Event) {
	c, i := a.harnessCardOf(ev.ID)
	if c == nil {
		a.beginHarnessCard(ev)
		c, i = a.harnessCardOf(ev.ID)
	}
	page := *ev.Harness
	c.page, c.phase, c.hint, c.thought = &page, "", "", ""
	c.ended = time.Now()
	a.entries[i].stale = true
	a.follow()
	a.touch()
}

func (a *app) harnessFeedRows(c *harnessCard, width int, selected bool) []string {
	if c == nil {
		return nil
	}
	if c.page == nil {
		head := "⠿ harness · " + c.phase
		switch {
		case c.model != "" && c.goal != "":
			head += " with " + c.model + " · " + c.goal
		case c.model != "":
			head += " with " + c.model
		case c.goal != "":
			head += " " + c.goal
		}
		if c.task != 0 {
			head += fmt.Sprintf(" — task %d", c.task)
		}
		if c.attempts > 1 {
			head += fmt.Sprintf(" · attempt %d/%d", c.attempt, c.attempts)
		}
		if c.stalled {
			head += fmt.Sprintf(" · thinking · %ds — reasoning models answer in one burst at the end", int(time.Since(c.began).Seconds()))
		}
		out := []string{a.pal.dim(fit(head, width))}
		if tail := strings.TrimSpace(c.thought); tail != "" {
			lines := wrap(tail, width-2)
			if len(lines) > 3 {
				lines = lines[len(lines)-3:]
			}
			for _, line := range lines {
				out = append(out, a.pal.dim("  "+line))
			}
		}
		if c.hint != "" {
			out = append(out, a.pal.dim(fit("  "+c.hint, width)))
		}
		return out
	}
	inner := width - 4
	if inner < 12 {
		inner = 12
	}
	name := c.page.Id.Name
	version := c.page.Id.Version
	if version == 0 {
		version = 1
	}
	head := "┌─ harness designed " + strings.Repeat("─", max(1, inner-24)) + fmt.Sprintf(" v%d ─┐", version)
	rows := []string{fit(head, width), boxLine(name, inner), boxLine(c.page.Id.Desc, inner), boxLine("", inner)}
	for _, line := range harnessDiagram(*c.page, inner, layoutTier(width) == tierPhone) {
		rows = append(rows, boxLine(line, inner))
	}
	rows = append(rows, boxLine("", inner))
	for _, line := range harnessPoints(*c.page) {
		for _, part := range wrap(line, inner) {
			rows = append(rows, boxLine(part, inner))
		}
	}
	if !c.began.IsZero() {
		end := c.ended
		if end.IsZero() {
			end = time.Now()
		}
		rows = append(rows, boxLine("thought for "+taskSpanWord(end.Sub(c.began)), inner))
	}
	actions := "[enter] save   [e] improve   [esc] drop"
	if c.state != "" {
		actions = c.state
	}
	c.buttonRow = actions
	rows = append(rows, boxLine(actions, inner), "└"+strings.Repeat("─", inner+2)+"┘")
	if selected {
		rows[0] = a.pal.accent(rows[0])
	}
	return rows
}

func boxLine(s string, width int) string {
	return "│ " + fit(s, width) + strings.Repeat(" ", max(0, width-ansi.StringWidth(fit(s, width)))) + " │"
}

func harnessPoints(h subharness.Harness) []string {
	out := make([]string, 0, len(h.Program.Nodes)+2)
	for _, n := range h.Program.Nodes {
		detail := firstNonEmpty(n.Fields.Get("brief"), n.Fields.Get("check"), n.Fields.Get("tool"), n.Kind)
		out = append(out, "• "+n.Id+": "+firstLineOf(detail))
	}
	if h.Verify.Ladder != "" {
		out = append(out, "verify: "+h.Verify.Ladder)
	}
	if len(h.Whitelist) > 0 {
		out = append(out, "tools: "+strings.Join(h.Whitelist, " · "))
	}
	return out
}

// harnessDiagram lays nodes by dependency depth. A single path uses the compact
// horizontal form; branches use stable stacked rows; phones always rotate the
// same order vertically.
func harnessDiagram(h subharness.Harness, width int, phone bool) []string {
	ids := make([]string, 0, len(h.Program.Nodes))
	for _, n := range h.Program.Nodes {
		ids = append(ids, n.Id)
	}
	if len(ids) == 0 {
		return nil
	}
	linear := len(h.Program.Edges) == len(ids)-1
	if phone {
		out := []string{}
		for i, id := range ids {
			out = append(out, "┌"+strings.Repeat("─", min(14, max(4, len(id))))+"┐", "│ "+fit(id, min(12, max(2, width-4)))+" │", "└"+strings.Repeat("─", min(14, max(4, len(id))))+"┘")
			if i < len(ids)-1 {
				out = append(out, "▼")
			}
		}
		return out
	}
	if !linear {
		pred, succ := map[string]int{}, map[string][]string{}
		for _, edge := range h.Program.Edges {
			succ[edge.From()] = append(succ[edge.From()], edge.To())
			pred[edge.To()]++
		}
		for _, id := range ids {
			if len(succ[id]) < 2 {
				continue
			}
			out := []string{"[" + fit(id, 12) + "]"}
			for i, branch := range succ[id] {
				arm := "├──▶ "
				if i == len(succ[id])-1 {
					arm = "└──▶ "
				}
				out = append(out, arm+"["+fit(branch, 12)+"]")
			}
			for _, join := range ids {
				if pred[join] > 1 {
					out = append(out, "      ▼", "    ["+fit(join, 12)+"]")
					break
				}
			}
			return out
		}
	}
	boxes := make([]string, len(ids))
	for i, id := range ids {
		boxes[i] = "[" + fit(id, 12) + "]"
	}
	return []string{strings.Join(boxes, "──▶")}
}

func (a *app) harnessCardKey(msg tea.KeyPressMsg) bool {
	if a.sel < 0 || a.sel >= len(a.entries) {
		return false
	}
	c := a.entries[a.sel].harness
	if c == nil || c.page == nil || c.state != "" {
		return false
	}
	switch msg.String() {
	case "enter":
		a.resolveHarnessCard(c, "save")
	case "e":
		a.resolveHarnessCard(c, "improve")
	case "esc":
		a.resolveHarnessCard(c, "drop")
	default:
		return false
	}
	return true
}

func (a *app) resolveHarnessCard(c *harnessCard, action string) {
	if action == "save" {
		if a.agent != nil {
			a.agent.ResolveHarness(c.id, true, "")
		}
		c.state = "saved as " + c.page.Id.Name + " v1"
	}
	if action == "drop" {
		if a.agent != nil {
			a.agent.ResolveHarness(c.id, false, "")
		}
		c.state = "dropped"
	}
	if action == "improve" {
		if a.agent != nil {
			a.agent.ResolveHarness(c.id, false, "")
		}
		a.input.setText("Improve harness " + c.page.Id.Name + ": ")
		c.state = "improvement requested"
	}
	a.touch()
}

func (a *app) harnessCardPress(i, x int) {
	if i < 0 || i >= len(a.entries) {
		return
	}
	c := a.entries[i].harness
	if c == nil || c.page == nil || c.state != "" {
		return
	}
	if x < 15 {
		a.resolveHarnessCard(c, "save")
	} else if x < 30 {
		a.resolveHarnessCard(c, "improve")
	} else {
		a.resolveHarnessCard(c, "drop")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
