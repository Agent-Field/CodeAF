package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The settings rows reaching internal/session: the tool gate, the auxiliary
// models, and the ceiling. This is the wiring wave's whole contract with the
// registry, so it is tested against a real profile directory rather than a
// hand-built map.

// v3Profile writes one profile config.json and answers with its directory.
func v3Profile(t *testing.T, rows map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTheSettingsRowsReachTheSessionConfig(t *testing.T) {
	dir := v3Profile(t, map[string]any{
		"tools.approvalMode":     "allow",
		"tools.approval":         "bash:prompt, write:deny",
		"models.tiers.low":       "cheap/model",
		"models.tiers.high":      "capable/model",
		"models.roles":           "title:pinned/model",
		"session.spendRailUSD":   4.5,
		"unrelated_other_person": "left alone",
	})

	cfg, err := applyV3Governance(session.Config{Model: "session/model"}, dir, false)
	if err != nil {
		t.Fatalf("the rows did not load: %v", err)
	}

	// The gate: the mode is the default, the exceptions beat it, and the bash
	// pattern floor still stands over an allow.
	if cfg.ApprovalPolicy == nil {
		t.Fatal("no policy reached the session")
	}
	policy := *cfg.ApprovalPolicy
	for _, want := range []struct {
		tool   string
		args   string
		action approval.Action
	}{
		{"read", `{}`, approval.ActionAllow},                           // the default
		{"bash", `{"command":"go test ./..."}`, approval.ActionPrompt}, // the exception
		{"write", `{"path":"x"}`, approval.ActionDeny},
		{"bash", `{"command":"rm -rf /"}`, approval.ActionPrompt}, // matched, not judged
	} {
		if got := policy.Check(want.tool, json.RawMessage(want.args)); got.Action != want.action {
			t.Fatalf("%s %s → %s, want %s", want.tool, want.args, got, want.action)
		}
	}

	// The auxiliary models: a pin beats a tier, a tier beats the session model,
	// and an unset role still resolves — to the model the person already pays for.
	if cfg.RolesSource == nil {
		t.Fatal("no roles source reached the session")
	}
	model, err := roles.Resolve(cfg.RolesSource, roles.RoleTitle, "session/model")
	if err != nil || model != "pinned/model" {
		t.Fatalf("the title role resolved to %q (%v), want the pin", model, err)
	}
	model, err = roles.Resolve(cfg.RolesSource, roles.RoleCompaction, "session/model")
	if err != nil || model != "capable/model" {
		t.Fatalf("compaction resolved to %q (%v), want the high tier", model, err)
	}
	if got, ok := roles.TierModel(cfg.RolesSource, roles.TierLow); !ok || got != "cheap/model" {
		t.Fatalf("the low tier is %q (ok=%v)", got, ok)
	}

	if cfg.SpendRailUSD != 4.5 {
		t.Fatalf("the ceiling is %v, want 4.5", cfg.SpendRailUSD)
	}
}

func TestAnEmptyProfileGatesEverythingAndFollowsTheSessionModel(t *testing.T) {
	cfg, err := applyV3Governance(session.Config{Model: "session/model"}, t.TempDir(), false)
	if err != nil {
		t.Fatalf("a fresh install has to boot: %v", err)
	}
	// The strictest default is the one an unconfigured install gets: a session
	// with no settings should ask, not run.
	if got := cfg.ApprovalPolicy.Check("bash", json.RawMessage(`{"command":"ls"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("an unconfigured gate answered %s", got)
	}
	// Pure reads never ask — the gate's business is what can change the system.
	if got := cfg.ApprovalPolicy.Check("read", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionAllow {
		t.Fatalf("a fresh install asked about a read: %s", got)
	}
	// And every auxiliary call rides the model the person already has.
	model, err := roles.Resolve(cfg.RolesSource, roles.RoleTitle, "session/model")
	if err != nil || model != "session/model" {
		t.Fatalf("the floor resolved to %q (%v)", model, err)
	}
	if cfg.SpendRailUSD != 0 {
		t.Fatalf("an unset ceiling is %v, want 0", cfg.SpendRailUSD)
	}
}

func TestAnUnreadableRowStopsTheLaunchAndNamesItself(t *testing.T) {
	// A typo in the approvals row is a rule somebody thinks is protecting them.
	// It must not be skipped, and the message must say which row to open.
	dir := v3Profile(t, map[string]any{"tools.approval": "bash:always"})
	if _, err := applyV3Governance(session.Config{}, dir, false); err == nil {
		t.Fatal("a malformed approvals row booted")
	} else if !strings.Contains(err.Error(), "tools.approval") || !strings.Contains(err.Error(), "always") {
		t.Fatalf("the error does not name the row and the value: %v", err)
	}

	dir = v3Profile(t, map[string]any{"tools.approval": "bash"})
	if _, err := applyV3Governance(session.Config{}, dir, false); err == nil {
		t.Fatal("a row that is not a pair booted")
	}

	dir = v3Profile(t, map[string]any{"models.roles": "title"})
	_, err := applyV3Governance(session.Config{}, dir, false)
	if err == nil || !strings.Contains(err.Error(), "models.roles") {
		t.Fatalf("a malformed roles row has to name itself: %v", err)
	}
}

func TestYoloIsADefaultAndNotAnOverride(t *testing.T) {
	dir := v3Profile(t, map[string]any{
		"tools.approvalMode": "prompt",
		"tools.approval":     "bash:prompt",
	})
	cfg, err := applyV3Governance(session.Config{}, dir, true)
	if err != nil {
		t.Fatal(err)
	}
	policy := *cfg.ApprovalPolicy
	// The posture: everything the person did not write down now runs.
	if got := policy.Check("edit", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionAllow {
		t.Fatalf("--yolo left the default at %s", got)
	}
	// What they DID write down still stands. --yolo is "stop asking me about the
	// ordinary things", not "forget the rules I wrote".
	if got := policy.Check("bash", json.RawMessage(`{"command":"ls"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("--yolo overrode a written rule: %s", got)
	}
	// And without it the same profile asks.
	cfg, err = applyV3Governance(session.Config{}, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ApprovalPolicy.Check("edit", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("the gate answered %s without --yolo", got)
	}
}
