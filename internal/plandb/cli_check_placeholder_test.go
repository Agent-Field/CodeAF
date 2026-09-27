package plandb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// placeholderChecks reads the stored declaration through the ordinary store
// door, so these CLI tests distinguish a refusal from a rewritten command.
func placeholderChecks(t *testing.T, h *cliHarness, id string) []string {
	t.Helper()
	store, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task := store.Task(id)
	if task == nil {
		t.Fatalf("task %s was not stored", id)
	}
	return task.Checks
}

func TestAddSendsBackAPlaceholderCheckBesideNumberedFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for n := 1; n <= 20; n++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("issue_%02d.go", n)), []byte("package main\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h := cliNewHarness(t)
	h.cliInitFresh()
	check := "gofmt -l issue_.go"
	code := h.run("--db", h.db, "add", "Reverse issue_07.go", "--description", "reverse the helper in issue_07.go", "--check", check)
	cliWantError(t, h, code, "Nothing was added.")
	for _, want := range []string{check, "issue_.go", "issue_01.go", "issue_02.go", "and 18 more", "gofmt -l issue_07.go", "each part's check", "number variable came out empty"} {
		if !strings.Contains(h.errb.String(), want) {
			t.Errorf("send-back %q misses %q", h.errb.String(), want)
		}
	}
	store, err := Open(h.db, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(store.Tasks()); got != 1 {
		t.Errorf("refused add stored %d tasks, want only the root", got)
	}
	_ = store.Close()

	corrected := "  gofmt -l issue_07.go  "
	id := h.cliAdd("Reverse issue_07.go", "corrected", "--description", "reverse the helper in issue_07.go", "--check", corrected)
	if got := placeholderChecks(t, h, strings.TrimPrefix(id, "t-")); len(got) != 1 || got[0] != corrected {
		t.Fatalf("corrected checks = %q, want the command byte-for-byte", got)
	}
}

func TestSetChecksSendsBackAPlaceholderCheck(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "issue_01.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("Reverse issue_01.go", "part", "--check", "go test ./...")
	code := h.run("--db", h.db, "task", "set-checks", "t-part", "--check", "gofmt -l issue_.go")
	cliWantError(t, h, code, "Nothing was changed.")
	if got := placeholderChecks(t, h, "part"); len(got) != 1 || got[0] != "go test ./..." {
		t.Fatalf("refused replacement changed checks to %q", got)
	}
}

func TestAddSendsBackAPlaceholderBesideTargetsThePlanNames(t *testing.T) {
	for _, tc := range []struct {
		name, priorTitle, title, description, sibling string
	}{
		{"prior task", "Reverse issue_03.go", "Reverse helper", "reverse the helper", "issue_03.go"},
		{"own work order", "Plan", "Reverse helper", "reverse issue_NN.go", "issue_NN.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			h := cliNewHarness(t)
			h.cliInitFresh()
			h.cliAdd(tc.priorTitle, "prior")
			code := h.run("--db", h.db, "add", tc.title, "--description", tc.description, "--check", "gofmt -l issue_.go")
			cliWantError(t, h, code, "Nothing was added.")
			if !strings.Contains(h.errb.String(), tc.sibling) {
				t.Fatalf("send-back %q misses sibling %q", h.errb.String(), tc.sibling)
			}
			store, err := Open(h.db, "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if got := len(store.Tasks()); got != 2 {
				t.Errorf("refused add stored %d tasks, want root and prior", got)
			}
			_ = store.Close()
		})
	}
}

func TestAddKeepsLegitimateChecksExactly(t *testing.T) {
	for _, tc := range []struct {
		name, check string
	}{
		{"future package", "go test ./internal/rank"},
		{"all packages", "go test ./..."},
		{"existing file", "gofmt -l main.go"},
		{"existing underscore file", "gofmt -l issue_.go"},
		{"no numbered sibling", "gofmt -l notes_.go"},
		{"glob", "gofmt -l issue_*.go"},
		{"shell variable", `test -f "$FILE"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			for _, file := range []string{"main.go", "issue_.go", "issue_01.go"} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte("package main\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			h := cliNewHarness(t)
			h.cliInitFresh()
			h.cliAdd("Check a part", "part", "--check", tc.check)
			if got := placeholderChecks(t, h, "part"); len(got) != 1 || got[0] != tc.check {
				t.Fatalf("stored checks = %q, want %q byte-for-byte", got, tc.check)
			}
		})
	}
}
