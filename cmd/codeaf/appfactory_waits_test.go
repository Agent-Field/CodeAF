package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func waitsBackend(t *testing.T, handler http.HandlerFunc) *appBackend {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	h := testFactoryHub(t)
	h.loaded, h.rows = true, []appFactoryRow{{ID: "f", URL: srv.URL, Mode: api.FactoryTeam}}
	h.hold("f", "waits-fixture-bearer-credential", 0)
	b := &appBackend{}
	b.factoryOnce.Do(func() { b.factories = h })
	t.Cleanup(h.close)
	return b
}

// W9: discovery reads the connected factory and carries its held authority.
func TestFactoryWaitsFeatures(t *testing.T) {
	for _, about := range []string{`{"features":["waits_on"]}`, `{}`, `{"features":null}`} {
		t.Run(about, func(t *testing.T) {
			var reads atomic.Int64
			b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/v1/about" || r.Header.Get("Authorization") != "Bearer waits-fixture-bearer-credential" {
					t.Errorf("unexpected features read: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(about))
			})
			got, err := b.FactoryFeatures(context.Background(), appwire.FactoryFeaturesParams{Factory: "f"})
			want := []string{}
			if strings.Contains(about, "waits_on") {
				want = []string{"waits_on"}
			}
			if err != nil || !reflect.DeepEqual(got.Features, want) || reads.Load() != 1 {
				t.Fatalf("features = %+v, %v; reads = %d", got, err, reads.Load())
			}
			caps, err := b.FactoryCapabilities(context.Background())
			if err != nil || !caps.WaitsOn {
				t.Fatalf("capabilities = %+v, %v", caps, err)
			}
		})
	}
}

// W9: older routes and strict decoders refuse clearly, while 422 and stale keep their mappings.
func TestFactoryWaitsRefusals(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		answer, code, reason string
	}{
		{"missing", 404, `{"error":"factory endpoint not found"}`, appwire.CodeConflict, "factory_too_old"},
		{"method", 405, `{"error":"this endpoint accepts GET"}`, appwire.CodeConflict, "factory_too_old"},
		{"old field", 400, `{"error":"json: unknown field \"waits_on\""}`, appwire.CodeConflict, "factory_too_old"},
		{"invalid", 422, `{"error":"a job cannot wait on itself"}`, appwire.CodeBadParams, ""},
		{"stale", 409, `{"error":"job moved on while you looked","reason":"stale"}`, appwire.CodeStale, "stale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int64
			b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) {
				writes.Add(1)
				if r.Method != http.MethodPut || r.URL.Path != "/v1/jobs/1/waits_on" {
					t.Errorf("unexpected write: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.answer))
			})
			_, err := b.FactoryWaitsSet(context.Background(), appwire.FactoryWaitsSetParams{Factory: "f", Job: 1, WaitsOn: []int{2}})
			var wire *appwire.Error
			if !errors.As(err, &wire) || wire.Code != tc.code || writes.Load() != 1 {
				t.Fatalf("refusal = %v; writes = %d", err, writes.Load())
			}
			detail, _ := wire.Detail.(*appwire.FactoryRefusal)
			if tc.reason != "" && (detail == nil || detail.Reason != tc.reason) {
				t.Fatalf("detail = %+v", wire.Detail)
			}
			if tc.reason == "factory_too_old" && wire.Message != "This factory doesn't support that yet; nothing was changed. Update the factory to use it." {
				t.Fatalf("old factory words = %q", wire.Message)
			}
			if tc.status == 422 && wire.Message != "a job cannot wait on itself" {
				t.Fatalf("422 words = %q", wire.Message)
			}
		})
	}
}

// W9, W11: one PUT is followed by exactly one optional current-job read.
func TestFactoryWaitsSetReadsCurrentOnce(t *testing.T) {
	for _, failRead := range []bool{false, true} {
		var writes, reads atomic.Int64
		b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPut:
				writes.Add(1)
				var body struct {
					WaitsOn []int       `json:"waits_on"`
					Expect  *api.Expect `json:"expect"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !reflect.DeepEqual(body.WaitsOn, []int{2, 3}) || body.Expect == nil || body.Expect.Version == nil || *body.Expect.Version != 7 {
					t.Errorf("body = %+v, %v", body, err)
				}
				_, _ = w.Write([]byte(`{"id":1}`))
			case http.MethodGet:
				reads.Add(1)
				if r.URL.Path != "/v1/jobs/1" {
					t.Errorf("unexpected current read: %s", r.URL.Path)
				}
				if failRead {
					w.WriteHeader(503)
					return
				}
				_, _ = w.Write([]byte(`{"id":1,"state":"backlog","waits_on":[2,3],"waiting_on":[3]}`))
			default:
				t.Errorf("unexpected method %s", r.Method)
			}
		})
		rev := 7
		got, err := b.FactoryWaitsSet(context.Background(), appwire.FactoryWaitsSetParams{Factory: "f", Job: 1, WaitsOn: []int{2, 3}, Expect: &api.Expect{Version: &rev}})
		if err != nil || got.Factory != "f" || got.Job != 1 || !reflect.DeepEqual(got.WaitsOn, []int{2, 3}) || writes.Load() != 1 || reads.Load() != 1 {
			t.Fatalf("answer = %+v, %v; writes/reads = %d/%d", got, err, writes.Load(), reads.Load())
		}
		if failRead && got.Current != nil || !failRead && (got.Current == nil || !reflect.DeepEqual(got.Current.WaitingOn, []int{3})) {
			t.Fatalf("current = %+v", got.Current)
		}
	}
}

// W10: direct backend callers also cannot send an invalid wait list.
func TestFactoryWaitsBackendBounds(t *testing.T) {
	var sends atomic.Int64
	b := waitsBackend(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); w.WriteHeader(500) })
	for _, ids := range [][]int{{0}, {-1}, {2, 2}, make([]int, 21)} {
		_, err := b.FactoryWaitsSet(context.Background(), appwire.FactoryWaitsSetParams{Factory: "f", Job: 1, WaitsOn: ids})
		var wire *appwire.Error
		if !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
			t.Fatalf("invalid = %v", err)
		}
	}
	if sends.Load() != 0 {
		t.Fatalf("invalid sends = %d", sends.Load())
	}
}
