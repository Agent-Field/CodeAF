package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// theObservedFailure is the message that shipped the bug, typos and trailing
// ellipsis included. The routing model read it as a durable preference and
// wrote a notebook fact; no command, no charter, no ratification card.
const theObservedFailure = "whenever a new pr comes to agentfield org, make sure to check for security scan and vulnerability..."

func TestRecognizedStandingLanguageDraftsACharterWithoutTheRoutingModel(t *testing.T) {
	graph := openHeadStore(t)
	// The router is loaded with exactly the decision that caused the failure.
	// If the head consults it at all, this test fails on both counts.
	router := &fakeClient{responses: []string{
		`{"reply":"Noting it as a durable rule.","command":null,` +
			`"remember":{"scope":"user","kind":"preference","body":"Check new agentfield PRs for security scans."}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "standing", Role: store.RoleUser, Body: theObservedFailure,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(router, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	if calls := router.callCount(); calls != 0 {
		t.Fatalf("routing model was consulted %d times; recognition must not depend on it", calls)
	}
	facts, err := graph.RecentFacts(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("durable ask was noted as a fact instead of drafted: %+v", facts)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice ||
		commands[0].Instruction != theObservedFailure {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	messages, err := graph.Messages("standing", user.Seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != store.RoleAgent ||
		messages[0].Body != standingDraftReply || messages[0].CommandSeq != commands[0].Seq {
		t.Fatalf("thread after recognition = %+v", messages)
	}

	// The rest of the path is the compiler's, exactly as it would have been had
	// the routing model chosen splice: one charter draft and one ratification
	// card carrying its options.
	compilerClient := &fakeClient{responses: []string{
		`{"watch":{},"sentinel":"Did a new PR open in the agentfield org?",` +
			`"action":"Check the new PR for security scan and vulnerability findings.","rails":{}}`,
	}}
	compiler := NewCompiler(compilerClient)
	reconciler := resident.New(graph, func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
		brief, err := compiler.Compile(ctx, instruction, graphContext)
		if err != nil {
			return resident.Compiled{}, err
		}
		return resident.Compiled{
			Goal: brief.Goal, Assumptions: brief.Assumptions, Scale: brief.Scale,
			BuildsOn: brief.BuildsOn, Question: brief.Question,
			QuestionOptions: brief.QuestionOptions, Charter: brief.Charter,
		}, nil
	}, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	charters, err := graph.Charters()
	if err != nil || len(charters) != 1 {
		t.Fatalf("charters = %+v err=%v", charters, err)
	}
	if charters[0].Invariant != theObservedFailure || charters[0].Status != store.CharterProposed {
		t.Fatalf("charter = %+v", charters[0])
	}
	card := waitForAgentReply(t, graph, "standing", messages[0].Seq)
	if len(card.Options) != 3 || card.Options[0].Value != "charter:ratify:"+charters[0].ID ||
		card.Options[0].Label != "yes, stand this up" {
		t.Fatalf("ratification card = %+v", card)
	}
	if !strings.Contains(card.Body, theObservedFailure) || !strings.Contains(card.Body, "What should I do?") {
		t.Fatalf("ratification question body = %q", card.Body)
	}
}

func TestNonStandingPhrasingStaysOnTheOrdinaryPath(t *testing.T) {
	// The cue set is unchanged, so the negative case is phrasing the cues
	// already exclude: a one-shot trigger definition, and plain work. ("whenever
	// I say X I mean Y" does still fire the cues; that costs one ratification
	// card, which is the deliberate side of the trade.)
	for _, message := range []string{
		"when i say ship it i mean run the deploy script once",
		"check the last pr for a security scan",
	} {
		t.Run(message, func(t *testing.T) {
			if RecognizesStandingIntent(message) {
				t.Fatalf("cues fired on non-standing phrasing")
			}
			graph := openHeadStore(t)
			router := &fakeClient{responses: []string{
				`{"reply":"On it.","command":{"kind":"splice","target":"","instruction":"` + message + `"}}`,
			}}
			user, err := graph.PostMessage(store.Message{
				SessionID: "ordinary", Role: store.RoleUser, Body: message,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := New(router, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if calls := router.callCount(); calls != 1 {
				t.Fatalf("routing model calls = %d, want 1", calls)
			}
			reply := waitForAgentReply(t, graph, "ordinary", user.Seq)
			if reply.Body != "On it." {
				t.Fatalf("reply = %q, want the routing model's own answer", reply.Body)
			}
			charters, err := graph.Charters()
			if err != nil || len(charters) != 0 {
				t.Fatalf("charters = %+v err=%v", charters, err)
			}
		})
	}
}
