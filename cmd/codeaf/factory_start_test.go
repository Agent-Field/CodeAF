package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// `factory_start` IS THE RUN BUTTON FROM THE MANAGER'S CHAT: it launches the
// item the conversation manages (recording the steps as the manager's, so the
// launch gives it no second turn to shape), goes past the approve step the
// run holds at, and says why when nothing is left to start. Another
// conversation starts nothing.
func TestFactoryStartLaunchesContinuesAndSaysWhy(t *testing.T) {
	g := newRunRig(t)
	owner := buildFactoryRunner(g.st, g.workspace, t.TempDir(), nil, nil)
	stopAll(t, g.st, owner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go drainFactoryMailbox(ctx, g.st.Mailbox(), owner, nil, 5*time.Millisecond)

	talk := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(talk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := g.st.Add(context.Background(), factory.Item{
		Title: "prove it builds", Repo: "api", Tier: factory.TierOwner, Talk: talk,
		Stages: []factory.Stage{{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, {Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	it, _ := g.st.Get(id)
	ref := it.Ref()
	door, ok := runDoor(g.st, g.workspace).(session.RunStarter)
	if !ok {
		t.Fatal("the floor's run door cannot start")
	}
	if _, err := door.StartRun(context.Background(), filepath.Join(t.TempDir(), "other.jsonl")); !errors.Is(err, errNotAManager) {
		t.Fatalf("another conversation started the item: %v", err)
	}
	line, err := door.StartRun(context.Background(), talk)
	if err != nil || line != ref+" started" {
		t.Fatalf("start answered %q, %v", line, err)
	}
	held := g.waitFor(t, id, factory.StateNeedsYou)
	if !factoryrun.ShapedByManager(held) {
		t.Fatalf("the start left the item unshaped, so the launch would shape it again: %q", held.Adapted)
	}
	if held.QKind != factory.QKindApprove {
		t.Fatalf("held on %q, not the approve step", held.QKind)
	}
	line, err = door.StartRun(context.Background(), talk)
	if err != nil || !strings.HasPrefix(line, ref+" went past") {
		t.Fatalf("continue answered %q, %v", line, err)
	}
	g.waitFor(t, id, factory.StateLanded)
	if _, err := door.StartRun(context.Background(), talk); err == nil || !strings.Contains(err.Error(), "has landed") {
		t.Fatalf("a landed item started again: %v", err)
	}
	if !door.(session.RunManagerDoor).Manages(talk) {
		t.Fatal("the item's conversation is not its manager")
	}
}
