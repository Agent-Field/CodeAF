package tui3

// A BACKGROUND JOB IS NOT ON THE ROSTER, AND SAYS NOTHING IN THE CONVERSATION.
//
// These cases used to pin the opposite. A job was published through the ROSTER
// door as a [session.TaskNotice] of a `job` kind, drawn as a row among the task
// families with its log path underneath, and this file pinned the three refusals
// that arrangement then needed: no card, no ✕, and a log line the row grew
// because a job had nowhere else to carry one.
//
// The arrangement is gone. A job publishes itself (session's jobnotice.go), is
// kept apart from the nodes (jobstate.go) and is drawn in a section of its own
// (jobsview.go), where it has a name, a clock and a page with a stop on it.
//
// SO WHAT IS LEFT HERE IS WHAT THE OTHER FILES CANNOT SEE. jobsview_test.go
// tests a pure function and never holds a window, so it cannot say whether a job
// reached the roster or wrote into the conversation. Those two are facts about
// this window's own wiring, and they are the two this file keeps.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// runningJob is one background job as the engine publishes it now: its own
// number, the name the namer gave it, and the command underneath.
func runningJob(id int, name, command string) session.JobNotice {
	return session.JobNotice{
		ID:      id,
		Name:    name,
		Command: command,
		Kind:    session.JobKindCommand,
		State:   session.JobRunning,
		LogPath: "/tmp/aforge/jobs/3.log",
		Started: time.Now().Add(-90 * time.Second),
	}
}

// A JOB IS NEVER A NODE. The whole cost of the old arrangement was that it was
// filed among the tasks and then excluded again, one surface at a time — so the
// pin is at the door: a job that arrives leaves the task side exactly as empty
// as it found it.
func TestABackgroundJobNeverBecomesATaskNode(t *testing.T) {
	a, _, _ := taskApp(t)
	a.jobUpdate(runningJob(3, "run the dev server", "npm run dev"))

	if len(a.tasks) != 0 || len(a.taskOrder) != 0 {
		t.Fatalf("a background job was filed as a task node: %d in tasks, %d in order",
			len(a.tasks), len(a.taskOrder))
	}
	if len(a.jobs) != 1 {
		t.Fatalf("the job was not kept on the job side: %d held", len(a.jobs))
	}
}

// AND THE OLD DOOR IS SHUT. The engine still publishes a job's
// [session.TaskNotice] while the two contracts overlap, and a window that filed
// it would draw the job twice — once among the families and once in its own
// section.
func TestAJobArrivingOnTheRosterDoorIsIgnored(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(4, "npm run dev", session.TaskRunning, session.TaskNotice{
		Kind: session.TaskKindJob,
	}))

	if len(a.tasks) != 0 || len(a.taskOrder) != 0 {
		t.Fatalf("a job on the roster door was filed as a node: %d in tasks, %d in order",
			len(a.tasks), len(a.taskOrder))
	}
}

// A JOB SETTLES AND SAYS NOTHING IN THE CONVERSATION. A card is how work
// somebody handed over reports back, and a job has no report, no branch, no
// acceptance and no price — a card built from that would be a block of nothing
// pushed in every time a `sleep 5` came home.
func TestAFinishedBackgroundJobWritesNoCard(t *testing.T) {
	a, _, _ := taskApp(t)
	job := runningJob(3, "run the dev server", "npm run dev")
	a.jobUpdate(job)
	before := len(a.entries)

	job.State, job.ExitCode = session.JobDone, 0
	a.jobUpdate(job)
	if len(a.entries) != before {
		t.Fatalf("a job landing wrote %d entries into the conversation:\n%s",
			len(a.entries)-before, taskText(a))
	}

	// THE SAME IS TRUE OF ONE THAT DID NOT COME OFF. A failing command is news
	// for the model, told on its own lane; it is not a card, because there is
	// still nobody who wrote a report about it.
	job.State, job.ExitCode = session.JobFailed, 1
	a.jobUpdate(job)
	if len(a.entries) != before {
		t.Fatalf("a failed job wrote %d entries into the conversation:\n%s",
			len(a.entries)-before, taskText(a))
	}
}

// A TASK'S ROW STILL OFFERS A STOP WITH A JOB BESIDE IT. The job clause is out
// of [app.stopTaskTarget] now that `job:3` is a real cancel id (session's
// jobstop.go), and this is what says removing it left the roster's own stop
// exactly where it was.
func TestATaskRowStillOffersAStopBesideAJob(t *testing.T) {
	a, _, _ := taskApp(t)
	a.jobUpdate(runningJob(3, "run the dev server", "npm run dev"))
	a.taskUpdate(update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{}))
	a.railHold, a.railWhere = true, railSpot{id: 7}

	if target := a.stopHere(); target.empty() {
		t.Fatal("a running task's row stopped offering a stop")
	}
}

// ZERO JOBS ADDS NOTHING ANYWHERE — the emptiness law read off the whole column
// rather than off the section's own function: a session that has started none
// draws exactly what it drew before jobs had a section at all.
func TestASessionWithNoJobsDrawsNothingAboutJobs(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{}))
	column := strings.ToLower(strings.Join(railText(a, a.viewHeight()), "\n"))

	for _, word := range []string{"job", "0 jobs", "no jobs"} {
		if strings.Contains(column, word) {
			t.Fatalf("a session with no jobs says %q on the column:\n%s", word, column)
		}
	}
}
