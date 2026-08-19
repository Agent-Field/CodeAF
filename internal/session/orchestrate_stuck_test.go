package session

// TWO DEFECTS FROM ONE REAL RUN, PINNED.
//
// A person asked for a research report on 18 August. The run's twelve nodes all
// landed by 19:41 and the root row in the project index still said "running" at
// nine that evening, through a restart of the whole program. Underneath it, one
// brief — "write the final report to research/ai-dev-startups-2026.md" — had
// been handed to EIGHT successive workers, every one of which came back "done"
// with a sentence like "Now I have both files. Let me write the synthesized
// report." and a file that was never written.
//
// The tests below are the two halves of that: a row that can never sit saying
// "running" once nothing is running it, and a run that stops handing out a brief
// that keeps coming back empty.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── a run that ended is a run the project's record says ended ───────────────

// A SETTLED RUN CLOSES ITS OWN ROW. The root takes a row saying "running" the
// moment it is minted, because a run somebody can watch has to be in the list
// they are watching. That row is a promise, and this is the promise kept in the
// ordinary case: the run ends inside the process that started it.
func TestARunClosesItsOwnRowWhenItSettles(t *testing.T) {
	var index string
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		index = TaskIndexPath(config.SessionFile)
	})
	family := agent.newOrchestrateFamily("audit the pricing code", "", "run-7")

	rows := ReadTaskIndex(index)
	if len(rows) != 1 || !rows[0].Live() {
		t.Fatalf("a run in flight is not in the project's record as running: %+v", rows)
	}

	family.settle(orchestrate.Snapshot{
		Answer: "the pricing rounds down in two places",
		Fuel:   orchestrate.Fuel{Spent: 0.42},
		Done:   true,
	}, nil)

	rows = ReadTaskIndex(index)
	if len(rows) != 1 {
		t.Fatalf("the index answers with more than one row for one run: %+v", rows)
	}
	switch {
	case rows[0].Live():
		t.Fatalf("the run is still described as running after it settled: %+v", rows[0])
	case rows[0].Status != string(TaskDone):
		t.Fatalf("the closing row says %q", rows[0].Status)
	case rows[0].Outcome != "the pricing rounds down in two places":
		t.Fatalf("the closing row's outcome is %q", rows[0].Outcome)
	case rows[0].Cost != 0.42:
		t.Fatalf("the closing row lost what the run spent: %+v", rows[0])
	}
}

// AND A RUN NOBODY EVER CLOSED IS CLOSED WHEN THE SESSION OPENS AGAIN. This is
// the defect exactly: the process went away with the row still saying
// "running", so [orchestrateFamily.settle] never ran, and nothing else in the
// program had ever looked at that file again. Opening the session is the moment
// the claim becomes false, and it is where it now gets answered.
func TestARunLeftInFlightIsClosedWhenTheSessionOpensAgain(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	index := TaskIndexPath(journal)

	first, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	first.newOrchestrateFamily("research new developer-tooling startups", "", "run-1")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	rows := ReadTaskIndex(index)
	if len(rows) != 1 || !rows[0].Live() {
		t.Fatalf("the dead process did not leave a running row to find: %+v", rows)
	}

	second, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	rows = ReadTaskIndex(index)
	if len(rows) != 1 {
		t.Fatalf("the index answers with more than one row for one run: %+v", rows)
	}
	if rows[0].Live() {
		t.Fatalf("the run is STILL running hours and one restart later: %+v", rows[0])
	}
	if rows[0].Outcome != taskInterruptedOutcome {
		t.Fatalf("the closed row says %q, want the interrupted sentence", rows[0].Outcome)
	}
	if rows[0].Title != "research new developer-tooling startups" {
		t.Fatalf("the closed row lost what the work was: %+v", rows[0])
	}
	// AND THE SURFACE AGREES. Everything a person or the model reads about this
	// project's work comes through here, and it is what was drawing "running".
	for _, row := range second.TaskIndex() {
		if row.Live() {
			t.Fatalf("the list a person reads still holds live work: %+v", row)
		}
	}
}

// ANOTHER WINDOW'S WORK IS NOT OURS TO CLOSE. The index is one file shared by
// every session open on the project, and a running row belonging to a session
// that is not this one is another process's live work.
func TestOpeningASessionLeavesAnotherSessionsRunningRowAlone(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	index := TaskIndexPath(journal)
	appendTaskIndex(index, TaskIndexEntry{
		ID: "3", Name: "someone-elses-run", Label: "someone else's run",
		Title: "someone else's run", Status: string(TaskRunning), SessionID: "another-window",
	})

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	_ = agent
	rows := ReadTaskIndex(index)
	if len(rows) != 1 || !rows[0].Live() {
		t.Fatalf("we closed a row that was never ours: %+v", rows)
	}
}

// ── a brief that keeps coming back empty is not sent out again ──────────────

// THE EIGHT CHILDREN, IN ONE TEST. The worker here does what the real one did:
// it reads its sources, announces the deliverable, and ends its turn without
// writing anything. The planner does what the real one did: it sees a digest
// that reads like progress and sends the same brief out again.
//
// What must happen now is that neither of those lies survives. A node scoped to
// write a file and ending without writing it comes back FAILED with a digest
// that says so, and after two of them the run stops taking new work for that
// file and goes to a write-up that says which part of the goal is incomplete.
func TestARunStopsAfterTheSameFileComesBackUnwrittenTwice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var (
		mu      sync.Mutex
		minted  int
		briefed string
	)
	completer := replier(func(messages []ai.Message) string {
		asked := lastUserText(messages)
		switch {
		case isPlannerCall(messages):
			// A planner that never tires: every completion, the same brief again.
			mu.Lock()
			minted++
			id := fmt.Sprintf("n%d", minted)
			mu.Unlock()
			return fmt.Sprintf(`{"add":[{"id":%q,"goal":"write the final report to report.md",`+
				`"write_scope":["report.md"]}]}`, id)
		case strings.Contains(asked, "Ground every claim"):
			mu.Lock()
			briefed = asked
			mu.Unlock()
			return "the write-up"
		default:
			// The defect in one sentence: the worker says what it is about to do
			// and then stops.
			return "Now I have both files. Let me write the synthesized report."
		}
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })

	id, err := agent.RunOrchestrate(context.Background(), "write up the research", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitForRun(t, agent, id)

	if len(snap.Nodes) != orchestrate.RepeatLimit {
		t.Fatalf("the run sent the same brief out %d times, want %d: %+v",
			len(snap.Nodes), orchestrate.RepeatLimit, snap.Nodes)
	}
	for _, node := range snap.Nodes {
		if node.State != orchestrate.Failed {
			t.Fatalf("node %s wrote nothing and still landed %v", node.ID, node.State)
		}
		if !strings.HasPrefix(node.Digest, "INCOMPLETE: nothing was written") {
			t.Fatalf("node %s handed on narration as a result: %q", node.ID, node.Digest)
		}
		if !strings.Contains(node.Err, "report.md") {
			t.Fatalf("node %s does not say which file went unwritten: %q", node.ID, node.Err)
		}
	}

	var stopped bool
	for _, note := range snap.Notes {
		if strings.Contains(note, "report.md has been handed out 2 times and nothing was written to it") {
			stopped = true
		}
	}
	if !stopped {
		t.Fatalf("the run never said out loud that it had stopped repeating itself: %+v", snap.Notes)
	}

	mu.Lock()
	brief := briefed
	mu.Unlock()
	if !strings.Contains(brief, "incomplete") || !strings.Contains(brief, "report.md") {
		t.Fatalf("the write-up was not asked to be honest about the unwritten file: %q", brief)
	}
	if snap.Answer != "the write-up" {
		t.Fatalf("a run that gave up still owes a write-up, got %q", snap.Answer)
	}
}

// A READ-ONLY NODE WRITES NOTHING BY DESIGN and is never held to this. Its brief
// says "THIS IS READ-ONLY WORK: find out, do not change anything", and a run
// that failed it for obeying would fail every question it ever asked.
func TestAReadOnlyNodeIsNotFailedForWritingNothing(t *testing.T) {
	if got := orchestrateUnwritten(orchestrate.Node{ID: "n1", Goal: "read the tariff table"}, nil); got != "" {
		t.Fatalf("read-only work was called incomplete: %q", got)
	}
	scoped := orchestrate.Node{ID: "n2", Goal: "write it up", WriteScope: []string{"report.md"}}
	if got := orchestrateUnwritten(scoped, []string{"report.md"}); got != "" {
		t.Fatalf("a node that wrote its file was called incomplete: %q", got)
	}
	if got := orchestrateUnwritten(scoped, nil); !strings.Contains(got, "report.md") {
		t.Fatalf("a node scoped to write that wrote nothing said %q", got)
	}
}
