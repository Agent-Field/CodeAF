package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A SOURCE CROSSES THE WIRE AS THE ENGINE RECORDED IT: the same identity, kind and
// time, the words bounded and scrubbed, and nothing at all where the engine
// recorded nothing.
func TestAppTaskCarriesTheSourceTheEngineRecordedAndNothingElse(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	source := &session.TaskSource{ID: "s/abc#4", Kind: session.TaskSourceMessage, Text: "deploy with sk-ant-api03-" + strings.Repeat("x", 40), At: at}
	task, ok := appTaskOf("a1", &session.TaskNotice{ID: 3, Title: "t", State: session.TaskFailed, Source: source, Dismissed: true}, false, "", at)
	if !ok || task.Source == nil || task.Source.ID != "s/abc#4" || task.Source.Kind != appwire.SourceMessage || !task.Dismissed {
		t.Fatalf("task = %+v", task)
	}
	if strings.Contains(task.Source.Text, "sk-ant") || task.Source.At == nil || !task.Source.At.Equal(at) {
		t.Fatalf("source = %+v, want the words scrubbed and the time kept", task.Source)
	}
	long := &session.TaskSource{ID: "s#d1", Kind: session.TaskSourceDirective, Text: strings.Repeat("é", session.MaxTaskSourceBytes)}
	if got := appSourceOf(long); got == nil || len(got.Text) > session.MaxTaskSourceBytes || !got.Cut || got.At != nil || strings.ContainsRune(got.Text, '\uFFFD') {
		t.Fatalf("a long source went out as %d bytes: %+v", len(got.Text), got)
	}

	// Nothing recorded, nothing said; a record that does not name a known kind is not a source.
	bare, _ := appTaskOf("a1", &session.TaskNotice{ID: 4, Title: "old", State: session.TaskFailed}, false, "", at)
	raw, _ := json.Marshal(bare)
	if bare.Source != nil || bare.Dismissed || strings.Contains(string(raw), "source") || strings.Contains(string(raw), "dismissed") {
		t.Fatalf("a task with no recorded source says %s", raw)
	}
	for _, bad := range []*session.TaskSource{{ID: "", Kind: session.TaskSourceMessage}, {ID: "x", Kind: "inferred"}} {
		if appSourceOf(bad) != nil {
			t.Fatalf("%+v went out as a source", bad)
		}
	}

	view := appTaskOfView("a1", session.AppTaskView{ID: "7", Title: "v", State: "failed", Source: source, Dismissed: true, FinishedAt: at})
	if view.Source == nil || view.Source.ID != source.ID || !view.Dismissed || view.FinishedAt == nil || !view.FinishedAt.Equal(at) {
		t.Fatalf("view = %+v", view)
	}
	running := appTaskOfView("a1", session.AppTaskView{ID: "8", Title: "r", State: "running", FinishedAt: at})
	if running.FinishedAt != nil {
		t.Fatalf("a running task has a finish time %v", running.FinishedAt)
	}
}

// DISMISS AND RESTORE REACH THE ENGINE AS THE TASK DOOR'S OWN ACTIONS AND ARE SAID
// BACK AS task.changed READ AFTER THE ENGINE ANSWERED.
func TestAppDismissAndRestoreGoThroughTheTaskDoorAndSayTaskChanged(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{{ID: "5", Title: "watch places-v1", State: "failed", Result: "exit 1", Dismissed: true, Source: &session.TaskSource{ID: "s#2", Kind: session.TaskSourceMessage, Text: "watch the places"}}}}
	backend, rec, agent := workBackend(t, conv)
	id := agent.ID + ":5"
	if err := backend.ActOnTask(id, session.AppActionDismiss, "", ""); err != nil {
		t.Fatal(err)
	}
	got := taskChanged(rec, t, func(task appwire.Task) bool { return task.ID == id && task.Dismissed })
	if got.State != appwire.TaskFailed || got.Result != "exit 1" || got.Source == nil || got.Source.ID != "s#2" {
		t.Fatalf("task.changed carried %+v", got)
	}
	conv.mu.Lock()
	conv.appTasks[0].Dismissed = false
	conv.mu.Unlock()
	if err := backend.ActOnTask(id, session.AppActionRestore, "", ""); err != nil {
		t.Fatal(err)
	}
	taskChanged(rec, t, func(task appwire.Task) bool { return task.ID == id && !task.Dismissed && task.Source != nil })
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if len(conv.intents) != 2 || conv.intents[0].Action != "dismiss" || conv.intents[1].Action != "restore" || conv.intents[0].ID != "5" {
		t.Fatalf("intents = %+v", conv.intents)
	}
}

// THE ENGINE'S REFUSALS ARE bad_params, NOT ENGINE FAULTS.
func TestAppDismissRefusalsAreBadParams(t *testing.T) {
	for _, words := range []string{
		"task 2 is not a settled failure: it is running",
		"task 2 is not settled work to hide: 3 still waits on it",
		"task 5 still has 6 under it, which is waiting",
	} {
		err := appTaskError(errors.New(words))
		var wireErr *appwire.Error
		if !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeBadParams {
			t.Errorf("%q became %v", words, err)
		}
	}
}

// A PLAN STORE'S FAILED COMMIT IS UNCERTAIN on the plain door too, whatever
// road raised it and whatever its words say.
func TestAPlanStoreCommitFailureIsUncertainOnThePlainDoor(t *testing.T) {
	err := appTaskError(fmt.Errorf("amend: %w: %w", plandb.ErrCommitUncertain, errors.New("no such task, disk I/O error")))
	var wireErr *appwire.Error
	if !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeUncertain {
		t.Fatalf("a failed commit became %v", err)
	}
}

func TestAppTaskRetrySupportIsAnExplicitProducerFact(t *testing.T) {
	for _, supported := range []bool{false, true} {
		task, ok := appTaskOf("a", &session.TaskNotice{ID: 1, Title: "same title", State: session.TaskFailed, Retryable: supported}, false, "", time.Now())
		if !ok || task.Retryable != supported {
			t.Fatalf("notice support = %v", task.Retryable)
		}
		view := appTaskOfView("a", session.AppTaskView{ID: "1", Title: "same title", State: "failed", Retryable: supported})
		if view.Retryable != supported {
			t.Fatalf("read support = %v", view.Retryable)
		}
		body, err := json.Marshal(task)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		if value, exists := wire["retryable"]; !exists || value != supported {
			t.Fatalf("support must be explicit, including false: %s", body)
		}
	}
}
