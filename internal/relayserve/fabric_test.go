package relayserve_test

import (
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/relayserve/fabrictest"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
