package head

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
	// Deixis with no vocabulary of its own. Nothing in "pause it" can be ranked
	// against a brief, so what makes the referent resolvable is the verb reading
	// above the message and the one live row underneath it — both in front of the
	// loop before it decides anything.
	t.Run("unique pronoun", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "migration", "Database migration", "migrate the database")
		user := postUser(t, graph, "unique", "pause it")

		reading := deterministicReading(t, New(nil, graph), user)
		if !strings.Contains(reading, `carries the verb "pause" aimed at existing work`) {
			t.Fatalf("the reading did not report the verb to the loop:\n%s", reading)
		}
		head, client := beltHead(graph, beltTurn{calls: []ai.ToolCall{
			beltCall("c1", beltToolStop, map[string]any{
				"words": "pause it", "targets": []string{"migration"}})}}, beltTurn{})
		if err := head.answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if opening := client.openingPrompt(); !strings.Contains(opening, "migration") {
			t.Fatalf("the only thing the pronoun could mean was not in front of the loop:\n%s", opening)
		}
		commands, err := graph.PendingCommands(10)
		if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandPause || commands[0].Target != "migration" {
			t.Fatalf("unique resolution commands = %+v err=%v", commands, err)
		}
	})

	// Two jobs the words reach equally. The verb that knows what it wants to do
	// and not what to do it to hands back BOTH candidates, in the words a person
	// would recognise them by, and journals nothing; the question is posted by
	// the ask tool, with the same durable options a surface renders as rows.
	t.Run("ambiguous", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the audio job")
		spliceSurgeryJob(t, graph, "audio-fr", "French audio", "produce the audio job")
		user := postUser(t, graph, "ambiguous", "cancel the audio job")

		run := &beltRun{head: New(nil, graph), user: user}
		candidates, failed := run.execute(beltToolStop, beltArguments(t, map[string]any{
			"words": user.Body}))
		if failed {
			t.Fatalf("a described cancel refused: %s", candidates)
		}
		if !strings.Contains(candidates, "more than one thing matches those words") ||
			!strings.Contains(candidates, "English audio") || !strings.Contains(candidates, "French audio") {
			t.Fatalf("the candidate list is not both jobs by name:\n%s", candidates)
		}
		// The status and age each candidate carries: "claimed" tells a reader
		// nothing, and this is the first thing they read when asked which.
		if !strings.Contains(candidates, "waiting") {
			t.Fatalf("candidates lack the status a person can read:\n%s", candidates)
		}
		if run.acted {
			t.Fatal("describing work acted on it")
		}

		asked, failed := run.execute(beltToolAsk, beltArguments(t, map[string]any{
			"question": "Which job do you mean?",
			"options":  []string{"English audio", "French audio"}}))
		if failed {
			t.Fatalf("asking failed: %s", asked)
		}
		reply := waitForAgentReply(t, graph, "ambiguous", user.Seq)
		if !strings.HasPrefix(reply.Body, "Which job do you mean?") || len(reply.Options) != 2 {
			t.Fatalf("ambiguous reply = %+v", reply)
		}
		// The answer comes back to the loop rather than being applied by the
		// question machinery: only the party that asked knows what it settles.
		if !isAskQuestion(reply.Options) {
			t.Fatalf("the question was minted by something other than the ask tool: %+v", reply.Options)
		}
		commands, err := graph.PendingCommands(10)
		if err != nil || len(commands) != 0 {
			t.Fatalf("ambiguous resolution emitted commands: %+v err=%v", commands, err)
		}
	})

	// A sentence a recognizer resolves to nothing is not answered by the
	// recognizer. It reaches the loop like every other sentence, and the loop
	// says what is true.
	t.Run("none falls through instead of answering", func(t *testing.T) {
		graph := openHeadStore(t)
		client := &fakeClient{responses: []string{"Nothing by that name is on the board."}}
		user := postUser(t, graph, "none", "cancel the audio job")
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

	// The same, on the sentence the finding was written about: work folded a tick
	// after it was announced is invisible to a verb's status filter, and the
	// readers behind it — the settled reads, the recall index — can see it.
	t.Run("try again on a folded failure reaches the loop", func(t *testing.T) {
		graph := openHeadStore(t)
		spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the audio job")
		failNode(t, graph, "audio-en")
		if err := graph.Fold("audio-en", "the audio job failed", nil); err != nil {
			t.Fatal(err)
		}
		client := &fakeClient{responses: []string{"Starting the audio job over."}}
		user := postUser(t, graph, "folded", "try again")
		if err := New(client, graph).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if client.callCount() == 0 {
			t.Fatal("try again dead-ended in surgery over a folded failure")
		}
		if reply := waitForAgentReply(t, graph, "folded", user.Seq); strings.TrimSpace(reply.Body) == "" {
			t.Fatal("a folded failure ended the turn in silence")
		}
	})
}

// "pause the migration while I think" is about work, and the standing-rule
// reader must not claim it: a rule edit and a job pause share their verb, and
// the difference is whether a rule exists to mean.
func TestOrdinaryPauseFallsThroughCharterManagementToSurgery(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "migration", "Schema migration", "migrate the schema")
	user := postUser(t, graph, "pause-work", "pause the migration while I think")

	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, `carries the verb "pause" aimed at existing work`) {
		t.Fatalf("the pause reading never reached the loop:\n%s", reading)
	}
	if candidates, err := New(nil, graph).charterCandidates(charterReference("pause the migration while i think", "")); err != nil || len(candidates) != 0 {
		t.Fatalf("charter management claimed a pause aimed at work: %+v err=%v", candidates, err)
	}

	head, _ := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolStop, map[string]any{
			"words": "pause it", "targets": []string{"migration"}})}}, beltTurn{})
	if err := head.answer(context.Background(), user); err != nil {
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

// The gate is the product's promise that nothing large is thrown away without
// somebody naming the loss out loud. It sits inside the tool: the consent
// question IS the turn's reply, nothing is journaled while it is open, and the
// answer settles it through the same durable option machinery it always did.
func TestConsequentialCancelAsksOneStructuredConfirmBeforeCommand(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "expensive", "Audio render", "render the audio")
	// Spelled from the gate for [TestConfirmSurgeryGatesTheKeyPathExactlyAsASentenceIsGated]'s
	// reason: a literal under a raised gate is a test that passes about nothing.
	loss := store.SurgerySpendGateUSD * 3.4
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "expensive", Cost: loss}); err != nil {
		t.Fatal(err)
	}
	user := postUser(t, graph, "gate", "cancel the audio job")
	head, client := beltHead(graph, beltTurn{
		calls: []ai.ToolCall{beltCall("c1", beltToolStop, map[string]any{
			"targets": []string{"expensive"}})},
		text: "Cancelled it."})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "gate", user.Seq)
	if !strings.Contains(reply.Body, `"kind":"confirm"`) || !strings.Contains(reply.Body, `"default":"2"`) ||
		!strings.Contains(reply.Body, fmt.Sprintf("~%s spent", moneyUSD(loss))) || len(reply.Options) != 2 {
		t.Fatalf("confirm reply = %+v", reply)
	}
	// The gate owns the words. A model that spoke over it would be a second
	// voice on a decision only the gate knows the shape of.
	if strings.Contains(reply.Body, "Cancelled it.") {
		t.Fatalf("the model spoke over the consent question: %q", reply.Body)
	}
	if _, tooled := client.counts(); tooled != 1 {
		t.Fatalf("the loop kept going after a gate stopped it: %d tooled calls", tooled)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("cancel command escaped confirmation: %+v", commands)
	}
	answer := postUser(t, graph, "gate", "1")
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
