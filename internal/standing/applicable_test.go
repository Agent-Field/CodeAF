package standing

import (
	"testing"
	"time"
)

// stand makes one item at a moment the test owns, so that Updated — which is
// what orders a shelf — is decidable rather than whatever the machine's clock
// did between two calls.
func stand(t *testing.T, store *Store, at time.Time, item Item) Item {
	t.Helper()
	store.clock = held(at)
	made, err := store.Create(item)
	if err != nil {
		t.Fatalf("create %q: %v", item.Words, err)
	}
	return made
}

// words is what a table case asserts on: the whole answer, in order.
func words(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Words)
	}
	return out
}

func same(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for at := range got {
		if got[at] != want[at] {
			return false
		}
	}
	return true
}

func TestApplicableIsWhatReachesAPlaceInTheOrderItIsRead(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	moment := now.Add(time.Hour)

	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes) * time.Minute) }
	item := func(words, workspace string, altitude Altitude) Item {
		made := reminder(words, moment)
		made.Workspace = workspace
		made.Altitude = altitude
		return made
	}

	stand(t, store, at(1), item("everywhere", "/home/me", AltitudeMachine))
	stand(t, store, at(2), item("in this project, said first", "/w/a", AltitudeProject))
	stand(t, store, at(3), item("in this project, said second", "/w/a", AltitudeProject))
	stand(t, store, at(4), item("in another project", "/w/b", AltitudeProject))

	here := item("just this chat", "/w/a", AltitudeConversation)
	here.Origin.SessionID = "s1"
	stand(t, store, at(5), here)

	// A machine-wide order the person kept out of one project. It still reaches
	// every other place, which is the whole point of an exception naming one.
	elsewhere := item("everywhere but here", "/home/me", AltitudeMachine)
	elsewhere.Exceptions = []Exception{{Workspace: "/w/a", At: at(6)}}
	stand(t, store, at(6), elsewhere)

	// Neither of these governs anything, and neither may appear in any answer.
	paused := stand(t, store, at(7), item("paused", "/w/a", AltitudeProject))
	paused.Status = StatusPaused
	store.clock = held(at(8))
	if err := store.Save(paused); err != nil {
		t.Fatalf("pause: %v", err)
	}
	stopped := stand(t, store, at(9), item("stopped", "/w/a", AltitudeProject))
	stopped.Status = StatusRetired
	store.clock = held(at(10))
	if err := store.Save(stopped); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// An item from before altitudes were spelled: no altitude at all, which
	// reads as the project it was filed under and must not migrate.
	stand(t, store, at(11), item("from before altitudes", "/w/b", ""))

	for _, tried := range []struct {
		name      string
		workspace string
		session   string
		want      []string
	}{{
		name:      "the conversation an order was made in",
		workspace: "/w/a",
		session:   "s1",
		want: []string{
			"just this chat",
			"in this project, said second",
			"in this project, said first",
			"everywhere",
		},
	}, {
		name:      "another conversation of the same project",
		workspace: "/w/a",
		session:   "s2",
		want: []string{
			"in this project, said second",
			"in this project, said first",
			"everywhere",
		},
	}, {
		name:      "a project the machine-wide exception does not name",
		workspace: "/w/b",
		session:   "s3",
		want: []string{
			"from before altitudes",
			"in another project",
			"everywhere but here",
			"everywhere",
		},
	}, {
		name: "a place with no conversation and no project at all",
		want: []string{
			"everywhere but here",
			"everywhere",
		},
	}} {
		t.Run(tried.name, func(t *testing.T) {
			got, err := store.Applicable(tried.workspace, tried.session)
			if err != nil {
				t.Fatalf("applicable: %v", err)
			}
			if !same(words(got), tried.want) {
				t.Fatalf("what stands is %q, wanted %q", words(got), tried.want)
			}
		})
	}
}

func TestAShelfLeadsWithWhatMovedLastAndNotWithWhatWasMadeLast(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	moment := now.Add(time.Hour)

	older := reminder("made first", moment)
	older.Workspace = "/w/a"
	made := stand(t, store, now.Add(time.Minute), older)
	newer := reminder("made second", moment)
	newer.Workspace = "/w/a"
	stand(t, store, now.Add(2*time.Minute), newer)

	store.clock = held(now.Add(3 * time.Minute))
	if err := store.Save(made); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Applicable("/w/a", "")
	if err != nil {
		t.Fatalf("applicable: %v", err)
	}
	want := []string{"made first", "made second"}
	if !same(words(got), want) {
		t.Fatalf("the shelf reads %q, wanted %q — the one touched last leads", words(got), want)
	}
}
