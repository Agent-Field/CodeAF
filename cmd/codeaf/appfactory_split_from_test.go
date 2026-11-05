package main

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// S1: the engine advertises split lineage through factory.capabilities.
func TestAppFactoryCreateSplitFromCapability(t *testing.T) {
	w := newFactoryWire(t, testFactoryHub(t))
	var got map[string]any
	w.must("factory.capabilities", map[string]any{}, &got)
	if got["create_split_from"] != true {
		t.Fatalf("create_split_from = %v, want true", got["create_split_from"])
	}
}

// S2: a split create carries the parent number on either route and its current read.
func TestAppFactoryCreateSplitFromRoutesAndRead(t *testing.T) {
	for _, issue := range []bool{false, true} {
		t.Run(map[bool]string{false: "job", true: "issue"}[issue], func(t *testing.T) {
			var posts atomic.Int64
			stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					io.WriteString(w, `{"id":81,"job_id":"j","version":1,"split_from":80}`)
					return
				}
				posts.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				path := "/v1/jobs"
				if issue {
					path = "/v1/issues"
				}
				if r.Method != http.MethodPost || r.URL.Path != path || body["split_from"] != float64(80) {
					t.Errorf("create request: %s %s %+v", r.Method, r.URL.Path, body)
				}
				io.WriteString(w, `{"factory":{"name":"Stub"},"id":81}`)
			})
			b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
			var made appwire.FactoryJobActed
			wireOver(t, b).must("factory.job_create", map[string]any{
				"factory": conn.ID, "repo": "o/r", "title": "Smaller fix", "open_issue": issue, "split_from": 80,
			}, &made)
			if posts.Load() != 1 || made.Job != 81 || made.Current == nil || made.Current.SplitFrom != 80 {
				t.Fatalf("split receipt: %+v; posts=%d", made, posts.Load())
			}
		})
	}
}

// S2/S4: the real wire and Factory retain lineage without changing the parent.
func TestAppFactoryCreateSplitFromPersistsAndLeavesParent(t *testing.T) {
	f := startActionFactory(t)
	p := signedIn(t, f, 1, "alice")
	parent := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Assignee: "alice"})
	before := p.job(t, parent.ID)
	made := p.act2(t, "factory.job_create", map[string]any{
		"factory": p.conn.ID, "repo": "acme/api", "title": "Smaller fix", "split_from": parent.ID,
	})
	if got := p.job(t, made.Job); got.SplitFrom != parent.ID || got.FromJob != 0 {
		t.Fatalf("split job read: %+v", got)
	}
	if after := p.job(t, parent.ID); !reflect.DeepEqual(before, after) {
		t.Fatal("split create changed the parent's read")
	}
	plain := p.act2(t, "factory.job_create", map[string]any{"factory": p.conn.ID, "repo": "acme/api", "title": "Independent fix"})
	var got map[string]json.RawMessage
	p.w.must("factory.job", map[string]any{"factory": p.conn.ID, "job": plain.Job}, &got)
	if _, exists := got["split_from"]; exists {
		t.Fatal("plain job read includes split_from")
	}
}

// S3: invalid numbers send nothing, and a missing parent keeps the existing 422 mapping.
func TestAppFactoryCreateSplitFromRefusals(t *testing.T) {
	var posts atomic.Int64
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"error":"split_from must name a visible job in this factory"}`)
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	w := wireOver(t, b)
	for _, id := range []int{0, -1} {
		w.mustFail("factory.job_create", map[string]any{
			"factory": conn.ID, "repo": "o/r", "title": "Fix", "split_from": id,
		}, appwire.CodeBadParams)
	}
	if posts.Load() != 0 {
		t.Fatal("invalid split_from was sent to the Factory")
	}
	w.mustFail("factory.job_create", map[string]any{
		"factory": conn.ID, "repo": "o/r", "title": "Fix", "split_from": 80,
	}, appwire.CodeBadParams)
	if posts.Load() != 1 {
		t.Fatalf("missing parent sent %d requests, want 1", posts.Load())
	}
}

// S5: an older strict decoder refuses before creation, with no fallback request.
func TestAppFactoryCreateSplitFromOlderFactory(t *testing.T) {
	var posts, created atomic.Int64
	stub := startStub(t, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		var body struct{ Repo, Title, Text, Source string }
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON body: " + err.Error()})
			return
		}
		created.Add(1)
		io.WriteString(w, `{"id":81}`)
	})
	b, conn := attachStub(t, testFactoryHub(t), stub, actionTestCredential)
	p := &person{w: wireOver(t, b), conn: conn}
	words, detail := p.refused(t, "factory.job_create", map[string]any{
		"factory": conn.ID, "repo": "o/r", "title": "Fix", "split_from": 80,
	}, appwire.CodeConflict)
	if detail == nil || detail.Reason != appwire.FactoryReasonTooOld || words != factoryTooOld().Error() || posts.Load() != 1 || created.Load() != 0 {
		t.Fatalf("old Factory refusal: %+v; posts=%d created=%d", detail, posts.Load(), created.Load())
	}
}

// S3: a hidden parent and a missing parent have the same refusal and no effects.
func TestAppFactoryCreateSplitFromHiddenParent(t *testing.T) {
	f := startActionFactory(t)
	var issues atomic.Int64
	f.github.mu.Lock()
	f.github.rest = func(r *http.Request) (int, string, bool) {
		if r.URL.Path == "/repos/acme/hidden" {
			return http.StatusNotFound, `{}`, true
		}
		if r.Method == http.MethodPost && r.URL.Path == "/repos/acme/api/issues" {
			issues.Add(1)
			return http.StatusCreated, `{"number":42}`, true
		}
		return 0, "", false
	}
	f.github.mu.Unlock()
	p := signedIn(t, f, 1, "alice")
	parent := f.create(t, factory.Item{Repo: "acme/hidden", Kind: factory.KindIssue, State: factory.StateNew})
	p.w.mustFail("factory.job", map[string]any{"factory": p.conn.ID, "job": parent.ID}, appwire.CodeNotFound)
	before, err := f.st.Get(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var refusal string
	for _, issue := range []bool{false, true} {
		for _, id := range []int{parent.ID, 80} {
			words := p.w.mustFail("factory.job_create", map[string]any{
				"factory": p.conn.ID, "repo": "acme/api", "title": "Fix", "split_from": id, "open_issue": issue,
			}, appwire.CodeBadParams)
			if refusal != "" && words != refusal {
				t.Fatalf("hidden/missing parent refusals differ: %q / %q", refusal, words)
			}
			refusal = words
		}
	}
	items, err := f.st.List()
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0], before) || issues.Load() != 0 {
		t.Fatalf("refused split: jobs=%d GitHub=%d err=%v", len(items), issues.Load(), err)
	}
}
