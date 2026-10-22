package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func toastFixture() *app {
	a := newTestApp(&fakeAgent{model: "m"})
	a.file = "/tmp/toast/session/transcript.jsonl"
	a.focused = true
	a.entries = foldFixture()
	a.turn = 1
	a.closeHome()
	a.dismissWelcome()
	return a
}
func TestCompletionToastCoalescesExpiresAndDoesNotMoveComposer(t *testing.T) {
	a := toastFixture()
	now := time.Unix(100, 0)
	a.clock = func() time.Time { return now }
	before, _, _, _ := a.chrome(a.width)
	a.queueCompletionToast(a.file, 1, 7, 0, false)
	first := a.completionToast.serial
	a.queueCompletionToast(a.file, 1, 8, 0, true)
	frame, _, _ := a.frame()
	if !strings.Contains(ansi.Strip(frame), "2 work items finished") {
		t.Fatalf("missing burst toast:\n%s", ansi.Strip(frame))
	}
	after, _, _, _ := a.chrome(a.width)
	if len(before) != len(after) {
		t.Fatal("toast moved composer")
	}
	a.route(completionToastExpiredMsg{first})
	if !a.completionToastActive() {
		t.Fatal("old timer dismissed newer completion")
	}
	now = now.Add(completionToastDuration)
	if a.completionToastActive() {
		t.Fatal("toast outlived six seconds")
	}
	frame, _, _ = a.frame()
	if strings.Contains(ansi.Strip(frame), "View work") {
		t.Fatal("expired toast still drawn")
	}
}
func TestCompletionToastViewClickOpensOwningFoldAndPreservesDraft(t *testing.T) {
	a := toastFixture()
	a.input.setText("keep this draft")
	a.queueCompletionToast(a.file, 1, 0, 0, false)
	a.frame()
	x, y := a.completionToast.view.from, a.completionToastY
	_, took := a.completionToastPress(x, y)
	if !took || !a.workOpen[1] {
		t.Fatalf("View click did not open originating work: took=%v open=%v", took, a.workOpen)
	}
	if a.input.String() != "keep this draft" {
		t.Fatal("toast changed draft")
	}
	if a.completionToastActive() {
		t.Fatal("View did not dismiss toast")
	}
}
func TestCompletionToastDismissClickAndUnfocusedSilence(t *testing.T) {
	a := toastFixture()
	a.queueCompletionToast(a.file, 1, 0, 0, false)
	a.frame()
	if _, took := a.completionToastPress(a.completionToast.dismiss.from, a.completionToastY); !took {
		t.Fatal("dismiss target missed")
	}
	if a.completionToastActive() {
		t.Fatal("dismiss failed")
	}
	a.focused = false
	a.queueCompletionToast(a.file, 1, 1, 0, false)
	if a.completionToast.count != 0 {
		t.Fatal("unfocused terminal queued in-app toast")
	}
}
func TestHeldCompletionOnlySignalsObservedEdgesAndDeduplicates(t *testing.T) {
	w := &behindWatch{}
	terminal := &session.TaskNotice{ID: 7, State: session.TaskDone}
	w.noteTask(terminal)
	if len(w.takeCompletions()) != 0 {
		t.Fatal("old task replay raised toast")
	}
	w.noteTask(&session.TaskNotice{ID: 7, State: session.TaskRunning})
	w.noteTask(terminal)
	w.noteTask(terminal)
	got := w.takeCompletions()
	if len(got) != 1 || got[0].task != 7 {
		t.Fatalf("task edge duplicated/lost: %+v", got)
	}
	w.noteJob(&session.JobNotice{ID: 4, State: session.JobDone})
	if len(w.takeCompletions()) != 0 {
		t.Fatal("old process replay raised toast")
	}
	w.noteJob(&session.JobNotice{ID: 4, State: session.JobRunning})
	w.noteJob(&session.JobNotice{ID: 4, State: session.JobFailed})
	w.noteJob(&session.JobNotice{ID: 4, State: session.JobFailed})
	got = w.takeCompletions()
	if len(got) != 1 || got[0].process != 4 || !got[0].incomplete {
		t.Fatalf("process edge duplicated/lost: %+v", got)
	}
}
func TestCompletionToastWidthNeverOverflows(t *testing.T) {
	a := toastFixture()
	a.queueCompletionToast(a.file, 1, 1, 0, true)
	for _, width := range []int{20, 24, 32, 60, 120} {
		line := a.completionToastLine(width)
		if ansi.StringWidth(line) > width || a.completionToast.dismiss.to > width {
			t.Fatalf("toast actions clipped at width %d: %q", width, ansi.Strip(line))
		}
	}
}

func TestLiveCompletionToastOnlyWhenOriginalWorkIsOutOfSight(t *testing.T) {
	for _, away := range []bool{false, true} {
		a := toastFixture()
		if away {
			a.showPage(pageSettings)
		}
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Check parser", State: session.TaskRunning}})
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Check parser", State: session.TaskDone}})
		a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Check parser", State: session.TaskDone}})
		if away && a.completionToast.count != 1 {
			t.Fatalf("offscreen completion not signalled exactly once: %+v", a.completionToast)
		}
		if !away && a.completionToast.count != 0 {
			t.Fatal("watched work raised redundant toast")
		}
	}
	a := toastFixture()
	a.showPage(pageSettings)
	a.taskUpdate(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 7, Title: "Old task", State: session.TaskDone}})
	if a.completionToast.count != 0 {
		t.Fatal("restored terminal event raised toast")
	}
}

func TestReplyToastDoesNotDuplicateTaskSignalAndViewPreservesDraft(t *testing.T) {
	a := toastFixture()
	a.queueCompletionToast(a.file, 1, 7, 0, false)
	a.queueReplyToast(a.file)
	if a.completionToast.count != 1 || a.completionToast.task != 7 {
		t.Fatal("reply duplicated task completion signal")
	}
	a.dismissCompletionToast()
	a.input.setText("draft stays")
	a.queueReplyToast(a.file)
	frame, _, _ := a.frame()
	if !strings.Contains(ansi.Strip(frame), "Reply ready") {
		t.Fatal("reply completion not labelled")
	}
	a.viewCompletionToast()
	if a.input.String() != "draft stays" || !a.stick {
		t.Fatal("View reply changed draft or failed to reveal latest answer")
	}
}

func TestHeldDecisionAndCreditStopDoNotRaiseRoutineCompletionToast(t *testing.T) {
	a := toastFixture()
	file := "/tmp/asking/session/transcript.jsonl"
	w := &behindWatch{}
	held := &kept{conv: Conversation{Agent: askingAgent{&fakeAgent{model: "m"}}, SessionFile: file}, watch: w}
	a.behind = map[string]*kept{a.convKey(file): held}
	w.noteTask(&session.TaskNotice{ID: 7, State: session.TaskRunning})
	w.noteTask(&session.TaskNotice{ID: 7, State: session.TaskDone})
	a.behindStir(behindStirMsg{key: a.convKey(file)})
	if a.completionToastActive() {
		t.Fatal("persistent question got routine finished toast")
	}
	w = &behindWatch{}
	w.noteTask(&session.TaskNotice{ID: 8, State: session.TaskRunning})
	w.noteTask(&session.TaskNotice{ID: 8, State: session.TaskFailed, Crew: &crewroute.Decision{Stopped: "add credits to continue"}})
	if len(w.takeCompletions()) != 0 {
		t.Fatal("actionable credit stop became routine completion")
	}
}
