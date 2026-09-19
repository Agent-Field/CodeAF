package standing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Organize rides the same elected pass as tidy. What this file holds shut is
// the pass's side: it is called once, a nil is silent, and a failure is one
// pass's problem. It does not change Interval.

func TestThePassRunsOrganizeAfterTidy(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	ticker := newTicker(store, &fakeRunner{}, now)
	order := make([]string, 0, 2)
	ticker.Tidy = func(context.Context) (Tidied, error) {
		order = append(order, "tidy")
		return Tidied{}, nil
	}
	ticker.Organize = func(context.Context) error {
		order = append(order, "organize")
		return nil
	}

	mustTick(t, ticker)
	if len(order) != 2 || order[0] != "tidy" || order[1] != "organize" {
		t.Fatalf("pass order %v, want tidy then organize", order)
	}
}

func TestAPassWithNothingToOrganizeWithSaysNothing(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	pass := mustTick(t, newTicker(store, &fakeRunner{}, now))
	if pass.Errors != 0 || len(pass.Notes) != 0 {
		t.Fatalf("the pass is %+v", pass)
	}
}

func TestAFailedOrganizeIsCountedAndNoted(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	ticker := newTicker(store, &fakeRunner{}, now)
	ticker.Organize = func(context.Context) error {
		return errors.New("collections would not open")
	}
	pass := mustTick(t, ticker)
	if pass.Errors != 1 || len(pass.Notes) != 1 || !strings.Contains(pass.Notes[0], "collections would not open") {
		t.Fatalf("the pass is %+v", pass)
	}
}
