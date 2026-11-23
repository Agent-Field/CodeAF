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

func TestReviewUnreadableAllowMustKeepDeny(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("mode 000 requires nonroot")
	}
	for _, door := range []string{"live allow", "yolo launch"} {
		t.Run(door, func(t *testing.T) {
			profile := v3Profile(t, map[string]any{config.KeyToolApprovalMode: "prompt", config.KeyToolApprovals: "write:deny", config.KeyBashApprovals: "deny git status"})
			workspace := t.TempDir()
			gate := &v3ApprovalGate{workspace: workspace, profileDir: profile}
			first, _, err := gate.Build(session.PostureAllow)
			if err != nil {
				t.Fatal(err)
			}
			args := json.RawMessage(`{"path":"example.go"}`)
			if first.Check("write", args).Action != approval.ActionDeny {
				t.Fatal("fixture did not deny write")
			}
			path := filepath.Join(profile, "config.json")
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(path, 0600)
			var policy *approval.Policy
			if door == "live allow" {
				policy, _, err = gate.Build(session.PostureAllow)
			} else {
				policy, err = v3Policy(workspace, profile, true)
			}
			if err != nil {
				t.Fatalf("interactive policy must open: %v", err)
			}
			for _, call := range []struct {
				name string
				args json.RawMessage
			}{{"write", args}, {"bash", json.RawMessage(`{"command":"git status"}`)}} {
				decision := policy.Check(call.name, call.args)
				t.Logf("%s with unreadable saved deny: %s", call.name, decision.Action)
				if decision.Action != approval.ActionPrompt {
					t.Errorf("unreadable saved deny: %s = %s; want prompt", call.name, decision.Action)
				}
				if !strings.Contains(decision.Rule, path) || !strings.Contains(decision.Rule, "could not be read") {
					t.Errorf("prompt does not explain failed file read: %s", decision.Rule)
				}
			}
			for _, posture := range []string{"", session.PostureAllow, session.PostureGuardian, session.PostureDeny} {
				gate.headless = true
				p, guardian, err := gate.Build(posture)
				if err != nil || p == nil || guardian {
					t.Fatalf("headless %q: policy=%+v guardian=%v err=%v", posture, p, guardian, err)
				}
				if got := p.Check("write", args); got.Action != approval.ActionPrompt || !strings.Contains(got.Rule, path) {
					t.Errorf("headless %q: %s; want prompt naming file", posture, got)
				}
			}
		})
	}
}
