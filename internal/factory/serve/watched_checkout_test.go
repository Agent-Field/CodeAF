package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A WATCHED REPOSITORY WITH NO JOBS YET SAYS WHERE IT IS CHECKED OUT. Setup
// records a clone, or a folder the person already has, in the store's
// checkout record before the repository's first job exists; the queue's row
// for it must then read checked_out with that folder, as a repository with
// jobs does.
func TestAWatchedRepositoryWithNoJobsReadsItsRecordedCheckout(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, _ := identityHandler(t, g)
	h.seam.Load = func() (factory.Snapshot, error) {
		return factory.Snapshot{Now: *now, Checkouts: map[string]string{}}, nil
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := st.SetRepos([]string{"acme/new", "acme/bare"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("acme/new", dir); err != nil {
		t.Fatal(err)
	}
	h.config.Store = st
	q := queueAs(t, h, "floor-secret", false)
	got := map[string]string{}
	for _, r := range q.Repos {
		if r.CheckedOut != (r.Checkout != "") {
			t.Errorf("%s: checked_out %v with checkout %q", r.Name, r.CheckedOut, r.Checkout)
		}
		got[r.Name] = r.Checkout
	}
	if len(q.Repos) != 2 || got["acme/new"] != dir || got["acme/bare"] != "" {
		t.Fatalf("watched repositories = %+v, want acme/new checked out in %s", q.Repos, dir)
	}
}
