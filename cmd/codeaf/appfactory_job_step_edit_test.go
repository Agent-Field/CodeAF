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

// stepFactory is a stub of the factory's step-edits route. It answers POST with
// whatever status the test set, and keeps every request it received.
type stepFactory struct {
	mu     sync.Mutex
	status int
	reply  string
	job    string // the body of GET /v1/jobs/7
	hang   bool
	gets   []string
	posts  []string
	bodies []string
	url    string
}

func startStepFactory(t *testing.T) *stepFactory {
	t.Helper()
	f := &stepFactory{status: 200, job: `{"id":"7","document_id":7,"version":5}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/about" || r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(api.Me{Contract: api.Contract, Factory: api.Factory{Name: "acme"}, Mode: api.FactoryTeam})
		case r.URL.Path == "/v1/jobs/7/step-edits" && r.Method == http.MethodGet:
			f.gets = append(f.gets, r.URL.Path)
			_, _ = io.WriteString(w, `{"factory":{"name":"acme"},"steps":[{"name":"plan"}],"version":4,"lines":[]}`)
		case r.URL.Path == "/v1/jobs/7/step-edits" && r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			f.posts = append(f.posts, r.URL.Path)
			f.bodies = append(f.bodies, string(b))
			if f.hang {
				if hj, ok := w.(http.Hijacker); ok {
					c, _, _ := hj.Hijack()
					c.Close()
					return
				}
			}
			w.WriteHeader(f.status)
			_, _ = io.WriteString(w, f.reply)
		case r.URL.Path == "/v1/jobs/7" && r.Method == http.MethodGet:
			f.gets = append(f.gets, r.URL.Path)
			_, _ = io.WriteString(w, f.job)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func (f *stepFactory) counts() (gets, posts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.gets), len(f.posts)
}

func attachStep(t *testing.T, f *stepFactory) (*factoryWire, string) {
	t.Helper()
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": f.url, "token": testHostToken}, &conn)
	return wire, conn.ID
}

func TestAppFactoryJobStepEditReadsTheVersionAndAnnouncesTheCarrier(t *testing.T) {
	f := startStepFactory(t)
	wire, id := attachStep(t, f)
	if !strings.Contains(wire.hello, appwire.FeatureFactoryJobStepEdit) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobStepEdit, wire.hello)
	}
	var got appwire.FactoryJobStepEdits
	wire.must("factory.job_step_edits_read", map[string]any{"factory": id, "job": 7}, &got)
	if got.Job != 7 || got.Version != 4 || len(got.Steps) != 1 || got.Steps[0].Name != "plan" || got.Lines == nil || len(got.Lines) != 0 {
		t.Fatalf("read = %+v", got)
	}
	if _, posts := f.counts(); posts != 0 {
		t.Fatal("a read sent a write")
	}
}

// THE WRITE GOES TO THE ONE FIXED ROUTE, ONCE, WITH EXACTLY THE FIELDS THE EDIT
// NAMED, AND IS FOLLOWED BY ONE REREAD.
func TestAppFactoryJobStepEditSendsOnceAndRereadsOnce(t *testing.T) {
	f := startStepFactory(t)
	f.reply = `{"factory":{"name":"acme"},"steps":[{"name":"plan"},{"name":"lint"}],"version":5,"lines":["you added lint after plan"]}`
	wire, id := attachStep(t, f)
	var out appwire.FactoryJobStepEdited
	wire.must("factory.job_step_edit", map[string]any{
		"factory": id, "job": 7, "expect": map[string]any{"version": 4},
		"edit": map[string]any{"add": []any{map[string]any{"step": map[string]any{"name": "lint", "ask": "run it", "effort": "cheap"}, "after": "plan"}}, "why": "more checks"},
	}, &out)
	gets, posts := f.counts()
	if posts != 1 || gets != 1 {
		t.Fatalf("posts %d gets %d, want 1 and 1", posts, gets)
	}
	if out.Version != 5 || len(out.Steps) != 2 || len(out.Lines) != 1 || out.Current == nil || out.Current.Version != 5 {
		t.Fatalf("out = %+v", out)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(f.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	// Exactly add, why and expect; no author, no route, no key the edit did not name.
	if len(body) != 3 || body["why"] != "more checks" || body["expect"].(map[string]any)["version"] != float64(4) {
		t.Fatalf("body = %s", f.bodies[0])
	}
	step := body["add"].([]any)[0].(map[string]any)["step"].(map[string]any)
	if len(step) != 3 || step["name"] != "lint" || step["ask"] != "run it" || step["effort"] != "cheap" {
		t.Fatalf("step = %v", step)
	}
}

func TestAppFactoryJobStepEditNeverSendsWithoutAVersionOrWithUnsupportedFields(t *testing.T) {
	f := startStepFactory(t)
	wire, id := attachStep(t, f)
	edit := map[string]any{"skip": []string{"plan"}}
	wire.mustFail("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "edit": edit}, appwire.CodeBadParams)
	wire.mustFail("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"state": "running"}, "edit": edit}, appwire.CodeBadParams)
	wire.mustFail("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4},
		"edit": map[string]any{"add": []any{map[string]any{"step": map[string]any{"name": "lint", "ask": "x", "max": 9}}}}}, appwire.CodeBadParams)
	wire.mustFail("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": edit, "by": "manager"}, appwire.CodeBadParams)
	if _, posts := f.counts(); posts != 0 {
		t.Fatalf("%d refused requests reached the factory", posts)
	}
}

// THE FACTORY'S REFUSALS ARE CARRIED WHOLE AND NEVER SENT AGAIN.
func TestAppFactoryJobStepEditRefusalsAndUncertainty(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		code   string
		hang   bool
	}{
		{"stale", 409, `{"error":"job moved on while you looked","reason":"stale","now_state":"running"}`, appwire.CodeStale, false},
		{"finished", 409, `{"error":"finished or publishing jobs cannot edit steps"}`, appwire.CodeConflict, false},
		{"not the job's person", 403, `{"error":"only @bob may do that","reason":"not_job_person","job_person":"@bob"}`, appwire.CodeForbidden, false},
		{"refused part", 409, `{"error":"you may not change build, which has already run","reason":"conflict"}`, appwire.CodeConflict, false},
		{"unsupported field", 422, `{"error":"new steps accept name, kind, ask, until and effort"}`, appwire.CodeBadParams, false},
		{"missing route", 404, `{"error":"factory endpoint not found"}`, appwire.CodeNotFound, false},
		{"no edit seam or worker desk", 501, `{"error":"this factory does not provide that action"}`, appwire.CodeEngine, false},
		{"factory failed", 500, `{"error":"store failed"}`, appwire.CodeUncertain, false},
		{"connection dropped", 0, ``, appwire.CodeUncertain, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := startStepFactory(t)
			f.status, f.reply, f.hang = c.status, c.reply, c.hang
			wire, id := attachStep(t, f)
			wire.mustFail("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": map[string]any{"skip": []string{"plan"}}}, c.code)
			gets, posts := f.counts()
			if posts != 1 {
				t.Fatalf("the write was sent %d times", posts)
			}
			if gets != 0 {
				t.Fatalf("a failed write was followed by %d reads", gets)
			}
		})
	}
}

// A 2xx THAT IS NOT A WHOLE RECEIPT IS NEVER AN EMPTY EDIT: IT IS uncertain, SENT
// ONCE, WITH NO REREAD.
func TestAppFactoryJobStepEditIncompleteYesIsUncertain(t *testing.T) {
	cases := map[string]string{
		"not json":          `not json`,
		"empty body":        ``,
		"json null":         `null`,
		"an array":          `[]`,
		"an error in a 2xx": `{"error":"no worker desk"}`,
		"missing steps":     `{"factory":{"name":"acme"},"version":5,"lines":["x"]}`,
		"missing lines":     `{"factory":{"name":"acme"},"steps":[{"name":"plan"}],"version":5}`,
		"missing version":   `{"factory":{"name":"acme"},"steps":[{"name":"plan"}],"lines":["x"]}`,
		"zero version":      `{"steps":[],"lines":[],"version":0}`,
		"negative version":  `{"steps":[],"lines":[],"version":-1}`,
		"older version":     `{"steps":[{"name":"plan"}],"lines":[],"version":3}`,
		"steps not array":   `{"steps":"plan","lines":[],"version":5}`,
		"lines not array":   `{"steps":[],"lines":"x","version":5}`,
		"version a string":  `{"steps":[],"lines":[],"version":"5"}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			f := startStepFactory(t)
			f.reply = reply
			wire, id := attachStep(t, f)
			wire.mustFail("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": map[string]any{"on": []string{"plan"}}}, appwire.CodeUncertain)
			if gets, posts := f.counts(); posts != 1 || gets != 0 {
				t.Fatalf("posts %d gets %d, want 1 and 0", posts, gets)
			}
		})
	}
}

// A REAL NO-OP: THE SAME VERSION, EMPTY LINES (null OR []) ARE A CONFIRMED YES.
func TestAppFactoryJobStepEditConfirmedNoopAtTheSameVersion(t *testing.T) {
	for name, reply := range map[string]string{
		"empty arrays": `{"factory":{"name":"acme"},"steps":[{"name":"plan"}],"version":4,"lines":[]}`,
		"null arrays":  `{"factory":{"name":"acme"},"steps":null,"version":4,"lines":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := startStepFactory(t)
			f.reply = reply
			f.job = `{"id":"7","document_id":7,"version":4}`
			wire, id := attachStep(t, f)
			var out appwire.FactoryJobStepEdited
			wire.must("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": map[string]any{"on": []string{"plan"}}}, &out)
			if out.Job != 7 || out.Version != 4 || out.Steps == nil || out.Lines == nil || len(out.Lines) != 0 || out.Current == nil {
				t.Fatalf("out = %+v", out)
			}
			if gets, posts := f.counts(); posts != 1 || gets != 1 {
				t.Fatalf("posts %d gets %d, want 1 and 1", posts, gets)
			}
		})
	}
}

// THE REREAD IS KEPT ONLY WHEN IT IS THIS JOB AT A VERSION NOT OLDER THAN THE RECEIPT.
func TestAppFactoryJobStepEditOmitsAForeignOrOlderCurrent(t *testing.T) {
	const receipt = `{"factory":{"name":"acme"},"steps":[{"name":"plan"}],"version":5,"lines":["you switched plan on"]}`
	cases := map[string]struct {
		job  string
		kept bool
	}{
		"this job, newer":    {`{"id":"7","document_id":7,"version":6}`, true},
		"foreign document":   {`{"id":"8","document_id":8,"version":5}`, false},
		"no document id":     {`{"id":"7","version":5}`, false},
		"older than expect":  {`{"id":"7","document_id":7,"version":3}`, false},
		"older than receipt": {`{"id":"7","document_id":7,"version":4}`, false},
		"no store behind it": {`{"id":"7","document_id":7}`, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := startStepFactory(t)
			f.reply, f.job = receipt, c.job
			wire, id := attachStep(t, f)
			var out appwire.FactoryJobStepEdited
			wire.must("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": map[string]any{"on": []string{"plan"}}}, &out)
			if out.Version != 5 || len(out.Lines) != 1 {
				t.Fatalf("the receipt was lost: %+v", out)
			}
			if (out.Current != nil) != c.kept {
				t.Fatalf("current = %+v, kept want %v", out.Current, c.kept)
			}
		})
	}
}

// A FACTORY OLDER THAN REMOVE AND MOVE refuses them as fields it does not
// know, and that is said as a factory that doesn't support them yet. The same
// status with the factory's own sentence, or on an edit that sends neither, is
// carried as it was.
func TestAppFactoryJobStepEditOnAnOlderFactoryIsSaidAsNotSupported(t *testing.T) {
	cases := []struct {
		name  string
		edit  map[string]any
		reply string
		code  string
		says  string
	}{
		{"remove", map[string]any{"remove": []string{"plan"}}, `{"error":"invalid JSON body: json: unknown field \"remove\""}`, appwire.CodeConflict, "doesn't support that yet"},
		{"move", map[string]any{"move": []map[string]any{{"step": "plan", "after": ""}}}, `{"error":"invalid JSON body: json: unknown field \"move\""}`, appwire.CodeConflict, "doesn't support that yet"},
		{"a remove the factory refused", map[string]any{"remove": []string{"proof"}}, `{"error":"you may not remove proof; proof vouches for the work before it"}`, appwire.CodeBadParams, "you may not remove proof"},
		{"a skip", map[string]any{"skip": []string{"plan"}}, `{"error":"invalid JSON body: json: unknown field \"skip\""}`, appwire.CodeBadParams, "unknown field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := startStepFactory(t)
			f.status, f.reply = 422, c.reply
			wire, id := attachStep(t, f)
			msg := wire.mustFail("factory.job_step_edit", map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": c.edit}, c.code)
			if !strings.Contains(msg, c.says) || strings.Contains(msg, "can see") {
				t.Fatalf("said %q", msg)
			}
		})
	}
}
