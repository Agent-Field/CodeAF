package head

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type fakeClient struct {
	mutex     sync.Mutex
	responses []string
	calls     int
	seen      []ai.Message
}

func (client *fakeClient) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.calls++
	client.seen = append([]ai.Message(nil), messages...)
	if len(client.responses) == 0 {
		return nil, errors.New("no fake response left")
	}
	text := client.responses[0]
	client.responses = client.responses[1:]
	return textResponse(text), nil
}

func (client *fakeClient) callCount() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.calls
}

func TestHeadPostsReply(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"The graph is ready and waiting for work.","command":null}`,
	}}
	if _, err := graphStore.PostMessage(store.Message{SessionID: "chat-1", Role: store.RoleUser, Body: "what is happening?"}); err != nil {
		t.Fatalf("post user message: %v", err)
	}

	stop := startServing(t, New(client, graphStore))
	defer stop()
	reply := waitForAgentReply(t, graphStore, "chat-1", 0)
	if reply.Body != "The graph is ready and waiting for work." {
		t.Fatalf("reply body = %q", reply.Body)
	}
	if reply.SessionID != "chat-1" || reply.CommandSeq != 0 {
		t.Fatalf("reply metadata wrong: %+v", reply)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
}

func TestHeadRequestsSpliceAndLinksReply(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"Splicing that in — I'll report when it lands.","command":{"kind":"splice","target":"","instruction":"make me X"}}`,
	}}
	user, err := graphStore.PostMessage(store.Message{SessionID: "chat-2", Role: store.RoleUser, Body: "make me X"})
	if err != nil {
		t.Fatalf("post user message: %v", err)
	}

	stop := startServing(t, New(client, graphStore))
	defer stop()
	reply := waitForAgentReply(t, graphStore, "chat-2", user.Seq)
	commands, err := graphStore.PendingCommands(0)
	if err != nil {
		t.Fatalf("list pending commands: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("pending commands = %+v, want one", commands)
	}
	command := commands[0]
	if command.Kind != store.CommandSplice || command.Instruction != "make me X" || command.SessionID != "chat-2" {
		t.Fatalf("command wrong: %+v", command)
	}
	if reply.CommandSeq != command.Seq {
		t.Fatalf("reply command seq = %d, command seq = %d", reply.CommandSeq, command.Seq)
	}
	if command.Seq >= reply.Seq {
		t.Fatalf("command %d was not persisted before reply %d", command.Seq, reply.Seq)
	}
}

func TestHeadMalformedOutputFallsBackToRawReply(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{"I can still answer this plainly."}}
	user, err := graphStore.PostMessage(store.Message{SessionID: "chat-3", Role: store.RoleUser, Body: "hello"})
	if err != nil {
		t.Fatalf("post user message: %v", err)
	}

	stop := startServing(t, New(client, graphStore))
	defer stop()
	reply := waitForAgentReply(t, graphStore, "chat-3", user.Seq)
	if reply.Body != "I can still answer this plainly." || reply.CommandSeq != 0 {
		t.Fatalf("fallback reply wrong: %+v", reply)
	}
	commands, err := graphStore.PendingCommands(0)
	if err != nil {
		t.Fatalf("list pending commands: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("malformed output emitted commands: %+v", commands)
	}
}

func TestRenderNotebookUsesMessageScopeCues(t *testing.T) {
	graphStore := openHeadStore(t)
	fact, err := graphStore.RecordFact("", "file:internal/resident/notebook.go", store.FactQuirk,
		"scope-only memory with unrelated vocabulary")
	if err != nil {
		t.Fatalf("record fact: %v", err)
	}
	rendered := renderNotebook(graphStore, "Please inspect internal/resident/notebook.go")
	if !strings.Contains(rendered, fmt.Sprintf("#%d [", fact.Seq)) ||
		!strings.Contains(rendered, "scope-only memory with unrelated vocabulary") {
		t.Fatalf("scope-exact notebook fact did not reach head: %q", rendered)
	}
}

func TestHeadRetractsNumberedNotebookBelief(t *testing.T) {
	graphStore := openHeadStore(t)
	fact, err := graphStore.RecordFact(store.RootID, "tool:git", store.FactQuirk,
		"git always destroys worktrees")
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{fmt.Sprintf(
		`{"reply":"I'll forget that.","command":null,"remember":null,"retract":{"seq":%d}}`, fact.Seq,
	)}}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "chat-retract", Role: store.RoleUser, Body: "that's wrong — forget that",
	})
	if err != nil {
		t.Fatal(err)
	}

	stop := startServing(t, New(client, graphStore))
	defer stop()
	reply := waitForAgentReply(t, graphStore, "chat-retract", user.Seq)
	if want := fmt.Sprintf("Forgot #%d: %s", fact.Seq, fact.Body); reply.Body != want {
		t.Fatalf("retraction reply = %q, want %q", reply.Body, want)
	}
	quarantined, found, err := graphStore.FactBySeq(fact.Seq)
	if err != nil || !found || quarantined.Status != store.FactQuarantined ||
		quarantined.EvidenceSeq != user.Seq || quarantined.StatusOrigin != store.FactOriginUser {
		t.Fatalf("retracted fact = %+v found=%t err=%v", quarantined, found, err)
	}
	if rendered := renderNotebook(graphStore, "git worktrees"); strings.Contains(rendered, fact.Body) {
		t.Fatalf("quarantined fact reached head retrieval: %q", rendered)
	}
	if err := graphStore.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graphStore.FactBySeq(fact.Seq)
	if err != nil || !found || rebuilt.Status != store.FactQuarantined || rebuilt.EvidenceSeq != user.Seq {
		t.Fatalf("rebuilt retraction = %+v found=%t err=%v", rebuilt, found, err)
	}
}

func TestCompilerParsesAssumptions(t *testing.T) {
	client := &fakeClient{responses: []string{strings.Join([]string{
		"Here is the brief:",
		"```json",
		`{"goal":"Build a useful prototype with reproducible evidence.","deliverable":"prototype plus test report","budget":"$0.40","assumptions":["Use the current branch","Target a technical reader"]}`,
		"```",
	}, "\n")}}
	brief, err := NewCompiler(client).Compile(context.Background(), "build it", "root is running")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if brief.Deliverable != "prototype plus test report" || brief.Budget != "$0.40" {
		t.Fatalf("brief fields wrong: %+v", brief)
	}
	if len(brief.Assumptions) != 2 || brief.Assumptions[0] != "Use the current branch" {
		t.Fatalf("assumptions wrong: %+v", brief.Assumptions)
	}
}

func TestCompilerPreservesVerbatimInstruction(t *testing.T) {
	instruction := "Make me X — keep the API name.\nDo not rename `Widget`."
	client := &fakeClient{responses: []string{
		`{"goal":"Produce and verify the requested change.","deliverable":"a tested implementation","budget":"$0.40","assumptions":["Use repository conventions"]}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), instruction, "root is running")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	wantSuffix := "Verbatim request:\n" + instruction
	if !strings.HasSuffix(brief.Goal, wantSuffix) {
		t.Fatalf("goal lost verbatim instruction:\n%s", brief.Goal)
	}
	if strings.Count(brief.Goal, instruction) != 1 {
		t.Fatalf("instruction was not anchored exactly once:\n%s", brief.Goal)
	}
}

func TestCompilerUsesOnlyExplicitUnsettledFlagForTrial(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Compare both approaches cheaply, then use the observed winner.","deliverable":"tested implementation","budget":"$0.40","assumptions":[],"trial_of":0}`,
		`{"goal":"Use the requested approach.","deliverable":"tested implementation","budget":"$0.40","assumptions":[],"trial_of":999}`,
	}}
	compiler := NewCompiler(client)
	flagged := "notebook:\n- " + store.UnsettledFactFlag + "42\n  approach A vs approach B"
	brief, err := compiler.Compile(context.Background(), "build it", flagged)
	if err != nil {
		t.Fatal(err)
	}
	if brief.TrialOf != 42 {
		t.Fatalf("flagged trial_of = %d, want 42", brief.TrialOf)
	}
	plain, err := compiler.Compile(context.Background(), "build it again", "ordinary prose says two options are unsettled")
	if err != nil {
		t.Fatal(err)
	}
	if plain.TrialOf != 0 {
		t.Fatalf("unflagged trial_of = %d, want 0", plain.TrialOf)
	}
}

func TestHeadRestartSkipsAnsweredHistory(t *testing.T) {
	graphStore := openHeadStore(t)
	if _, err := graphStore.PostMessage(store.Message{SessionID: "chat-4", Role: store.RoleUser, Body: "old question"}); err != nil {
		t.Fatalf("post old user message: %v", err)
	}
	if _, err := graphStore.PostMessage(store.Message{SessionID: "chat-4", Role: store.RoleAgent, Body: "old answer"}); err != nil {
		t.Fatalf("post old agent message: %v", err)
	}
	client := &fakeClient{responses: []string{
		`{"reply":"new answer","command":null}`,
	}}

	stop := startServing(t, New(client, graphStore))
	defer stop()
	user, err := graphStore.PostMessage(store.Message{SessionID: "chat-4", Role: store.RoleUser, Body: "new question"})
	if err != nil {
		t.Fatalf("post new user message: %v", err)
	}
	reply := waitForAgentReply(t, graphStore, "chat-4", user.Seq)
	if reply.Body != "new answer" {
		t.Fatalf("new reply = %q", reply.Body)
	}
	messages, err := graphStore.Messages("chat-4", 0, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("answered history was replayed: %+v", messages)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("provider calls = %d, want only the new message", calls)
	}
}

func openHeadStore(t *testing.T) *store.Store {
	t.Helper()
	graphStore, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = graphStore.Close() })
	return graphStore
}

func startServing(t *testing.T, conversationalHead *Head) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- conversationalHead.Serve(ctx) }()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("serve returned %v, want context cancellation", err)
				}
			case <-time.After(2 * time.Second):
				t.Error("serve did not stop after cancellation")
			}
		})
	}
}

func waitForAgentReply(t *testing.T, graphStore *store.Store, sessionID string, afterSeq int64) store.Message {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := graphStore.Messages(sessionID, afterSeq, 0)
		if err != nil {
			t.Fatalf("list replies: %v", err)
		}
		for _, message := range messages {
			if message.Role == store.RoleAgent {
				return message
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for agent reply in %s after %d", sessionID, afterSeq)
	return store.Message{}
}

func textResponse(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "stop",
	}}}
}

func TestHeadParsesReflexAndAnchorsVerbatimIntent(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"Doing that now.","command":{"kind":"reflex","target":"","instruction":"model rewrite"}}`,
	}}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "chat-reflex", Role: store.RoleUser,
		Body: "Read VERSION and tell me the value.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := New(client, graphStore).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graphStore.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("pending reflex commands = %+v", commands)
	}
	command := commands[0]
	if command.Kind != store.CommandSplice || !command.Reflex || command.Instruction != user.Body {
		t.Fatalf("parsed reflex command = %+v, want verbatim %q", command, user.Body)
	}
}

func TestHeadPromotesConsequentialReflexDecisionBeforePersistence(t *testing.T) {
	unsafe := routeDecision{
		Reply:   "On it.",
		Command: &routeCommand{Kind: routeReflexKind, Instruction: "Pay the vendor five dollars."},
	}
	if err := unsafe.validate(); err == nil {
		t.Fatal("routing decision validation accepted a money-shaped reflex")
	}
	for _, ask := range []string{
		"Pay the vendor five dollars.",
		"Delete /etc/obsolete.conf.",
		"Publish the draft release.",
	} {
		t.Run(ask, func(t *testing.T) {
			graphStore := openHeadStore(t)
			client := &fakeClient{responses: []string{
				`{"reply":"On it.","command":{"kind":"reflex","target":"","instruction":"ignored"}}`,
			}}
			user, err := graphStore.PostMessage(store.Message{
				SessionID: "chat-consequence", Role: store.RoleUser, Body: ask,
			})
			if err != nil {
				t.Fatal(err)
			}
			decision, err := New(client, graphStore).route(context.Background(), user)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Command == nil || decision.Command.Kind != string(store.CommandSplice) {
				t.Fatalf("decision for %q = %+v, want ordinary splice", ask, decision.Command)
			}
			_, reflex, _ := commandKind(decision.Command.Kind)
			if reflex {
				t.Fatalf("consequential ask %q remained a reflex", ask)
			}
			if decision.Command.Instruction != ask {
				t.Fatalf("instruction = %q, want %q", decision.Command.Instruction, ask)
			}
		})
	}
}

func TestHeadReceivesMeasuredReflexPrior(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"Nothing is running.","command":null}`,
	}}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "chat-prior", Role: store.RoleUser, Body: "what is running?",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(client, graphStore).
		WithSelfKnowledge(func() string {
			return "reflex: median 200 tokens, 2 turns; n=10; success=90.0%; promoted=10.0%; avg cost=$0.0010"
		}).
		route(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != 2 ||
		!strings.Contains(client.seen[1].Content[0].Text, "Measured execution history") ||
		!strings.Contains(client.seen[1].Content[0].Text, "promoted=10.0%") {
		t.Fatalf("measured reflex prior did not reach head: %+v", client.seen)
	}
}

func TestHeadGroundsCompetenceQuestionInExistingSingleCall(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"I'm strongest at Go parser work, with eight clean runs.","command":null}`,
	}}
	user := store.Message{SessionID: "competence", Body: "what are you good at now?"}
	groundCalls := 0
	decision, err := New(client, graphStore).
		WithCompetenceMap(func() string {
			groundCalls++
			return `- {"scope":"tool:go","class":"strong","samples":8,"failure_rate":0}`
		}).
		route(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Command != nil || groundCalls != 1 || client.callCount() != 1 {
		t.Fatalf("competence route = %+v, ground calls %d, provider calls %d", decision, groundCalls, client.callCount())
	}
	if len(client.seen) != 2 || !strings.Contains(client.seen[1].Content[0].Text, "Competence map (ground truth") ||
		!strings.Contains(client.seen[1].Content[0].Text, `"scope":"tool:go"`) {
		t.Fatalf("competence evidence did not reach head: %+v", client.seen)
	}
}

func TestHeadDoesNotReadCompetenceMapForUnrelatedMessage(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{
		`{"reply":"Hello.","command":null}`,
	}}
	called := false
	_, err := New(client, graphStore).
		WithCompetenceMap(func() string { called = true; return "unexpected" }).
		route(context.Background(), store.Message{Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("unrelated message read competence map")
	}
}

func TestHeadVoicePromptPreservesEmptyBytesAndRendersPreference(t *testing.T) {
	t.Run("empty notebook", func(t *testing.T) {
		graphStore := openHeadStore(t)
		client := &fakeClient{responses: []string{
			`{"reply":"Ready.","command":null}`,
		}}
		user := store.Message{SessionID: "voice-empty", Body: "answer this plainly"}
		if _, err := New(client, graphStore).route(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if len(client.seen) != 2 || client.seen[0].Content[0].Text != headSystemPrompt {
			t.Fatalf("empty-notebook head system prompt changed: %+v", client.seen)
		}
	})

	t.Run("standing preference", func(t *testing.T) {
		graphStore := openHeadStore(t)
		const preference = "keep answers short; no preamble"
		if _, err := graphStore.RecordFact("", "user", store.FactPreference, preference); err != nil {
			t.Fatal(err)
		}
		client := &fakeClient{responses: []string{
			`{"reply":"Ready.","command":null}`,
		}}
		user := store.Message{SessionID: "voice-learned", Body: "answer this plainly"}
		if _, err := New(client, graphStore).route(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if len(client.seen) != 2 || !strings.Contains(client.seen[0].Content[0].Text, preference) {
			t.Fatalf("head system prompt omitted voice preference: %+v", client.seen)
		}
	})
}

func TestHeadRememberPathCapturesStatedVoicePreference(t *testing.T) {
	graphStore := openHeadStore(t)
	const preference = "keep answers short; no preamble"
	client := &fakeClient{responses: []string{
		`{"reply":"Got it.","command":null,"remember":{"scope":"user","kind":"preference","body":"keep answers short; no preamble"},"retract":null}`,
	}}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "voice-remember", Role: store.RoleUser,
		Body: "From now on, keep answers short and skip the preamble.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graphStore).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	facts, err := graphStore.ActiveFacts("user", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Kind != store.FactPreference || facts[0].Body != preference {
		t.Fatalf("remembered voice preference = %+v", facts)
	}
}

func TestHeadAffirmativeRaisesRailAndRunnerResumes(t *testing.T) {
	graph := openHeadStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "paused-work", Brief: "finish after approval", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "rail-head", Intent: "finish it"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 1}); err != nil {
		t.Fatal(err)
	}
	runs := 0
	runner := resident.NewRunner(graph, func(context.Context, store.Node) (resident.ExecResult, error) {
		runs++
		return resident.ExecResult{Summary: "finished after approval"}, nil
	}, "head-rail-runner", 1).WithDailyBudgetUSD(1)
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 0 {
		t.Fatalf("paused tick dispatched=%d err=%v", dispatched, err)
	}

	user, err := graph.PostMessage(store.Message{SessionID: "rail-head", Role: store.RoleUser, Body: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}
	if err := New(client, graph).WithDailyBudgetUSD(1).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if client.calls != 0 {
		t.Fatalf("affirmative rail reply used %d model calls, want zero", client.calls)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	raises := 0
	for _, event := range events {
		if event.Kind == store.EventRailRaised {
			raises++
		}
	}
	if raises != 1 {
		t.Fatalf("rail raise events = %d, want one", raises)
	}
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("resumed tick dispatched=%d err=%v", dispatched, err)
	}
	runner.Wait()
	if runs != 1 {
		t.Fatalf("resumed executor runs = %d, want one", runs)
	}
	node, ok, err := graph.Node("paused-work")
	if err != nil || !ok || node.Status != store.Done {
		t.Fatalf("resumed node = %+v ok=%t err=%v", node, ok, err)
	}
	messages, err := graph.Messages("rail-head", user.Seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !strings.Contains(messages[0].Body, "continuing") {
		t.Fatalf("rail acknowledgement = %+v", messages)
	}
}

func TestHeadSnapshotIncludesDailySpendAndCeiling(t *testing.T) {
	graph := openHeadStore(t)
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 2.5}); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{
		`{"reply":"Nothing is running.","command":null,"remember":null,"retract":null}`,
	}}
	user := store.Message{SessionID: "rail-status", Body: "what is running?"}
	if _, err := New(client, graph).WithDailyBudgetUSD(20).route(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != 2 || !strings.Contains(client.seen[1].Content[0].Text,
		"today's spend: $2.50 of $20.00 daily rail") {
		t.Fatalf("head snapshot omitted daily rail: %+v", client.seen)
	}
}

func TestAffirmativeRailReplyVocabulary(t *testing.T) {
	for _, reply := range []string{"y", "Yes.", "continue", "go ahead", "proceed", "okay"} {
		if !affirmativeRailReply(reply) {
			t.Errorf("%q was not recognized as affirmative", reply)
		}
	}

	for _, reply := range []string{"no", "not yet", "what will it cost?"} {
		if affirmativeRailReply(reply) {
			t.Errorf("%q was recognized as affirmative", reply)
		}
	}
}

func TestStandingRecognitionTable(t *testing.T) {
	tests := []struct {
		instruction string
		standing    bool
	}{
		{"Whenever a PR opens, review it.", true},
		{"Every morning send me a digest.", true},
		{"Each time the build fails, summarize it.", true},
		{"Keep the suite green.", true},
		{"Watch this folder for new PDFs.", true},
		{"Remind me when the deployment finishes.", true},
		{"Remind me in 20 minutes to stretch.", true},
		{"Make sure the release branch stays green.", true},
		{"Summarize this file.", false},
		{"Build the widget and keep the API name.", false},
		{"When I say go, do X once.", false},
		{"Review every file in this directory once.", false},
	}
	for _, test := range tests {
		t.Run(test.instruction, func(t *testing.T) {
			if got := RecognizesStandingIntent(test.instruction); got != test.standing {
				t.Fatalf("standing = %t, want %t", got, test.standing)
			}
		})
	}
}

func TestCompilerBuildsStandingCharterDraftFields(t *testing.T) {
	tests := []struct {
		name        string
		instruction string
		context     string
		wantKind    store.WatchKind
		wantCadence string
		wantCron    func(t *testing.T, schedule store.CronSchedule)
		wantExpiry  string
		wantMax     int
		wantCost    float64
	}{
		{
			name: "measured recurring invariant", instruction: "Every morning review new PRs.",
			context: "reflex: avg cost=$0.07", wantKind: store.WatchCron,
			wantCadence: "Every morning",
			wantCron: func(t *testing.T, schedule store.CronSchedule) {
				if schedule.Kind != store.CronDaily || schedule.Hour != 9 || schedule.Minute != 0 {
					t.Fatalf("morning cadence compiled to %+v, want daily 09:00", schedule)
				}
			},
			wantExpiry: "never", wantMax: 10, wantCost: 0.07,
		},
		{
			name: "reminder degenerate charter", instruction: "Remind me tomorrow at 9 to call Mom.",
			wantKind: store.WatchCron, wantCadence: "tomorrow at 9",
			wantCron: func(t *testing.T, schedule store.CronSchedule) {
				tomorrow := time.Now().AddDate(0, 0, 1)
				if schedule.Kind != store.CronAt || schedule.At.Hour() != 9 ||
					schedule.At.Minute() != 0 || schedule.At.Day() != tomorrow.Day() {
					t.Fatalf("reminder cadence compiled to %+v, want at tomorrow 09:00", schedule)
				}
			},
			wantExpiry: "once", wantMax: 1, wantCost: 0.15,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{responses: []string{
				`{"invariant":"model rewrite","watch":{},"sentinel":"Has the condition occurred?","action":"Carry out the requested action.","rails":{}}`,
			}}
			brief, err := NewCompiler(client).Compile(context.Background(), test.instruction, test.context)
			if err != nil {
				t.Fatal(err)
			}
			if brief.Charter == nil || brief.Question == "" || len(brief.QuestionOptions) != 3 {
				t.Fatalf("standing brief = %+v", brief)
			}
			charter := brief.Charter
			if charter.Invariant != test.instruction || charter.Watch.Kind != test.wantKind ||
				charter.Watch.Cadence != test.wantCadence {
				t.Fatalf("charter watch/invariant = %+v", charter)
			}
			if charter.Watch.Spec.Kind != store.WatchCron || charter.Watch.Spec.Cron == nil {
				t.Fatalf("cadence words did not compile to a typed cron spec: %+v", charter.Watch.Spec)
			}
			test.wantCron(t, *charter.Watch.Spec.Cron)
			if charter.Rails.Expiry != test.wantExpiry || charter.Rails.MaxPerDay != test.wantMax ||
				charter.Rails.EstimatedCostUSD != test.wantCost ||
				strings.TrimSpace(charter.Rails.MaxPerDayJustification) == "" {
				t.Fatalf("charter rails = %+v", charter.Rails)
			}
			if charter.Sentinel == "" || charter.Action == "" ||
				(test.name == "reminder degenerate charter" && charter.Action != "Say: call Mom.") {
				t.Fatalf("charter judgment/action missing: %+v", charter)
			}
			if reminder := test.name == "reminder degenerate charter"; charter.SayOnly != reminder {
				t.Fatalf("say-only = %t, want %t", charter.SayOnly, reminder)
			}
		})
	}
}

func TestCompilerEpisodicOutputByteIdentity(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Produce the requested summary.","deliverable":"summary","budget":"$0.10","assumptions":["Use the current file"]}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "Summarize this file.", "root is running")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"goal":"Produce the requested summary.\n\nVerbatim request:\nSummarize this file.","assumptions":["Use the current file"],"deliverable":"summary","budget":"$0.10","scale":"task","builds_on":[],"question":"","trial_of":0}`
	if string(encoded) != want {
		t.Fatalf("episodic compile bytes changed:\n got %s\nwant %s", encoded, want)
	}
}

func TestCompilerQuestionOptionsPreserveOrder(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"question":"Which region?","question_options":[{"label":"Toronto","value":"ca"},{"label":"London","value":"uk"}]}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "Publish the regional report.", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.QuestionOptions) != 2 || brief.QuestionOptions[0].Label != "Toronto" ||
		brief.QuestionOptions[1].Value != "uk" {
		t.Fatalf("question options = %+v", brief.QuestionOptions)
	}
}

func TestRatificationOptionRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name       string
		reply      string
		wantStatus store.CharterStatus
		wantOnce   bool
	}{
		{name: "affirmative activates", reply: "yes", wantStatus: store.CharterActive},
		{name: "numeric activates", reply: "1", wantStatus: store.CharterActive},
		{name: "decline stays disarmed", reply: "3", wantStatus: store.CharterRetired, wantOnce: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := openHeadStore(t)
			client := &fakeClient{responses: []string{
				`{"watch":{},"sentinel":"Is there a new PR?","action":"Review the new PR.","rails":{}}`,
			}}
			compiler := NewCompiler(client)
			compile := func(ctx context.Context, instruction, graphContext string) (resident.Compiled, error) {
				brief, err := compiler.Compile(ctx, instruction, graphContext)
				if err != nil {
					return resident.Compiled{}, err
				}
				return resident.Compiled{
					Goal: brief.Goal, Assumptions: brief.Assumptions, Scale: brief.Scale,
					BuildsOn: brief.BuildsOn, Question: brief.Question,
					QuestionOptions: brief.QuestionOptions, Charter: brief.Charter,
				}, nil
			}
			reconciler := resident.New(graph, compile, nil)
			command, err := graph.RequestCommand(store.Command{
				SessionID: "ratify", Kind: store.CommandSplice,
				Instruction: "Whenever a PR opens, review it.",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			question := waitForAgentReply(t, graph, "ratify", command.Seq)
			if len(question.Options) != 3 || question.Options[0].Label != "yes, stand this up" {
				t.Fatalf("ratification question = %+v", question)
			}
			user, err := graph.PostMessage(store.Message{
				SessionID: "ratify", Role: store.RoleUser, Body: test.reply,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			charters, err := graph.Charters()
			if err != nil || len(charters) != 1 || charters[0].Status != test.wantStatus {
				t.Fatalf("charters = %+v err=%v", charters, err)
			}
			if test.wantStatus == store.CharterActive {
				// The point of the whole seam: a recognized then ratified ask
				// lands in the one canonical table the watch engine reads.
				due, err := graph.DueCharters(time.Now().Add(time.Minute), 10)
				if err != nil {
					t.Fatal(err)
				}
				visible := false
				for _, charter := range due {
					visible = visible || charter.ID == charters[0].ID
				}
				if !visible {
					t.Fatalf("ratified charter is invisible to the watch engine: due=%+v", due)
				}
			}
			if test.wantOnce {
				nodes, err := graph.Nodes()
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, node := range nodes {
					found = found || (node.Parent == store.RootID && node.Provenance.Intent == charters[0].Invariant)
				}
				if !found {
					t.Fatal("declining standing did not splice the requested action once")
				}
			}
		})
	}
}

func TestGenericQuestionNumericSelectionContinuesCompile(t *testing.T) {
	graph := openHeadStore(t)
	compile := func(context.Context, string, string) (resident.Compiled, error) {
		return resident.Compiled{
			Question: "Which region?",
			QuestionOptions: []store.QuestionOption{
				{Label: "Toronto", Value: "ca"}, {Label: "London", Value: "uk"},
			},
		}, nil
	}
	reconciler := resident.New(graph, compile, nil)
	original, err := graph.RequestCommand(store.Command{
		SessionID: "generic-options", Kind: store.CommandSplice, Instruction: "Publish the regional report.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	question := waitForAgentReply(t, graph, "generic-options", original.Seq)
	if len(question.Options) != 2 {
		t.Fatalf("question options = %+v", question.Options)
	}
	user, err := graph.PostMessage(store.Message{
		SessionID: "generic-options", Role: store.RoleUser, Body: "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	pending, err := graph.PendingCommands(0)
	if err != nil || len(pending) != 1 ||
		!strings.Contains(pending[0].Instruction, "Answer to compiler question: London") {
		t.Fatalf("continued command = %+v err=%v", pending, err)
	}
}

func TestConversationalCharterManagement(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		wantStatus  store.CharterStatus
		wantCadence string
	}{
		{name: "pause", message: "pause the morning digest", wantStatus: store.CharterPaused},
		{name: "retire", message: "stop watching the morning digest", wantStatus: store.CharterRetired},
		{name: "edit cadence", message: "make the morning digest hourly", wantStatus: store.CharterActive, wantCadence: "hourly"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openHeadStore(t)
			charter := activateHeadCharter(t, graph, "digest",
				"Every morning send the release digest.")
			user, err := graph.PostMessage(store.Message{
				SessionID: "manage", Role: store.RoleUser, Body: test.message,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if err := resident.New(graph, nil, nil).Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			updated, found, err := graph.Charter(charter.ID)
			if err != nil || !found || updated.Status != test.wantStatus {
				t.Fatalf("updated charter = %+v found=%t err=%v", updated, found, err)
			}
			if test.wantCadence != "" {
				if updated.Watch.Cadence != test.wantCadence {
					t.Fatalf("cadence = %q, want %q", updated.Watch.Cadence, test.wantCadence)
				}
				if updated.Watch.Cron == nil || updated.Watch.Cron.Kind != store.CronEveryHours ||
					updated.Watch.Cron.Interval != 1 {
					t.Fatalf("cadence edit did not produce a typed hourly schedule: %+v", updated.Watch)
				}
			}
			messages, err := graph.Messages("manage", user.Seq, 0)
			if err != nil || len(messages) < 2 || messages[len(messages)-1].Role != store.RoleSystem ||
				strings.Contains(messages[len(messages)-1].Body, "\n") {
				t.Fatalf("management receipts = %+v err=%v", messages, err)
			}
		})
	}
}

func TestAmbiguousCharterManagementProducesOptions(t *testing.T) {
	graph := openHeadStore(t)
	activateHeadCharter(t, graph, "frontend-prs", "Whenever frontend PRs open, review them.")
	activateHeadCharter(t, graph, "backend-prs", "Whenever backend PRs open, review them.")
	user, err := graph.PostMessage(store.Message{
		SessionID: "ambiguous-charter", Role: store.RoleUser, Body: "stop watching PRs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "ambiguous-charter", user.Seq)
	if !strings.HasPrefix(reply.Body, "Which standing charter do you mean?") || len(reply.Options) != 2 {
		t.Fatalf("ambiguous reply = %+v", reply)
	}
	// The body carries the structured payload the TUI's question components
	// read, beside the durable option rows.
	if !strings.Contains(reply.Body, `"kind":"choose"`) {
		t.Fatalf("ambiguous reply lacks the structured question payload: %q", reply.Body)
	}
	pending, err := graph.PendingCommands(0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("ambiguous management emitted commands: %+v err=%v", pending, err)
	}
}

func activateHeadCharter(t *testing.T, graph *store.Store, id, invariant string) store.Charter {
	t.Helper()
	charter, err := graph.DraftCharter(id, "manage", 0, store.CharterSpec{
		Invariant: invariant,
		Watch: store.CharterWatch{
			Kind: store.WatchCron, Cadence: "every morning", Schedule: "0 9 * * *",
		},
		Sentinel: "Is a delivery due?", Action: "Send the digest.",
		Rails: store.CharterSpecRails{
			EstimatedCostUSD: 0.05, MaxPerDay: 1,
			MaxPerDayJustification: "one scheduled delivery", Expiry: "never",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SetCharterStatus(id, store.CharterActive, store.Ratification{
		Origin: store.OriginUser, SessionID: "manage", Evidence: "yes, stand this up",
	}); err != nil {
		t.Fatal(err)
	}
	charter.Status = store.CharterActive
	return charter
}
