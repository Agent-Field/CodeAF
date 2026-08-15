package settings

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// clock is an injected now(). Every timing assertion in this file moves it by
// hand, so the debounce is proved to the millisecond without a sleep anywhere
// in the suite.
type clock struct{ at time.Time }

func (c *clock) now() time.Time       { return c.at }
func (c *clock) tick(d time.Duration) { c.at = c.at.Add(d) }

func newTimedSheet(t *testing.T, debounce time.Duration) (*sheet, *clock) {
	t.Helper()
	c := &clock{at: time.Unix(1_700_000_000, 0)}
	s := newSheet(t, func(o *Options) {
		o.Debounce = debounce
		o.Now = c.now
	})
	return s, c
}

// The debounce is real, and this is the test that says so: eight toggles
// inside the window are ONE write of ONE file, and nothing at all is written
// while the window is open.
func TestRapidEditsCoalesceIntoASingleWrite(t *testing.T) {
	s, c := newTimedSheet(t, 300*time.Millisecond)
	s.gotoRow(t, config.KeyAttribution)

	for range 8 {
		s.press(typeRune(' '))
		c.tick(20 * time.Millisecond)
		s.settle()
	}
	if len(s.applied) != 0 {
		t.Fatalf("%d writes landed inside the debounce window: %v", len(s.applied), s.applied)
	}
	if _, err := os.Stat(config.BudgetConfigPath(s.dir)); err == nil {
		t.Fatal("the profile file was written before the window closed")
	}

	c.tick(300 * time.Millisecond)
	s.settle()

	if len(s.applied) != 1 {
		t.Fatalf("writes = %v, want exactly one", s.applied)
	}
	if s.applied[0] != config.KeyAttribution {
		t.Fatalf("wrote %q", s.applied[0])
	}
}

// Trailing, not leading: each edit moves the deadline, so a burst that keeps
// arriving keeps waiting rather than writing at a fixed rate.
func TestEachEditMovesTheDeadline(t *testing.T) {
	s, c := newTimedSheet(t, 300*time.Millisecond)
	s.gotoRow(t, config.KeyAttribution)

	s.press(typeRune(' '))
	c.tick(250 * time.Millisecond)
	s.settle()
	if len(s.applied) != 0 {
		t.Fatal("wrote before the window closed")
	}

	s.press(typeRune(' ')) // the deadline moves
	c.tick(250 * time.Millisecond)
	s.settle()
	if len(s.applied) != 0 {
		t.Fatalf("wrote %v at 500ms although the second edit moved the deadline", s.applied)
	}

	c.tick(60 * time.Millisecond)
	s.settle()
	if len(s.applied) != 1 {
		t.Fatalf("writes = %v, want one after the window finally closed", s.applied)
	}
}

// The durability half of a trailing debounce: leaving flushes. Nothing a user
// typed is lost by closing the sheet — only delayed by staying in it.
func TestClosingFlushesWhatIsStillPending(t *testing.T) {
	s, _ := newTimedSheet(t, time.Hour)
	s.gotoRow(t, config.KeyAttribution)
	s.press(typeRune(' '))

	if len(s.applied) != 0 {
		t.Fatal("the value was written before the window closed")
	}
	s.press(namedKey(tea.KeyEscape))

	if len(s.applied) != 1 {
		t.Fatalf("writes = %v, want the pending edit flushed on close", s.applied)
	}
	if config.AttributionAt(s.dir) {
		t.Fatal("the store did not take the flushed value")
	}
}

// Two rows edited in one burst land in one pass over the registry, and neither
// loses the other: config.json carries both, plus whatever was already in it.
func TestOneFlushCarriesEveryPendingRowAndPreservesTheRest(t *testing.T) {
	s, c := newTimedSheet(t, 100*time.Millisecond)

	s.gotoRow(t, config.KeyDailyBudget)
	s.press(namedKey(tea.KeyEnter))
	s.press(ctrlKey('u'))
	s.typeText("42")
	s.press(namedKey(tea.KeyEnter))

	s.gotoRow(t, config.KeyAttribution)
	s.press(typeRune(' '))

	c.tick(time.Second)
	s.settle()

	raw, err := os.ReadFile(config.BudgetConfigPath(s.dir))
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("profile is not json: %v", err)
	}
	for _, key := range []string{config.KeyDailyBudget, config.KeyAttribution} {
		if _, ok := values[key]; !ok {
			t.Fatalf("%s is missing from %s", key, string(raw))
		}
	}

	// A second registry over the same directory is the round trip that matters:
	// what this surface wrote is what the next process reads.
	fresh := config.NewSettings(config.SettingsOptions{ProfileDir: s.dir})
	row, ok := fresh.Row(config.KeyDailyBudget)
	if !ok || row.Value() != "$42" {
		t.Fatalf("a fresh registry reads %q, want $42", row.Value())
	}
}

// A host that routes messages gets a punctual write; a stale tick, left over
// from a deadline a later edit moved, writes nothing.
func TestRoutedTickWritesAndAStaleOneIsIgnored(t *testing.T) {
	s, c := newTimedSheet(t, 100*time.Millisecond)
	s.gotoRow(t, config.KeyAttribution)

	cmd := s.Key(typeRune(' '))
	if cmd == nil {
		t.Fatal("staging an edit must arm a tick the host can route")
	}
	stale := cmd()
	if _, ok := stale.(flushMsg); !ok {
		t.Fatalf("armed command produced %#v, want a flush tick", stale)
	}

	s.press(typeRune(' ')) // moves the deadline and the epoch
	c.tick(time.Second)

	s.Update(stale)
	if len(s.applied) != 0 {
		t.Fatalf("a stale tick wrote %v", s.applied)
	}

	s.Update(flushMsg{epoch: s.epoch})
	if len(s.applied) != 1 {
		t.Fatalf("writes = %v, want the current tick to land one", s.applied)
	}
}

// A negative debounce writes through — the shape a caller asks for when it
// wants no window at all.
func TestNegativeDebounceWritesImmediately(t *testing.T) {
	s := newSheet(t, func(o *Options) { o.Debounce = -1 })
	s.gotoRow(t, config.KeyAttribution)
	s.press(typeRune(' '))

	if len(s.applied) != 1 {
		t.Fatalf("writes = %v, want one straight through", s.applied)
	}
}
