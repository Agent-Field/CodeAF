package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func legacySession(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(dir, placeTranscript), "{\"v\":1}\n")
	putFile(t, filepath.Join(dir, placeState), "{\"state\":1}\n")
	putFile(t, filepath.Join(dir, placeTasks), "{\"tasks\":1}\n")
	putFile(t, filepath.Join(dir, placeTeamCursors), "{\"team\":1}\n")
	if err := SaveMeta(dir, Meta{ID: filepath.Base(dir), Title: "kept", Effort: "high", Approval: "allow", Archived: true}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func wantTruthMoved(t *testing.T, dir string) {
	t.Helper()
	for name, want := range map[string]string{placeState: "{\"state\":1}\n", placeTasks: "{\"tasks\":1}\n", placeTeamCursors: "{\"team\":1}\n"} {
		if got := slurp(t, filepath.Join(dir, cellStateDir, name)); got != want {
			t.Errorf(".cell/%s = %q", name, got)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s left at the top: %v", name, err)
		}
	}
	meta, err := LoadMeta(dir)
	if err != nil || meta.Effort != "high" || meta.Approval != "allow" || !meta.Archived || meta.Title != "kept" {
		t.Errorf("meta after migration = %+v, %v", meta, err)
	}
	if top := slurp(t, filepath.Join(dir, placeMeta)); strings.Contains(top, "effort") || !strings.Contains(top, "kept") {
		t.Errorf("meta.json must hold the summary alone:\n%s", top)
	}
}

func TestMigrationCarriesTheTruth(t *testing.T) {
	dir := legacySession(t)
	for range 2 { // the second call finds nothing left to do
		if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
			t.Fatal(err)
		}
		wantTruthMoved(t, dir)
	}
}

// A folder a migration left half done (cell in place, truth still at the top)
// is finished by the next call.
func TestMigrationFinishesAFolderThatMovedOnlyItsJournal(t *testing.T) {
	dir := legacySession(t)
	if err := cell.MigrateLegacy(dir); err != nil {
		t.Fatal(err)
	}
	if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
		t.Fatal(err)
	}
	wantTruthMoved(t, dir)
}

// legacyWithJournals is a legacy folder that also ran two tasks, one of them
// with a nested audit directory.
func legacyWithJournals(t *testing.T) (dir string, journals map[string]string) {
	t.Helper()
	dir = legacySession(t)
	journals = map[string]string{
		"20260824-100000_1.jsonl":            "{\"n\":1}\n",
		"20260824-100100_2.jsonl":            "{\"n\":2}\n",
		"audit/20260824-100200_2-audit.json": "{\"a\":2}\n",
	}
	for name, body := range journals {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, placeNodeJournals, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		putFile(t, filepath.Join(dir, placeNodeJournals, name), body)
	}
	return dir, journals
}

func wantJournalsMoved(t *testing.T, dir string, journals map[string]string) {
	t.Helper()
	for name, want := range journals {
		if got := slurp(t, filepath.Join(dir, cellStateDir, placeNodeJournals, name)); got != want {
			t.Errorf(".cell/tasks/%s = %q", name, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, placeNodeJournals)); !os.IsNotExist(err) {
		t.Errorf("tasks/ left at the top: %v", err)
	}
	if got := (Place{Dir: dir}).NodeJournals(); got != filepath.Join(dir, cellStateDir, placeNodeJournals) {
		t.Errorf("NodeJournals = %s", got)
	}
}

func TestMigrationCarriesTheNodeJournals(t *testing.T) {
	dir, journals := legacyWithJournals(t)
	for range 2 {
		if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
			t.Fatal(err)
		}
		wantJournalsMoved(t, dir, journals)
	}
}

// Every crash point of a migration opens and the next call finishes it: with
// the cell renamed into place and the journals still linked at both names, and
// with a journal a window wrote at the old name after the link.
func TestInterruptedMigrationFinishesTheNodeJournals(t *testing.T) {
	dir, journals := legacyWithJournals(t)
	if err := cell.MigrateLegacy(dir); err != nil { // the journal moved, nothing else
		t.Fatal(err)
	}
	late := "20260824-100300_3.jsonl"
	putFile(t, filepath.Join(dir, placeNodeJournals, late), "{\"n\":3}\n")
	journals[late] = "{\"n\":3}\n"
	if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
		t.Fatal(err)
	}
	wantJournalsMoved(t, dir, journals)
}

// A crash after every file is linked but before the legacy names go leaves two
// names for one file; the next call removes the legacy ones.
func TestMigrationDropsTheSecondNameOfALinkedJournal(t *testing.T) {
	dir, journals := legacyWithJournals(t)
	if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
		t.Fatal(err)
	}
	for name, body := range journals { // put the legacy names back as links
		to := filepath.Join(dir, placeNodeJournals, name)
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(dir, cellStateDir, placeNodeJournals, name), to); err != nil {
			t.Fatal(err)
		}
		_ = body
	}
	if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
		t.Fatal(err)
	}
	wantJournalsMoved(t, dir, journals)
}

// session.json written before the schema freeze spelled two keys in camelCase.
// It reads once, and the next write is snake_case only.
func TestLegacyTruthKeysReadOnceThenWriteSnakeCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	old := `{"V":1,"launchDir":"workspace:cmd","archived":true,"archivedTasks":{"t1":true}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readTruth(path)
	if err != nil || got.LaunchDir != "workspace:cmd" || !got.ArchivedTasks["t1"] || !got.Archived {
		t.Fatalf("legacy read: %+v %v", got, err)
	}
	if err := writeJSONAtomic(path, got); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	for _, key := range []string{`"launch_dir"`, `"archived_tasks"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("rewrite lacks %s: %s", key, raw)
		}
	}
	if strings.Contains(string(raw), "launchDir") || strings.Contains(string(raw), "archivedTasks") {
		t.Errorf("rewrite kept camelCase: %s", raw)
	}
}
