package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

type hookRemote struct {
	mu    sync.Mutex
	log   []string
	body  string
	onGET func()
}

func probeWire(t *testing.T) (*editWire, *hookRemote, *factoryClock) {
	g := &hookRemote{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.log = append(g.log, r.Method+" "+r.URL.EscapedPath())
		body, hook := g.body, g.onGET
		g.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(api.Me{Mode: api.FactoryTeam})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pipeline/edit-source"):
			if hook != nil {
				hook()
			}
			serveRaw(body)(w, r)
		case r.Method == http.MethodPost:
			prOpened(prDefaultBranch, "")(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	w := newEditWire(t, &editStub{t: t, srv: srv}, false, api.FactoryTeam, prToken)
	c := &factoryClock{at: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}
	w.hub.now = c.now
	w.hub.hold(w.id, prToken, 0)
	g.body = runtimeReply(t, attested(editSourceFor(prRepo, runtimeSourceFile)), nil)
	return w, g, c
}

func (g *hookRemote) n(p string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := 0
	for _, l := range g.log {
		if strings.HasPrefix(l, p) {
			k++
		}
	}
	return k
}

func TestRuntimeGuardRegressionSwapDuringSourceReadRecordsNothing(t *testing.T) {
	w, g, _ := probeWire(t)
	g.onGET = func() { w.hub.hold(w.id, "tok_other_0123456789abcdef0123", 0) }
	_, err := w.source()
	if err == nil {
		t.Fatal("caller change accepted")
	}
	g.onGET = nil
	w.hub.hold(w.id, prToken, 0)
	if _, ok := w.hub.runtimeRecord(w.id, w.hub.rows[0].URL, prToken, prRepo, prBaseSHA); ok {
		t.Fatal("recorded across a swap")
	}
	if _, ok := w.hub.runtimeRecord(w.id, w.hub.rows[0].URL, "tok_other_0123456789abcdef0123", prRepo, prBaseSHA); ok {
		t.Fatal("recorded for the new caller")
	}
}

func TestRuntimeGuardRegressionPRAfterExpiryNeverReads(t *testing.T) {
	w, g, c := probeWire(t)
	if _, err := w.source(); err != nil {
		t.Fatal(err)
	}
	c.advance(appFactoryRuntimeAttestLife)
	g.mu.Lock()
	g.log = nil
	g.mu.Unlock()
	_, err := w.backend.FactoryPipelinePR(context.Background(), appwire.FactoryPipelinePRParams{Factory: w.id, Repo: prRepo, Text: runtimeSourceFile, ExpectedBaseSHA: prBaseSHA})
	t.Logf("err: %v", err)
	if err == nil || g.n("POST") != 0 || g.n("GET /v1/repos") != 0 {
		t.Fatalf("err %v log %v", err, g.log)
	}
}

func TestRuntimeGuardRegressionSwapDuringPRRefreshSendsNothing(t *testing.T) {
	w, g, _ := probeWire(t)
	if _, err := w.source(); err != nil {
		t.Fatal(err)
	}
	g.onGET = func() { w.hub.hold(w.id, "tok_other_0123456789abcdef0123", 0) }
	_, err := w.backend.FactoryPipelinePR(context.Background(), appwire.FactoryPipelinePRParams{Factory: w.id, Repo: prRepo, Text: runtimeSourceFile, ExpectedBaseSHA: prBaseSHA})
	t.Logf("err: %v", err)
	if err == nil || g.n("POST") != 0 {
		t.Fatalf("err %v log %v", err, g.log)
	}
}

func TestRuntimeGuardRegressionAttestedLegacyBaseMayGainRuntimeKeysInPR(t *testing.T) {
	w, g, _ := probeWire(t)
	g.body = runtimeReply(t, attested(editSourceFor(prRepo, editTestFile)), nil)
	if _, err := w.source(); err != nil {
		t.Fatal(err)
	}
	_, err := w.backend.FactoryPipelinePR(context.Background(), appwire.FactoryPipelinePRParams{Factory: w.id, Repo: prRepo, Text: runtimeSourceFile, ExpectedBaseSHA: prBaseSHA})
	if err != nil || g.n("POST") != 1 || g.n("GET /v1/repos") != 2 {
		t.Fatalf("legacy base attestation: %v posts=%d reads=%d", err, g.n("POST"), g.n("GET /v1/repos"))
	}
}

func TestRuntimeGuardRegressionRuntimeKeysIn(t *testing.T) {
	for text, want := range map[string]bool{
		"steps:\n  a:\n    prompt: x # kind: check\n":          false,
		"steps:\n  a:\n    prompt: \"kind: check\"\n":          false,
		"steps: {a: {kind: check}}\n":                          true,
		"steps:\n  a:\n    \"kind\": check\n":                  true,
		"\uFEFFsteps:\n  a:\n    when: thin\n":                 true,
		"steps:\n  a:\n    ? fanout\n    : one\n":              true,
		"steps:\n  a:\n    <<: {kind: check}\n    prompt: x\n": false,
		"steps:\n  a: x\nsteps:\n  b:\n    kind: check\n":      true,
		"steps:\n  a:\n    kind: check\n---\nsteps: {}\n":      false,
		"steps:\n  a:\n    !!str kind : check\n":               true,
	} {
		if got := pipeline.RuntimeKeysIn(text); got != want {
			t.Errorf("%q: got %v want %v", text, got, want)
		}
		_, probs, editable := pipeline.Inspect(text)
		t.Logf("%q keys=%v editable=%v probs=%d", text, pipeline.RuntimeKeysIn(text), editable, len(probs))
	}
}
