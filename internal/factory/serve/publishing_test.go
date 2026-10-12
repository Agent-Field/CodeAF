package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const approvedHead = "1111111111111111111111111111111111111111"
const movedHead = "2222222222222222222222222222222222222222"

func publishingSetup(t *testing.T) (*workerFixture, *record.Record, factory.Item, factory.Seam, serve.Config) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &workerFixture{st: st}
	f.clock.Store(now.UnixNano())
	clock := func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }
	rec, err := record.Open(st, record.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	desk, err := lease.Open(st, lease.Options{Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	s := factory.Seam{
		Load:    func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err },
		SignOff: lease.NewDoors(desk).SignOff,
		Answer:  lease.NewDoors(desk).Answer,
		GitHub:  func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "sam"}, nil },
	}
	cfg := config
	cfg.Store, cfg.Record, cfg.PullWait, cfg.LeaseOptions = st, rec, time.Millisecond, lease.Options{Now: clock}
	f.handler = serve.New(s, cfg)
	f.server = httptest.NewServer(f.handler)
	t.Cleanup(f.server.Close)
	t.Cleanup(f.handler.Close)
	it, err := st.Create(factory.Item{Repo: "owner/repo", Title: "fix", State: factory.StateLanded, Assignee: "sam", WorktreeMachine: "laptop", HeadCommit: approvedHead, Proof: []factory.Claim{{Text: "works", OK: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return f, rec, it, s, cfg
}

func TestApprovalRejectsChangedHeadAndRecordsExactCommit(t *testing.T) {
	f, rec, it, _, _ := publishingSetup(t)
	path := fmt.Sprintf("/v1/jobs/%d/approve", it.ID)
	for _, commit := range []string{"", movedHead} {
		r := keyedRequest(f.handler, path, `{"commit":"`+commit+`"}`, "")
		if commit == "" {
			if r.Code != 422 {
				t.Fatalf("missing commit=%d %s", r.Code, r.Body)
			}
			continue
		}
		var body struct{ Reason, HeadCommit string }
		var raw map[string]string
		_ = json.Unmarshal(r.Body.Bytes(), &raw)
		body.Reason, body.HeadCommit = raw["reason"], raw["head_commit"]
		if r.Code != 409 || body.Reason != "changed_since_opened" || body.HeadCommit != approvedHead {
			t.Fatalf("stale=%d %s", r.Code, r.Body)
		}
	}
	events, _ := rec.Since(context.Background(), time.Time{})
	if len(events) != 0 {
		t.Fatal("refused approval created an event")
	}
	r := keyedRequest(f.handler, path, `{"commit":"`+approvedHead+`"}`, "approve-once")
	if r.Code != 200 {
		t.Fatalf("approve=%d %s", r.Code, r.Body)
	}
	settleApproval(t, f)
	events, _ = rec.Since(context.Background(), time.Time{})
	if len(events) != 1 || events[0].Commit != approvedHead {
		t.Fatalf("approval=%+v", events)
	}
	trailer, err := rec.Trailer(context.Background(), it.ID)
	if err != nil || trailer != "Factory-Approval: "+events[0].ID+" @sam "+approvedHead {
		t.Fatalf("trailer=%q, %v", trailer, err)
	}
	var job api.Job
	f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "", nil, 200, &job)
	if job.Publishing == nil || job.Publishing.State != "queued" || job.Publishing.Code != "machine_offline" {
		t.Fatalf("offline publish=%+v", job.Publishing)
	}
}

func TestFailedPublishRetriesSameApprovalAndOpensOnlyOnce(t *testing.T) {
	f, rec, it, _, _ := publishingSetup(t)
	worker := f.attach(t, "sam")
	path := fmt.Sprintf("/v1/jobs/%d", it.ID)
	approved := keyedRequest(f.handler, path+"/approve", `{"commit":"`+approvedHead+`"}`, "approval")
	if approved.Code != 200 {
		t.Fatalf("approve=%d %s", approved.Code, approved.Body)
	}
	if again := keyedRequest(f.handler, path+"/approve", `{}`, "approval"); again.Body.String() != approved.Body.String() {
		t.Fatal("double approval did not replay")
	}
	settleApproval(t, f)
	pulled := f.pull(t, worker)
	if len(pulled.Signals) != 1 || pulled.Signals[0].Verb != lease.SignalPublish {
		t.Fatalf("signals=%+v", pulled.Signals)
	}
	reportPath := "/v1/workers/leases/" + pulled.Lease.ID
	for _, state := range []string{"pushing", "failed"} {
		p := &factory.Publishing{State: state, Attempt: 1, At: now}
		if state == "failed" {
			p.Code, p.Detail = "no_push_permission", "permission denied"
		}
		f.request(t, "POST", reportPath+"/report", worker.Credential, api.WorkerReport{Publishing: p}, 200, nil)
	}
	f.request(t, "POST", reportPath+"/done", worker.Credential, api.WorkerDone{State: api.StateNeedsYou, Ready: true}, 200, nil)
	var failed api.Job
	f.request(t, "GET", path, "", nil, 200, &failed)
	if failed.Gate != "publish_failed" || failed.State != api.StateNeedsYou || failed.Publishing.Code != "no_push_permission" {
		t.Fatalf("failed=%+v", failed)
	}
	retried := keyedRequest(f.handler, path+"/retry_publish", "", "retry")
	if retried.Code != 200 {
		t.Fatalf("retry=%d %s", retried.Code, retried.Body)
	}
	if again := keyedRequest(f.handler, path+"/retry_publish", "", "retry"); again.Body.String() != retried.Body.String() {
		t.Fatal("retry did not replay")
	}
	pulled = f.pull(t, worker)
	if len(pulled.Signals) != 1 || pulled.Job.Publishing.Attempt != 2 {
		t.Fatalf("retry=%+v", pulled)
	}
	reportPath = "/v1/workers/leases/" + pulled.Lease.ID
	pr := &api.PR{Number: 8, URL: "https://github.com/owner/repo/pull/8"}
	f.request(t, "POST", reportPath+"/report", worker.Credential, api.WorkerReport{PR: pr, Publish: "pr"}, 200, nil)
	f.request(t, "POST", reportPath+"/done", worker.Credential, api.WorkerDone{State: api.StateDone}, 200, nil)
	var opened api.Job
	f.request(t, "GET", path, "", nil, 200, &opened)
	if opened.State != api.StateRunning || opened.Publishing.State != "opened" || opened.Publishing.PR.Number != 8 || opened.Publishing.Attempt != 2 {
		t.Fatalf("opened=%+v", opened)
	}
	events, _ := rec.Since(context.Background(), time.Time{})
	if len(events) != 3 || events[1].Kind != record.Approved || events[2].Kind != record.Posted || events[0].Kind != record.RunnerAdded {
		t.Fatalf("events=%+v", events)
	}
	var table struct{ Codes []publish.Code }
	f.request(t, "GET", "/v1/publish/codes", "", nil, 200, &table)
	if len(table.Codes) != 10 {
		t.Fatalf("codes=%+v", table)
	}
}

func TestRetryRefusesChangedCommitWithoutAnotherApproval(t *testing.T) {
	f, rec, it, _, _ := publishingSetup(t)
	_, err := rec.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "sam", Commit: approvedHead})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.HeadCommit = movedHead
		it.MarkPublishing("failed", "unknown", "failed", now)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := keyedRequest(f.handler, fmt.Sprintf("/v1/jobs/%d/retry_publish", it.ID), "", "retry-stale")
	if r.Code != 409 {
		t.Fatalf("retry=%d %s", r.Code, r.Body)
	}
	saved, _ := f.st.Get(it.ID)
	if saved.PublishStatus.State != "failed" {
		t.Fatal("stale retry queued publishing")
	}
	events, _ := rec.Since(context.Background(), time.Time{})
	if len(events) != 1 {
		t.Fatal("retry created approval")
	}
}

func TestConcurrentDoubleApprovalAcrossServersCreatesOneReceipt(t *testing.T) {
	f, rec, it, seam, cfg := publishingSetup(t)
	other := serve.New(seam, cfg)
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for _, h := range []*serve.Handler{f.handler, other} {
		wg.Add(1)
		go func(h *serve.Handler) {
			defer wg.Done()
			responses <- keyedRequest(h, fmt.Sprintf("/v1/jobs/%d/approve", it.ID), `{"commit":"`+approvedHead+`"}`, "")
		}(h)
	}
	wg.Wait()
	close(responses)
	ok, conflicts := 0, 0
	for r := range responses {
		switch r.Code {
		case 200:
			ok++
		case 409:
			conflicts++
		default:
			t.Fatalf("approve=%d %s", r.Code, r.Body)
		}
	}
	settleApproval(t, f)
	events, _ := rec.Since(context.Background(), time.Time{})
	if ok != 1 || conflicts != 1 || len(events) != 1 {
		t.Fatalf("ok=%d conflicts=%d events=%+v", ok, conflicts, events)
	}
}

func settleApproval(t *testing.T, f *workerFixture) {
	t.Helper()
	f.clock.Add(int64(factory.ApprovalUndo))
	f.request(t, "GET", "/v1/queue", "", nil, 200, nil)
}
