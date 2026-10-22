package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func historicalCompletionFixture() []session.DisplayEntry {
	task := session.TaskReplyTag{ID: 9, Title: "Check parser", Request: "Check the parser", State: session.TaskDone, Revision: 1}
	return []session.DisplayEntry{
		{Role: "user", Text: "Check the parser"},
		{Role: "assistant", Text: "The parser check is running.", Answer: true},
		{Role: "user", Text: "Build the manual"},
		{Role: "tool", Tool: "jobs", CallID: "job-start", Output: "job 3 started; log at /tmp/manual.log", Answered: true},
		{Role: "assistant", Text: "The manual build is running.", Answer: true},
		{Role: "user", Text: "Keep this new discussion separate"},
		{Role: "aside", Text: "EXACT TASK RECEIPT", RoutineCompletion: true, CompletionTasks: []session.TaskReplyTag{task}},
		{Role: "aside", Text: "task 9 crew · bugfix · worker a · planner b · checker c · $0.310", RoutineCompletion: true},
		{Role: "assistant", Text: "The parser handles the empty map.", Answer: true, ReplyTags: []session.TaskReplyTag{task}},
		{Role: "aside", Text: "job 3 exited 0: manual ready", RoutineCompletion: true},
		{Role: "aside", Text: "while you worked:\n\nambiguous task receipt\n\nRequested watch matched"},
		{Role: "aside", Text: "Requested reminder: review the parser"},
		{Role: "user", Text: "EXACT TASK RECEIPT"},
	}
}

func TestHistoricalCompletionGroupingKeepsOriginalOwnersAndExactPayloads(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, _ = a.replayBlocks(historicalCompletionFixture(), chatReplay(0))
	tasks, jobs := 0, 0
	reminder, mixed, quote := false, false, false
	for _, e := range a.entries {
		if c := e.completion; c != nil {
			if c.task == 9 {
				tasks++
				if c.owner != 1 {
					t.Fatalf("task attached to later request: %+v", c)
				}
			}
			if c.job == 3 {
				jobs++
				if c.owner != 2 {
					t.Fatalf("process attached to later request: %+v", c)
				}
			}
			if c.text != e.text {
				t.Fatal("replay lost exact receipt payload")
			}
		}
		reminder = reminder || e.text == "Requested reminder: review the parser"
		mixed = mixed || strings.Contains(e.text, "ambiguous task receipt") && e.completion == nil
		quote = quote || e.kind == entryUser && e.text == "EXACT TASK RECEIPT" && e.completion == nil
	}
	if tasks != 2 || jobs != 1 || !reminder || !mixed || !quote {
		t.Fatalf("lost receipt or meaningful occurrence: tasks=%d jobs=%d reminder=%v mixed=%v quote=%v", tasks, jobs, reminder, mixed, quote)
	}
}

func TestHistoricalCompletionOwnershipNeverGuessesAcrossRequests(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{{Role: "user", Text: "Unrelated new request"}, {Role: "aside", Text: "task 37 done: older report · transcript file:///tmp/report", RoutineCompletion: true}}, chatReplay(0))
	c := a.entries[len(a.entries)-1].completion
	if c == nil || c.task != 37 || c.owner != 0 {
		t.Fatalf("unknown old work invented a request owner: %+v", c)
	}
}

func TestHistoricalCompletionGroupingSurvivesReopenAndTabReturn(t *testing.T) {
	first := &fakeAgent{model: "m", past: historicalCompletionFixture()}
	a := newTestApp(first)
	a.file = t.TempDir() + "/first.jsonl"
	a.entries, _ = a.replayBlocks(first.past, chatReplay(0))
	check := func() {
		t.Helper()
		tasks, jobs := 0, 0
		for _, e := range a.entries {
			if c := e.completion; c != nil {
				if c.task == 9 && c.owner == 1 {
					tasks++
				}
				if c.job == 3 && c.owner == 2 {
					jobs++
				}
			}
		}
		if tasks != 2 || jobs != 1 {
			t.Fatalf("tab/reopen duplicated or lost grouped receipts: %d %d", tasks, jobs)
		}
	}
	check()
	original, side := a.front(), a.detachConversation()
	drain(t, a, a.attachConversation(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: t.TempDir() + "/second.jsonl"}, nil))
	for _, e := range a.entries {
		if e.completion != nil {
			t.Fatal("another conversation inherited grouped work")
		}
	}
	a.detachConversation()
	drain(t, a, a.attachConversation(original, side))
	check()
	reopened := newTestApp(&fakeAgent{model: "m", past: historicalCompletionFixture()})
	reopened.entries, _ = reopened.replayBlocks(historicalCompletionFixture(), chatReplay(0))
	a.entries = reopened.entries
	check()
}

func TestLegacyTaskAndCrewReceiptsAcquireTheSameOriginalRequest(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	tag := session.TaskReplyTag{ID: 9, Title: "Parser", Request: "Check parser", State: session.TaskDone, Revision: 1}
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{
		{Role: "user", Text: "Check parser"}, {Role: "assistant", Text: "Working on it.", Answer: true},
		{Role: "user", Text: "A different request"},
		{Role: "aside", Text: "task 9 done: parser ready · transcript file:///tmp/parser", RoutineCompletion: true},
		{Role: "aside", Text: "task 9 crew · bugfix · worker a · checker c", RoutineCompletion: true},
		{Role: "assistant", Text: "The parser passes.", Answer: true, ReplyTags: []session.TaskReplyTag{tag}},
	}, chatReplay(0))
	receipts := 0
	for _, e := range a.entries {
		if c := e.completion; c != nil {
			receipts++
			if c.owner != 1 || c.key != "task/9/1" {
				t.Fatalf("old receipt not grouped with original request: %+v", c)
			}
		}
	}
	if receipts != 2 {
		t.Fatalf("legacy receipts lost: %d", receipts)
	}
	if turn, ok := a.workCompletionReplayTurn(9); !ok || turn != 1 {
		t.Fatalf("toast view lost original disclosure: %d %v", turn, ok)
	}
}

func TestLegacyToastViewUsesRetainedLocalDisclosureWithoutInventingOwner(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{{Role: "user", Text: "Current request"}, {Role: "aside", Text: "task 37 done: older report · transcript file:///tmp/report", RoutineCompletion: true}}, chatReplay(0))
	if turn, ok := a.workCompletionReplayTurn(37); !ok || turn != 1 {
		t.Fatalf("retained background disclosure cannot be revealed: %d %v", turn, ok)
	}
	if a.entries[1].completion.owner != 0 {
		t.Fatal("fallback invented original admission ownership")
	}
	if _, ok := a.workCompletionReplayTurn(38); ok {
		t.Fatal("toast invented a missing work disclosure")
	}
}

func TestToastIdentityRevealsUnknownBackgroundFoldRatherThanWrongRequest(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 40
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{{Role: "user", Text: "Unrelated request"}, {Role: "aside", Text: "task 37 done: older report · transcript file:///tmp/report", RoutineCompletion: true}}, chatReplay(0))
	a.touch()
	var key int
	found := false
	for _, f := range a.deckFolds(a.bodyDeck()) {
		if f.background && len(f.completions) == 1 && f.completions[0].task == 37 {
			key, found = f.key, true
		}
	}
	if !found || key >= 0 {
		t.Fatalf("unknown work has no separate background disclosure: key=%d found=%v", key, found)
	}
	if !a.revealCompletionIdentity(37, 0) || !a.workOpen[key] {
		t.Fatal("toast View failed to reveal actual retained background fold")
	}
	if a.workOpen[1] {
		t.Fatal("toast opened unrelated request instead of background disclosure")
	}
	if a.revealCompletionIdentity(38, 0) {
		t.Fatal("toast View invented a missing receipt")
	}
}

func TestHistoricalTaskAttemptsKeepTheirOwnIdentityAndRequest(t *testing.T) {
	first := session.TaskReplyTag{ID: 9, Title: "First parser attempt", Request: "First request", State: session.TaskFailed, Revision: 1}
	second := session.TaskReplyTag{ID: 9, Title: "Second parser attempt", Request: "Second request", State: session.TaskDone, Revision: 2}
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{
		{Role: "user", Text: first.Request}, {Role: "aside", Text: "First attempt details", RoutineCompletion: true, CompletionTasks: []session.TaskReplyTag{first}},
		{Role: "user", Text: second.Request}, {Role: "aside", Text: "Second attempt details", RoutineCompletion: true, CompletionTasks: []session.TaskReplyTag{second}},
		{Role: "assistant", Text: "The second attempt passes.", ReplyTags: []session.TaskReplyTag{second}, Answer: true},
	}, chatReplay(0))
	for _, e := range a.entries {
		if c := e.completion; c != nil {
			switch c.text {
			case "First attempt details":
				if c.key != "task/9/1" || c.owner != 1 || c.title != first.Title {
					t.Fatalf("old attempt inherited later result identity: %+v", c)
				}
			case "Second attempt details":
				if c.key != "task/9/2" || c.owner != 2 {
					t.Fatalf("new attempt lost identity: %+v", c)
				}
			}
		}
	}
}

func TestReopenedRequestedAlertsAndAmbiguousMixedMessagesRemainVisibleOutsideWork(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 140, 50
	const watch = "watch release: until matched"
	const mixed = "while you worked:\n\nThe task ended.\n\nRequested alert: review release"
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{
		{Role: "user", Text: "Check release"},
		{Role: "tool", Tool: "read", CallID: "read-1", Output: "private operational output", Answered: true},
		{Role: "aside", Text: "provider retry bookkeeping"},
		{Role: "aside", Text: watch + "\nREADY", Addressed: true},
		{Role: "aside", Text: mixed},
		{Role: "aside", Text: "Requested reminder: review parser", Addressed: true},
		{Role: "assistant", Text: "The release is ready.", Answer: true},
	}, chatReplay(0))
	a.touch()
	visible := taskText(a)
	for _, want := range []string{watch, "Requested alert: review release", "Requested reminder: review parser", "The release is ready."} {
		if !strings.Contains(visible, want) {
			t.Fatalf("requested alert hidden inside routine work: missing %q\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "provider retry bookkeeping") {
		t.Fatalf("attention exposed unrelated housekeeping: %s", visible)
	}
}

func TestRepeatedOriginalRequestDoesNotReassignHistoricalTask(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	tag := session.TaskReplyTag{ID: 9, Title: "Parser", Request: "Check parser", State: session.TaskDone}
	a.entries, _ = a.replayBlocks([]session.DisplayEntry{{Role: "user", Text: "Check parser"}, {Role: "assistant", Text: "Working.", Answer: true}, {Role: "user", Text: "Check parser"}, {Role: "aside", Text: "task receipt", RoutineCompletion: true, CompletionTasks: []session.TaskReplyTag{tag}}}, chatReplay(0))
	c := a.entries[len(a.entries)-1].completion
	if c == nil || c.owner != 0 {
		t.Fatalf("ambiguous repeated request assigned to wrong occurrence: %+v", c)
	}
	blocks := []entry{{kind: entryUser, text: "Check parser", turn: 1}, {kind: entryTask, turn: 1, card: &taskCard{id: 9}}, {kind: entryUser, text: "Check parser", turn: 2}}
	if owner := replayCompletionOwner(blocks, 9, 0, "Check parser"); owner != 1 {
		t.Fatalf("exact proposal identity lost to ambiguous words: %d", owner)
	}
}
