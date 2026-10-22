package tui3

// A CONVERSATION REOPENED REDRAWS THE JOBS IT RAN.
//
// The engine keeps its half of that promise: waking a checkpointed conversation
// replays every restored job row onto the standing lane as a
// [session.EventJobUpdate] (session's task_run.go). The surface used to throw
// them away — [app.taskEvent]'s switch had no case for the kind — so a developer
// who left a dev server and a build running yesterday came back to a column that
// said they never ran, and because the section is the ONLY door to a job's page
// there was no way left to read the log.
//
// These cases pin the routing, which is the only piece that was missing: the
// reducer (jobstate.go), the state and the drawing (jobsview.go) were all there
// and all tested.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// restoredJob is one job as the engine replays it out of a checkpoint: settled,
// with the log still on disk and the clock it ran against.
func restoredJob(id int, name, command string, state session.JobState) session.JobNotice {
	started := taskFixtureNow.Add(-40 * time.Minute)
	return session.JobNotice{
		ID:      id,
		Name:    name,
		Command: command,
		Kind:    session.JobKindCommand,
		State:   state,
		LogPath: "/tmp/codeaf/jobs/" + name + ".log",
		Started: started,
		Elapsed: 9 * time.Minute,
	}
}

// TestAReopenedConversationStillShowsItsJobs is the routing: a job replayed onto
// the standing lane reaches the window's own job state, and the column's section
// draws it.
func TestAReopenedConversationStillShowsItsJobs(t *testing.T) {
	a, _, _ := taskApp(t)

	jobs := []session.JobNotice{
		restoredJob(1, "the dev server", "npm run dev", session.JobStopped),
		restoredJob(2, "make check", "make check", session.JobDone),
		restoredJob(3, "the packed corpora", "make test-packed-manual", session.JobFailed),
	}
	for i := range jobs {
		job := jobs[i]
		a.taskEvent(session.Event{Kind: session.EventJobUpdate, Job: &job})
	}

	if len(a.jobs) != len(jobs) {
		t.Fatalf("the standing lane replayed %d jobs and the window kept %d — a reopened conversation drew none of the work it ran",
			len(jobs), len(a.jobs))
	}

	if rows := a.jobSection(48, 20); len(rows) != 0 {
		t.Fatal("restored completed jobs appeared in sidebar")
	}
	a.showProcesses("")
	rows, hits, _, _ := a.processListFrame(100, 24)
	text := strings.Join(rows, "\n")
	for _, job := range jobs {
		if !strings.Contains(text, job.Name) {
			t.Fatalf("on-demand process history lost %q: %s", job.Name, text)
		}
	}
	at := -1
	for i, hit := range hits {
		if hit == 1000 {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("restored process has no click target")
	}
	a.width, a.height = 100, 24
	if cmd := a.jobPagePress(8, at); cmd == nil || a.jobPage != 3 {
		t.Fatal("restored process log door did not open selected process")
	}

}

// TestAJobReplayedOnTheStandingLaneOpensItsPage closes the sentence row 1 of the
// jobs audit ends on: the section is the only door to a job's page, so a job the
// lane dropped was a log that could not be read from a resumed conversation —
// which is precisely when the log is the only thing left.
func TestAJobReplayedOnTheStandingLaneOpensItsPage(t *testing.T) {
	a, _, _ := taskApp(t)

	job := restoredJob(7, "the dev server", "npm run dev", session.JobStopped)
	a.taskEvent(session.Event{Kind: session.EventJobUpdate, Job: &job})

	if a.jobAt(7) == nil {
		t.Fatalf("a job replayed onto the standing lane could not be looked up by its number, so its page has no record to open")
	}
}
