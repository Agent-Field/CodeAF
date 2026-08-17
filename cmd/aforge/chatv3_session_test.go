package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A second window in the same directory resumes the same transcript, which the
// first one is holding open. That is the ordinary case, not the exotic one, and
// it must not be a crash.
func TestASecondWindowOnALockedSessionStartsANewOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()

	found, err := v3ResolveSession("", workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if found.Resumed {
		t.Fatal("a directory that has never held a session cannot resume one")
	}
	transcript := found.Transcript
	cfg := session.Config{
		Workspace:   workspace,
		Model:       "test/model",
		APIKey:      "test-key",
		BaseURL:     "https://example.invalid/v1",
		SessionFile: transcript,
		Place:       found.Place,
	}

	first, firstCfg, notice, err := openV3Agent(cfg, workspace)
	if err != nil {
		t.Fatalf("the first window did not open: %v", err)
	}
	defer func() { _ = first.Close() }()
	if notice != "" {
		t.Fatalf("the first window announced %q", notice)
	}
	if firstCfg.SessionFile != transcript {
		t.Fatalf("the first window moved its own file to %s", firstCfg.SessionFile)
	}

	second, secondCfg, notice, err := openV3Agent(cfg, workspace)
	if err != nil {
		t.Fatalf("a contended resume must not fail: %v", err)
	}
	defer func() { _ = second.Close() }()
	if notice != "session open elsewhere — started a new one" {
		t.Fatalf("the second window said %q", notice)
	}
	if secondCfg.SessionFile == transcript {
		t.Fatal("the second window took the locked file")
	}
	// A second conversation about the same project is a second FOLDER in the
	// same bucket, never a second journal in the first one's folder.
	if directory := filepath.Dir(secondCfg.SessionFile); directory == filepath.Dir(transcript) {
		t.Fatalf("the new session shares the first one's folder: %s", directory)
	}
	if bucket := sessionBucket(secondCfg.SessionFile); bucket != sessionBucket(transcript) {
		t.Fatalf("the new session landed in %s, want this project's bucket %s", bucket, sessionBucket(transcript))
	}
	if !strings.HasSuffix(secondCfg.SessionFile, ".jsonl") {
		t.Fatalf("the new session is named %s", secondCfg.SessionFile)
	}
	if _, err := os.Stat(secondCfg.SessionFile); err != nil {
		t.Fatalf("the new session file was not created: %v", err)
	}
}

// sessionBucket is the project directory above one session folder.
func sessionBucket(transcript string) string {
	return filepath.Dir(filepath.Dir(transcript))
}

// An error that is NOT the lock is still an error: the fallback is for one
// condition, and a launcher that swallowed the rest would hide a broken config
// behind a new empty session.
func TestAnOrdinaryFailureIsStillAFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	_, _, notice, err := openV3Agent(session.Config{
		Workspace: workspace, Model: "", APIKey: "k", BaseURL: "https://example.invalid/v1",
	}, workspace)
	if err == nil {
		t.Fatal("a session with no model has to fail")
	}
	if notice != "" {
		t.Fatalf("a plain failure announced %q", notice)
	}
}
