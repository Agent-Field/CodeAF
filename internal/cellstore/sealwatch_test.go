package cellstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestSealWatchFailureIsVisibleAndSaidOnce(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Report(errors.New("disk full"))
	if !w.Failing() {
		t.Fatal("a failed seal must leave the watch failing")
	}
	first := w.Take()
	if !strings.Contains(first, "not being sealed") || !strings.Contains(first, "disk full") {
		t.Fatalf("first sentence %q", first)
	}
	if again := w.Take(); again != "" {
		t.Fatalf("the same failure said again: %q", again)
	}
	if !w.Failing() {
		t.Fatal("saying the sentence must not clear the state")
	}
}

func TestSealWatchRecoveryIsSaidAndClearsTheState(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Take()
	w.Report(nil)
	if w.Failing() {
		t.Fatal("a seal that held must clear the state")
	}
	if got := w.Take(); !strings.Contains(got, "works again") {
		t.Fatalf("recovery sentence %q", got)
	}
	w.Report(nil)
	if got := w.Take(); got != "" {
		t.Fatalf("a healthy seal after a healthy seal said %q", got)
	}
}

func TestSealWatchCauseChangeIsSaidAgain(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Take()
	w.Report(errors.New("engine not found"))
	if got := w.Take(); !strings.Contains(got, "different reason") || !strings.Contains(got, "engine not found") {
		t.Fatalf("cause change sentence %q", got)
	}
}

func TestSealWatchFailingAgainAfterRecoveryIsANewFailure(t *testing.T) {
	var w SealWatch
	w.Report(errors.New("disk full"))
	w.Report(nil)
	w.Take()
	w.Take()
	w.Report(errors.New("disk full"))
	if got := w.Take(); !strings.Contains(got, "not being sealed") {
		t.Fatalf("second failure sentence %q", got)
	}
}

// The recorder is what feeds the watch: a seal that fails leaves it failing,
// and the next seal that holds clears it.
func TestRecorderFeedsTheSealWatch(t *testing.T) {
	c := newCell(t)
	fail := true
	e := (&fakeEngine{}).engine(t)
	good := e.Run
	e.Run = func(ctx context.Context, d string, env []string, argv ...string) ([]byte, error) {
		if fail && len(argv) > 2 && argv[2] == "hook" {
			return nil, errors.New("disk full")
		}
		return good(ctx, d, env, argv...)
	}
	var w SealWatch
	r, err := NewRecorder(&stubExec{}, e, c, filepath.Join(t.TempDir(), "wal"), Options{Report: w.Report})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "one"), nil); err != nil {
		t.Fatal(err)
	}
	if !w.Failing() {
		t.Fatal("a failed seal must show")
	}
	fail = false
	if _, err := r.Exec(context.Background(), req(executor.NetPolicy{}, "two"), nil); err != nil {
		t.Fatal(err)
	}
	if w.Failing() {
		t.Fatal("a seal that held must clear it")
	}
}

// A turn ending reaches whoever the door named, and costs nothing when nobody
// listens.
func TestSealWatchForwardsTurnEnd(t *testing.T) {
	var w SealWatch
	w.TurnEnded() // no listener: nothing happens
	heard := 0
	w.OnTurnEnd = func() { heard++ }
	w.TurnEnded()
	w.TurnEnded()
	if heard != 2 {
		t.Fatalf("heard %d turn ends, want 2", heard)
	}
}
