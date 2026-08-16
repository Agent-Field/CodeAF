package resident

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The failure: the head says "on it" and journals the splice in the same
// breath, the request cannot be turned into work, the command is rejected — and
// the refusal was filed as a system post the thread renders as a collapsed grey
// line, because a splice is a kind the head speaks for. The user is left with
// "on it", which is not true. A rejection speaks, whoever else spoke first.
func TestRejectedCommandForAHeadSpokenKindStillSpeaks(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "refusal", Kind: store.CommandSplice,
		Instruction: "do the thing with the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, func(context.Context, string, string) (Compiled, error) {
		return Compiled{}, errors.New("the request never resolved into work")
	}, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	resolved, found, err := graph.CommandBySeq(command.Seq)
	if err != nil || !found {
		t.Fatalf("command found=%t err=%v", found, err)
	}
	if resolved.Status != store.CommandRejected {
		t.Fatalf("an uncompilable splice resolved %s", resolved.Status)
	}

	spoken := commandMessages(t, graph, "refusal", command.Seq)
	if len(spoken) != 1 {
		t.Fatalf("responses to one refusal = %d, want 1: %+v", len(spoken), spoken)
	}
	receipt := spoken[0]
	if receipt.Role != store.RoleAgent || receipt.NodeID != "" {
		t.Fatalf("refusal role=%s node=%q, want an unanchored agent line", receipt.Role, receipt.NodeID)
	}
	if !strings.Contains(receipt.Body, "couldn't apply that request") {
		t.Fatalf("refusal body = %q", receipt.Body)
	}
}

// The rule stated directly, over every kind: a rejection is heard and lands in
// the thread, and an applied receipt still follows the original audit — filed
// where the head or a card already answers for it.
func TestEveryRejectionSpeaksAndAppliedReceiptsKeepTheirPlace(t *testing.T) {
	for _, kind := range []store.CommandKind{
		store.CommandCancel, store.CommandRedirect, store.CommandSplice,
		store.CommandCharterRatify, store.CommandStandingWatchEnable,
	} {
		command := store.Command{Kind: kind, Target: "job"}
		if voice := receiptVoice(command, store.CommandRejected); voice != store.RoleAgent {
			t.Errorf("a rejected %s was filed as %s", kind, voice)
		}
		if receiptAnchor(command, store.CommandRejected) != "" {
			t.Errorf("a rejected %s was anchored away from the thread", kind)
		}
		applied := receiptVoice(command, store.CommandApplied)
		if HeadSpeaksFor(kind) && applied == store.RoleAgent {
			t.Errorf("an applied %s now doubles the head", kind)
		}
	}
}
