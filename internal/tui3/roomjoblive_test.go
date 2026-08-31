package tui3

// ── A JOB'S PAGE IS ALIVE FOR AS LONG AS THE JOB IS ─────────────────────────
//
// THE BUG A PERSON PHOTOGRAPHED: a room thirty-two seconds into a running job,
// its header counting up, and under it three dead rows — the log path, one line
// saying a job keeps a log, and `task finished — esc to return`. The foot came
// from [app.openRoom] reading the engine's refusal of a job's id as a landing,
// which is the ORDINARY answer for a job (session's jobrow.go), so the landed
// vocabulary was spoken over every running job there has ever been.
//
// These pin both halves of the fix: what the page SAYS follows the roster's row,
// and what the page SHOWS is the log, read on the room's own beat.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// jobLogFile is one background job's log, on this disk, with lines in it.
func jobLogFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "video.log")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the job log: %v", err)
	}
	return path
}

// appendJobLog is the process writing another line while the page is open.
func appendJobLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("opening the job log: %v", err)
	}
	defer file.Close()
	for _, line := range lines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatalf("appending to the job log: %v", err)
		}
	}
}

// liveJobRoom is a roster carrying ONE BACKGROUND JOB in the state named, with
// doors that refuse its id exactly as the real ones do — the whole point being
// that the refusal is the healthy case and must not be read as a landing.
func liveJobRoom(t *testing.T, state session.TaskState, log string) *app {
	t.Helper()
	a, agent, _ := roomApp(t)
	agent.watchErr = errors.New("no task 4 in this session")
	agent.journal = ""
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "video", state,
		session.TaskNotice{Kind: session.TaskKindJob, Report: "job 4 · log " + log})})
	return a
}

// A RUNNING JOB'S PAGE NEVER SAYS THE WORK FINISHED, and says the true thing
// instead. This is the screenshot, tested.
func TestARunningJobsRoomSaysTheLogIsGrowingAndNeverThatItFinished(t *testing.T) {
	log := jobLogFile(t, "ffmpeg started · 1200 frames")
	a := liveJobRoom(t, session.TaskRunning, log)
	clickRailNode(t, a, 4)

	text := roomText(a)
	if strings.Contains(text, roomFinishedWord) {
		t.Fatalf("a running job's page says the task finished:\n%s", text)
	}
	if !strings.Contains(text, roomJobRunningWord) {
		t.Fatalf("a running job's page draws no foot of its own:\n%s", text)
	}
	// AND THE BOX SAYS THE SAME THING, because the placeholder read the same
	// field the foot did and told the same lie: `task finished` in front of a
	// prompt somebody was about to type into.
	rows := a.roomSteerLaneRows([]string{a.pal.dim(prompt)}, 120)
	if line := plain(rows[0]); strings.Contains(line, roomFinishedWord) {
		t.Fatalf("the box under a running job says the task finished: %q", line)
	}
	// The engine's own refusal never reaches the page, running or landed.
	if strings.Contains(text, "no task 4 in this session") {
		t.Fatalf("the engine's refusal leaked into the room:\n%s", text)
	}
}

// AND THE PAGE IS THE LOG. It is read on the way in and again on the room's
// beat, newest at the bottom, under the path the row already carries — and the
// line about a job keeping a log comes off, because that line answers "why is
// there nothing here" and there is something here.
func TestARunningJobsRoomTailsItsLogAndGrowsWithIt(t *testing.T) {
	log := jobLogFile(t, "ffmpeg started · 1200 frames")
	a := liveJobRoom(t, session.TaskRunning, log)
	clickRailNode(t, a, 4)

	text := roomText(a)
	if !strings.Contains(text, "ffmpeg started · 1200 frames") {
		t.Fatalf("the page did not read the job's log:\n%s", text)
	}
	if !strings.Contains(text, "job 4 · log "+log) {
		t.Fatalf("the page dropped the path the row carries:\n%s", text)
	}
	if strings.Contains(text, roomJobLogWord) {
		t.Fatalf("a page with the log on it still says there is nothing to show:\n%s", text)
	}

	// THE PROCESS WRITES ANOTHER LINE and the next beat brings it, without the
	// page being reopened and without the first line going anywhere.
	appendJobLog(t, log, "frame 640 · 22 fps")
	drive(t, a, farRoomTickMsg{gen: a.room.gen})
	text = roomText(a)
	for _, want := range []string{"ffmpeg started · 1200 frames", "frame 640 · 22 fps"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the tail did not grow with the log — %q is missing:\n%s", want, text)
		}
	}
	// Newest at the bottom, in the order the process wrote them.
	if strings.Index(text, "ffmpeg started") > strings.Index(text, "frame 640") {
		t.Fatalf("the log is drawn newest-first:\n%s", text)
	}
}

// THE ROW IS WHAT ENDS THE PAGE, and the ending is complete: a process writes
// its last lines and exits, the row lands on the exit, so one reading follows
// the landing.
func TestAJobsRoomFlipsToFinishedAndKeepsTheFinalTail(t *testing.T) {
	log := jobLogFile(t, "ffmpeg started · 1200 frames")
	a := liveJobRoom(t, session.TaskRunning, log)
	clickRailNode(t, a, 4)

	appendJobLog(t, log, "done · wrote out.mp4")
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "video", session.TaskDone,
		session.TaskNotice{Kind: session.TaskKindJob, Report: "job 4 · log " + log})})
	drive(t, a, farRoomTickMsg{gen: a.room.gen})

	text := roomText(a)
	if !strings.Contains(text, roomFinishedWord) {
		t.Fatalf("a landed job's page never took the finished foot:\n%s", text)
	}
	if strings.Contains(text, roomJobRunningWord) {
		t.Fatalf("a landed job's page still says the log is growing:\n%s", text)
	}
	if !strings.Contains(text, "done · wrote out.mp4") {
		t.Fatalf("the last lines the process wrote never reached the page:\n%s", text)
	}
}

// AN EMPTY OR UNREADABLE LOG DRAWS THE LINE IT ALWAYS DREW AND NOTHING ELSE.
// That is the emptiness law over a reading: a job that has not written its first
// line yet is the ordinary case for the first second of every one of them, and a
// page that answered it with a failure would be shouting about healthy work.
func TestAJobsRoomWithNothingInItsLogDrawsNoFailure(t *testing.T) {
	a := liveJobRoom(t, session.TaskRunning, filepath.Join(t.TempDir(), "never-written.log"))
	clickRailNode(t, a, 4)

	text := roomText(a)
	if !strings.Contains(text, roomJobLogWord) {
		t.Fatalf("a job with an unwritten log drew no reason for the blank:\n%s", text)
	}
	for _, banned := range []string{"no such file", "error", "cannot", "failed"} {
		if strings.Contains(strings.ToLower(text), banned) {
			t.Fatalf("the page shouted %q about a job that is running fine:\n%s", banned, text)
		}
	}
	if !strings.Contains(text, roomJobRunningWord) {
		t.Fatalf("a running job with no log yet lost its foot:\n%s", text)
	}
}

// AND A HOSTED JOB'S LOG IS NOT ON THIS DISK. The path on the row belongs to the
// engine's machine; opening it here finds either nothing or a stranger's file,
// which is the exact fault the far card's reader was written to avoid.
func TestAHostedJobsRoomNeverReadsThisDisk(t *testing.T) {
	log := jobLogFile(t, "THIS LAPTOP'S FILE")
	a := liveJobRoom(t, session.TaskRunning, log)
	a.host = "box"
	clickRailNode(t, a, 4)

	if text := roomText(a); strings.Contains(text, "THIS LAPTOP'S FILE") {
		t.Fatalf("a hosted job's page read a log on this disk:\n%s", text)
	}
}

// SOMEBODY ELSE'S OUTPUT IS MADE SAFE BEFORE IT IS DRAWN. A job's log is
// whatever a compiler or a server wrote to a pipe, and an escape sequence drawn
// into the frame repaints rows this surface owns.
func TestAJobsLogLineIsStrippedOfWhatWouldRepaintTheFrame(t *testing.T) {
	got := jobLogLine("\x1b[31mbuild\x1b[0m\tfailed\x07 ")
	if got != "build    failed" {
		t.Fatalf("a raw log line drew as %q", got)
	}
}
