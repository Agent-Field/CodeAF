package tui3

// A ROW ON THE ROSTER THAT IS NOT A NODE STILL OPENS A PAGE WITH SOMETHING ON IT.
//
// A background job is a roster row and NOT a node in the task graph
// (internal/session's jobrow.go: "it registers and it does not admit"), so every
// room door in the engine refuses its id — the row's number was minted from the
// graph's sequence and no node was ever admitted under it. The rail is a flat
// list of rows and its enter key opens the room of whatever is under it, so the
// refusal reaches the page. These tests pin what the page does with it: it draws
// what the row already knows, and it never puts the engine's own sentence in
// front of a person.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// jobRoomApp is a roster carrying one settled BACKGROUND JOB — the row the
// engine publishes for `bash background:true` — and doors that refuse its id
// exactly as the real ones do.
//
// The refusal is the engine's own string because that is the whole point of the
// test: the surface has to survive the sentence without repeating it.
func jobRoomApp(t *testing.T, report string) (*app, *roomFake) {
	t.Helper()
	a, agent, _ := roomApp(t)
	agent.watchErr = errors.New("no task 4 in this session")
	// A job has no transcript at all — no journal is ever minted for one — which
	// is the case the room has to be honest about rather than blank about.
	agent.journal = ""
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "video", session.TaskDone,
		session.TaskNotice{
			Kind:    session.TaskKindJob,
			Report:  report,
			Elapsed: 4*time.Minute + 30*time.Second,
		})})
	return a, agent
}

// THE BUG THE OWNER SAW: a correct header over an empty body. The room drew its
// title, its state and its elapsed from the row the surface already held, and
// then everything under the header was the engine's refusal and the foot.
//
// The page must never be that again: whatever else is true, a landed room says
// what it knows.
func TestAJobsRoomDrawsWhatTheRowKnowsInsteadOfAVoid(t *testing.T) {
	const log = "job 4 · log /tmp/lab/video.log"
	a, _ := jobRoomApp(t, log)
	clickRailNode(t, a, 4)

	text := roomText(a)
	// THE MACHINERY NEVER REACHES THE PAGE. "no task 4 in this session" is the
	// engine telling a caller its id is not in the graph; it is not a sentence
	// about anything the person did, and it reads as the surface having lost the
	// task they are looking at.
	if strings.Contains(text, "no task 4 in this session") {
		t.Fatalf("the engine's refusal leaked into the room:\n%s", text)
	}
	// AND THE ROW'S OWN FACT IS DRAWN. For a job that is the log path, which is
	// the entire record of what it did (session's jobRowLead).
	if !strings.Contains(text, log) {
		t.Fatalf("the room drew nothing of what the row already knew:\n%s", text)
	}
	if !strings.Contains(text, roomFinishedWord) {
		t.Fatalf("the landed foot went missing:\n%s", text)
	}
}

// A ROOM IS NEVER AN EMPTY PAGE. This is the law the bug broke, stated over the
// worst case there is: no lane, no journal, no node, and nothing on the record
// but the row itself.
func TestALandedRoomNeverDrawsAnEmptyBody(t *testing.T) {
	a, _ := jobRoomApp(t, "")
	clickRailNode(t, a, 4)

	text := roomText(a)
	if strings.Contains(text, "no task 4 in this session") {
		t.Fatalf("the engine's refusal leaked into the room:\n%s", text)
	}
	// With not even a log path to draw, what is left is the honest sentence about
	// this kind of work — a job never wrote a transcript, so the line about a
	// transcript being gone would be the wrong half of the truth. What must never
	// happen is the foot alone over a blank.
	if !strings.Contains(text, roomJobLogWord) {
		t.Fatalf("a room with nothing at all to show drew no reason for the blank:\n%s", text)
	}
	if strings.Contains(text, roomGoneWord) {
		t.Fatalf("a job's room claims a transcript went missing; it never had one:\n%s", text)
	}
	if !strings.Contains(text, roomFinishedWord) {
		t.Fatalf("the landed foot went missing:\n%s", text)
	}
}

// AN ORDINARY NODE WHOSE TRANSCRIPT IS GONE STILL HAS ITS REPORT, and the room
// draws it. This is the same law over the other kind of row: the record the
// roster holds outlives the file, so a person who opens the page after the
// session folder was deleted reads what the work came to instead of a blank.
func TestALandedRoomFallsBackToTheReportOnTheRecord(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.journal = ""
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{
			Report:  "Added the guard in parseRow and covered it with a test.",
			Elapsed: 2 * time.Minute,
		})})
	close(agent.lane(7))
	clickRailNode(t, a, 7)

	text := roomText(a)
	if !strings.Contains(text, "Added the guard in parseRow") {
		t.Fatalf("the room drew none of the report the roster still holds:\n%s", text)
	}
	// The report is what happened; this line is why there is nothing under it.
	// An ordinary node DID write a transcript, so this is the right half of the
	// truth for it — unlike a job.
	if !strings.Contains(text, roomGoneWord) {
		t.Fatalf("the room drew a report with no word on the missing transcript:\n%s", text)
	}
	if !strings.Contains(text, roomFinishedWord) {
		t.Fatalf("the landed foot went missing:\n%s", text)
	}
}

// THE REFUSAL DOES NOT SUPPRESS THE REASON. The regression that made the page
// blank was arithmetic and not vocabulary: the surface appended the engine's
// error as a block, and the "there is nothing here" line is drawn only when the
// block list is EMPTY (room.go). One machinery note was enough to make the room
// count itself as having a transcript.
func TestARefusedDoorDoesNotCountAsATranscript(t *testing.T) {
	a, _ := jobRoomApp(t, "")
	clickRailNode(t, a, 4)

	if room := a.room; room != nil {
		for _, e := range room.entries {
			if e.kind == entryNote && strings.Contains(e.text, "no task") {
				t.Fatalf("a door's refusal was appended to the room as a block: %q", e.text)
			}
		}
	}
}
