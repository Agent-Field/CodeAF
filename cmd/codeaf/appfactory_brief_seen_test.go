package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// EACH PERSON'S SEEN REVISION IS THEIR OWN, ONLY MOVES FORWARD, AND MOVES
// NOBODY ELSE'S PAGE. Alice marks revision 3 and then a late 2; she reads 3,
// Bob reads nothing, and the version Bob read before still guards his write.
func TestAppFactoryJobBriefSeenIsTheCallersOwnAndKeepsOthersFresh(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	if !strings.Contains(alice.w.lines[0], appwire.FeatureFactoryJobBriefSeen) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobBriefSeen, alice.w.lines[0])
	}
	it := f.create(t, factory.Item{Title: "ordinary issue", State: factory.StateNew})
	bobRead := bob.job(t, it.ID)

	seen := func(rev int) appwire.FactoryJobActed {
		var out appwire.FactoryJobActed
		alice.w.must("factory.job_brief_seen", map[string]any{"factory": alice.conn.ID, "job": it.ID, "rev": rev}, &out)
		return out
	}
	if out := seen(3); out.Action != appwire.JobActBriefSeen || out.Current == nil || out.Current.BriefSeen != 3 {
		t.Fatalf("seen 3 answered %+v", out)
	}
	if out := seen(2); out.Current == nil || out.Current.BriefSeen != 3 {
		t.Fatalf("a lower revision moved the seen one back: %+v", out.Current)
	}
	if got := alice.job(t, it.ID).BriefSeen; got != 3 {
		t.Fatalf("alice reads brief_seen %d, want 3", got)
	}
	if got := bob.job(t, it.ID); got.BriefSeen != 0 || got.Version != bobRead.Version {
		t.Fatalf("bob reads brief_seen %d version %d; want 0 and the %d he read", got.BriefSeen, got.Version, bobRead.Version)
	}
	var dismissed appwire.FactoryJobActed
	bob.w.must("factory.job_dismiss", map[string]any{"factory": bob.conn.ID, "job": it.ID, "expect": map[string]any{"version": bobRead.Version}}, &dismissed)

	for _, bad := range []map[string]any{
		{"factory": alice.conn.ID, "job": it.ID, "rev": 0},
		{"factory": alice.conn.ID, "job": it.ID},
		{"factory": alice.conn.ID, "job": 0, "rev": 1},
		{"factory": alice.conn.ID, "job": it.ID, "rev": 1, "expect": map[string]any{"version": 1}},
	} {
		alice.refused(t, "factory.job_brief_seen", bad, "bad_params")
	}
}
