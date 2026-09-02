package session

// A BACKGROUND JOB PUBLISHES ITSELF WHILE IT LIVES (jobrow.go, jobnotice.go).
//
// Every case here reads the LANE a surface draws from — [Agent.WatchTaskUpdates],
// the one standing door — rather than the registry, because the defect this
// wave fixed was not that the registry forgot a job. It was that nothing outside
// the registry could ever be told about one.
//
// WHAT THEY WAIT FOR IS AN [EventJobUpdate] AND NOT A TASK ROW. A job used to be
// announced as a [TaskNotice] of a `job` kind, and these cases waited on that;
// the row is still what the CHECKPOINT keeps, because a file already written is
// read by the aforge that opens it next, but nothing is drawn from it any more.
// A job that arrived on the roster's lane would be one piece of work counted
// twice — once in the jobs section and once among the task families.

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
//
// IT IS ONE LANE FOR BOTH KINDS OF NEWS. Jobs and tasks are published as
// different events and travel the same standing subscription, which is why this
// is not renamed: a surface opens one door and is told about everything this
// conversation is doing.
func rosterLane(t *testing.T, agent *Agent) <-chan Event {
	t.Helper()
	lane, stop := agent.WatchTaskUpdates()
	t.Cleanup(stop)
	return lane
}

// awaitJob drains a lane until a job in the wanted state arrives, and fails on
// the deadline. It is a poll rather than a single receive because the lane
// carries other news and because a job's ending is another goroutine's
// (jobs_test.go's [waitFor] states the same rule for the registry).
func awaitJob(t *testing.T, lane <-chan Event, want JobState) JobNotice {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-lane:
			if !open {
				t.Fatalf("the lane closed before a %s job arrived", want)
			}
			if event.Kind != EventJobUpdate || event.Job == nil {
				continue
			}
			if event.Job.State == want {
				return *event.Job
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %s job", want)
		}
	}
}

// ── the job appears ─────────────────────────────────────────────────────────

// THE OWNER'S SCENARIO, REDUCED TO ITS ONE MISSING FACT: a command started in
// the background is put in front of whoever is drawing live work, carrying the
// command and its log, from the moment it starts.
func TestABackgroundCommandIsPublishedToTheSurface(t *testing.T) {
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

	job := awaitJob(t, lane, JobRunning)
	if job.Command != "sleep 30" {
		t.Fatalf("the job carries the command %q, not the one that made it", job.Command)
	}
	if job.ID == 0 {
		t.Fatal("the job carries no id, so no surface can key it or stop it")
	}
	// THE LOG IS A FIELD AND NOT A SENTENCE. It is a job's only handle back — it
	// has no room, no branch and no report anybody wrote — and a surface should
	// never have to parse prose to find it.
	if !strings.Contains(job.LogPath, ".log") {
		t.Fatalf("the job does not name its log: %q", job.LogPath)
	}
	// AND IT HAS A START, so a column can count its clock up on its own beat
	// rather than re-asking the engine four times a second.
	if job.Started.IsZero() {
		t.Fatal("the job carries no start, so no clock can tick from it")
	}

	// AND THE HEAD COUNT SAYS SOMETHING IS WORKING. An empty column and a "0
	// working" head were the two halves of the same lie.
	if count := CountWorking(agent.WorkingNow()); count != 1 {
		t.Fatalf("a running background job counts as %d working, not 1", count)
	}
}

// A PROMOTED COMMAND IS A JOB IN EVERY WAY, INCLUDING THIS ONE. A foreground
// call that reaches its bound is adopted rather than killed (promote.go), and
// what it publishes is what tells the person the work did not stop.
func TestAPromotedCommandIsPublishedToTheSurface(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	answer, _ := runBash(t, context.Background(), agent, map[string]any{
		"command": "sleep 30",
		"timeout": 0.4,
	})
	id := promotedJobID(t, answer)

	job := awaitJob(t, lane, JobRunning)
	if job.Command != "sleep 30" {
		t.Fatalf("the promoted job carries the command %q", job.Command)
	}
	// THE ID IS THE ONE THE MODEL WAS ALREADY GIVEN. `jobs output 3` and
	// `jobs kill 3` take it, and so does [Agent.Cancel] as `job:3` — one thing,
	// one number, which is the whole reason a job no longer mints a second.
	if job.ID != id {
		t.Fatalf("the published job is %d and the model was told %d", job.ID, id)
	}
}

// ── the job settles ─────────────────────────────────────────────────────────

// A JOB THAT CAME OFF IS FINISHED WORK. Nothing is left to do about it and it
// says so, which is what lets a column stop drawing a clock over it.
func TestAJobThatExitsCleanlySettlesAsDone(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	runBash(t, context.Background(), agent, map[string]any{
		"command":    "true",
		"background": true,
	})

	job := awaitJob(t, lane, JobDone)
	if job.ExitCode != 0 {
		t.Fatalf("a job that ended cleanly carries exit %d", job.ExitCode)
	}
	// AND THE PRESENT IT REPORTED IS OVER. A settled job that still counted as
	// working would leave the head saying a number nothing is behind.
	waitFor(t, "the head count to drop back to nothing", func() bool {
		return CountWorking(agent.WorkingNow()) == 0
	})
}

// A JOB THAT DID NOT COME OFF IS INCOMPLETE WORK, and this is the only place a
// person finds that out without asking.
func TestAJobThatExitsBadlySettlesAsIncomplete(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	lane := rosterLane(t, agent)

	runBash(t, context.Background(), agent, map[string]any{
		"command":    "exit 3",
		"background": true,
	})

	job := awaitJob(t, lane, JobFailed)
	// THE CODE IS CARRIED AND NOT SUMMARISED. "exited 3" is a sentence a surface
	// writes at its own width; the number is the fact.
	if job.ExitCode != 3 {
		t.Fatalf("a job that exited 3 carries exit %d", job.ExitCode)
	}
}

// A JOB SOMEBODY ENDED IS NOT A FAILURE, and it is a STATE of its own rather
// than a flag beside one: "it failed" and "you stopped it" are different news
// about a process that is equally not running, and a surface should not read two
// fields to tell them apart.
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
	awaitJob(t, lane, JobRunning)

	if text, isError := killJobThroughTool(t, agent, id); isError {
		t.Fatalf("the kill answered as an error: %q", text)
	}
	if job := awaitJob(t, lane, JobStopped); job.ID != id {
		t.Fatalf("the stopped job is %d and the killed one was %d", job.ID, id)
	}
}

// killJobThroughTool ends one job through the tool, which is the model's door.
func killJobThroughTool(t *testing.T, agent *Agent, id int) (string, bool) {
	t.Helper()
	return runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"kill","id":%d}`, id))
}

// ── the job is there for whoever arrives later ──────────────────────────────

// A SURFACE THAT ATTACHES WHILE THE WORK IS ALREADY GOING IS HANDED IT. This is
// the case a switched-away conversation and a resumed window are both made of,
// and the graph's own replay cannot answer it — a job is no node.
func TestAReattachedLaneIsHandedTheJobsAlreadyRunning(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	first := rosterLane(t, agent)
	runBash(t, context.Background(), agent, map[string]any{
		"command":    "sleep 30",
		"background": true,
	})
	started := awaitJob(t, first, JobRunning)

	// A SECOND LANE, opened with nothing drawn on it, exactly as a surface
	// switching back to this conversation opens one.
	second := rosterLane(t, agent)
	replayed := awaitJob(t, second, JobRunning)
	if replayed.ID != started.ID {
		t.Fatalf("the replayed job is %d and the live one was %d — a surface would draw two",
			replayed.ID, started.ID)
	}
	if replayed.LogPath != started.LogPath {
		t.Fatalf("the replayed job's log is %q and the live one's %q",
			replayed.LogPath, started.LogPath)
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

// A CHECKPOINTED ROW IS READ BACK AS THE JOB IT WAS. The store keeps a task row
// because that is what older aforges wrote, and the packed `job 3 · log /…`
// sentence inside it is parsed exactly once, here at the edge — so everything
// above gets an id and a path in fields of their own (jobnotice.go).
func TestACheckpointedRowIsReadBackAsTheJobItWas(t *testing.T) {
	t.Parallel()
	job, isJob := jobNoticeFromRow(TaskNotice{
		ID: 9, Title: "npm run dev", Kind: TaskKindJob,
		State: TaskFailed, Stopped: true,
		Report: jobRowLead(3, "/tmp/jobs/3.log"),
	})
	if !isJob {
		t.Fatal("a checkpointed job row was not read back as a job")
	}
	// THE JOB'S OWN NUMBER WINS OVER THE ROSTER'S. The row was drawn under 9 and
	// the work was always job 3; 3 is what `jobs kill` and `job:3` take.
	if job.ID != 3 {
		t.Fatalf("the restored job is %d, not the number the model was given", job.ID)
	}
	if job.LogPath != "/tmp/jobs/3.log" {
		t.Fatalf("the restored job's log is %q", job.LogPath)
	}
	if job.State != JobStopped {
		t.Fatalf("a row that was cut short came back as %q", job.State)
	}
	if _, isJob := jobNoticeFromRow(TaskNotice{ID: 5, State: TaskDone}); isJob {
		t.Fatal("an ordinary task row was read back as a job")
	}
}

// AND A CONVERSATION REOPENED TOMORROW REDRAWS THE JOB IT RAN, settled: the
// process the job WAS is gone, so nothing is restarted, nothing re-enters a
// frontier and nothing counts as working — but the section is not empty beside a
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
	started := awaitJob(t, lane, JobRunning)
	if err := yesterday.Close(); err != nil {
		t.Fatalf("close the first conversation: %v", err)
	}

	today, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	restored := awaitJob(t, rosterLane(t, today), JobStopped)
	if restored.ID != started.ID {
		t.Fatalf("the restored job is %d and yesterday's was %d", restored.ID, started.ID)
	}
	// THE LOG SURVIVES THE PROCESS, and it is the whole of what a finished job
	// left behind — a restored job without it is a row a person cannot act on.
	if restored.LogPath != started.LogPath {
		t.Fatalf("the restored job's log is %q and yesterday's was %q",
			restored.LogPath, started.LogPath)
	}
	if working := CountWorking(today.WorkingNow()); working != 0 {
		t.Fatalf("%d pieces of work are moving in a session that has started none", working)
	}
}

// ── nothing, when there is nothing ──────────────────────────────────────────

// ZERO JOBS ADDS NOTHING ANYWHERE — the emptiness law, stated over the two doors
// a job reaches: nothing published and no head count.
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

// ── the one job that is NOT published ───────────────────────────────────────

// A TASK NODE IS IN THE REGISTRY AND IS NOT PUBLISHED AS A JOB. It already has a
// roster row of its own, published by the graph that runs it, and a second
// appearance here would draw and count the same piece of work twice (jobs.go's
// [jobKindTask]).
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
