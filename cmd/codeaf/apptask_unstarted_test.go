package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// AN APPROVED PROPOSAL WHOSE START WAS REFUSED IS LISTED FAILED, NEVER QUEUED.
//
// The engine's order for one id is the card on its clock, the card restated
// approved, and then — because the store refused the run — the run's row
// settled failed with the reason as its report and no result. The live list
// used to stop at the approval and keep a queued task forever; it now ends on
// the failure, says what stopped it, and the list read agrees.
func TestAppAnApprovedProposalThatDidNotStartIsListedFailed(t *testing.T) {
	conv := &fakeConversation{}
	backend, rec, agent := workBackend(t, conv)
	deadline := time.Now().Add(12 * time.Second).Round(time.Second)
	reason := `task 2 did not start: task "2" has hard dependency "1" across its containment lineage. Nothing is running for it and nothing was started in its place.`

	conv.lane(session.Event{Kind: session.EventTaskProposal, Task: &session.TaskNotice{ID: 2, Title: "consume the run", Deadline: deadline}})
	taskChanged(rec, t, func(task appwire.Task) bool { return task.ID == agent.ID+":2" && task.Proposed })
	conv.lane(session.Event{Kind: session.EventTaskProposal, Task: &session.TaskNotice{ID: 2, Title: "consume the run", Decided: &session.TaskAnswer{Approved: true}}})
	taskChanged(rec, t, func(task appwire.Task) bool {
		return task.ID == agent.ID+":2" && !task.Proposed && task.State == appwire.TaskQueued
	})
	conv.lane(session.Event{Kind: session.EventTaskUpdate, Task: &session.TaskNotice{ID: 2, Title: "consume the run", State: session.TaskFailed, Report: reason, EndedAt: time.Now()}})
	failed := taskChanged(rec, t, func(task appwire.Task) bool {
		return task.ID == agent.ID+":2" && task.State == appwire.TaskFailed
	})
	if failed.Proposed || failed.ClockUntil != nil || failed.Result != reason || failed.StartedAt != nil {
		t.Fatalf("the refused task = %+v, want failed with the reason and no start", failed)
	}

	list, err := backend.ListTasks(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range list {
		if task.ID != agent.ID+":2" {
			continue
		}
		found = true
		if task.State != appwire.TaskFailed || task.Proposed || !strings.Contains(task.Result, "containment lineage") {
			t.Fatalf("the listed task = %+v, want failed with the reason", task)
		}
	}
	if !found {
		t.Fatalf("the refused task is missing from the list: %+v", list)
	}
}

// A REOPENED CONVERSATION LISTS THE SAME FAILURE from its checkpoint: a run
// row kept failed with only a report reads failed with that report as its
// result, exactly as the engine's own read answers it.
func TestAppAnUnstartedRunRowReadsBackFailedWithItsReason(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, filepath.Join(root, "profile"))
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	tasks := `{"type":"tasks","version":1,"seq":2,"runs":[
{"id":2,"title":"consume the run","state":"failed","report":"task 2 did not start: the store refused it. Nothing is running for it and nothing was started in its place.","endedAt":"2026-10-15T09:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(transcript), "tasks.json"), []byte(tasks), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newAppBackend(newAppRecorder(), nil)
	defer backend.Close()
	id := appAgentID(transcript)
	list, err := backend.ListTasks(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("tasks = %+v", list)
	}
	if task := list[0]; task.ID != id+":2" || task.State != appwire.TaskFailed || !strings.HasPrefix(task.Result, "task 2 did not start") {
		t.Fatalf("the reopened task = %+v, want failed with its reason", task)
	}
}
