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
// now backed, and so is coming back with what the workforce makes of a CHANGE,
// since the receipt wakes the head too (wake.go). The promises it may not make
// are the ones nothing in the belt can keep: opening a file, watching something,
// speaking on another channel.
//
// The lexical enforcement that used to stand behind this — promise.go, three cue
// lists and a re-ask over every reply that tripped one — is gone. It was scanning
// for a sentence the machinery had learned to keep, and the two wakes plus
// run.summary() are what keep the honest half now: a receipt can only say what a
// tool reported, and a promise to speak again is a promise something actually
// executes. This test pins the words and the wiring together, so the voice and
// the machinery can never again disagree about what the head is able to do.
// §7. A one-sentence question got twenty-one lines with four bold headers and
// eight bullets, and every single turn ended with "Want me to …?". Neither is a
// capability limit — the same head answers a recall question in one line — so
// both are stated as principles and neither as a rule with a case list.
//
// What this pins is that the calibration is stated, and that it is stated
// WITHOUT a worked example: the offer's own model sentence used to sit in the
// prompt, which is a template to copy in the one place the failure was
// copying. The campaign removes case law rather than adding to it.
func TestTheVoiceSizesTheReplyAndDoesNotTemplateTheOffer(t *testing.T) {
	for name, want := range map[string]string{
		"the reply is calibrated to the ask": "The reply is the size of the question",
		"the offer is judged, not habitual":  "an offer made because the turn is ending is one they learn to skip",
	} {
		if !strings.Contains(orchestratorVoice, want) {
			t.Errorf("the voice no longer states %s: %q missing", name, want)
		}
	}
	// No sentence in the whole prompt is offered as one to copy. A quoted
	// example is the template a model reaches for first.
	if strings.Contains(orchestratorPrompt, "Want me to") {
		t.Error("the prompt carries a worked offer for the model to copy")
	}
}

// §3c. Money, time and completion are the three things a person cannot check,
// and the head narrated all three from its own impression of what happened.
// The judgment section says where those figures come from, once and generally.
func TestTheJudgmentSendsMoneyAndCompletionToARead(t *testing.T) {
	for name, want := range map[string]string{
		"the three unverifiable figures": "Money, time and completion come from a read taken this turn",
		"and the invented mechanism":     "never from a mechanism that would explain them",
	} {
		if !strings.Contains(orchestratorJudgment, want) {
			t.Errorf("the judgment no longer states %s: %q missing", name, want)
		}
	}
}

func TestTheHeadOnlyPromisesWhatTheWakeCanKeep(t *testing.T) {
	for name, want := range map[string]string{
		"the receipt survives":        "say what you have put in hand",
		"the backed promise is named": "you may promise to come back with either",
		"the change wake is named":    "what the workforce makes of a change you hand over",
		"the wake is the reason":      "Finished work returns to this conversation by itself",
		"the unbacked ones are not":   "never promise to open, preview, run or watch anything later",
		"the channel is named once":   "no other channel to reach them on",
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
