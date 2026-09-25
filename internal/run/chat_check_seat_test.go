package run_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// CODEAF_CHECK_MODEL REACHES A CHAT'S RUN. The manual names the environment
// value as the check seat's rung after the flag, and a conversation has no
// flag, so the environment is the whole of the person's say over which model
// checks a `/task`'s work. The chat's door hands the engine its work and plan
// seats; the check seat is read at the engine's end of the seam, so a run the
// chat opens seats its check on the environment's model and never on the
// profile's careful row while the variable is set.
func TestTheChatDoorsCheckRidesTheCheckModelVariable(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	t.Setenv(config.CheckModelEnv, "vendor/env-check")
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "Review", Role: plandb.RoleCheck}}); err != nil {
		t.Fatal(err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	var mu sync.Mutex
	var asked []string
	refuse := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("scripted: no provider behind this seat")
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run.ChatEngine.Start(ctx, session.RunSpec{
		Store:      store,
		Workspace:  t.TempDir(),
		ProfileDir: dir,
		WorkModel:  "vendor/chat-work",
		PlanModel:  "vendor/chat-plan",
		CompleterFor: func(model string) session.Completer {
			mu.Lock()
			asked = append(asked, model)
			mu.Unlock()
			return refuse
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(asked, "vendor/env-check") {
		t.Fatalf("the chat's run seated its launches on %v; the check never rode %s=vendor/env-check",
			asked, config.CheckModelEnv)
	}
	if slices.Contains(asked, "vendor/profile-careful") {
		t.Fatalf("the chat's run seated a check on the profile's careful row %v while %s was set",
			asked, config.CheckModelEnv)
	}
}

// UNDER `--one-model` THE RUN HAS ONE SEAT (contract 3a). The chat's door names
// the conversation's model as every seat, and the engine seats every role on it
// — the root's planning, a leaf, a check and a probe — whatever the profile's
// crew rows say and whatever CODEAF_CHECK_MODEL says. The profile rows below
// name OTHER models for every tier, so a seat that fell back to the profile or
// the environment would be seen here.
func TestUnderOneModelEveryRunSeatIsTheConversationsModel(t *testing.T) {
	t.Setenv(config.CheckModelEnv, "vendor/env-check")
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "one", Title: "One", ParentID: store.RootID()},
		{ID: "review", Title: "Review", Role: plandb.RoleCheck},
		{ID: "discriminate", Title: "Discriminate", Role: plandb.RoleProbe},
	}); err != nil {
		t.Fatal(err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierLowModel:        "vendor/profile-small",
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	recorder := &recordingCompleter{}
	factory := run.CrewFactory(store, t.TempDir(), dir, run.Seats{One: "vendor/one"}, recorder.forModel)
	for _, id := range []string{store.RootID(), "one", "review", "discriminate"} {
		recorder.models = nil
		factory(*store.Task(id))
		if len(recorder.models) != 1 || recorder.models[0] != "vendor/one" {
			t.Errorf("task %s was seated on %v under one model, want vendor/one", id, recorder.models)
		}
	}
}

// THE CHAT'S DOOR CARRIES THE ONE SEAT TO THE ENGINE (contract 3a). The spec
// names only OneModel — no work, plan or check seat — so a launch that rode
// anything else read it from the profile or the environment, which is the leak
// the tagged suite billed claude-fable-5.1 through.
func TestTheChatDoorsOneModelReachesEveryLaunch(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	t.Setenv(config.CheckModelEnv, "vendor/env-check")
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "Review", Role: plandb.RoleCheck}}); err != nil {
		t.Fatal(err)
	}
	dir := crewProfile(t, map[string]string{
		config.KeyTierHighModel:       "vendor/profile-careful",
		config.KeyTierWorkerModel:     "vendor/profile-worker",
		config.KeyTierMastermindModel: "vendor/profile-thinking",
	})
	var mu sync.Mutex
	var asked []string
	refuse := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("scripted: no provider behind this seat")
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run.ChatEngine.Start(ctx, session.RunSpec{
		Store:      store,
		Workspace:  t.TempDir(),
		ProfileDir: dir,
		OneModel:   "vendor/one",
		CompleterFor: func(model string) session.Completer {
			mu.Lock()
			asked = append(asked, model)
			mu.Unlock()
			return refuse
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if len(asked) == 0 {
		t.Fatal("the run launched nothing, so nothing was seated")
	}
	for _, model := range asked {
		if model != "vendor/one" {
			t.Fatalf("a launch under one model was seated on %s (all asked: %v)", model, asked)
		}
	}
}
