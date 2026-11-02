package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// FOLLOWING IS THE CALLER'S OWN AND MOVES NOBODY ELSE'S PAGE. Alice follows
// and reads following; Bob reads not following, and the version he read
// before still guards his write. Alice's off is hers alone.
func TestAppFactoryJobFollowIsTheCallersOwnAndKeepsOthersFresh(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	if !strings.Contains(alice.w.lines[0], appwire.FeatureFactoryJobFollow) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobFollow, alice.w.lines[0])
	}
	it := f.create(t, factory.Item{Title: "ordinary issue", State: factory.StateNew})
	bobRead := bob.job(t, it.ID)

	follow := func(p *person, on bool) appwire.FactoryJobActed {
		var out appwire.FactoryJobActed
		p.w.must("factory.job_follow", map[string]any{"factory": p.conn.ID, "job": it.ID, "on": on}, &out)
		return out
	}
	if out := follow(alice, true); out.Action != appwire.JobActFollow || out.Current == nil || !out.Current.Following {
		t.Fatalf("follow answered %+v", out)
	}
	if got := bob.job(t, it.ID); got.Following || got.Version != bobRead.Version {
		t.Fatalf("bob reads following %v version %d; want false and the %d he read", got.Following, got.Version, bobRead.Version)
	}
	var dismissed appwire.FactoryJobActed
	bob.w.must("factory.job_dismiss", map[string]any{"factory": bob.conn.ID, "job": it.ID, "expect": map[string]any{"version": bobRead.Version}}, &dismissed)
	if out := follow(bob, true); out.Current == nil || !out.Current.Following {
		t.Fatalf("bob's follow answered %+v", out.Current)
	}
	if out := follow(alice, false); out.Current == nil || out.Current.Following {
		t.Fatalf("alice's off answered %+v", out.Current)
	}
	if !bob.job(t, it.ID).Following || alice.job(t, it.ID).Following {
		t.Fatal("one person's off reached the other")
	}

	for _, bad := range []map[string]any{
		{"factory": alice.conn.ID, "job": it.ID},
		{"factory": alice.conn.ID, "job": 0, "on": true},
		{"factory": alice.conn.ID, "job": it.ID, "on": "yes"},
		{"factory": alice.conn.ID, "job": it.ID, "on": true, "expect": map[string]any{"version": 1}},
	} {
		alice.refused(t, "factory.job_follow", bad, "bad_params")
	}
}
