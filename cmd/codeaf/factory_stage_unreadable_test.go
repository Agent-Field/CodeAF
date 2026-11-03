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
