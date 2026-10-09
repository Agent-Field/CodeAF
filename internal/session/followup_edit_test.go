package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// queuedBehindARunningTurn starts a turn that blocks until release is closed
// and queues the given words behind it, one scripted step per queued message,
// returning their streams in the order they were typed.
func queuedBehindARunningTurn(t *testing.T, words ...string) (agent *Agent, completer *scriptedCompleter, first <-chan Event, queued []<-chan Event, release chan struct{}) {
	t.Helper()
	entered := make(chan struct{})
	release = make(chan struct{})
	script := []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		close(entered)
		<-release
		return textResponse("one"), nil
	}}
	for range words {
		script = append(script, func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("next"), nil })
	}
	completer = &scriptedCompleter{steps: script}
	agent, _ = newTestAgent(t, completer, nil)
	first = mustSubmit(t, agent, "the question")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}
	for _, w := range words {
		ch, err := agent.FollowUp(w)
		if err != nil {
			t.Fatalf("FollowUp: %v", err)
		}
		queued = append(queued, ch)
	}
	return agent, completer, first, queued, release
}

// openedWith is the words request n of the completer was opened by: the last
// message of a turn's first request is the user's own.
func openedWith(c *scriptedCompleter, n int) string {
	request := c.request(n)
	return messageText(request[len(request)-1])
}

// AN EDIT REPLACES THE WORDS AND KEEPS THE PLACE: the turn that drains the
// message opens with the new text, and an empty edit is refused.
func TestEditFollowUpChangesTheWordsTheTurnOpensWith(t *testing.T) {
	agent, completer, first, queued, release := queuedBehindARunningTurn(t, "the typo")
	if err := agent.EditFollowUp(queued[0], "  the fix  "); err != nil {
		t.Fatalf("EditFollowUp: %v", err)
	}
	if err := agent.EditFollowUp(queued[0], "   "); err == nil {
		t.Fatal("an empty edit was accepted")
	}
	close(release)
	collect(t, first)
	collect(t, queued[0])
	if got := openedWith(completer, 1); got != "the fix" {
		t.Fatalf("the follow-up turn opened with %q", got)
	}
	if countMessages(agent, "the typo") != 0 {
		t.Fatal("the replaced words reached the transcript")
	}
}

// AN EDIT OF A MESSAGE THAT IS NO LONGER QUEUED IS REFUSED, never applied late:
// once the turn has started on the old words the new ones are not what the
// model was asked.
func TestEditFollowUpIsRefusedOnceTheTurnHasStarted(t *testing.T) {
	agent, _, first, queued, release := queuedBehindARunningTurn(t, "the follow-up")
	close(release)
	collect(t, first)
	collect(t, queued[0])
	if err := agent.EditFollowUp(queued[0], "too late"); !errors.Is(err, ErrFollowUpGone) {
		t.Fatalf("EditFollowUp after delivery = %v, want ErrFollowUpGone", err)
	}
	if countMessages(agent, "too late") != 0 {
		t.Fatal("a late edit reached the transcript")
	}
}

// REORDERING CHANGES THE ORDER THE TURNS RUN IN: a message moved before the
// first runs first, an unknown neighbour means the end, and a stream that is
// not queued is false.
func TestMoveFollowUpSetsTheOrderTheTurnsRunIn(t *testing.T) {
	agent, completer, first, queued, release := queuedBehindARunningTurn(t, "a", "b", "c")
	a, b, c := queued[0], queued[1], queued[2]
	if !agent.MoveFollowUp(c, a) { // c a b
		t.Fatal("the move before a neighbour was refused")
	}
	if !agent.MoveFollowUp(c, make(chan Event)) { // a b c
		t.Fatal("a move before an unknown neighbour was refused")
	}
	if !agent.MoveFollowUp(a, nil) { // b c a
		t.Fatal("a move to the end was refused")
	}
	if !agent.MoveFollowUp(a, b) { // a b c
		t.Fatal("a move to the front was refused")
	}
	if !agent.MoveFollowUp(c, b) { // a c b
		t.Fatal("the last move was refused")
	}
	if agent.MoveFollowUp(make(chan Event), nil) {
		t.Fatal("an unknown stream was moved")
	}
	close(release)
	collect(t, first)
	for _, ch := range queued {
		collect(t, ch)
	}
	got := strings.Join([]string{openedWith(completer, 1), openedWith(completer, 2), openedWith(completer, 3)}, ",")
	if got != "a,c,b" {
		t.Fatalf("turns ran in order %q, want a,c,b", got)
	}
}

// THE RACE WITH DELIVERY: edits and moves hammered while the turn ends either
// land before the drain (and the turn carries them) or report the message gone;
// the message runs exactly once, with one of the two texts, and never a late
// success.
func TestEditAndMoveRaceTheDrainHonestly(t *testing.T) {
	for round := 0; round < 25; round++ {
		agent, completer, first, queued, release := queuedBehindARunningTurn(t, "old")
		done := make(chan error, 1)
		go func() {
			var last error
			for i := 0; i < 50; i++ {
				last = agent.EditFollowUp(queued[0], "new")
				agent.MoveFollowUp(queued[0], nil)
			}
			done <- last
		}()
		close(release)
		collect(t, first)
		collect(t, queued[0])
		last := <-done
		if opened := openedWith(completer, 1); opened != "old" && opened != "new" {
			t.Fatalf("the turn opened with %q", opened)
		}
		if last != nil && !errors.Is(last, ErrFollowUpGone) {
			t.Fatalf("race edit = %v", last)
		}
		if completer.requests() != 2 {
			t.Fatalf("requests = %d, want the message to run exactly once", completer.requests())
		}
	}
}
