package tui3

// A landed room with nothing in it says so. The engine keeps a task's
// transcript across restarts (internal/session's task_store.go), so the page
// that would once have been blank after a resume replays; the line below is for
// the file that is genuinely gone, and it must never appear over a transcript.

import (
	"strings"
	"testing"
)

// A ROOM WITH NO LANE AND NO ENTRIES SAYS THE TRANSCRIPT IS GONE, in one dim
// line above the foot, so a deleted file reads as a fact and not as a blank
// page.
func TestALandedRoomWithNothingToReplaySaysTheTranscriptIsGone(t *testing.T) {
	a, agent, _ := roomApp(t)
	close(agent.lane(7))
	clickRail(t, a, 0)

	text := roomText(a)
	if !strings.Contains(text, roomGoneWord) {
		t.Fatalf("an empty landed room draws no reason for the blank:\n%s", text)
	}
	if !strings.Contains(text, roomFinishedWord) {
		t.Fatalf("the foot went missing under the empty room:\n%s", text)
	}
	if strings.Index(text, roomGoneWord) > strings.Index(text, roomFinishedWord) {
		t.Fatalf("the reason is drawn under the foot, want it above:\n%s", text)
	}
}

// A ROOM WITH ENTRIES CHANGES NOTHING: the transcript is there, and a line
// saying it is not would be a lie over the top of it.
func TestALandedRoomWithATranscriptDoesNotSayItIsGone(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"Added the guard in parseRow."}`,
	)
	close(agent.lane(7))
	clickRail(t, a, 0)

	text := roomText(a)
	if strings.Contains(text, roomGoneWord) {
		t.Fatalf("a room with a transcript claims the transcript is gone:\n%s", text)
	}
	if !strings.Contains(text, "Added the guard in parseRow.") {
		t.Fatalf("the transcript did not replay:\n%s", text)
	}
	if !strings.Contains(text, roomFinishedWord) {
		t.Fatalf("a landed room lost its foot:\n%s", text)
	}
}
