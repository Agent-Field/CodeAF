package tui3

import (
	tea "charm.land/bubbletea/v2"
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
	assertReplayText(t, a, "stale view", 0)
	assertReplayText(t, a, "completed while disconnected", 1)
	assertReplayText(t, a, "remaining live answer", 1)
	if got := plain(frame(a)); !strings.Contains(got, "remaining live answer") {
		t.Fatalf("live observer tail is not visible: %s", got)
	}
}

func TestQueuedHostedFollowRechecksOwnershipAtActualAdmission(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	blocker := make(chan session.Event)
	a.stream = blocker
	covered := false
	old := make(chan session.Event)
	close(old)
	drive(t, a, followingMsg{turn: Following{Said: "check invoices", Events: old, Covered: func() bool { return covered }}, gen: a.convGen})
	if len(a.follows) != 1 {
		t.Fatal("turn not queued behind current stream")
	}
	a.replayList([]session.DisplayEntry{{Role: "user", Text: "check invoices"}, {Role: "assistant", Text: "checked"}})
	covered = true
	a.stream = nil
	drain(t, a, a.startFollow())
	n := 0
	for _, e := range a.entries {
		if e.kind == entryUser {
			n++
		}
	}
	if n != 1 || len(a.follows) != 0 {
		t.Fatalf("covered queued send admitted: users=%d pending=%d", n, len(a.follows))
	}
}

func assertReplayText(t *testing.T, a *app, text string, want int) {
	t.Helper()
	n := 0
	for _, e := range a.entries {
		n += strings.Count(e.text, text)
	}
	if n != want {
		t.Fatalf("%q appears %d times, want %d: %+v", text, n, want, a.entries)
	}
}

func replayTestAgent(events <-chan session.Event) *atomicBoundaryAgent {
	return &atomicBoundaryAgent{
		replayBoundaryAgent: &replayBoundaryAgent{fakeAgent: &fakeAgent{model: "m"}, cursor: session.ReplayCursor{Owner: "engine", Turn: 1}},
		events:              events,
	}
}

func TestReconnectRefreshReplacesAlreadyVisibleCurrentPrefix(t *testing.T) {
	events := make(chan session.Event, 3)
	events <- session.Event{Kind: session.EventTextDelta, Text: "original prefix ", ReplayObserved: true}
	events <- session.Event{Kind: session.EventTextDelta, Text: "new suffix", ReplayObserved: true}
	events <- session.Event{Kind: session.EventTurnDone, ReplayObserved: true}
	close(events)
	a := newTestApp(replayTestAgent(events))
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "original prefix "})
	drain(t, a, a.refreshHostedReplay())
	assertReplayText(t, a, "original prefix", 1)
	assertReplayText(t, a, "new suffix", 1)
}

func TestReconnectRefreshHoldsNewSubmitUntilIdleSnapshotIsFolded(t *testing.T) {
	agent := replayTestAgent(nil)
	a := newTestApp(agent)
	snapshot := a.refreshHostedReplay()
	// The server has replied idle, but its UI fold is deliberately delayed.
	answer := snapshot()
	fresh := make(chan session.Event, 2)
	fresh <- session.Event{Kind: session.EventTextDelta, Text: "new accepted answer"}
	fresh <- session.Event{Kind: session.EventTurnDone}
	close(fresh)
	calls := 0
	cmd := a.submitting("new question", func() (<-chan session.Event, error) { calls++; return fresh, nil })
	if cmd != nil || calls != 0 {
		t.Fatal("submit crossed an unfurled snapshot")
	}
	_, cmd = a.Update(answer)
	drain(t, a, cmd)
	if calls != 1 {
		t.Fatalf("submit called %d times", calls)
	}
	assertReplayText(t, a, "new question", 1)
	assertReplayText(t, a, "new accepted answer", 1)
}

func TestReconnectRefreshWaitsForAlreadyIssuedSubmitResponse(t *testing.T) {
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "accepted before snapshot", ReplayObserved: true}
	events <- session.Event{Kind: session.EventTurnDone, ReplayObserved: true}
	close(events)
	agent := replayTestAgent(events)
	agent.entries = []session.DisplayEntry{{Role: "user", Text: "earlier question"}}
	a := newTestApp(agent)
	canonical := make(chan session.Event)
	close(canonical)
	submit := a.submitting("earlier question", func() (<-chan session.Event, error) { return canonical, nil })
	if cmd := a.refreshHostedReplay(); cmd != nil || !a.hostReplayWaiting {
		t.Fatal("snapshot started ahead of an issued submit")
	}
	response := runSubmit(t, submit)
	drain(t, a, a.adopt(response))
	assertReplayText(t, a, "earlier question", 1)
	assertReplayText(t, a, "accepted before snapshot", 1)
}

func TestReconnectRefreshKeepsQueuedLocalFollowUp(t *testing.T) {
	a := newTestApp(replayTestAgent(nil))
	fresh := make(chan session.Event, 2)
	fresh <- session.Event{Kind: session.EventTextDelta, Text: "queued answer"}
	fresh <- session.Event{Kind: session.EventTurnDone}
	close(fresh)
	a.follows = []queued{{text: "queued question", ch: fresh, covered: func() bool { return false }}}
	drain(t, a, a.refreshHostedReplay())
	assertReplayText(t, a, "queued question", 1)
	assertReplayText(t, a, "queued answer", 1)
}

func TestReconnectRefreshHoldsFollowUpCallUntilSnapshotIsFolded(t *testing.T) {
	agent := replayTestAgent(nil)
	a := newTestApp(agent)
	snapshot := a.refreshHostedReplay()
	answer := snapshot()
	if cmd := a.sendFollow("after reconnect", "after reconnect"); cmd != nil {
		t.Fatal("follow-up escaped replay gate")
	}
	if len(a.hostDeferred) != 1 {
		t.Fatal("follow-up not retained")
	}
	// Only examine admission here; the fake's channel is intentionally idle.
	_, cmd := a.Update(answer)
	msgs := runCmd(cmd)
	var settled tea.Cmd
	for _, msg := range msgs {
		if follow, ok := msg.(followMsg); ok {
			settled = a.queueFollow(follow)
		}
	}
	_ = settled
	if a.hostCalls != 0 || len(a.hostDeferred) != 0 {
		t.Fatal("follow-up not settled after snapshot")
	}
}
