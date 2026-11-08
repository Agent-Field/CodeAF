package agentfield

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// fakePlane is the control plane's API, exactly the calls the node makes,
// recording each.
type fakePlane struct {
	*httptest.Server
	key string

	mu       sync.Mutex
	regs     []Registration
	leases   []map[string]any
	reports  map[string][]ExecutionUpdate
	forget   bool // answer the next lease 404, as a control plane that forgot the node
	triggers []NewTrigger
}

func newFakePlane(t *testing.T, key string) *fakePlane {
	return newFakePlaneWith(t, key, nil, nil)
}

// newFakePlaneWith is a fake plane with extra routes, and down saying when
// every HTTP call fails as a control plane that is not there.
func newFakePlaneWith(t *testing.T, key string, extra func(*http.ServeMux), down func() bool) *fakePlane {
	p := &fakePlane{key: key, reports: map[string][]ExecutionUpdate{}}
	mux := http.NewServeMux()
	if extra != nil {
		extra(mux)
	}
	mux.HandleFunc("GET /api/v1/sources", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"sources": []Source{{Name: "generic_bearer", Kind: "http", SecretRequired: true, ConfigSchema: json.RawMessage(`{"type":"object"}`)}}})
	})
	mux.HandleFunc("POST /api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		var reg Registration
		_ = json.NewDecoder(r.Body).Decode(&reg)
		p.mu.Lock()
		p.regs = append(p.regs, reg)
		p.mu.Unlock()
		writeJSON(w, 201, map[string]any{"success": true})
	})
	mux.HandleFunc("PATCH /api/v1/nodes/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.mu.Lock()
		forget := p.forget
		p.forget = false
		p.leases = append(p.leases, body)
		p.mu.Unlock()
		if forget {
			writeJSON(w, 404, map[string]any{"error": "node not found"})
			return
		}
		writeJSON(w, 200, map[string]any{"lease_seconds": 300, "next_lease_renewal": time.Now().Add(5 * time.Minute).Format(time.RFC3339)})
	})
	mux.HandleFunc("POST /api/v1/executions/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		var u ExecutionUpdate
		_ = json.NewDecoder(r.Body).Decode(&u)
		p.mu.Lock()
		p.reports[r.PathValue("id")] = append(p.reports[r.PathValue("id")], u)
		p.mu.Unlock()
		writeJSON(w, 200, map[string]any{"status": u.Status})
	})
	mux.HandleFunc("POST /api/v1/triggers", func(w http.ResponseWriter, r *http.Request) {
		var nt NewTrigger
		_ = json.NewDecoder(r.Body).Decode(&nt)
		p.mu.Lock()
		p.triggers = append(p.triggers, nt)
		p.mu.Unlock()
		writeJSON(w, 201, Trigger{ID: "trig-1", SourceName: nt.SourceName, TargetNodeID: nt.TargetNodeID, TargetReasoner: nt.TargetReasoner, Enabled: true})
	})
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down != nil && down() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "down"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/link") {
			mux.ServeHTTP(w, r)
			return
		}
		if key != "" && r.Header.Get("X-API-Key") != key {
			writeJSON(w, 401, map[string]any{"error": "unauthorized", "message": "invalid or missing API key"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *fakePlane) registrations() []Registration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Registration(nil), p.regs...)
}

func (p *fakePlane) reportsFor(id string) []ExecutionUpdate {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ExecutionUpdate(nil), p.reports[id]...)
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type world struct {
	t      *testing.T
	plane  *fakePlane
	floor  *store.Store
	node   *Node
	base   string // the node's listener
	nudges int
	ahead  time.Duration // how far the node's clock runs ahead of now
	filed  []int         // every job the node said it filed
	mu     sync.Mutex
}

// newWorld is a connected, started node over a temp floor and profile,
// against a fake control plane.
func newWorld(t *testing.T, trust factory.Tier) *world {
	t.Helper()
	w := &world{t: t, plane: newFakePlane(t, "af-key-1")}
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w.floor = floor
	conn := &Connection{Store: floor, Profile: t.TempDir()}
	s, err := Clean(Settings{URL: w.plane.URL, BaseURL: "http://127.0.0.1:1", Listen: "127.0.0.1:0", Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Keep(s, "af-key-1", "itok-1"); err != nil {
		t.Fatal(err)
	}
	w.node = &Node{Conn: conn, Store: floor, Version: NodeVersion, Build: "test",
		Nudge: func() { w.mu.Lock(); w.nudges++; w.mu.Unlock() },
		Filed: func(job int) { w.mu.Lock(); w.filed = append(w.filed, job); w.mu.Unlock() },
		Clock: func() time.Time { w.mu.Lock(); defer w.mu.Unlock(); return time.Now().Add(w.ahead) }}
	if err := w.node.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.node.Stop(); w.node.Forget() })
	eventually(t, "the registration", func() bool { return len(w.plane.registrations()) > 0 })
	w.base = "http://" + w.node.Health().Listening
	return w
}

func (w *world) post(path string, headers map[string]string, body string) (int, map[string]any) {
	w.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, w.base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// THE CONTROL PLANE'S BEARER, as its trigger dispatcher now sends it.
	req.Header.Set("Authorization", "Bearer itok-1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// dispatch is what the control plane's trigger dispatcher sends.
func dispatch(source string, event any, key string) string {
	raw, _ := json.Marshal(map[string]any{"event": event, "_meta": map[string]any{"trigger_id": "trig-1", "source": source, "event_type": "x", "event_id": "ev-1", "idempotency_key": key, "received_at": "2026-11-06T15:00:00Z"}})
	return string(raw)
}

func TestTheRegistrationNamesThreeReasonersAndAPlainBase(t *testing.T) {
	w := newWorld(t, "")
	reg := w.plane.registrations()[0]
	if secret := config.AgentFieldNodeSecretAt(w.node.Conn.Profile); secret != "" {
		t.Fatalf("a fresh connect kept an address secret")
	}
	if reg.ID != DefaultNodeID || reg.BaseURL != "http://127.0.0.1:1" || reg.Version != NodeVersion || reg.InstanceID == "" {
		t.Fatalf("registration %s %s %s %q", reg.ID, reg.BaseURL, reg.Version, reg.InstanceID)
	}
	if reg.CallbackDiscovery["mode"] != "manual" || reg.DeploymentType != "long_running" {
		t.Fatalf("discovery %v deployment %q", reg.CallbackDiscovery, reg.DeploymentType)
	}
	var names []string
	for _, r := range reg.Reasoners {
		names = append(names, r.ID+"="+*r.AcceptsWebhook)
		var schema map[string]any
		if json.Unmarshal(r.InputSchema, &schema) != nil || json.Unmarshal(r.OutputSchema, &schema) != nil {
			t.Fatalf("reasoner %s carries a schema that is not JSON", r.ID)
		}
	}
	if strings.Join(names, ",") != "intake=true,message=false,status=false" {
		t.Fatalf("reasoners %v", names)
	}
	// A NEW START IS A NEW INSTANCE.
	first := reg.InstanceID
	if err := w.node.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the second registration", func() bool { return len(w.plane.registrations()) > 1 })
	if again := w.plane.registrations()[1].InstanceID; again == first || again == "" {
		t.Fatalf("a restart registered instance %q, the first was %q", again, first)
	}
}

func TestTheLeaseIsRenewedAndAForgottenNodeRegistersAgain(t *testing.T) {
	w := newWorld(t, "")
	eventually(t, "a lease", func() bool { return !w.node.Health().LeaseUntil.IsZero() })
	w.plane.mu.Lock()
	if w.plane.leases[0]["phase"] != "ready" {
		t.Fatalf("lease %v", w.plane.leases[0])
	}
	w.plane.mu.Unlock()
	if h := w.node.Health(); h.LeaseSeconds != 300 || h.Registered.IsZero() {
		t.Fatalf("health %+v", h)
	}
	if got := leaseEvery(300); got != 100*time.Second {
		t.Fatalf("a 300 s lease is renewed every %s, want a third of it", got)
	}
	// The control plane forgets the node: the next lease answers 404 and the
	// node registers again.
	w.plane.mu.Lock()
	w.plane.forget = true
	w.plane.mu.Unlock()
	w.node.mu.Lock()
	r := w.node.running
	w.node.mu.Unlock()
	lease, err := r.client.Renew(context.Background(), DefaultNodeID, "ready", NodeVersion, nil)
	if !RefusedAs(err, RefusedMissing) {
		t.Fatalf("a forgotten node's lease: %+v %v", lease, err)
	}
	// Stop says offline on the way out.
	w.node.Stop()
	w.plane.mu.Lock()
	last := w.plane.leases[len(w.plane.leases)-1]
	w.plane.mu.Unlock()
	if last["phase"] != "offline" {
		t.Fatalf("the last lease on stop is %v, want offline", last)
	}
}

// A DISPATCH WITHOUT THE CONTROL PLANE'S BEARER IS REFUSED: no bearer, a
// wrong one, the API key in its place, an X-API-Key, a cancel, and a path
// under an old secret prefix when none is kept. /health stays open for the
// control plane's probe.
func TestADispatchWithoutTheBearerIsRefused(t *testing.T) {
	w := newWorld(t, "")
	body := dispatch("generic_bearer", map[string]any{"title": "x"}, "k")
	for _, tc := range []struct {
		name, path string
		hdr        map[string]string
		code       int
	}{
		{"no bearer", "/reasoners/intake", nil, http.StatusUnauthorized},
		{"wrong bearer", "/reasoners/intake", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"the API key as bearer", "/reasoners/intake", map[string]string{"Authorization": "Bearer af-key-1"}, http.StatusUnauthorized},
		{"X-API-Key", "/reasoners/intake", map[string]string{"X-API-Key": "itok-1"}, http.StatusUnauthorized},
		{"a cancel", "/_internal/executions/x/cancel", nil, http.StatusUnauthorized},
		{"an old prefix with none kept", "/n/" + strings.Repeat("0", 64) + "/reasoners/intake", map[string]string{"Authorization": "Bearer itok-1"}, http.StatusNotFound},
	} {
		req, _ := http.NewRequest(http.MethodPost, w.base+tc.path, strings.NewReader(body))
		for k, v := range tc.hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.code {
			t.Errorf("%s: %s answered %d, want %d", tc.name, tc.path, resp.StatusCode, tc.code)
		}
	}
	items, _ := w.floor.ListSaved()
	if len(items) != 0 {
		t.Fatalf("a refused call filed %d jobs", len(items))
	}
	resp, err := http.Get(w.base + "/health")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("health: %v %v", resp, err)
	}
	resp.Body.Close()
}

// THE OLD PREFIX STILL ANSWERS FOR ONE RELEASE when its secret is kept, with
// no bearer, because a control plane that registered it sends none.
func TestTheLegacyPrefixStillAnswersWhenKept(t *testing.T) {
	var got string
	h := Guarded("tok", "legacy-secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.URL.Path }))
	for path, want := range map[string]int{"/n/legacy-secret/reasoners/intake": 200, "/n/other/reasoners/intake": 404, "/reasoners/intake": 401} {
		got = ""
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != want {
			t.Errorf("%s answered %d, want %d", path, rec.Code, want)
		}
		if want == 200 && got != "/reasoners/intake" {
			t.Errorf("%s reached %q, want the prefix taken off", path, got)
		}
	}
	// No token kept refuses every reasoner call, a bearer or not.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/reasoners/intake", nil)
	req.Header.Set("Authorization", "Bearer ")
	Guarded("", "", http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token kept answered %d", rec.Code)
	}
}

// GitHub stays native: a github event wakes the GitHub read for a watched
// repository and files nothing, and an unwatched one does neither.
func TestAGitHubEventNudgesTheWatchedRepositoryAndFilesNothing(t *testing.T) {
	w := newWorld(t, "")
	if err := w.floor.SetRepos([]string{"santoshkumarradha/factory-demo"}); err != nil {
		t.Fatal(err)
	}
	var gh map[string]any
	_ = json.Unmarshal(fixture(t, "github_issues_opened.json"), &gh)
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-gh"}, dispatch("github", gh, "delivery-1"))
	if code != 200 || out["filed"] != false || out["nudged"] != "santoshkumarradha/factory-demo" || w.nudges != 1 {
		t.Fatalf("a watched repository's event: %d %v nudges=%d", code, out, w.nudges)
	}
	gh["repository"] = map[string]any{"full_name": "someone/else"}
	code, out = w.post("/reasoners/intake", nil, dispatch("github", gh, "delivery-2"))
	if code != 200 || out["filed"] != false || w.nudges != 1 {
		t.Fatalf("an unwatched repository's event: %d %v nudges=%d", code, out, w.nudges)
	}
	items, _ := w.floor.ListSaved()
	if len(items) != 0 || len(w.node.Open()) != 0 {
		t.Fatalf("a github event filed %d jobs and kept %d executions", len(items), len(w.node.Open()))
	}
}

func TestACronTickFilesNoJob(t *testing.T) {
	w := newWorld(t, "")
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-cron"}, dispatch("cron", map[string]any{"fired_at": "2026-11-06T15:05:00Z", "expression": "* * * * *", "timezone": "UTC"}, "* * * * *@x"))
	if code != 200 || out["filed"] != false {
		t.Fatalf("a cron tick: %d %v", code, out)
	}
	if items, _ := w.floor.ListSaved(); len(items) != 0 {
		t.Fatalf("a cron tick filed %d jobs", len(items))
	}
	if h := w.node.Health(); h.LastEventSource != "agentfield:cron" || !strings.Contains(h.LastEventWhat, "filed nothing") {
		t.Fatalf("health %+v", h)
	}
}

// THE COMPLETION: intake answers 202 and keeps the execution; the job's
// pull request completes it with {job, state, pr_url, steps_done,
// steps_total}; a second report changes nothing.
func TestTheIntakeAnswers202AndThePullRequestCompletesTheExecution(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	entry := map[string]any{"title": "AFNODE: add Len", "body": "In ledger.go add Len.", "repo": "santoshkumarradha/factory-demo"}
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-1"}, dispatch("generic_bearer", entry, "idem-1"))
	if code != http.StatusAccepted || out["created"] != true {
		t.Fatalf("intake: %d %v", code, out)
	}
	job := int(out["job"].(float64))
	it, err := w.floor.Get(job)
	if err != nil || it.SourceRef != "agentfield:generic-bearer:idem-1" || it.Tier != factory.TierOwner || it.Repo != "santoshkumarradha/factory-demo" {
		t.Fatalf("job %+v %v", it, err)
	}
	if open := w.node.Open(); len(open) != 1 || open[0].ID != "exec-1" || open[0].Job != job {
		t.Fatalf("open executions %+v", open)
	}
	eventually(t, "a running report", func() bool { return len(w.plane.reportsFor("exec-1")) > 0 })
	if first := w.plane.reportsFor("exec-1")[0]; first.Status != ExecRunning || first.Progress == nil {
		t.Fatalf("first report %+v", first)
	}

	const pr = "https://github.com/santoshkumarradha/factory-demo/pull/9"
	if err := w.floor.Update(job, func(it *factory.Item) error {
		it.PullRequest = &factory.PullRequest{URL: pr, Number: 9, State: factory.PROpen}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The bus carries the moment, as the lifecycle sends it.
	inbox.Notify(context.Background(), w.floor, job, inbox.Opened(pr))
	eventually(t, "the completion", func() bool {
		r := w.plane.reportsFor("exec-1")
		return len(r) > 0 && r[len(r)-1].Status == ExecSucceeded
	})
	reports := w.plane.reportsFor("exec-1")
	done := reports[len(reports)-1]
	if done.Result["pr_url"] != pr || int(done.Result["job"].(float64)) != job || done.Result["state"] == nil || done.Result["steps_total"] == nil || done.CompletedAt == nil {
		t.Fatalf("completion %+v", done)
	}
	if len(w.node.Open()) != 0 {
		t.Fatalf("the completed execution is still open")
	}
	time.Sleep(100 * time.Millisecond)
	if again := w.plane.reportsFor("exec-1"); len(again) != len(reports) {
		t.Fatalf("a completed execution was told again: %d reports, then %d", len(reports), len(again))
	}

	// The same delivery again is the same job.
	code, out = w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-2"}, dispatch("generic_bearer", entry, "idem-1"))
	if code != http.StatusAccepted || out["created"] != false || int(out["job"].(float64)) != job {
		t.Fatalf("a repeat: %d %v", code, out)
	}
	eventually(t, "the repeat's completion", func() bool {
		r := w.plane.reportsFor("exec-2")
		return len(r) > 0 && r[len(r)-1].Status == ExecSucceeded
	})
}

// A job a trigger files is told at once, so the desk's Backlog reads it then
// rather than on its next slow refresh (factory.jobs_changed in the app).
func TestAFiledJobIsToldAtOnce(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	entry := map[string]any{"title": "AFNODE: add Len", "body": "In ledger.go add Len.", "repo": "santoshkumarradha/factory-demo"}
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-1"}, dispatch("generic_bearer", entry, "idem-1"))
	if code != http.StatusAccepted || out["created"] != true {
		t.Fatalf("intake: %d %v", code, out)
	}
	job := int(out["job"].(float64))
	eventually(t, "the job told as filed", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.filed) > 0 && w.filed[0] == job
	})
}

func TestAStoppedJobFailsItsExecutionWithTheJobsWords(t *testing.T) {
	w := newWorld(t, "")
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-s"}, dispatch("generic_bearer", map[string]any{"title": "Fix it"}, "idem-s"))
	if code != http.StatusAccepted {
		t.Fatalf("intake: %d %v", code, out)
	}
	job := int(out["job"].(float64))
	_ = w.floor.Update(job, func(it *factory.Item) error {
		it.State = factory.StateNeedsYou
		it.Failure = &factory.Failure{Step: "test", Detail: "go test failed twice"}
		return nil
	})
	// The bus carries the moment, as the lifecycle sends it.
	inbox.Notify(context.Background(), w.floor, job, inbox.Stuck(""))
	eventually(t, "the failure", func() bool {
		r := w.plane.reportsFor("exec-s")
		return len(r) > 0 && r[len(r)-1].Status == ExecFailed
	})
	r := w.plane.reportsFor("exec-s")
	if last := r[len(r)-1]; !strings.Contains(last.Error, "go test failed twice") || last.Result["job"] == nil {
		t.Fatalf("failure %+v", last)
	}
}

// A refused entry answers 422 in the inbox's words and keeps no execution.
func TestARefusedEntryAnswers422(t *testing.T) {
	w := newWorld(t, "")
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-bad"}, dispatch("generic_bearer", map[string]any{"title": "x", "repo": "not a repo"}, "k"))
	if code != http.StatusUnprocessableEntity || !strings.Contains(out["error"].(string), "owner/name") || len(w.node.Open()) != 0 {
		t.Fatalf("a refused entry: %d %v", code, out)
	}
	if h := w.node.Health(); !strings.Contains(h.LastEventWhat, "refused") {
		t.Fatalf("health %+v", h)
	}
}

// The message and status reasoners are the Trigger API's own doors
// (trigger.Message, trigger.Status): a message is a comment on the job, held
// to the lower of its trust and the control plane's, and the status is the
// job wire's facts.
func TestMessageAndStatusReasoners(t *testing.T) {
	w := newWorld(t, "")
	_, out := w.post("/reasoners/intake", nil, `{"title":"Fix it","external_id":"x-1"}`)
	job := int(out["job"].(float64))
	code, st := w.post("/reasoners/status", nil, `{"source":"agentfield:execute","id":"x-1"}`)
	if code != 200 || int(st["job"].(float64)) != job || st["state"] == nil || st["steps_total"] == nil || st["updated"] == nil {
		t.Fatalf("status: %d %v", code, st)
	}
	if code, byNumber := w.post("/reasoners/status", nil, `{"job":`+itoaT(job)+`}`); code != 200 || byNumber["state"] != st["state"] {
		t.Fatalf("status by number: %d %v", code, byNumber)
	}
	code, msg := w.post("/reasoners/message", nil, `{"job":`+itoaT(job)+`,"text":"use a table test","author":"dana"}`)
	if code != 200 || msg["accepted"] != true || int(msg["job"].(float64)) != job || msg["outcome"] == "" || msg["line"] == "" {
		t.Fatalf("message: %d %v", code, msg)
	}
	it, _ := w.floor.Get(job)
	if len(it.Comments) != 1 || it.Comments[0].Body != "use a table test" || it.Comments[0].Author != "dana" {
		t.Fatalf("the message is not a comment on the job: %+v", it.Comments)
	}
	// A stranger's job reads the message as context only.
	_ = w.floor.Update(job, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil })
	code, msg = w.post("/reasoners/message", nil, `{"source":"agentfield:execute","id":"x-1","text":"rm -rf"}`)
	if code != 200 || msg["outcome"] != string(factory.CommentContext) {
		t.Fatalf("a stranger's message: %d %v", code, msg)
	}
	if code, _ := w.post("/reasoners/message", nil, `{"job":`+itoaT(job)+`,"text":""}`); code != 422 {
		t.Fatalf("an empty message answered %d", code)
	}
	if code, _ := w.post("/reasoners/status", nil, `{"source":"agentfield:execute","id":"nope"}`); code != 404 {
		t.Fatalf("status of no job answered %d", code)
	}
	if code, _ := w.post("/reasoners/message", nil, `{"job":999,"text":"hi"}`); code != 404 {
		t.Fatalf("a message for no job answered %d", code)
	}
}

func TestConnectionKeepsNoSecretInTheFloorAndForgetsOnDisconnect(t *testing.T) {
	floor, _ := store.Open(t.TempDir())
	c := &Connection{Store: floor, Profile: t.TempDir()}
	s, err := Clean(Settings{URL: "http://cp:8080/", BaseURL: "http://10.0.0.5:8765"})
	if err != nil || s.NodeID != DefaultNodeID || s.Listen != "0.0.0.0:8765" || s.Trust != factory.TierCollab || s.URL != "http://cp:8080" {
		t.Fatalf("Clean = %+v, %v", s, err)
	}
	if loop, _ := Clean(Settings{URL: "http://cp:8080", BaseURL: "http://127.0.0.1:8765"}); loop.Listen != "127.0.0.1:8765" {
		t.Fatalf("a loopback base_url listens on %q", loop.Listen)
	}
	if _, err := Clean(Settings{URL: "http://cp:8080", BaseURL: "http://10.0.0.5"}); err == nil {
		t.Fatal("a base_url with no port and no listen address was taken")
	}
	// NO base_url IS A LINK-ONLY NODE: it listens on nothing.
	if linkOnly, err := Clean(Settings{URL: "http://cp:8080", Listen: "0.0.0.0:9"}); err != nil || linkOnly.BaseURL != "" || linkOnly.Listen != "" {
		t.Fatalf("a link-only node: %+v %v", linkOnly, err)
	}
	if err := c.Keep(s, "secret-key-123", "itok-456"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(floor.Root(), settingsFile))
	if bytes.Contains(raw, []byte("secret-key-123")) || bytes.Contains(raw, []byte("itok-456")) {
		t.Fatalf("the settings file holds a secret: %s", raw)
	}
	if st := c.Status(); !st.Connected || !st.KeyKept || !st.TokenKept {
		t.Fatalf("status %+v", st)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if st := c.Status(); st.Connected || config.AgentFieldKeyAt(c.Profile) != "" || config.AgentFieldInternalTokenAt(c.Profile) != "" {
		t.Fatalf("after disconnect: %+v", st)
	}
}

func TestTheClientRefusesAWrongKeyInWords(t *testing.T) {
	p := newFakePlane(t, "right")
	_, err := Check(context.Background(), nil, p.URL, "wrong")
	if !RefusedAs(err, RefusedKey) || !strings.Contains(err.Error(), "did not accept that API key") || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("a wrong key: %v", err)
	}
	if _, err := Check(context.Background(), nil, "http://127.0.0.1:1", "k"); err == nil || !strings.Contains(err.Error(), "could not be reached") {
		t.Fatalf("an unreachable control plane: %v", err)
	}
}

func itoaT(n int) string { b, _ := json.Marshal(n); return string(b) }

// A job that ends with no moment on the bus (a person dropped it) still
// completes its execution: the next lease tick catches it up. An execution
// already told its job's state is not told again — nothing is retold to keep
// it alive, the lease's inflight does that.
func TestADroppedJobIsCaughtUpOnTheNextLease(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-q"}, dispatch("generic_bearer", map[string]any{"title": "Fix it"}, "idem-q"))
	if code != http.StatusAccepted {
		t.Fatalf("intake: %d %v", code, out)
	}
	job := int(out["job"].(float64))
	eventually(t, "a running report", func() bool { return len(w.plane.reportsFor("exec-q")) > 0 })
	w.node.mu.Lock()
	r := w.node.running
	w.node.mu.Unlock()
	w.node.catchUp(context.Background(), r)
	if n := len(w.plane.reportsFor("exec-q")); n != 1 {
		t.Fatalf("an unchanged execution was told %d times", n)
	}
	if got := w.node.inflight(); len(got) != 1 || got[0] != "exec-q" {
		t.Fatalf("the lease's inflight is %v, want [exec-q]", got)
	}
	_ = w.floor.Update(job, func(it *factory.Item) error { it.State = factory.StateDismissed; return nil })
	w.node.catchUp(context.Background(), r)
	rs := w.plane.reportsFor("exec-q")
	if last := rs[len(rs)-1]; last.Status != ExecSucceeded || len(w.node.Open()) != 0 {
		t.Fatalf("after the drop: %+v, open %v", last, w.node.Open())
	}
	if got := w.node.inflight(); len(got) != 0 {
		t.Fatalf("a completed execution is still inflight: %v", got)
	}
}

// THE HTTP LEASE CARRIES INFLIGHT.
func TestTheHTTPLeaseListsInflight(t *testing.T) {
	w := newWorld(t, "")
	w.node.mu.Lock()
	r := w.node.running
	w.node.mu.Unlock()
	if _, err := r.client.Renew(context.Background(), DefaultNodeID, "ready", NodeVersion, []string{"exec-a", "exec-b"}); err != nil {
		t.Fatal(err)
	}
	w.plane.mu.Lock()
	last := w.plane.leases[len(w.plane.leases)-1]
	w.plane.mu.Unlock()
	if got, _ := json.Marshal(last["inflight"]); string(got) != `["exec-a","exec-b"]` {
		t.Fatalf("lease inflight %s", got)
	}
}

// AN APP-RUN ENGINE'S AGENTFIELD KEY OUTLIVES THE LAUNCH (live e2e round 9:
// held in memory alone, it was lost whenever the desk started the engine
// again, and the next Approve read "AgentField did not accept that API key").
// The key and the internal token are handed to the app and written to the
// engine's own profile, owner-readable only; the next launch, where the app
// hands nothing back, reads them from there, and Disconnect forgets both.
func TestAnAppRunConnectionKeepsItsKeyInTheProfileForTheNextLaunch(t *testing.T) {
	floor, _ := store.Open(t.TempDir())
	c := &Connection{Store: floor, Profile: t.TempDir()}
	creds := config.NewAppCredentials()
	restore := config.UseAppCredentials(creds)
	s, _ := Clean(Settings{URL: "http://cp:8080", BaseURL: "http://10.0.0.5:8765"})
	if err := c.Keep(s, "secret-key-123", "itok-456"); err != nil {
		restore()
		t.Fatal(err)
	}
	if key, _ := creds.Key(config.AppAgentFieldProvider); key != "secret-key-123" {
		restore()
		t.Fatalf("the app was not handed the key: %q", key)
	}
	restore()
	next := config.NewAppCredentials()
	defer config.UseAppCredentials(next)()
	if config.AgentFieldKeyAt(c.Profile) != "secret-key-123" || config.AgentFieldInternalTokenAt(c.Profile) != "itok-456" {
		t.Fatal("the next launch lost the AgentField key or token")
	}
	if info, err := os.Stat(config.BudgetConfigPath(c.Profile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the profile holding the key is not owner-readable only: %v", err)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if config.AgentFieldKeyAt(c.Profile) != "" || config.AgentFieldInternalTokenAt(c.Profile) != "" {
		t.Fatal("Disconnect left the key in the profile")
	}
}
