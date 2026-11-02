package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE SAVED ONE-MODEL SETTING REACHES EVERY TEXT ROLE, as the launch flag does.
// The profile points every tier and pin at a different model; with
// `models.tasks.mode=one` the roles a conversation's errands climb, the
// crew-only ones included, answer the saved model and nothing else. The
// setting is read live: switching back to Auto mid-process gives the rows back.
func TestSavedOneModelSettlesEveryTextRoleLive(t *testing.T) {
	profile := factoryPolicyProfile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel,
		"models.tiers.worker":    "tier/worker",
		"models.roles":           "title:pinned/title\nplanner:pinned/planner",
	})
	cfg, err := applyV3Governance(session.Config{Workspace: t.TempDir(), Model: "conversation/model"}, profile, false, false)
	if err != nil {
		t.Fatal(err)
	}
	source := roles.Source(cfg.RolesSource)
	for _, ask := range []struct {
		role  roles.Role
		floor string
	}{
		{roles.RoleMarkReader, ""},
		{roles.RoleHandoff, ""},
		{roles.RoleTitle, "conversation/model"},
		{roles.RolePlanner, "conversation/model"},
		{roles.RoleRouter, "conversation/model"},
	} {
		got, err := roles.Resolve(source, ask.role, ask.floor)
		if err != nil || got != savedOneModel {
			t.Errorf("%s resolved to %q, %v; want the saved one model %q", ask.role, got, err, savedOneModel)
		}
	}

	auto := crewroute.ModeAuto
	if err := config.ApplyTaskModels(context.Background(), profile, config.TaskModelsChange{Mode: &auto}); err != nil {
		t.Fatal(err)
	}
	if got, err := roles.Resolve(source, roles.RoleTitle, "conversation/model"); err != nil || got != "pinned/title" {
		t.Fatalf("after switching to Auto the title resolved to %q, %v; want its pin again", got, err)
	}
}

// THE MODEL POOL'S JUDGE STANDS DOWN UNDER THE SAVED ONE-MODEL SETTING. Its
// whole job is to ask a model outside the crew, which one-model mode refuses,
// so a landing asks nobody and writes nothing, and neither does the sweep.
func TestPoolJudgeStandsDownUnderTheSavedOneModelSetting(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	profileDir := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one",
		config.KeyTaskModelsOne:  savedOneModel,
	})
	settings := config.Config{APIKey: "test-key"}
	var asked []string
	hook := poolJudgeHook(settings, profileDir, t.TempDir(), poolTestCatalog, poolTestAsk(settings, &asked), time.Now, "task")
	if hook != nil {
		hook(poolTestLanding())
	}
	pending := poolTestLanding()
	pending.ID, pending.Attempt = 9, 1
	if err := writePendingLanding(profileDir, "do", pending); err != nil {
		t.Fatal(err)
	}
	poolJudgeSweep(settings, profileDir, "", poolTestCatalog, poolTestAsk(settings, &asked), time.Now)
	if len(asked) != 0 {
		t.Fatalf("the judge asked %v under one-model mode", asked)
	}
	if _, err := os.Stat(record.OwnSheetPath(config.ProfilePath(profileDir, "pool"))); !os.IsNotExist(err) {
		t.Fatal("the judge wrote an own sheet under one-model mode")
	}
	if alreadyJudged(config.ProfilePath(profileDir, "pool"), pending.ID, pending.Attempt) {
		t.Fatal("a landing nobody judged was marked judged, so Auto could never score it")
	}
}
