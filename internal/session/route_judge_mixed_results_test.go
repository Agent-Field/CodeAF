package session

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReviewMixedResultsKeepIndependentRepairEligible(t *testing.T) {
	c := &routeCompleter{answer: "The cancelled UI audit stays cancelled. I will complete the independent storage audit.", verdict: `{"work":true,"goal":"Complete the independent pricing storage audit; leave the cancelled UI audit stopped","acceptance":"Storage audit report exists","why":"finish the still-authorized independent audit"}`}
	a, _, _ := routeAgent(t, c)
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	a.personAsk = routeAsk
	stopped := reviewResultNode(t, a, true, "cancelled pricing UI audit")
	failed := reviewResultNode(t, a, false, "failed pricing storage audit")
	reviewWake(t, a, []TaskReplyTag{stopped.resultTag(), failed.resultTag()})
	a.mu.Lock()
	results := append([]uint64(nil), a.turnResults...)
	a.mu.Unlock()
	t.Logf("result IDs=%v; screen=%d confirm=%d", results, c.asked(), c.confirms())
	if len(results) != 2 {
		t.Fatal("fixture did not deliver both results")
	}
	if c.asked() == 0 {
		t.Fatal("a different failed task's repair was suppressed by the stopped sibling's inherited request")
	}
	c.mu.Lock()
	question, confirm := c.question, c.confirmQ
	c.mu.Unlock()
	if question != confirm {
		t.Fatalf("screen and confirm read different obligations:\n%s\n%s", question, confirm)
	}
	for _, want := range []string{"Only the non-stopped task results", "Do not restart stopped tasks", "Task 2 (attempt 0), failed pricing storage audit", "Stopped task 1, report only: cancelled pricing UI audit"} {
		if !strings.Contains(question, want) {
			t.Errorf("the mixed-result question omits %q:\n%s", want, question)
		}
	}
}

func TestStoppedResultRoutingUsesTheDeliveredAttempt(t *testing.T) {
	for _, stoppedAtDelivery := range []bool{true, false} {
		t.Run(fmt.Sprintf("stopped_at_delivery_%t", stoppedAtDelivery), func(t *testing.T) {
			c := &routeCompleter{answer: "Here is what happened.", verdict: routeYes}
			a, _, _ := routeAgent(t, c)
			a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
			node := reviewResultNode(t, a, stoppedAtDelivery, "delayed audit")
			tag := node.resultTag()
			g := a.graph()
			g.mu.Lock()
			// Another attempt changes the live node before this result is consumed.
			node.attempt++
			node.stopped = !stoppedAtDelivery
			g.mu.Unlock()
			a.mu.Lock()
			note := wakeNote("the old attempt arrived")
			note.replyTags = []TaskReplyTag{tag}
			a.rememberOwedLocked(note)
			a.mu.Unlock()
			ruling := a.readRouteJudge(context.Background(), note, false, c.answer)
			defer ruling.release()
			if ruling.start == stoppedAtDelivery {
				t.Fatalf("start=%t followed the live node instead of delivered stopped=%t", ruling.start, stoppedAtDelivery)
			}
			wantCalls := 1
			if stoppedAtDelivery {
				wantCalls = 0
			}
			if c.asked() != wantCalls || c.confirms() != wantCalls {
				t.Fatalf("screen=%d confirm=%d, want %d each", c.asked(), c.confirms(), wantCalls)
			}
			// The next wake cannot inherit this attempt's cancellation snapshot.
			a.mu.Lock()
			a.forgetOwedLocked()
			a.mu.Unlock()
			fresh := a.readRouteJudge(context.Background(), wakeNote("new event"), false, c.answer)
			defer fresh.release()
			if !fresh.start {
				t.Fatal("a previous turn's stopped result suppressed a later wake")
			}
		})
	}
}

func TestMixedResultRoutingRetainsEachTasksEffectiveTarget(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reversed_%t", reverse), func(t *testing.T) {
			c := &routeCompleter{answer: "Only the storage audit needs work.", verdict: routeYes}
			a, _, _ := routeAgent(t, c)
			a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
			stopped := reviewResultNode(t, a, true, "cancelled UI audit")
			failed := reviewResultNode(t, a, false, "storage audit")
			remaining := failed.resultTag()
			const target = "audit storage transactions only and produce the storage report"
			remaining.Obligation, remaining.Revision = target, 2
			tags := []TaskReplyTag{stopped.resultTag(), remaining}
			if reverse {
				tags[0], tags[1] = tags[1], tags[0]
			}
			note := wakeNote("the two results arrived")
			note.replyTags = tags
			a.mu.Lock()
			a.rememberOwedLocked(note)
			a.mu.Unlock()
			ruling := a.readRouteJudge(context.Background(), note, false, c.answer)
			defer ruling.release()
			if !ruling.start {
				t.Fatal("the independent result was suppressed")
			}
			c.mu.Lock()
			question, confirm := c.question, c.confirmQ
			c.mu.Unlock()
			if question != confirm || !strings.Contains(question, target) || strings.Contains(question, "Its own target: "+routeAsk) {
				t.Fatalf("routing lost the eligible task's own effective target:\n%s\n%s", question, confirm)
			}
		})
	}
}
