package lane

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── ONE BEAT PER SHEET, HOWEVER MANY DOORS ASK FOR ONE ──────────────────────
//
// Every door of this build now asks for a beat — the headless run as well as
// the session — and a process that opens two of them must still fetch each
// model's sheet once. Two fetch loops over one sheet would pay the router twice
// for the same half-hour aggregate and tick on two clocks the cache disagrees
// with, which is the drift [Beat] exists to prevent.

// TestASecondBeatJoinsTheFirstRatherThanFetchingAgain is that law, measured on
// the wire the only way it can be: by counting what the router was asked.
func TestASecondBeatJoinsTheFirstRatherThanFetchingAgain(t *testing.T) {
	// A HOME OF ITS OWN. The beat primes the default ledger from every reading,
	// and that ledger writes through a store rooted at AFORGE_HOME.
	t.Setenv(home.EnvVar, t.TempDir())
	Default().Reset()
	t.Cleanup(Default().Reset)

	const other = "qwen/qwen3.5-9b"
	s, stub, _ := wired(t)
	stub.Model(other, scripted()...)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	first := make(chan struct{})
	go func() {
		Beat(ctx, s, []string{scriptedModel}, time.Hour)
		close(first)
	}()
	waitFor(t, func() bool { return stub.Sheets(scriptedModel) == 1 })

	// THE SECOND DOOR'S BEAT RETURNS AT ONCE. It hands its models to the beat
	// that is already running and runs no loop of its own, which is what lets a
	// headless door ask for a beat without taking one away from a session.
	second := make(chan struct{})
	go func() {
		Beat(ctx, s, []string{scriptedModel, other}, time.Hour)
		close(second)
	}()
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("a second beat on one sheet ran a fetch loop of its own")
	}

	// The model only the second door knew about is fetched, because it joined
	// the round the first door is running.
	waitFor(t, func() bool { return stub.Sheets(other) == 1 })
	if got := stub.Sheets(scriptedModel); got != 1 {
		t.Fatalf("the router was asked for one model's sheet %d times, want 1", got)
	}

	stop()
	<-first

	// AND THE CLAIM IS GIVEN BACK WHEN THE BEAT ENDS. A process that stopped its
	// beat and started another — a session closing while the run goes on — must
	// get a beat and not silence.
	after, stopAfter := context.WithCancel(context.Background())
	defer stopAfter()
	third := make(chan struct{})
	go func() {
		Beat(after, s, []string{other}, time.Millisecond)
		close(third)
	}()
	waitFor(t, func() bool { return stub.Sheets(other) > 1 })
	stopAfter()
	<-third
}
