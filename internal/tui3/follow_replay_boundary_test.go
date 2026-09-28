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
	a := newTestApp(&fakeAgent{model: "m"})
	a.replayList([]session.DisplayEntry{{Role: "assistant", Text: answer}})
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: answer}
	events <- session.Event{Kind: session.EventTurnDone}
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
