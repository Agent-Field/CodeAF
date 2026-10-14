package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// A store an older build left can hold a change to a standing rule — or an
// answer to the offer to keep watching — that was queued and never applied.
// The machinery it was for is gone, so it is refused in a sentence a person can
// read, spoken in the thread like every refusal, and never with the generic
// line that would quote the journal's spelling of the verb back at them.
func TestARetiredSchedulerVerbIsRefusedInWords(t *testing.T) {
	reconciler := New(openStore(t), nil, nil)
	for _, kind := range []store.CommandKind{"charter_ratify", "charter_fire", "standing_watch_enable"} {
		outcome, err := reconciler.applyCommand(context.Background(), store.Command{
			Seq: 7, SessionID: "older-build", Kind: kind, Target: "charter-7", Instruction: "yes",
		})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if outcome.status != store.CommandRejected || outcome.receipt != retiredSchedulerReceipt {
			t.Fatalf("%s settled as %+v", kind, outcome)
		}
		if strings.Contains(outcome.receipt, string(kind)) || strings.Contains(outcome.receipt, "charter") {
			t.Fatalf("%s's refusal speaks the machinery's words: %q", kind, outcome.receipt)
		}
		if voice := receiptVoice(store.Command{Kind: kind}, outcome.status); voice != store.RoleAgent {
			t.Fatalf("%s's refusal would be filed as %s rather than said", kind, voice)
		}
	}
}

// The other half of the same rule. A redirection is answered by the head in its
// own voice at the moment it is journaled, so the reconciler's receipt stays
// filed on the job's card — one user action, one visible response, and the
// receipt is not it.
func TestRedirectReceiptStaysOnTheCardAndNeverDoublesTheHead(t *testing.T) {
	graph := openStore(t)
	spliceRedirectJob(t, graph)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "redirect", Kind: store.CommandRedirect, Target: "api",
		Instruction: "no, use the v2 API not v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, message := range commandMessages(t, graph, "redirect", command.Seq) {
		if message.Role != store.RoleSystem || message.NodeID != "api" {
			t.Fatalf("redirect receipt role=%s node=%q, want a system post on the job",
				message.Role, message.NodeID)
		}
	}
}

// Every command kind the store knows is either answered elsewhere or answered by
// its own receipt. A kind that is neither is a user action that ends in silence,
// which is the whole failure, so the audit is asserted rather than remembered.
func TestEveryCommandKindHasSomebodyToAnswerForIt(t *testing.T) {
	for _, kind := range []store.CommandKind{
		store.CommandSplice, store.CommandAmend, store.CommandCancel, store.CommandRedirect,
		store.CommandExpedite, store.CommandPause, store.CommandResume, store.CommandReprioritize,
		store.CommandRestart, store.CommandHandover,
		store.CommandServiceStop, store.CommandServiceRestart, store.CommandServiceAutoRestart,
	} {
		command := store.Command{Kind: kind, Target: "job"}
		voice := receiptVoice(command, store.CommandApplied)
		spoken := HeadSpeaksFor(kind)
		if spoken && voice == store.RoleAgent {
			t.Errorf("%s is answered by the head and by its receipt", kind)
		}
		if !spoken && voice != store.RoleAgent {
			t.Errorf("%s is answered by nobody", kind)
		}
	}
}

func commandMessages(t *testing.T, graph *store.Store, sessionID string, commandSeq int64) []store.Message {
	t.Helper()
	messages, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	stamped := make([]store.Message, 0, 2)
	for _, message := range messages {
		if message.CommandSeq == commandSeq {
			stamped = append(stamped, message)
		}
	}
	return stamped
}
