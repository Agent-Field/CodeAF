package session

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A chat the app's quit stopped mid-turn is opened HELD: its interrupted task
// run is not resumed, so nothing spends, until the person's Continue — and
// Continue resumes it exactly once (resume_quit.go).
func TestAHeldOpenResumesNothingUntilContinueAndThenOnce(t *testing.T) {
	for _, door := range []string{"continue", "resume after quit"} {
		t.Run(door, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			engine := newBeltRunDouble("done")
			registerBeltRunEngine(t, engine)
			place, repo := t.TempDir(), newTestRepo(t)
			open := func(hold bool) *Agent {
				a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
					c.Workspace, c.Place, c.SessionFile = repo, Place{Dir: place}, filepath.Join(place, placeTranscript)
					c.AskConsent = false
					c.HoldInterruptedWork = hold
				})
				return a
			}
			first := open(false)
			g := first.graph()
			id := g.reserve()
			tree, err := prepareTaskTree(first.config.Place, repo, "1111bbbb1111bbbb", id, "keep work")
			if err != nil {
				t.Fatal(err)
			}
			store, err := plandb.Open(g.planPath(), "task", strconv.FormatUint(id, 10), "task", "original brief")
			if err != nil {
				t.Fatal(err)
			}
			_ = store.Close()
			first.publishRunRow(g, TaskNotice{ID: id, Title: "task", State: TaskRunning, Copy: runCopyOf(tree), CrewState: first.unroutedCrewRecord(), PlanTask: planStoreID(strconv.FormatUint(id, 10))})
			_ = first.Close()

			second := open(true)
			second.beltMu.Lock()
			started := second.beltRun != nil
			second.beltMu.Unlock()
			if started || engine.didRun() {
				t.Fatal("a held open resumed the interrupted run")
			}
			if !second.HeldInterruptedWork() {
				t.Fatal("the held open does not say it is holding work")
			}
			if row, found := runRowOf(second.graph(), id); !found || row.State != TaskInterrupted {
				t.Fatalf("held row = %+v", row)
			}
			switch door {
			case "continue":
				if !second.ContinueInterruptedWork() {
					t.Fatal("Continue found nothing held")
				}
			case "resume after quit":
				second.ResumeAfterQuit()
			}
			select {
			case <-engine.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("Continue did not resume the run")
			}
			second.beltMu.Lock()
			resumed := second.beltRun
			second.beltMu.Unlock()
			if second.ContinueInterruptedWork() || second.HeldInterruptedWork() {
				t.Fatal("a second Continue carried on again")
			}
			second.beltMu.Lock()
			same := second.beltRun == resumed
			second.beltMu.Unlock()
			if !same {
				t.Fatal("a second Continue made a second driver")
			}
			endBeltRun(t, second, engine)
		})
	}
}

// An open that is not held recovers as it always has, and holds nothing for a
// Continue to release.
func TestAnOrdinaryOpenHoldsNothing(t *testing.T) {
	place := t.TempDir()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place, c.SessionFile = Place{Dir: place}, filepath.Join(place, placeTranscript)
	})
	if a.HeldInterruptedWork() || a.ContinueInterruptedWork() {
		t.Fatal("an ordinary open held work")
	}
}
