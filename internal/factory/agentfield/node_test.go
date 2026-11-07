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
	p := &fakePlane{key: key, reports: map[string][]ExecutionUpdate{}}
	mux := http.NewServeMux()
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
	base   string // the registered base_url, secret included
	nudges int
	ahead  time.Duration // how far the node's clock runs ahead of now
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
	if err := conn.Keep(s, "af-key-1"); err != nil {
		t.Fatal(err)
	}
	w.node = &Node{Conn: conn, Store: floor, Version: NodeVersion, Build: "test",
		Nudge: func() { w.mu.Lock(); w.nudges++; w.mu.Unlock() },
		Clock: func() time.Time { w.mu.Lock(); defer w.mu.Unlock(); return time.Now().Add(w.ahead) }}
	if err := w.node.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.node.Stop(); w.node.Forget() })
	eventually(t, "the registration", func() bool { return len(w.plane.registrations()) > 0 })
	w.base = "http://" + w.node.Health().Listening + SecretPrefix + config.AgentFieldNodeSecretAt(conn.Profile)
	return w
}

func (w *world) post(path string, headers map[string]string, body string) (int, map[string]any) {
	w.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, w.base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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

func TestTheRegistrationNamesThreeReasonersAndTheSecretBase(t *testing.T) {
	w := newWorld(t, "")
	reg := w.plane.registrations()[0]
	secret := config.AgentFieldNodeSecretAt(w.node.Conn.Profile)
	if len(secret) != 64 {
		t.Fatalf("the address secret is %d characters, want 64 hex", len(secret))
	}
	if reg.ID != DefaultNodeID || reg.BaseURL != "http://127.0.0.1:1/n/"+secret || reg.Version != NodeVersion || reg.InstanceID == "" {
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
	lease, err := r.client.Renew(context.Background(), DefaultNodeID, "ready", NodeVersion)
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

// A CALL WITHOUT THE SECRET IS NOT ANSWERED AT ALL: not under another secret,
// not at the bare path, not with the API key.
func TestACallWithoutTheSecretIsRefused(t *testing.T) {
	w := newWorld(t, "")
	listening := "http://" + w.node.Health().Listening
	for _, path := range []string{"/reasoners/intake", "/n/wrong/reasoners/intake", "/n/" + strings.Repeat("0", 64) + "/reasoners/intake", "/health"} {
		req, _ := http.NewRequest(http.MethodPost, listening+path, strings.NewReader(dispatch("generic_bearer", map[string]any{"title": "x"}, "k")))
		req.Header.Set("X-API-Key", "af-key-1")
		req.Header.Set("Authorization", "Bearer af-key-1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, resp.StatusCode)
		}
	}
	items, _ := w.floor.ListSaved()
	if len(items) != 0 {
		t.Fatalf("a refused call filed %d jobs", len(items))
	}
	resp, err := http.Get(w.base + "/health")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("health under the secret: %v %v", resp, err)
	}
	resp.Body.Close()
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
	if err := c.Keep(s, "secret-key-123"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(floor.Root(), settingsFile))
	if bytes.Contains(raw, []byte("secret-key-123")) || bytes.Contains(raw, []byte(config.AgentFieldNodeSecretAt(c.Profile))) {
		t.Fatalf("the settings file holds a secret: %s", raw)
	}
	if st := c.Status(); !st.Connected || !st.KeyKept {
		t.Fatalf("status %+v", st)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if st := c.Status(); st.Connected || config.AgentFieldKeyAt(c.Profile) != "" || config.AgentFieldNodeSecretAt(c.Profile) != "" {
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
// completes its execution: the lease's renewal tells an execution quiet for
// keepAlive where its job stands.
func TestAQuietExecutionIsToldAgainWithTheLease(t *testing.T) {
	w := newWorld(t, factory.TierOwner)
	code, out := w.post("/reasoners/intake", map[string]string{"X-Execution-ID": "exec-q"}, dispatch("generic_bearer", map[string]any{"title": "Fix it"}, "idem-q"))
	if code != http.StatusAccepted {
		t.Fatalf("intake: %d %v", code, out)
	}
	job := int(out["job"].(float64))
	eventually(t, "a running report", func() bool { return len(w.plane.reportsFor("exec-q")) > 0 })
	_ = w.floor.Update(job, func(it *factory.Item) error { it.State = factory.StateDismissed; return nil })
	w.node.mu.Lock()
	client := w.node.running.client
	w.node.mu.Unlock()
	w.node.retellQuiet(context.Background(), client)
	if n := len(w.plane.reportsFor("exec-q")); n != 1 {
		t.Fatalf("an execution told %d times inside keepAlive", n)
	}
	w.mu.Lock()
	w.ahead = keepAlive
	w.mu.Unlock()
	w.node.retellQuiet(context.Background(), client)
	r := w.plane.reportsFor("exec-q")
	if last := r[len(r)-1]; last.Status != ExecSucceeded || len(w.node.Open()) != 0 {
		t.Fatalf("after keepAlive: %+v, open %v", last, w.node.Open())
	}
}
