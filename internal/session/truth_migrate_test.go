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
