package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// embedStepAgent is a real conversation holding its journal's lock, the way
// the runner's stage agent holds a running step's chat: opened in this
// process, registered as live, nobody else able to open the file.
func embedStepAgent(t *testing.T) (*session.Agent, string, func(string) (*session.Agent, error)) {
	t.Helper()
	server := sourcestub.New("deepseek/deepseek-v4-flash")
	t.Cleanup(server.Close)
	sources := modelsource.NewSet(modelsource.Connected{
		Source: modelsource.DefaultSource(server.URL()), Key: "test-key", Address: server.URL(),
	})
	dir := filepath.Join(t.TempDir(), "90be26ee38c80e56")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(dir, "transcript.jsonl")
	workspace := t.TempDir()
	open := func(file string) (*session.Agent, error) {
		return session.New(session.Config{Workspace: workspace, Model: "deepseek/deepseek-v4-flash", Sources: sources, SessionFile: file})
	}
	agent, err := open(chat)
	if err != nil {
		t.Fatalf("open the step's conversation: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent, chat, open
}

// A RUNNING STEP'S CHAT IS LENT LIVE, NEVER OPENED A SECOND TIME: the window's
// own door, which the journal's lock would refuse, is not asked; the window is
// handed the step's own agent as a view; and the view going (a close, a
// detach) leaves the step running.
func TestFactoryEmbedLendsTheRunningStepsAgent(t *testing.T) {
	agent, chat, open := embedStepAgent(t)
	if _, err := open(chat); !errors.Is(err, session.ErrSessionLocked) {
		t.Fatalf("a second open of a held journal was not refused by its lock: %v", err)
	}
	asked := 0
	d := &sayDoor{}
	door := d.adopt(func(_, transcript string) (tui3.Conversation, error) {
		asked++
		a, err := open(transcript)
		if err != nil {
			return tui3.Conversation{}, err
		}
		return tui3.Conversation{Agent: a, SessionFile: transcript}, nil
	})
	conv, err := door("/work/CodeAF-1796", chat)
	if err != nil {
		t.Fatalf("the window was refused the running step's chat: %v", err)
	}
	if asked != 0 {
		t.Fatalf("the window's own door was asked %d times for a journal this process holds", asked)
	}
	view, ok := conv.Agent.(liveStepView)
	if !ok || view.Agent != agent {
		t.Fatalf("the window was handed %T, not the step's own agent", conv.Agent)
	}
	if conv.SessionFile != chat || conv.Workspace != "/work/CodeAF-1796" {
		t.Fatalf("the view names %q in %q", conv.SessionFile, conv.Workspace)
	}
	// The view can attach to the turn in flight and to every wake, as the
	// window does to any conversation it holds.
	if _, ok := conv.Agent.(interface {
		AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func())
	}); !ok {
		t.Fatal("the view cannot attach to the step's turn")
	}
	if err := conv.Agent.Close(); err != nil {
		t.Fatal(err)
	}
	if d, ok := conv.Agent.(interface{ Detach() error }); !ok || d.Detach() != nil {
		t.Fatal("the view cannot be detached")
	}
	if agent.Closed() {
		t.Fatal("the view going closed the step's agent")
	}
	if live, ok := session.LiveAgentFor(chat); !ok || live != agent {
		t.Fatal("the step's agent is no longer live after the view went")
	}
}

// A STEP WHOSE ROUND HAS ENDED IS OPENED FROM ITS JOURNAL: with its agent
// closed, the window's own door opens the transcript, and the box can carry
// the conversation on.
func TestFactoryEmbedOpensAFinishedStepFromItsJournal(t *testing.T) {
	agent, chat, open := embedStepAgent(t)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if !agent.Closed() {
		t.Fatal("a closed agent says it is open")
	}
	asked := 0
	door := (&sayDoor{}).adopt(func(_, transcript string) (tui3.Conversation, error) {
		asked++
		a, err := open(transcript)
		if err != nil {
			return tui3.Conversation{}, err
		}
		t.Cleanup(func() { _ = a.Close() })
		return tui3.Conversation{Agent: a, SessionFile: transcript}, nil
	})
	conv, err := door("", chat)
	if err != nil || asked != 1 {
		t.Fatalf("a finished step's chat did not open from its journal: %v (door asked %d)", err, asked)
	}
	if _, lent := conv.Agent.(liveStepView); lent {
		t.Fatal("a finished step's chat was lent as a view of a closed agent")
	}
}

// RESUMING A PAUSED STEP TAKES ITS JOURNAL BACK from a window's view of it:
// an idle conversation held on the journal is closed so the step can reopen
// it, and nothing held is nothing done.
func TestFactoryEmbedResumeTakesTheJournalBack(t *testing.T) {
	view, chat, open := embedStepAgent(t)
	if err := stageTakeJournal(chat); err != nil {
		t.Fatalf("an idle view refused the step its journal: %v", err)
	}
	if !view.Closed() {
		t.Fatal("the window's view still holds the journal")
	}
	step, err := open(chat)
	if err != nil {
		t.Fatalf("the step could not reopen its journal after the view let go: %v", err)
	}
	t.Cleanup(func() { _ = step.Close() })
	if err := stageTakeJournal(filepath.Join(t.TempDir(), "nobody.jsonl")); err != nil {
		t.Fatalf("a journal nobody holds was refused: %v", err)
	}
}
