package main

import (
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
