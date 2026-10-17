package session

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// conversationAgent is a conversation's own agent, holding its history in dir.
func conversationAgent(t *testing.T, dir string) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: dir}
		c.SessionFile = Place{Dir: dir}.Transcript()
	})
	return agent
}

// workerAgent is a task worker writing into the conversation in dir.
func workerAgent(t *testing.T, dir string, task uint64) *Agent {
	t.Helper()
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = filepath.Join(dir, placeNodeJournals, "x_"+strconv.FormatUint(task, 10)+".jsonl")
		c.droppings = Place{Dir: dir}
		c.taskID = task
	})
	t.Cleanup(func() { _ = worker.Close() })
	return worker
}

// AN UNKNOWN RECORD IS NEVER RUN AGAIN. A record saved as running that no live
// owner vouches for has an ending nobody saw; the old door rewrote it to
// stopped and ran it.
func TestRerunRefusesARecordNobodySawEnd(t *testing.T) {
	dir := t.TempDir()
	agent := conversationAgent(t, dir)
	agent.keepProcessNotice(JobNotice{ID: 900, Kind: JobKindCommand, Command: "printf unknown", State: JobRunning,
		Started: time.Now(), Dir: agent.config.Workspace})
	if _, err := agent.RerunProcess(900); err == nil {
		t.Fatal("an unknown record was run again")
	}
}

// A RECORD FROM ANOTHER FOLDER, OR WITH NONE, IS NEVER RUN IN THIS ONE.
func TestRerunRefusesARecordFromAnotherFolderOrNone(t *testing.T) {
	dir := t.TempDir()
	agent := conversationAgent(t, dir)
	elsewhere := t.TempDir()
	agent.keepProcessNotice(JobNotice{ID: 901, Kind: JobKindCommand, Command: "printf elsewhere", State: JobDone,
		Started: time.Now(), Ended: time.Now(), Dir: elsewhere})
	agent.keepProcessNotice(JobNotice{ID: 902, Kind: JobKindCommand, Command: "printf nowhere", State: JobDone,
		Started: time.Now(), Ended: time.Now()})
	for _, id := range []int{901, 902} {
		if _, err := agent.RerunProcess(id); err == nil {
			t.Fatalf("record %d was run again in the conversation's own workspace", id)
		}
	}
}

// A TASK WORKER'S RECORD IS NOT RUN AGAIN FROM THE CONVERSATION, even in the
// same folder: it was the worker's command.
func TestRerunRefusesATaskWorkersRecord(t *testing.T) {
	dir := t.TempDir()
	agent := conversationAgent(t, dir)
	agent.keepProcessNotice(JobNotice{ID: 903, Kind: JobKindCommand, Command: "printf worker", State: JobDone,
		Started: time.Now(), Ended: time.Now(), Dir: agent.config.Workspace})
	// keepProcessNotice stamps this agent's own words; write the worker's.
	root, head, err := processStore{dir: dir}.open(processFoldWriteTime)
	if err != nil {
		t.Fatal(err)
	}
	n, _, _ := readRecord(root, 903)
	n.StartedBy, n.Task = JobStartedByTask, 7
	if _, err := (processStore{dir: dir}).put(root, &head, n, true); err != nil {
		t.Fatal(err)
	}
	root.Close()
	if _, err := agent.RerunProcess(903); err == nil {
		t.Fatal("a task worker's record was run again from the conversation")
	}
}

// A PERSON'S STOP REACHES A TASK WORKER'S JOB the conversation's page shows
// live: the owner is found in the engine's own owner map, never by a guess.
func TestStopReachesATaskWorkersLiveJob(t *testing.T) {
	dir := t.TempDir()
	conversation := conversationAgent(t, dir)
	worker := workerAgent(t, dir, 7)
	lane := rosterLane(t, worker)
	id := startJob(t, worker, "sleep 30")
	if _, err := conversation.Cancel("job:" + strconv.Itoa(id)); err != nil {
		t.Fatalf("the conversation could not stop its task worker's job: %v", err)
	}
	awaitJob(t, lane, JobStopped)
}

// THE FACTS ARE THE REGISTRY'S AND THE RECORD'S, by number and start.
func TestProcessActionsAnswerEachRunFromItsHolder(t *testing.T) {
	dir := t.TempDir()
	agent := conversationAgent(t, dir)
	lane := rosterLane(t, agent)
	done := startJob(t, agent, "printf done")
	awaitJob(t, lane, JobDone)
	n, _, err := agent.ProcessRecord(done)
	if err != nil {
		t.Fatal(err)
	}
	stamp := n.Started.UnixMilli()
	facts, err := agent.ProcessActions(context.Background(), done, stamp)
	if err != nil || !facts.Rerun.Available || facts.Stop.Available || facts.Stop.Reason != ProcessStopEnded {
		t.Fatalf("ended own command: %+v %v", facts, err)
	}
	if _, err := agent.ProcessActions(context.Background(), done, stamp+1); !errors.Is(err, ErrNoSuchProcess) {
		t.Fatalf("another run's stamp was answered: %v", err)
	}
	if _, err := agent.RerunProcessAt(context.Background(), done, stamp+1); !errors.Is(err, ErrNoSuchProcess) {
		t.Fatalf("another run's stamp was run again: %v", err)
	}
	agent.keepProcessNotice(JobNotice{ID: 950, Kind: JobKindCommand, Command: "printf unknown", State: JobRunning,
		Started: time.UnixMilli(1_700_000_000_000), Dir: agent.config.Workspace})
	facts, err = agent.ProcessActions(context.Background(), 950, 1_700_000_000_000)
	if err != nil || facts.Unknown != ProcessUnknownOwnerLost || facts.Rerun.Reason != ProcessRerunUnknown || facts.Stop.Reason != ProcessStopUnknown {
		t.Fatalf("unknown record: %+v %v", facts, err)
	}
	var refused *ProcessActionRefused
	if _, err := agent.RerunProcessAt(context.Background(), 950, 1_700_000_000_000); !errors.As(err, &refused) || refused.Reason != ProcessRerunUnknown {
		t.Fatalf("unknown rerun: %v", err)
	}
	if _, err := agent.StopProcessAt(context.Background(), 950, 1_700_000_000_000); !errors.As(err, &refused) || refused.Reason != ProcessStopUnknown {
		t.Fatalf("unknown stop: %v", err)
	}
}

// A FOLDER THAT IS GONE, OR A WORKSPACE THAT MOVED, IS NEVER A FALLBACK.
func TestRerunRefusesAGoneFolderAndAMovedWorkspace(t *testing.T) {
	dir := t.TempDir()
	agent := conversationAgent(t, dir)
	gone := filepath.Join(t.TempDir(), "gone")
	agent.jobs.mu.Lock()
	agent.jobs.workspace = gone
	agent.jobs.mu.Unlock()
	agent.keepProcessNotice(JobNotice{ID: 960, Kind: JobKindCommand, Command: "printf gone", State: JobDone,
		Started: time.UnixMilli(1_700_000_000_000), Ended: time.Now(), Dir: gone})
	facts, err := agent.ProcessActions(context.Background(), 960, 1_700_000_000_000)
	if err != nil || facts.Rerun.Reason != ProcessRerunFolderGone {
		t.Fatalf("gone folder: %+v %v", facts, err)
	}
	if _, err := agent.jobs.startIn("printf moved", t.TempDir()); !errors.Is(err, errWorkspaceMoved) {
		t.Fatalf("a moved workspace started the command: %v", err)
	}
}

// A STAMPED STOP REACHES A TASK WORKER'S JOB AND SAYS WHAT IT PROVED; its
// facts say it may be stopped and never run again from here.
func TestStampedStopReachesATaskWorkersJob(t *testing.T) {
	dir := t.TempDir()
	conversation := conversationAgent(t, dir)
	worker := workerAgent(t, dir, 7)
	id := startJob(t, worker, "sleep 30")
	facts, err := conversation.ProcessActions(context.Background(), id, worker.jobs.find(id).info().started.UnixMilli())
	if err != nil || !facts.Stop.Available || facts.Rerun.Reason != ProcessRerunRunning || facts.Record.StartedBy != JobStartedByTask {
		t.Fatalf("worker job facts: %+v %v", facts, err)
	}
	got, err := conversation.StopProcessAt(context.Background(), id, facts.Record.Started.UnixMilli())
	if err != nil || got.Ended || got.Unproven {
		t.Fatalf("stamped stop: %+v %v", got, err)
	}
	if worker.jobs.find(id).running() {
		t.Fatal("the worker's job is still running")
	}
	after, err := conversation.ProcessActions(context.Background(), id, facts.Record.Started.UnixMilli())
	if err != nil || after.Stop.Available || after.Rerun.Available {
		t.Fatalf("stopped worker job facts: %+v %v", after, err)
	}
}

// STOP-ALL IS ONE SNAPSHOT AND ONE SHARED DEADLINE: three jobs, one of which
// ignores TERM, end within one bound rather than one bound each, and saved
// records are never touched.
func TestStopAllStopsOwnAndWorkerJobsUnderOneDeadline(t *testing.T) {
	dir := t.TempDir()
	conversation := conversationAgent(t, dir)
	worker := workerAgent(t, dir, 7)
	startJob(t, conversation, "sleep 30")
	startJob(t, conversation, "trap '' TERM; sleep 30")
	startJob(t, worker, "trap '' TERM; sleep 30")
	conversation.keepProcessNotice(JobNotice{ID: 970, Kind: JobKindCommand, Command: "printf saved", State: JobRunning,
		Started: time.Now(), Dir: conversation.config.Workspace})
	began := time.Now()
	receipt, err := conversation.StopConversationProcesses(context.Background())
	took := time.Since(began)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Eligible != 3 || receipt.Stopped != 3 || len(receipt.NotStopped) != 0 || receipt.Unenumerated != 0 || receipt.RemainingUnknown {
		t.Fatalf("receipt: %+v", receipt)
	}
	if took > ProcessStopAllWait+time.Second {
		t.Fatalf("stop-all took %v, more than its one shared deadline", took)
	}
	if facts, _ := conversation.ProcessActions(context.Background(), 970, 0); facts.Unknown == "" && facts.Record.ID != 0 {
		t.Fatalf("a saved record was touched: %+v", facts)
	}
	again, err := conversation.StopConversationProcesses(context.Background())
	if err != nil || again.Eligible != 0 || again.Stopped != 0 {
		t.Fatalf("nothing running answered: %+v %v", again, err)
	}
}

// A CANCELLED STOP-ALL STILL SIGNALS, ANSWERS AT ONCE, AND JOINS EVERY STOP:
// a job it could not see end is named, never counted stopped.
func TestStopAllUnderACancelledContextNamesWhatItCouldNotProve(t *testing.T) {
	dir := t.TempDir()
	conversation := conversationAgent(t, dir)
	startJob(t, conversation, "trap '' TERM; sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	began := time.Now()
	receipt, err := conversation.StopConversationProcesses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(began) > time.Second {
		t.Fatal("a cancelled stop-all waited out its graces")
	}
	if receipt.Eligible != 1 || receipt.Stopped+len(receipt.NotStopped) != 1 {
		t.Fatalf("receipt: %+v", receipt)
	}
}

// THE SCAN IS BOUNDED, and says so when it stopped short.
func TestRunningNewestStopsAtItsScanAndSaysSo(t *testing.T) {
	r := newJobRegistry(t.TempDir(), Place{}, nil)
	for i := 1; i <= 5; i++ {
		one := &job{id: i, done: make(chan struct{})}
		if i%2 == 0 {
			close(one.done)
		}
		r.jobs = append(r.jobs, one)
	}
	running, more := r.runningNewest(3)
	if !more || len(running) != 2 || running[0].id != 5 || running[1].id != 3 {
		t.Fatalf("scan of 3: more=%v running=%d", more, len(running))
	}
	running, more = r.runningNewest(5)
	if more || len(running) != 3 {
		t.Fatalf("whole scan: more=%v running=%d", more, len(running))
	}
}
