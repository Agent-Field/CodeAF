package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAJournalledPersonMessageEnqueuesOrganize(t *testing.T) {
	home := t.TempDir()
	chat := filepath.Join(home, "aaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(chat, 0o700); err != nil {
		t.Fatal(err)
	}
	got := make(chan [2]string, 2)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(chat, "transcript.jsonl")
		config.Place = Place{Dir: chat}
		config.EnqueueOrganize = func(chatID, sourceRev string) {
			got <- [2]string{chatID, sourceRev}
		}
	})
	agent.mu.Lock()
	agent.recordUserLocked(userText("customers must authenticate"))
	agent.mu.Unlock()
	select {
	case pair := <-got:
		if pair[0] != "aaaaaaaaaaaaaaaa" {
			t.Fatalf("enqueued chat %q", pair[0])
		}
		if pair[1] == "" {
			t.Fatal("enqueued an empty source revision")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a journalled person message did not enqueue observe_and_organize")
	}
}

func TestASessionNoteDoesNotEnqueueOrganize(t *testing.T) {
	home := t.TempDir()
	chat := filepath.Join(home, "aaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(chat, 0o700); err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(chat, "transcript.jsonl")
		config.Place = Place{Dir: chat}
		config.EnqueueOrganize = func(string, string) { got <- struct{}{} }
	})
	agent.mu.Lock()
	note := userText("a task landed")
	note.authored = true
	agent.recordUserLocked(note)
	agent.mu.Unlock()
	select {
	case <-got:
		t.Fatal("a session-authored note enqueued organize")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestASlashCommandDoesNotEnqueueOrganize(t *testing.T) {
	home := t.TempDir()
	chat := filepath.Join(home, "aaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(chat, 0o700); err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(chat, "transcript.jsonl")
		config.Place = Place{Dir: chat}
		config.EnqueueOrganize = func(string, string) { got <- struct{}{} }
	})
	agent.mu.Lock()
	agent.recordUserLocked(userText("/status"))
	agent.mu.Unlock()
	select {
	case <-got:
		t.Fatal("a slash command enqueued organize")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestOrganizeWaitsForAConversationId(t *testing.T) {
	got := make(chan struct{}, 1)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "transcript.jsonl")
		config.EnqueueOrganize = func(string, string) { got <- struct{}{} }
	})
	agent.mu.Lock()
	agent.recordUserLocked(userText("customers must authenticate"))
	agent.mu.Unlock()
	select {
	case <-got:
		t.Fatal("a session with no conversation id enqueued organize")
	case <-time.After(200 * time.Millisecond):
	}
}
