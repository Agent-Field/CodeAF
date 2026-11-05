package serve_test

import (
	"net/http/httptest"
	"sync"
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

func TestSettleSeesExternalStopStartAndBudget(t *testing.T) {
	for _, change := range []string{"stop", "start", "budget", "create"} {
		t.Run(change, func(t *testing.T) {
			f := newV2Jobs(t)
			in := autoProof()
			in.Autonomy = factory.AutonomyManual
			if change == "start" {
				in.Autonomy = factory.AutonomyAuto
				factory.MarkPersonStopped(&in)
			}
			if change == "budget" {
				in.Autonomy, in.Cap = factory.AutonomyAuto, 1
			}
			it := f.create(t, in)
			f.read(t, it) // Warm List while publication is blocked.
			other, err := store.Open(f.st.Root())
			if err != nil {
				t.Fatal(err)
			}
			if change == "create" {
				in = autoProof()
				in.Repo = "acme/api"
				it, err = other.Create(in)
			} else {
				err = other.Update(it.ID, func(cur *factory.Item) error {
					cur.Autonomy = factory.AutonomyAuto
					switch change {
					case "stop":
						factory.MarkPersonStopped(cur)
					case "start":
						factory.ClearPersonStop(cur)
					case "budget":
						cur.Cap = 10
					}
					return nil
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			f.read(t, it) // The next request must settle the new facts.
			got, err := f.st.Get(it.ID)
			if err != nil || got.Publishing != (change != "stop") {
				t.Fatalf("next settlement after %s: publishing=%v, err=%v", change, got.Publishing, err)
			}
		})
	}
}

func TestSettleRechecksStopAndBudgetAfterListing(t *testing.T) {
	for _, change := range []string{"stop", "budget"} {
		t.Run(change, func(t *testing.T) {
			f := newV2Jobs(t)
			it := f.create(t, autoProof())
			if _, err := f.st.List(); err != nil {
				t.Fatal(err)
			}
			other, err := store.Open(f.st.Root())
			if err != nil {
				t.Fatal(err)
			}
			// Retention is the public seam after a candidate is copied. Change
			// the disk before settlement can act on that candidate.
			var once sync.Once
			f.st.SetOutputPolicy(func() (int, int) {
				once.Do(func() {
					if err := other.Update(it.ID, func(cur *factory.Item) error {
						if change == "stop" {
							factory.MarkPersonStopped(cur)
						} else {
							cur.Cap = 1
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				})
				return 0, 0
			})
			f.read(t, it)
			got, err := f.st.Get(it.ID)
			if err != nil || got.Publishing {
				t.Fatalf("settlement used stale %s authority: publishing=%v, err=%v", change, got.Publishing, err)
			}
		})
	}
}
