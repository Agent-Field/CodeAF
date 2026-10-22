package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func completionFoldApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := taskApp(t)
	a.width, a.height = 200, 80
	a.entries, a.workMode = foldFixture(), config.WorkFold
	a.turn = 1
	// The fixture models a real recorded task proposal, not an unrecorded slash command.
	a.entries = append(a.entries, entry{kind: entryTask, turn: 1, card: &taskCard{id: 7, title: "Recorded task", verdict: taskClockWord}})
	return a
}

func TestLateTaskUpdatesOriginalWorkedGroupAndItsResultDoor(t *testing.T) {
	a := completionFoldApp(t)
	a.taskUpdate(update(7, "Compile parser", session.TaskRunning, session.TaskNotice{}))
	a.turn = 2
	a.entries = append(a.entries, entry{kind: entryUser, turn: 2, text: "Discuss a different issue"}, entry{kind: entryAssistant, turn: 2, text: "The new answer remains visible.", settled: true})
	a.taskUpdate(update(7, "Compile parser", session.TaskDone, session.TaskNotice{Report: "Compiled successfully", Result: "Original deliverable"}))
	for _, f := range a.deckFolds(a.conversation()) {
		if len(f.completions) > 0 && f.turn != 1 {
			t.Fatal("completion attached to newer question")
		}
	}
	collapsed := strings.Join(plainRows(a), "\n")
	if strings.Contains(collapsed, "Compile parser") || !strings.Contains(collapsed, "The new answer remains visible.") {
		t.Fatal(collapsed)
	}
	a.openWorkfold(1)
	a.touch()
	rows := a.visible(a.bodyWidth())
	at := -1
	for i, r := range rows {
		if r.hit == hitWorkCompletion && r.completion.task == 7 {
			if at >= 0 {
				t.Fatal("duplicate task result rows")
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatal("expanded original work has no result door")
	}
	a.stick = false
	a.offset = 0
	a.press(8, a.bodyTop()+at)
	if !a.taskSheet.detailOn || a.taskSheet.detail.ID != "7" {
		t.Fatal("result click did not open exact task")
	}
}

func TestWorkingGroupKeepsCompletionsWhileOriginalAnswerStreams(t *testing.T) {
	a := completionFoldApp(t)
	a.state = stateWorking
	a.live = len(a.entries) - 1
	a.entries[a.live].settled = false
	a.taskUpdate(update(7, "Background compile", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(7, "Background compile", session.TaskDone, session.TaskNotice{Result: "Compiler output"}))
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, "finished") || strings.Contains(got, "Background compile") {
		t.Fatal(got)
	}
	a.openWorkfold(1)
	a.touch()
	found := false
	for _, r := range a.visible(a.bodyWidth()) {
		if r.hit == hitWorkCompletion && r.completion.task == 7 {
			found = true
		}
	}
	if !found {
		t.Fatal("ongoing Working disclosure lost completed task")
	}
}

func TestWorkedGroupRetainsOriginalReceiptAlongsideCanonicalResult(t *testing.T) {
	a := completionFoldApp(t)
	a.taskUpdate(update(7, "Compile parser", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(7, "Compile parser", session.TaskDone, session.TaskNotice{}))
	raw := "original checker alpha · paid $0.123\noriginal result report"
	a.entries = append(a.entries, entry{kind: entryNote, turn: 2, completion: &workCompletion{owner: 1, key: "task/7/0", task: 7, title: "Compile parser", state: "done", text: raw}})
	a.openWorkfold(1)
	a.touch()
	var details *workCompletion
	count := 0
	for _, r := range a.visible(a.bodyWidth()) {
		if r.hit == hitWorkCompletion && r.completion.task == 7 {
			count++
		}
		if r.hit == hitWorkReceiptDetails {
			details = r.completion
		}
	}
	if count != 1 || details == nil {
		t.Fatalf("receipt/snapshot dedup lost details: count %d details %+v", count, details)
	}
	a.toggleWorkCompletionDetails(details)
	if text := strings.Join(plainRows(a), "\n"); !strings.Contains(text, "paid $0.123") || !strings.Contains(text, "original result report") {
		t.Fatal(text)
	}
}

func TestUnknownReceiptKeepsFullTextInsideLocalBackgroundDisclosure(t *testing.T) {
	a := completionFoldApp(t)
	raw := "runtime receipt with no proven owner\ncomplete original result retained"
	a.entries = append(a.entries, entry{kind: entryNote, turn: 2, text: raw, completion: &workCompletion{key: "receipt/unknown", title: "Background work", text: raw}})
	if got := strings.Join(plainRows(a), "\n"); strings.Contains(got, "complete original result retained") {
		t.Fatal("unknown receipt leaked outside disclosure")
	}
	key := 0
	for _, f := range a.deckFolds(a.conversation()) {
		if f.background {
			key = f.key
		}
	}
	if key == 0 {
		t.Fatal("unknown receipt has no inspectable local disclosure")
	}
	a.openWorkfold(key)
	a.touch()
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, "complete original result retained") {
		t.Fatal(got)
	}
}

func TestProcessCompletionUpdatesOriginalGroupAndKeepsLogDoor(t *testing.T) {
	a := completionFoldApp(t)
	job := jobNotice(3, session.JobRunning, "compile", jobLogFile(t, "compiler log retained"))
	a.jobUpdate(job)
	a.turn = 2
	a.entries = append(a.entries, entry{kind: entryUser, turn: 2, text: "Another question"}, entry{kind: entryAssistant, turn: 2, text: "Another answer", settled: true})
	job.State = session.JobDone
	a.jobUpdate(job)
	a.openWorkfold(1)
	a.touch()
	var result *workCompletion
	for _, r := range a.visible(a.bodyWidth()) {
		if r.hit == hitWorkCompletion && r.completion.job == 3 {
			result = r.completion
		}
	}
	if result == nil || result.owner != 1 {
		t.Fatal("process result left original request")
	}
	cmd := a.openWorkCompletion(result)
	if cmd == nil || a.jobPage != 3 {
		t.Fatal("process result lost log door")
	}
	takeJobLog(t, a, cmd)
	if rows, _, _, _ := a.jobPageFrame(120, 40); !strings.Contains(plain(strings.Join(rows, "\n")), "compiler log retained") {
		t.Fatal("opened wrong log")
	}
}

func TestWorkedGroupKeepsTaskRevisionsDistinctAndMergesLatestSnapshot(t *testing.T) {
	a := completionFoldApp(t)
	a.taskUpdate(update(7, "Compile parser", session.TaskRunning, session.TaskNotice{}))
	a.taskUpdate(update(7, "Compile parser", session.TaskDone, session.TaskNotice{}))
	for _, revision := range []int{1, 2} {
		a.entries = append(a.entries, entry{kind: entryNote, turn: 2, completion: &workCompletion{owner: 1, key: "task/7/" + itoa(revision), task: 7, title: "Compile parser", state: "done", text: "recorded revision " + itoa(revision)}})
	}
	count := 0
	for _, f := range a.deckFolds(a.conversation()) {
		for _, c := range f.completions {
			if c.task == 7 {
				count++
				if c.key == "task/7" {
					t.Fatal("snapshot duplicated latest recorded revision")
				}
			}
		}
	}
	if count != 2 {
		t.Fatalf("recorded revisions became %d task rows, want two", count)
	}
}

func TestTaskPartInheritsOriginalParentRequestRatherThanCurrentChat(t *testing.T) {
	a := completionFoldApp(t)
	a.taskUpdate(update(7, "Parent task", session.TaskRunning, session.TaskNotice{}))
	a.turn = 2
	a.taskUpdate(update(8, "Later child", session.TaskRunning, session.TaskNotice{Parent: 7}))
	if owner, ok := a.workCompletionTurn(8); !ok || owner != 1 {
		t.Fatalf("child was attributed to newer chat turn: %d %v", owner, ok)
	}
}

func TestRestoredManualTaskWithoutReceiptKeepsLocalResultDisclosure(t *testing.T) {
	a := completionFoldApp(t)
	a.turn = 2
	a.taskUpdate(update(9, "Manually started task", session.TaskDone, session.TaskNotice{Report: "Restored manual task report", Result: "Restored manual output"}))
	key := 0
	count := 0
	for _, f := range a.deckFolds(a.conversation()) {
		for _, c := range f.completions {
			if c.task == 9 {
				count++
				key = f.key
				if !f.background {
					t.Fatal("restore invented original user ownership")
				}
			}
		}
	}
	if count != 1 || key == 0 {
		t.Fatalf("manual restored completion vanished: rows %d key %d", count, key)
	}
	a.openWorkfold(key)
	a.touch()
	var result *workCompletion
	for _, r := range a.visible(a.bodyWidth()) {
		if r.hit == hitWorkCompletion && r.completion.task == 9 {
			result = r.completion
		}
	}
	if result == nil {
		t.Fatal("manual restored task lost clickable result")
	}
	a.openWorkCompletion(result)
	if !a.taskSheet.detailOn || a.taskSheet.detail.ID != "9" {
		t.Fatal("manual restored result door opened wrong task")
	}
}

func TestUnrecordedManualTaskDoesNotClaimPreviousUserRequest(t *testing.T) {
	a := completionFoldApp(t)
	a.taskUpdate(update(99, "Manual task after prior reply", session.TaskRunning, session.TaskNotice{}))
	if _, owned := a.workCompletionTurn(99); owned {
		t.Fatal("manual slash task claimed unrelated previous request")
	}
	a.taskUpdate(update(99, "Manual task after prior reply", session.TaskDone, session.TaskNotice{}))
	found := false
	for _, f := range a.deckFolds(a.conversation()) {
		for _, c := range f.completions {
			if c.task == 99 {
				found = true
				if !f.background {
					t.Fatal("manual task grouped into unrelated user request")
				}
			}
		}
	}
	if !found {
		t.Fatal("manual task lost local completion disclosure")
	}
}

func TestLatestWorkShortcutUsesPositionForNegativeBackgroundKeys(t *testing.T) {
	a := completionFoldApp(t)
	raw := "retained background completion details"
	a.entries = append(a.entries, entry{kind: entryNote, turn: 2, completion: &workCompletion{key: "receipt/latest", title: "Background work", text: raw}})
	latest := 0
	for _, f := range a.deckFolds(a.conversation()) {
		if f.background {
			latest = f.key
		}
	}
	if latest >= 0 {
		t.Fatal("fixture has no negative background disclosure")
	}
	if !a.toggleLatestWorkfold() || !a.workOpen[latest] {
		t.Fatal("ctrl+e did not open newest background disclosure")
	}
	if a.workOpen[1] {
		t.Fatal("ctrl+e opened older positive-key work instead")
	}
	if got := strings.Join(plainRows(a), "\n"); !strings.Contains(got, raw) {
		t.Fatal("newest background details stayed hidden")
	}
	if !a.toggleLatestWorkfold() || a.workOpen[latest] {
		t.Fatal("second ctrl+e did not close same newest disclosure")
	}
}
