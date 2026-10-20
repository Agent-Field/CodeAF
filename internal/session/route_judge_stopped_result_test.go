package session

import (
	"context"
	"strings"
	"testing"
	"time"
)

func reviewResultNode(t *testing.T, a *Agent, stopped bool, title string) *TaskNode {
	t.Helper()
	g := a.graph()
	id := g.reserve()
	g.admit(id, taskSpec{title: title, request: routeAsk, brief: title, acceptance: "done"})
	n := g.node(id)
	g.mu.Lock()
	n.state = TaskFailed
	n.stopped = stopped
	if stopped {
		n.ending = TaskEndingStopped
	}
	g.mu.Unlock()
	return n
}

func reviewWake(t *testing.T, a *Agent, tags []TaskReplyTag) {
	t.Helper()
	wakes, leave := a.WatchWakes()
	defer leave()
	note := wakeNote("The stopped audit was cancelled. A different audit failed and needs a repair.")
	note.replyTags = tags
	if !a.enqueueNote(note) {
		t.Fatal("wake not accepted")
	}
	select {
	case stream := <-wakes:
		collect(t, stream)
	case <-time.After(10 * time.Second):
		t.Fatal("no wake")
	}
}

func TestReviewFreshHumanRepeatDuringStoppedWake(t *testing.T) {
	c := &routeCompleter{answer: "I will fix the audit now.", verdict: routeYes}
	a, _, _ := routeAgent(t, c)
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	a.personAsk = routeAsk
	stopped := reviewResultNode(t, a, true, "cancelled old audit")
	c.onAnswer = func(round int) {
		if round != 1 {
			return
		}
		ch, err := a.Submit(context.Background(), routeAsk)
		if err != nil {
			t.Error(err)
			return
		}
		go func() {
			for range ch {
			}
		}()
	}
	reviewWake(t, a, []TaskReplyTag{stopped.resultTag()})
	a.mu.Lock()
	heard, turn := a.personHeard, a.turnSeq
	a.mu.Unlock()
	t.Logf("fresh human message consumed in turn=%d; current turn=%d; screen=%d confirm=%d", heard, turn, c.asked(), c.confirms())
	if heard != turn {
		t.Fatal("fixture did not consume the human message in the result turn")
	}
	if c.asked() == 0 {
		t.Fatal("fresh human repeat was consumed but the stopped-result guard suppressed routing")
	}
}

func TestFreshHumanSteeringSuppliesTheRouteAsk(t *testing.T) {
	c := &routeCompleter{answer: "I will implement the screenshots now.", verdict: routeYes}
	a, _, _ := routeAgent(t, c)
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	stopped := reviewResultNode(t, a, true, "cancelled old audit")
	const fresh = "implement headless screenshots across the app and add tests"
	c.onAnswer = func(round int) {
		if round != 1 {
			return
		}
		ch, err := a.Submit(context.Background(), fresh)
		if err != nil {
			t.Error(err)
			return
		}
		go func() {
			for range ch {
			}
		}()
	}
	reviewWake(t, a, []TaskReplyTag{stopped.resultTag()})
	c.mu.Lock()
	question, confirm := c.question, c.confirmQ
	c.mu.Unlock()
	for _, got := range []string{question, confirm} {
		if !strings.Contains(got, fresh) || strings.Contains(got, routeAsk) {
			t.Fatalf("routing did not read the new human instruction: %q", got)
		}
	}
}
