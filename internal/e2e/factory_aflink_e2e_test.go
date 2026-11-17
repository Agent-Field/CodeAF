//go:build e2e

package e2e

// factory_aflink_e2e_test.go is THE LIVE PROOF OF THE AGENTFIELD NODE LINK
// PROTOTYPE: the factory connects to a real control plane (built from
// Agent-Field/agentfield proto/node-link) with NO base_url, so it binds no
// port for AgentField at all and the control plane has no address to dial.
// A generic_bearer trigger's event must therefore arrive DOWN THE LINK the
// node opened. The job runs write/test/proof on factory-demo and opens a pull
// request; the control plane is killed and restarted mid-run, the link must
// come back and the completion (succeeded, result.pr_url) must still arrive.
// A second event's execution is then cancelled from AgentField and the job
// must stop.
//
//	AFNODE_CP_URL=http://127.0.0.1:$port AFNODE_CP_KEY_FILE=cpkey AFNODE_BEARER_FILE=bearer \
//	AFLINK_CP_RESTART='./restart-cp.sh' FLE2E=1 FLE2E_GH_TOKEN_FILE=… \
//	go test -tags e2e -run TestFactoryAgentFieldLinkE2E -count=1 -timeout 60m -v ./internal/e2e/
//
// AFLINK_CP_RESTART is a shell command that kills the control plane and
// starts it again on the same port and database, returning once it answers.
// Everything it files is titled PROTO and is closed with "agentfield link
// prototype, closing".

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	aflPrefix = "PROTO"
	aflClose  = "agentfield link prototype, closing"
	aflRule   = "AgentField link prototype"
)

type aflStatus struct {
	Connected    bool       `json:"connected"`
	NodeID       string     `json:"node_id"`
	BaseURL      string     `json:"base_url"`
	Listening    string     `json:"listening"`
	Link         string     `json:"link"`
	LinkUps      int        `json:"link_ups"`
	RegisteredAt *time.Time `json:"registered_at"`
	LastWhat     string     `json:"last_event_what"`
	LastVia      string     `json:"last_event_via"`
	LastError    string     `json:"last_error"`
}

func TestFactoryAgentFieldLinkE2E(t *testing.T) {
	cpURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AFNODE_CP_URL")), "/")
	if cpURL == "" {
		t.Skip("opt-in live lane: set AFNODE_CP_URL to a running proto/node-link control plane")
	}
	restart := strings.TrimSpace(os.Getenv("AFLINK_CP_RESTART"))
	plane := &afnPlane{t: t, url: cpURL, key: afnFile(t, "AFNODE_CP_KEY_FILE")}
	bearer := afnFile(t, "AFNODE_BEARER_FILE")
	w := newFLEWorld(t)
	began, spend0 := time.Now(), w.daySpend()
	var triggers []string
	t.Cleanup(func() { aflCleanup(w, plane, triggers) })

	status := func() aflStatus {
		var st aflStatus
		w.decode("factory.agentfield_status", map[string]any{"factory": w.factory}, &st)
		return st
	}

	// 1. CONNECT WITH NO base_url: a link-only node.
	var st aflStatus
	w.decode("factory.agentfield_connect", map[string]any{"factory": w.factory, "url": cpURL, "api_key": plane.key}, &st)
	t.Logf("factory.agentfield_connect (no base_url) → %+v", st)
	if !st.Connected || st.RegisteredAt == nil || st.BaseURL != "" || st.Listening != "" {
		t.Fatalf("connect answered %+v, want connected, registered, no base_url, listening on nothing", st)
	}
	fleEventually(t, "the link up", time.Minute, time.Second, func() bool { st = status(); return st.Link == "up" })
	var node struct {
		BaseURL    string `json:"base_url"`
		InstanceID string `json:"instance_id"`
		Health     string `json:"health_status"`
	}
	plane.do("GET", "/api/v1/nodes/codeaf-factory", nil, &node)
	t.Logf("AFLINK node on the control plane: base_url=%q instance=%s health=%s; node link=%s ups=%d listening=%q", node.BaseURL, node.InstanceID, node.Health, st.Link, st.LinkUps, st.Listening)
	if node.BaseURL != "" {
		t.Fatalf("the control plane holds base_url %q for a link-only node", node.BaseURL)
	}
	// NAT: THE ENGINE HOLDS NO LISTENING SOCKET FOR AGENTFIELD. The only one is
	// the factory's own loopback API, which the control plane is never told.
	if out, err := exec.Command("lsof", "-nP", "-a", "-p", strconv.Itoa(w.eng.cmd.Process.Pid), "-iTCP", "-sTCP:LISTEN").CombinedOutput(); err == nil || len(out) > 0 {
		t.Logf("AFLINK listening sockets of the engine (pid %d):\n%s", w.eng.cmd.Process.Pid, out)
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n")[1:] {
			if line != "" && !strings.Contains(line, "127.0.0.1:") && !strings.Contains(line, "[::1]:") {
				t.Fatalf("the engine listens beyond loopback: %s", line)
			}
		}
	}

	// 2. THE TRIGGER, THE ROUTE, THE QUICK PIPELINE.
	var trig struct {
		ID        string `json:"id"`
		IngestURL string `json:"ingest_url"`
	}
	w.decode("factory.agentfield_trigger_add", map[string]any{"factory": w.factory, "source": "generic_bearer", "config": map[string]any{"idempotency_header": "X-Delivery"}, "secret_env_var": "AF_NODE_BEARER"}, &trig)
	triggers = append(triggers, trig.ID)
	t.Logf("AFLINK trigger %s → codeaf-factory.intake, ingest %s", trig.ID, trig.IngestURL)
	w.setQuickCopy()
	defer w.clearQuickCopy()
	w.setRoutes([]map[string]any{{
		"name": aflRule,
		"when": map[string]any{"sources": []string{afnSource}},
		"then": map[string]any{"repo": fleRepo, "pipeline": "quick", "priority": "p1", "start": "auto"},
	}})

	post := func(ext, title, body string) {
		raw, _ := json.Marshal(map[string]any{"title": title, "external_id": ext, "body": body, "author": "aflink-e2e"})
		req, _ := http.NewRequest("POST", trig.IngestURL, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-Delivery", ext)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", trig.IngestURL, err)
		}
		out, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("POST /sources/%s (%s) → %d %s", trig.ID, ext, resp.StatusCode, out)
		if resp.StatusCode/100 != 2 {
			t.Fatalf("ingest answered %d", resp.StatusCode)
		}
	}
	filed := func(ext string) (factory.Item, string) {
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
		var execID string
		for _, x := range afnExecutions(t, w) {
			if x.Job == it.ID {
				execID = x.ID
			}
		}
		if execID == "" {
			t.Fatalf("no AgentField execution recorded for job %d", it.ID)
		}
		return it, execID
	}

	// 3. THE EVENT: it can only reach the factory down the link.
	ext := "aflink-" + strconv.FormatInt(time.Now().Unix(), 10)
	post(ext, aflPrefix+": add a tiny Go method", "In ledger.go, add a method `func (l *Ledger) IsEmpty() bool` that reports whether the ledger has no entries, "+
		"with a doc comment, and a table test for it in ledger_test.go (an empty ledger is empty, one add is not). Change nothing else.")
	it, execID := filed(ext)
	st = status()
	t.Logf("AFLINK job %d filed (execution %s): the call came by %q — %s", it.ID, execID, st.LastVia, st.LastWhat)
	if st.LastVia != "link" {
		t.Fatalf("the dispatch came by %q, want link", st.LastVia)
	}
	fleEventually(t, "job "+strconv.Itoa(it.ID)+" routed and working", 4*time.Minute, 3*time.Second, func() bool {
		it = w.item(it.ID)
		return it.Route != "" && fleWorking(it)
	})
	if j := w.job(it.ID); it.Route != aflRule || !slices.Equal(j.stepNames(), fleQuick) {
		t.Fatalf("job %d: route %q steps %v", it.ID, it.Route, j.stepNames())
	}

	// 4. KILL THE CONTROL PLANE MID-RUN AND START IT AGAIN.
	upsBefore := status().LinkUps
	if restart != "" {
		out, err := exec.Command("sh", "-c", restart).CombinedOutput()
		t.Logf("AFLINK control plane restarted mid-run (job %d %s): %v\n%s", it.ID, w.item(it.ID).State, err, out)
		if err != nil {
			t.Fatalf("restart: %v", err)
		}
		fleEventually(t, "the link to reconnect", 2*time.Minute, time.Second, func() bool { st = status(); return st.Link == "up" && st.LinkUps > upsBefore })
		t.Logf("AFLINK link reconnected: ups %d → %d", upsBefore, st.LinkUps)
	} else {
		t.Log("AFLINK_CP_RESTART unset: no mid-run restart")
	}

	// 5. THE PULL REQUEST AND THE COMPLETION.
	var j fleJob
	fleEventually(t, "job "+strconv.Itoa(it.ID)+" to open a pull request", 30*time.Minute, 10*time.Second, func() bool {
		j = w.job(it.ID)
		if j.PR != nil && j.PR.URL != "" {
			return true
		}
		if cur := w.item(it.ID); factory.StoppedByPerson(cur) || (j.State == "done" && j.PR == nil) {
			t.Fatalf("job %d ended without a pull request: %s %q", it.ID, j.State, j.Reason)
		}
		return false
	})
	prURL := j.PR.URL
	fleEventually(t, "the three steps done", 3*time.Minute, 5*time.Second, func() bool { j = w.job(it.ID); return j.StepsDone >= len(fleQuick) })
	models := j.stepModels(t, fleModel)
	t.Logf("AFLINK job %d opened %s, steps %d/%d: %v", it.ID, prURL, j.StepsDone, j.StepsTotal, models)
	var ex struct {
		Status string         `json:"status"`
		Result map[string]any `json:"result"`
	}
	fleEventually(t, "execution "+execID+" succeeded", 3*time.Minute, 3*time.Second, func() bool {
		ex.Result = nil
		plane.do("GET", "/api/v1/executions/"+execID, nil, &ex)
		return ex.Status == "succeeded"
	})
	exRaw, _ := json.Marshal(ex)
	t.Logf("AFLINK execution %s → %s", execID, exRaw)
	if got, _ := ex.Result["pr_url"].(string); got != prURL {
		t.Fatalf("result.pr_url %q, want %q", got, prURL)
	}

	// 6. CANCEL FROM AGENTFIELD STOPS THE JOB.
	ext2 := ext + "-cancel"
	post(ext2, aflPrefix+": cancel me", "In ledger.go, add a method `func (l *Ledger) Last() (Entry, bool)` returning the newest entry, with a table test. Change nothing else.")
	it2, exec2 := filed(ext2)
	fleEventually(t, "job "+strconv.Itoa(it2.ID)+" working", 4*time.Minute, 2*time.Second, func() bool { it2 = w.item(it2.ID); return fleWorking(it2) })
	var cancelOut map[string]any
	code := plane.do("POST", "/api/v1/executions/"+exec2+"/cancel", map[string]any{"reason": "PROTO cancel from AgentField"}, &cancelOut)
	t.Logf("AFLINK POST /api/v1/executions/%s/cancel → %d %v", exec2, code, cancelOut)
	fleEventually(t, "job "+strconv.Itoa(it2.ID)+" stopped", 2*time.Minute, 2*time.Second, func() bool {
		it2 = w.item(it2.ID)
		return !fleWorking(it2) && factory.StoppedByPerson(it2)
	})
	st = status()
	var ex2 struct {
		Status string `json:"status"`
	}
	plane.do("GET", "/api/v1/executions/"+exec2, nil, &ex2)
	t.Logf("AFLINK job %d after the cancel: state=%s stopped_by_person=%v; execution %s=%s; node: via=%s %q", it2.ID, it2.State, factory.StoppedByPerson(it2), exec2, ex2.Status, st.LastVia, st.LastWhat)
	if !strings.Contains(st.LastWhat, "cancelled execution "+exec2) || st.LastVia != "link" {
		t.Fatalf("the cancel did not come down the link: via=%s %q", st.LastVia, st.LastWhat)
	}

	cost := max(w.daySpend()-spend0, w.job(it.ID).spent())
	t.Logf("AFLINK RESULT verdict=%v cost=$%.4f wall=%s", !t.Failed(), cost, time.Since(began).Round(time.Second))
}

// aflCleanup deletes the run's triggers, disconnects the node, and closes
// every PROTO pull request and issue with its sentence, deleting branches.
func aflCleanup(w *fleWorld, plane *afnPlane, triggers []string) {
	t := w.t
	for _, id := range triggers {
		t.Logf("cleanup: deleted trigger %s: %d", id, plane.do("DELETE", "/api/v1/triggers/"+id, nil, nil))
	}
	if w.factory != "" {
		raw, err := w.eng.call("factory.agentfield_disconnect", map[string]any{"factory": w.factory}, time.Minute)
		t.Logf("cleanup: disconnect → %s %v", fleClip(raw, 300), err)
	}
	if out, err := w.gh("pr", "list", "-R", fleRepo, "--state", "open", "--search", aflPrefix+" in:title", "--json", "number,title", "--limit", "100"); err == nil {
		var prs []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal([]byte(out), &prs)
		for _, pr := range prs {
			if strings.Contains(pr.Title, aflPrefix) {
				o, err := w.gh("pr", "close", strconv.Itoa(pr.Number), "-R", fleRepo, "--comment", aflClose, "--delete-branch")
				t.Logf("cleanup: closed PR #%d: %v %s", pr.Number, err, o)
			}
		}
	}
	if out, err := w.gh("issue", "list", "-R", fleRepo, "--state", "open", "--search", aflPrefix+" in:title", "--json", "number,title", "--limit", "100"); err == nil {
		var issues []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal([]byte(out), &issues)
		for _, is := range issues {
			if strings.Contains(is.Title, aflPrefix) {
				o, err := w.gh("issue", "close", strconv.Itoa(is.Number), "-R", fleRepo, "--comment", aflClose)
				t.Logf("cleanup: closed issue #%d: %v %s", is.Number, err, o)
			}
		}
	}
	if out, err := w.gh("api", "repos/"+fleRepo+"/branches", "--paginate", "-q", ".[].name"); err == nil {
		for _, b := range strings.Fields(out) {
			if w.branchesBefore[b] || !strings.Contains(strings.ToLower(b), "proto") {
				continue
			}
			o, err := w.gh("api", "-X", "DELETE", "repos/"+fleRepo+"/git/refs/heads/"+b)
			t.Logf("cleanup: deleted branch %s: %v %s", b, err, o)
		}
	}
}
