package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
)

func TestSealWatchOutlivesDeferredConversationClose(t *testing.T) {
	retired := make(chan struct{})
	watch := &cellstore.SealWatch{OnRetire: func() { close(retired) }}
	file := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.Seals = watch; cfg.SessionFile = file })
	done := make(chan struct{})
	agent.beltRun = &beltRun{over: done}
	defer func() {
		select {
		case <-done:
		default:
			close(done)
		}
		agent.beltMu.Lock()
		defer agent.beltMu.Unlock()
		agent.beltRun = nil
	}()
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-retired:
		t.Fatal("watch retired before its workers and journal finished")
	default:
	}
	if !InUse(file) {
		t.Fatal("fixture did not retain its unfinished journal")
	}
	close(done)
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("completed local close retained its watch")
	}
	if InUse(file) {
		t.Fatal("watch retired before the journal closed")
	}
}
