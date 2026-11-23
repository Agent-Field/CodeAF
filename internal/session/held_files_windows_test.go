package session

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/store"
)

// A real os.Open reader denies replacement on Windows until it closes. Join it
// before returning so even the unfixed writer's early failure leaves no handle.
func whileWindowsReaderHolds(t *testing.T, path string, write func()) {
	t.Helper()
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	returned, closed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(closed)
		select {
		case <-time.After(100 * time.Millisecond):
		case <-returned:
		}
		_ = reader.Close()
	}()
	defer func() { close(returned); <-closed }()
	write()
}

func TestC1TaskCheckpointWaitsForWindowsReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.tasks.json")
	graph := newTaskGraph()
	graph.store = newTaskStore(path)
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "save progress", brief: "b", acceptance: "a"})
	node := graph.node(id)
	graph.mu.Lock()
	node.cost, node.input, node.output = 0.02, 10, 5
	graph.mu.Unlock()
	var messages bytes.Buffer
	old := log.Writer()
	log.SetOutput(&messages)
	defer log.SetOutput(old)
	whileWindowsReaderHolds(t, path, func() {
		node.finish("landed", nil, "", mergeInPlace)
		graph.complete(node, TaskDone)
	})
	if strings.Contains(messages.String(), "could not write the task checkpoint") {
		t.Fatalf("a transient reader lost a checkpoint: %s", messages.String())
	}
	record := recordOf(t, readCheckpoint(t, path), id)
	if record.State != TaskDone || record.CostUSD != 0.02 || record.Input != 10 || record.Output != 5 {
		t.Fatalf("landed bill did not reach the checkpoint: %+v", record)
	}
}

func TestC2SessionStoresWaitForWindowsReaders(t *testing.T) {
	writers := map[string]func(*testing.T, string) func(string) error{
		"fixes": func(t *testing.T, path string) func(string) error {
			return func(words string) error {
				if !writeFixDocument(path, fixDocument{Type: "fixes", Version: fixesFileVersion,
					Entries: []*fixEntry{{Tool: "bash", Error: "broken", Fix: words}}}) {
					return fmt.Errorf("fix was not saved")
				}
				return nil
			}
		},
		"state": func(t *testing.T, path string) func(string) error {
			s := newStateStore(path)
			return func(words string) error {
				_, err := s.track(words, kindProgress, statusOpen, "fixture tool result")
				return err
			}
		},
		"card": func(t *testing.T, path string) func(string) error {
			s := newCardStore(path)
			return func(words string) error { s.merge(reflex.StateDelta{Goal: words}); return nil }
		},
		"memory": func(t *testing.T, path string) func(string) error {
			return func(words string) error {
				return writeMemoryLedger(path, []store.LedgerMemoryEvent{{ID: words}})
			}
		},
		"team": func(t *testing.T, path string) func(string) error {
			return func(words string) error { return writeTeamFile(path, []byte(words)) }
		},
		"presence-request": func(t *testing.T, path string) func(string) error {
			return func(words string) error { return writePresenceRequest(path, []byte(words)) }
		},
		"task-beat": func(t *testing.T, path string) func(string) error {
			return func(words string) error { newTaskBeat(path, 1, words, time.Now()).write(); return nil }
		},
		"task-cleanup": func(t *testing.T, path string) func(string) error {
			return func(words string) error { return saveTaskCleanup(path, words, nil, nil) }
		},
	}
	for name, factory := range writers {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".json")
			if name == "task-cleanup" {
				path = filepath.Join(filepath.Dir(path), taskCleanupFile)
			}
			write := factory(t, path)
			if err := write("before"); err != nil {
				t.Fatal(err)
			}
			whileWindowsReaderHolds(t, path, func() {
				if err := write("after"); err != nil {
					t.Fatal(err)
				}
			})
			raw, err := os.ReadFile(path)
			if err != nil || !bytes.Contains(raw, []byte("after")) {
				t.Fatalf("new state did not survive the reader: %s (%v)", raw, err)
			}
		})
	}
}
