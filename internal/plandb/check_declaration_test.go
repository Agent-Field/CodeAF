package plandb

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A CHECK THAT CAN NEVER EARN HOLDS IS REFUSED BEFORE THE PLAN CHANGES. The
// shell CLI is the real admission door used by workers, including replacements.
func TestCliUnusableCheckDeclarationsChangeNothing(t *testing.T) {
	for _, command := range []string{
		"cd /tmp/tree && go test ./...",
		"go test ./... && go vet ./...",
		"go test ./...; go vet ./...",
		"go test ./... | tee result.txt",
		"go test ./... > result.txt",
		`grep "$(pwd)" README.md`,
		`test -f "$FILE"`,
		"go test ./...\ngo vet ./...",
		"sudo reboot",
		"*",
		"--help",
	} {
		for _, verb := range []string{"add", "set-checks"} {
			t.Run(verb+"/"+command, func(t *testing.T) {
				h := cliNewHarness(t)
				h.cliInitFresh()
				h.cliAdd("Existing", "existing", "--check", "go test ./...")
				argv := []string{"--db", h.db, "add", "Invalid", "--as", "invalid", "--check", command}
				if verb == "set-checks" {
					argv = []string{"--db", h.db, "task", "set-checks", "t-existing", "--check", command}
				}
				if code := h.run(argv...); code == 0 {
					t.Fatalf("unusable declaration was accepted: %q", command)
				}
				if !strings.Contains(h.errb.String(), "--check") {
					t.Fatalf("refusal gives no declaration remedy: %s", h.errb)
				}
				store := planReopen(t, h.db)
				defer store.Close()
				if store.Task("invalid") != nil || len(store.Tasks()) != 2 {
					t.Fatal("refused declaration changed the plan")
				}
				if got := store.Task("existing").Checks; !reflect.DeepEqual(got, []string{"go test ./..."}) {
					t.Fatalf("refused replacement changed checks: %q", got)
				}
			})
		}
	}
}

// BATCH ADMISSION AND REVISION OWN THE SAME CONTRACT AS THE CLI, and each
// refusal rolls back the entire write rather than keeping its valid prefix.
func TestStoreUnusableCheckDeclarationsAreAtomic(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
	defer store.Close()
	bad := []string{"go test ./... && go vet ./..."}
	if _, err := store.AddMany([]TaskSpec{
		{ID: "first", Title: "First", Checks: []string{"go test ./..."}},
		{ID: "bad", Title: "Bad", Checks: bad},
	}); err == nil {
		t.Fatal("batch admitted a check its completion gate cannot approve")
	}
	if store.Task("first") != nil || store.Task("bad") != nil {
		t.Fatal("refused batch kept a partial plan")
	}
	planAdd(t, store, TaskSpec{ID: "existing", Title: "Existing", Checks: []string{"go test ./..."}})
	title := "Changed"
	if _, err := store.Revise("existing", TaskPatch{Title: &title, Checks: &bad}); err == nil {
		t.Fatal("revision admitted an unusable check")
	}
	if task := store.Task("existing"); task.Title != "Existing" || !reflect.DeepEqual(task.Checks, []string{"go test ./..."}) {
		t.Fatalf("refused revision changed the contract: %#v", task.TaskSpec)
	}
}

func TestUnusableReviewCheckDoesNotReopenFinishedWork(t *testing.T) {
	store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
	defer store.Close()
	planAdd(t, store, TaskSpec{ID: "parent", Title: "Finished work"})
	planFinish(t, store, "parent", "worker", "finished")
	if _, err := store.AddReviewCheck(TaskSpec{
		ID: "review", Title: "Check", ParentID: "parent", Role: RoleCheck,
		Checks: []string{"go test ./... && go vet ./..."},
	}); err == nil {
		t.Fatal("unusable review declaration was admitted")
	}
	if store.Task("review") != nil || store.Task("parent").Status != StatusDone {
		t.Fatal("refused review changed finished work")
	}
}

// OLD ROWS STILL FACE THE COMPLETION GATE, even when their commands appear
// with zero exits. Admission must not turn those checks into reading contracts
// or prevent an unrelated question edit on work that already exists.
func TestLegacyUnusableChecksCannotEarnHoldsWithZeroExit(t *testing.T) {
	for _, command := range []string{"go test ./... && go vet ./...", "sudo reboot", "*"} {
		t.Run(command, func(t *testing.T) {
			store := planOpen(t, filepath.Join(t.TempDir(), "plan.db"))
			defer store.Close()
			planAdd(t, store, TaskSpec{ID: "review", Title: "Check", Role: RoleCheck})
			// Restore the bytes of a row admitted before this guard existed.
			if _, err := store.changeTask("review", func(_ *state, task *Task, _ time.Time) error {
				task.Checks = []string{command}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			question := "Which requirement remains?"
			if _, err := store.Revise("review", TaskPatch{Question: &question}); err != nil {
				t.Fatalf("legacy check blocked an unrelated question: %v", err)
			}
			if _, err := store.Claim("review", "checker"); err != nil {
				t.Fatal(err)
			}
			line, err := json.Marshal(map[string]any{"kind": "step", "command": command, "exit_code": 0})
			if err != nil {
				t.Fatal(err)
			}
			writeTrajectoryLines(t, store, "review", string(line))
			if _, err := store.Done("review", "checker", "holds: a command exited zero", nil, nil); err == nil {
				t.Fatal("an unusable legacy declaration earned holds")
			}
		})
	}
}

// SEPARATE COMMANDS AND QUOTED TEXT KEEP THEIR EXACT BYTES and earn holds only
// after every declaration appears with a zero exit in this checker's record.
func TestCliSeparateCheckDeclarationsCanEarnRecordedCompletion(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	checks := []string{"go test ./...", `grep 'alpha && beta' README.md`}
	h.cliAdd("Check", "review", "--role", RoleCheck, "--check", checks[0], "--check", checks[1])
	if code := h.run("--db", h.db, "go", "--agent", "checker"); code != 0 {
		t.Fatalf("claim: %s", h.errb)
	}
	if code := h.run("--db", h.db, "done", "t-review", "--agent", "checker", "--result", "holds: both checks passed"); code == 0 {
		t.Fatal("declaration without recorded runs earned completion")
	}
	store := planReopen(t, h.db)
	if got := store.Task("review").Checks; !reflect.DeepEqual(got, checks) {
		t.Fatalf("declarations changed: %q", got)
	}
	lines := []string{`{"kind":"begin","exits_recorded":true}`}
	for _, check := range checks {
		line, err := json.Marshal(map[string]any{"kind": "step", "command": "cd /tmp/tree && " + check, "exit_code": 0})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(line))
	}
	writeTrajectoryLines(t, store, "review", lines...)
	store.Close()
	if code := h.run("--db", h.db, "done", "t-review", "--agent", "checker", "--result", "holds: both checks passed"); code != 0 {
		t.Fatalf("recorded declarations did not earn completion: %s", h.errb)
	}
}
