package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestFactoryOwnershipSurvivesRestartAndDropKeepsOrdinaryChats(t *testing.T) {
	places, profile, conversation := bringBackFixture(t)
	floor, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	job := factory.Item{ID: 41, Num: 59, Repo: "acme/api"}
	orphan, ordinary, other, legacy := conversation("old-round"), conversation("ordinary"), conversation("other-job"), conversation("legacy-round")
	if err := markFactoryChat(floor, job, orphan); err != nil {
		t.Fatal(err)
	}
	if err := markFactoryChat(floor, factory.Item{ID: 42, Repo: job.Repo}, other); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(floor.Root(), "work", "api-59")
	for _, file := range []string{ordinary, legacy} {
		if err := session.SaveMeta(filepath.Dir(file), session.Meta{ID: filepath.Base(filepath.Dir(file)), Workspace: work}); err != nil {
			t.Fatal(err)
		}
	}
	row, _ := json.Marshal(map[string]string{"type": "message", "role": "user", "content": "write: fix the issue\n\nYou are the write step of a codeaf run on issue #59. Complete the implementation."})
	if err := os.WriteFile(legacy, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if !factoryOwnedConversation(orphan) || !factoryOwnedConversation(legacy) || factoryOwnedConversation(ordinary) {
		t.Fatal("ownership misclassified a chat")
	}
	if got := ownedJobChats(places, job, []string{filepath.Join(floor.Root(), "different")}); len(got) != 0 {
		t.Fatal("another factory claimed this job's chats")
	}
	// The current job deliberately has no links to either previous run.
	if err := deleteJobChats(profile, places, job, nil, floor.Root()); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{orphan, legacy} {
		if conversationKept(t, file) {
			t.Fatal("Drop retained an earlier run")
		}
	}
	for _, file := range []string{ordinary, other} {
		if !conversationKept(t, file) {
			t.Fatal("Drop deleted an unrelated chat")
		}
	}
}
