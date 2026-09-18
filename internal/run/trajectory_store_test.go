package run

import (
	"path/filepath"
	"strings"
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

func TestC249BackfillKeepsExitBearingChainSegments(t *testing.T) {
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
	got := session.InvocableChecks("/home/santosh/src/bl-c249", commands)
	if len(got) == 0 || len(got) == len(commands) {
		t.Fatalf("c249 backfill kept %d checks, want real segments rather than 0 or 37: %q", len(got), got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"go test ./internal/tui3", "go build ./...", "go vet ./internal/tui3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("c249 backfill lacks %q: %q", want, got)
		}
	}
	for _, drop := range []string{"cat ", "sed ", "git status", "git grep", "git add", "plandb done"} {
		if strings.Contains(joined, drop) {
			t.Errorf("c249 backfill retained %q: %q", drop, got)
		}
	}
}

func TestRecordCheckFindingRecordsCheckAnswerOnLeaf(t *testing.T) {
	dir := t.TempDir()
	store, err := plandb.Open(filepath.Join(dir, "plandb.db"), "p", "root", "root", "root")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "leaf", Title: "leaf", ParentID: store.RootID()},
		{ID: "review", Title: "check: leaf", ParentID: store.RootID(), Role: plandb.RoleCheck},
	}); err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{store: store, checkOf: map[string]string{"review": "leaf"}}
	s.recordCheckFinding(*store.Task("review"), "check: fixture reading completed")
	notes := store.Notes("leaf", 0)
	if len(notes) != 1 || notes[0].Agent != "check" || notes[0].Body != "fixture reading completed" {
		t.Fatalf("leaf notes = %#v, want the check answer recorded by check", notes)
	}
}
