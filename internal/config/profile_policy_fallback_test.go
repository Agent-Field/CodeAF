package config

import (
	"os"
	"strings"
	"testing"
)

func TestUnreadableProfilePolicyReadReportsFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000 files")
	}
	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			profile := t.TempDir()
			path := BudgetConfigPath(profile)
			if err := os.WriteFile(path, []byte(`{"tools.approvalMode":"allow","tools.approval":"edit:allow","tools.bashPatterns":"allow custom-command"}`), 0600); err != nil {
				t.Fatal(err)
			}
			project, err := LoadProjectConfig(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if warm {
				if got, err := project.ResolveString(profile, KeyToolApprovalMode); err != nil || got != "allow" {
					t.Fatalf("warm policy: %q, %v", got, err)
				}
			}
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(path, 0600)
			for _, key := range []string{KeyToolApprovalMode, KeyToolApprovals, KeyBashApprovals} {
				if _, err := project.ResolveString(profile, key); err == nil || !strings.Contains(err.Error(), path) {
					t.Errorf("%s: %v; want read failure naming %s", key, err, path)
				}
			}
			if got := ToolApprovalModeAt(profile); got != "prompt" {
				t.Errorf("ordinary mode = %q; want prompt", got)
			}
			if got, err := HeadlessToolApprovalModeAt(t.TempDir(), profile); err == nil || !strings.Contains(err.Error(), path) {
				t.Errorf("headless mode = %q, %v; want read failure naming %s", got, err, path)
			}
		})
	}
}
