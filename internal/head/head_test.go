package head

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type headModalities bool

func (supported headModalities) Supports(_, direction, modality string) bool {
	return bool(supported) && direction == "input" && modality == "image"
}

func TestHeadRoutesImagePartAndPreservesAttachmentOnCommand(t *testing.T) {
	graph := openHeadStore(t)
	path := filepath.Join(t.TempDir(), "diagram.png")
	if err := os.WriteFile(path, []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{
		`{"reply":"I’ll inspect that.","command":{"kind":"splice","target":"","instruction":"inspect the diagram"}}`,
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "vision", Role: store.RoleUser, Body: "inspect the diagram", Attachments: []string{path},
	})
	if err != nil {
		t.Fatal(err)
	}
	head := New(client, graph).WithImageInput(headModalities(true), "vision/model")
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	seenImage := false
	for _, part := range client.seen[1].Content {
		seenImage = seenImage || part.Type == "image_url" && part.ImageURL != nil && strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,")
	}
	if !seenImage {
		t.Fatalf("head messages omitted image part: %+v", client.seen)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || len(commands[0].Attachments) != 1 || commands[0].Attachments[0] != path {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
}

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
