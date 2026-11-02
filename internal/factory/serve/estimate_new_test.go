package serve_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/estimate"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func getNewEstimate(t *testing.T, seam factory.Seam, path string, status int) api.NewEstimate {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	serve.New(seam, config).ServeHTTP(w, r)
	checkJSON(t, w, status)
	var body api.NewEstimate
	if status == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return body
}

// Before a job exists the New job sheet asks what one of this kind costs in
// this repository: the answer is the kind pool, read or not, and no figure
// below five finished jobs.
func TestANewJobsEstimateIsItsKindsFinishedJobs(t *testing.T) {
	seam := factory.FixtureSeam(now)
	var items []factory.Item
	for i := range estimate.MinJobs {
		started := now.Add(-time.Duration(48-i) * time.Hour)
		items = append(items, factory.Item{ID: 9000 + i, Repo: "acme/web", Kind: factory.KindIssue, State: factory.StateShipped, DoneHow: "answered",
			Stream: &factory.Stream{Spent: 0.5, Started: started, Ended: started.Add(10 * time.Minute)}})
	}
	seam.Load = func() (factory.Snapshot, error) { return factory.Snapshot{Now: now, Items: items}, nil }
	got := getNewEstimate(t, seam, "/v1/repos/acme/web/estimate?kind=issue", http.StatusOK)
	if got.Repo != "acme/web" || got.Kind != "issue" || got.Estimate == nil || got.Estimate.N != 5 ||
		got.Estimate.USDHi != 0.5 || got.Estimate.Basis != estimate.BasisKind || got.Estimate.Repo != "acme/web" {
		t.Fatalf("estimate = %+v (%+v)", got, got.Estimate)
	}
	if got := getNewEstimate(t, seam, "/v1/repos/acme/web/estimate?kind=pr", http.StatusOK); got.Estimate != nil {
		t.Fatalf("a kind with no history got a figure: %+v", got.Estimate)
	}
	getNewEstimate(t, seam, "/v1/repos/acme/web/estimate", http.StatusUnprocessableEntity)
	getNewEstimate(t, seam, "/v1/repos/acme/web/estimate?kind=issue&x=1", http.StatusUnprocessableEntity)
}
