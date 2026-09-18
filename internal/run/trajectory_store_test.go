package run

import (
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppendTrajectoryFeedsExecutedCheckGate(t *testing.T) {
	dir := t.TempDir()
	store, err := plandb.Open(filepath.Join(dir, "plandb.db"), "p", "root", "root", "root")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "check: leaf", Role: plandb.RoleCheck, Checks: []string{"go test ./internal/widget"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim("review", "review"); err != nil {
		t.Fatal(err)
	}
	if err = appendTrajectory(dir, "review", Step{Kind: trajectoryStepKind, Step: 1, Command: "cd /tmp/tree && go test ./internal/widget"}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Done("review", "review", "holds: writer and reader agree", nil, nil); err != nil {
		t.Fatalf("store refused writer output: %v", err)
	}
}

func TestC249BackfillKeepsOnlyInvocableChecks(t *testing.T) {
	steps, err := Trajectory("testdata/c249", "jcrhzh")
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]string, 0, len(steps))
	characters := 0
	for _, step := range steps {
		commands = append(commands, step.Command)
		characters += utf8.RuneCountInString(step.Command)
	}
	if len(commands) != 37 || characters != 26312 {
		t.Fatalf("fixture commands = %d/%d characters, want 37/26312", len(commands), characters)
	}
	if got := session.InvocableChecks("/home/santosh/src/bl-c249", commands); len(got) != 0 {
		t.Fatalf("c249 backfill = %q, want empty contract", got)
	}
}
