package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// E1/E7': the actual wire announces context and posts exactly one versioned replacement.
func TestAppFactoryJobContextSetTransport(t *testing.T) {
	for _, attachments := range [][]appwire.FactoryJobContext{
		{{Title: " reference ", Markdown: strings.Repeat("é", 60000), Source: "guide"}}, {},
	} {
		var posts atomic.Int64
		stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Idempotency-Key") != "" {
				t.Error("context write carried a create key")
			}
			if r.Method == http.MethodGet {
				io.WriteString(w, `{"id":"j","document_id":9,"version":4,"context":[{"title":"reference","source":"guide"}]}`)
				return
			}
			posts.Add(1)
			var body struct {
				Expect  api.Expect                  `json:"expect"`
				Context []appwire.FactoryJobContext `json:"context"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if r.Method != http.MethodPost || r.URL.Path != "/v1/jobs/9/context" || body.Expect.Version == nil || *body.Expect.Version != 3 || len(body.Context) != len(attachments) || body.Context == nil {
				t.Errorf("context request: %s %s %+v", r.Method, r.URL.Path, body)
			}
			for i, c := range body.Context {
				if c.Title != strings.TrimSpace(attachments[i].Title) || c.Markdown != attachments[i].Markdown || c.Source != attachments[i].Source {
					t.Error("attachment changed")
				}
			}
			io.WriteString(w, `{"factory":{"name":"Stub"}}`)
		})
		b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
		wire := wireOver(t, b)
		if !strings.Contains(wire.hello, `factory.job_context`) {
			t.Fatal("hello lacks context feature")
		}
		var got appwire.FactoryJobActed
		wire.must("factory.job_context_set", map[string]any{"factory": conn.ID, "job": 9, "expect": map[string]int{"version": 3}, "context": attachments}, &got)
		if posts.Load() != 1 || got.Action != "context_set" || got.Job != 9 || got.Current == nil || got.Current.ID != 9 {
			t.Fatalf("context receipt: %+v; writes %d", got, posts.Load())
		}
	}
}

// E8'/E15: invalid context, missing versions and the removed create field send nothing.
func TestAppFactoryJobContextBadParamsSendNothing(t *testing.T) {
	var requests atomic.Int64
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			requests.Add(1)
		}
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	w := wireOver(t, b)
	for _, attachments := range []any{
		[]appwire.FactoryJobContext{{Title: "t", Markdown: strings.Repeat("x", 100001)}},
		make([]appwire.FactoryJobContext, 21),
		[]appwire.FactoryJobContext{{Title: "", Markdown: "body"}},
		[]appwire.FactoryJobContext{{Title: strings.Repeat("t", 201), Markdown: "body"}},
		[]appwire.FactoryJobContext{{Title: "t", Markdown: "body", Source: strings.Repeat("s", 501)}},
		[]appwire.FactoryJobContext{{Title: "t"}},
		"reference", []any{map[string]any{"title": "t", "markdown": 7}},
	} {
		w.mustFail("factory.job_context_set", map[string]any{"factory": conn.ID, "job": 9, "expect": map[string]int{"version": 3}, "context": attachments}, appwire.CodeBadParams)
	}
	w.mustFail("factory.job_context_set", map[string]any{"factory": conn.ID, "job": 9, "context": []any{}}, appwire.CodeBadParams)
	w.mustFail("factory.job_create", map[string]any{"factory": conn.ID, "repo": "o/r", "title": "Fix", "context": []any{}}, appwire.CodeBadParams)
	if requests.Load() != 0 {
		t.Fatal("invalid params reached Factory")
	}
}

// E13/E14: mux refusals alone mean too old; real Factory refusals and uncertain outcomes retain their meaning.
func TestAppFactoryJobContextRefusals(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body, code string
		old        bool
	}{
		{"mux 404", 404, "Not Found\n", appwire.CodeConflict, true},
		{"mux 405", 405, "Method Not Allowed\n", appwire.CodeConflict, true},
		{"Factory mux 404", 404, `{"error":"factory endpoint not found","reason":"not_found"}`, appwire.CodeConflict, true},
		{"job missing", 404, `{"error":"job not found","reason":"not_found"}`, appwire.CodeNotFound, false},
		{"Factory method", 405, `{"error":"this endpoint accepts GET"}`, appwire.CodeEngine, false},
		{"started", 409, `{"error":"The job has started; its steps already read their context.","reason":"conflict"}`, appwire.CodeConflict, false},
		{"stale", 409, `{"error":"job moved on while you looked","reason":"stale"}`, appwire.CodeStale, false},
		{"unavailable", 501, `{"error":"this factory does not provide that action"}`, appwire.CodeEngine, false},
		{"uncertain", 503, `{"error":"failed"}`, appwire.CodeUncertain, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int64
			stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					requests.Add(1)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
			version := 3
			_, err := b.FactoryJobContextSet(context.Background(), appwire.FactoryJobContextSetParams{Factory: conn.ID, Job: 9, Expect: &api.Expect{Version: &version}, Context: []appwire.FactoryJobContext{}})
			if wireCodeOf(t, err) != tc.code || requests.Load() != 1 {
				t.Fatalf("refusal %v; requests %d", err, requests.Load())
			}
			if tc.old {
				var e *appwire.Error
				if !errors.As(err, &e) || !reflect.DeepEqual(e.Detail, &appwire.FactoryRefusal{Reason: appwire.FactoryReasonTooOld}) {
					t.Fatalf("too old detail: %v", err)
				}
			}
		})
	}
}

// E14: standard ServeMux responses from genuinely absent routes are compatibility refusals.
func TestAppFactoryJobContextAbsentMuxRoute(t *testing.T) {
	for _, pattern := range []string{"GET /v1/jobs/{id}/context", "/other"} {
		mux := http.NewServeMux()
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {})
		stub := startStub(t, mux.ServeHTTP)
		b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
		version := 3
		_, err := b.FactoryJobContextSet(context.Background(), appwire.FactoryJobContextSetParams{Factory: conn.ID, Job: 9, Expect: &api.Expect{Version: &version}, Context: []appwire.FactoryJobContext{}})
		if wireCodeOf(t, err) != appwire.CodeConflict {
			t.Fatal(err)
		}
	}
}
