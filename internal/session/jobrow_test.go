package session

// A BACKGROUND JOB IS A ROW ON THE ROSTER WHILE IT LIVES (jobrow.go).
//
// Every case here reads the ROSTER LANE — [Agent.WatchTaskUpdates], the one door
// every surface draws live work from — rather than the registry, because the
// defect this wave fixed was not that the registry forgot a job. It was that
// nothing outside the registry could ever be told about one.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// rosterLane opens a standing subscription and hands back the events it has
// been given so far, newest last. The stop is registered with the test so a
// lane never outlives the case that opened it.
func rosterLane(t *testing.T, agent *Agent) <-chan Event {
	t.Helper()
	lane, stop := agent.WatchTaskUpdates()
	t.Cleanup(stop)
	return lane
}

// awaitJobRow drains a lane until a job row in the wanted state arrives, and
// fails on the deadline. It is a poll rather than a single receive because the
// roster carries other rows and because a job's ending is another goroutine's
// news (jobs_test.go's [waitFor] states the same rule for the registry).
func awaitJobRow(t *testing.T, lane <-chan Event, want TaskState) TaskNotice {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-lane:
			if !open {
				t.Fatalf("the roster lane closed before a %s job row arrived", want)
			}
			if event.Kind != EventTaskUpdate || event.Task == nil {
				continue
			}
			if event.Task.Kind == TaskKindJob && event.Task.State == want {
				return *event.Task
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %s job row on the roster", want)
		}
	}
}

// ── the row appears ─────────────────────────────────────────────────────────

// THE OWNER'S SCENARIO, REDUCED TO ITS ONE MISSING FACT: a command started in
// the background puts a row in front of whoever is drawing live work, naming the
// command and its log, from the moment it starts.
func TestABackgroundCommandPutsARowOnTheRoster(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	answer, isError := runBash(t, context.Background(), agent, map[string]any{
		"command":    "sleep 30",
		"background": true,
	})
	if isError {
		t.Fatalf("a background start answered as an error: %q", answer)
	}

	row := awaitJobRow(t, lane, TaskRunning)
	if row.Title != "sleep 30" {
		t.Fatalf("the row is named %q, not after the command that made it", row.Title)
	}
	if row.ID == 0 {
		t.Fatal("the row carries no id, so no surface can key it")
	}
	// THE LOG IS REACHABLE FROM THE ROW, which is a job's only handle back: it
	// has no room, no branch and no report anybody wrote.
	if !strings.Contains(row.Report, ".log") {
		t.Fatalf("the row does not name its log: %q", row.Report)
	}

	// AND THE HEAD COUNT SAYS SOMETHING IS WORKING. An empty column and a "0
	// working" head were the two halves of the same lie.
	if count := CountWorking(agent.WorkingNow()); count != 1 {
		t.Fatalf("a running background job counts as %d working, not 1", count)
	}
}

// A PROMOTED COMMAND IS A JOB IN EVERY WAY, INCLUDING THIS ONE. A foreground
// call that reaches its bound is adopted rather than killed (promote.go), and
// the row it grows is what tells the person the work did not stop.
func TestAPromotedCommandPutsARowOnTheRoster(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	answer, _ := runBash(t, context.Background(), agent, map[string]any{
		"command": "sleep 30",
		"timeout": 0.4,
	})
	id := promotedJobID(t, answer)

	row := awaitJobRow(t, lane, TaskRunning)
	if row.Title != "sleep 30" {
		t.Fatalf("the promoted row is named %q, not after the command that made it", row.Title)
	}
	if !strings.Contains(row.Report, jobRowLead(id, "")) {
		t.Fatalf("the row does not name job %d: %q", id, row.Report)
	}
}

// ── the row settles ─────────────────────────────────────────────────────────

// A JOB THAT CAME OFF IS FINISHED WORK. Nothing is left to do about it and the
// row says so, which is what lets a column stop drawing a spinner over it.
func TestAJobThatExitsCleanlySettlesAsDone(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	runBash(t, context.Background(), agent, map[string]any{
		"command":    "true",
		"background": true,
	})

	row := awaitJobRow(t, lane, TaskDone)
	if row.Stopped {
		t.Fatal("a job that ended on its own is marked as one somebody stopped")
	}
	// AND THE PRESENT IT REPORTED IS OVER. A settled row that still counted as
	// working would leave the head saying a number nothing is behind.
	waitFor(t, "the head count to drop back to nothing", func() bool {
		return CountWorking(agent.WorkingNow()) == 0
	})
}

// A JOB THAT DID NOT COME OFF IS INCOMPLETE WORK, and the row is the only place
// a person finds that out without asking.
func TestAJobThatExitsBadlySettlesAsIncomplete(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	runBash(t, context.Background(), agent, map[string]any{
		"command":    "exit 3",
		"background": true,
	})

	row := awaitJobRow(t, lane, TaskFailed)
	if row.Stopped {
		t.Fatal("a job that failed on its own is marked as one somebody stopped")
	}
}

// A JOB SOMEBODY ENDED IS NOT A FAILURE, and the row carries the difference:
// the state is the same, and the fact beside it is the whole of the news.
func TestAKilledJobSaysItWasStopped(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	answer, _ := runBash(t, context.Background(), agent, map[string]any{
		"command":    "sleep 30",
		"background": true,
	})
	var id int
	if _, err := fmt.Sscanf(answer, "job %d started", &id); err != nil {
		t.Fatalf("not a background-start sentence: %q", answer)
	}
	awaitJobRow(t, lane, TaskRunning)

	if text, isError := killJobThroughTool(t, agent, id); isError {
		t.Fatalf("the kill answered as an error: %q", text)
	}
	row := awaitJobRow(t, lane, TaskFailed)
	if !row.Stopped {
		t.Fatal("a job this session killed does not say it was stopped")
	}
}

// killJobThroughTool ends one job through the tool, which is the only door the model has.
func killJobThroughTool(t *testing.T, agent *Agent, id int) (string, bool) {
	t.Helper()
	return runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"kill","id":%d}`, id))
}

// ── the row is there for whoever arrives later ──────────────────────────────

// A SURFACE THAT ATTACHES WHILE THE WORK IS ALREADY GOING IS HANDED THE ROW.
// This is the case a switched-away conversation and a resumed window are both
// made of, and the graph's own replay cannot answer it — a job is no node
// (rosterrecord.go).
func TestAReattachedLaneIsHandedTheJobsAlreadyRunning(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	first := rosterLane(t, agent)
	runBash(t, context.Background(), agent, map[string]any{
		"command":    "sleep 30",
		"background": true,
	})
	started := awaitJobRow(t, first, TaskRunning)

	// A SECOND LANE, opened with nothing drawn on it, exactly as a surface
	// switching back to this conversation opens one.
	second := rosterLane(t, agent)
	replayed := awaitJobRow(t, second, TaskRunning)
	if replayed.ID != started.ID {
		t.Fatalf("the replayed row is id %d and the live one was %d — a surface would draw two",
			replayed.ID, started.ID)
	}
	if replayed.Title != started.Title {
		t.Fatalf("the replayed row is named %q and the live one %q", replayed.Title, started.Title)
	}
}

// ── and tomorrow ────────────────────────────────────────────────────────────

// A ROW STILL MOVING WHEN THE FILE WAS WRITTEN COMES BACK IN THE RUN ROWS' OWN
// REGISTER, with the one noun that differs changed: a job leaves the log it was
// writing where a run leaves its journal. It is asserted here rather than
// through a resume because which of the two endings a job gets at Close is a
// race with the process teardown — the reaper usually settles the row first, and
// this is the sentence for the times it does not.
func TestAJobRowStillMovingComesBackInTheJobsOwnWords(t *testing.T) {
	t.Parallel()
	restored := runRowNotice(runRecord{
		ID: 4, Title: "npm run dev", Kind: TaskKindJob, State: TaskRunning,
		Report: jobRowLead(3, "/tmp/jobs/3.log"),
	})
	if !restored.Stopped || restored.State != TaskFailed {
		t.Fatalf("a job row that was moving came back as %s (stopped %v)", restored.State, restored.Stopped)
	}
	if restored.Report != jobEndedReport {
		t.Fatalf("the restored row says %q, want %q", restored.Report, jobEndedReport)
	}
	// AND A RUN'S ROW IS UNTOUCHED BY IT: one register, two nouns, and neither
	// sentence may be said over the other's work.
	if run := runRowNotice(runRecord{ID: 5, State: TaskRunning}); run.Report != orchestrateEndedReport {
		t.Fatalf("a run's row now says %q", run.Report)
	}
}

// AND A CONVERSATION REOPENED TOMORROW REDRAWS THE JOB IT RAN, settled: the
// process the job WAS is gone, so nothing is restarted, nothing re-enters a
// frontier and nothing counts as working — but the column is not empty beside a
// transcript that talks about the work.
func TestAResumedConversationRedrawsAMidFlightJobSettled(t *testing.T) {
	t.Parallel()
	journal, _ := journalIn(t)
	yesterday, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	lane := rosterLane(t, yesterday)
	runBash(t, context.Background(), yesterday, map[string]any{
		"command":    "sleep 30",
		"background": true,
	})
	started := awaitJobRow(t, lane, TaskRunning)
	if err := yesterday.Close(); err != nil {
		t.Fatalf("close the first conversation: %v", err)
	}

	today, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	restored := awaitJobRow(t, rosterLane(t, today), TaskFailed)
	if restored.ID != started.ID {
		t.Fatalf("the restored row is id %d and yesterday's was %d", restored.ID, started.ID)
	}
	if restored.Kind != TaskKindJob {
		t.Fatalf("the restored row came back as kind %q", restored.Kind)
	}
	if !restored.Stopped {
		t.Fatalf("the restored row came back as a failure rather than work cut short: %+v", restored)
	}
	if restored.Title != started.Title {
		t.Fatalf("the restored row is named %q and yesterday's was %q", restored.Title, started.Title)
	}
	if working := CountWorking(today.WorkingNow()); working != 0 {
		t.Fatalf("%d pieces of work are moving in a session that has started none", working)
	}
}

// ── nothing, when there is nothing ──────────────────────────────────────────

// ZERO JOBS ADDS NOTHING ANYWHERE — the emptiness law, stated over the two doors
// a job reaches: no row on the roster and no head count.
func TestASessionWithNoJobsPublishesNothing(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	if nodes := agent.WorkingNow(); len(nodes) != 0 {
		t.Fatalf("a session that has started nothing reports %d working", len(nodes))
	}
	select {
	case event := <-lane:
		t.Fatalf("a lane on a session with no work was handed %v", event.Kind)
	case <-time.After(50 * time.Millisecond):
	}
}

// ── the one job that is NOT a row ───────────────────────────────────────────

// A TASK NODE IS IN THE REGISTRY AND IS NOT A JOB ROW. It already has a roster
// row of its own, published by the graph that runs it, and a second one here
// would draw and count the same piece of work twice (jobs.go's [jobKindTask]).
func TestATaskNodeIsNotGivenASecondRow(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	node, err := agent.jobs.startTask(7, "Fix the nil-map crash", func() {})
	if err != nil {
		t.Fatalf("could not register the node: %v", err)
	}
	graph := agent.graph()
	graph.mu.Lock()
	rows := len(graph.runRowsLocked())
	graph.mu.Unlock()
	if rows != 0 {
		t.Fatalf("registering a task node published %d record rows", rows)
	}
	// Nor does it join the live tree here: the graph's own half of
	// [Agent.WorkingNow] is where a node is counted.
	for _, one := range agent.jobsWorkingNow() {
		t.Fatalf("a task node appears in the jobs' half of the work tree as %q", one.Title)
	}
	node.settle(0)
}
