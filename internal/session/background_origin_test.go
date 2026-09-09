package session

import (
	"strings"
	"testing"
)

func TestBackgroundResultsKeepEveryOriginAndWholeOutcome(t *testing.T) {
	first := backgroundResult{text: "job 1 exited 0\nall 24 checks passed\nfull log: /logs/1", request: 4}
	second := backgroundResult{text: "job 2 exited 1\nmissing dependency\nfull log: /logs/2", request: 7}
	unknown := jobNote(backgroundResult{text: "legacy outcome"})
	batch := batchSessionNotes([]userMessage{jobNote(first), jobNote(second), unknown})
	a := &Agent{running: true, personSeq: 99}
	a.rememberOwedLocked(batch)
	if len(a.owedAsks) != 3 {
		t.Fatalf("background origins collapsed: %+v", a.owedAsks)
	}
	for i, want := range []backgroundResult{first, second, {text: "legacy outcome"}} {
		if got := a.owedAsks[i]; got.request != want.request || got.outcome != want.text {
			t.Fatalf("receipt %d lost its origin or outcome: %+v", i, got)
		}
	}
	if got := owedAsksText(a.owedAsks); got != backgroundReplyObligation {
		t.Fatalf("the reporting duty was repeated: %s", got)
	}
	if got := a.requestForWork(); got != 0 {
		t.Fatalf("mixed outcomes borrowed request %d", got)
	}
}

func TestBackgroundWorkOriginFollowsProducerInsteadOfLatestPerson(t *testing.T) {
	for _, one := range []struct {
		name    string
		origins []uint64
		want    uint64
	}{
		{"one older request", []uint64{4}, 4},
		{"several outcomes same request", []uint64{4, 4}, 4},
		{"different requests", []uint64{4, 7}, 0},
		{"unknown request", []uint64{0}, 0},
		{"known and unknown requests", []uint64{4, 0}, 0},
	} {
		t.Run(one.name, func(t *testing.T) {
			a := &Agent{running: true, personSeq: 99}
			for _, origin := range one.origins {
				a.rememberOwedLocked(jobNote(backgroundResult{text: "same outcome", request: origin}))
			}
			if got := a.requestForWork(); got != one.want {
				t.Fatalf("origin = %d, want %d", got, one.want)
			}
		})
	}
}

func TestBackgroundJobCapturesOriginBeforeItCanFinish(t *testing.T) {
	var result backgroundResult
	origin := uint64(4)
	registry := newJobRegistry(t.TempDir(), Place{}, func(got backgroundResult) { result = got })
	registry.request = func() uint64 { return origin }
	job, err := registry.newJob("render a report", jobKindRender)
	if err != nil {
		t.Fatal(err)
	}
	origin = 99
	registry.finish(job, 0, "complete report\nall supporting evidence")
	if result.request != 4 || !strings.Contains(result.text, "all supporting evidence") {
		t.Fatalf("ending lost the original request or outcome: %+v", result)
	}
}

func TestWorkOriginUsesPersonOrOwnedResultWithoutBorrowingLatestAsk(t *testing.T) {
	a := &Agent{running: true, personSeq: 99}
	graph := newTaskGraph()
	a.tasks = graph
	graph.nodes[1] = &TaskNode{admitBy: a, admitRequest: 4}
	graph.nodes[2] = &TaskNode{admitBy: a, admitRequest: 7}
	a.rememberOwedLocked(userMessage{authored: true, replyTags: []TaskReplyTag{{ID: 1, Request: "the same words"}}})
	if got := a.requestForWork(); got != 4 {
		t.Fatalf("owned result origin = %d", got)
	}
	a.rememberOwedLocked(userMessage{authored: true, replyTags: []TaskReplyTag{{ID: 2, Request: "the same words"}}})
	if got := a.requestForWork(); got != 0 {
		t.Fatalf("mixed task results claimed %d", got)
	}
	a.rememberOwedLocked(userText("new work"))
	if got := a.requestForWork(); got != 99 {
		t.Fatalf("person's new request lost: %d", got)
	}
	if got := (*Agent)(nil).requestForWork(); got != 0 {
		t.Fatalf("nil agent claimed %d", got)
	}
}
