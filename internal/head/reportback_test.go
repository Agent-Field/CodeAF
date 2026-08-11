package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// 13.10's producer half, taken at the head end.
//
// The voice told the head that a commission's receipt says "you will report
// back". Nothing in the machinery can keep that: `announceNode` posts a
// delivery as a SYSTEM row anchored to a node, and `answerable` — the one
// predicate the poll consults — takes a row only when it is the person's own
// (`store.RoleUser`) and belongs to no node. So the head said "I'll let you
// know when it lands" and then never spoke again, in a thread that keeps the
// receipt on screen next to the silence.
//
// That is the ARTIFACT of the honesty law rather than a lapse under it: "Never
// promise a behaviour you have not recorded", and an affordance that promises
// what the machine cannot do is the affordance lying. So the promise is gone
// and the true thing is said in its place — the finished work does arrive in
// the conversation on its own, as a card, which is the same reassurance without
// the false claim about who delivers it.
//
// The alternative — waking the head on a delivery so it can write the brief in
// its own voice — is Wave 5's, filed in audit-notes/wave5-coworking-handoff.md,
// and it is deliberately NOT taken here: the row a wake would hand the head is
// today one undressed prose blob (13.10 item 1, `internal/resident`), so a head
// woken now would translate a dump it can only re-read.
func TestTheHeadNeverPromisesAReportItCannotDeliver(t *testing.T) {
	for name, want := range map[string]string{
		"the receipt survives":        "the reply is a receipt: say what you have put in hand",
		"the promise does not":        "Do not promise to report back, to follow up, or to let them know when it lands",
		"and the reason is named":     "nothing wakes you when work finishes",
		"the true thing said instead": "the finished work arrives in this conversation by itself",
	} {
		if !strings.Contains(orchestratorVoice, want) {
			t.Errorf("the voice no longer states %s: %q missing", name, want)
		}
	}
	if strings.Contains(orchestratorVoice, "and that you will report back") {
		t.Error("the voice still instructs the head to promise a report-back")
	}

	// The mechanism the promise was false against, pinned so that a later wave
	// wiring the wake has to come back to this test and say so. While
	// `answerable` reads this way, a delivery cannot reach the head.
	delivered := store.Message{Role: store.RoleSystem, NodeID: "task-16", Body: "the job wrote rivers.txt"}
	if answerable(delivered) {
		t.Fatal("a delivery row is answerable now: the wake landed, so the voice may promise a report-back again")
	}
	typed := store.Message{Role: store.RoleUser, Body: "write me three haiku"}
	if !answerable(typed) {
		t.Fatal("the person's own row stopped being answerable")
	}
}
