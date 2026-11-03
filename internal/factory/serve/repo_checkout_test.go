package serve_test

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// queueRepos reads the queue's repositories as the wire carries them.
func queueRepos(t *testing.T, f *v2Jobs) []map[string]any {
	t.Helper()
	w := verbRequest(f.h, "GET", "/v1/queue", "")
	if w.Code != 200 {
		t.Fatalf("queue = %d %s", w.Code, w.Body)
	}
	var q struct {
		Repos []map[string]any `json:"repos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	return q.Repos
}

// withRepos makes the floor's snapshot list repos, none of which has a job.
func withRepos(t *testing.T, f *v2Jobs, repos ...string) {
	t.Helper()
	f.h.Close()
	f.seam.Load = func() (factory.Snapshot, error) {
		items, err := f.st.List()
		s := factory.Snapshot{Items: items}
		for _, r := range repos {
			s.Repos = append(s.Repos, factory.Repo{Name: r, Watched: true})
		}
		return s, err
	}
	f.h = serve.New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
}

// EACH REPOSITORY ON THE QUEUE SAYS HOW A RUN OF IT GETS ITS CHECKOUT, in the
// words a job's own read says and from the same door a start asks, so the
// Start gate can check a repository that has no jobs yet: here, will_clone
// (the runner clones it on the first step) or missing.
func TestQueueRepositoriesSayHowARunGetsItsCheckout(t *testing.T) {
	f := checkoutFloor(t, map[string]factory.CheckoutState{
		"acme/api": factory.CheckoutHere,
		"acme/web": factory.CheckoutWillClone,
	})
	withRepos(t, f, "acme/api", "acme/web", "notes")
	want := map[string]string{"acme/api": "here", "acme/web": "will_clone", "notes": "missing"}
	repos := queueRepos(t, f)
	if len(repos) != len(want) {
		t.Fatalf("queue repositories = %v", repos)
	}
	for _, r := range repos {
		if name := r["name"].(string); r["checkout_state"] != want[name] {
			t.Fatalf("%s checkout_state = %v, want %s", name, r["checkout_state"], want[name])
		}
	}
}

// A FACTORY THAT DOES NOT KNOW ITS MACHINE'S CHECKOUTS SAYS NOTHING: the word
// is absent, never a guess.
func TestQueueRepositoriesSayNoCheckoutWordWhenTheFactoryDoesNotKnow(t *testing.T) {
	f := newV2Jobs(t)
	withRepos(t, f, "acme/api")
	repos := queueRepos(t, f)
	if len(repos) != 1 {
		t.Fatalf("queue repositories = %v", repos)
	}
	if v, ok := repos[0]["checkout_state"]; ok {
		t.Fatalf("checkout_state = %v on a factory that does not know", v)
	}
}
