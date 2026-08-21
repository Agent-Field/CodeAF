package standing

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The tidy rides the pass, so what this file holds shut is the pass's side of
// the bargain: it is called, its money lands on the day's rail, it is refused
// once the day is spent, and a build without one is silent.

// THE PASS CALLS IT, AND WHAT IT SPENT IS ON THE DAY'S RAIL. A tidy that was
// billed and not written down is a rail quoting a figure that is too small.
func TestThePassTidiesAndPutsWhatItSpentOnTheRail(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	ticker := newTicker(store, &fakeRunner{}, now)
	called := 0
	ticker.Tidy = func(context.Context) (Tidied, error) {
		called++
		return Tidied{Merged: 2, Superseded: 1, USD: 0.002}, nil
	}

	pass := mustTick(t, ticker)
	if called != 1 {
		t.Fatalf("the tidy was called %d times, want once", called)
	}
	if pass.Tidied != 3 {
		t.Fatalf("the pass says %d lines moved, want 3", pass.Tidied)
	}
	if len(pass.Notes) != 1 || pass.Notes[0] != "consolidated · 2 merged · 1 superseded · $0.002" {
		t.Fatalf("the pass noted %v", pass.Notes)
	}
	spend, err := store.Today("", now)
	if err != nil {
		t.Fatal(err)
	}
	if spend.USD != 0.002 {
		t.Fatalf("the day's rail sees %v, want the tidy's own price", spend.USD)
	}
	// AND IT IS NOT A FIRING. Nobody armed it, so it belongs on the money and
	// on nothing's run count.
	if spend.Fired != 0 {
		t.Fatalf("the tidy counted as %d firings", spend.Fired)
	}
	// The wake log is the one proof the machine was awake, and it now carries
	// this too.
	raw, err := os.ReadFile(store.WakeLogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "tidied=3") {
		t.Fatalf("the wake log reads %q", strings.TrimSpace(string(raw)))
	}
}

// THE DAY'S RAIL OUTRANKS IT. It is off-path work nobody asked for, so it is
// the first thing a spent day stops paying for.
func TestASpentDayDoesNotTidy(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if err := store.Append(Entry{At: now, Kind: entryCheck, USD: 1.0}); err != nil {
		t.Fatal(err)
	}
	ticker := newTicker(store, &fakeRunner{}, now)
	ticker.DailyRailUSD = 0.5
	called := 0
	ticker.Tidy = func(context.Context) (Tidied, error) { called++; return Tidied{}, nil }

	mustTick(t, ticker)
	if called != 0 {
		t.Fatalf("a spent day still tidied %d times", called)
	}
}

// A BUILD WITH NO TIDY IS SILENT — no note, no error, nothing in the wake log
// beyond the zero. A capability that cannot work is absent, not broken.
func TestAPassWithNothingToTidyWithSaysNothing(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	pass := mustTick(t, newTicker(store, &fakeRunner{}, now))
	if pass.Tidied != 0 || len(pass.Notes) != 0 {
		t.Fatalf("the pass is %+v", pass)
	}
}

// A TIDY THAT FAILED IS ONE PASS'S PROBLEM. It is counted and noted, exactly as
// one item's bad day is, and the pass still writes its line.
func TestAFailedTidyIsCountedAndNoted(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	ticker := newTicker(store, &fakeRunner{}, now)
	ticker.Tidy = func(context.Context) (Tidied, error) {
		return Tidied{}, errors.New("the provider said no")
	}
	pass := mustTick(t, ticker)
	if pass.Errors != 1 || len(pass.Notes) != 1 || !strings.Contains(pass.Notes[0], "the provider said no") {
		t.Fatalf("the pass is %+v", pass)
	}
}
