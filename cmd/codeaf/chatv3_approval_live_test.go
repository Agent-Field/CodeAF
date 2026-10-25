package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestApprovalGateRejectsUnreadableProfiles(t *testing.T) {
	for _, broken := range []string{"malformed", "unreadable"} {
		for _, posture := range []string{"", session.PostureAllow} {
			t.Run(broken+"/"+posture, func(t *testing.T) {
				profile := v3Profile(t, map[string]any{
					config.KeyToolApprovalMode: "prompt",
					config.KeyToolApprovals:    "write:deny",
					config.KeyBashApprovals:    "deny git status",
				})
				gate := &v3ApprovalGate{workspace: t.TempDir(), profileDir: profile}
				first, _, err := gate.Build(posture)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(profile, "config.json")
				if broken == "malformed" {
					err = os.WriteFile(path, []byte(`{"tools.approvalMode":`), 0o600)
				} else {
					// A directory is unreadable as JSON even when tests run as root.
					if err = os.Rename(path, path+".saved"); err == nil {
						err = os.Mkdir(path, 0o700)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if policy, _, err := gate.Build(posture); err == nil || policy != nil {
					t.Fatalf("%s profile produced policy %+v, error %v; want a failed build", broken, policy, err)
				}
				if first.Check("write", json.RawMessage(`{"path":"example.go"}`)).Action != approval.ActionDeny {
					t.Fatal("failed rebuild changed the previous policy")
				}
			})
		}
	}
}

func TestApprovalGateReusesUnchangedRulesAndReadsSettingsChanges(t *testing.T) {
	profile := v3Profile(t, map[string]any{config.KeyToolApprovalMode: "allow"})
	gate := v3ApprovalGate{workspace: t.TempDir(), profileDir: profile}
	first, _, err := gate.Build("")
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := gate.Build("")
	if err != nil || again != first {
		t.Fatalf("unchanged settings rebuilt their immutable policy: %v", err)
	}
	row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(config.KeyToolApprovalMode)
	for _, mode := range []string{"prompt", "allow"} {
		if err := row.Apply(mode); err != nil {
			t.Fatal(err)
		}
		policy, _, err := gate.Build("")
		if err != nil || policy == nil || policy.Default != approval.Action(mode) {
			t.Fatalf("saved %s: policy = %+v, error = %v", mode, policy, err)
		}
	}
	for _, change := range []struct {
		key, value, tool, args string
	}{
		{config.KeyToolApprovals, "write:deny", "write", `{"path":"example.go"}`},
		{config.KeyBashApprovals, "deny git status", "bash", `{"command":"git status"}`},
	} {
		row, _ := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(change.key)
		if err := row.Apply(change.value); err != nil {
			t.Fatal(err)
		}
		policy, _, err := gate.Build("")
		if err != nil {
			t.Fatal(err)
		}
		if decision := policy.Check(change.tool, json.RawMessage(change.args)); decision.Action != approval.ActionDeny {
			t.Fatalf("saved %s: next decision = %+v, want deny", change.key, decision)
		}
	}
}

func TestApprovalGateReadsExternalConfigChanges(t *testing.T) {
	profile := v3Profile(t, map[string]any{config.KeyToolApprovalMode: "allow"})
	gate := v3ApprovalGate{workspace: t.TempDir(), profileDir: profile}
	if _, _, err := gate.Build(""); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"prompt", "allow"} {
		// An external writer does not advance this process's settings generation.
		data, _ := json.Marshal(map[string]string{config.KeyToolApprovalMode: mode})
		if err := os.WriteFile(filepath.Join(profile, "config.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		policy, _, err := gate.Build("")
		if err != nil || policy == nil || policy.Default != approval.Action(mode) {
			t.Fatalf("external %s: policy = %+v, error = %v", mode, policy, err)
		}
	}
}

func BenchmarkApprovalGateUnchangedSettings(b *testing.B) {
	profile, workspace := b.TempDir(), b.TempDir()
	fixture := []byte(`{"tools.approvalMode":"prompt","tools.approval":"write:deny","tools.bashPatterns":"allow git status"}`)
	if err := os.WriteFile(filepath.Join(profile, "config.json"), fixture, 0o600); err != nil {
		b.Fatal(err)
	}
	gate := v3ApprovalGate{workspace: workspace, profileDir: profile}
	if _, _, err := gate.Build(""); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, _, err := gate.Build(""); err != nil {
			b.Fatal(err)
		}
	}
}
