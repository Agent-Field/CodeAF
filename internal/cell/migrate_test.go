package cell

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// carried is what the interrupted-state tests stage: the journal and one more file.
var carried = []Carrier{Files(legacyTranscript, "state.json")}

const journal = "{\"v\":1}\n{\"role\":\"user\"}\n"

func legacyDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func wantMigrated(t *testing.T, dir string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, TranscriptPath))
	if err != nil || string(got) != journal {
		t.Fatalf("cell transcript = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, StateDir, "state.json")); err != nil || string(got) != "{}\n" {
		t.Fatalf("carried file = %q, %v", got, err)
	}
	for _, gone := range []string{"transcript.jsonl", "state.json", stagingDir} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s left behind: %v", gone, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`"/[^"]`).Match(raw) {
		t.Fatalf("absolute path in meta: %s", raw)
	}
	if !regexp.MustCompile(`"` + string(HostBound) + `"`).Match(raw) {
		t.Errorf("migrated class is not %s: %s", HostBound, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, EnvPath)); err != nil {
		t.Errorf("env/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks")); err != nil {
		t.Errorf("tasks/: %v", err)
	}
}

func TestMigrateLegacy(t *testing.T) {
	dir := legacyDir(t)
	if err := MigrateLegacy(dir, Files("state.json")); err != nil {
		t.Fatal(err)
	}
	wantMigrated(t, dir)
	first, _ := os.ReadFile(filepath.Join(dir, MetaPath))
	if err := MigrateLegacy(dir, Files("state.json")); err != nil {
		t.Fatal(err)
	}
	wantMigrated(t, dir)
	if again, _ := os.ReadFile(filepath.Join(dir, MetaPath)); string(again) != string(first) {
		t.Fatal("second run rewrote meta")
	}
}

// Each test below stops the migration where a crash could, then checks the
// folder still reads its journal and that a second call finishes the job.
func TestMigrateInterruptedStates(t *testing.T) {
	for name, stop := range map[string]func(t *testing.T, dir string){
		"after staging": func(t *testing.T, dir string) {
			if err := stageCell(dir, carried); err != nil {
				t.Fatal(err)
			}
		},
		"after the rename": func(t *testing.T, dir string) {
			if err := stageCell(dir, carried); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, stagingDir, StateDir), filepath.Join(dir, StateDir)); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := legacyDir(t)
			stop(t, dir)
			if got, err := os.ReadFile(journalPath(dir)); err != nil || string(got) != journal {
				t.Fatalf("journal = %q, %v", got, err)
			}
			if err := MigrateLegacy(dir, Files("state.json")); err != nil {
				t.Fatal(err)
			}
			wantMigrated(t, dir)
		})
	}
}

// journalPath is where a reader looks: the cell's journal when the folder is a
// cell, the top-level one otherwise.
func journalPath(dir string) string {
	if isCell(dir) {
		return filepath.Join(dir, TranscriptPath)
	}
	return filepath.Join(dir, "transcript.jsonl")
}

func TestMigrateLeavesOtherFolders(t *testing.T) {
	empty := t.TempDir()
	if err := MigrateLegacy(empty); err != nil {
		t.Fatal(err)
	}
	if isCell(empty) {
		t.Fatal("a folder with no journal became a cell")
	}
	c, err := Create(t.TempDir(), Options{Class: FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateLegacy(c.Root); err != nil {
		t.Fatal(err)
	}
}
