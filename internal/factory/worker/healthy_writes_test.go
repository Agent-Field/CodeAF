package worker_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

type healthyMergeSource struct {
	pullSource
	pr factory.PullRequest
}

func (s *healthyMergeSource) Follow(context.Context, factory.Item) (factory.PullRequest, error) {
	return s.pr, nil
}

func TestHealthyWorkerApprovedMerge(t *testing.T) {
	head := strings.Repeat("1", 40)
	src := &healthyMergeSource{pr: factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen, HeadSHA: head, Mergeable: factory.MergeableYes, ChecksRead: true, Checks: factory.PRChecks{Passed: 1}, ReviewsRead: true}}
	g := newRigWith(t, nil, func(o *run.Options) { o.Source = func(string) factory.Source { return src } }, "me")
	id := g.add(t, check("true"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateShipped
		it.Publish = "pr"
		it.HeadCommit = head
		it.PullRequest = &src.pr
		it.Proof = []factory.Claim{{Text: "works", OK: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(g.base+"/v1/jobs/"+strconv.Itoa(id)+"/merge", "application/json", strings.NewReader(`{"commit":"`+head+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("approve merge: %d", resp.StatusCode)
	}
	time.Sleep(factory.MergeUndo)
	resp2, err := http.Get(g.base + "/v1/queue")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for len(src.writes()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := src.writes(); len(got) != 1 || got[0].Verb != "merge" {
		t.Fatalf("healthy approved merge writes=%v", got)
	}
}

func TestHealthyWorkerApprovedPost(t *testing.T) {
	src := &pullSource{}
	g := newRigWith(t, nil, func(o *run.Options) {
		o.Exec[factory.StagePost] = run.NewPostExecutor(func(string) factory.Source { return src }, nil)
	})
	id := manualJob(t, g, named("proof"), factory.Stage{Name: "post", Kind: factory.StagePost, Ask: "comment approved proof", On: true})
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(src.writes()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := src.writes(); len(got) != 1 || got[0].Verb != "comment" {
		t.Fatalf("healthy approved post writes=%v", got)
	}
}
