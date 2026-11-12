package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// E1: factory.capabilities announces create extras and the separate context write.
func TestAppFactoryCreateCapabilities(t *testing.T) {
	w := newFactoryWire(t, testFactoryHub(t))
	var got map[string]any
	w.must("factory.capabilities", map[string]any{}, &got)
	for _, flag := range []string{"create_issue", "create_group", "create_budget", "job_context", "create_key"} {
		if got[flag] != true {
			t.Errorf("%s = %v", flag, got[flag])
		}
	}
	if _, exists := got["create_context"]; exists {
		t.Fatal("obsolete create_context capability")
	}
}

// E2/E3/E10: create chooses one route, preserves the plain body and scopes the key to that POST.
func TestAppFactoryCreateRoutesBodyAndKey(t *testing.T) {
	for _, issue := range []bool{false, true} {
		t.Run(fmt.Sprintf("issue=%v", issue), func(t *testing.T) {
			var posts atomic.Int64
			stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if r.Header.Get("Idempotency-Key") != "" {
						t.Error("key leaked to the follow-up read")
					}
					io.WriteString(w, `{"job_id":"j","version":1}`)
					return
				}
				posts.Add(1)
				path, key := "/v1/jobs", ""
				want := map[string]any{"repo": "o/r", "title": "Fix", "text": "body", "source": "app"}
				if issue {
					path, key = "/v1/issues", "k1"
					want["issue"], want["group"], want["milestone"], want["budget"] = true, "batch", "release", float64(10)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != path || r.Header.Get("Idempotency-Key") != key || !reflect.DeepEqual(body, want) {
					t.Errorf("request: %s %s key=%q body=%v; want %s key=%q body=%v", r.Method, r.URL.Path, r.Header.Get("Idempotency-Key"), body, path, key, want)
				}
				io.WriteString(w, `{"factory":{"name":"Stub"},"id":9}`)
			})
			b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
			wire := wireOver(t, b)
			p := map[string]any{"factory": conn.ID, "repo": "o/r", "title": "Fix", "text": "body"}
			if issue {
				p["open_issue"], p["group"], p["milestone"], p["budget"], p["key"] = true, "batch", "release", 10, "k1"
			}
			var made appwire.FactoryJobActed
			wire.must("factory.job_create", p, &made)
			if made.Action != appwire.JobActCreate || made.Job != 9 || posts.Load() != 1 {
				t.Fatalf("receipt %+v; posts %d", made, posts.Load())
			}
		})
	}
}

// E4/E11: old routes and strict fields become a typed refusal; other errors keep their meaning.
func TestAppFactoryCreateOlderFactoryRefusals(t *testing.T) {
	cases := []struct {
		name, words, code string
		status            int
		issue, old        bool
	}{
		{"issue 404", api.EndpointNotFound, appwire.CodeConflict, 404, true, true},
		{"issue 405", "method not allowed", appwire.CodeConflict, 405, true, true},
		{"plain 404", "not found", appwire.CodeNotFound, 404, false, false},
		{"unavailable", "unavailable", appwire.CodeEngine, 501, true, false},
		{"uncertain", "failed", appwire.CodeUncertain, 503, true, false},
		{"GitHub sign-in", "Reconnect GitHub", appwire.CodeSignedOut, 401, true, false},
		{"other validation", "invalid title", appwire.CodeBadParams, 422, false, false},
	}
	for _, field := range []string{"group", "milestone", "budget", "issue"} {
		cases = append(cases, struct {
			name, words, code string
			status            int
			issue, old        bool
		}{field, `invalid JSON body: json: unknown field "` + field + `"`, appwire.CodeConflict, 422, true, true})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int64
			stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					posts.Add(1)
				}
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]string{"error": tc.words})
			})
			b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
			_, err := b.FactoryJobCreate(context.Background(), appwire.FactoryJobCreateParams{Factory: conn.ID, Repo: "o/r", Title: "Fix", OpenIssue: tc.issue, Group: "batch"})
			if wireCodeOf(t, err) != tc.code || posts.Load() != 1 {
				t.Fatalf("refusal %v; requests %d", err, posts.Load())
			}
			if tc.old {
				var wire *appwire.Error
				if !errors.As(err, &wire) || wire.Error() != factoryTooOld().Error() {
					t.Fatalf("old factory words: %v", err)
				}
				d, ok := wire.Detail.(*appwire.FactoryRefusal)
				if !ok || d.Reason != appwire.FactoryReasonTooOld {
					t.Fatalf("old factory detail: %#v", wire.Detail)
				}
			}
		})
	}
}

// A 404 on the issue create route is an older Factory only when it is the
// mux's missing route (api.EndpointNotFound) or not the Factory's JSON at all.
// The current Factory's own 404, "repository not found" from its visibility
// check, keeps the plain not_found mapping and its words.
func TestAppFactoryIssueCreateNotFoundIsTooOldOnlyForAMissingRoute(t *testing.T) {
	for _, tc := range []struct {
		name, body, code, words string
		status                  int
		old                     bool
	}{
		{"hidden repository", `{"error":"repository not found","request_id":"ab12cd34"}`, appwire.CodeNotFound, "repository not found", 404, false},
		{"missing route", `{"error":"` + api.EndpointNotFound + `"}`, appwire.CodeConflict, "", 404, true},
		{"not JSON", "404 page not found\n", appwire.CodeConflict, "", 404, true},
		{"route without POST", `{"error":"this endpoint accepts GET"}`, appwire.CodeConflict, "", 405, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
			_, err := b.FactoryJobCreate(context.Background(), appwire.FactoryJobCreateParams{Factory: conn.ID, Repo: "o/r", Title: "Fix", OpenIssue: true})
			var wire *appwire.Error
			if !errors.As(err, &wire) || wire.Code != tc.code {
				t.Fatalf("refusal %v, want code %s", err, tc.code)
			}
			d, _ := wire.Detail.(*appwire.FactoryRefusal)
			tooOld := d != nil && d.Reason == appwire.FactoryReasonTooOld
			if tooOld != tc.old || (tc.old && wire.Error() != factoryTooOld().Error()) || (!tc.old && wire.Message != tc.words) {
				t.Fatalf("refusal %q detail %#v; want too old=%v words %q", wire.Error(), wire.Detail, tc.old, tc.words)
			}
		})
	}
}

// E5/E6/E10/E15: bad params and removed context send nothing to the fake Factory.
func TestAppFactoryCreateInvalidExtrasSendNothing(t *testing.T) {
	var posts atomic.Int64
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
		}
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	w := wireOver(t, b)
	for field, value := range map[string]any{
		"group": strings.Repeat("g", 201), "milestone": "bad\x00label", "budget": 7,
		"context": []appwire.FactoryJobContext{{Title: "reference"}}, "key": "a b",
	} {
		w.mustFail("factory.job_create", map[string]any{"factory": conn.ID, "repo": "o/r", "title": "Fix", field: value}, appwire.CodeBadParams)
	}
	if posts.Load() != 0 {
		t.Fatal("bad params reached the Factory")
	}
}

// E10: a key forwarded through the real wire and store-backed Factory makes one job.
func TestAppFactoryCreateKeyReplaysJobNumber(t *testing.T) {
	f := startActionFactory(t)
	p := signedIn(t, f, 1, "alice")
	params := map[string]any{"factory": p.conn.ID, "repo": "acme/api", "title": "Fix", "key": "k1"}
	first := p.act2(t, "factory.job_create", params)
	second := p.act2(t, "factory.job_create", params)
	if first.Job < 1 || first.Job != second.Job {
		t.Fatalf("replayed job numbers: %d / %d", first.Job, second.Job)
	}
	items, err := f.st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("same key: jobs=%d err=%v", len(items), err)
	}
	params["key"] = "k2"
	third := p.act2(t, "factory.job_create", params)
	items, err = f.st.List()
	if err != nil || len(items) != 2 || third.Job == first.Job {
		t.Fatalf("different key: jobs=%d number=%d err=%v", len(items), third.Job, err)
	}
}
