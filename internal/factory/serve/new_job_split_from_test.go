package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A stored fixture gives S2 its exact job number without minting 79 unrelated jobs.
func splitFromParent(t *testing.T, f *v2Jobs) factory.Item {
	t.Helper()
	it := factory.Item{
		ID: 80, Repo: "acme/api", Kind: factory.KindIssue, Title: "Large fix", State: factory.StateNew, Assignee: "sam",
		Failure: &factory.Failure{Next: factory.FailureStopped, Detail: "Split into smaller jobs"},
	}
	raw, err := json.Marshal(map[string]any{"schema": store.Schema, "revision": 1, "budget_revision": 1, "item": it})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.st.ItemPath(it.ID), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return it
}

// S2: lineage persists before a start, appears on reads and adds one activity line.
func TestNewJobSplitFromPersistsAndLeavesParent(t *testing.T) {
	for _, start := range []string{"", "now"} {
		t.Run("start="+start, func(t *testing.T) {
			f := newCreateExtrasJobs(t)
			parent := splitFromParent(t, f)
			before := f.read(t, parent)
			budget, assignee := 10.0, "sam"
			it := createExtrasJob(t, f, "/v1/jobs", api.NewJob{
				Repo: "acme/api", Title: "Smaller fix", Text: "Only the first part", Source: factory.AskApp,
				SplitFrom: &parent.ID, Start: start, Assignee: &assignee, Budget: &budget, Group: "batch", Milestone: "release",
			})
			reopenJobs(t, f)
			got := f.read(t, it)
			if it.SplitFrom != 80 || got.SplitFrom != 80 || got.FromJob != 0 || got.Budget != 10 || got.Group != "batch" || got.Milestone != "release" {
				t.Fatalf("split job read: %+v", got)
			}
			if got.Ask == nil || got.Ask.Text != "Only the first part" {
				t.Fatal("split lineage changed the ask")
			}
			if got.Desk == nil {
				t.Fatal("job read lost its activity")
			}
			lines := 0
			for _, event := range got.Desk.Activity {
				if event.What == "split from #80" {
					lines++
				}
			}
			if lines != 1 {
				t.Fatalf("split activity lines=%d, want 1", lines)
			}
			if after := f.read(t, parent); !reflect.DeepEqual(before, after) {
				t.Fatal("split create changed the parent job read")
			}
			w := verbRequest(f.h, http.MethodGet, "/v1/queue", "")
			checkJSON(t, w, http.StatusOK)
			var queue api.Queue
			if err := json.Unmarshal(w.Body.Bytes(), &queue); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, job := range queue.Jobs {
				if job.ID == it.ID {
					found = job.SplitFrom == 80
				}
			}
			if !found {
				t.Fatal("queue lost split lineage")
			}
		})
	}
}

// S2: an issue-backed job retains split lineage without changing the issue body.
func TestNewIssueSplitFromPersists(t *testing.T) {
	f := newCreateExtrasJobs(t)
	parent := splitFromParent(t, f)
	f.cfg.Mode = api.FactorySolo
	f.cfg.GitHubToken = func(context.Context) string { return "split-issue-fixture-credential" }
	calls := 0
	f.cfg.GitHubHTTPClient = &http.Client{Transport: jobsTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/issues" || body["body"] != "Only the first part" || len(body) != 3 {
			t.Fatalf("issue request: %s %s %+v", r.Method, r.URL.Path, body)
		}
		return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"number":42,"title":"Smaller fix","html_url":"https://github.com/acme/api/issues/42"}`))}, nil
	})}
	reopenJobs(t, f)
	before := f.read(t, parent)
	it := createExtrasJob(t, f, "/v1/issues", api.NewJob{
		Repo: "acme/api", Title: "Smaller fix", Text: "Only the first part", Source: factory.AskApp, SplitFrom: &parent.ID,
	})
	if got := f.read(t, it); got.SplitFrom != 80 || got.Num != 42 || calls != 1 {
		t.Fatalf("issue job: %+v; GitHub calls=%d", got, calls)
	}
	if after := f.read(t, parent); !reflect.DeepEqual(before, after) {
		t.Fatal("issue-backed split changed the parent job read")
	}
}

// S3: missing or invalid parent numbers are refused before a job or issue exists.
func TestNewJobSplitFromRefusesBeforeSideEffects(t *testing.T) {
	f := newCreateExtrasJobs(t)
	f.cfg.Mode = api.FactorySolo
	f.cfg.GitHubToken = func(context.Context) string { return "split-issue-fixture-credential" }
	calls := 0
	f.cfg.GitHubHTTPClient = &http.Client{Transport: jobsTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("invalid split_from reached GitHub")
	})}
	reopenJobs(t, f)
	for _, path := range []string{"/v1/jobs", "/v1/issues"} {
		for _, parent := range []int{80, 0, -1} {
			body := fmt.Sprintf(`{"repo":"acme/api","title":"Fix","issue":true,"split_from":%d}`, parent)
			w := verbRequest(f.h, http.MethodPost, path, body)
			checkJSON(t, w, http.StatusUnprocessableEntity)
			if !strings.Contains(w.Body.String(), "split_from") {
				t.Fatalf("refusal did not name split_from: %s", w.Body)
			}
		}
	}
	items, err := f.st.List()
	if err != nil || len(items) != 0 || calls != 0 {
		t.Fatalf("invalid split_from: jobs=%d GitHub=%d err=%v", len(items), calls, err)
	}
}

// S4: omitting lineage keeps the read and stored activity free of split facts.
func TestNewJobWithoutSplitFromOmitsLineage(t *testing.T) {
	f := newCreateExtrasJobs(t)
	it := createExtrasJob(t, f, "/v1/jobs", api.NewJob{Repo: "acme/api", Title: "Fix", Source: factory.AskApp})
	w := verbRequest(f.h, http.MethodGet, fmt.Sprintf("/v1/jobs/%d", it.ID), "")
	checkJSON(t, w, http.StatusOK)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, exists := got["split_from"]; exists {
		t.Fatal("plain job read includes split_from")
	}
	for _, event := range it.Activity {
		if strings.Contains(event.What, "split from") {
			t.Fatal("plain job has split activity")
		}
	}
}
