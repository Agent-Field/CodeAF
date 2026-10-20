package session

import (
	"context"
	"fmt"
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
	assertDevRouteReadings(t, c, 2)
}

func TestFreshHumanSteeringKeepsDevRouteAsk(t *testing.T) {
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
	assertDevRouteReadings(t, c, 2)
}

func TestStoppedOnlyResultWakeAdmitsNothing(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		t.Run(fmt.Sprint(multiple), func(t *testing.T) {
			c := &routeCompleter{answer: "The audit was stopped.", verdict: routeYes}
			a, _, _ := routeAgent(t, c)
			a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
			stopped := reviewResultNode(t, a, true, "stopped audit")
			tags := []TaskReplyTag{stopped.resultTag()}
			if multiple {
				tags = append(tags, reviewResultNode(t, a, true, "another stopped audit").resultTag())
			}
			reviewWake(t, a, tags)
			if c.asked() != 0 || c.confirms() != 0 || admitted(a.graph()) != len(tags) {
				t.Fatalf("stopped-only wake: screen=%d confirm=%d admitted=%d", c.asked(), c.confirms(), admitted(a.graph()))
			}
		})
	}
}
