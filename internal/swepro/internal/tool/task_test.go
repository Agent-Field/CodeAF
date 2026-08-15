package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
)

type fakeTaskSpawner struct {
	requests []TaskSpawnRequest
	result   TaskSpawnResult
	err      error
}

type taskObserver struct {
	tracked   []observer.ObservedSession
	untracked []string
}

func (o *taskObserver) Track(input observer.ObservedSession) {
	o.tracked = append(o.tracked, input)
}

func (o *taskObserver) Untrack(sessionID string) {
	o.untracked = append(o.untracked, sessionID)
}

func (f *fakeTaskSpawner) SpawnTask(_ context.Context, request TaskSpawnRequest) (TaskSpawnResult, error) {
	f.requests = append(f.requests, request)
	return f.result, f.err
}

func TestTaskDispatcherUsesSessionSeam(t *testing.T) {
	spawner := &fakeTaskSpawner{result: TaskSpawnResult{SessionID: "ses-child", ResultText: "finished"}}
	observerHook := &taskObserver{}
	dispatcher := &TaskDispatcher{
		Spawner: spawner, Observer: observerHook,
		Now: func() time.Time { return time.UnixMilli(1700000000000) },
	}
	output, err := dispatcher.Dispatch(context.Background(), "call-1", TaskDispatchInput{
		Params: TaskParams{
			Description:  "Implement parser",
			Prompt:       "Write it",
			SubagentType: "coder",
		},
		ParentSessionID:    "ses-parent",
		ChildSessionID:     "ses-child",
		AssignedPlanDBTask: "t-child",
		RootTaskID:         "t-root",
		ProjectID:          "p-demo",
		IssueFile:          ".codeaf/issues/t-child.md",
		Workspace:          "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(spawner.requests) != 1 {
		t.Fatalf("spawn count = %d", len(spawner.requests))
	}
	request := spawner.requests[0]
	if request.Tier != "high" || !strings.Contains(request.Prompt, "Full spec: .codeaf/issues/t-child.md") {
		t.Fatalf("request = %+v", request)
	}
	if !strings.Contains(output, "plandb_task_id: t-child") || !strings.Contains(output, "<task_result>\nfinished") {
		t.Fatalf("output = %q", output)
	}
	if len(observerHook.tracked) != 1 || len(observerHook.untracked) != 1 {
		t.Fatalf("observer lifecycle=%#v", observerHook)
	}
	observed := observerHook.tracked[0]
	if observed.SessionID != "ses-child" || observed.AgentRole != "coder" ||
		observed.TaskSummary != "Implement parser" || observed.StartedAt != 1700000000000 ||
		observed.Workspace != "/repo" || observed.ParentSessionID != "ses-parent" ||
		observerHook.untracked[0] != "ses-child" {
		t.Fatalf("observer payload=%#v untracked=%v", observed, observerHook.untracked)
	}

	duplicate, err := dispatcher.Dispatch(context.Background(), "call-1", TaskDispatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate != "duplicate task tool_use elided (callID=call-1)" || len(spawner.requests) != 1 {
		t.Fatalf("duplicate = %q; spawn count = %d", duplicate, len(spawner.requests))
	}
}

func TestTaskDispatcherPropagatesSpawnError(t *testing.T) {
	want := errors.New("spawn failed")
	dispatcher := &TaskDispatcher{Spawner: &fakeTaskSpawner{err: want}}
	_, err := dispatcher.Dispatch(context.Background(), "", TaskDispatchInput{
		Params:         TaskParams{SubagentType: "explorer"},
		ChildSessionID: "ses",
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestTaskObserverTracksExactlyLongRunningWriteRoles(t *testing.T) {
	for _, test := range []struct {
		agent string
		want  bool
	}{
		{agent: "coder", want: true},
		{agent: "fixer", want: true},
		{agent: "deep-worker", want: true},
		{agent: "subtask-executor", want: true},
		{agent: "explorer", want: false},
		{agent: "auditor", want: false},
	} {
		t.Run(test.agent, func(t *testing.T) {
			observerHook := &taskObserver{}
			dispatcher := &TaskDispatcher{
				Spawner:  &fakeTaskSpawner{result: TaskSpawnResult{SessionID: "child"}},
				Observer: observerHook,
			}
			_, err := dispatcher.Dispatch(context.Background(), "", TaskDispatchInput{
				Params: TaskParams{SubagentType: test.agent}, ChildSessionID: "child",
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(observerHook.tracked) == 1; got != test.want {
				t.Fatalf("tracked=%v want=%v (%#v)", got, test.want, observerHook)
			}
			if len(observerHook.untracked) != len(observerHook.tracked) {
				t.Fatalf("unbalanced lifecycle=%#v", observerHook)
			}
		})
	}
}
