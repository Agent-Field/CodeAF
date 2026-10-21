package session

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// sealedChat is a cell-layout session folder ready to hold deliverables.
func sealedChat(t *testing.T) Place {
	t.Helper()
	dir := legacySession(t)
	if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
		t.Fatal(err)
	}
	return Place{Dir: dir}
}

func cite(t *testing.T, place Place, name, body string) string {
	t.Helper()
	path := filepath.Join(place.Artifacts(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	putFile(t, path, body)
	RecordSealedArtifact(place, Artifact{Path: path, Session: place.ID(), Title: name, Created: time.Now()})
	return path
}

func TestArtifactsChatWithNoArtifacts(t *testing.T) {
	place := sealedChat(t)
	AdoptArtifacts(place, filepath.Join(t.TempDir(), "artifacts.jsonl"))
	if rows := SealedArtifacts(place.Dir); len(rows) != 0 {
		t.Fatalf("a chat that made nothing cites %+v", rows)
	}
	if _, err := os.Stat(place.ArtifactLedger()); !os.IsNotExist(err) {
		t.Fatalf("no ledger should exist yet: %v", err)
	}
}

func TestArtifactsInTheCellNotBesideIt(t *testing.T) {
	place := sealedChat(t)
	want := filepath.Join(place.Dir, cellStateDir, placeArtifacts)
	if place.Artifacts() != want {
		t.Fatalf("Artifacts() = %s, want %s", place.Artifacts(), want)
	}
}

func TestArtifactOverwrittenIsCitedOnce(t *testing.T) {
	place := sealedChat(t)
	cite(t, place, "report.md", "one")
	path := cite(t, place, "report.md", "two")
	rows := SealedArtifacts(place.Dir)
	if len(rows) != 1 || rows[0].Path != path {
		t.Fatalf("rows = %+v, want one for %s", rows, path)
	}
}

func TestArtifactRenamedAwayIsNotCited(t *testing.T) {
	place := sealedChat(t)
	old := cite(t, place, "draft.md", "x")
	if err := os.Rename(old, filepath.Join(place.Artifacts(), "final.md")); err != nil {
		t.Fatal(err)
	}
	if rows := SealedArtifacts(place.Dir); len(rows) != 0 {
		t.Fatalf("a file that is not there is cited: %+v", rows)
	}
}

// An old chat has its files in artifacts/ beside the cell and absolute rows in
// the machine index; one open brings the files and the rows into the cell.
func oldChat(t *testing.T, files map[string]string) (dir, index string) {
	t.Helper()
	dir = legacySession(t)
	index = filepath.Join(t.TempDir(), "artifacts.jsonl")
	for name, body := range files {
		path := filepath.Join(dir, placeArtifacts, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		putFile(t, path, body)
		RecordArtifact(index, Artifact{Path: path, Session: filepath.Base(dir), Title: name, Created: time.Now()})
	}
	return dir, index
}

func openOld(t *testing.T, dir, index string) Place {
	t.Helper()
	if err := cell.MigrateLegacy(dir, TruthCarriers()...); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: dir}
	AdoptArtifacts(place, index)
	return place
}

func TestArtifactMigrationAdoptsOldFilesAndRows(t *testing.T) {
	dir, index := oldChat(t, map[string]string{"a.md": "A", "sub/b.md": "B"})
	place := openOld(t, dir, index)
	if len(SealedArtifacts(dir)) != 2 {
		t.Fatalf("cited = %+v", SealedArtifacts(dir))
	}
	if _, err := os.Stat(filepath.Join(dir, placeArtifacts)); !os.IsNotExist(err) {
		t.Fatalf("artifacts/ left beside the cell: %v", err)
	}
	if got := slurp(t, filepath.Join(place.Artifacts(), "sub", "b.md")); got != "B" {
		t.Fatalf("b.md = %q", got)
	}
	AdoptArtifacts(place, index) // an open every time must not cite twice
	if n := len(SealedArtifacts(dir)); n != 2 {
		t.Fatalf("second adoption cites %d", n)
	}
}

func TestArtifactMigrationOfOldRowWithMissingFile(t *testing.T) {
	dir, index := oldChat(t, map[string]string{"kept.md": "K", "gone.md": "G"})
	if err := os.Remove(filepath.Join(dir, placeArtifacts, "gone.md")); err != nil {
		t.Fatal(err)
	}
	openOld(t, dir, index)
	rows := SealedArtifacts(dir)
	if len(rows) != 1 || rows[0].Title != "kept.md" {
		t.Fatalf("cited = %+v, want kept.md alone", rows)
	}
}

func TestArtifactMigrationOfALargeBinary(t *testing.T) {
	big := string(bytes.Repeat([]byte{0, 1, 2, 255}, 4<<20)) // 16 MiB
	dir, index := oldChat(t, map[string]string{"clip.bin": big})
	place := openOld(t, dir, index)
	if slurp(t, filepath.Join(place.Artifacts(), "clip.bin")) != big {
		t.Fatal("the binary changed in the move")
	}
	if len(SealedArtifacts(dir)) != 1 {
		t.Fatalf("cited = %+v", SealedArtifacts(dir))
	}
}

func TestArtifactLedgerHoldsNoAbsolutePath(t *testing.T) {
	place := sealedChat(t)
	cite(t, place, "r.md", "x")
	if got := slurp(t, place.ArtifactLedger()); bytes.Contains([]byte(got), []byte(place.Dir)) {
		t.Fatalf("ledger names the folder of this machine:\n%s", got)
	}
}
