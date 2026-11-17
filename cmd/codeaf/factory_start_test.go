package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// `factory_start` IS THE RUN BUTTON FROM THE MANAGER'S CHAT: it launches the
// item the conversation manages with its steps as they stand (recording
// nothing about them), goes past the approve step the
// run holds at, and says why when nothing is left to start. Another
// conversation starts nothing.
func TestFactoryStartLaunchesContinuesAndSaysWhy(t *testing.T) {
	g := newRunRig(t)
	g.workOn(t, func(mirror *store.Store) *factoryrun.Runner {
		return buildFactoryRunner(mirror, g.st, g.workspace, t.TempDir(), nil, nil)
	})

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
	door, ok := runDoor(g.st, g.workspace, "").(session.RunStarter)
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
	if len(held.Adapted) != 0 {
		t.Fatalf("the start recorded a change to the steps nobody made: %q", held.Adapted)
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

// `factory_start` AND THE HEADER'S START TAKE THE SAME PATH: on a repository
// this machine has no checkout of and cannot clone (GitHub not connected),
// the manager's start and the app's start_now refuse in the one sentence,
// the second as 409 needs_checkout, and neither moves the job's version; with
// GitHub connected the same job starts from the chat, because the runner
// will clone it.
func TestFactoryStartAndTheHeaderStartTakeTheSamePath(t *testing.T) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(name, "")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	talk := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(talk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{
		Title: "prove it builds", Repo: "acme/api", Tier: factory.TierOwner, Talk: talk,
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	was, _ := st.Revision(id)
	want := "acme/api isn't checked out on this machine · " + factory.CheckoutNeedsGitHub

	door := runDoor(st, "", profile).(session.RunStarter)
	var needs factory.NeedsCheckout
	if _, err := door.StartRun(context.Background(), talk); !errors.As(err, &needs) || err.Error() != want {
		t.Fatalf("factory_start with no way to a checkout = %v", err)
	}

	handler := serve.New(factorySeam(st, "", profile), serve.Config{Address: "http://test", Mode: api.FactorySolo, Store: st})
	t.Cleanup(handler.Close)
	r := httptest.NewRequest("POST", "/v1/jobs/"+strconv.Itoa(id)+"/start_now", strings.NewReader(`{}`))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 409 || body.Error != want || body.Reason != api.ReasonNeedsCheckout {
		t.Fatalf("the header's start = %d %s", w.Code, w.Body)
	}
	if now, _ := st.Revision(id); now != was {
		t.Fatalf("a refused start moved the job's version from %d to %d", was, now)
	}

	t.Setenv("GH_TOKEN", "tok-secret")
	it, _ := st.Get(id)
	if line, err := door.StartRun(context.Background(), talk); err != nil || line != it.Ref()+" started" {
		t.Fatalf("factory_start the runner will clone for = %q, %v", line, err)
	}
	if got, _ := st.Get(id); got.State != factory.StateQueued {
		t.Fatalf("the start did not queue the job: %s", got.State)
	}
}

// A PLAN A RULE HOLDS IS THE PERSON'S: `factory_start` will not take the
// manager past it, and the job stays where it waits.
func TestFactoryStartLeavesARuleHeldPlanToThePerson(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{Title: "touch the keys", Repo: "acme/api", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "plan", Ask: "plan", On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(id, func(it *factory.Item) error {
		it.State, it.QKind, it.RuleHold = factory.StateNeedsYou, factory.QKindApprove, "ask first · touch auth/ · auth/keys.go"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	_, err = startItem(st, "", t.TempDir(), it)
	if err == nil || !strings.Contains(err.Error(), "a rule asks first: ask first · touch auth/ · auth/keys.go") {
		t.Fatalf("the manager approved a rule-held plan: %v", err)
	}
	if got, _ := st.Get(id); got.State != factory.StateNeedsYou || got.QKind != factory.QKindApprove {
		t.Fatalf("the held job moved to %s %q", got.State, got.QKind)
	}
}

// AN APPROVAL THAT NAMES SOMEONE ELSE IS THEIRS: the manager leaves it to them.
func TestFactoryStartLeavesANamedApproversApprovalToThem(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Add(context.Background(), factory.Item{Title: "release it", Repo: "acme/api", Tier: factory.TierOwner, Assignee: "sam",
		Stages: []factory.Stage{{Name: "plan", Ask: "plan", On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true, Who: "@rel"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(id, func(it *factory.Item) error {
		it.State, it.QKind = factory.StateNeedsYou, factory.QKindApprove
		it.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseWaiting}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	_, err = startItem(st, "", t.TempDir(), it)
	if err == nil || !strings.Contains(err.Error(), "waits on @rel") {
		t.Fatalf("the manager approved @rel's approval: %v", err)
	}
}
