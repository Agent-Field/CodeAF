package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The failure this is about: an agent question asked "should I keep watching
// this when you're not here?", the user typed "1", the command applied, and the
// receipt was filed as an unanchored system post — which the thread renders as a
// collapsed grey line and no card claims, because a standing-watch command has no
// job behind it. The user saw silence, typed "1" again, and the duplicate fell
// through to the head as ordinary chat.
func TestStandingWatchReceiptSpeaksInTheThreadExactlyOnce(t *testing.T) {
	graph := openStore(t)
	charter := createProposedStandingCharter(t, graph, "charter-voice", "watch-voice")
	if err := graph.SetCharterStatus(charter.ID, store.CharterActive, store.Ratification{
		Origin: store.OriginUser, SessionID: "watch-voice", Evidence: "yes",
	}); err != nil {
		t.Fatal(err)
	}
	if posted, err := graph.OfferStandingWatch("watch-voice", charter.ID); err != nil || !posted {
		t.Fatalf("offer posted=%t err=%v", posted, err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID: "watch-voice", Kind: store.CommandStandingWatchEnable,
		Instruction: "yes, always",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).WithStandingWatch(&fakeStandingWatch{}).
		Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	spoken := commandMessages(t, graph, "watch-voice", command.Seq)
	if len(spoken) != 1 {
		t.Fatalf("responses to one answer = %d, want 1: %+v", len(spoken), spoken)
	}
	receipt := spoken[0]
	if receipt.Role != store.RoleAgent || receipt.NodeID != "" {
		t.Fatalf("receipt role=%s node=%q, want an unanchored agent line", receipt.Role, receipt.NodeID)
	}
	if !strings.Contains(receipt.Body, "I'll keep watch") {
		t.Fatalf("receipt = %q", receipt.Body)
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
		store.CommandCharterRatify, store.CommandCharterPause, store.CommandCharterRetire,
		store.CommandCharterCadence, store.CommandCharterOnce, store.CommandCharterFire,
		store.CommandCharterDecline, store.CommandCharterAlways, store.CommandCharterNever,
		store.CommandCharterProbation, store.CommandStandingWatchEnable, store.CommandStandingWatchDecline,
	} {
		command := store.Command{Kind: kind, Target: "job"}
		voice := receiptVoice(command, store.CommandApplied)
		spoken := headSpeaksFor(kind)
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
