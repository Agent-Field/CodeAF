package cellindex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// madeChat is a cell whose only deliverable is report.md, cited in its ledger.
func madeChat(t *testing.T) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: c.Root}
	path := filepath.Join(place.Artifacts(), "report.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# report\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session.RecordSealedArtifact(place, session.Artifact{Path: path, Session: c.ID, Title: "report", Created: time.Now()})
	return c
}

func TestArtifactIndexOfAChatWithNoArtifacts(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	if !(artifactIndex{}).Present(c) {
		t.Fatal("a chat that made nothing owes the index nothing")
	}
}

// Reading on machine B after a move: the folder is somewhere else and B's own
// index is empty, yet the picker on B names the file where it is on B.
func TestArtifactIndexReadOnMachineBAfterAMove(t *testing.T) {
	c := madeChat(t)
	movedTo := filepath.Join(t.TempDir(), "elsewhere", filepath.Base(c.Root))
	if err := os.MkdirAll(filepath.Dir(movedTo), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(c.Root, movedTo); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, t.TempDir()) // B's home: an index that starts empty
	onB := cell.Cell{Root: movedTo, ID: c.ID}

	ix := artifactIndex{}
	if ix.Present(onB) {
		t.Fatal("B's empty index reports the chat's files as present")
	}
	if err := ix.Build(onB, session.Digest{}); err != nil {
		t.Fatal(err)
	}
	rows := session.ReadArtifacts(artifactsIndexPath())
	want := filepath.Join(movedTo, ".cell", "artifacts", "report.md")
	if len(rows) != 1 || rows[0].Path != want || rows[0].Session != c.ID {
		t.Fatalf("B's index = %+v, want one row at %s", rows, want)
	}
	if !ix.Present(onB) {
		t.Fatal("the index still misses the row it just built")
	}
}
