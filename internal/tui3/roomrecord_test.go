package tui3

// A LANDED ROOM NEVER DRAWS AN EMPTY BODY.
//
// These pin the room's own honesty about a node with no transcript — the report
// the roster still holds, and a door's refusal never counting as a block. A
// background job is no longer a room (jobpage.go); the tests that used to pin
// that shape live there.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// AN ORDINARY NODE WHOSE TRANSCRIPT IS GONE STILL HAS ITS REPORT, and the room
// draws it. The record the roster holds outlives the file, so a person who
// opens the page after the session folder was deleted reads what the work came
// to instead of a blank.
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
	if !strings.Contains(text, roomGoneWord) {
		t.Fatalf("the room drew a report with no word on the missing transcript:\n%s", text)
	}
	if !strings.Contains(text, roomFinishedRefusal.what) {
		t.Fatalf("the landed foot went missing:\n%s", text)
	}
}

// THE REFUSAL DOES NOT SUPPRESS THE REASON. The regression that made the page
// blank was arithmetic and not vocabulary: the surface appended the engine's
// error as a block, and the "there is nothing here" line is drawn only when the
// block list is EMPTY (room.go). One machinery note was enough to make the room
// count itself as having a transcript.
func TestARefusedDoorDoesNotCountAsATranscript(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.watchErr = errors.New("no task 4 in this session")
	agent.journal = ""
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(4, "Port the parser", session.TaskDone,
		session.TaskNotice{Report: "Added the guard in parseRow."})})
	clickRailNode(t, a, 4)

	if room := a.room; room != nil {
		for _, e := range room.entries {
			if e.kind == entryNote && strings.Contains(e.text, "no task") {
				t.Fatalf("a door's refusal was appended to the room as a block: %q", e.text)
			}
		}
	}
	if text := roomText(a); strings.Contains(text, "no task 4 in this session") {
		t.Fatalf("the engine's refusal leaked into the room:\n%s", text)
	}
}
