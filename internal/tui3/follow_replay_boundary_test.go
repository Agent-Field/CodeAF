package tui3

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
	"testing"
)

// The hidden hosted conversation finishes before its tab is selected. Its
// atomic replay now contains the answer, while the connection still has the
// original Follow stream queued. Opening it must render that answer once.
func TestCompletedHostedFollowDoesNotRepeatAtomicReplay(t *testing.T) {
	answer := "The invoice boundary was independently verified."
	cursor := session.ReplayCursor{Owner: "checker-engine", Turn: 1}
	agent := &replayBoundaryAgent{fakeAgent: &fakeAgent{model: "m"}, cursor: cursor}
	a := newTestApp(agent)
	a.replayList([]session.DisplayEntry{{Role: "assistant", Text: answer}})
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: answer, ReplayCursor: cursor}
	events <- session.Event{Kind: session.EventTurnDone, ReplayCursor: cursor}
	close(events)
	drive(t, a, followingMsg{turn: Following{Events: events}, gen: a.convGen})
	count := 0
	for _, e := range a.entries {
		if e.kind == entryAssistant && strings.Contains(e.text, answer) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("one persisted answer rendered %d times", count)
	}
}

// The remote adapter's predicate is independent of the output's words.
type replayBoundaryAgent struct {
	*fakeAgent
	cursor session.ReplayCursor
}

func (a *replayBoundaryAgent) ReplayCovers(ev session.Event) bool {
	return !ev.ReplayObserved && a.cursor.Covers(ev.ReplayCursor)
}

func TestReplayBoundaryKeepsObserverAndNewIdenticalAnswers(t *testing.T) {
	cursor := session.ReplayCursor{Owner: "checker", Turn: 4}
	for _, tc := range []struct {
		name     string
		cursor   session.ReplayCursor
		observed bool
	}{
		{"authoritative observer", cursor, true},
		{"new turn", session.ReplayCursor{Owner: "checker", Turn: 5}, false},
		{"replacement engine", session.ReplayCursor{Owner: "replacement", Turn: 1}, false},
		{"older peer without identity", session.ReplayCursor{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(&replayBoundaryAgent{fakeAgent: &fakeAgent{model: "m"}, cursor: cursor})
			a.replayList([]session.DisplayEntry{{Role: "assistant", Text: "same words"}})
			events := make(chan session.Event, 2)
			events <- session.Event{Kind: session.EventTextDelta, Text: "same words", ReplayCursor: tc.cursor, ReplayObserved: tc.observed}
			events <- session.Event{Kind: session.EventTurnDone, ReplayCursor: tc.cursor, ReplayObserved: tc.observed}
			close(events)
			drive(t, a, followingMsg{turn: Following{Events: events}, gen: a.convGen})
			n := 0
			for _, e := range a.entries {
				if e.kind == entryAssistant && e.text == "same words" {
					n++
				}
			}
			if n != 2 {
				t.Fatalf("legitimate same-text reply lost: %d", n)
			}
		})
	}
}

func TestCoveredFollowDoesNotAddAnotherUserMessage(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.replayList([]session.DisplayEntry{{Role: "user", Text: "check invoices"}, {Role: "assistant", Text: "checked"}})
	events := make(chan session.Event)
	close(events)
	drive(t, a, followingMsg{turn: Following{Said: "check invoices", Events: events, Covered: func() bool { return true }}, gen: a.convGen})
	n := 0
	for _, e := range a.entries {
		if e.kind == entryUser {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("covered turn duplicated its user message: %d", n)
	}
}

func TestHostedReplayGateRetainsOnlyUncoveredArrivals(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.hostReplayLoading = true
	old := make(chan session.Event)
	close(old)
	fresh := make(chan session.Event, 2)
	fresh <- session.Event{Kind: session.EventTextDelta, Text: "new while opening"}
	fresh <- session.Event{Kind: session.EventTurnDone}
	close(fresh)
	drive(t, a, followingMsg{turn: Following{Events: old, Covered: func() bool { return true }}, gen: a.convGen}, followingMsg{turn: Following{Events: fresh, Covered: func() bool { return false }}, gen: a.convGen})
	if len(a.hostReplayPending) != 2 {
		t.Fatal("arrivals were not held at snapshot boundary")
	}
	a.replayList([]session.DisplayEntry{{Role: "assistant", Text: "already complete"}})
	drain(t, a, a.finishHostedReplay())
	if got := plain(frame(a)); !strings.Contains(got, "already complete") || !strings.Contains(got, "new while opening") {
		t.Fatalf("snapshot or new tail lost: %s", got)
	}
}

type atomicBoundaryAgent struct {
	*replayBoundaryAgent
	entries []session.DisplayEntry
	events  <-chan session.Event
}

func (a *atomicBoundaryAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	return a.entries, a.events, func() {}
}

func TestReconnectRefreshReplacesSnapshotAndKeepsObserverTail(t *testing.T) {
	cursor := session.ReplayCursor{Owner: "same engine", Turn: 2}
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "remaining live answer", ReplayCursor: cursor, ReplayObserved: true}
	events <- session.Event{Kind: session.EventTurnDone, ReplayCursor: cursor, ReplayObserved: true}
	close(events)
	agent := &atomicBoundaryAgent{replayBoundaryAgent: &replayBoundaryAgent{fakeAgent: &fakeAgent{model: "m"}, cursor: cursor}, entries: []session.DisplayEntry{{Role: "assistant", Text: "completed while disconnected"}}, events: events}
	a := newTestApp(agent)
	a.replayList([]session.DisplayEntry{{Role: "assistant", Text: "stale view"}})
	drive(t, a, followingMsg{turn: Following{Replay: true}, gen: a.convGen})
	got := plain(frame(a))
	if strings.Contains(got, "stale view") || !strings.Contains(got, "completed while disconnected") || !strings.Contains(got, "remaining live answer") {
		t.Fatalf("reconnect replay not authoritative: %s", got)
	}
}
