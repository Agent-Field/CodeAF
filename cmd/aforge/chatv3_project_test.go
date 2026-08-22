package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The project-local layer reaching internal/session. It is the same contract
// chatv3_wiring_test.go asserts for the profile, one rung higher: the workspace
// this session runs in gets to answer these rows, and what it says beats what
// the machine says.

// v3Project writes one <workspace>/.aforge-v3/config.json and answers with the
// workspace directory.
func v3Project(t *testing.T, rows map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	path := config.ProjectConfigPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTheProjectFileBeatsTheProfileAtTheSessionConfig(t *testing.T) {
	profile := v3Profile(t, map[string]any{
		"tools.approvalMode":   "allow",
		"tools.approval":       "read:allow, bash:allow",
		"models.tiers.low":     "profile/cheap",
		"models.tiers.high":    "profile/capable",
		"models.roles":         "title:profile/title",
		"session.spendRailUSD": 9.0,
	})
	workspace := v3Project(t, map[string]any{
		"tools.approvalMode":   "prompt",
		"tools.approval":       "write:deny",
		"models.tiers.low":     "project/cheap",
		"models.tiers.high":    "project/capable",
		"models.roles":         "title:project/title",
		"session.spendRailUSD": 1.25,
	})

	cfg, err := applyV3Governance(session.Config{
		Workspace: workspace,
		Model:     "session/model",
	}, profile, false, false)
	if err != nil {
		t.Fatalf("the project layer did not load: %v", err)
	}

	// The gate: the repository's blanket answer, and the repository's
	// exceptions INSTEAD OF the person's — bash is no longer allowed, because
	// nothing deep-merges between the two FILES.
	//
	// read is the one entry that survives, and it does not survive from the
	// profile: it is on the built-in floor every gate is assembled over
	// (v3BuiltinApprovals). The floor is beneath BOTH files rather than part of
	// either, so it says nothing about which file wins — a repository that wants
	// to be asked about reads writes `read:prompt` and is obeyed, exactly as a
	// person is.
	policy := *cfg.ApprovalPolicy
	for _, want := range []struct {
		tool   string
		args   string
		action approval.Action
	}{
		{"write", `{"path":"x"}`, approval.ActionDeny},
		{"read", `{"path":"x"}`, approval.ActionAllow},
		{"bash", `{"command":"ls"}`, approval.ActionPrompt},
	} {
		if got := policy.Check(want.tool, json.RawMessage(want.args)); got.Action != want.action {
			t.Fatalf("%s %s → %s, want %s", want.tool, want.args, got, want.action)
		}
	}

	model, err := roles.Resolve(cfg.RolesSource, roles.RoleTitle, "session/model")
	if err != nil || model != "project/title" {
		t.Fatalf("the title role resolved to %q (%v), want the project's pin", model, err)
	}
	model, err = roles.Resolve(cfg.RolesSource, roles.RoleCompaction, "session/model")
	if err != nil || model != "project/capable" {
		t.Fatalf("compaction resolved to %q (%v), want the project's high tier", model, err)
	}
	if got, ok := roles.TierModel(cfg.RolesSource, roles.TierLow); !ok || got != "project/cheap" {
		t.Fatalf("the low tier is %q (ok=%v)", got, ok)
	}
	if cfg.SpendRailUSD != 1.25 {
		t.Fatalf("the ceiling is %v, want the project's 1.25", cfg.SpendRailUSD)
	}
}

// A workspace with no settings changes nothing at all: the person's profile is
// still the answer, and a directory that has never heard of aforge costs a
// stat and no error.
func TestAWorkspaceWithNoProjectFileLeavesTheProfileInCharge(t *testing.T) {
	profile := v3Profile(t, map[string]any{
		"tools.approvalMode":   "deny",
		"models.tiers.low":     "profile/cheap",
		"session.spendRailUSD": 3.0,
	})
	cfg, err := applyV3Governance(session.Config{
		Workspace: t.TempDir(),
		Model:     "session/model",
	}, profile, false, false)
	if err != nil {
		t.Fatalf("a bare workspace has to boot: %v", err)
	}
	if got := cfg.ApprovalPolicy.Check("edit", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionDeny {
		t.Fatalf("the profile's gate answered %s", got)
	}
	if got, ok := roles.TierModel(cfg.RolesSource, roles.TierLow); !ok || got != "profile/cheap" {
		t.Fatalf("the low tier is %q (ok=%v), want the profile's", got, ok)
	}
	if cfg.SpendRailUSD != 3.0 {
		t.Fatalf("the ceiling is %v, want the profile's 3", cfg.SpendRailUSD)
	}
}

// A broken project file stops the launch and says which file. It is the same
// law the approvals row already obeys, one layer up: a settings file that
// silently does not apply is how a rule somebody wrote gets skipped.
func TestABrokenProjectFileStopsTheLaunchAndNamesTheFile(t *testing.T) {
	workspace := t.TempDir()
	path := config.ProjectConfigPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tools.approvalMode": `), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := applyV3Governance(session.Config{Workspace: workspace}, t.TempDir(), false, false)
	if err == nil {
		t.Fatal("a truncated project file booted")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("the error does not name the file: %v", err)
	}

	// And a row a repository may not answer at all is refused rather than
	// honored — a checked-in file does not get to move somebody's budget.
	nested := v3Project(t, map[string]any{"tools": map[string]any{"approvalMode": "allow"}})
	if _, err := applyV3Governance(session.Config{Workspace: nested}, t.TempDir(), false, false); err == nil {
		t.Fatal("a nested project file booted, and its gate row would never have applied")
	}
}

// --yolo is still a default and not an override, one layer up: the flag widens
// what the repository did not write down and leaves what it did alone.
func TestYoloIsADefaultAgainstTheProjectFileToo(t *testing.T) {
	workspace := v3Project(t, map[string]any{"tools.approval": "bash:prompt"})
	cfg, err := applyV3Governance(session.Config{Workspace: workspace}, t.TempDir(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	policy := *cfg.ApprovalPolicy
	if got := policy.Check("edit", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionAllow {
		t.Fatalf("--yolo left the default at %s", got)
	}
	if got := policy.Check("bash", json.RawMessage(`{"command":"ls"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("--yolo overrode the repository's own rule: %s", got)
	}
}
