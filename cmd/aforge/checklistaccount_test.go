package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// F. THE FAILED NODE'S OWN RECORD CARRIES THE ACCOUNT AFTER EVERYTHING THE
// failure already had to hand over, while an absent account changes no byte.
func TestAFailedNodesRecordCarriesTheAccountBelowItsFiles(t *testing.T) {
	node := store.Node{ID: "task-1", Brief: "repair the renderer"}
	err := errors.New("node task-1: context deadline exceeded")
	files := []string{"/work/internal/render.go"}
	withheld := "It stopped before it could deliver the changes."
	account := revision.ChecklistHeading +
		"\n1. render.go keeps its spacing — not reached: the run changed render.go"

	plain := humanFailure(node, err, files, withheld, "").Error()
	if unchanged := humanFailure(node, err, files, withheld, "   ").Error(); unchanged != plain {
		t.Fatalf("an empty account changed the failure:\nwant: %q\n got: %q", plain, unchanged)
	}
	body := humanFailure(node, err, files, withheld, account).Error()
	fileAt := strings.Index(body, files[0])
	withheldAt := strings.Index(body, withheld)
	accountAt := strings.Index(body, revision.ChecklistHeading)
	if fileAt < 0 || withheldAt <= fileAt || accountAt <= withheldAt {
		t.Fatalf("the account did not follow the files and withheld note:\n%s", body)
	}
	if !strings.Contains(body, "\n\n"+account) {
		t.Fatalf("the account was not separated from the failure by a blank line:\n%s", body)
	}
}

// G, I. THE MACHINE GETS EVERY CHECKLIST ROW AND THE PERSON GETS ONE ACCOUNT,
// even when the failed node's own error already carried that account through.
func TestHeadlessCarriesTheChecklistWholeInJsonAndOnceInWords(t *testing.T) {
	graph, watcher, _ := narrationFixture(t)
	written := filepath.Join(t.TempDir(), "shaped.go")
	if err := os.WriteFile(written, []byte("package shaped\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	points := []store.AcceptancePoint{
		{Behaviour: "shaped.go keeps the settled shape", Quote: "repair internal/shaped/shaped.go"},
		{Behaviour: "prose.go keeps the final paragraph", Quote: "repair prose.go:115"},
		{Behaviour: "README.md names the new ending", Quote: "update README.md"},
		{Behaviour: "render.go keeps narrow output", Quote: "repair render.go"},
	}
	if err := graph.RecordAcceptance("task-1", store.Acceptance{Points: points}); err != nil {
		t.Fatal(err)
	}
	node, found, err := graph.Node("task-1")
	if err != nil || !found {
		t.Fatalf("node task-1: found=%t err=%v", found, err)
	}
	account := checklistAccount(graph, node.ID, []string{written})
	failure := humanFailure(node, errors.New("node task-1: context deadline exceeded"),
		[]string{written}, "", account)
	claim, claimed, err := graph.Claim(node.ID, "test")
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%t err=%v", claimed, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, failure.Error()); err != nil {
		t.Fatal(err)
	}
	nodes, err := watcher.sessionNodes()
	if err != nil {
		t.Fatal(err)
	}
	outcome := watcher.compose(nodes)
	if got := strings.Count(outcome.Deliverable, revision.ChecklistHeading); got != 1 {
		t.Fatalf("the person's account appears %d times, want once:\n%s", got, outcome.Deliverable)
	}
	if len(outcome.Checklist) != len(points) {
		t.Fatalf("the headless outcome has %d checklist rows, want %d", len(outcome.Checklist), len(points))
	}

	raw, err := json.Marshal(errandEnvelope(outcome))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var checklist []revision.PointOutcome
	if err := json.Unmarshal(fields["checklist"], &checklist); err != nil {
		t.Fatalf("decode checklist from %s: %v", raw, err)
	}
	if len(checklist) != len(points) {
		t.Fatalf("--json has %d checklist rows, want %d: %s", len(checklist), len(points), raw)
	}
	for index, row := range checklist {
		if row.Behaviour != points[index].Behaviour {
			t.Errorf("row %d behaviour was clipped or changed: %q", index+1, row.Behaviour)
		}
		if row.State != outcome.Checklist[index].State {
			t.Errorf("row %d state = %q, want %q", index+1, row.State, outcome.Checklist[index].State)
		}
	}

	none, err := json.Marshal(errandEnvelope(headlessOutcome{stop: stopDone}))
	if err != nil {
		t.Fatal(err)
	}
	var emptyFields map[string]json.RawMessage
	if err := json.Unmarshal(none, &emptyFields); err != nil {
		t.Fatal(err)
	}
	if _, present := emptyFields["checklist"]; present {
		t.Fatalf("a run with no checklist carried the key: %s", none)
	}
}
