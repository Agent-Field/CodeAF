package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// 13.10's producer half, taken at the head end — and now the other side of the
// handshake it left.
//
// The voice used to tell the head that a commission's receipt says "you will
// report back", and nothing in the machinery could keep it: `announceNode`
// posts a delivery as a SYSTEM row anchored to a node, and `answerable` — the
// predicate the poll consulted — takes a row only when it is the person's own
// and belongs to no node. So the head said "I'll let you know when it lands"
// and then never spoke again. That wave closed the honesty half by deleting the
// promise; this one closes the wiring half, which is the arm that actually gives
// the person what the report asked for.
//
// The wake is `deliveredRow` and the turn is absorb.go. `answerable` is
// deliberately UNCHANGED: a delivery is not the person's own contiguous words
// and must never be folded into a run of them, which is what widening that
// predicate would have done.
//
// So the voice may promise to come back with what work FINDS — that promise is
// now backed. The promises it may not make are the ones nothing in the belt can
// keep: opening a file, watching something, speaking on another channel. The
// enforcement of that line is promise.go; this test pins the words and the
// wiring together, so the voice and the machinery can never again disagree
// about what the head is able to do.
func TestTheHeadOnlyPromisesWhatTheWakeCanKeep(t *testing.T) {
	for name, want := range map[string]string{
		"the receipt survives":        "the reply is a receipt: say what you have put in hand",
		"the backed promise is named": "You may say you will come back to them with what it finds",
		"the wake is the reason":      "work you commission wakes you when it lands",
		"the unbacked ones are not":   "Never promise to open, preview, display or run anything for them",
		"the true thing still said":   "the finished work arrives in this conversation by itself",
	} {
		if !strings.Contains(orchestratorVoice, want) {
			t.Errorf("the voice no longer states %s: %q missing", name, want)
		}
	}

	// The wake, pinned to the row shape the resident actually posts.
	delivered := store.Message{
		Role: store.RoleSystem, NodeID: "task-16", SessionID: "room",
		Body: "the job wrote rivers.txt",
	}
	if !deliveredRow(delivered) {
		t.Fatal("a delivery no longer wakes the head, so the voice may not promise to come back")
	}
	// And the fold is still the person's own contiguous words and nothing else.
	if answerable(delivered) {
		t.Fatal("a delivery became foldable: it will swallow whatever they type next")
	}
	typed := store.Message{Role: store.RoleUser, Body: "write me three haiku"}
	if !answerable(typed) {
		t.Fatal("the person's own row stopped being answerable")
	}
	// A row the head itself posted after a delivery is not a second delivery.
	if deliveredRow(store.Message{Role: store.RoleAgent, NodeID: "task-16", SessionID: "room"}) {
		t.Fatal("an agent row reads as a delivery")
	}
	// Nor is a command receipt, which is anchored to the command and not to a job.
	if deliveredRow(store.Message{
		Role: store.RoleSystem, NodeID: "task-16", SessionID: "room", CommandSeq: 4,
	}) {
		t.Fatal("a command receipt reads as a delivery")
	}
}

// Whose work earns the sentence. The person's own commissioned job does; the
// resident's practice, its standing furniture and its territory bookkeeping do
// not, because nobody is waiting on those and a paid turn narrating them speaks
// to an empty room.
func TestOnlyWorkThePersonAskedForIsAbsorbed(t *testing.T) {
	theirs := store.Node{
		ID: "task-16", Parent: store.RootID, Status: store.Done,
		Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "room"},
	}
	if !absorbable(theirs) {
		t.Fatal("the person's own settled job was not absorbed")
	}
	for name, node := range map[string]store.Node{
		"self-directed": {ID: "self-1", Parent: store.RootID, Status: store.Done,
			Provenance: store.Provenance{Origin: store.OriginSelf, SessionID: "room"}},
		"practice": {ID: "q-1", Parent: store.RootID, Status: store.Done, Group: store.PracticeGroup,
			Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "room"}},
		"standing furniture": {ID: "charter-1", Parent: store.RootID, Status: store.Done, Group: charterNodeGroup,
			Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "room"}},
		"a step inside a job": {ID: "task-16~write", Parent: "task-16", Status: store.Done,
			Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "room"}},
		"still running": {ID: "task-17", Parent: store.RootID, Status: store.Running,
			Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "room"}},
		"nobody's room": {ID: "task-18", Parent: store.RootID, Status: store.Done,
			Provenance: store.Provenance{Origin: store.OriginUser}},
	} {
		if absorbable(node) {
			t.Fatalf("%s earned a paid turn", name)
		}
	}
}
