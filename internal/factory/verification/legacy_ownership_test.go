package verification

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyOwnershipReconcilesDeletedFixturesAndKeepsUncertainEvidence(t *testing.T) {
	root, prefix := fakeChecks(t)
	owner, err := OwnerAt(root)
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "old.jsonl")
	runOwned(t, first, root, root, prefix+"go test ./...")
	checks, err := Read(first)
	if err != nil {
		t.Fatal(err)
	}
	original := checks[0]
	external := filepath.Join(t.TempDir(), "gone", "nested")
	for _, tc := range []struct {
		name, cwd  string
		diagnostic bool
	}{
		{"actual checkout", owner, false},
		{"outside deleted fixture", external, true},
		{"prefix sibling", owner + "-other", true},
		{"unknown cwd", "", false},
		{"relative cwd", "../fixture", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := original
			c.OwnerRoot = ""
			c.Cwd = tc.cwd
			exit := 7
			c.Exit = &exit
			target := filepath.Join(t.TempDir(), "inherited.jsonl")
			if err := Inherit(target, []Check{c}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(target + ".checks/inherited.json")
			got, err := ReadIn(target, root)
			if err != nil || len(got) != 1 || got[0].Diagnostic != tc.diagnostic {
				t.Fatalf("diagnostic=%v err=%v", got, err)
			}
			if got[0].Exit == nil || *got[0].Exit != 7 || got[0].Source != original.Source || got[0].ID != original.ID {
				t.Fatal("history changed")
			}
			after, _ := os.ReadFile(target + ".checks/inherited.json")
			if string(before) != string(after) {
				t.Fatal("reading recovery mutated saved records")
			}
			if tc.diagnostic && got[0].DiagnosticReason == "" {
				t.Fatal("missing disposition")
			}
		})
	}
}

func TestLegacyPassingCheckWithUnknownOwnershipIsNotProof(t *testing.T) {
	root, prefix := fakeChecks(t)
	first := filepath.Join(t.TempDir(), "first.jsonl")
	runOwned(t, first, root, root, prefix+"go test ./...")
	checks, err := Read(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{"", "relative/path"} {
		checks[0].OwnerRoot = ""
		checks[0].Cwd = cwd
		file := filepath.Join(t.TempDir(), "unknown.jsonl")
		if err := Inherit(file, checks); err != nil {
			t.Fatal(err)
		}
		got, err := ReadIn(file, root)
		if err != nil || len(got) != 1 || got[0].Diagnostic || got[0].Problem == "" {
			t.Fatal("unknown ownership became proof", got, err)
		}
	}
}
