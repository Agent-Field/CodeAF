package session

// A PROGRAM SAVED AS A PAGE IS ON THE LIST, AND IT IS ON IT THE MOMENT IT IS
// SAVED.
//
// This is the scenario that was reported: somebody asks for a program to be
// built, approves the card, the design says it saved — and `/subharness` did not
// have it, because the list read two bundle stores and the page had gone to a
// third place. These tests hold the fix at the seam the surface actually opens
// ([Agent.SubharnessList] and [Agent.SubharnessIntake]), against a real registry
// with a real store on disk behind it.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// pageRuns records what the runner behind the store was asked to do rather than
// doing it. It locks because a run happens on the node's own goroutine.
type pageRuns struct {
	mu    sync.Mutex
	asked []string
}

func (r *pageRuns) took(request string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, request)
}

func (r *pageRuns) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// pageStore is a store on disk with nothing in it yet.
func pageStore(t *testing.T) (*subharness.Store, *pageRuns) {
	t.Helper()
	return subharness.At(t.TempDir()), &pageRuns{}
}

// savePage writes one program into the store under a name and a line of purpose,
// the way an approved design does.
func savePage(t *testing.T, store *subharness.Store, name, purpose string) {
	t.Helper()
	page := subharness.Harness{
		Id: subharness.Id{Name: name, Desc: purpose},
		Program: subharness.Program{
			Nodes: []subharness.Node{
				{Id: "start", Kind: subharness.KindTrigger, Fields: subharness.Fields{"source": subharness.TriggerIdle}},
				{Id: "work", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
					"brief": "make the pictures", "tools": "read", "max_turns": "4",
				}},
				{Id: "check", Kind: subharness.KindVerify, Fields: subharness.Fields{
					"ladder": subharness.VerifySchema, "check": "the pictures are there",
				}},
			},
			Edges: []subharness.Edge{{"start", "work"}, {"work", "check"}},
		},
		Whitelist: []string{"read"},
		Verify:    subharness.Verify{Ladder: subharness.VerifyInvariants},
		Dyn:       subharness.Dyn{Ladder: subharness.DynFixed},
	}
	if _, err := store.Save(page); err != nil {
		t.Fatalf("the page could not be saved: %v", err)
	}
}

// pageRegistry is a registry with the page store in front of it, and the runner
// behind it records the name and the words it was handed.
func pageRegistry(store *subharness.Store, runs *pageRuns) *exec.Registry {
	registry := exec.NewRegistry(&fakeGeneralist{})
	registry.UseBundles(exec.LayerPages, store.Source(
		func(_ context.Context, name, text, _ string, step func(subharness.Trail)) (string, subharness.Usage, error) {
			runs.took(name + " · " + text)
			step(subharness.Trail{Step: 1, Id: "work", Kind: subharness.KindAgentLoop})
			return "the pictures are in ./out", subharness.Usage{Calls: 3, CostUSD: 0.4}, nil
		}))
	return registry
}

// THE REPORTED SCENARIO, END TO END: a page is saved, and `/subharness` has it —
// under its own name, with the line it was designed for, marked as the person's
// own, and with a card asking the one thing a page can be asked.
func TestAProgramSavedAsAPageIsOnTheSubharnessList(t *testing.T) {
	store, runs := pageStore(t)
	savePage(t, store, "social-marketing-images", "the weekly marketing pictures for company X")
	agent := agentWithPrograms(t, pageRegistry(store, runs), nil)

	rows := agent.SubharnessList()
	if len(rows) != 1 {
		t.Fatalf("the list has %d rows, not 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Manifest.Name != "social-marketing-images" {
		t.Fatalf("the list drew %q", row.Manifest.Name)
	}
	if row.Manifest.Purpose != "the weekly marketing pictures for company X" {
		t.Fatalf("the row's one line is %q", row.Manifest.Purpose)
	}
	// THE MARK IS THE PERSON'S OWN and it is the registry's to stamp: a page is
	// theirs in exactly the way a bundle in their home store is theirs, and a row
	// a person could tell apart would be two systems on one list.
	if row.Manifest.Provenance != exec.FromYou {
		t.Fatalf("the row is marked %q", row.Manifest.Provenance)
	}
	// AND NOTHING IS INVENTED FOR IT. A page states no budget shape, so the row
	// draws no time at all rather than the generalist's.
	if row.Manifest.DeadlineFloor != 0 {
		t.Fatalf("the row claims a budget shape the page never stated: %v", row.Manifest.DeadlineFloor)
	}

	card, err := agent.SubharnessIntake("social-marketing-images")
	if err != nil {
		t.Fatalf("the card did not open: %v", err)
	}
	if len(card.Fields) != 1 || card.Fields[0].Field.Name != subharness.BriefField {
		t.Fatalf("the card's fields are not the one field a page takes: %+v", card.Fields)
	}
	if !card.Fields[0].Field.Required || strings.TrimSpace(card.Fields[0].Field.Description) == "" {
		t.Fatalf("the one field is not asked for, or says nothing: %+v", card.Fields[0].Field)
	}
	if len(card.Missing) != 1 || card.Missing[0] != subharness.BriefField {
		t.Fatalf("the required blank is not the one that is blank: %v", card.Missing)
	}
}

// AND THE LIST IS READ FROM THE DISK EVERY TIME IT IS OPENED. A page approved a
// minute ago is a page this list has to know about — which is the whole of the
// report: the person saved one and then went looking for it. Nothing is
// refreshed, nothing is restarted, and the second listing simply has it.
func TestAPageSavedAfterTheFirstListIsOnTheNextOne(t *testing.T) {
	store, runs := pageStore(t)
	agent := agentWithPrograms(t, pageRegistry(store, runs), nil)

	if rows := agent.SubharnessList(); len(rows) != 0 {
		t.Fatalf("an empty store listed %d programs", len(rows))
	}
	savePage(t, store, "social-marketing-images", "the weekly marketing pictures for company X")

	rows := agent.SubharnessList()
	if len(rows) != 1 || rows[0].Manifest.Name != "social-marketing-images" {
		t.Fatalf("the page saved since the first list is not on the second: %+v", rows)
	}
	// And a second one lands the same way, so what is being tested is the walk
	// and not a one-time read.
	savePage(t, store, "chase-a-flake", "work out why one test is flaky")
	if rows := agent.SubharnessList(); len(rows) != 2 {
		t.Fatalf("the second page did not land either: %+v", rows)
	}
}

// A PAGE AT A LATER VERSION IS ONE ROW AND IT IS THE HEAD. Saving again mints
// the next version beside the first; the list is what a person can run, and that
// is the newest page rather than one row per version they have ever approved.
func TestASecondVersionOfAPageIsStillOneRow(t *testing.T) {
	store, runs := pageStore(t)
	savePage(t, store, "social-marketing-images", "the weekly marketing pictures")
	savePage(t, store, "social-marketing-images", "the weekly marketing pictures, checked twice")
	agent := agentWithPrograms(t, pageRegistry(store, runs), nil)

	rows := agent.SubharnessList()
	if len(rows) != 1 {
		t.Fatalf("two versions drew %d rows: %+v", len(rows), rows)
	}
	if rows[0].Manifest.Purpose != "the weekly marketing pictures, checked twice" {
		t.Fatalf("the row is not the head version: %q", rows[0].Manifest.Purpose)
	}
}

// AND RUNNING ONE FROM THE LIST IS A TASK LIKE EVERY OTHER RUN. The card's one
// field becomes the request, the program is walked by the same runner a turn
// walks it with, and what comes back settles the node the way any other
// subharness run settles it — there is nothing about this row a person could
// tell apart from the row above it.
func TestRunningAPageFromTheListIsATaskLikeAnyOtherRun(t *testing.T) {
	store, runs := pageStore(t)
	savePage(t, store, "social-marketing-images", "the weekly marketing pictures")
	agent := agentWithPrograms(t, pageRegistry(store, runs), nil)

	id, title, err := agent.SubharnessRun(context.Background(), "social-marketing-images",
		json.RawMessage(`{"brief":"the october set, landscape"}`))
	if err != nil {
		t.Fatalf("the run did not start: %v", err)
	}
	if id == 0 || !strings.Contains(title, "social-marketing-images") {
		t.Fatalf("the run answered %d, %q", id, title)
	}
	node := waitForSettled(t, agent, id)
	if node.State != TaskDone {
		t.Fatalf("the run settled %s: %s", node.State, node.Report)
	}
	if node.Kind != TaskKindSubharness {
		t.Fatalf("the node is a %q", node.Kind)
	}
	if !strings.Contains(node.Report, "the pictures are in ./out") {
		t.Fatalf("the run's own account did not reach the card: %q", node.Report)
	}
	// THE ONE FIELD IS THE REQUEST, and it reaches the program as the person's
	// own words rather than as JSON they never wrote.
	took := runs.all()
	if len(took) != 1 || took[0] != "social-marketing-images · the october set, landscape" {
		t.Fatalf("the program was asked for %v", took)
	}
}
