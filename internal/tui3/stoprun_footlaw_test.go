package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// NO VERB IS OFFERED THAT THE STORE WOULD REFUSE.
//
// The foot named `x stop it` and `p pause` under the run's own task, and the
// store refuses both there for every caller, so the only keys a person was
// shown for ending a run could only answer with a refusal (measured on the real
// binary 2026-09-19). The words come from one function for the list and the
// page alike ([app.tasksPlanKeyWords]), so the law is held at that function and
// BY PROPERTY: for every kind of row a run's store can hold, every word it
// answers is pressed against a REAL store through the REAL wire, and none of
// them may be refused. A verb added to the foot later is under this law the day
// it is added, because an unknown word fails it.
func TestNoVerbTheFootOffersIsOneTheStoreRefuses(t *testing.T) {
	kinds := []struct {
		name string
		// row names the task the foot is asked about, and seed puts the store in
		// the state that makes the row that kind.
		row  func(root string) string
		seed func(t *testing.T, store *plandb.Store)
	}{
		{"the run's own task", func(root string) string { return root }, func(t *testing.T, store *plandb.Store) {
			footLawAdd(t, store, "part")
		}},
		{"a part that is waiting", func(string) string { return "part" }, func(t *testing.T, store *plandb.Store) {
			footLawAdd(t, store, "part")
		}},
		{"a part that is running", func(string) string { return "part" }, func(t *testing.T, store *plandb.Store) {
			footLawAdd(t, store, "part")
			if _, err := store.Claim("part", "worker"); err != nil {
				t.Fatal(err)
			}
		}},
		{"a part that is held", func(string) string { return "part" }, func(t *testing.T, store *plandb.Store) {
			footLawAdd(t, store, "part")
			if _, err := store.Pause("part"); err != nil {
				t.Fatal(err)
			}
		}},
		{"a part that has ended", func(string) string { return "part" }, func(t *testing.T, store *plandb.Store) {
			footLawAdd(t, store, "part")
			if _, err := store.Cancel("part", ""); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, kind := range kinds {
		t.Run(kind.name, func(t *testing.T) {
			// Every word is pressed against a store of its own, because a verb that
			// lands changes what the next one would meet.
			for _, word := range footLawRow(t, kind.seed, kind.row).words {
				// THE ID PRESSED IS THE ONE THE ENGINE ANSWERED, spelled as the
				// surface holds it, so the law travels the road a key travels.
				pressed := footLawRow(t, kind.seed, kind.row)
				counted, id := pressed.agent, pressed.id
				var err error
				switch word {
				case tasksPlanCancelWord:
					err = counted.PlanCancel(id)
				case tasksPlanPauseWord:
					err = counted.PlanPause(id)
				case tasksPlanResumeWord:
					err = counted.PlanResume(id)
				default:
					t.Fatalf("the foot offers %q and this law does not know which verb it presses: teach it, so the word is held to the store too", word)
				}
				if err != nil {
					t.Fatalf("under %s the foot offers %q and the store refuses it: %v", kind.name, word, err)
				}
			}
		})
	}
}

func footLawAdd(t *testing.T, store *plandb.Store, id string) {
	t.Helper()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: id, Title: "a part", Description: "its own words"}}); err != nil {
		t.Fatal(err)
	}
}

// footLawSeed puts one real store into the state a kind names and answers the
// id of the row the foot is asked about.
func footLawSeed(t *testing.T, path string, seed func(*testing.T, *plandb.Store), row func(string) string) string {
	t.Helper()
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seed(t, store)
	return row(store.RootID())
}

// footLawPressed is one kind of row over a store of its own: the agent that
// reaches it over the wire, the id the engine answered for it, and the words
// the foot offers under it.
type footLawPressed struct {
	agent *countedPlanAgent
	id    string
	words []string
}

// footLawRow seeds a real store, reads the row the REAL engine answers for it
// over the wire, and asks the foot about that row, so the law sees the row a
// person's foot is drawn from and not one a test spelled.
func footLawRow(t *testing.T, seed func(*testing.T, *plandb.Store), row func(string) string) footLawPressed {
	t.Helper()
	a, counted, path := hostedPlanApp(t, false)
	id := footLawSeed(t, path, seed, row)
	var found *session.PlanTaskRow
	for _, candidate := range counted.Agent.PlanTasks() {
		if strings.TrimPrefix(candidate.ID, "t-") == id {
			candidate := candidate
			found = &candidate
		}
	}
	if found == nil {
		t.Fatalf("the engine answered no row for %q", id)
	}
	return footLawPressed{agent: counted, id: found.ID, words: a.tasksPlanKeyWords(*found)}
}
