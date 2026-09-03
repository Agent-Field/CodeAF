package main

import (
	"encoding/json"
	"errors"
	"os"
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

// THE SENTINEL IS NEVER TOLD OF A FAILURE THAT DID NOT STAND.
//
// The plan sentinel is shown each landed leaf and may edit the job's UNSTARTED
// remainder on it — and a leaf handed over as "FAILED:" is exactly the licence it
// edits under (resident.RevisionEvent writes one of two words, and that is the
// one). So a wire failure the tree then overturns must never reach it as one: a
// sibling dropped or rewritten on a failure that was withdrawn thirty lines
// later is a plan changed for a reason nobody can point at afterwards, and the
// job would be holding two different answers about one leaf.
func TestTheSentinelIsNotToldOfAFailureTheTreeSettled(t *testing.T) {
	dropped := errors.New("API error (404): All providers have been ignored.")
	for _, shape := range []struct {
		name     string
		err      error
		settled  bool
		expected string
	}{
		{name: "the leaf delivered", err: nil, settled: false, expected: ""},
		{name: "the wire dropped and the tree settled it", err: dropped, settled: true, expected: ""},
		{name: "the wire dropped and the failure stands", err: dropped, settled: false,
			expected: dropped.Error()},
	} {
		t.Run(shape.name, func(t *testing.T) {
			if told := leafFailureForThePlan(shape.err, shape.settled); told != shape.expected {
				t.Fatalf("the sentinel was told %q, want %q", told, shape.expected)
			}
		})
	}
}

// AND THE SETTLEMENT IS DECIDED BEFORE THE SENTINEL IS CONVENED, WHICH IS THE
// HALF OF THE FIX NO BEHAVIOUR TEST IN THIS PACKAGE CAN SEE.
//
// The sentinel only runs on a job with pending siblings — reviseOn returns
// before the model call when the landed node IS the job root — so every scripted
// errand here, all of which compile to one leaf, exercise the ordering and can
// never observe it. What is checkable is the seam: the settlement is asked for
// ABOVE the sentinel's call and the answer is carried into it, rather than being
// taken thirty lines below where the sentinel has already been told the wrong
// thing. This is the same shape of pin TestEveryDeliveryGateIsHeldToTheJobsRecord
// puts on the same file, and for the same reason — the defect was an ORDER, not
// a value.
func TestTheTreeIsAskedBeforeThePlanSentinelIsTold(t *testing.T) {
	source, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatalf("read the wiring: %v", err)
	}
	body := string(source)
	asked := strings.Index(body, "settledDelivery, settledOnTree = settledOnTheTree(")
	if asked < 0 {
		t.Fatal("nothing asks the tree any more; this test has stopped watching anything")
	}
	told := strings.Index(body, "plans.reviseAfter(ctx,")
	if told < 0 {
		t.Fatal("nothing convenes the plan sentinel any more; this test has stopped watching anything")
	}
	if asked > told {
		t.Fatal("the plan sentinel is told how the leaf ended before the tree has been asked, " +
			"so it can edit the remainder on a failure the tree then overturns")
	}
	// And what it is told comes from the one function that decides it. An inline
	// condition here is how the two answers came to disagree in the first place.
	if !strings.Contains(body, "absolute, leafFailureForThePlan(err, settledOnTree), workerModel)") {
		t.Fatal("the sentinel is no longer told through leafFailureForThePlan, " +
			"so the decision has more than one author again")
	}
}

// A SETTLEMENT NOBODY COULD RECORD IS A SETTLEMENT NOTHING DOWNSTREAM CAN READ.
//
// Everything that reads how this run ended — the exit code, the closing line, an
// autopsy — reads the delivery gate row out of the store and reads nothing else.
// So a settlement whose row did not land may not overturn the wire's failure:
// the leaf would hand back a success no reader could find any account of, on top
// of a failure it had just thrown away. The failure is already true and already
// recorded, and only a settlement that is itself on the record may overturn one.
//
// The decision is its own function precisely so it can be held to that without
// a store built to fail on demand.
func TestASettlementThatCouldNotBeRecordedDoesNotOverturnTheFailure(t *testing.T) {
	for _, shape := range []struct {
		name     string
		receipt  string
		recorded error
		stands   bool
	}{
		{name: "met and written down", receipt: revision.RequestMetWords, stands: true},
		{name: "met, and the row never landed", receipt: revision.RequestMetWords,
			recorded: errors.New("disk full"), stands: false},
		{name: "not met", receipt: "", stands: false},
		{name: "not met and the row never landed either", receipt: "",
			recorded: errors.New("disk full"), stands: false},
		{name: "a receipt of nothing but spaces", receipt: "   ", stands: false},
	} {
		t.Run(shape.name, func(t *testing.T) {
			if stands := settlementStands(shape.receipt, shape.recorded); stands != shape.stands {
				t.Fatalf("settlementStands(%q, %v) = %v, want %v",
					shape.receipt, shape.recorded, stands, shape.stands)
			}
		})
	}

	// And the words a person is left with say which of the two happened, because
	// "the tree did not do what was asked" and "it did and the saying of it did
	// not survive" are different news to whoever has the files on disk.
	met := store.DeliveryGate{Pass: true, Receipt: revision.RequestMetWords}
	if words := settlementShortWords(met); !strings.Contains(words, "could not be written down") {
		t.Fatalf("a settlement lost to the record was explained as something else: %q", words)
	}
	short := store.DeliveryGate{Missing: "the migration steps"}
	if words := settlementShortWords(short); !strings.Contains(words, "the migration steps") {
		t.Fatalf("what the tree was short of never reached the record: %q", words)
	}
	if words := settlementShortWords(short); strings.Contains(words, "could not be written down") {
		t.Fatalf("a tree that fell short was blamed on the record: %q", words)
	}
}
