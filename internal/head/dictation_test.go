package head

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The literal transcript. A person dictating cancels a job, changes their mind
// inside the same breath, and ends up asking for speed — and the deterministic
// arm that reads the first word is the last reader that should be allowed to
// decide it. Nothing may be cancelled, and the sentence must reach the belt,
// which reads it whole.
const dictatedRepair = "um cancel the— no wait, keep it, just make it faster"

func TestDictatedSelfRepairNeverReachesACancel(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "ledger-audit", "Ledger audit", "audit the ledger")
	startNode(t, graph, "ledger-audit")

	if intent, managing := nodeSurgery(dictatedRepair); managing {
		t.Fatalf("a self-repaired sentence was claimed deterministically as %s", intent.Kind)
	}

	client := &fakeClient{responses: []string{`{"reply":"noted","command":null}`}}
	user := postUser(t, graph, "dictated", dictatedRepair)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	for _, command := range pendingCommandsOf(t, graph) {
		if command.Kind == store.CommandCancel {
			t.Fatalf("the dictated repair still cancelled work: %+v", command)
		}
	}
	if client.callCount() == 0 {
		t.Fatal("the dictated repair never reached model judgment")
	}
}

// The other half, and the reason the filter is worth having: a clean command
// with a spoken noise in front of it now lands where it always should have.
func TestLeadingFillerNoLongerHidesACleanCommand(t *testing.T) {
	for _, message := range []string{
		"um, cancel the ledger audit",
		"okay so cancel the ledger audit",
		"cancel the ledger audit",
	} {
		intent, managing := nodeSurgery(message)
		if !managing || intent.Kind != store.CommandCancel {
			t.Fatalf("%q read as %s (managing=%t), want a cancel", message, intent.Kind, managing)
		}
		if intent.Reference != "ledger audit" {
			t.Fatalf("%q resolved to reference %q, want the work it names", message, intent.Reference)
		}
	}
}

// A correction opening a sentence is the cue, not a repair of one. The
// withdrawal must not swallow the arms it was written to protect.
func TestOpeningRepairWordsStayCues(t *testing.T) {
	if selfRepairsAfterCue("actually change the audit task") {
		t.Fatal("a sentence that opens with its own cue reads as self-repair")
	}
	intent, managing := nodeSurgery("actually change the audit task")
	if !managing || intent.Kind != store.CommandAmend {
		t.Fatalf("an ordinary amendment was withdrawn: %s managing=%t", intent.Kind, managing)
	}
}
