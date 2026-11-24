package automation

import (
	"testing"
	"time"
)

// AN ORGANIZATION'S STANDING ORDER NEVER RUNS ON A PERSON'S COMPUTER (E4 F0).
// Its due slot passes with no run at all, and a run somebody queued by hand
// ("run it now") ends at once, before it asks and before any work, saying why.
// A Personal order beside it still runs.
func TestAnOrganizationsOrderNeverRunsOnThisComputer(t *testing.T) {
	runner := &fakeRunner{report: Report{Line: "drafted", USD: 0.01}}
	r := startClock(t, runner)

	org := routine("1h")
	org.Workstream = "org:conn-1/ws_pantry"
	org.Trust = TrustAsksFirst
	made, err := r.store.Create(org)
	if err != nil {
		t.Fatal(err)
	}
	if !made.OrgWorkstream() {
		t.Fatal("an org: workstream is not read as an organization's")
	}
	// Its slot comes due: the clock passes it over without a run.
	if err := r.store.advance(made, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := r.store.Get(made.ID)
		if got.Next.After(time.Now()) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if runs, _ := r.store.Runs(made.ID, 10); len(runs) != 0 {
		t.Fatalf("a due organization's order made runs here: %+v", runs)
	}

	// Run it now: the run ends stopped, saying why, and nothing worked.
	r.now(t, made.ID)
	runs := r.settle(t, made.ID, 1)
	if runs[0].Outcome != OutcomeStopped || runs[0].Line != LineOrgWorkstream {
		t.Fatalf("a queued run of an organization's order = %+v", runs[0])
	}
	if asking, _ := r.store.Asking(); len(asking) != 0 {
		t.Fatalf("it asked somebody to say go: %+v", asking)
	}
	if n := runner.worked.Load(); n != 0 {
		t.Fatalf("an organization's order did work on this computer %d time(s)", n)
	}

	mine := routine("1h")
	mine.Workstream = "ws_personal"
	kept, err := r.store.Create(mine)
	if err != nil {
		t.Fatal(err)
	}
	r.now(t, kept.ID)
	if got := r.settle(t, kept.ID, 1); got[0].Outcome == OutcomeStopped {
		t.Fatalf("a Personal order was refused: %+v", got[0])
	}
}
