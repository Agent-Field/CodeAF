package tui3

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestProbeOpenAIEndpointSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"data": []map[string]any{
				{"id": "m1"},
				{"id": "m2"},
				{"id": "m3"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	count, err := ProbeOpenAIEndpoint(context.Background(), ts.URL+"/v1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 models, got %d", count)
	}
}

func TestProbeOpenAIEndpointAuthRequired(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := map[string]any{"data": []map[string]any{{"id": "secret-model"}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	_, err := ProbeOpenAIEndpoint(context.Background(), ts.URL+"/v1", "")
	if err != errAuthRequired {
		t.Fatalf("expected errAuthRequired, got %v", err)
	}

	count, err := ProbeOpenAIEndpoint(context.Background(), ts.URL+"/v1", "test-key")
	if err != nil {
		t.Fatalf("unexpected error with key: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 model, got %d", count)
	}
}

func TestProbeLoopbackPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot bind to loopback ephemeral port")
	}
	port := listener.Addr().(*net.TCPAddr).Port

	ts := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/models") {
				resp := map[string]any{
					"data": []map[string]any{
						{"id": "local-model-1"},
						{"id": "local-model-2"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
				return
			}
			http.NotFound(w, r)
		}),
	}
	go ts.Serve(listener)
	defer ts.Close()

	probe := probeLoopbackPort(context.Background(), port, "test-server :"+strconv.Itoa(port))
	if probe == nil {
		t.Fatalf("expected probe to find server on port %d, got nil", port)
	}
	if probe.Models != 2 {
		t.Fatalf("expected 2 models, got %d", probe.Models)
	}
}

func TestAddProviderPanelRebuild(t *testing.T) {
	var p addProviderPanel
	p.rebuild(nil, nil)
	if len(p.items) == 0 {
		t.Fatal("expected items, got none")
	}
	if p.items[0].title != "providers" {
		t.Fatalf("expected 'providers' heading, got %q", p.items[0].title)
	}
	if p.cursor != 1 {
		t.Fatalf("expected cursor at first item (1), got %d", p.cursor)
	}

	probes := []LocalServerProbe{
		{Port: 8317, Name: "127.0.0.1:8317", Address: "http://127.0.0.1:8317/v1", Models: 12},
	}
	p.rebuild(probes, nil)
	if p.items[0].title != "found on this machine" {
		t.Fatalf("expected 'found on this machine' heading, got %q", p.items[0].title)
	}
	if p.cursor != 1 {
		t.Fatalf("expected cursor on first probed item (1), got %d", p.cursor)
	}
	cur, ok := p.current()
	if !ok || cur.probe == nil || cur.probe.Port != 8317 {
		t.Fatalf("expected cursor on probe 8317, got %+v", cur)
	}
}
