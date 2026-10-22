package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The worker is built once, but its next conversation belongs to the current
// launch. Starting it before the pin must not leave a manager or stage on the
// first launch's model, tiers or role pins.
func TestFactoryConversationsInheritALaterOneModelLaunch(t *testing.T) {
	const pinned = "deepseek/deepseek-v4.1-flash"
	model := newStageModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	proc := v3TestProcess(t)
	t.Setenv(factoryNoWorkerEnv, "1")
	workspace := resolvedTempDir(t)
	profile := oneModelProfile(t)
	launch, err := openV3Launch(proc, v3Options{Model: "old/model", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	launch.Config, err = applyV3Governance(launch.Config, profile, false, false)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previousParent := factoryWorkerParent()
	factoryProcess.once = sync.Once{}
	t.Cleanup(func() {
		factoryProcess.once = sync.Once{}
		setFactoryProcess(func() { factoryProcess.parent = previousParent })
	})
	startFactoryWorker(st, workspace, profile, launch.Config)
	maker := factoryStageMakerFrom(st, workspace, profile, factoryWorkerParent)
	launch, err = openV3Launch(proc, v3Options{Model: pinned, OneModel: true, Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	startFactoryWorker(st, workspace, profile, launch.Config)
	parent := factoryWorkerParent()
	assertFactoryPin(t, parent, pinned)
	call, ok := factoryTriageCall(func() (config.Config, error) {
		return config.Config{APIKey: "sk-test", BaseURL: model.server.URL, Model: "profile/default", ProfileDir: profile}, nil
	})
	if !ok {
		t.Fatal("no triage call for the pinned desk")
	}
	if _, err := call(context.Background(), "read this item"); err != nil {
		t.Fatal(err)
	}

	stageSettle = time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// A stage is a job's work: it charges that job's budget live, and a stage
	// with no job to charge stops before it spends.
	it, err := st.Create(factory.Item{Repo: "work", Kind: factory.KindIssue, Title: "look it over", Cap: 10,
		State: factory.StateNew, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if err != nil {
		t.Fatal(err)
	}
	spec := factoryrun.ConversationSpec{Item: it, Name: "review", Brief: "look it over", Dir: workspace}
	for _, name := range []string{"fresh", "resumed"} {
		t.Run(name, func(t *testing.T) {
			bucket, err := v3ProjectDir(workspace)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := maker.stageConfig(spec, bucket, workspace)
			if err != nil {
				t.Fatal(err)
			}
			assertFactoryPin(t, cfg, pinned)
			chat, err := maker.Open(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			defer chat.Close()
			if err := chat.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			spec.Resume = chat.ID()
		})
	}
	if it, err = st.Get(it.ID); err != nil {
		t.Fatal(err)
	}
	// The item's manager conversation is made as a run makes it, and a turn
	// the worker gives it opens on the pinned launch, not the first one.
	talk, err := managerMaker(st, workspace, profile)(ctx, it)
	if err != nil {
		t.Fatal(err)
	}
	it.Talk = talk
	turn, err := openManagerTurn(parent, workspace, profile, it, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if agent, ok := turn.(*session.Agent); !ok || agent.Model() != pinned {
		t.Fatalf("manager = %T; want an agent on %s", turn, pinned)
	}
	if err := turn.Close(); err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.models) == 0 {
		t.Fatal("no factory conversation reached the model endpoint")
	}
	for _, got := range model.models {
		if got != pinned {
			t.Errorf("factory requested %q; want %q", got, pinned)
		}
	}
}

func assertFactoryPin(t *testing.T, cfg session.Config, pinned string) {
	t.Helper()
	if !cfg.OneModel || cfg.Model != pinned || cfg.TaskModel != "" || cfg.RouteCrew != nil || len(cfg.ModelFallbacks) != 0 || cfg.NearestModels != nil {
		t.Fatal("factory conversation lost its one-model posture or retained another model route")
	}
	for _, role := range roles.Vocabulary() {
		switch role {
		case roles.RoleVision, roles.RoleImageGen, roles.RoleSpeech, roles.RoleVideo,
			// Reserved in this build: no conversation resolves it (internal/roles).
			roles.RoleConsolidate:
			continue
		}
		got, err := roles.Resolve(roles.Source(cfg.RolesSource), role, cfg.Model)
		if err != nil || got != pinned {
			t.Errorf("factory role %s = %q, %v; want %q", role, got, err, pinned)
		}
	}
}
