package relayserve_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relayserve/fabrictest"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func TestFabricConformance(t *testing.T) {
	fabrictest.Run(t, func(t *testing.T) *fabrictest.Rig {
		r := newRig(t)
		return &fabrictest.Rig{URL: func() string { return r.srv.URL }, Client: r.srv.Client(), Now: r.clock.Now, Advance: r.clock.Advance, Restart: r.restart}
	})
}

// A body that has not arrived must not prevent another device from revoking.
func TestFabricSlowBodyDoesNotBlockRevocation(t *testing.T) {
	for _, route := range []struct{ method, path string }{{"PUT", "/v1/clients/dev_00000000000000000000000000000000"}, {"POST", "/v1/dir/vault"}} {
		t.Run(route.path, func(t *testing.T) {
			r := newRig(t)
			full, client, _ := twoOfOne(t, r)
			body := &pausedBody{entered: make(chan struct{}), release: make(chan struct{})}
			req := httptest.NewRequest(route.method, route.path, body)
			done := make(chan struct{})
			go func() { defer close(done); r.svc.Handler.ServeHTTP(httptest.NewRecorder(), req) }()
			<-body.entered
			defer func() { close(body.release); <-done }()
			hc := &http.Client{Timeout: 2 * time.Second}
			dir := directory.NewHTTP(r.srv.URL, reqsign.SignFor(full, r.clock.Now), hc)
			if err := dir.Revoke(bg, client.dev.ID()); err != nil {
				t.Fatalf("revoke while anonymous body is unfinished: %v", err)
			}
		})
	}
}

type pausedBody struct{ entered, release chan struct{} }

func (b *pausedBody) Read([]byte) (int, error) { close(b.entered); <-b.release; return 0, io.EOF }
func (*pausedBody) Close() error               { return nil }

func TestFabricSlowResponseDoesNotBlockRevocation(t *testing.T) {
	r := newRig(t)
	full, client, _ := twoOfOne(t, r)
	req := httptest.NewRequest("PUT", "/v1/clients/"+client.dev.ID(), nil)
	// An invalid request still produces a JSON refusal that a reader can stall.
	writer := &pausedResponse{header: make(http.Header), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); r.svc.Handler.ServeHTTP(writer, req) }()
	<-writer.entered
	defer func() { close(writer.release); <-done }()
	dir := directory.NewHTTP(r.srv.URL, reqsign.SignFor(full, r.clock.Now), &http.Client{Timeout: 2 * time.Second})
	if err := dir.Revoke(bg, client.dev.ID()); err != nil {
		t.Fatalf("revoke while response reader is stalled: %v", err)
	}
}

type pausedResponse struct {
	header           http.Header
	entered, release chan struct{}
}

func (w *pausedResponse) Header() http.Header { return w.header }
func (*pausedResponse) WriteHeader(int)       {}
func (w *pausedResponse) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(p), nil
}

func pairTestClient(t *testing.T, r *rig) (device, device) {
	t.Helper()
	id, full := newIdentity(t)
	client := newDevice(t, id, t.TempDir())
	body, err := json.Marshal(map[string]any{"cert": client.Cert(), "methods": []string{"chat.send"}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("PUT", r.srv.URL+"/v1/clients/"+client.dev.ID(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	signAs(req, body, full, r)
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("register client: %d", resp.StatusCode)
	}
	return full, client
}

// TestSlotWaitEndsWith503OnShutdown verifies that closing the relay ends parked slot waits with 503 unavailable (contract R1).
func TestSlotWaitEndsWith503OnShutdown(t *testing.T) {
	r := newRig(t)
	_, client := pairTestClient(t, r)
	path := "/v1/clients/" + client.dev.ID() + "/slots?since=0&wait=25"
	type result struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		req, err := http.NewRequest("GET", r.srv.URL+path, nil)
		if err != nil {
			done <- result{err: err}
			return
		}
		signAs(req, nil, client, r)
		resp, err := r.srv.Client().Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		done <- result{status: resp.StatusCode, body: raw, err: err}
	}()
	time.Sleep(50 * time.Millisecond)
	if err := r.svc.Close(); err != nil {
		t.Fatal(err)
	}
	r.srv.Close()
	r.srv = nil
	select {
	case res := <-done:
		if res.err != nil || res.status != 503 || !bytes.Contains(res.body, []byte(`"unavailable"`)) {
			t.Fatalf("shutdown slot wait: %+v %s", res, res.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("parked slot wait did not return on service shutdown")
	}
}

// TestLegacySlotFileReadsVersionZero verifies that state files written before slot versions read as version 0 (contract R1).
func TestLegacySlotFileReadsVersionZero(t *testing.T) {
	r := newRig(t)
	full, client := pairTestClient(t, r)
	stateDir := filepath.Join(r.store, "fabric", full.id.ID())
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"slots": map[string]any{
			"home": map[string]any{
				"sealed": []byte("legacy"),
				"writer": full.dev.ID(),
				"at":     r.clock.Now().UnixMilli(),
			},
		},
		"queue":       []any{},
		"last_number": 0,
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, client.dev.ID()+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("GET", r.srv.URL+"/v1/clients/"+client.dev.ID()+"/slots/home", nil)
	if err != nil {
		t.Fatal(err)
	}
	signAs(req, nil, client, r)
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Version uint64 `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || got.Version != 0 {
		t.Fatalf("legacy slot version = %d, err = %v", got.Version, err)
	}
}
