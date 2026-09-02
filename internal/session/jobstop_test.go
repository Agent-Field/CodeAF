package session

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// A PERSON'S STOP ENDS THE PROCESS AND IS NOT A FAILURE. The registry's
// killRequested flag is what [jobStateWord] reads to publish [JobStopped]
// rather than [JobFailed], and Cancel must travel that path — a person who
// stopped a job should not be told it failed.
func TestAJobStoppedByThePersonSettlesAsStoppedAndNotAsFailed(t *testing.T) {
	agent, _ := jobsAgent(t)
	id := startJob(t, agent, "sleep 30")

	line, err := agent.Cancel(CancelJob + ":" + strconv.Itoa(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "stopped") || !strings.Contains(line, "log is kept") {
		t.Fatalf("the line a person is shown reads %q", line)
	}

	target := agent.jobs.find(id)
	if target == nil {
		t.Fatal("the job vanished from the registry")
	}
	if target.running() {
		t.Fatal("the sleeper is still running after Cancel")
	}
	info := target.info()
	if info.state != jobKilled {
		t.Fatalf("registry state is %v, want jobKilled", info.state)
	}
	if notice := noticeOf(info); notice.State != JobStopped {
		t.Fatalf("the published notice is %q, want stopped — a person who ended it did not fail it", notice.State)
	}

	// THE MODEL IS TOLD, because it did not ask. A `jobs kill` the model just
	// ran stays quiet (TestJobsKillEndsSleeperWithoutSelfReport); a person
	// pressing stop is news the model has not heard.
	if !notesContain(agent, fmt.Sprintf("job %d was stopped", id)) {
		t.Fatalf("the model was not told the person stopped the job: %v", sessionNotes(agent))
	}
}

// IT IS IDEMPOTENT THE WAY THE OTHER BRANCHES ARE. An id this session has
// never heard of is an error; a press on work that has already landed is a
// sentence saying so and nothing else. Neither panics.
func TestCancelOnAnUnknownJobAndOnOneAlreadyOverAnswersTheWayTheOtherBranchesDo(t *testing.T) {
	agent, _ := jobsAgent(t)

	line, err := agent.Cancel(CancelJob + ":99")
	if err == nil {
		t.Fatalf("an unknown job was stopped anyway: %q", line)
	}
	if !strings.Contains(err.Error(), "there is no job 99 in this session") {
		t.Fatalf("unknown job error reads %q", err)
	}

	id := startJob(t, agent, "true")
	waitExited(t, agent, id)

	line, err = agent.Cancel(CancelJob + ":" + strconv.Itoa(id))
	if err != nil {
		t.Fatalf("a settled job panicked or errored: %v", err)
	}
	if !strings.Contains(line, "already finished") || !strings.Contains(line, "nothing to stop") {
		t.Fatalf("stopping settled work reads %q", line)
	}

	// And a second press on one this person just ended is the same sentence.
	id = startJob(t, agent, "sleep 30")
	if _, err := agent.Cancel(CancelJob + ":" + strconv.Itoa(id)); err != nil {
		t.Fatal(err)
	}
	line, err = agent.Cancel(CancelJob + ":" + strconv.Itoa(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "already finished") {
		t.Fatalf("a second press on a stopped job reads %q", line)
	}
}

// THE PUBLISHED NOTICE CARRIES JobStopped, which is the state a surface draws
// from — not the registry's private flag, not a failed exit code.
func TestAStoppedJobsPublishedNoticeCarriesJobStopped(t *testing.T) {
	agent, _ := jobsAgent(t)
	id := startJob(t, agent, "sleep 30")

	if _, err := agent.Cancel(CancelJob + ":" + strconv.Itoa(id)); err != nil {
		t.Fatal(err)
	}
	notice := noticeOf(agent.jobs.find(id).info())
	if notice.State != JobStopped {
		t.Fatalf("published state is %q, want %q", notice.State, JobStopped)
	}
	if notice.ID != id {
		t.Fatalf("published id is %d, want the registry's own %d", notice.ID, id)
	}
	if !notice.Over() {
		t.Fatal("a stopped notice does not report as over")
	}
}

// AN INTERRUPT STILL LEAVES A BACKGROUND COMMAND RUNNING. Hands die with the
// turn they were part of; a job is a command the person asked to be left
// running, and Cancel is the door that ends one, not Interrupt.
func TestAnInterruptLeavesABackgroundCommandRunning(t *testing.T) {
	agent, _ := jobsAgent(t)
	id := startJob(t, agent, "sleep 30")

	agent.Interrupt()

	target := agent.jobs.find(id)
	if target == nil || !target.running() {
		t.Fatal("an interrupt killed a background command")
	}
	if notice := noticeOf(target.info()); notice.State != JobRunning {
		t.Fatalf("after interrupt the job is %q, want still running", notice.State)
	}
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("an interrupt that left the job running still told the model something: %v", queued)
	}
}
