package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

func mintOpened(t *testing.T, bucket, workspace string) session.Place {
	t.Helper()
	place, err := v3MintSession(bucket, workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := openV3Agent(session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	}, workspace, v3OpenSession)
	if err != nil {
		t.Fatalf("session did not open: %v", err)
	}
	// An agent left open keeps writing into the session folder after the test
	// returns, which races the temp-dir cleanup and fails the test at random.
	t.Cleanup(func() { _ = agent.Close() })
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

// legacyFolder writes a session folder the way a build without cells does: the
// real mint and the real agent, with a task journal beside the transcript.
func legacyFolder(t *testing.T, bucket, workspace string) session.Place {
	t.Helper()
	t.Setenv(cell.EnvVar, "")
	place, err := v3MintSession(bucket, workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := openV3Agent(v3TestConfig(workspace, place), workspace, v3OpenSession)
	if err != nil {
		t.Fatalf("legacy session did not open: %v", err)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(place.NodeJournals(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(place.NodeJournals(), "n1.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return place
}

func v3TestConfig(workspace string, place session.Place) session.Config {
	return session.Config{
		Workspace: workspace, Model: "test/model", APIKey: "test-key",
		BaseURL: "https://example.invalid/v1",
		Place:   place, SessionFile: place.Transcript(),
	}
}

func TestLegacyFolderMigratesOnResumeWhenTheFlagIsOn(t *testing.T) {
	bucket, workspace := t.TempDir(), t.TempDir()
	place := legacyFolder(t, bucket, workspace)
	before, err := os.ReadFile(place.Transcript())
	if err != nil || len(before) == 0 {
		t.Fatalf("legacy transcript: %v (%d bytes)", err, len(before))
	}

	t.Setenv(cell.EnvVar, "1")
	cfg := v3Migrated(v3TestConfig(workspace, place))
	want := filepath.Join(place.Dir, cell.TranscriptPath)
	if cfg.SessionFile != want {
		t.Fatalf("session file = %s, want %s", cfg.SessionFile, want)
	}
	after, err := os.ReadFile(want)
	if err != nil || string(after) != string(before) {
		t.Fatalf("transcript changed by migration: %v", err)
	}
	for _, gone := range []string{"transcript.jsonl", cell.StateDir + "-staging"} {
		if _, err := os.Stat(filepath.Join(place.Dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s left behind: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(place.NodeJournals(), "n1.jsonl")); err != nil {
		t.Errorf("task journal lost: %v", err)
	}
	if filepath.Base(place.Dir) != place.ID() || len(place.ID()) != 16 {
		t.Errorf("session id changed: %q", place.ID())
	}

	agent, _, _, err := openV3Agent(v3TestConfig(workspace, place), workspace, v3OpenSession)
	if err != nil {
		t.Fatalf("migrated session did not resume: %v", err)
	}
	_ = agent.Close()
	spoken, empty := v3ScanBucket(bucket)
	if len(spoken)+len(empty) != 1 {
		t.Fatalf("listing = %v %v, want the migrated session", spoken, empty)
	}
}

func TestLegacyFolderStaysLegacyWhenTheFlagIsOff(t *testing.T) {
	bucket, workspace := t.TempDir(), t.TempDir()
	place := legacyFolder(t, bucket, workspace)

	cfg := v3Migrated(v3TestConfig(workspace, place))
	if cfg.SessionFile != place.Transcript() {
		t.Fatalf("session file moved: %s", cfg.SessionFile)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, cell.StateDir)); !os.IsNotExist(err) {
		t.Fatalf("flag off must not create .cell: %v", err)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, "transcript.jsonl")); err != nil {
		t.Fatalf("legacy transcript gone: %v", err)
	}
}

func TestDeclareClassFollowsTheCell(t *testing.T) {
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	declareClass(session.Config{Place: session.Place{Dir: c.Root, Workspace: workspace}})
	if got := executor.ClassOf(workspace); got != executor.Sandboxed {
		t.Fatalf("class = %d, want sandboxed", got)
	}
	other := t.TempDir()
	declareClass(session.Config{Place: session.Place{Dir: t.TempDir(), Workspace: other}})
	if got := executor.ClassOf(other); got != executor.HostBound {
		t.Fatalf("a folder that is no cell declared %d, want the legacy rule", got)
	}
}
