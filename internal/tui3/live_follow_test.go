package tui3

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// The reliability contract of the live view (R1 to R4 of the e2efix lane), as a
// person meets it:
//
//	R1  Move here on a live view opens the moved chat on this computer, and the
//	    words typed next go to that chat.
//	R2  The computer that gave the chat up shows a live view of the new holder,
//	    even when the new holder opens the chat a moment after it is taken.
//	R3  When the host goes away and comes back the viewer stays on the live view.
//	R4  A road swap is invisible: nothing the viewer already sees disappears,
//	    repeats or changes place.

var retryRow = chatlist.Row{
	Cell:     "0000000000000042",
	Title:    "Parser work",
	Device:   "tower",
	DeviceID: "dev_tower",
	Status:   chatlist.Running,
}

// retryingDoor is a live door that is shut for the first `shut` asks, the way
// a computer that has just taken a chat is shut until it has opened it.
func retryingDoor(t *testing.T, a *app, shut int) (asked *int, agents *[]*fakeAgent) {
	t.Helper()
	calls, built := new(int), new([]*fakeAgent)
	root := t.TempDir()
	a.openLive = func(_ context.Context, row chatlist.Row) (Conversation, error) {
		*calls++
		if *calls <= shut {
			return Conversation{}, ErrNoLiveView
		}
		agent := &fakeAgent{model: "m"}
		*built = append(*built, agent)
		return Conversation{
			Agent:       agent,
			SessionFile: filepath.Join(root, row.Cell, "session.jsonl"),
			Live: &LiveSeam{
				Host: row.Device, Device: row.DeviceID, Cell: row.Cell, Row: row,
				Read: func() LiveRead { return LiveRead{Kind: LiveOn} },
			},
			Link: &LinkSeam{},
		}, nil
	}
	return calls, built
}

func supersededNotes(a *app) int {
	n := 0
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, chatlist.Superseded("tower")) {
			n++
		}
	}
	return n
}

// R2: a window whose chat was taken asks the new holder for a live view as soon
// as the directory says so. That computer may not have the chat open yet, and
// the window used to give up on the first refusal and stay read-only for good.
// It must try again, quietly, and end up on a live view.
func TestR2AWindowWhoseNewHolderOpensTheChatLateStillFollowsIt(t *testing.T) {
	oldAgent := &fakeAgent{model: "m"}
	a := newTestApp(oldAgent)
	t.Cleanup(a.doorLine.close)
	a.dismissWelcome()
	a.devRow.devices = rosterOf
	a.takeUp(Conversation{
		Agent:       oldAgent,
		SessionFile: filepath.Join(t.TempDir(), retryRow.Cell, "session.jsonl"),
	}, true)
	a.input.setText("words already in the box")
	calls, agents := retryingDoor(t, a, 2)

	drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{retryRow}}))
	if *calls != 1 || a.liveFront() != nil {
		t.Fatalf("first try: %d asks, live view %v; want one refused ask and no view", *calls, a.liveFront())
	}
	if supersededNotes(a) != 1 {
		t.Fatalf("the refused first try said the superseded sentence %d times, want once", supersededNotes(a))
	}
	if a.followRetry == nil {
		t.Fatal("a refused first try left nothing to try again with")
	}

	// The second try is refused as well, and says nothing more.
	drain(t, a, a.followRetried(followRetryMsg{seq: a.followRetry.seq}))
	if *calls != 2 || a.liveFront() != nil {
		t.Fatalf("second try: %d asks, live view %v; want two asks and no view", *calls, a.liveFront())
	}
	if supersededNotes(a) != 1 {
		t.Fatalf("a refused retry repeated the superseded sentence (%d times)", supersededNotes(a))
	}

	// The third finds the door open: the window becomes a live view of the new
	// holder with the old agent retired and the typed words kept.
	drain(t, a, a.followRetried(followRetryMsg{seq: a.followRetry.seq}))
	if a.liveFront() == nil || a.liveFront().Host != "tower" || len(*agents) != 1 || a.agent != (*agents)[0] {
		t.Fatalf("third try did not end on a live view of the new holder: live=%+v", a.liveFront())
	}
	if oldAgent.closes != 1 {
		t.Fatalf("old local agent closes = %d, want 1", oldAgent.closes)
	}
	if got := a.input.String(); got != "words already in the box" {
		t.Fatalf("composer words after the follow = %q", got)
	}
	if a.followRetry != nil {
		t.Fatal("a finished follow left its retry behind")
	}
}

// R2, the bound: a holder that never opens the chat is asked a few times and
// then left alone, and a retry that outlived its displacement does nothing.
func TestR2TheRetriesStopWhenTheyAreSpentOrNoLongerWanted(t *testing.T) {
	oldAgent := &fakeAgent{model: "m"}
	a := newTestApp(oldAgent)
	t.Cleanup(a.doorLine.close)
	a.dismissWelcome()
	a.devRow.devices = rosterOf
	a.takeUp(Conversation{
		Agent:       oldAgent,
		SessionFile: filepath.Join(t.TempDir(), retryRow.Cell, "session.jsonl"),
	}, true)
	calls, _ := retryingDoor(t, a, 1000)

	drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{retryRow}}))
	seq := a.followRetry.seq
	for i := 0; i < len(followRetryWaits)+3 && a.followRetry != nil; i++ {
		drain(t, a, a.followRetried(followRetryMsg{seq: seq}))
	}
	if a.followRetry != nil {
		t.Fatal("the retries never ended")
	}
	if want := 1 + len(followRetryWaits); *calls != want {
		t.Fatalf("asked the door %d times, want %d (the first try and %d retries)", *calls, want, len(followRetryWaits))
	}
	if supersededNotes(a) != 1 {
		t.Fatalf("the superseded sentence was said %d times, want once", supersededNotes(a))
	}
	// A late timer of the finished displacement is ignored.
	before := *calls
	drain(t, a, a.followRetried(followRetryMsg{seq: seq}))
	if *calls != before {
		t.Fatal("a stale retry timer asked the door")
	}
}

// R2: the person moved on, so the retry must not swap the window under them.
func TestR2ARetryDoesNothingOnceThePersonLeftTheDisplacedChat(t *testing.T) {
	oldAgent := &fakeAgent{model: "m"}
	a := newTestApp(oldAgent)
	t.Cleanup(a.doorLine.close)
	a.dismissWelcome()
	a.devRow.devices = rosterOf
	a.takeUp(Conversation{
		Agent:       oldAgent,
		SessionFile: filepath.Join(t.TempDir(), retryRow.Cell, "session.jsonl"),
	}, true)
	calls, _ := retryingDoor(t, a, 1)
	drain(t, a, a.recordMachines(homeMachinesMsg{rows: []chatlist.Row{retryRow}}))
	a.file = filepath.Join(t.TempDir(), "0000000000000099", "session.jsonl")
	drain(t, a, a.followRetried(followRetryMsg{seq: a.followRetry.seq}))
	if *calls != 1 || a.liveFront() != nil {
		t.Fatalf("a retry after the person left asked the door again (%d asks) or opened a view (%v)", *calls, a.liveFront())
	}
	if a.followRetry != nil {
		t.Fatal("a retry for a conversation no longer in front was kept")
	}
}
