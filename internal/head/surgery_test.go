package head

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestNodeSurgeryRecognitionTable(t *testing.T) {
	tests := []struct {
		message string
		kind    store.CommandKind
		route   string
	}{
		{"cancel the audio job", store.CommandCancel, "surgery"},
		{"stop that", store.CommandCancel, "surgery"},
		{"pause the migration while I think", store.CommandPause, "surgery"},
		{"actually make the README task also cover the changelog", store.CommandAmend, "surgery"},
		{"do the tests first", store.CommandReprioritize, "surgery"},
		{"restart the failed one", store.CommandRestart, "surgery"},
		{"resume the migration", store.CommandResume, "surgery"},
		{"stop watching PRs", store.CommandCharterRetire, "charter"},
		{"write the tests before the implementation", "", "new"},
		{"create a pause button", "", "new"},
		{"explain how to cancel a subscription", "", "new"},
		{"thanks, that helps", "", "new"},
	}
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			charterIntent, charter := charterManagement(test.message)
			intent, surgery := nodeSurgery(test.message)
			route := "new"
			kind := store.CommandKind("")
			if charter && (strings.Contains(strings.ToLower(test.message), "watch") ||
				strings.Contains(strings.ToLower(test.message), "monitor")) {
				route, kind = "charter", charterIntent.Kind
			} else if surgery {
				route, kind = "surgery", intent.Kind
			}
			if route != test.route || kind != test.kind {
				t.Fatalf("route/kind = %s/%s, want %s/%s (charter=%t surgery=%t)",
					route, kind, test.route, test.kind, charter, surgery)
			}
		})
	}
}

func TestSurgeryReferentResolutionUniqueAmbiguousAndNone(t *testing.T) {
	t.Run("unique pronoun", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "migration", "Database migration", "migrate the database")
		user, err := graph.PostMessage(store.Message{SessionID: "unique", Role: store.RoleUser, Body: "pause it"})
		if err != nil {
			t.Fatal(err)
		}
		if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		commands, err := graph.PendingCommands(10)
		if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandPause || commands[0].Target != "migration" {
			t.Fatalf("unique resolution commands = %+v err=%v", commands, err)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the audio job")
		spliceSurgeryJob(t, graph, "audio-fr", "French audio", "produce the audio job")
		user, err := graph.PostMessage(store.Message{SessionID: "ambiguous", Role: store.RoleUser, Body: "cancel the audio job"})
		if err != nil {
			t.Fatal(err)
		}
		if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		reply := waitForAgentReply(t, graph, "ambiguous", user.Seq)
		if !strings.HasPrefix(reply.Body, "Which job do you mean?") || len(reply.Options) != 2 {
			t.Fatalf("ambiguous reply = %+v", reply)
		}
		for _, option := range reply.Options {
			if option.Hint == "" || !strings.Contains(option.Hint, "pending") {
				t.Fatalf("option lacks status + age hint: %+v", option)
			}
		}
		commands, err := graph.PendingCommands(10)
		if err != nil || len(commands) != 0 {
			t.Fatalf("ambiguous resolution emitted commands: %+v err=%v", commands, err)
		}
	})

	// A deterministic arm that resolved nothing declines. "try again" on the job
	// that just failed used to end here — folded a tick after it was announced,
	// invisible to a verb's status filter, and answered with "I couldn't find
	// any current work that matches" by the one reader that could not see it.
	// The readers behind it can: the belt reads settled work, the router carries
	// fold roots. So the message must reach them.
	t.Run("none falls through instead of answering", func(t *testing.T) {
		graph := openHeadStore(t)
		client := &fakeClient{responses: []string{
			`{"reply":"Nothing by that name is on the board.","command":null}`,
		}}
		user, err := graph.PostMessage(store.Message{SessionID: "none", Role: store.RoleUser, Body: "cancel the audio job"})
		if err != nil {
			t.Fatal(err)
		}
		if err := New(client, graph).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		reply := waitForAgentReply(t, graph, "none", user.Seq)
		if reply.Body != "Nothing by that name is on the board." {
			t.Fatalf("surgery answered instead of declining: %q", reply.Body)
		}
		if client.callCount() == 0 {
			t.Fatal("the message never reached a reader behind surgery")
		}
	})

	// The same decline, on the sentence the finding was written about.
	t.Run("try again on a folded failure reaches the router", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the audio job")
		failNode(t, graph, "audio-en")
		if err := graph.Fold("audio-en", "the audio job failed", nil); err != nil {
			t.Fatal(err)
		}
		client := &fakeClient{responses: []string{
			`{"reply":"Starting the audio job over.","command":null}`,
		}}
		user := postUser(t, graph, "folded", "try again")
		if err := New(client, graph).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if client.callCount() == 0 {
			t.Fatal("try again dead-ended in surgery over a folded failure")
		}
	})
}

func TestOrdinaryPauseFallsThroughCharterManagementToSurgery(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "migration", "Schema migration", "migrate the schema")
	user, err := graph.PostMessage(store.Message{
		SessionID: "pause-work", Role: store.RoleUser, Body: "pause the migration while I think",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandPause || commands[0].Target != "migration" {
		t.Fatalf("ordinary pause commands = %+v err=%v", commands, err)
	}
}

func TestSurgeryConsequenceThresholdsBothSides(t *testing.T) {
	tests := []struct {
		name   string
		kind   store.CommandKind
		impact store.SurgeryImpact
		want   bool
	}{
		{"spend at threshold", store.CommandCancel, store.SurgeryImpact{Nodes: 1, Cost: SurgerySpendGateUSD}, false},
		{"spend over threshold", store.CommandCancel, store.SurgeryImpact{Nodes: 1, Cost: SurgerySpendGateUSD + 0.001}, true},
		{"runtime at threshold", store.CommandCancel, store.SurgeryImpact{Nodes: 1, RunningFor: SurgeryRuntimeGate}, false},
		{"runtime over threshold", store.CommandCancel, store.SurgeryImpact{Nodes: 1, RunningFor: SurgeryRuntimeGate + time.Second}, true},
		{"three node cascade", store.CommandPause, store.SurgeryImpact{Nodes: SurgeryCascadeGateNodes, OpenNodes: SurgeryCascadeGateNodes}, false},
		{"four node cascade", store.CommandPause, store.SurgeryImpact{Nodes: SurgeryCascadeGateNodes + 1, OpenNodes: SurgeryCascadeGateNodes + 1}, true},
		{"cheap amend", store.CommandAmend, store.SurgeryImpact{Nodes: 1, Cost: 10, RunningFor: time.Hour}, false},
		{"expensive restart", store.CommandRestart, store.SurgeryImpact{Nodes: 1, Cost: SurgerySpendGateUSD + 0.01}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := surgeryNeedsConfirmation(test.kind, test.impact); got != test.want {
				t.Fatalf("gate = %t, want %t", got, test.want)
			}
		})
	}
}

func TestConsequentialCancelAsksOneStructuredConfirmBeforeCommand(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "expensive", "Audio render", "render the audio")
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "expensive", Cost: 0.85}); err != nil {
		t.Fatal(err)
	}
	user, err := graph.PostMessage(store.Message{SessionID: "gate", Role: store.RoleUser, Body: "cancel the audio job"})
	if err != nil {
		t.Fatal(err)
	}
	head := New(&fakeClient{}, graph)
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "gate", user.Seq)
	if !strings.Contains(reply.Body, `"kind":"confirm"`) || !strings.Contains(reply.Body, `"default":"2"`) ||
		!strings.Contains(reply.Body, "~$0.85 spent") || len(reply.Options) != 2 {
		t.Fatalf("confirm reply = %+v", reply)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("cancel command escaped confirmation: %+v", commands)
	}
	answer, err := graph.PostMessage(store.Message{SessionID: "gate", Role: store.RoleUser, Body: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := head.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCancel || commands[0].Target != "expensive" {
		t.Fatalf("confirmed command = %+v err=%v", commands, err)
	}
}

func spliceSurgeryJob(t *testing.T, graph *store.Store, id, title, intent string) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Title: title, Brief: intent, Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "surgery", Intent: intent}); err != nil {
		t.Fatal(err)
	}
}
