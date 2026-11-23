package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// briefFactory is a stub factory that answers a job's brief route, or, when
// old is set, answers it as a factory older than the route does.
type briefFactory struct {
	mu   sync.Mutex
	old  bool
	asks []string
	url  string
}

func startBriefFactory(t *testing.T, old bool) *briefFactory {
	t.Helper()
	f := &briefFactory{old: old}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/about" || r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(api.Me{Contract: api.Contract, Factory: api.Factory{Name: "acme"}, Mode: api.FactoryTeam})
		case r.URL.Path == "/v1/jobs/7/brief" && r.Method == http.MethodGet && f.old:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"factory endpoint not found"}`)
		case r.URL.Path == "/v1/jobs/7/brief" && r.Method == http.MethodGet:
			f.asks = append(f.asks, r.URL.RequestURI())
			_, _ = io.WriteString(w, `{"factory":{"name":"acme"},"job":7,"status":"failed","failure":"no JSON","failed_after":"proof","since":2,`+
				`"brief":{"rev":3,"after_step":"test","model":"m","cost_usd":0.004,"issue":"Fix retries","steps_done":4,"steps_total":6,"verdict":"Fixed.",`+
				`"facts":[{"key":"test","label":"Test","value":"3 passed","ok":true,"evidence":{"step":"test","try":1,"kind":"tests"}}],"blocks":[],"changed_keys":["test"]},`+
				`"changed_since_seen":["verdict","test"]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func attachBrief(t *testing.T, f *briefFactory) (*factoryWire, string) {
	t.Helper()
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": f.url, "token": testHostToken}, &conn)
	return wire, conn.ID
}

func TestAppFactoryJobBriefCarriesTheFactorysBrief(t *testing.T) {
	f := startBriefFactory(t, false)
	wire, id := attachBrief(t, f)
	if !strings.Contains(wire.hello, appwire.FeatureFactoryJobBrief) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryJobBrief)
	}
	var got appwire.FactoryJobBrief
	wire.must("factory.job_brief", map[string]any{"factory": id, "job": 7, "since": 2}, &got)
	if got.Factory != id || got.Job != 7 || got.Status != "failed" || got.Failure != "no JSON" || got.FailedAfter != "proof" || got.Since != 2 ||
		got.Brief.Rev != 3 || got.Brief.Verdict != "Fixed." || len(got.Brief.Facts) != 1 || got.Brief.Facts[0].Evidence.Step != "test" ||
		strings.Join(got.ChangedSinceSeen, ",") != "verdict,test" {
		t.Fatalf("brief = %+v", got)
	}
	wire.must("factory.job_brief", map[string]any{"factory": id, "job": 7}, &got)
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.asks, " ") != "/v1/jobs/7/brief?since=2 /v1/jobs/7/brief" {
		t.Fatalf("asked %v", f.asks)
	}
}

func TestAppFactoryJobBriefOnAnOlderFactoryIsTooOld(t *testing.T) {
	f := startBriefFactory(t, true)
	wire, id := attachBrief(t, f)
	if msg := wire.mustFail("factory.job_brief", map[string]any{"factory": id, "job": 7}, appwire.CodeConflict); !strings.Contains(msg, "doesn't support that yet") {
		t.Fatalf("old factory = %s", msg)
	}
}
