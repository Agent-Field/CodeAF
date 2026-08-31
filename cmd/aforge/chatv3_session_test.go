package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A second window in the same directory resumes the same transcript, which the
// first one is holding open. It used to be handed a NEW conversation and one
// sentence about it, which is the defect this lane ends: the chat the person
// came back for was still running, and nothing led them to it. Now it is a
// refusal that says where the conversation is and what lets go of it.
func TestASecondWindowOnALockedSessionIsRefusedAndNamesTheWayOut(t *testing.T) {
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

	first, firstCfg, notice, err := openV3Agent(cfg, workspace, v3OpenSession)
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

	second, secondCfg, _, err := openV3Agent(cfg, workspace, v3OpenSession)
	if err == nil {
		_ = second.Close()
		t.Fatal("a contended resume opened a conversation anyway")
	}
	// THE SENTENCE IS THE FEATURE. It says where the conversation is, in the
	// words a person would use about it; it points at MOVING it here rather than
	// at starting a different one, which is what somebody who meets this
	// actually wants; and it names the one command that lets go of a workspace —
	// spelled with its --workspace, because without one that command means the
	// home directory.
	said := err.Error()
	for _, want := range []string{
		"open in another window",
		"press enter on it to move it here",
		"aforge engine --stop --workspace " + workspace,
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("the refusal said %q, which does not carry %q", said, want)
		}
	}
	// AND IT DOES NOT OFFER A NEW CONVERSATION AS THE WAY OUT. That was the old
	// answer, it answered a question nobody asked, and the manual quotes this
	// sentence — so a road back to it here would be a road back to it there.
	if strings.Contains(said, "start a new conversation") {
		t.Fatalf("the refusal still offers a new conversation: %q", said)
	}
	// AND NOTHING WAS MINTED ON THE WAY PAST. The old fallback left a second
	// folder in this project's bucket every time somebody opened a second
	// terminal; a refusal that still did that would be the same litter with a
	// worse ending.
	if secondCfg.SessionFile != transcript {
		t.Fatalf("the refused window moved the session file to %s", secondCfg.SessionFile)
	}
	spoken, empty := v3ScanBucket(sessionBucket(transcript))
	if len(spoken)+len(empty) != 1 {
		t.Fatalf("the refused window left %d folders in the bucket, want the one the first window is in", len(spoken)+len(empty))
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
	}, workspace, v3OpenSession)
	if err == nil {
		t.Fatal("a session with no model has to fail")
	}
	if notice != "" {
		t.Fatalf("a plain failure announced %q", notice)
	}
}

// LAUNCH-ON-LOCK: a terminal that meets a held journal is not left with
// nothing, and not left with something it did not ask for either. It opens a
// fresh conversation beside the held one AND carries that one's path to the
// surface, which lands on home with the row armed.
func TestALaunchThatMeetsALockOpensAFreshOneAndCarriesTheHeldRow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()

	found, err := v3ResolveSession("", workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
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

	first, _, _, err := openV3Agent(cfg, workspace, v3OpenSession)
	if err != nil {
		t.Fatalf("the first window did not open: %v", err)
	}
	defer func() { _ = first.Close() }()

	_, _, _, err = openV3Agent(cfg, workspace, v3OpenSession)
	// THE FAILURE IS TYPED AND CARRIES THE PATH, because the sentence
	// deliberately does not contain one and the surface needs one to point at.
	var held *sessionHeldElsewhere
	if !errors.As(err, &held) {
		t.Fatalf("a contended open answered %v, want a conversation held elsewhere", err)
	}
	if held.transcript != transcript {
		t.Fatalf("the refusal names %s, want the journal that is held, %s", held.transcript, transcript)
	}
	// And it still unwraps to the lock, so every errors.Is on this road answers
	// what it always answered.
	if !errors.Is(err, session.ErrSessionLocked) {
		t.Fatalf("%v no longer reads as a locked session", err)
	}

	second, fresh, err := v3TakeOverInstead(cfg, workspace)
	if err != nil {
		t.Fatalf("the second window was left with nothing: %v", err)
	}
	defer func() { _ = second.Close() }()

	if fresh.SessionFile == transcript {
		t.Fatal("the fresh conversation took the held journal")
	}
	// A second conversation about the same project is a second FOLDER in the
	// same bucket, never a second journal in the first one's folder.
	if directory := filepath.Dir(fresh.SessionFile); directory == filepath.Dir(transcript) {
		t.Fatalf("the fresh conversation shares the held one's folder: %s", directory)
	}
	if bucket := sessionBucket(fresh.SessionFile); bucket != sessionBucket(transcript) {
		t.Fatalf("the fresh conversation landed in %s, want this project's bucket %s", bucket, sessionBucket(transcript))
	}
	// AND THE HELD CONVERSATION IS UNTOUCHED. It is about to be offered on
	// home; a launch that had disturbed it on the way past would be offering
	// something it had already changed.
	if !session.InUse(transcript) {
		t.Fatal("the held conversation lost its lock while a fresh one was opened beside it")
	}
}
