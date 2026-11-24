package cellstore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// viewerStore is a store whose window can be switched to "only shows the chat",
// the way a drive side is when another computer takes the chat.
type viewerStore struct {
	mu      sync.Mutex
	seal    error
	prepare error
	viewing bool
}

func (s *viewerStore) Seal(context.Context, cell.Cell, TurnInfo) (Sealed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Sealed{}, s.seal
}

func (s *viewerStore) Prepare(context.Context, cell.Cell) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prepare
}

func (s *viewerStore) Viewing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewing
}

// another computer takes the chat: from now on every seal is refused as read-only.
func (s *viewerStore) taken() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewing, s.seal, s.prepare = true, ErrReadOnly, nil
}

// warmedAndFailed is a recorder whose first capture failed while this window
// still drove the chat, with nothing pending: only the failure is left behind.
func warmedAndFailed(t *testing.T, fault error) (*Recorder, cell.Cell, *viewerStore) {
	t.Helper()
	s := &viewerStore{prepare: fault}
	r, c, _ := pauseRecorder(t, s, Options{Async: true})
	r.Warm(t.Context())
	waitPauseCapture(t, r)
	// The failure is real while this window drives the chat.
	if err := Captured(t.Context(), c); !errors.Is(err, fault) {
		t.Fatalf("precondition: the failed warm capture was not reported while driving: %v", err)
	}
	return r, c, s
}

// C3.1: a window that only shows a chat, with nothing of its own unsaved,
// quits without a failure, whatever an earlier attempt left behind.
func TestAViewingWindowWithNothingOfItsOwnQuitsWithoutAFailure(t *testing.T) {
	fault := errors.New("the first capture could not screen the folder")
	r, c, s := warmedAndFailed(t, fault)
	s.taken()
	if err := r.Drain(t.Context()); err != nil {
		t.Fatalf("a window that only shows the chat reported %v at quit", err)
	}
	if err := Await(t.Context(), c); err != nil {
		t.Fatalf("the quit boundary kept the failure of a window that only shows the chat: %v", err)
	}
}

// C3.3: the same window with a call of its own that was never sealed still
// reports the failure: its turns are real and are kept.
func TestAViewingWindowWithACallOfItsOwnKeepsItsFailure(t *testing.T) {
	fault := errors.New("capture refused")
	s := &viewerStore{seal: fault}
	r, _, _ := pauseRecorder(t, s, Options{Async: true})
	pauseCall(t, r, executor.Call{Tool: "bash", Deferred: true}, AgentRun)
	r.Settle(t.Context())
	waitPauseCapture(t, r)
	s.taken() // the chat is taken before the call could be sealed
	if err := r.Drain(t.Context()); !errors.Is(err, fault) {
		t.Fatalf("a window with an unsaved call of its own reported %v, want %v", err, fault)
	}
}

// C3.3: lines this window wrote to the transcript that no seal covers are its
// own too, so the failure stays.
func TestAViewingWindowWithTranscriptLinesOfItsOwnKeepsItsFailure(t *testing.T) {
	fault := errors.New("the first capture could not screen the folder")
	r, c, s := warmedAndFailed(t, fault)
	s.taken()
	pauseResult(t, c, `{"type":"message","role":"assistant","content":"written here, never sealed"}`)
	if err := r.Drain(t.Context()); !errors.Is(err, fault) {
		t.Fatalf("a window with transcript lines of its own reported %v, want %v", err, fault)
	}
}

// C3.3: a window that still drives the chat gets no such mercy: its failure is
// reported whether or not anything is pending.
func TestADrivingWindowKeepsItsFailure(t *testing.T) {
	fault := errors.New("the first capture could not screen the folder")
	r, _, _ := warmedAndFailed(t, fault)
	if err := r.Drain(t.Context()); !errors.Is(err, fault) {
		t.Fatalf("a window that drives the chat reported %v, want %v", err, fault)
	}
}
