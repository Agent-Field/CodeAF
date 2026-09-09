package session

// A RUN HELD AT ITS FUEL GATE SAYS SO ON THE ROSTER.
//
// The gate was visible in exactly one place — the run's own page, which somebody
// has to know to open — and the row every other surface draws went on saying the
// run was working. These pin the other half: the run's OWN row carries the gate
// from the moment the tank empties until somebody answers, the workers under it
// go on saying what they are actually doing, and nothing that publishes a row
// afterwards can take the gate off by accident.

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// rootRow is the run's own row out of a drained lane, and the workers under it.
// The run's row is the one with no node name on it (TaskNotice.Node).
func rootRow(t *testing.T, notices []TaskNotice, root uint64) TaskNotice {
	t.Helper()
	var found TaskNotice
	var seen bool
	for _, notice := range notices {
		if notice.ID == root {
			found, seen = notice, true
		}
	}
	if !seen {
		t.Fatalf("the run's own row was not published at all: %+v", notices)
	}
	return found
}

// THE GATE IS THE RUN'S AND NOT ITS WORKERS'. Whatever was in flight when the
// tank emptied is still working — the run lets it finish — so a family that
// labelled every row paused would be reporting a stillness that is not happening
// on any of them.
func TestARunHeldAtItsFuelGateHoldsItsOwnRowAndNotItsWorkers(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-pricing")
	family.upsert([]orchestrate.NodeStatus{
		node("n1", "tariff table", "You are reading the tariff table.", orchestrate.Running),
		node("n2", "invoice writer", "You are reading the invoice writer.", orchestrate.Queued),
	})
	familyNotices(t, updates)

	family.pauseRun(true)
	notices := familyNotices(t, updates)
	if len(notices) != 1 {
		t.Fatalf("%d rows published for one gate, want the run's own: %+v", len(notices), notices)
	}
	root := notices[0]
	if root.ID != family.root || root.Node != "" || root.Parent != 0 {
		t.Fatalf("the gate landed on row %+v, want the run's own", root)
	}
	if !root.Paused {
		t.Fatalf("the run's row does not say it is held at its gate: %+v", root)
	}
	// IT IS STILL RUNNING, and that pair is the whole design: the run has not
	// ended, and it is not moving.
	if root.State != TaskRunning {
		t.Fatalf("the held run's row says %q, want it still running", root.State)
	}

	// AND THE WORKERS ARE UNTOUCHED. Their last rows said running and queued and
	// nothing has republished them.
	replayed := runRowsOf(familyNotices(t, agent.TaskUpdates()), "run-pricing")
	if len(replayed) != 3 {
		t.Fatalf("%d rows replayed, want the run and its two workers: %+v", len(replayed), replayed)
	}
	states := map[string]TaskState{}
	for _, row := range replayed[1:] {
		if row.Paused {
			t.Fatalf("worker %q was labelled held at a gate it is not standing at: %+v", row.Node, row)
		}
		states[row.Node] = row.State
	}
	if states["n1"] != TaskRunning || states["n2"] != TaskQueued {
		t.Fatalf("the workers' states moved under the gate: %+v", states)
	}
	// AND A LANE THAT OPENS LATE IS TOLD ABOUT THE GATE, because the replay is the
	// notice that was sent rather than a row rebuilt from somewhere else — a
	// surface that attached while the run was waiting used to be handed a root
	// that looked like work in flight.
	if !replayed[0].Paused {
		t.Fatalf("the replayed run's row lost its gate: %+v", replayed[0])
	}
}

// A LATE ROW MAY NOT TAKE THE GATE OFF. The run's own row is published by three
// things that know nothing about a fuel tank — the forming line, the namer that
// answers a second or two after the run is minted, and the mint itself — and any
// of them landing after the tank emptied would have put the run back to claiming
// it was working while a person was still being asked for money.
func TestALateRowDoesNotTakeTheGateOffARunsRow(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code that we shipped in June", "cheap/planner", "run-pricing")
	family.pauseRun(true)
	familyNotices(t, updates)

	// The namer answering late (taskname.go's rename), while the run is still
	// forming: the row is redrawn under its new name and keeps the gate.
	family.rename("pricing audit")
	named := rootRow(t, familyNotices(t, updates), family.root)
	if named.Title != "pricing audit" {
		t.Fatalf("the run was not renamed: %+v", named)
	}
	if !named.Paused {
		t.Fatalf("the name that arrived late took the gate off the row: %+v", named)
	}
	// AND THE FORMING LINE IS STILL ON IT, which is the case this catches twice
	// over: a tank can empty on the opening planner call, before there is a single
	// worker to draw.
	if named.Doing != orchestrateForming {
		t.Fatalf("the renamed row lost the forming line: %+v", named)
	}

	// The first worker arriving, which republishes the run's row to take the
	// forming line off it ([orchestrateFamily.formingDone]).
	family.upsert([]orchestrate.NodeStatus{
		node("n1", "tariff table", "You are reading the tariff table.", orchestrate.Running),
	})
	formed := rootRow(t, familyNotices(t, updates), family.root)
	if formed.Doing != "" {
		t.Fatalf("the forming line outlived the first worker: %+v", formed)
	}
	if !formed.Paused {
		t.Fatalf("a worker arriving took the gate off the run's row: %+v", formed)
	}
}

// AND THE GATE COMES DOWN WHEN IT IS ANSWERED — which is the same defect the
// other way round: a row that went on asking after the person answered.
func TestAnsweringTheGateTakesItOffTheRunsRow(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-pricing")
	family.pauseRun(true)
	familyNotices(t, updates)

	family.pauseRun(false)
	notices := familyNotices(t, updates)
	if len(notices) != 1 {
		t.Fatalf("%d rows published for one answer: %+v", len(notices), notices)
	}
	if back := notices[0]; back.ID != family.root || back.Paused || back.State != TaskRunning {
		t.Fatalf("the answered run's row is %+v, want the run's own, running and no longer held", back)
	}

	// AND IT IS SAID ONCE. A run publishes on every launch, landing, note and
	// steer; a gate that had not moved is not news, and a roster redrawn for it is
	// a row a surface has to decide to ignore.
	family.pauseRun(false)
	if again := familyNotices(t, updates); len(again) != 0 {
		t.Fatalf("a gate that did not move published %d rows: %+v", len(again), again)
	}
}

// A SETTLED RUN HAS NO GATE, and it cannot be given one. The third answer —
// "stop" — ends the run through [Agent.Cancel] while the gate is still up, so
// the settle has to take it down; and a pause arriving after the last row would
// put a live question over work that is over.
func TestASettledRunHasNoGate(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-pricing")
	family.pauseRun(true)
	familyNotices(t, updates)

	family.settle(orchestrate.Snapshot{Done: true, Stopped: true}, nil)
	closing := rootRow(t, familyNotices(t, updates), family.root)
	if closing.Paused {
		t.Fatalf("the run's last row still asks for a decision: %+v", closing)
	}
	if closing.State != TaskFailed || !closing.Stopped {
		t.Fatalf("a run stopped at its gate settled as %+v", closing)
	}

	family.pauseRun(true)
	if late := familyNotices(t, updates); len(late) != 0 {
		t.Fatalf("a settled run was put back at a gate: %+v", late)
	}
	// And nothing can read the flag back out of the family afterwards — a replay
	// and a checkpoint are both built from what it is holding.
	replayed := runRowsOf(familyNotices(t, agent.TaskUpdates()), "run-pricing")
	if len(replayed) != 1 || replayed[0].Paused {
		t.Fatalf("the settled run replays as %+v", replayed)
	}
}

// AND A RESTORED RUN IS NEVER AT A GATE. The orchestrator died with the process,
// so a row that came back asking for money would be asking on behalf of
// something no answer can reach: it comes back settled, like every other row
// that was moving when aforge closed (task_store.go's [runRecord]).
func TestARestoredRunIsNeverHeldAtAGate(t *testing.T) {
	_, rows := resumedRunRows(t, func(family *orchestrateFamily) {
		family.upsert([]orchestrate.NodeStatus{
			node("n1", "tariff table", "You are reading the tariff table.", orchestrate.Running),
		})
		family.pauseRun(true)
	})
	if len(rows) != 2 {
		t.Fatalf("%d rows redrawn, want the run and its worker: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if row.Paused {
			t.Fatalf("row %d came back asking a question nothing can answer: %+v", row.ID, row)
		}
		if !row.State.settled() {
			t.Fatalf("row %d came back %s — a restored run is history, not work", row.ID, row.State)
		}
	}
}

// ── the reading ─────────────────────────────────────────────────────────────

// THE READING SAYS WHAT THE ROW IS FOR: work that will not move until a person
// says something. Every surface takes its glyph, its group and its counts from
// this one function, which is why the gate is projected here rather than drawn
// from a second table beside it.
func TestTheReadingOfARunHeldAtItsGate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts TaskFacts
		want  TaskPresence
		on    TaskWaitOn
		ask   bool
	}{
		{
			name:  "held at the gate",
			facts: TaskFacts{State: TaskRunning, Paused: true},
			want:  TaskPresenceNeedsLook, on: TaskWaitPerson, ask: true,
		},
		{
			// The gate outranks whatever the run was last saying about itself. The
			// pacing word and the gap describe work that has since stopped moving,
			// and reading either one first would put "still going" over a question
			// nobody has answered.
			name:  "held with a stale hold and gap on it",
			facts: TaskFacts{State: TaskRunning, Paused: true, Hold: "rate limited", Gap: "adding the amp-labs section"},
			want:  TaskPresenceNeedsLook, on: TaskWaitPerson, ask: true,
		},
		{
			name:  "a worker under a held run",
			facts: TaskFacts{State: TaskRunning},
			want:  TaskPresenceWorking, on: TaskWaitNobody,
		},
		{
			// A flag left on a row that has since landed cannot contradict its
			// ending: the reading asks about a gate only of work the graph is still
			// holding open.
			name:  "a landed run carrying a stale flag",
			facts: TaskFacts{State: TaskDone, Paused: true},
			want:  TaskPresenceDone, on: TaskWaitNobody,
		},
		{
			name:  "a run somebody stopped at its gate",
			facts: TaskFacts{State: TaskFailed, Paused: true, Stopped: true},
			want:  TaskPresenceStopped, on: TaskWaitNobody,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := ProjectTask(tc.facts)
			if status.Presence != tc.want || status.On != tc.on {
				t.Fatalf("the reading is %q on %q, want %q on %q", status.Presence, status.On, tc.want, tc.on)
			}
			if status.Attention != tc.ask {
				t.Fatalf("the reading asks for a person: %v, want %v", status.Attention, tc.ask)
			}
			if status.Fault {
				t.Fatalf("a gate is not a fault: %+v", status)
			}
			// The gate never moves the lifecycle: the run is over or it is not, and
			// a question about money says nothing about which.
			if status.Settled() != tc.facts.State.settled() {
				t.Fatalf("the reading settled a %q row: %+v", tc.facts.State, status)
			}
		})
	}
}

// ── the whole road ──────────────────────────────────────────────────────────

// THE OWNER'S SCENARIO, end to end and through the real doors: a run spends its
// tank, the roster's row says a person is being asked, and answering the gate
// takes it back off. It drives [Agent.RunOrchestrate] rather than the family so
// that the two wires this depends on — the engine's OnPause and
// [Agent.ResolveOrchestrate] — are the ones under test.
func TestARunThatEmptiesItsTankHoldsItsRowUntilItIsAnswered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// The machine ledger writes on its process-wide background queue. Register
	// this cleanup before the agent is made so cleanup closes its run first, then
	// drains the last usage rows before TempDir removes their temporary home.
	t.Cleanup(FlushUsage)
	// The planner's own call is priced, which is the honest shape: a run's tank
	// pays for the judgement as well as the work, and one call at sixty cents
	// empties a fifty-cent tank on the opening plan.
	completer := replier(func(messages []ai.Message) string {
		if isPlannerCall(messages) {
			return `{"add":[{"id":"n1","goal":"read the notes"}]}`
		}
		return "n1 read the notes"
	})
	agent, _ := newTestAgent(t, pricedAt(completer, 0.60), nil)
	updates := agent.TaskUpdates()

	id, err := agent.RunOrchestrate(context.Background(), "summarise the release notes", "", 0.50)
	if err != nil {
		t.Fatal(err)
	}
	held := waitForRunRow(t, updates, id, func(row TaskNotice) bool { return row.Node == "" && row.Paused })
	if held.State != TaskRunning {
		t.Fatalf("the held run's row says %q, want it still running: %+v", held.State, held)
	}
	// The row and the run agree: this is a gate that is genuinely up.
	snap, known := agent.OrchestrateSnapshot(id)
	if !known || !snap.Paused {
		t.Fatalf("the row said the gate was up and the run says %+v", snap)
	}
	if status := ProjectTask(TaskFacts{State: held.State, Paused: held.Paused}); !status.Attention ||
		status.Presence != TaskPresenceNeedsLook {
		t.Fatalf("the roster's reading of a run out of money is %+v", status)
	}

	if _, err := agent.ResolveOrchestrate(id, "topup:1.00"); err != nil {
		t.Fatalf("the gate would not take an answer: %v", err)
	}
	// AND THE ROW STOPS ASKING. What state it is in by the time this arrives is
	// the run's business — a topped-up run carries on and may well finish — so
	// what is asserted is the only thing the answer promises: no row of this run
	// is still standing at a gate the person has answered.
	back := waitForRunRow(t, updates, id, func(row TaskNotice) bool { return row.Node == "" && !row.Paused })
	if back.ID != held.ID {
		t.Fatalf("the answer landed on row %d, want the run's own %d", back.ID, held.ID)
	}
}

// waitForRunRow takes rows off a lane until one of this run's rows answers the
// question, or fails the test rather than hanging the suite on a lane that never
// spoke.
func waitForRunRow(t *testing.T, updates <-chan Event, run string, want func(TaskNotice) bool) TaskNotice {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case event, open := <-updates:
			if !open {
				t.Fatalf("the task lane closed before run %q said what was asked of it", run)
			}
			if event.Kind != EventTaskUpdate || event.Task == nil || event.Task.Run != run {
				continue
			}
			if want(*event.Task) {
				return *event.Task
			}
		case <-deadline:
			t.Fatalf("no row of run %q ever answered", run)
		}
	}
}

func TestALateRunningPublicationCannotReopenASettledRun(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "cheap/planner", "run-late")
	family.pauseRun(true)
	familyNotices(t, updates)
	family.settle(orchestrate.Snapshot{Done: true, Stopped: true}, nil)
	familyNotices(t, updates)
	family.publish(TaskNotice{ID: family.root, Run: family.run, State: TaskRunning, Paused: true})
	if rows := familyNotices(t, updates); len(rows) != 0 {
		t.Fatalf("late progress reopened settled work: %+v", rows)
	}
	rows := runRowsOf(familyNotices(t, agent.TaskUpdates()), "run-late")
	if len(rows) != 1 || rows[0].State == TaskRunning || rows[0].Paused {
		t.Fatalf("late progress corrupted replay: %+v", rows)
	}
}
