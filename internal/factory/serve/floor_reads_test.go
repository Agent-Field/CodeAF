package serve_test

import (
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// runner/self crosses ServeHTTP's settle scan and the capacity scan on the
// same route the idle runner calls, without asking the UI to load the floor.
func TestIdleRequestsReadOnlyChangedJobs(t *testing.T) {
	f := workerSetup(t)
	t.Cleanup(f.handler.Close)
	for range 300 {
		if _, err := f.st.Create(factory.Item{Repo: "owner/repo", State: factory.StateNew}); err != nil {
			t.Fatal(err)
		}
	}
	runner := f.attach(t, "sam")
	look := func() {
		t.Helper()
		r := httptest.NewRequest("GET", "/v1/runner/self", nil)
		r.Header.Set("Authorization", "Bearer "+runner.Credential)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("runner/self: %d %s", w.Code, w.Body.String())
		}
	}
	look()
	before := f.st.DocumentReads()
	for range 3 {
		look()
	}
	if n := f.st.DocumentReads() - before; n != 0 {
		t.Fatalf("settle and capacity on unchanged 300-job floor: read %d documents, want 0", n)
	}
	other, err := store.Open(f.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Update(150, func(it *factory.Item) error { it.Title = "changed elsewhere"; return nil }); err != nil {
		t.Fatal(err)
	}
	before = f.st.DocumentReads()
	look()
	if n := f.st.DocumentReads() - before; n != 1 {
		t.Fatalf("settle and capacity after one change: read %d documents, want 1", n)
	}
}
