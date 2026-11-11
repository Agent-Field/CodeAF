package service

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

// portSpec is the web dev server on port, the command saying the port the way
// an agent's fix of a port conflict changes it.
func portSpec(dir string, port int) Spec {
	return Spec{Name: "web", Command: "npm run dev -- --port " + strconv.Itoa(port), Dir: dir, Health: Health{Port: port}}
}

// conflicted is a service kept onto a port another program holds: failed, its
// episode open, nothing started.
func conflicted(t *testing.T, h *harness) Row {
	t.Helper()
	h.rt.portsUsed[5173] = true
	row, err := h.s.Keep("agent-a", portSpec(h.dir, 5173), PolicyOnFailure, Provenance{Label: "Start the web dev server"})
	if err != nil {
		t.Fatal(err)
	}
	if row.State != StateFailed || !row.PortConflict || !row.EpisodeOpen || row.Episode != row.ID+".1" {
		t.Fatalf("a port conflict is a failed row with its episode open: %+v", row)
	}
	return row
}

// TestARepairRunsTheChangedCommandOnTheSameRow is the whole road: a port
// conflict, the agent's changed command approved, ONE row under the original
// id, started once, healthy, and the one episode the person was told about
// closing on that same id when — and only when — the repaired service passes
// its check.
func TestARepairRunsTheChangedCommandOnTheSameRow(t *testing.T) {
	h := newHarness(t)
	var mu sync.Mutex
	var events []Event
	h.s.Subscribe(func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() })
	failed := conflicted(t, h)
	seq := h.s.seq

	fixed := portSpec(h.dir, 5174)
	if _, ok := h.s.Approved(fixed); ok {
		t.Fatal("the changed command was approved before anybody said yes to it")
	}
	if _, err := h.s.Repairable("agent-a", failed.ID, fixed); err != nil {
		t.Fatal(err)
	}
	row, err := h.s.Repair("agent-a", failed.ID, fixed, PolicyAlways, Provenance{Label: "use another port", CallID: "c2"})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != failed.ID || !row.Running || row.PortConflict || row.Command != fixed.Command || row.Health.Port != 5174 ||
		row.Policy != PolicyAlways || row.From.CallID != "c2" || row.LogPath != failed.LogPath {
		t.Fatalf("the repair is the same row running the new command: %+v", row)
	}
	if !row.EpisodeOpen || row.Episode != failed.Episode {
		t.Fatalf("the episode closes when it is healthy, not when it is tried: %+v", row)
	}
	if h.rt.starts != 1 || h.s.seq != seq || len(h.s.List("agent-a")) != 1 {
		t.Fatalf("one start, no new id, one row: starts %d seq %d→%d rows %d", h.rt.starts, seq, h.s.seq, len(h.s.List("agent-a")))
	}
	if policy, ok := h.s.Approved(fixed); !ok || policy != PolicyAlways {
		t.Fatal("the yes to the changed command was not banked")
	}

	h.advance(CheckEvery)
	got := h.get(failed.ID)
	if got.State != StateHealthy || got.EpisodeOpen || got.Episode != failed.Episode || got.EpisodeResolvedAt == nil || got.EpisodeReason != failed.EpisodeReason {
		t.Fatalf("the original episode closes on the repaired service's first pass: %+v", got)
	}
	h.s.Close()
	mu.Lock()
	defer mu.Unlock()
	for _, e := range events {
		if e.Row.ID != failed.ID || e.Removed || (e.Row.Episode != "" && e.Row.Episode != failed.Episode) {
			t.Fatalf("every event is the one row and its one episode: %+v", e)
		}
	}
}

// TestARepairIsRefusedBeforeAnythingHappens: a foreign id, an unknown id, a
// live service, one still stopping and a duplicate are each refused by the
// side-effect-free question and by the repair itself, with nothing launched,
// nothing signalled, no yes banked and no row changed.
func TestARepairIsRefusedBeforeAnythingHappens(t *testing.T) {
	h := newHarness(t)
	failed := conflicted(t, h)
	h.rt.portsUsed[5173] = false
	live := h.keep("live", PolicyOnFailure)
	other, err := h.s.Keep("agent-b", portSpec(h.dir, 6000), PolicyOnFailure, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	starts := h.rt.starts
	cases := []struct {
		name, owner, id string
		spec            Spec
		want            error
	}{
		{"foreign", "agent-a", other.ID, portSpec(h.dir, 5175), ErrNotFound},
		{"unknown", "agent-a", "s99", portSpec(h.dir, 5175), ErrNotFound},
		{"another owner's view", "agent-b", failed.ID, portSpec(h.dir, 5175), ErrNotFound},
		{"live", "agent-a", live.ID, portSpec(h.dir, 5175), ErrRunning},
		{"duplicate", "agent-a", failed.ID, webSpec("live", h.dir), ErrDuplicate},
	}
	for _, c := range cases {
		if _, err := h.s.Repairable(c.owner, c.id, c.spec); !errors.Is(err, c.want) {
			t.Errorf("%s: Repairable said %v, want %v", c.name, err, c.want)
		}
		if _, err := h.s.Repair(c.owner, c.id, c.spec, PolicyOnFailure, Provenance{}); !errors.Is(err, c.want) {
			t.Errorf("%s: Repair said %v, want %v", c.name, err, c.want)
		}
		if _, ok := h.s.Approved(c.spec); ok && c.want != ErrDuplicate {
			t.Errorf("%s: a refused repair banked a yes", c.name)
		}
	}
	settle(t, h.s)
	if h.rt.starts != starts {
		t.Fatal("a refused repair started something")
	}
	if p := h.rt.proc(live.PID); !p.alive || len(p.signals) != 0 {
		t.Fatalf("a refused repair signalled the live service: %+v", p.signals)
	}
	if got := h.get(failed.ID); got.Command != failed.Command || got.State != StateFailed {
		t.Fatalf("a refused repair changed the row: %+v", got)
	}

	h.rt.proc(live.PID).ignoreTerm = true
	if _, err := h.s.Stop("agent-a", live.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Repair("agent-a", live.ID, portSpec(h.dir, 5176), PolicyOnFailure, Provenance{}); !errors.Is(err, ErrStopping) {
		t.Fatalf("a service in its stop grace was repaired: %v", err)
	}
	if signals := h.rt.proc(live.PID).signals; len(signals) != 1 || signals[0] {
		t.Fatalf("a refused repair sent signals of its own: %+v", signals)
	}
	// The grace runs out on the test's clock, so the stop finishes before
	// Close waits for it.
	for deadline := time.Now().Add(5 * time.Second); h.get(live.ID).Stopping && time.Now().Before(deadline); {
		h.clock.fireDue(time.Second)
		time.Sleep(time.Millisecond)
	}
}

// TestARepairTheFileCannotKeepPutsEverythingBack: the row, its spec, and the
// banked yeses are as they were, and nothing ran.
func TestARepairTheFileCannotKeepPutsEverythingBack(t *testing.T) {
	h := newHarness(t)
	failed := conflicted(t, h)
	h.rt.portsUsed[5173] = false
	h.setWrite(func(string, []byte) error { return errors.New("disk full") })
	fixed := portSpec(h.dir, 5174)
	if _, err := h.s.Repair("agent-a", failed.ID, fixed, PolicyAlways, Provenance{}); !errors.Is(err, ErrNotSaved) {
		t.Fatalf("an unsaved repair answered %v", err)
	}
	settle(t, h.s)
	got := h.get(failed.ID)
	if got.Command != failed.Command || got.Health != failed.Health || got.State != StateFailed || got.Policy != failed.Policy || !got.EpisodeOpen {
		t.Fatalf("the row was not put back: %+v", got)
	}
	if _, ok := h.s.Approved(fixed); ok {
		t.Fatal("an unsaved repair left its yes banked")
	}
	if _, ok := h.s.Approved(portSpec(h.dir, 5173)); !ok {
		t.Fatal("an unsaved repair lost the original yes")
	}
	if h.rt.launches != 0 || h.rt.starts != 0 {
		t.Fatal("an unsaved repair launched something")
	}
}

// TestOnlyAChangedSpecStartsWithAWholeBudget: the restarts spent on the old
// command are not the new command's, and a repair to the very same spec is no
// way to refill them.
func TestOnlyAChangedSpecStartsWithAWholeBudget(t *testing.T) {
	h := newHarness(t)
	failed := conflicted(t, h)
	h.rt.portsUsed[5173] = false
	spend := func() {
		h.s.mu.Lock()
		now := h.clock.Now()
		h.s.entries[failed.ID].rec.Restarts = []time.Time{now, now, now}
		h.s.entries[failed.ID].rec.State = StateGaveUp
		h.s.mu.Unlock()
	}
	spend()
	same := portSpec(h.dir, 5173)
	same.Name = "renamed"
	row, err := h.s.Repair("agent-a", failed.ID, same, PolicyOnFailure, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if row.Restarts != RestartLimit || row.Name != "renamed" {
		t.Fatalf("a repair to the same spec refilled the budget: %+v", row)
	}
	if _, err := h.s.Stop("agent-a", failed.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, h.s)
	spend()
	row, err = h.s.Repair("agent-a", failed.ID, portSpec(h.dir, 5174), PolicyOnFailure, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if row.Restarts != 0 || !row.Running {
		t.Fatalf("a changed spec did not start with its whole budget: %+v", row)
	}
}

// TestARepairKeepsThePersonsPolicyForAnAlreadyApprovedSpec, as Keep does, and
// the old command's yes is never the new command's.
func TestARepairKeepsThePersonsPolicyForAnAlreadyApprovedSpec(t *testing.T) {
	h := newHarness(t)
	fixed := portSpec(h.dir, 5174)
	if _, err := h.s.Keep("agent-b", fixed, PolicyNever, Provenance{}); err != nil {
		t.Fatal(err)
	}
	failed := conflicted(t, h)
	row, err := h.s.Repair("agent-a", failed.ID, fixed, PolicyAlways, Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if row.Policy != PolicyNever {
		t.Fatalf("the person's policy for this exact spec did not stand: %s", row.Policy)
	}
}

// TestARepairedServiceIsTakenBackAsRepaired: the file a repair wrote names the
// new spec under the old id, and the next codeaf finds it.
func TestARepairedServiceIsTakenBackAsRepaired(t *testing.T) {
	h := newHarness(t)
	failed := conflicted(t, h)
	if _, err := h.s.Repair("agent-a", failed.ID, portSpec(h.dir, 5174), PolicyOnFailure, Provenance{}); err != nil {
		t.Fatal(err)
	}
	h.reopen()
	rows := h.s.List("agent-a")
	if len(rows) != 1 || rows[0].ID != failed.ID || !rows[0].Running || rows[0].Health.Port != 5174 || !rows[0].ReAdopted {
		t.Fatalf("%+v", rows)
	}
	if next := h.keep("other", PolicyOnFailure); next.ID != "s2" {
		t.Fatalf("the repair moved the id counter: %s", next.ID)
	}
}
