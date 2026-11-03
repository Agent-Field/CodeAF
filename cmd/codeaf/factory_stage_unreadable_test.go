package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestStageUnreadableProfileRequiresPerson(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("mode 000 requires nonroot")
	}
	profile := v3Profile(t, map[string]any{config.KeyToolApprovalMode: "prompt", config.KeyToolApprovals: "write:deny"})
	file := filepath.Join(profile, "config.json")
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
	rows := &v3ApprovalGate{workspace: t.TempDir(), profileDir: profile, headless: true}
	for _, posture := range []string{session.PostureAllow, session.PostureAsk, session.PostureDeny} {
		t.Run(posture, func(t *testing.T) {
			var gate session.ApprovalGate = stageApprovalGate{rows: rows, posture: posture}
			policy, guardian, err := gate.Build(posture)
			if err != nil {
				t.Fatal(err)
			}
			decision := policy.Check("write", json.RawMessage(`{"path":"x","content":"x"}`))
			t.Logf("stage write: %s", decision)
			if decision.Action != approval.ActionPrompt || guardian || !strings.Contains(decision.Rule, file) || !strings.Contains(decision.Rule, "could not be read") {
				t.Fatalf("unreadable saved denial must require a person with a clear reason: decision=%+v guardian=%v", decision, guardian)
			}
		})
	}
}

func TestStageProfileReadFailureWinsOverProjectParseError(t *testing.T) {
	profile, workspace := t.TempDir(), t.TempDir()
	file := filepath.Join(profile, "config.json")
	if err := os.Mkdir(file, 0o700); err != nil {
		t.Fatal(err)
	}
	_, readErr := config.ReadProfileApprovalConfig(profile)
	if readErr == nil {
		t.Fatal("expected a filesystem read failure")
	}
	if err := os.Mkdir(filepath.Join(workspace, ".codeaf"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".codeaf", "config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, posture := range []string{session.PostureAllow, session.PostureAsk, session.PostureDeny, session.PostureGuardian, session.PostureAuto} {
		t.Run(posture, func(t *testing.T) {
			var gate session.ApprovalGate = stageApprovalGate{rows: &v3ApprovalGate{workspace: workspace, profileDir: profile}, posture: posture}
			policy, guardian, err := gate.Build(posture)
			if err != nil {
				t.Fatalf("known profile failure lost to project error: %v", err)
			}
			decision := policy.Check("write", json.RawMessage(`{"path":"notes.txt","content":"no"}`))
			if guardian || decision.Action != approval.ActionPrompt || !strings.Contains(decision.ProfileReadError, file) {
				t.Fatalf("lost profile failure: guardian=%v decision=%+v", guardian, decision)
			}
		})
	}
}

func TestStageReadableMalformedProfileKeepsDefaultAllowance(t *testing.T) {
	for _, contents := range []string{"{", "null", "[]"} {
		t.Run(contents, func(t *testing.T) {
			profile := t.TempDir()
			if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			rows := &v3ApprovalGate{workspace: t.TempDir(), profileDir: profile}
			var gate session.ApprovalGate = stageApprovalGate{rows: rows, posture: session.PostureAllow}
			policy, guardian, err := gate.Build(session.PostureAllow)
			if err != nil {
				t.Fatal(err)
			}
			decision := policy.Check("write", json.RawMessage(`{"path":"notes.txt","content":"ok"}`))
			if guardian || decision.Action != approval.ActionAllow || decision.ProfileReadError != "" {
				t.Fatalf("readable default-stage bypass changed: guardian=%v decision=%+v", guardian, decision)
			}
			if _, _, err := gate.Build(session.PostureAsk); err == nil {
				t.Fatal("readable malformed JSON must still fail ordinary row construction")
			}
		})
	}
}
