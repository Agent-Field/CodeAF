package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

const savedOneModel = "deepseek/deepseek-v4.1-flash"

// factoryPolicyProfile is a real profile on disk: the default service has a
// (fake) key, every other model setting points at a DIFFERENT model than the
// saved one, and nothing reads the network or a real credential.
func factoryPolicyProfile(t *testing.T, rows map[string]any) string {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	base := map[string]any{
		"api_key":                 "sk-test-not-real",
		"models.roles":            "planner:pinned/planner\ndesigner:pinned/designer",
		"models.tiers.low":        "tier/low",
		"models.tiers.high":       "tier/high",
		"models.tiers.reflex":     "tier/reflex",
		"models.tiers.mastermind": "tier/mastermind",
		"task.model":              "task/model",
		"models.fallbacks":        "fallback/one,fallback/two",
	}
	for k, v := range rows {
		base[k] = v
	}
	return v3Profile(t, base)
}

// The worker has no h.parent here: this is the production path, where the
// profile's saved Task models row is the only word on the model.
func TestFactoryParentFollowsSavedOneModelPolicy(t *testing.T) {
	for _, saved := range []string{savedOneModel, savedOneModel + "@openrouter"} {
		t.Run(saved, func(t *testing.T) {
			profile := factoryPolicyProfile(t, map[string]any{
				config.KeyTaskModelsMode: "one",
				config.KeyTaskModelsOne:  saved,
			})
			cfg, err := factoryGovernedParent(session.Config{Workspace: t.TempDir(), Model: "profile/default"}, profile)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Model != savedOneModel {
				t.Fatalf("model = %q; want exactly %q, not the default", cfg.Model, savedOneModel)
			}
			t.Logf("saved %q -> session model %q", saved, cfg.Model)
			assertFactoryPin(t, cfg, cfg.Model)
			send, one, err := factoryTaskOneModel(profile, cfg.Sources)
			if err != nil || !one || send != savedOneModel {
				t.Fatalf("triage seat = %q, %v, %v; want the same route %q", send, one, err, cfg.Model)
			}
			settings := config.Config{ProfileDir: profile, Model: "profile/default"}
			if got, err := factoryTriageModel(settings); err != nil || got != savedOneModel {
				t.Fatalf("standalone triage = %q, %v; want %q and never the low tier", got, err, cfg.Model)
			}
		})
	}
}

func TestFactoryParentAutoModeIsExactlyTheOrdinaryGovernance(t *testing.T) {
	profile := factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsOne: savedOneModel, // kept while the mode is auto
	})
	in := session.Config{Workspace: t.TempDir(), Model: "profile/default"}
	got, err := factoryGovernedParent(in, profile)
	if err != nil {
		t.Fatal(err)
	}
	want, err := applyV3Governance(in, profile, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "profile/default" || got.OneModel || got.RouteCrew == nil || got.RolesSource == nil ||
		got.TaskModel != want.TaskModel || len(got.ModelFallbacks) != len(want.ModelFallbacks) || len(got.ModelFallbacks) == 0 {
		t.Fatalf("auto mode changed the parent: %+v", got)
	}
	// The read rides insight_factory_read, whose unpinned ladder is the page
	// AI's default while its provider is connected (factory_insight_roles_test.go).
	if m, err := factoryTriageModel(config.Config{ProfileDir: profile, Model: "profile/default"}); err != nil || m != config.InsightDefault.Model {
		t.Fatalf("auto triage = %q, %v; want the page AI's default", m, err)
	}
}

func TestFactoryParentRefusesAnOneModelItCannotMeet(t *testing.T) {
	cases := map[string]map[string]any{
		"malformed pin":       {config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: "deepseek/deepseek-v4.1-flash@"},
		"no pin":              {config.KeyTaskModelsMode: "one"},
		"auto pin":            {config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: "auto"},
		"provider not linked": {config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: savedOneModel + "@nowhere"},
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			profile := factoryPolicyProfile(t, rows)
			cfg, err := factoryGovernedParent(session.Config{Workspace: t.TempDir(), Model: "profile/default"}, profile)
			if err == nil {
				t.Fatalf("built a parent on %q; want a refusal", cfg.Model)
			}
			if _, terr := factoryTriageModel(config.Config{ProfileDir: profile, Model: "profile/default"}); terr == nil {
				t.Fatal("triage fell back to another model instead of refusing")
			}
			refused := factoryRefusedParent(session.Config{Model: "profile/default", TaskModel: "task/model", ModelFallbacks: []string{"x"}})
			if refused.Model != "" || !refused.OneModel || refused.RouteCrew != nil || len(refused.ModelFallbacks) != 0 || refused.TaskModel != "" {
				t.Fatalf("refused parent still has a way to a model: %+v", refused)
			}
		})
	}
}

// resetFactoryParent isolates the process-wide launch parent a test sets, so
// one leaked OneModel parent cannot make another test order-dependent.
func resetFactoryParent(t *testing.T, parent session.Config) {
	t.Helper()
	var before session.Config
	setFactoryProcess(func() { before, factoryProcess.parent = factoryProcess.parent, parent })
	t.Cleanup(func() { setFactoryProcess(func() { factoryProcess.parent = before }) })
}

func assertSavedOneModelParent(t *testing.T, cfg session.Config) {
	t.Helper()
	if cfg.Model != savedOneModel || !cfg.OneModel {
		t.Fatalf("parent model=%q oneModel=%v; want exactly %q under one model", cfg.Model, cfg.OneModel, savedOneModel)
	}
	if cfg.RouteCrew != nil || cfg.RolesSource != nil || cfg.TaskModel != "" || len(cfg.ModelFallbacks) != 0 || cfg.NearestModels != nil {
		t.Fatalf("parent kept a route to another model: %+v", cfg)
	}
	if cfg.TaskLanded != nil || cfg.ContextWindow != 0 {
		t.Fatalf("parent kept the launch model's judge hook or window: landed=%v window=%d", cfg.TaskLanded != nil, cfg.ContextWindow)
	}
}

func savedOneProfile(t *testing.T) string {
	return factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel,
	})
}

// A window or engine launch builds its parent with the flag off and hands it in
// (startFactoryWorker); the host must still settle the saved policy on it.
func TestInjectedLaunchParentFollowsSavedOneModelPolicy(t *testing.T) {
	profile := savedOneProfile(t)
	launch, err := applyV3Governance(session.Config{Workspace: t.TempDir(), Model: "profile/default"}, profile, false, false)
	if err != nil {
		t.Fatal(err)
	}
	launch.TaskLanded = func(session.TaskLanding) {}
	launch.ContextWindow = 4096
	resetFactoryParent(t, launch)

	host := &appFactoryHost{profile: profile, workspace: t.TempDir(), parent: factoryWorkerParent}
	source, _, ok := host.parentSource()
	if !ok {
		t.Fatal("no parent source")
	}
	cfg := source.Config()
	assertSavedOneModelParent(t, cfg)
	assertFactoryPin(t, cfg, savedOneModel)
	if source.Refusal() != nil {
		t.Fatalf("refusal = %v", source.Refusal())
	}
	if tri, err := factoryTriageModel(config.Config{ProfileDir: profile, Model: "profile/default"}); err != nil || tri != savedOneModel {
		t.Fatalf("triage = %q, %v; want %q", tri, err, savedOneModel)
	}
}

// `codeaf runner join` with no flags starts from the zero Config.
func TestRunnerJoinZeroParentFollowsSavedOneModelPolicy(t *testing.T) {
	profile := savedOneProfile(t)
	resetFactoryParent(t, session.Config{})
	host := &appFactoryHost{profile: profile, workspace: t.TempDir(), parent: factoryWorkerParent}
	source, _, ok := host.parentSource()
	if !ok {
		t.Fatal("no parent source")
	}
	cfg := source.Config()
	assertSavedOneModelParent(t, cfg)
	if cfg.Sources.Empty() {
		t.Fatal("one-model parent has no service set to dispatch through")
	}
}

func TestExplicitLaunchPinWinsOverTheSavedPolicy(t *testing.T) {
	profile := savedOneProfile(t)
	launch := session.Config{Model: "explicit/pin", OneModel: true, TaskLanded: nil}
	got, err := factoryApplyTaskPolicy(launch, profile)
	if err != nil || got.Model != "explicit/pin" || !got.OneModel {
		t.Fatalf("got %q oneModel=%v err=%v; the launch pin must win", got.Model, got.OneModel, err)
	}
}

func TestAutoInjectedParentIsUntouched(t *testing.T) {
	profile := factoryPolicyProfile(t, map[string]any{config.KeyTaskModelsOne: savedOneModel})
	launch, err := applyV3Governance(session.Config{Workspace: t.TempDir(), Model: "profile/default"}, profile, false, false)
	if err != nil {
		t.Fatal(err)
	}
	launch.TaskLanded = func(session.TaskLanding) {}
	launch.ContextWindow = 4096
	got, err := factoryApplyTaskPolicy(launch, profile)
	if err != nil || got.Model != "profile/default" || got.OneModel || got.RouteCrew == nil || got.TaskLanded == nil || got.ContextWindow != 4096 || len(got.ModelFallbacks) == 0 {
		t.Fatalf("auto parent changed: %+v err=%v", got, err)
	}
}

// A refused conversation opens nothing: the person reads the reason and no
// session folder is left behind.
func TestRefusedStageNamesTheFixAndMintsNoSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", home)
	profile := factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel + "@nowhere",
	})
	launch := session.Config{Workspace: t.TempDir(), Model: "profile/default", TaskLanded: func(session.TaskLanding) {}}
	resetFactoryParent(t, launch)
	host := &appFactoryHost{profile: profile, workspace: launch.Workspace, parent: factoryWorkerParent}
	source, _, _ := host.parentSource()
	cfg := source.Config()
	if cfg.Model != "" || !cfg.OneModel || cfg.TaskLanded != nil {
		t.Fatalf("refused parent = %+v", cfg)
	}
	bucket, err := v3ProjectDir(launch.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadDir(bucket)
	maker := factoryStageMakerFrom(nil, launch.Workspace, profile, source.Config).withRefusal(source.Refusal)
	_, err = maker.Open(context.Background(), factoryrun.ConversationSpec{Name: "s", Brief: "b", Dir: launch.Workspace})
	if err == nil || strings.Contains(err.Error(), "model is required") || err.Error() != source.Refusal().Error() {
		t.Fatalf("Open error = %v; want the policy refusal %v", err, source.Refusal())
	}
	if after, _ := os.ReadDir(bucket); len(after) != len(before) {
		t.Fatalf("refusal minted a session folder: %d -> %d entries", len(before), len(after))
	}
	turns := factoryShapeTurnsFrom(nil, launch.Workspace, profile, source.Config)
	turns.refusal = source.Refusal
	if _, err := turns.open(context.Background(), factory.Item{Talk: "x"}, nil, false); err == nil || err.Error() != source.Refusal().Error() {
		t.Fatalf("manager open error = %v; want %v", err, source.Refusal())
	}
}

// A saved one model reaches the person's provider or is refused: a connection
// that does not serve the pinned vendor must not fall to the default service.
func TestOneModelNeverHopsToAnotherProvider(t *testing.T) {
	profile := factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  "openai/gpt-x@moonshot",
		"model_sources":          []map[string]any{{"id": "moonshot", "written": "moonshot", "key": "sk-moonshot-0123456789abcdef", "order": 1}},
	})
	if send, one, err := factoryTaskOneModel(profile, config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)); err == nil || !one {
		t.Fatalf("send=%q one=%v err=%v; want a refusal, not the default service", send, one, err)
	}
	if m, err := factoryTriageModel(config.Config{ProfileDir: profile, Model: "profile/default"}); err == nil {
		t.Fatalf("triage ran %q instead of refusing", m)
	}
	if cfg, err := factoryApplyTaskPolicy(session.Config{Model: "profile/default"}, profile); err == nil || cfg.Model != "" {
		t.Fatalf("policy produced %q, %v", cfg.Model, err)
	}
}

// The person's daily crew cap is not silently bypassed: the stages are not
// crew-routed, so one model refuses admission at the cap.
func TestOneModelRefusesAtTheDailyCrewCap(t *testing.T) {
	profile := factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel,
		config.KeyCrewCap:        1.0,
	})
	before := config.CrewHistory
	config.CrewHistory = func(string) config.CrewDay { return config.CrewDay{SpentUSD: 5} }
	t.Cleanup(func() { config.CrewHistory = before })
	_, one, err := factoryTaskOneModel(profile, config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL))
	if !one || !errors.Is(err, config.ErrCrewAtCap) {
		t.Fatalf("one=%v err=%v; want ErrCrewAtCap", one, err)
	}
	if cfg, err := factoryApplyTaskPolicy(session.Config{Model: "profile/default"}, profile); err == nil || cfg.Model != "" {
		t.Fatalf("admitted at the cap: %q %v", cfg.Model, err)
	}
}

func TestLiveWindowIsReusedOnlyWhenProvenOnThePin(t *testing.T) {
	one := savedOneProfile(t)
	auto := factoryPolicyProfile(t, nil)
	crew := func() session.Config { return session.Config{Model: "window/model"} }
	pin := func() session.Config { return session.Config{Model: "explicit/pin", OneModel: true} }
	if factoryLiveOK(crew, one)("window/model") {
		t.Error("saved one model used a window conversation on its own model")
	}
	if !factoryLiveOK(crew, auto)("window/model") {
		t.Error("auto lost the live road")
	}
	if !factoryLiveOK(pin, one)("explicit/pin") || factoryLiveOK(pin, one)("window/model") {
		t.Error("explicit pin must reuse only a conversation on that very pin")
	}
}

// A shared basename under two vendors is two models: identity is the whole
// canonical id, never the last segment.
func TestOneModelIdentityIsTheWholeCanonicalID(t *testing.T) {
	or := modelsource.Connected{Source: modelsource.DefaultSource(config.DefaultBaseURL), Key: "sk-test-not-real"}
	direct := modelsource.Connected{Source: modelsource.Source{ID: "moonshot", Written: "moonshot"}, Key: "sk-moonshot-0123456789abcdef"}
	set := modelsource.NewSet(or, direct)
	cases := []struct {
		name, provider, pin, picked, send string
		ok                                bool
	}{
		{"same tail, other vendor", "openrouter", "vendor-a/shared-model", "vendor-b/shared-model", "vendor-b/shared-model", false},
		{"bare send of other vendor", "openrouter", "vendor-a/shared-model", "vendor-a/shared-model", "vendor-b/shared-model", false},
		{"dated snapshot", "openrouter", "deepseek/deepseek-v4.1-flash", "deepseek/deepseek-v4.1-flash-20260101", "deepseek/deepseek-v4.1-flash-20260101", false},
		{"free pool", "openrouter", "deepseek/deepseek-v4.1-flash", "deepseek/deepseek-v4.1-flash:free", "deepseek/deepseek-v4.1-flash:free", false},
		{"exact", "openrouter", savedOneModel, savedOneModel, savedOneModel, true},
		{"exact with thinking level", "openrouter", savedOneModel + ":high", savedOneModel, savedOneModel + ":high", true},
		{"proven openrouter prefix", "openrouter", savedOneModel, savedOneModel, "openrouter/" + savedOneModel, true},
		{"direct drops the vendor on the wire", "moonshot", "moonshotai/kimi-k3", "moonshotai/kimi-k3", "moonshot/kimi-k3", true},
		{"direct, other model on the wire", "moonshot", "moonshotai/kimi-k3", "moonshotai/kimi-k3", "moonshot/kimi-k2", false},
		{"direct, catalog answered another vendor", "moonshot", "moonshotai/kimi-k3", "other/kimi-k3", "moonshot/kimi-k3", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := factoryCheckIdentity(set, c.provider, c.pin, c.picked, c.send)
			if c.ok && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("accepted a model that is not the pinned one")
			}
		})
	}
}
