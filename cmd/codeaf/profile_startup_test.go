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

func TestUnreadableProfileOrdinaryStartupPrompts(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000 files")
	}
	t.Setenv("CODEAF_DAILY_BUDGET", "0")
	profile := t.TempDir()
	path := filepath.Join(profile, "config.json")
	if err := os.WriteFile(path, []byte(`{"tools.approvalMode":"allow","tools.approval":"edit:allow","tools.bashPatterns":"allow custom-command"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if limit, err := config.DailyBudgetUSDAt(profile); err != nil || limit != 0 {
		t.Fatalf("explicit budget pin: %v %v", limit, err)
	}
	cfg, err := applyV3Governance(session.Config{Workspace: t.TempDir()}, profile, false, true)
	if err != nil {
		t.Fatalf("ordinary interactive startup failed independently of spending authority: %v", err)
	}
	if got := cfg.ApprovalPolicy.Check("edit", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("unreadable policy edit action=%v; want prompt", got.Action)
	}
	if got := cfg.ApprovalPolicy.Check("bash", json.RawMessage(`{"command":"custom-command"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("unreadable policy bash action=%v; want prompt", got.Action)
	}
}
