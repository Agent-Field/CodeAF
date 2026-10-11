package serve_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type workerFixture struct {
	st      *store.Store
	handler *serve.Handler
	server  *httptest.Server
	clock   atomic.Int64
}

func workerSetup(t *testing.T) *workerFixture {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &workerFixture{st: st}
	f.clock.Store(now.UnixNano())
	cfg := config
	cfg.Store, cfg.PullWait = st, time.Millisecond
	cfg.LeaseOptions = lease.Options{Now: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }}
	seam := factory.Seam{Load: func() (factory.Snapshot, error) {
		items, err := st.List()
		return factory.Snapshot{Items: items}, err
	}}
	f.handler = serve.New(seam, cfg)
	f.server = httptest.NewServer(f.handler)
	t.Cleanup(f.server.Close)
	return f
}

func (f *workerFixture) add(t *testing.T, person string) factory.Item {
	t.Helper()
	it, err := f.st.Create(factory.Item{Title: "fix the job", Repo: "owner/repo", State: factory.StateQueued, Assignee: person,
		Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true, Until: factory.UntilDone}, {Name: "approve", Kind: factory.StageGate, On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func (f *workerFixture) request(t *testing.T, method, path, credential string, body any, status int, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	res, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: %d %s; want %d", method, path, res.StatusCode, data, status)
	}
	if status == http.StatusNoContent {
		if len(data) != 0 {
			t.Fatalf("204 has a body: %s", data)
		}
		return
	}
	var identity struct {
		Factory api.Factory `json:"factory"`
	}
	if err := json.Unmarshal(data, &identity); err != nil || identity.Factory.Version != config.Version {
		t.Fatalf("response has no factory identity: %s, %v", data, err)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *workerFixture) attach(t *testing.T, handle string) api.WorkerAttached {
	t.Helper()
	var attached api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Name: "laptop", Handle: handle}, 201, &attached)
	if attached.WorkerID == "" || attached.Credential == "" {
		t.Fatal("attach omitted identity or credential")
	}
	return attached
}

func (f *workerFixture) pull(t *testing.T, worker api.WorkerAttached) api.WorkerPull {
	t.Helper()
	var pulled api.WorkerPull
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 200, &pulled)
	if pulled.Lease.ID == "" || pulled.Lease.Worker != worker.WorkerID {
		t.Fatalf("bad lease: %+v", pulled.Lease)
	}
	return pulled
}

func TestWorkerAttachPullHeartbeatReportAndDone(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	first, second := f.attach(t, "@SAM"), f.attach(t, "sam")
	if first.Credential == second.Credential || first.WorkerID == second.WorkerID {
		t.Fatal("attach reused credentials")
	}
	pulled := f.pull(t, first)
	if pulled.Job.ID != job.ID || pulled.Job.State != api.StateRunning || pulled.Lease.Job != job.ID || pulled.Lease.Handle != "sam" {
		t.Fatalf("pull returned the wrong job: %+v", pulled)
	}
	f.request(t, "GET", "/v1/workers/pull", second.Credential, nil, 204, nil)
	f.request(t, "GET", "/v1/workers/pull", first.Credential, nil, 204, nil)
	endpoint := "/v1/workers/leases/" + pulled.Lease.ID
	f.clock.Add(int64(15 * time.Second))
	var heartbeat struct {
		Lease api.WorkerLease `json:"lease"`
	}
	f.request(t, "POST", endpoint+"/heartbeat", first.Credential, nil, 200, &heartbeat)
	if !heartbeat.Lease.ExpiresAt.Equal(pulled.Lease.ExpiresAt.Add(15 * time.Second)) {
		t.Fatalf("heartbeat did not extend: %+v", heartbeat)
	}
	report := api.WorkerReport{
		Steps: []api.StepReport{{Name: "proof", State: api.StepDone, Round: 2, Result: &api.StepResult{
			Done: true, Output: "tests pass\nwith evidence", Cost: .25, Exit: 0, Findings: 1, Chat: "step-chat", Notes: []string{"keep this for review"},
			Claims: []api.Claim{{Text: "tests pass", Proven: true, Evidence: &api.Evidence{Kind: "test", Ref: "go test ./..."}}},
		}}},
		Log: []api.LogLine{{Tone: "test", Text: "ran the checks"}}, Cost: .25,
		Claims:   []api.Claim{{Text: "policy allows this", Proven: true, Evidence: &api.Evidence{Kind: "policy", Ref: "repo rules"}}},
		Question: &api.Question{Text: "publish this?", FromStep: "approve", Kind: factory.QKindApprove, Options: []string{"yes", "no"}},
		PR:       &api.PR{Number: 42, URL: "https://github.com/owner/repo/pull/42", CI: "green"},
	}
	f.request(t, "POST", endpoint+"/report", first.Credential, report, 200, nil)
	it, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if it.State != factory.StateNeedsYou || it.Stream.Spent != .25 || it.Question != "publish this?" || it.QKind != factory.QKindApprove || it.Asking.With != factory.AskedYou {
		t.Fatalf("report did not write state/cost/question: %+v", it)
	}
	ph := it.Stream.Phases[0]
	if ph.State != factory.PhaseDone || ph.Round != 2 || ph.Chat != "step-chat" || ph.Result == nil || !ph.Result.Done || ph.Result.Output != report.Steps[0].Result.Output || !reflect.DeepEqual(ph.Result.Notes, report.Steps[0].Result.Notes) {
		t.Fatalf("step result lost facts: %+v", ph)
	}
	if len(it.Proof) != 1 || !it.Proof[0].OK || len(it.Policy) != 1 || it.PullRequest.Number != 42 {
		t.Fatalf("report lost evidence or PR: %+v", it)
	}
	found := false
	for _, line := range it.Stream.Log {
		if line.Text == "ran the checks" {
			found = true
			if !line.At.Equal(time.Unix(0, f.clock.Load())) {
				t.Fatal("log was not stamped by factory")
			}
		}
	}
	if !found {
		t.Fatal("report lost log lines")
	}
	var got api.Job
	f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", job.ID), "", nil, 200, &got)
	if got.PR == nil || got.PR.Number != 42 || got.Cost != .25 || len(got.Claims) != 1 || len(got.Questions) == 0 {
		t.Fatalf("job read lost report facts: %+v", got)
	}
	restored := api.ToItem(got)
	if !reflect.DeepEqual(restored.PullRequest, it.PullRequest) {
		t.Fatal("PR lost in codec round trip")
	}
	f.request(t, "POST", endpoint+"/done", first.Credential, api.WorkerDone{State: api.StateDone}, 400, nil)
	f.request(t, "POST", endpoint+"/done", first.Credential, api.WorkerDone{State: api.StateNeedsYou, Ready: true}, 200, nil)
	it, err = f.st.Get(job.ID)
	if err != nil || it.State != factory.StateLanded || it.Stream.Ended.IsZero() {
		t.Fatalf("done did not land: %+v %v", it, err)
	}
	f.request(t, "POST", endpoint+"/heartbeat", first.Credential, nil, 409, nil)
	f.request(t, "POST", endpoint+"/report", first.Credential, report, 409, nil)
}

func TestWorkerCredentialsAndLeaseOwnership(t *testing.T) {
	f := workerSetup(t)
	f.add(t, "sam")
	worker, stranger := f.attach(t, "sam"), f.attach(t, "sam")
	pulled := f.pull(t, worker)
	endpoint := "/v1/workers/leases/" + pulled.Lease.ID
	for _, credential := range []string{"", "floor-secret", "wrong"} {
		f.request(t, "GET", "/v1/workers/pull", credential, nil, 401, nil)
		for _, verb := range []string{"heartbeat", "report", "done"} {
			f.request(t, "POST", endpoint+"/"+verb, credential, map[string]any{}, 401, nil)
		}
	}
	for _, verb := range []string{"heartbeat", "report", "done"} {
		f.request(t, "POST", endpoint+"/"+verb, stranger.Credential, map[string]any{}, 403, nil)
	}
	// Off-loopback attach uses the floor token; execution uses only the worker
	// credential and cannot grant access to ordinary floor reads.
	for _, tc := range []struct {
		path, method, auth string
		status             int
	}{
		{"/v1/workers/attach", "POST", "", 401},
		{"/v1/workers/attach", "POST", "floor-secret", 201},
		{"/v1/workers/pull", "GET", worker.Credential, 204},
		{"/v1/queue", "GET", worker.Credential, 401},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"name":"remote","handle":"sam"}`))
		r.RemoteAddr = "192.0.2.1:1234"
		if tc.auth != "" {
			r.Header.Set("Authorization", "Bearer "+tc.auth)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("remote %s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/v1/workers/pull", nil)
	r.Header.Add("Authorization", "Bearer "+worker.Credential)
	r.Header.Add("Authorization", "Bearer "+worker.Credential)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("duplicate authorization accepted")
	}
}

func TestWorkerExpiredReportsCannotWriteToReassignedJob(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	worker := f.attach(t, "sam")
	old := f.pull(t, worker)
	f.clock.Add(int64(lease.DefaultTTL))
	newer := f.pull(t, worker)
	if newer.Lease.ID == old.Lease.ID || newer.Job.ID != job.ID {
		t.Fatal("re-pull did not mint a new hold")
	}
	for _, verb := range []string{"heartbeat", "report", "done"} {
		f.request(t, "POST", "/v1/workers/leases/"+old.Lease.ID+"/"+verb, worker.Credential, map[string]any{}, 409, nil)
	}
	it, err := f.st.Get(job.ID)
	if err != nil || it.State != factory.StateRunning || it.Stream != nil {
		t.Fatalf("stale report changed job: %+v %v", it, err)
	}
}

func TestWorkerReportInvalidInputWritesNothing(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	worker := f.attach(t, "sam")
	pulled := f.pull(t, worker)
	path := "/v1/workers/leases/" + pulled.Lease.ID + "/report"
	before, err := f.st.Revision(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []any{
		api.WorkerReport{Cost: -1},
		api.WorkerReport{Cost: 1, Steps: []api.StepReport{{Name: "missing"}}},
		api.WorkerReport{Cost: 1, Steps: []api.StepReport{{Name: "approve", State: api.StepDone}}},
		api.WorkerReport{Cost: 1, Question: &api.Question{}},
		api.WorkerReport{Cost: 1, PR: &api.PR{Number: 0}},
		map[string]any{"item": map[string]any{"state": "done"}},
	} {
		f.request(t, "POST", path, worker.Credential, body, 400, nil)
	}
	after, err := f.st.Revision(job.ID)
	if err != nil || after != before {
		t.Fatalf("invalid report was persisted: revisions %d/%d %v", before, after, err)
	}
}

func TestWorkerRoutingOptInAndCancellation(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "other")
	worker := f.attach(t, "sam")
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	f.add(t, "")
	f.request(t, "GET", "/v1/workers/pull", worker.Credential, nil, 204, nil)
	var opt api.WorkerAttached
	f.request(t, "POST", "/v1/workers/attach", "", api.WorkerAttach{Name: "desktop", Handle: "sam", TakesUnassigned: map[string]bool{"owner/repo": true}}, 201, &opt)
	pulled := f.pull(t, opt)
	if pulled.Job.ID == job.ID {
		t.Fatal("opt-in took another person's job")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/v1/workers/pull", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+worker.Credential)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Body.Len() != 0 {
		t.Fatal("canceled pull wrote a body")
	}
}

func TestWorkerConcurrentPullsOfferJobOnce(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	workers := []api.WorkerAttached{f.attach(t, "sam"), f.attach(t, "sam")}
	type response struct {
		status int
		pulled api.WorkerPull
		err    error
	}
	results := make(chan response, len(workers))
	start := make(chan struct{})
	for _, worker := range workers {
		go func() {
			<-start
			req, err := http.NewRequest("GET", f.server.URL+"/v1/workers/pull", nil)
			if err != nil {
				results <- response{err: err}
				return
			}
			req.Header.Set("Authorization", "Bearer "+worker.Credential)
			res, err := f.server.Client().Do(req)
			if err != nil {
				results <- response{err: err}
				return
			}
			defer res.Body.Close()
			got := response{status: res.StatusCode}
			if res.StatusCode == 200 {
				got.err = json.NewDecoder(res.Body).Decode(&got.pulled)
			}
			results <- got
		}()
	}
	close(start)
	count := map[int]int{}
	for range workers {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		count[got.status]++
		if got.status == 200 && got.pulled.Job.ID != job.ID {
			t.Fatal("pulled another job")
		}
	}
	if count[200] != 1 || count[204] != 1 {
		t.Fatalf("concurrent pulls = %v", count)
	}
}

func TestWorkerReportKeepsRunnerOutputAndLogLimits(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	worker := f.attach(t, "sam")
	pulled := f.pull(t, worker)
	report := api.WorkerReport{Steps: []api.StepReport{{Name: "proof", Result: &api.StepResult{Output: strings.Repeat("x", factory.ResultOutputMost+100)}}}}
	// One accepted report exercises the bounded log without exceeding the
	// request body's own limit or introducing a real-time wait.
	for i := range 500 {
		report.Log = append(report.Log, api.LogLine{Text: fmt.Sprintf("line %d", i)})
	}
	f.request(t, "POST", "/v1/workers/leases/"+pulled.Lease.ID+"/report", worker.Credential, report, 200, nil)
	it, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Stream.Log) != 400 || it.Stream.Log[0].Text != "line 100" || len(it.Stream.Phases[0].Result.Output) != factory.ResultOutputMost {
		t.Fatalf("report did not apply the runner limits: lines=%d output=%d", len(it.Stream.Log), len(it.Stream.Phases[0].Result.Output))
	}
}

func TestWorkerLongPollFindsArrivingJob(t *testing.T) {
	f := workerSetup(t)
	cfg := config
	cfg.Store, cfg.PullWait = f.st, time.Second
	h := serve.New(factory.Seam{}, cfg)
	s := httptest.NewServer(h)
	defer s.Close()
	other := &workerFixture{st: f.st, handler: h, server: s}
	worker := other.attach(t, "sam")
	sent := make(chan struct{})
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { close(sent) }}
	ctx := httptrace.WithClientTrace(context.Background(), trace)
	req, err := http.NewRequestWithContext(ctx, "GET", s.URL+"/v1/workers/pull", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+worker.Credential)
	type response struct {
		res *http.Response
		err error
	}
	result := make(chan response, 1)
	go func() { res, err := s.Client().Do(req); result <- response{res, err} }()
	<-sent
	job := f.add(t, "sam")
	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer got.res.Body.Close()
	if got.res.StatusCode != 200 {
		t.Fatalf("waiting worker missed arrival: %d", got.res.StatusCode)
	}
	var pulled api.WorkerPull
	if err := json.NewDecoder(got.res.Body).Decode(&pulled); err != nil {
		t.Fatal(err)
	}
	if pulled.Job.ID != job.ID {
		t.Fatal("long poll returned another job")
	}
}
