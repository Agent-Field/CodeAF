package session

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	_ "modernc.org/sqlite"
)

// wedgeTheStore takes the store's write lock from a second connection and holds
// it, which is what the resident looks like from here when it has stopped inside
// a transaction.
func wedgeTheStore(t *testing.T, path string) {
	t.Helper()
	other, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(10000)&_txlock=immediate")
	if err != nil {
		t.Fatalf("open the second connection: %v", err)
	}
	tx, err := other.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("take the write lock: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO events (ts, kind, node_id, payload) VALUES (?, ?, ?, ?)`,
		"2026-01-01T00:00:00Z", "held_by_the_test", "", "{}"); err != nil {
		t.Fatalf("write under the held lock: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback()
		_ = other.Close()
	})
}

// QUITTING IS NOT NEGOTIABLE. The chat log's drain used to be an unbounded wait
// on a store write that was itself an unbounded wait for a lock, so a quit with
// a wedged database behind it hung the process holding the person's terminal.
// It has a deadline now, and it says in the log what it left behind.
func TestClosingTheChatLogDoesNotWaitOnAWedgedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.db")
	brain, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.Memory = brain
	})
	if agent.chatlog == nil {
		t.Fatal("a session with a store opened no chat log")
	}
	wedgeTheStore(t, path)

	var said bytes.Buffer
	log.SetOutput(&said)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	agent.record(textMessage("user", "one last thing before I go"))

	start := time.Now()
	agent.chatlog.close()
	took := time.Since(start)

	if took > closeSettle+time.Second {
		t.Fatalf("closing the chat log took %s behind a wedged store; the deadline is %s", took, closeSettle)
	}
	if !strings.Contains(said.String(), "the session file has the whole transcript") {
		t.Fatalf("a close that gave up said nothing about it; log = %q", said.String())
	}
}

// And a close against a database nobody is holding still drains: the deadline is
// a ceiling on a wedged store, not a race the ordinary case has to win.
func TestClosingTheChatLogStillLandsTheLastWordsWhenTheStoreIsFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.db")
	brain, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = brain.Close() })

	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.Memory = brain
	})
	agent.record(textMessage("user", "the last thing I said"))
	agent.chatlog.close()

	messages, err := brain.Messages(agent.threadID(), 0, 0)
	if err != nil {
		t.Fatalf("read the thread back: %v", err)
	}
	found := false
	for _, message := range messages {
		if strings.Contains(message.Body, "the last thing I said") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the last words never reached the store; thread has %d message(s)", len(messages))
	}
}
