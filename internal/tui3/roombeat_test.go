package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE PULSE'S ONE READING, DRAWN AND WITHDRAWN (issue #837).
//
// A node's beat sidecar is the only witness that knows a model call went out
// and has not come back: the page's own rows cannot say it, because a request
// still streaming has written no entry yet. These tests feed the open page a
// beat row each way round and read the header the same draw reads it — the
// live segment is the chat foot's own sentence, and the call's clock stands in
// the task's own clock's place only while the call is open.

// beatRoom is a running node's page with the pulse held the way a reading
// leaves it: the row on the beat side, and the reading flag that says it was
// read rather than merely empty.
func beatRoom(t *testing.T, row session.TaskBeatRow) *app {
	t.Helper()
	a := headRoom(t)
	a.room.beat, a.room.beatRead = row, true
	a.room.dirty = true
	return a
}

// anOpenCall is a beat row whose request has gone out and not come back.
func anOpenCall(started time.Time) session.TaskBeatRow {
	return session.TaskBeatRow{
		Started:         started.Add(-time.Hour),
		RequestStarted:  started,
		RequestFinished: started.Add(-time.Minute),
	}
}

// TestTheHeaderDrawsTheOpenCallWhileThePulseSaysOneIsInFlight — request_started
// newer than request_finished is the sidecar's whole meaning of "inside a
// call", and while it holds the header's live segment is THE CALL's clock: the
// call's own elapsed, spelled the way the chat foot spells it, in place of the
// task's own age and its `still working`.
func TestTheHeaderDrawsTheOpenCallWhileThePulseSaysOneIsInFlight(t *testing.T) {
	opened := time.Now().Add(-11 * time.Minute)
	a := beatRoom(t, anOpenCall(opened))
	line := plain(a.roomFactsLine(160))
	for _, want := range []string{"11m", "working"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the header does not say %q while a call is open: %q", want, line)
		}
	}
	// AND NOTHING OF THE TASK'S OWN CLOCK LEAKS IN BESIDE IT. The whole defect
	// was a header saying only `working · 10m 21s` — the task's age — about a
	// call that was ten minutes in. The open call's own figure is what replaced
	// it, and a header carrying both is the same page saying two clocks about
	// one silence.
	if strings.Contains(line, "10m") {
		t.Fatalf("the header spelled the task's own age beside the call's clock: %q", line)
	}
	// AND THE VAGUEST SENTENCE IS GONE. `still working` is the ladder's answer
	// when nothing better is known; a call's own clock is better and wins.
	if strings.Contains(line, stillWorkingWord) {
		t.Fatalf("the header said `still working` over a call it could time: %q", line)
	}
}

// TestTheCallSegmentSpellsItselfExactlyAsTheChatFootDoes — no new words. The
// rate is the foot's own `N tok/s`, built from the same desk the foot reads, and
// the clock is the live count-up every moving figure on this surface uses. A
// header that invented a second spelling would be two answers to "how fast".
func TestTheCallSegmentSpellsItselfExactlyAsTheChatFootDoes(t *testing.T) {
	opened := time.Now().Add(-95 * time.Second)
	a := beatRoom(t, anOpenCall(opened))
	if got := a.roomOpenCallWord(a.roomNode()); !strings.Contains(got, "1m 35s") {
		t.Fatalf("the open call's clock is not the live count-up: %q", got)
	}
	// THE RATE RIDES ONLY WHILE IT IS BEING MEASURED, which is the foot's own
	// law: a rate nobody is producing is nothing, never `0 tok/s`.
	if got := a.roomOpenCallWord(a.roomNode()); strings.Contains(got, "0 tok/s") {
		t.Fatalf("an unmeasured rate became a figure: %q", got)
	}
}

// TestTheHeaderGoesBackToTheTaskClockWhenTheCallComesBack — the reverse order,
// request_finished newer, is the sidecar saying no call is open, and the live
// segment reverts to the task's own elapsed time and its ordinary ladder.
func TestTheHeaderGoesBackToTheTaskClockWhenTheCallComesBack(t *testing.T) {
	finished := time.Now().Add(-time.Minute)
	row := anOpenCall(finished)
	row.RequestFinished = finished.Add(time.Second)
	a := beatRoom(t, row)
	line := plain(a.roomFactsLine(160))
	if strings.Contains(line, "11m") || strings.Contains(line, "10m") {
		t.Fatalf("a closed call's clock stayed on the header: %q", line)
	}
	if !strings.Contains(line, "3m") {
		t.Fatalf("the header did not go back to the task's own clock: %q", line)
	}
}

// TestAPageWithNoPulseDrawsNoCall — an unread beat is the honest unknown, and
// unknown renders as nothing: the emptiness law, not a failure to draw.
func TestAPageWithNoPulseDrawsNoCall(t *testing.T) {
	a := headRoom(t)
	a.room.beatRead = false
	a.room.dirty = true
	line := plain(a.roomFactsLine(160))
	if strings.Contains(line, "still working") && strings.Contains(line, "bash") &&
		strings.Contains(line, "0 tok/s") {
		t.Fatalf("an unknown pulse drew a call: %q", line)
	}
	if ansi.StringWidth(line) != 160 {
		t.Fatalf("the header changed its row budget: %q", line)
	}
}
