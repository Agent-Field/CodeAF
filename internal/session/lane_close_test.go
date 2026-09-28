package session

import (
	"context"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// A cancelled fetch can still be finishing a cache write. Close must join the
// beat before the caller can remove or switch the session's state directory.
type closingBeatSheet struct {
	*beatSheet
	started   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	finished  chan struct{}
}

func (s *closingBeatSheet) Refresh(ctx context.Context, _ string) error {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
	close(s.finished)
	return ctx.Err()
}

func TestCloseJoinsTheSessionLaneBeat(t *testing.T) {
	sheet := &closingBeatSheet{beatSheet: &beatSheet{}, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	lanes.Default().SetSheet(sheet)
	t.Cleanup(lanes.Default().Reset)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(sheet.release) }) })
	agent, err := newAgent(Config{Workspace: t.TempDir(), Model: "talk/model", BaseURL: "https://openrouter.ai/api/v1"}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release.Do(func() { close(sheet.release) }); _ = agent.Close() })
	select {
	case <-sheet.started:
	case <-time.After(2 * time.Second):
		t.Fatal("beat did not start")
	}
	closed := make(chan struct{})
	go func() { _ = agent.Close(); close(closed) }()
	select {
	case <-sheet.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the beat")
	}
	select {
	case <-closed:
		t.Fatal("Close returned while the cancelled beat could still write")
	case <-time.After(50 * time.Millisecond):
	}
	release.Do(func() { close(sheet.release) })
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after the beat returned")
	}
	select {
	case <-sheet.finished:
	default:
		t.Fatal("Close left the beat unfinished")
	}
}
