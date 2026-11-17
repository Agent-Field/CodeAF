package serve_test

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/github"
)

func TestV2CloseAndReopenKeepRunAndGitHub(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 9, Origin: factory.OriginForge, Assignee: "sam", State: factory.StateRunning, Talk: "manager", Cap: 10, ChatSpent: 1, HeadCommit: "kept", PullRequest: &factory.PullRequest{Number: 11, State: factory.PROpen}, Stream: &factory.Stream{Spent: 2, Phases: []factory.Phase{{Name: "write", State: factory.PhaseRunning, Chat: "write-chat"}}}})
	j := f.read(t, it)
	f.post(t, it, "close", fmt.Sprintf(`{"expect":{"version":%d}}`, j.Version), 200)
	closed := f.read(t, it)
	if closed.State != "done" || closed.DoneHow != "closed_locally" || closed.StatePresentation.Title != "Closed" || closed.Display.RunningVisible || !closed.Can.Reopen || closed.Can.Restore || closed.Can.Reply || closed.Can.Close {
		t.Fatalf("wrong projection: %+v", closed)
	}
	if len(f.folders.deleted()) != 0 || len(f.chats.asked()) != 0 {
		t.Fatal("Close deleted retained work")
	}
	f.post(t, it, "control", `{"mode":"resume_pipeline"}`, 409)
	if err := github.Merge(f.st, "github", []factory.Item{it}); err != nil {
		t.Fatal(err)
	}
	if f.read(t, it).DoneHow != "closed_locally" {
		t.Fatal("GitHub reopened local closure")
	}
	f.post(t, it, "reopen", "", 200)
	got, _ := f.st.Get(it.ID)
	if got.DoneHow != "" || got.Talk != "manager" || got.HeadCommit != "kept" || got.PullRequest.Number != 11 || got.Stream.Spent != 2 || got.ChatSpent != 1 || got.Stream.Phases[0].Chat != "write-chat" || got.State == factory.StateQueued {
		t.Fatalf("Reopen lost work or started charges: %+v", got)
	}
}
func TestV2CloseOnlyForOwnerAndActiveJobs(t *testing.T) {
	f := newV2Jobs(t)
	other := f.create(t, factory.Item{State: factory.StateNew, Assignee: "other"})
	f.post(t, other, "close", "", 403)
	own := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	f.post(t, own, "close", "", 200)
	f.post(t, own, "close", "", 409)
}
