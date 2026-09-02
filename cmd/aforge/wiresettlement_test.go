package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/revision"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A LEAF MARKED FAILED BY THE WIRE AFTER ITS WORK LANDED IS JUDGED ON THE TREE.
//
// The run this replicates wrote the one-line fix it was sent for, ran the
// project's own checks on the finished tree and found them green, and was then
// refused by its provider on the call it would have delivered on. The door read
// the refusal as the run's ending: exit 1 at seventy-four seconds, the
// provider's sentence as the deliverable, and no judgement of any kind in the
// journal — a correct patch on disk, thrown away by the transport.
//
// What must happen instead is that the delivery on the tree is put to the gate
// and the request question decides. This drives the whole door to prove it: the
// worker writes its file, its next call is refused by the transport, and the run
// ends delivered with the receipt saying why.
func TestWorkOnTheTreeSettlesARunWhoseLastCallNeverCameBack(t *testing.T) {
	script := newScriptedBrain(t)
	script.writeFile = true
	script.leafDropsAfterWriting = true
	// The gate is not what this test is about. What is under test is the door
	// behind it, so the judge lets the delivery through and the request question
	// is the only thing left to decide the run.
	script.gatePasses = true
	script.requestMet = "met"
	defer script.close()

	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "graph.db")
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:      "write the release note and include the migration steps",
		workspace: workspace, database: database, keep: true,
		timeout: 60 * time.Second, asJSON: true,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err != nil {
		t.Fatalf("a run whose work landed left with %v, want the settled exit\nstderr:\n%s",
			err, stderr.String())
	}
	if script.count("leaf-dropped") == 0 {
		t.Fatalf("the wire never dropped, so this proves nothing\nstderr:\n%s", stderr.String())
	}
	// Once. The question is the one call this door buys, and a door that asks
	// it twice is a door that pays twice for one answer.
	if asked := script.count("request-met"); asked != 1 {
		t.Fatalf("the request question was put %d times, want exactly once\nstderr:\n%s",
			asked, stderr.String())
	}

	outcome := decodeErrand(t, stdout.String())
	assertErrandIsHonest(t, outcome, err)
	if !outcome.Settled {
		t.Fatalf("the run called itself unsettled: %+v", outcome)
	}
	if len(outcome.Artifacts) == 0 {
		t.Fatalf("the work on the tree never reached the outcome: %+v", outcome)
	}
	// The person gets the work, not the provider's sentence about a call.
	if strings.Contains(outcome.Deliverable, "All providers have been ignored") {
		t.Fatalf("the transport's refusal was delivered as the answer:\n%s", outcome.Deliverable)
	}
	if !strings.Contains(stdout.String(), artifactName) {
		t.Fatalf("stdout never named what the run left behind:\n%s", stdout.String())
	}

	// And the journal says why the run ended, in both registers: the receipt on
	// the gate row, and the plain words on the node's own record.
	graph, openErr := store.Open(database)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer graph.Close()
	events, eventsErr := graph.Events(0, 5000)
	if eventsErr != nil {
		t.Fatal(eventsErr)
	}
	receipted, said := false, false
	for _, event := range events {
		switch event.Kind {
		case store.EventDeliveryGate:
			var gate store.DeliveryGate
			if json.Unmarshal(event.Payload, &gate) == nil &&
				strings.Contains(gate.Receipt, revision.RequestMetWords) {
				receipted = true
			}
		case store.EventMessagePosted:
			var message struct {
				Body string `json:"body"`
			}
			if json.Unmarshal(event.Payload, &message) == nil &&
				strings.Contains(message.Body, treeStandsWords) {
				said = true
			}
		}
	}
	if !receipted {
		t.Fatal("no gate row carries the reason this run ended")
	}
	if !said {
		t.Fatalf("the run never said what happened in plain words:\n%s", stderr.String())
	}
}

// The negative twin, and the whole of what this law may not widen into.
//
// The same transport refusal over a leaf that left NOTHING behind is the failure
// it always was: there is no delivery on the tree to judge, so nothing is put to
// the gate, the request question is never bought, and the run ends failed.
func TestAWireFailureWithNothingOnTheTreeStillFails(t *testing.T) {
	script := newScriptedBrain(t)
	script.leafFails = true
	script.requestMet = "met"
	defer script.close()

	database := filepath.Join(t.TempDir(), "graph.db")
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:     "write the release note and include the migration steps",
		database: database, keep: true,
		timeout: 60 * time.Second, asJSON: true,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status == 0 {
		t.Fatalf("a wire failure over an empty tree left with %v, want a non-zero exit\nstderr:\n%s",
			err, stderr.String())
	}
	if script.count("leaf-failed") == 0 {
		t.Fatalf("no leaf was ever attempted\nstderr:\n%s", stderr.String())
	}
	// The question is never even reached: a run with nothing to hand over has
	// nothing to ask about, and buying the answer anyway would be spending on
	// every failed leaf in the product.
	if asked := script.count("request-met"); asked != 0 {
		t.Fatalf("the request question was put %d times over an empty tree", asked)
	}
	outcome := decodeErrand(t, stdout.String())
	if strings.TrimSpace(outcome.Deliverable) == "" {
		t.Fatalf("the run ended without naming what went wrong:\n%s", stdout.String())
	}
}
