package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func newCreateExtrasJobs(t *testing.T) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	f.seam.NewJob = factory.LocalSeam(f.st, now).NewJob
	reopenJobs(t, f)
	return f
}

func createExtrasJob(t *testing.T, f *v2Jobs, path string, body api.NewJob) factory.Item {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := verbRequest(f.h, http.MethodPost, path, string(raw))
	checkJSON(t, w, http.StatusCreated)
	var made api.JobCreated
	if err := json.Unmarshal(w.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	it, err := f.st.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

// E5/E6/E7: extras persist outside the description, read as metadata, and budget survives starts.
func TestNewJobExtrasPersistAndBudgetPrecedesStart(t *testing.T) {
	for _, start := range []string{"", "now"} {
		t.Run("start="+start, func(t *testing.T) {
			f := newCreateExtrasJobs(t)
			budget, assignee := 10.0, "sam"
			text, markdown := strings.Repeat("d", 20000), strings.Repeat("é", 60000)
			it := createExtrasJob(t, f, "/v1/jobs", api.NewJob{
				Repo: "acme/api", Title: "Fix", Text: text, Source: factory.AskApp, Start: start, Assignee: &assignee,
				Group: " batch ", Milestone: " release ", Budget: &budget,
				Context: []factory.JobContext{{Title: " reference ", Source: "guide", Markdown: markdown}},
			})
			if it.Group != "batch" || it.Milestone != "release" || it.Cap != 10 || it.Body != text || it.Ask.Text != text || len(it.Context) != 1 || it.Context[0].Title != "reference" || it.Context[0].Markdown != markdown {
				t.Fatal("created job lost extras or mixed them into the description")
			}
			events, err := f.rec.Since(context.Background(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			budgetAt, startedAt := -1, -1
			for i, e := range events {
				if e.Job == it.ID && e.Kind == record.BudgetChanged {
					budgetAt = i
					if e.Detail != "$10.00" || e.Handle != "sam" {
						t.Fatalf("budget activity: %+v", e)
					}
				}
				if e.Job == it.ID && e.Kind == record.AutoStarted {
					startedAt = i
				}
			}
			if budgetAt < 0 || start == "now" && (startedAt < 0 || budgetAt >= startedAt) {
				t.Fatalf("activity order: budget=%d start=%d", budgetAt, startedAt)
			}
			if start == "" {
				if it.State != factory.StateNew {
					t.Fatalf("backlog state %s", it.State)
				}
				f.post(t, it, "run", "", http.StatusOK)
			}
			if got := f.read(t, it); got.Budget != 10 || got.Group != "batch" || got.Milestone != "release" || got.State != api.StateQueued {
				t.Fatalf("read after start: budget=%v group=%q milestone=%q state=%q", got.Budget, got.Group, got.Milestone, got.State)
			}
			for _, path := range []string{fmt.Sprintf("/v1/jobs/%d", it.ID), "/v1/queue"} {
				w := verbRequest(f.h, http.MethodGet, path, "")
				checkJSON(t, w, http.StatusOK)
				if strings.Contains(w.Body.String(), "markdown") || !strings.Contains(w.Body.String(), `"context":[{"title":"reference","source":"guide"}]`) {
					t.Fatalf("read did not return context metadata only: %s", path)
				}
			}
		})
	}
}

// E2/E5: omitted extras stay absent on ordinary job reads.
func TestNewJobWithoutExtrasKeepsReadShape(t *testing.T) {
	f := newCreateExtrasJobs(t)
	it := createExtrasJob(t, f, "/v1/jobs", api.NewJob{Repo: "acme/api", Title: "Fix", Source: factory.AskApp})
	w := verbRequest(f.h, http.MethodGet, fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	checkJSON(t, w, http.StatusOK)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"group", "milestone", "context"} {
		if _, ok := got[field]; ok {
			t.Errorf("absent %s appeared in read", field)
		}
	}
}

// E3/E10: one issue is opened with text alone, and replay preserves its job number.
func TestNewIssueJobExtrasAndReplay(t *testing.T) {
	f := newCreateExtrasJobs(t)
	f.cfg.Mode = api.FactorySolo
	f.cfg.GitHubToken = func(context.Context) string { return "issue-fixture-credential" }
	calls := 0
	f.cfg.GitHubHTTPClient = &http.Client{Transport: jobsTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/issues" || body["title"] != "Fix" || body["body"] != "description only" || len(body) != 3 {
			t.Fatalf("GitHub request: %s %s %+v", r.Method, r.URL.Path, body)
		}
		return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"number":42,"title":"Fix","html_url":"https://github.com/acme/api/issues/42"}`))}, nil
	})}
	reopenJobs(t, f)
	body := `{"repo":"acme/api","title":"Fix","text":"description only","source":"app","group":"batch","milestone":"release","budget":10,"context":[{"title":"reference","source":"guide","markdown":"attached material"}]}`
	first := keyedRequest(f.h, "/v1/issues", body, "k1")
	second := keyedRequest(f.h, "/v1/issues", body, "k1")
	checkJSON(t, first, http.StatusCreated)
	checkJSON(t, second, http.StatusCreated)
	if first.Body.String() != second.Body.String() || calls != 1 {
		t.Fatalf("issue replay changed the receipt or opened %d issues", calls)
	}
	items, err := f.st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("jobs=%d err=%v", len(items), err)
	}
	it := items[0]
	if it.Num != 42 || it.Origin != factory.OriginForge || it.Group != "batch" || it.Cap != 10 || it.Context[0].Markdown != "attached material" {
		t.Fatalf("issue job: %+v", it)
	}
	checkJSON(t, verbRequest(f.h, http.MethodPost, "/v1/jobs", body), http.StatusCreated)
	if calls != 1 {
		t.Fatal("plain job opened an issue")
	}
}

// E10: store-backed creates replay for the same caller and key, including after reopening.
func TestNewJobCreateKeyReplay(t *testing.T) {
	f := newCreateExtrasJobs(t)
	body := `{"repo":"acme/api","title":"Fix","source":"app"}`
	first := keyedRequest(f.h, "/v1/jobs", body, "k1")
	checkJSON(t, first, http.StatusCreated)
	reopenJobs(t, f)
	second := keyedRequest(f.h, "/v1/jobs", body, "k1")
	checkJSON(t, second, http.StatusCreated)
	if first.Body.String() != second.Body.String() {
		t.Fatal("replay named a different job")
	}
	third := keyedRequest(f.h, "/v1/jobs", body, "k2")
	checkJSON(t, third, http.StatusCreated)
	items, err := f.st.List()
	if err != nil || len(items) != 2 || first.Body.String() == third.Body.String() {
		t.Fatalf("different key did not make a second job: count=%d err=%v", len(items), err)
	}
}

// E12/E5/E6/E8: invalid extras are checked before any issue or job is made, on either route.
func TestNewJobInvalidExtrasHaveNoEffects(t *testing.T) {
	f := newCreateExtrasJobs(t)
	f.cfg.Mode = api.FactorySolo
	f.cfg.GitHubToken = func(context.Context) string { return "issue-fixture-credential" }
	calls := 0
	f.cfg.GitHubHTTPClient = &http.Client{Transport: jobsTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("invalid extras reached GitHub")
	})}
	reopenJobs(t, f)
	for _, path := range []string{"/v1/jobs", "/v1/issues"} {
		for field, value := range map[string]any{
			"budget": 7, "group": strings.Repeat("g", 201), "milestone": "bad\x00label",
			"context": []factory.JobContext{{Title: "reference", Markdown: strings.Repeat("x", 100001)}},
		} {
			raw, err := json.Marshal(map[string]any{"repo": "acme/api", "title": "Fix", "issue": true, field: value})
			if err != nil {
				t.Fatal(err)
			}
			w := verbRequest(f.h, http.MethodPost, path, string(raw))
			checkJSON(t, w, http.StatusUnprocessableEntity)
			if !strings.Contains(w.Body.String(), field) {
				t.Fatalf("refusal did not name %s: %s", field, w.Body)
			}
		}
	}
	items, err := f.st.List()
	if err != nil || len(items) != 0 || calls != 0 {
		t.Fatalf("invalid extras: jobs=%d GitHub=%d err=%v", len(items), calls, err)
	}
}

// E7/E9: only the worker admission codec carries full attachments to the runner.
func TestNewJobContextWorkerAdmission(t *testing.T) {
	f := newCreateExtrasJobs(t)
	it := createExtrasJob(t, f, "/v1/jobs", api.NewJob{
		Repo: "acme/api", Title: "Fix", Source: factory.AskApp,
		Context: []factory.JobContext{{Title: "reference", Source: "guide", Markdown: "complete attachment"}},
	})
	raw, err := json.Marshal(api.WorkerJob(it))
	if err != nil {
		t.Fatal(err)
	}
	var wire api.Job
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if got := api.ToItem(wire); !reflect.DeepEqual(got.Context, it.Context) {
		t.Fatal("worker lost the context needed for its plan brief")
	}
}
