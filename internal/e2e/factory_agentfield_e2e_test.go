//go:build e2e

package e2e

// factory_agentfield_e2e_test.go is THE END-TO-END PROOF THAT THE FACTORY IS AN
// AGENTFIELD NODE (docs/design/factory-general/DESIGN.md §AgentField): a real
// AgentField control plane, built from its own repository and run beside this
// test, dispatches a generic_bearer trigger to the factory's intake reasoner;
// the event becomes a routed job on the quick pipeline, the job opens a pull
// request on factory-demo, and the AgentField execution the trigger made ends
// succeeded with that pull request's link as result.pr_url. A cron tick that
// carries no entry reaches the intake and files nothing.
//
// It runs on the general factory lane's world ([newFLEWorld]: the real
// `bin/codeaf app` over stdio, a throwaway home whose only carried row is the
// api_key, every model deepseek/deepseek-v4.1-flash, the real factory-demo
// sandbox) and adds the control plane, which the person running it starts:
//
//	AGENTFIELD_HOME=$tmp/home AGENTFIELD_STORAGE_MODE=local AGENTFIELD_PORT=$port \
//	AGENTFIELD_API_KEY=$(cat cpkey) AF_NODE_BEARER=$(cat bearer) agentfield-server server
//
//	make build
//	FLE2E=1 FLE2E_GH_TOKEN_FILE=… AFNODE_CP_URL=http://127.0.0.1:$port \
//	AFNODE_CP_KEY_FILE=cpkey AFNODE_BEARER_FILE=bearer \
//	go test -tags e2e -run TestFactoryAgentFieldNodeE2E -count=1 -timeout 60m -v ./internal/e2e/
//
// AFNODE_BEARER_ENV names the control plane's variable holding the bearer
// secret (default AF_NODE_BEARER). Everything it files is titled AFNODE, and
// its cleanup closes those pull requests and issues with "agentfield node e2e,
// closing", deletes their branches, deletes the triggers it made and
// disconnects the node.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	afnPrefix = "AFNODE"
	afnClose  = "agentfield node e2e, closing"
	afnSource = "agentfield:generic-bearer"
	afnRule   = "AgentField small changes"
)

// afnPlane is the control plane the run talks to with its API key.
type afnPlane struct {
	t   *testing.T
	url string
	key string
}

func (p *afnPlane) do(method, path string, body any, into any) int {
	p.t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, p.url+path, r)
	if err != nil {
		p.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if into != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			p.t.Fatalf("%s %s answered %d %s", method, path, resp.StatusCode, fleClip(raw, 1500))
		}
	}
	return resp.StatusCode
}

func afnFile(t *testing.T, name string) string {
	t.Helper()
	path := strings.TrimSpace(os.Getenv(name))
	if path == "" {
		t.Skip("opt-in live lane: set AFNODE_CP_URL, AFNODE_CP_KEY_FILE and AFNODE_BEARER_FILE for a running AgentField control plane")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return strings.TrimSpace(string(raw))
}

func afnFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type afnEvent struct {
	ID             string          `json:"id"`
	SourceName     string          `json:"source_name"`
	EventType      string          `json:"event_type"`
	IdempotencyKey string          `json:"idempotency_key"`
	Status         string          `json:"status"`
	ErrorMessage   string          `json:"error_message"`
	Normalized     json.RawMessage `json:"normalized_payload"`
}

// afnExecutions reads the node's side table: the execution each filed job
// reports on.
func afnExecutions(t *testing.T, w *fleWorld) []struct {
	ID    string    `json:"id"`
	Job   int       `json:"job"`
	Said  string    `json:"said"`
	Ended time.Time `json:"ended"`
} {
	var table struct {
		Executions []struct {
			ID    string    `json:"id"`
			Job   int       `json:"job"`
			Said  string    `json:"said"`
			Ended time.Time `json:"ended"`
		} `json:"executions"`
	}
	raw, err := os.ReadFile(filepath.Join(w.floor.Root(), "agentfield-executions.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &table)
	}
	return table.Executions
}

func TestFactoryAgentFieldNodeE2E(t *testing.T) {
	cpURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AFNODE_CP_URL")), "/")
	if cpURL == "" {
		t.Skip("opt-in live lane: set AFNODE_CP_URL to a running AgentField control plane")
	}
	plane := &afnPlane{t: t, url: cpURL, key: afnFile(t, "AFNODE_CP_KEY_FILE")}
	bearer := afnFile(t, "AFNODE_BEARER_FILE")
	bearerEnv := strings.TrimSpace(os.Getenv("AFNODE_BEARER_ENV"))
	if bearerEnv == "" {
		bearerEnv = "AF_NODE_BEARER"
	}
	w := newFLEWorld(t)
	began, spend0 := time.Now(), w.daySpend()

	var triggers []string
	branchesBefore := w.branchesBefore
	t.Cleanup(func() { afnCleanup(w, plane, triggers, branchesBefore) })

	// 1. CONNECT: the node registers on the real control plane.
	port := afnFreePort(t)
	var st struct {
		Connected    bool       `json:"connected"`
		NodeID       string     `json:"node_id"`
		Listening    string     `json:"listening"`
		RegisteredAt *time.Time `json:"registered_at"`
		LeaseSeconds int        `json:"lease_seconds"`
		LastEvent    *time.Time `json:"last_event"`
		LastWhat     string     `json:"last_event_what"`
		LastError    string     `json:"last_error"`
		KeyKept      bool       `json:"key_kept"`
	}
	w.decode("factory.agentfield_connect", map[string]any{"factory": w.factory, "url": cpURL, "api_key": plane.key, "base_url": "http://127.0.0.1:" + strconv.Itoa(port)}, &st)
	t.Logf("factory.agentfield_connect → %+v", st)
	if !st.Connected || st.RegisteredAt == nil || !st.KeyKept || st.NodeID != "codeaf-factory" {
		t.Fatalf("connect answered %+v, want connected, registered, key kept, node codeaf-factory", st)
	}
	var node struct {
		ID         string                `json:"id"`
		BaseURL    string                `json:"base_url"`
		Reasoners  []struct{ ID string } `json:"reasoners"`
		Lifecycle  string                `json:"lifecycle_status"`
		InstanceID string                `json:"instance_id"`
	}
	if code := plane.do("GET", "/api/v1/nodes/codeaf-factory", nil, &node); code != 200 {
		t.Fatalf("the control plane does not know node codeaf-factory: %d", code)
	}
	var rs []string
	for _, r := range node.Reasoners {
		rs = append(rs, r.ID)
	}
	slices.Sort(rs)
	if !slices.Equal(rs, []string{"intake", "message", "status"}) || !strings.Contains(node.BaseURL, "/n/") {
		t.Fatalf("node %+v: want reasoners intake, message, status at a secret path", node)
	}
	t.Logf("AFNODE node registered: base_url=%s… reasoners=%v lifecycle=%s instance=%s", node.BaseURL[:strings.Index(node.BaseURL, "/n/")+3], rs, node.Lifecycle, node.InstanceID)

	// 2. A generic_bearer TRIGGER aimed at codeaf-factory.intake.
	var sources struct {
		Sources []struct {
			Name string `json:"name"`
		} `json:"sources"`
	}
	w.decode("factory.agentfield_sources", map[string]any{"factory": w.factory}, &sources)
	if !slices.ContainsFunc(sources.Sources, func(s struct {
		Name string `json:"name"`
	}) bool {
		return s.Name == "generic_bearer"
	}) {
		t.Fatalf("the source catalog has no generic_bearer: %+v", sources)
	}
	type wireTrigger struct {
		ID        string `json:"id"`
		Source    string `json:"source"`
		Reasoner  string `json:"reasoner"`
		IngestURL string `json:"ingest_url"`
	}
	var trig wireTrigger
	w.decode("factory.agentfield_trigger_add", map[string]any{"factory": w.factory, "source": "generic_bearer", "config": map[string]any{"idempotency_header": "X-Delivery"}, "secret_env_var": bearerEnv}, &trig)
	triggers = append(triggers, trig.ID)
	t.Logf("factory.agentfield_trigger_add → %+v", trig)
	if trig.ID == "" || trig.Reasoner != "intake" || trig.IngestURL != cpURL+"/sources/"+trig.ID {
		t.Fatalf("trigger %+v, want one on intake with ingest_url %s/sources/<id>", trig, cpURL)
	}
	var listed struct {
		Triggers []wireTrigger `json:"triggers"`
	}
	w.decode("factory.agentfield_triggers", map[string]any{"factory": w.factory}, &listed)
	if !slices.ContainsFunc(listed.Triggers, func(x wireTrigger) bool { return x.ID == trig.ID }) {
		t.Fatalf("factory.agentfield_triggers does not list %s: %+v", trig.ID, listed)
	}

	// 3. THE ROUTE and the quick pipeline copy.
	w.setQuickCopy()
	defer w.clearQuickCopy()
	w.setRoutes([]map[string]any{{
		"name": afnRule,
		"when": map[string]any{"sources": []string{afnSource}},
		"then": map[string]any{"repo": fleRepo, "pipeline": "quick", "priority": "p1", "start": "auto"},
	}})

	// 4. THE EVENT, posted the way an outside system posts it.
	ext := "afnode-" + strconv.FormatInt(time.Now().Unix(), 10)
	entry := map[string]any{
		"title":       afnPrefix + ": add a Len() method to Ledger",
		"external_id": ext,
		"body": "In ledger.go, add a method `func (l *Ledger) Len() int` that returns the number of entries in the ledger, " +
			"with a doc comment, and a table test for it in ledger_test.go (empty ledger is 0, two adds is 2). Change nothing else.",
		"author": "afnode-e2e",
	}
	raw, _ := json.Marshal(entry)
	req, _ := http.NewRequest("POST", trig.IngestURL, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-Delivery", ext)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", trig.IngestURL, err)
	}
	ingest, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("POST /sources/%s → %d %s", trig.ID, resp.StatusCode, ingest)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("ingest answered %d %s", resp.StatusCode, ingest)
	}

	// (a) the event is stored in AgentField, and dispatched.
	var ev afnEvent
	fleEventually(t, "the event stored and dispatched in AgentField", 2*time.Minute, 2*time.Second, func() bool {
		var events struct {
			Events []afnEvent `json:"events"`
		}
		plane.do("GET", "/api/v1/triggers/"+trig.ID+"/events", nil, &events)
		for _, e := range events.Events {
			if e.IdempotencyKey == ext {
				ev = e
				return e.Status == "dispatched" || e.Status == "failed"
			}
		}
		return false
	})
	t.Logf("AFNODE event %s stored: status=%s idempotency=%s error=%q", ev.ID, ev.Status, ev.IdempotencyKey, ev.ErrorMessage)
	if ev.Status != "dispatched" {
		t.Fatalf("event %s is %s: %s", ev.ID, ev.Status, ev.ErrorMessage)
	}

	// (b) a job is filed under the trigger's own source.
	ref := afnSource + ":" + ext
	var it factory.Item
	fleEventually(t, "a job with SourceRef "+ref, 2*time.Minute, 2*time.Second, func() bool {
		found := w.bySourceRef(ref)
		if len(found) == 1 {
			it = found[0]
			return true
		}
		return false
	})
	w.started[it.ID] = true
	t.Logf("AFNODE job %d filed: source_ref=%s trust=%s", it.ID, it.SourceRef, it.Tier)
	var execID string
	for _, x := range afnExecutions(t, w) {
		if x.Job == it.ID {
			execID = x.ID
		}
	}
	if execID == "" {
		t.Fatalf("no AgentField execution recorded for job %d", it.ID)
	}
	t.Logf("AFNODE job %d reports on execution %s", it.ID, execID)

	fleEventually(t, "the rule to route job "+strconv.Itoa(it.ID), 4*time.Minute, 3*time.Second, func() bool {
		it = w.item(it.ID)
		return it.Route != "" || fleWorking(it) || it.State != factory.StateNew
	})
	if it.Route != afnRule || it.Repo != fleRepo {
		t.Fatalf("job %d: Route %q Repo %q, want %q on %s", it.ID, it.Route, it.Repo, afnRule, fleRepo)
	}
	j := w.job(it.ID)
	if got := j.stepNames(); !slices.Equal(got, fleQuick) {
		t.Fatalf("job %d steps are %v, want %v", it.ID, got, fleQuick)
	}

	// While it runs, the execution is running.
	var exec struct {
		Status string         `json:"status"`
		Result map[string]any `json:"result"`
		Error  string         `json:"error"`
	}
	plane.do("GET", "/api/v1/executions/"+execID, nil, &exec)
	t.Logf("execution %s while the job runs: %s", execID, exec.Status)

	// (c)+(d) write/test/proof on the one model, and a pull request.
	fleEventually(t, "job "+strconv.Itoa(it.ID)+" to open a pull request", 30*time.Minute, 10*time.Second, func() bool {
		j = w.job(it.ID)
		if j.PR != nil && j.PR.URL != "" {
			return true
		}
		cur := w.item(it.ID)
		if factory.StoppedByPerson(cur) || (j.State == "done" && j.PR == nil) {
			t.Fatalf("job %d ended without a pull request: state %s reason %q", it.ID, j.State, j.Reason)
		}
		if j.State == "needs_you" && j.Reason == "approval_start" {
			t.Fatalf("job %d: start: auto, but its start is held: %q", it.ID, j.AutonomyWhy)
		}
		return false
	})
	prURL := j.PR.URL
	t.Logf("AFNODE job %d opened %s", it.ID, prURL)
	fleEventually(t, "job "+strconv.Itoa(it.ID)+" to count its three steps done", 3*time.Minute, 5*time.Second, func() bool {
		j = w.job(it.ID)
		return j.StepsDone >= len(fleQuick)
	})
	models := j.stepModels(t, fleModel)
	t.Logf("AFNODE job %d steps %d/%d: %v", it.ID, j.StepsDone, j.StepsTotal, models)

	// (e) the execution reaches succeeded with result.pr_url.
	fleEventually(t, "execution "+execID+" to succeed with the pull request", 2*time.Minute, 3*time.Second, func() bool {
		exec.Result = nil
		plane.do("GET", "/api/v1/executions/"+execID, nil, &exec)
		return exec.Status == "succeeded"
	})
	execRaw, _ := json.Marshal(exec)
	t.Logf("AFNODE execution %s → %s", execID, execRaw)
	if got, _ := exec.Result["pr_url"].(string); got != prURL {
		t.Fatalf("execution %s result.pr_url = %q, want %q", execID, got, prURL)
	}

	// 5. A CRON TICK reaches the intake and files nothing.
	jobsBefore := len(w.items())
	var cron wireTrigger
	w.decode("factory.agentfield_trigger_add", map[string]any{"factory": w.factory, "source": "cron", "config": map[string]any{"expression": "* * * * *"}}, &cron)
	triggers = append(triggers, cron.ID)
	t.Logf("cron trigger %s", cron.ID)
	var tick afnEvent
	fleEventually(t, "a cron tick dispatched to the intake", 3*time.Minute, 3*time.Second, func() bool {
		var events struct {
			Events []afnEvent `json:"events"`
		}
		plane.do("GET", "/api/v1/triggers/"+cron.ID+"/events", nil, &events)
		for _, e := range events.Events {
			if e.Status == "dispatched" || e.Status == "failed" {
				tick = e
				return true
			}
		}
		return false
	})
	plane.do("DELETE", "/api/v1/triggers/"+cron.ID, nil, nil)
	triggers = slices.DeleteFunc(triggers, func(id string) bool { return id == cron.ID })
	fleEventually(t, "the node to have seen the tick", time.Minute, 2*time.Second, func() bool {
		w.decode("factory.agentfield_status", map[string]any{"factory": w.factory}, &st)
		return strings.Contains(st.LastWhat, "cron tick")
	})
	t.Logf("AFNODE cron tick %s: status=%s payload=%s; node says %q", tick.ID, tick.Status, tick.Normalized, st.LastWhat)
	if tick.Status != "dispatched" {
		t.Errorf("tick %s is %s: %s", tick.ID, tick.Status, tick.ErrorMessage)
	}
	if n := len(w.items()); n != jobsBefore {
		t.Errorf("the cron tick filed %d jobs, want none", n-jobsBefore)
	}

	cost := max(w.daySpend()-spend0, w.job(it.ID).spent())
	t.Logf("AFNODE RESULT verdict=%v cost=$%.6f wall=%s evidence=%s", !t.Failed(), cost, time.Since(began).Round(time.Second),
		fmt.Sprintf("event=%s(%s) job=%d ref=%s route=%q steps=%v models=%v PR=%s exec=%s status=%s pr_url=%v cron=%s(%s) files_nothing=%q",
			ev.ID, ev.Status, it.ID, ref, it.Route, fleQuick, models, prURL, execID, exec.Status, exec.Result["pr_url"], tick.ID, tick.Status, st.LastWhat))
}

// afnCleanup runs before the general lane's own cleanup: it deletes the
// triggers this run made, disconnects the node, and closes every AFNODE pull
// request and issue with its one sentence, deleting their branches.
func afnCleanup(w *fleWorld, plane *afnPlane, triggers []string, branchesBefore map[string]bool) {
	t := w.t
	if os.Getenv("FLE2E_KEEP") == "1" {
		return
	}
	for _, id := range triggers {
		code := plane.do("DELETE", "/api/v1/triggers/"+id, nil, nil)
		t.Logf("cleanup: deleted trigger %s: %d", id, code)
	}
	if w.factory != "" {
		raw, err := w.eng.call("factory.agentfield_disconnect", map[string]any{"factory": w.factory}, time.Minute)
		t.Logf("cleanup: factory.agentfield_disconnect → %s %v", fleClip(raw, 500), err)
	}
	if out, err := w.gh("pr", "list", "-R", fleRepo, "--state", "open", "--search", afnPrefix+" in:title", "--json", "number,title,headRefName", "--limit", "100"); err == nil {
		var prs []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Head   string `json:"headRefName"`
		}
		_ = json.Unmarshal([]byte(out), &prs)
		for _, pr := range prs {
			if !strings.Contains(pr.Title, afnPrefix) {
				continue
			}
			o, err := w.gh("pr", "close", strconv.Itoa(pr.Number), "-R", fleRepo, "--comment", afnClose, "--delete-branch")
			t.Logf("cleanup: closed PR #%d (%s): %v %s", pr.Number, pr.Head, err, o)
		}
	}
	if out, err := w.gh("issue", "list", "-R", fleRepo, "--state", "open", "--search", afnPrefix+" in:title", "--json", "number,title", "--limit", "100"); err == nil {
		var issues []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal([]byte(out), &issues)
		for _, is := range issues {
			if !strings.Contains(is.Title, afnPrefix) {
				continue
			}
			o, err := w.gh("issue", "close", strconv.Itoa(is.Number), "-R", fleRepo, "--comment", afnClose)
			t.Logf("cleanup: closed issue #%d: %v %s", is.Number, err, o)
		}
	}
	if out, err := w.gh("api", "repos/"+fleRepo+"/branches", "--paginate", "-q", ".[].name"); err == nil {
		for _, b := range strings.Fields(out) {
			if branchesBefore[b] || !strings.Contains(strings.ToLower(b), strings.ToLower(afnPrefix)) {
				continue
			}
			o, err := w.gh("api", "-X", "DELETE", "repos/"+fleRepo+"/git/refs/heads/"+b)
			t.Logf("cleanup: deleted branch %s: %v %s", b, err, o)
		}
	}
}
