package tui3

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

const completionToastDuration = 6 * time.Second
const completionToastKey = "alt+j"

type completionToast struct {
	file              string
	turn              int
	task              uint64
	process           int
	count, incomplete int
	until             time.Time
	serial, armed     uint64
	view, dismiss     hudSpan
}
type completionToastExpiredMsg struct{ serial uint64 }
type heldCompletion struct {
	task       uint64
	process    int
	incomplete bool
}

// Callers must observe the same item's running-to-settled edge. Replay is quiet.
func (a *app) queueCompletionToast(file string, turn int, task uint64, process int, incomplete bool) {
	if !a.focused {
		return
	}
	t := &a.completionToast
	if t.file != file || t.turn != turn || !a.now().Before(t.until) {
		serial := t.serial
		*t = completionToast{file: file, turn: turn, serial: serial}
	}
	t.task, t.process = task, process
	t.count++
	if incomplete {
		t.incomplete++
	}
	t.serial++
	t.until = a.now().Add(completionToastDuration)
	a.touch()
}
func (a *app) completionToastActive() bool {
	return a.focused && a.completionToast.count > 0 && a.now().Before(a.completionToast.until)
}
func (a *app) completionToastAlarm() tea.Cmd {
	t := &a.completionToast
	if !a.completionToastActive() || t.armed == t.serial {
		return nil
	}
	t.armed = t.serial
	serial, wait := t.serial, t.until.Sub(a.now())
	return tea.Tick(wait, func(time.Time) tea.Msg { return completionToastExpiredMsg{serial} })
}
func (a *app) dismissCompletionToast() {
	a.completionToast.count = 0
	a.completionToast.serial++
	a.touch()
}

// Borrow existing clearance or footer; never add a row or move the input.
func (a *app) completionToastLine(width int) string {
	t := &a.completionToast
	t.view, t.dismiss = hudSpan{}, hudSpan{}
	if !a.completionToastActive() || width < 20 {
		return ""
	}
	label := fmt.Sprintf("%d work item finished", t.count)
	if t.count != 1 {
		label = fmt.Sprintf("%d work items finished", t.count)
	}
	if t.incomplete > 0 {
		label += fmt.Sprintf(" · %d incomplete", t.incomplete)
	}
	if t.turn == -1 {
		label = "Reply ready"
	}
	action, end := "View work · "+completionToastKey, " · ×"
	room := width - ansi.StringWidth(action+end) - 4
	if room < 8 {
		action = "View"
		room = width - ansi.StringWidth(action+end) - 4
	}
	label = ansi.Truncate(label, max(0, room), "…")
	prefix := " " + label + " · "
	t.view = hudSpan{from: ansi.StringWidth(prefix), to: ansi.StringWidth(prefix + action)}
	t.dismiss = hudSpan{from: t.view.to + 3, to: t.view.to + 4}
	return fit(a.pal.dim(prefix)+a.pal.accent(action)+a.pal.dim(end), width)
}
func (a *app) completionToastAllowed() bool {
	if !a.completionToastActive() || a.setup.open || a.rew.on || a.awaitingTask() || a.overlayHeight() > 0 {
		return false
	}
	if _, up := a.questionDialog(a.width); up {
		return false
	}
	return true
}
func (a *app) completionToastOver(frame string) string {
	a.completionToastY = -1
	if !a.completionToastAllowed() {
		return frame
	}
	lines := strings.Split(frame, "\n")
	if len(lines) < 2 {
		return frame
	}
	y := len(lines) - 1
	if !a.pageShowing() && !a.jobPageOpen() && !a.roomOpen() && !a.copy.on && !a.railFull() && a.welcomeHeight() == 0 && a.footClearance() > 0 {
		_, marks, _, _ := a.chrome(a.width)
		for i, m := range marks {
			if m.kind == chromeCompletionToast {
				y = len(lines) - len(marks) + i
				break
			}
		}
	}
	line := a.completionToastLine(a.width)
	if line == "" || y < 0 || y >= len(lines) {
		return frame
	}
	lines[y] = line
	a.completionToastY = y
	return strings.Join(lines, "\n")
}
func (a *app) completionToastPress(x, y int) (tea.Cmd, bool) {
	if !a.completionToastAllowed() || y != a.completionToastY {
		return nil, false
	}
	if a.completionToast.dismiss.holds(x) {
		a.dismissCompletionToast()
		return nil, true
	}
	if a.completionToast.view.holds(x) {
		return a.viewCompletionToast(), true
	}
	return nil, false
}
func (a *app) viewCompletionToast() tea.Cmd {
	t := a.completionToast
	a.dismissCompletionToast()
	var cmd tea.Cmd
	if a.convKey(t.file) != a.convKey(a.file) {
		var owned bool
		cmd, owned = a.bringForward(t.file)
		if !owned {
			return cmd
		}
	}
	cmd = tea.Batch(cmd, a.showPage(pageNone))
	a.closeJobPage()
	a.closeRoom()
	a.pendingCompletionReveal = &t
	return tea.Batch(cmd, a.applyCompletionReveal())
}

// A hosted tab loads its journal asynchronously. Keep the person's explicit
// navigation until that reading and its standing roster have arrived.
func (a *app) applyCompletionReveal() tea.Cmd {
	t := a.pendingCompletionReveal
	if t == nil {
		return nil
	}
	if a.convKey(t.file) != a.convKey(a.file) {
		a.pendingCompletionReveal = nil
		return nil
	}
	if a.hostReplayLoading || a.hostReplayWaiting {
		return nil
	}
	if t.turn == -1 {
		a.toLatest()
		a.pendingCompletionReveal = nil
		return nil
	}
	if a.revealCompletionIdentity(t.task, t.process) {
		a.pendingCompletionReveal = nil
		return nil
	}
	turn, owned := t.turn, t.turn > 0
	if t.task != 0 {
		turn, owned = a.workCompletionTurn(t.task)
	}
	if t.process != 0 {
		turn, owned = a.workProcessTurn(t.process)
	}
	if owned {
		a.revealWorkCompletion(turn)
		for _, r := range a.visible(a.bodyWidth()) {
			if r.hit == hitWorkFold && r.turn == turn {
				a.pendingCompletionReveal = nil
				break
			}
		}
	}
	return nil
}

func (w *behindWatch) takeCompletions() []heldCompletion {
	w.workMu.Lock()
	defer w.workMu.Unlock()
	result := w.completions
	w.completions = nil
	return result
}
func taskCompletionState(s session.TaskState) bool {
	return s == session.TaskDone || s == session.TaskFailed
}

func (a *app) workTurnVisible(turn int) bool {
	if a.pageShowing() || a.roomOpen() || a.jobPageOpen() || a.copy.on || a.railFull() {
		return false
	}
	rows := a.visible(a.bodyWidth())
	start := a.offsetFor(len(rows), a.viewHeight())
	for i := start; i < min(len(rows), start+a.viewHeight()); i++ {
		r := rows[i]
		if r.hit == hitWorkFold && r.turn == turn {
			return true
		}
		if r.entry >= 0 && r.entry < len(a.entries) && a.entries[r.entry].turn == turn {
			return true
		}
	}
	return false
}

// A visible answer needs no second signal. A reply outside this view gets the
// same temporary door, without duplicating a task completion already announced.
func (a *app) queueReplyToast(file string) {
	if a.completionToastActive() && a.completionToast.file == file {
		return
	}
	a.queueCompletionToast(file, -1, 0, 0, false)
}

func (a *app) completionIdentityVisible(task uint64, job int, turn int, owned bool) bool {
	if owned {
		return a.workTurnVisible(turn)
	}
	if a.pageShowing() || a.roomOpen() || a.jobPageOpen() || a.copy.on || a.railFull() {
		return false
	}
	keys := map[int]bool{}
	for _, f := range a.deckFolds(a.bodyDeck()) {
		for _, c := range f.completions {
			if task != 0 && c.task == task || job != 0 && c.job == job {
				keys[f.key] = true
			}
		}
	}
	rows := a.visible(a.bodyWidth())
	start := a.offsetFor(len(rows), a.viewHeight())
	for i := start; i < min(len(rows), start+a.viewHeight()); i++ {
		if rows[i].hit == hitWorkFold && keys[rows[i].turn] {
			return true
		}
	}
	return false
}
