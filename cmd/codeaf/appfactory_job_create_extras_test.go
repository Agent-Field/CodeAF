package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// E1: factory.capabilities announces create extras and the separate context
// write. create_key alone also speaks for one Factory: it is true only in an
// answer about a named Factory whose GET /v1/features lists keyed_create, since
// an older one keeps every keyed answer, refusals and 5xx included, for a day.
func TestAppFactoryCreateCapabilities(t *testing.T) {
	keyed := startStub(t, featuresStub(`{"features":["keyed_create","group_policy"]}`, http.StatusOK))
	plain := startStub(t, featuresStub(`{"features":["group_policy","waits_on"]}`, http.StatusOK))
	older := startStub(t, featuresStub(`{"error":"`+api.EndpointNotFound+`"}`, http.StatusNotFound))
	broken := startStub(t, featuresStub(`{"error":"store is busy"}`, http.StatusInternalServerError))
	hub := testFactoryHub(t)
	b, keyedConn := attachStub(t, hub, keyed, actionTestCredential)
	ids := map[string]string{"keyed": keyedConn.ID}
	for name, s := range map[string]*stubFactory{"plain": plain, "older": older, "broken": broken} {
		conn, err := b.FactoryAttach(context.Background(), s.srv.URL, actionTestCredential)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = conn.ID
	}
	w := wireOver(t, b)
	for _, c := range []struct {
		name, factory string
		key           bool
	}{
		{"no factory named", "", false},
		{"a factory that says keyed_create", ids["keyed"], true},
		{"a factory that does not say it", ids["plain"], false},
		{"a factory older than /v1/features", ids["older"], false},
		{"a factory whose features cannot be read", ids["broken"], false},
		{"a factory this engine does not hold", "factory_nope", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			params := map[string]any{}
			if c.factory != "" {
				params["factory"] = c.factory
			}
			var got map[string]any
			w.must("factory.capabilities", params, &got)
			for _, flag := range []string{"create_issue", "create_group", "create_budget", "job_context"} {
				if got[flag] != true {
					t.Errorf("%s = %v", flag, got[flag])
				}
			}
			if got["create_key"] != c.key {
				t.Fatalf("create_key = %v, want %v", got["create_key"], c.key)
			}
			if _, exists := got["create_context"]; exists {
				t.Fatal("obsolete create_context capability")
			}
		})
	}
}

// stubRequests is how many requests the stub saw that start with prefix.
func stubRequests(s *stubFactory, prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// featuresStub answers GET /v1/features with body and status and refuses
// everything else, so a test sees any request it did not expect.
func featuresStub(body string, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/features" {
			w.WriteHeader(status)
			io.WriteString(w, body)
			return
		}
		w.WriteHeader(http.StatusTeapot)
		io.WriteString(w, `{"error":"unexpected request"}`)
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
						t.Error("key leaked to a read")
					}
					if r.URL.Path == "/v1/features" {
						io.WriteString(w, `{"features":["keyed_create"]}`)
						return
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

// A CREATE KEY GOES ONLY TO A FACTORY THAT SAYS keyed_create. An older one
// keeps every keyed answer for a day, a refusal or a 5xx included, so a draft
// it refused once could not launch again under its key. A keyed create to it
// is refused as too old and sends no create at all; an unkeyed create still
// goes, and never asks the Factory what it keeps.
func TestAppFactoryCreateKeyGoesOnlyToAFactoryThatSaysSo(t *testing.T) {
	for _, c := range []struct {
		name, features, code string
		status               int
		keyed                bool
	}{
		{"says keyed_create", `{"features":["group_policy","keyed_create"]}`, "", http.StatusOK, true},
		{"does not say it", `{"features":["group_policy","waits_on"]}`, appwire.CodeConflict, http.StatusOK, false},
		{"says nothing", `{"features":[]}`, appwire.CodeConflict, http.StatusOK, false},
		{"older than /v1/features", `{"error":"` + api.EndpointNotFound + `"}`, appwire.CodeConflict, http.StatusNotFound, false},
		{"cannot say now", `{"error":"store is busy"}`, appwire.CodeEngine, http.StatusInternalServerError, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var keys []string
			stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/v1/features" {
					w.WriteHeader(c.status)
					io.WriteString(w, c.features)
					return
				}
				if r.Method == http.MethodGet {
					io.WriteString(w, `{"id":9,"version":1}`)
					return
				}
				mu.Lock()
				keys = append(keys, r.Header.Get("Idempotency-Key"))
				mu.Unlock()
				io.WriteString(w, `{"factory":{"name":"Stub"},"id":9}`)
			})
			b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
			wire := wireOver(t, b)
			// A factory this engine does not hold is not_found, key or not.
			wire.mustFail("factory.job_create", map[string]any{"factory": "factory_nope", "repo": "o/r", "title": "Fix", "key": "k"}, appwire.CodeNotFound)
			p := map[string]any{"factory": conn.ID, "repo": "o/r", "title": "Fix", "key": "workstream:2:ws:5:draft"}
			if c.keyed {
				wire.must("factory.job_create", p, &appwire.FactoryJobActed{})
				if len(keys) != 1 || keys[0] != "workstream:2:ws:5:draft" {
					t.Fatalf("creates sent with keys %q, want one with the key", keys)
				}
			} else {
				wire.mustFail("factory.job_create", p, c.code)
				_, err := b.FactoryJobCreate(context.Background(), appwire.FactoryJobCreateParams{Factory: conn.ID, Repo: "o/r", Title: "Fix", Key: "workstream:2:ws:5:draft"})
				if len(keys) != 0 {
					t.Fatalf("a keyed create reached a factory that did not say keyed_create: keys %q", keys)
				}
				var refusal *appwire.Error
				if c.code == appwire.CodeConflict {
					if !errors.As(err, &refusal) || refusal.Error() != factoryTooOld().Error() {
						t.Fatalf("refusal %v, want the too-old words", err)
					}
					if d, _ := refusal.Detail.(*appwire.FactoryRefusal); d == nil || d.Reason != appwire.FactoryReasonTooOld {
						t.Fatalf("refusal detail %#v, want reason %s", refusal.Detail, appwire.FactoryReasonTooOld)
					}
				}
			}
			delete(p, "key")
			before := stubRequests(stub, "GET /v1/features")
			wire.must("factory.job_create", p, &appwire.FactoryJobActed{})
			mu.Lock()
			defer mu.Unlock()
			if last := keys[len(keys)-1]; last != "" {
				t.Fatalf("an unkeyed create carried key %q", last)
			}
			if after := stubRequests(stub, "GET /v1/features"); after != before {
				t.Fatal("an unkeyed create asked the factory what it keeps")
			}
		})
	}
}

// E10: a key forwarded through the real wire and store-backed Factory makes
// one job when the Factory says keyed_create, and is never sent to the same
// Factory when it does not say so. A front door adds or hides the word, so this
// holds with this branch's Factory and with one that says it itself.
func TestAppFactoryCreateKeyReplaysJobNumber(t *testing.T) {
	f := startActionFactory(t)
	var mu sync.Mutex
	var keys []string
	front := func(say bool) string {
		target, _ := url.Parse(f.URL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/features" {
				words := []string{"group_policy", "waits_on"}
				if say {
					words = append(words, "keyed_create")
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string][]string{"features": words})
				return
			}
			if r.Method == http.MethodPost && r.URL.Path == "/v1/jobs" {
				mu.Lock()
				keys = append(keys, r.Header.Get("Idempotency-Key"))
				mu.Unlock()
			}
			proxy.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	p := signedInAt(t, f, front(true), 1, "alice")
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
	if len(keys) != 3 || keys[0] != "k1" || keys[2] != "k2" {
		t.Fatalf("keys the factory got: %q", keys)
	}

	old := signedInAt(t, f, front(false), 2, "bob")
	msg, refusal := old.refused(t, "factory.job_create", map[string]any{"factory": old.conn.ID, "repo": "acme/api", "title": "Fix", "key": "k3"}, appwire.CodeConflict)
	items, err = f.st.List()
	if msg != factoryTooOld().(*appwire.Error).Message || refusal == nil || refusal.Reason != appwire.FactoryReasonTooOld || err != nil || len(items) != 2 || len(keys) != 3 {
		t.Fatalf("a keyed create to a factory that does not say keyed_create: %q %+v, jobs=%d, keys %q", msg, refusal, len(items), keys)
	}
}
