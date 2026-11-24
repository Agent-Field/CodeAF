package tui3

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// liveRefreshAgent is a live view's agent whose snapshot is whatever the test
// says the host's record holds now.
type liveRefreshAgent struct {
	*fakeAgent
	record []session.DisplayEntry
}

func (l *liveRefreshAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	return append([]session.DisplayEntry(nil), l.record...), nil, nil
}

func liveWithRecord(t *testing.T, record ...session.DisplayEntry) (*app, *liveRefreshAgent) {
	t.Helper()
	agent := &liveRefreshAgent{fakeAgent: &fakeAgent{model: "m"}, record: record}
	a := newTestApp(agent)
	t.Cleanup(a.doorLine.close)
	a.dismissWelcome()
	a.takeUp(Conversation{
		Agent:       agent,
		SessionFile: filepath.Join(t.TempDir(), retryRow.Cell, "session.jsonl"),
		Live: &LiveSeam{
			Host: "tower", Device: "dev_tower", Cell: retryRow.Cell, Row: retryRow,
			Read: func() LiveRead { return LiveRead{Kind: LiveOn} },
		},
		Link: &LinkSeam{},
	}, true)
	a.replayList(record)
	return a, agent
}

func noteIndexes(a *app, text string) []int {
	var at []int
	for i, e := range a.entries {
		if e.kind == entryNote && e.text == text {
			at = append(at, i)
		}
	}
	return at
}

// R4: the snapshot a live view takes again after a reconnect (a road swap is
// one) rebuilds the transcript from the host's record. A sentence this window
// said about itself is in nobody's record, and it must still be there once, in
// the place the person read it.
func TestR4ASnapshotRefreshKeepsWhatTheViewerWasToldLocally(t *testing.T) {
	const failed = "could not move this chat here"
	a, agent := liveWithRecord(t,
		session.DisplayEntry{Role: "user", Text: "host question"},
		session.DisplayEntry{Role: "assistant", Text: "host answer"})
	a.note(failed)
	if len(noteIndexes(a, failed)) != 1 {
		t.Fatalf("setup: the sentence was not written once:\n%+v", a.entries)
	}
	last := len(a.entries)

	// The host answered a second question while the road swapped: the refreshed
	// record is longer than what the viewer drew.
	agent.record = append(agent.record,
		session.DisplayEntry{Role: "user", Text: "later question"},
		session.DisplayEntry{Role: "assistant", Text: "later answer"})
	drain(t, a, a.refreshHostedReplay())

	at := noteIndexes(a, failed)
	if len(at) != 1 {
		t.Fatalf("the sentence is on screen %d times after a refresh, want once:\n%+v", len(at), a.entries)
	}
	if at[0] >= len(a.entries)-1 || at[0] > last {
		t.Fatalf("the sentence moved to the end (%d of %d) instead of staying where it was read (%d)", at[0], len(a.entries), last-1)
	}
	for i, want := range []string{"host question", "host answer"} {
		if a.entries[i].text != want {
			t.Fatalf("entry %d = %q, want %q: the record was reordered", i, a.entries[i].text, want)
		}
	}
	if !strings.Contains(plain(frame(a)), failed) {
		t.Fatalf("the frame lost the sentence:\n%s", plain(frame(a)))
	}
}

// R4: a second refresh in a row neither repeats nor drops it, and two notes
// keep their order.
func TestR4TwoRefreshesKeepTwoNotesOnceAndInOrder(t *testing.T) {
	a, _ := liveWithRecord(t,
		session.DisplayEntry{Role: "user", Text: "host question"},
		session.DisplayEntry{Role: "assistant", Text: "host answer"})
	a.note("first sentence")
	a.note("second sentence")
	for i := 0; i < 2; i++ {
		drain(t, a, a.refreshHostedReplay())
	}
	first, second := noteIndexes(a, "first sentence"), noteIndexes(a, "second sentence")
	if len(first) != 1 || len(second) != 1 || first[0] >= second[0] {
		t.Fatalf("notes after two refreshes: first at %v, second at %v; want one each, in order:\n%+v", first, second, a.entries)
	}
}

// R4: a sentence the host's record already says is not drawn twice.
func TestR4ARecordedSentenceIsNotRepeatedByTheLocalCopy(t *testing.T) {
	a, agent := liveWithRecord(t, session.DisplayEntry{Role: "user", Text: "host question"})
	a.note("said both places")
	agent.record = append(agent.record, session.DisplayEntry{Role: "aside", Text: "said both places"})
	drain(t, a, a.refreshHostedReplay())
	if n := len(noteIndexes(a, "said both places")); n > 1 {
		t.Fatalf("the sentence is drawn %d times, want at most once", n)
	}
}

// R5: a conversation held over ssh (no live seam) keeps the refresh it always
// had, which rebuilds from the host's record alone.
func TestR5ASnapshotRefreshWithoutALiveViewIsUnchanged(t *testing.T) {
	agent := &liveRefreshAgent{fakeAgent: &fakeAgent{model: "m"}, record: []session.DisplayEntry{{Role: "user", Text: "host question"}}}
	a := newTestApp(agent)
	t.Cleanup(a.doorLine.close)
	a.dismissWelcome()
	a.replayList(agent.record)
	a.note("a local sentence")
	drain(t, a, a.refreshHostedReplay())
	if n := len(noteIndexes(a, "a local sentence")); n != 0 {
		t.Fatalf("a window that is not a live view kept a local note across the refresh (%d)", n)
	}
}
