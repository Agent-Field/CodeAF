package tui3

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// errRoomRead is a far machine refusing a journal read: the shape that must
// reach the page as a failure and never as an arrival still on its way.
var errRoomRead = errors.New("session: no journal for task 9")

// A TASK OPENED THE SECOND IT STARTS IS THE PAGE THIS FILE IS ABOUT. The defect
// it pins is one screenshot: a header counting `working 11s` over a whole screen
// of nothing but `loading this task's conversation…` — the surface talking about
// its own plumbing to somebody who opened the page to see the work.
//
// The four things asserted here are the four halves of the answer: the page says
// what the person asked for before any journal is read, it says what the engine
// last reported the work is doing, `loading` is a claim about a read that really
// is on the wire, and a read that FAILED is never spelled as one that is still
// coming.

// startingRoomLab is a hosted surface standing on a RUNNING far node — the shape
// a just-started task has: a row, no transcript URI yet, nothing journaled.
func startingRoomLab(t *testing.T) (*app, session.TaskIndexEntry) {
	t.Helper()
	a := hostedPlaceLab(t)
	entry := farCardEntry(time.Now())
	entry.Status, entry.TranscriptURI, entry.EndedAt = string(session.TaskRunning), "", time.Time{}
	a.adoptFarTaskRows([]session.TaskIndexEntry{entry})
	return a, entry
}

// The instruction is on the page from the first frame, because the contract is
// frozen at admission and this window has been holding it since the proposal.
func TestAStartingRoomDrawsTheInstructionBeforeAnyJournalArrives(t *testing.T) {
	a, entry := startingRoomLab(t)
	node := a.tasks[9]
	if node == nil {
		t.Fatal("the far row did not become a roster node")
	}
	node.brief = "Widen the import pipe so the nightly run stops timing out."
	a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) {
		return session.TaskRecord{}, nil
	}
	a.openRoomFor(9, entry.Title)
	if a.room == nil {
		t.Fatal("the running far row opened no room")
	}
	text := farRoomText(a)
	if !strings.Contains(text, "stops timing out") {
		t.Fatalf("the starting page did not draw the instruction:\n%s", text)
	}
	// AND THE LOADING LINE IS STILL THERE, because a read really is on the wire.
	// It is the WHOLE of the old page that was the defect, not the sentence.
	if !strings.Contains(text, roomLoadingWord) {
		t.Fatalf("a page with a read on the wire did not say so:\n%s", text)
	}
}

// What the engine last said the work is DOING goes under the instruction, and it
// is the sentence the header had no room for — never the header's own word.
func TestAStartingRoomSaysWhatTheWorkIsDoingWithoutRepeatingTheHeader(t *testing.T) {
	a, entry := startingRoomLab(t)
	node := a.tasks[9]
	node.brief = "Widen the import pipe."
	node.waiting = "rate limited"
	a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) {
		return session.TaskRecord{}, nil
	}
	a.openRoomFor(9, entry.Title)
	text := farRoomText(a)
	if !strings.Contains(text, "rate limited") {
		t.Fatalf("the page did not say why the work is not moving:\n%s", text)
	}
	// The empty page uses the roster's complete explanation, including its
	// state, because some reasons are noun fragments such as "its parts".
	if !strings.Contains(text, a.taskStatus(node).RowWord()) {
		t.Fatalf("the body omitted the shared waiting explanation:\n%s", text)
	}
	// AND A LANDED PAGE SAYS NONE OF IT. These are reports of right now.
	a.room.done = true
	if say := a.roomStartingSay(node); say != "" {
		t.Fatalf("a finished page claimed live activity: %q", say)
	}
}

// `loading` IS A CLAIM ABOUT THE WIRE. A page with no reader behind it never
// makes it — this is the stuck sentence the defect report showed, and the room
// falls through to the honest line instead.
func TestARoomWithNoReaderNeverSaysItIsLoading(t *testing.T) {
	a, entry := startingRoomLab(t)
	node := a.tasks[9]
	node.brief = "Widen the import pipe."
	// No by-id reader, and the running row has no transcript URI: there is
	// nothing [app.readRoomRecord] can ask, so nothing is coming.
	a.farRoomRecord = nil
	a.openFarRoom(node, entry.Title)
	if a.room == nil {
		t.Fatal("the door opened no room")
	}
	if a.room.loading {
		t.Fatal("a page with no read armed still called itself loading")
	}
	if cmd := a.takeRoomPump(); cmd != nil {
		t.Fatal("a page with no reader parked a command anyway")
	}
	text := farRoomText(a)
	if strings.Contains(text, roomLoadingWord) {
		t.Fatalf("the page promised an arrival nothing was going to make:\n%s", text)
	}
	if !strings.Contains(text, roomYetWord) {
		t.Fatalf("the page did not say why it is empty:\n%s", text)
	}
	if !strings.Contains(text, "Widen the import pipe") {
		t.Fatalf("the page lost the instruction:\n%s", text)
	}
}

// A READER THAT GOES AWAY BETWEEN BEATS STOPS THE WORD TOO. The same stuck
// sentence, reached one tick later instead of at the door.
func TestABeatWithNothingToAskStopsSayingItIsLoading(t *testing.T) {
	a, entry := startingRoomLab(t)
	a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) {
		return session.TaskRecord{}, nil
	}
	a.openRoomFor(9, entry.Title)
	if !a.room.loading {
		t.Fatal("the page did not open with its read on the wire")
	}
	_ = a.takeRoomPump()
	// The reader is given back while the first read is still out.
	a.farRoomRecord, a.farRecord = nil, nil
	if cmd := a.farRoomPoll(a.room.gen); cmd != nil {
		t.Fatal("a beat with nothing to ask armed a read anyway")
	}
	if a.room.loading {
		t.Fatal("the page kept claiming a read was on the way")
	}
	if text := farRoomText(a); strings.Contains(text, roomLoadingWord) {
		t.Fatalf("the loading line outlived its reader:\n%s", text)
	}
}

// A FAILURE IS NOT A LOADING PAGE, and the page keeps everything it knew.
func TestAFailedReadSaysSoAndKeepsTheInstruction(t *testing.T) {
	a, entry := startingRoomLab(t)
	a.tasks[9].brief = "Widen the import pipe."
	var reads atomic.Int64
	a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) {
		reads.Add(1)
		return session.TaskRecord{}, errRoomRead
	}
	a.openRoomFor(9, entry.Title)
	msg := a.takeRoomPump()().(roomRecordMsg)
	a.farRoomRead(msg)
	if a.room.loading {
		t.Fatal("a page whose read failed still called itself loading")
	}
	text := farRoomText(a)
	if !strings.Contains(text, roomReadFailedWord) {
		t.Fatalf("the failed read said nothing:\n%s", text)
	}
	if strings.Contains(text, roomLoadingWord) {
		t.Fatalf("a failure was drawn as work still on the way:\n%s", text)
	}
	if !strings.Contains(text, "Widen the import pipe") {
		t.Fatalf("the failure took the instruction with it:\n%s", text)
	}
}

// AND THE PROVISIONAL PAGE REPLACES ITSELF. Every one of those rows is drawn
// only for a page with no blocks, so the journal's arrival takes the whole
// scaffold off in one frame — no second door, and nothing to dismiss.
func TestTheStartingPageIsReplacedTheMomentTheJournalArrives(t *testing.T) {
	a, entry := startingRoomLab(t)
	a.tasks[9].brief = "Widen the import pipe."
	a.tasks[9].waiting = "rate limited"
	journal := []byte(`{"type":"message","role":"user","content":"widen the pipe"}` + "\n" +
		`{"type":"message","role":"assistant","content":"checking the far lock now"}` + "\n")
	var reads atomic.Int64
	a.farRoomRecord = func(uint64, int) (session.TaskRecord, error) {
		if reads.Add(1) == 1 {
			return session.TaskRecord{}, nil
		}
		return session.TaskRecord{Journal: journal, Kept: true}, nil
	}
	a.openRoomFor(9, entry.Title)
	first := a.takeRoomPump()().(roomRecordMsg)
	if cmd := a.farRoomRead(first); cmd == nil {
		t.Fatal("the empty running room did not arm its beat")
	}
	if text := farRoomText(a); !strings.Contains(text, "Widen the import pipe") {
		t.Fatalf("the page between reads lost the instruction:\n%s", text)
	}
	poll := a.farRoomPoll(a.room.gen)
	a.farRoomRead(poll().(roomRecordMsg))
	text := farRoomText(a)
	if !strings.Contains(text, "checking the far lock now") {
		t.Fatalf("the journal did not reach the page:\n%s", text)
	}
	for _, gone := range []string{roomLoadingWord, roomYetWord, "rate limited"} {
		if strings.Contains(text, gone) {
			t.Fatalf("the page kept %q after its transcript arrived:\n%s", gone, text)
		}
	}
}

// AND THE UNDERLYING WORK IS NEVER RESTARTED BY ANY OF IT. Opening the page
// twice is one door, one reader and no second task: the id already running is
// the id read, and the second press closes the page rather than starting
// anything (room.go's [app.openRoomFor]).
func TestOpeningAStartingRoomStartsNoSecondTask(t *testing.T) {
	a, entry := startingRoomLab(t)
	var reads atomic.Int64
	a.farRoomRecord = func(id uint64, _ int) (session.TaskRecord, error) {
		if id != 9 {
			t.Fatalf("the page read task %d", id)
		}
		reads.Add(1)
		return session.TaskRecord{}, nil
	}
	before := len(a.tasks)
	a.openRoomFor(9, entry.Title)
	_ = a.takeRoomPump()
	a.openRoomFor(9, entry.Title)
	if a.room != nil {
		t.Fatal("a second press on the open page did not close it")
	}
	if len(a.tasks) != before {
		t.Fatalf("opening a room changed the roster: %d rows, was %d", len(a.tasks), before)
	}
	if reads.Load() > 1 {
		t.Fatalf("one open made %d reads", reads.Load())
	}
}
