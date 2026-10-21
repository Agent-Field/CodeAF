package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRuleMetadataOldSettingsAndOrigin(t *testing.T) {
	dir := t.TempDir()
	if err := RememberBashApproval(dir, "git status*"); err != nil {
		t.Fatal(err)
	}
	old, err := RuleMetadataAt(dir, "bash:git status*")
	if err != nil || old.Uses != 0 || !old.Origin.At.IsZero() {
		t.Fatalf("old rule = %+v, %v", old, err)
	}
	origin := RuleOrigin{Agent: "api-docs", Place: "api", At: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
	for _, err := range []error{RememberBashApproval(dir, "go test*", origin), RememberToolApproval(dir, "edit", "allow", origin)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"bash:go test*", "tool:edit"} {
		got, err := RuleMetadataAt(dir, id)
		if err != nil || got.Origin != origin {
			t.Fatalf("%s = %+v, %v", id, got, err)
		}
	}
	// Settings stay in the format readers predating the sidecar understand.
	if rules, err := ParseBashApprovals(BashApprovalsAt(dir)); err != nil || len(rules) != 2 {
		t.Fatalf("old parser = %v, %v", rules, err)
	}
	if rows, err := ParseToolApprovals(ToolApprovalsAt(dir)); err != nil || rows["edit"] != "allow" {
		t.Fatalf("old tool parser = %v, %v", rows, err)
	}
}

func TestRuleUsageBatchesAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 12, 14, 2, 0, 0, time.UTC)
	if _, err := UseApprovalRule(dir, "bash:go test*", at); err != nil {
		t.Fatal(err)
	}
	// Stop the timer explicitly: persistence is driven by settlement here,
	// rather than making the test depend on the scheduler's one-second clock.
	ruleUsage.Lock()
	ruleUsage.timers[dir].Stop()
	ruleUsage.Unlock()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = UseApprovalRule(dir, "bash:go test*", at) }()
	}
	wg.Wait()
	if _, err := os.Stat(filepath.Join(dir, "rule-metadata.json")); !os.IsNotExist(err) {
		t.Fatalf("hot path wrote sidecar: %v", err)
	}
	got, err := RuleMetadataAt(dir, "bash:go test*")
	if err != nil || got.Uses != 41 || !got.LastUsed.Equal(at) {
		t.Fatalf("pending = %+v, %v", got, err)
	}
	if err := FlushRuleUsage(dir); err != nil {
		t.Fatal(err)
	}
	// A fresh process has no access to the pending map or Go's test cache.
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuleUsageRestartReader$")
	cmd.Env = append(os.Environ(), "RULE_METADATA_TEST_PROFILE="+dir)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart: %v\n%s", err, raw)
	}
	if err := FlushRuleUsage(dir); err != nil {
		t.Fatal(err)
	}
	got, _ = RuleMetadataAt(dir, "bash:go test*")
	if got.Uses != 41 {
		t.Fatalf("flush counted twice: %+v", got)
	}
}

func TestRuleUsageRestartReader(t *testing.T) {
	dir := os.Getenv("RULE_METADATA_TEST_PROFILE")
	if dir == "" {
		t.Skip("subprocess reader")
	}
	got, err := RuleMetadataAt(dir, "bash:go test*")
	if err != nil || got.Uses != 41 || got.LastUsed.IsZero() {
		t.Fatalf("restart = %+v, %v", got, err)
	}
}

func TestRuleUsageKeepsFailedBatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rule-metadata.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = UseApprovalRule(dir, "tool:read", time.Now())
	if err := FlushRuleUsage(dir); err == nil {
		t.Fatal("malformed metadata overwritten")
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := FlushRuleUsage(dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var rows map[string]RuleMetadata
	if err := json.Unmarshal(raw, &rows); err != nil || rows["tool:read"].Uses != 1 {
		t.Fatalf("retried = %s, %v", raw, err)
	}
}

func TestRuleUsageMergesAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	if _, err := UseApprovalRule(dir, "tool:read", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Leave the parent's delta pending while the child writes its own batch.
	ruleUsage.Lock()
	ruleUsage.timers[dir].Stop()
	ruleUsage.Unlock()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuleUsageProcessWriter$")
	cmd.Env = append(os.Environ(), "RULE_METADATA_TEST_PROFILE="+dir)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("writer: %v\n%s", err, raw)
	}
	if err := FlushRuleUsage(dir); err != nil {
		t.Fatal(err)
	}
	got, err := RuleMetadataAt(dir, "tool:read")
	if err != nil || got.Uses != 2 {
		t.Fatalf("merged = %+v, %v", got, err)
	}
}

func TestRuleUsageProcessWriter(t *testing.T) {
	dir := os.Getenv("RULE_METADATA_TEST_PROFILE")
	if dir == "" {
		t.Skip("subprocess writer")
	}
	if _, err := UseApprovalRule(dir, "tool:read", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := FlushRuleUsage(dir); err != nil {
		t.Fatal(err)
	}
}
