package session

import (
	"context"
	"testing"
	"time"
)

func TestReviewRestoredStoppedResultCannotRestart(t *testing.T) {
	c := &routeCompleter{answer: "The audit was stopped.", verdict: routeYes}
	a, _, _ := routeAgent(t, c)
	a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
	a.personAsk = routeAsk
	stopped := reviewResultNode(t, a, true, "cancelled audit")
	g := a.graph()
	g.mu.Lock()
	record := stopped.recordLocked()
	g.mu.Unlock()
	restored := restoreNode(g, record)
	g.mu.Lock()
	g.nodes[stopped.id] = restored
	g.mu.Unlock()
	t.Logf("checkpoint ending=%q, restored stopped=%t", record.Ending, restored.wasStopped())
	reviewWake(t, a, []TaskReplyTag{restored.resultTag()})
	if c.asked() != 0 || c.confirms() != 0 || admitted(g) != 1 {
		t.Fatal("a restored stopped result reached the classifier and can restart the stopped request")
	}
}

func TestRestoredNonStoppedResultsRemainEligible(t *testing.T) {
	for _, ending := range []TaskEnding{"", TaskEndingInterrupted, TaskEndingWire, TaskEndingRefused} {
		t.Run(string(ending), func(t *testing.T) {
			c := &routeCompleter{answer: "The audit needs a repair.", verdict: routeYes}
			a, _, _ := routeAgent(t, c)
			a.principal = NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
			node := reviewResultNode(t, a, false, "failed audit")
			g := a.graph()
			g.mu.Lock()
			node.ending = ending
			record := node.recordLocked()
			g.mu.Unlock()
			restored := restoreNode(g, record)
			if restored.wasStopped() {
				t.Fatalf("ending %q restored as a person's cancellation", ending)
			}
			g.mu.Lock()
			g.nodes[node.id] = restored
			g.mu.Unlock()
			note := wakeNote("the failed result arrived")
			note.replyTags = []TaskReplyTag{restored.resultTag()}
			a.mu.Lock()
			a.rememberOwedLocked(note)
			a.mu.Unlock()
			ruling := a.readRouteJudge(context.Background(), note, false, c.answer)
			defer ruling.release()
			if !ruling.start || c.asked() != 1 || c.confirms() != 1 {
				t.Fatalf("restored non-stopped result was suppressed: start=%t screen=%d confirm=%d", ruling.start, c.asked(), c.confirms())
			}
		})
	}
}
