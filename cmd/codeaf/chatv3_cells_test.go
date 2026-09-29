package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

func mintOpened(t *testing.T, bucket, workspace string) session.Place {
	t.Helper()
	place, err := v3MintSession(bucket, workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := openV3Agent(session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	}, workspace, v3OpenSession); err != nil {
		t.Fatalf("session did not open: %v", err)
	}
	return place
}

func TestNewChatIsACellWhenTheFlagIsOn(t *testing.T) {
	t.Setenv(cell.EnvVar, "1")
	bucket, workspace := t.TempDir(), t.TempDir()
	place := mintOpened(t, bucket, workspace)

	want := filepath.Join(place.Dir, cell.TranscriptPath)
	if place.Transcript() != want {
		t.Fatalf("transcript = %s, want %s", place.Transcript(), want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("transcript not written in .cell: %v", err)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, "transcript.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("a legacy transcript sits beside the cell: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(place.Dir, cell.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), bucket) || strings.Contains(string(raw), workspace) {
		t.Fatalf("cell meta holds an absolute path: %s", raw)
	}

	// Resume and listing find it through the same scan every launch uses.
	spoken, empty := v3ScanBucket(bucket)
	if len(spoken)+len(empty) != 1 || append(spoken, empty...)[0].dir != place.Dir {
		t.Fatalf("listing = %v %v, want the cell", spoken, empty)
	}
	if folder, ok := session.FolderOf(place.Transcript()); !ok || folder != place.Dir {
		t.Fatalf("FolderOf = %q %v", folder, ok)
	}
	if got := v3PlaceOf(place.Transcript(), workspace); got.Dir != place.Dir {
		t.Fatalf("v3PlaceOf dir = %q, want %q", got.Dir, place.Dir)
	}
}

func TestNewChatIsALegacyFolderWhenTheFlagIsOff(t *testing.T) {
	t.Setenv(cell.EnvVar, "")
	bucket, workspace := t.TempDir(), t.TempDir()
	place := mintOpened(t, bucket, workspace)

	if want := filepath.Join(place.Dir, "transcript.jsonl"); place.Transcript() != want {
		t.Fatalf("transcript = %s, want %s", place.Transcript(), want)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, cell.StateDir)); !os.IsNotExist(err) {
		t.Fatalf("flag off must not create .cell: %v", err)
	}
}
