package tui3

// A BACKGROUND JOB ON THE COLUMN.
//
// The engine publishes a job as a row of its own kind through the one roster
// door every other row comes through (session's jobrow.go), so this surface
// needs nothing new to draw one. What it needed was three refusals, and they are
// what these cases pin: no card in the conversation, no ✕ on the row, and — the
// one thing it grew — a line under the row naming the log, because a job has no
// room and the log is the only handle back to it.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// jobRow is one background job's row as the engine publishes it: the command as
// the name, and the job's number and log under it.
func jobRow(id uint64, title string, state session.TaskState) session.Event {
	return update(id, title, state, session.TaskNotice{
		Kind:   session.TaskKindJob,
		Report: "job 3 · log /tmp/aforge/jobs/3.log",
	})
}

// A RUNNING JOB IS A ROW, WHICH IS THE WHOLE OF THE OWNER'S LAW: work this
// conversation started shows on the right, whatever door started it. Before this
// the column stayed empty for the entire life of the job.
func TestARunningBackgroundJobIsARowOnTheColumn(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(jobRow(4, "npm run dev", session.TaskRunning))

	row, drawn := railRowFor(a, a.viewHeight(), "npm run dev")
	if !drawn {
		t.Fatalf("a running background job draws no row:\n%s", strings.Join(railText(a, a.viewHeight()), "\n"))
	}
	if strings.TrimSpace(row) == "" {
		t.Fatal("the job's row is blank")
	}
	// THE LOG IS UNDER IT. A job has no room to walk into, so a path this row
	// did not draw would be a path nowhere on this surface.
	text := strings.Join(railText(a, a.viewHeight()), "\n")
	if !strings.Contains(text, "job 3") {
		t.Fatalf("the row does not name the job it is:\n%s", text)
	}
}

// A JOB SETTLES ON THE COLUMN AND SAYS NOTHING IN THE CONVERSATION. A card is
// how work somebody handed over reports back, and a job has no report, no
// branch, no acceptance and no price — a card built from that would be a block
// of nothing pushed in every time a `sleep 5` came home.
func TestAFinishedBackgroundJobWritesNoCard(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(jobRow(4, "npm run dev", session.TaskRunning))
	before := len(a.entries)

	a.taskUpdate(jobRow(4, "npm run dev", session.TaskDone))
	if len(a.entries) != before {
		t.Fatalf("a job landing wrote %d entries into the conversation:\n%s",
			len(a.entries)-before, taskText(a))
	}
	// And the row is still there, settled: the column is the record.
	if _, drawn := railRowFor(a, a.viewHeight(), "npm run dev"); !drawn {
		t.Fatal("a settled job's row left the column")
	}
}

// THE SAME IS TRUE OF A JOB THAT DID NOT COME OFF. A failing command is news for
// the model, which is told on its own lane; it is not a card, because there is
// still nobody who wrote a report about it.
func TestAFailedBackgroundJobWritesNoCard(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(jobRow(4, "make build", session.TaskRunning))
	before := len(a.entries)

	a.taskUpdate(jobRow(4, "make build", session.TaskFailed))
	if len(a.entries) != before {
		t.Fatalf("a failed job wrote %d entries into the conversation:\n%s",
			len(a.entries)-before, taskText(a))
	}
}

// NO ✕ ON A JOB'S ROW. The id it carries is the roster's and names nothing the
// engine's stop door can find, so a key wired to it would raise a card whose one
// possible answer was a refusal. A capability that cannot work is absent.
func TestAJobRowOffersNoStop(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(jobRow(4, "npm run dev", session.TaskRunning))
	a.railHold, a.railWhere = true, railSpot{id: 4}

	if target := a.stopHere(); !target.empty() {
		t.Fatalf("a job's row offers a stop addressed to %q", target.id)
	}
}

// A TASK'S ROW STILL OFFERS ONE, which is what says the refusal above is about
// the KIND of work and not about the roster having lost its stop.
func TestATaskRowStillOffersAStopBesideAJob(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(jobRow(4, "npm run dev", session.TaskRunning))
	a.taskUpdate(update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{}))
	a.railHold, a.railWhere = true, railSpot{id: 7}

	if target := a.stopHere(); target.empty() {
		t.Fatal("a running task's row stopped offering a stop")
	}
}

// ZERO JOBS ADDS NOTHING ANYWHERE — the emptiness law, read off the column
// itself: a session that has started none draws exactly what it drew before jobs
// had rows at all.
func TestASessionWithNoJobsDrawsNoJobRows(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{}))
	withTaskOnly := strings.Join(railText(a, a.viewHeight()), "\n")

	if strings.Contains(withTaskOnly, "job ") {
		t.Fatalf("a session with no jobs draws a job line:\n%s", withTaskOnly)
	}
	for _, word := range []string{"0 jobs", "no jobs"} {
		if strings.Contains(strings.ToLower(withTaskOnly), word) {
			t.Fatalf("the column announces the absence of jobs (%q):\n%s", word, withTaskOnly)
		}
	}
}
