package head

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/resident"
	"github.com/Agent-Field/codeaf/internal/store"
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
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolTask, map[string]any{
			"instruction": "inspect the diagram"})}},
		{text: "I’ll inspect that."},
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
	model     string
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
	response := textResponse(text)
	response.Model = client.model
	return response, nil
}

func (client *fakeClient) Model() string { return client.model }

func (client *fakeClient) callCount() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.calls
}

// systemPrompt is which of the head's prompts the last call carried. It is how a
// test says "not the router" now that more than one path speaks to a model.
func (client *fakeClient) systemPrompt() string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.seen) == 0 {
		return ""
	}
	var text strings.Builder
	for _, part := range client.seen[0].Content {
		text.WriteString(part.Text)
	}
	return text.String()
}

// userPrompt is everything the last call said below the system message. Tests
// that pin where a block sits need both halves, because "which message carries
// it" is now part of what is being asserted.
func (client *fakeClient) userPrompt() string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.seen) < 2 {
		return ""
	}
	var text strings.Builder
	for _, part := range client.seen[1].Content {
		text.WriteString(part.Text)
	}
	return text.String()
}

func TestHeadPostsReply(t *testing.T) {
	graphStore := openHeadStore(t)
	// One call, no tools asked for, and the words are the reply. There is no
	// envelope to parse any more: what the model says IS what the person reads.
	client := &fakeClient{responses: []string{"The graph is ready and waiting for work."}}
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

func TestHeadUsesPerMessageClientAndPersistsResolvedReplyModel(t *testing.T) {
	graph := openHeadStore(t)
	talk := &fakeClient{model: "cheap/talk", responses: []string{"ordinary answer"}}
	boost := &fakeClient{model: "anthropic/claude-opus-5-2026-08-01", responses: []string{"boosted answer"}}
	var selected []string
	conversationalHead := New(talk, graph).WithMessageClient(func(message store.Message) (Client, error) {
		selected = append(selected, message.Model)
		return boost, nil
	})

	ordinary, err := graph.PostMessage(store.Message{SessionID: "client-override", Role: store.RoleUser, Body: "easy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conversationalHead.answer(context.Background(), ordinary); err != nil {
		t.Fatal(err)
	}
	boosted, err := graph.PostMessage(store.Message{
		SessionID: "client-override", Role: store.RoleUser, Body: "hard", Model: "anthropic/claude-opus-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := conversationalHead.answer(context.Background(), boosted); err != nil {
		t.Fatal(err)
	}

	if talk.callCount() != 1 || boost.callCount() != 1 || len(selected) != 1 || selected[0] != boosted.Model {
		t.Fatalf("client calls talk=%d boost=%d selected=%v", talk.callCount(), boost.callCount(), selected)
	}
	messages, err := graph.Messages("client-override", ordinary.Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	var replies []store.Message
	for _, message := range messages {
		if message.Role == store.RoleAgent {
			replies = append(replies, message)
		}
	}
	if len(replies) != 2 || replies[0].Model != "" || replies[1].Model != boost.model {
		t.Fatalf("reply attribution = %+v", replies)
	}
}

func TestHeadResolvesReferencedAgentQuestionWithoutRoutingNewWork(t *testing.T) {
	graph := openHeadStore(t)
	question, err := graph.AskQuestion(store.AgentQuestion{
		SessionID: "agent-question", Text: "Which tone should I use?", Urgency: store.QuestionWhenever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
		t.Fatal(err)
	}
	user, err := graph.PostMessage(store.Message{
		SessionID: "agent-question", Role: store.RoleUser, Body: "Keep it concise.",
		QuestionSeq: question.Seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if calls := client.callCount(); calls != 0 {
		t.Fatalf("answer was routed as new work: provider calls=%d", calls)
	}
	resolved, found, err := graph.AgentQuestionBySeq(question.Seq)
	if err != nil || !found || resolved.Status != store.QuestionAnswered ||
		resolved.Resolution != user.Body || resolved.AnswerMessageSeq != user.Seq {
		t.Fatalf("resolved question = %+v found=%t err=%v", resolved, found, err)
	}
	messages, err := graph.Messages("agent-question", user.Seq, 0)
	if err != nil || len(messages) != 1 || messages[0].Role != store.RoleAgent ||
		!strings.Contains(messages[0].Body, "use that") {
		t.Fatalf("answer acknowledgement = %+v err=%v", messages, err)
	}
}

func TestHeadRequestsSpliceAndLinksReply(t *testing.T) {
	graphStore := openHeadStore(t)
	// Commissioning work is a tool call inside the turn rather than the turn's
	// terminal decision, and the receipt still has to be tied to the command the
	// tool journaled — otherwise a reply claims work that has no row.
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolTask, map[string]any{"instruction": "make me X"})}},
		{text: "Splicing that in — I'll report when it lands."},
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
	rendered := renderNotebook(graphStore, "Please inspect internal/resident/notebook.go", "", notebookContextBytes)
	if !strings.Contains(rendered, fmt.Sprintf("#%d [", fact.Seq)) ||
		!strings.Contains(rendered, "scope-only memory with unrelated vocabulary") {
		t.Fatalf("scope-exact notebook fact did not reach head: %q", rendered)
	}
}

func TestHeadLearningQuestionCarriesSeededNotebookFact(t *testing.T) {
	graph := openHeadStore(t)
	fact, err := graph.RecordFact(store.RootID, "repo:codeaf", store.FactLesson,
		"card receipts stay anchored to the job that produced them")
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{responses: []string{
		"I learned that card receipts stay with their originating job.",
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "learning-question", Role: store.RoleUser,
		Body: "what have you learned about this repo?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != 2 {
		t.Fatalf("head messages = %+v", client.seen)
	}
	prompt := client.seen[1].Content[0].Text
	if !strings.Contains(prompt, fmt.Sprintf("#%d", fact.Seq)) || !strings.Contains(prompt, fact.Body) {
		t.Fatalf("learning question omitted notebook grounding:\n%s", prompt)
	}
}

// THE HEAD LOST ITS RETRACTION DOOR IN THIS WAVE. The router's `retract` field
// was the only caller of the quarantine seam, and the belt has no tool that
// reaches it — so "that's wrong, forget that" can be replied to warmly and
// change nothing, which is the exact accumulation failure the note tool's
// supersession exists to prevent from the other side.
//
// The seam itself is intact and is what this pins: quarantine still retires a
// numbered belief, still keeps the user's own message as the evidence for it,
// still hides it from head retrieval, and still survives a rebuild of the
// journal. Everything a door would need is here; the door is what is missing,
// and it is recorded as a gap rather than papered over with a passing test.
func TestHeadRetractsNumberedNotebookBelief(t *testing.T) {
	graphStore := openHeadStore(t)
	fact, err := graphStore.RecordFact(store.RootID, "tool:git", store.FactQuirk,
		"git always destroys worktrees")
	if err != nil {
		t.Fatal(err)
	}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "chat-retract", Role: store.RoleUser, Body: "that's wrong — forget that",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The door back. The router's `retract` field was the only caller of this
	// seam and it died with the router; the forget tool is what reaches it now,
	// and this test is what says the behaviour on the far side of it is
	// unchanged — the belief is quarantined rather than deleted, the evidence is
	// the message that retired it, and the retirement replays.
	run := &beltRun{head: New(nil, graphStore), user: user}
	message, failed := run.execute(beltToolForget, beltArguments(t,
		map[string]any{"belief": fact.Seq}))
	if failed {
		t.Fatalf("the belt could not let go of a belief the person retracted: %s", message)
	}
	if run.commandSeq != 0 {
		t.Fatalf("letting go of a belief journaled a graph command: %d", run.commandSeq)
	}
	quarantined, found, err := graphStore.FactBySeq(fact.Seq)
	if err != nil || !found || quarantined.Status != store.FactQuarantined ||
		quarantined.EvidenceSeq != user.Seq || quarantined.StatusOrigin != store.FactOriginUser {
		t.Fatalf("retracted fact = %+v found=%t err=%v", quarantined, found, err)
	}
	if rendered := renderNotebook(graphStore, "git worktrees", "", notebookContextBytes); strings.Contains(rendered, fact.Body) {
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
	if len(brief.Assumptions) != 2 || brief.Assumptions[0] != "Use the current branch" {
		t.Fatalf("assumptions wrong: %+v", brief.Assumptions)
	}
}

// TestCompileSurvivesAMissingDeliverableAndBudget pins the removal of two
// schema-required, validation-enforced fields that nothing read. An empty one
// used to return "compile request: empty budget" with no retry, so a perfectly
// good request was rejected for a value no consumer would ever have looked at.
// The fields are gone; a provider that still emits them is simply ignored.
func TestCompileSurvivesAMissingDeliverableAndBudget(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Close the quarter's books.","assumptions":["Use the Q3 ledger"]}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "close the books", "root is running")
	if err != nil {
		t.Fatalf("a brief with no deliverable and no budget was rejected: %v", err)
	}
	if !strings.HasPrefix(brief.Goal, "Close the quarter's books.") {
		t.Fatalf("goal wrong: %q", brief.Goal)
	}
	for _, gone := range []string{`"deliverable"`, `"budget"`} {
		if strings.Contains(compilerSystemPrompt, gone) {
			t.Errorf("the compiler still asks for %s, a field nothing reads", gone)
		}
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
	client := &fakeClient{responses: []string{"new answer"}}

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

// A reflex is the model's judgment, passed as an argument to spawn rather than
// encoded in a command kind on a terminal routing decision. What has to survive
// that move is the pair of properties the terminal position guaranteed: the flag
// reaches the journaled row, and the words that travel are the person's own.
func TestHeadParsesReflexAndAnchorsVerbatimIntent(t *testing.T) {
	graphStore := openHeadStore(t)
	const ask = "Read VERSION and tell me the value."
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolTask, map[string]any{
			"instruction": ask, "reflex": true})}},
		{text: "Doing that now."},
	}}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "chat-reflex", Role: store.RoleUser, Body: ask,
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
	// The tool the model reads is what tells it the flag is narrow. A permission
	// with no stated bound is a permission with no bound.
	spawnDescription := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolTask {
			spawnDescription = definition.Function.Description
		}
	}
	if !strings.Contains(spawnDescription, "do not improve or summarize them") {
		t.Errorf("the verbatim rule is no longer stated where the model reads it: %q", spawnDescription)
	}
}

// The consequence gate, moved with the judgment it guards.
//
// It used to run at routing-decision validation, before persistence, because
// that was the last place a reflex could still be caught. The judgment is an
// argument to a tool now, so the gate moved to the journaling door — the last
// place it can still be true, rather than trusted from wherever the flag was
// set. Same three asks, same verdict: the reflex is dropped, the work is
// ordinary work the person sees coming, and the words are untouched.
func TestHeadPromotesConsequentialReflexBeforePersistence(t *testing.T) {
	for _, ask := range []string{
		"Pay the vendor five dollars.",
		"Delete /etc/obsolete.conf.",
		"Publish the draft release.",
	} {
		t.Run(ask, func(t *testing.T) {
			graphStore := openHeadStore(t)
			if !consequenceGated(ask) {
				t.Fatalf("%q is no longer read as consequential", ask)
			}
			user := postUser(t, graphStore, "chat-consequence", ask)
			run := &beltRun{head: New(nil, graphStore), user: user}
			result, failed := run.execute(beltToolTask, beltArguments(t, map[string]any{
				"instruction": ask, "reflex": true}))
			if failed {
				t.Fatalf("spawn refused %q outright: %s", ask, result)
			}
			commands := pendingCommandsOf(t, graphStore)
			if len(commands) != 1 {
				t.Fatalf("%q journaled %d commands: %+v", ask, len(commands), commands)
			}
			command := commands[0]
			if command.Kind != store.CommandSplice || strings.TrimSpace(command.Target) != "" {
				t.Fatalf("command for %q = %s at %q, want an ordinary splice", ask, command.Kind, command.Target)
			}
			if command.Reflex {
				t.Fatalf("consequential ask %q remained a reflex", ask)
			}
			if command.Instruction != ask {
				t.Fatalf("instruction = %q, want %q", command.Instruction, ask)
			}
			// And the receipt does not claim the fast lane it was denied.
			if strings.Contains(result, "immediate action") {
				t.Fatalf("the receipt promised a reflex that was refused: %q", result)
			}
		})
	}
}

func TestHeadReceivesMeasuredReflexPrior(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{"Nothing is running."}}
	user, err := graphStore.PostMessage(store.Message{
		SessionID: "chat-prior", Role: store.RoleUser, Body: "what is running?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(client, graphStore).
		WithSelfKnowledge(func() string {
			return "reflex: median 200 tokens, 2 turns; n=10; success=90.0%; promoted=10.0%; avg cost=$0.0010"
		}).
		answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != 2 ||
		!strings.Contains(client.seen[1].Content[0].Text, "Measured execution history") ||
		!strings.Contains(client.seen[1].Content[0].Text, "promoted=10.0%") {
		t.Fatalf("measured reflex prior did not reach head: %+v", client.seen)
	}
}

// The head does not carry the competence map in its prompt at all — it is a belt
// read — and what it must not do is invent one in its absence. The enforcement
// used to be split between a router prompt that promised the block and a belt
// that held the tool; it is one prompt and one tool now, and both halves are
// pinned: the prompt never promises the block and forbids saying anything a tool
// did not show, and the read that WOULD ground the answer exists and says so.
func TestRouterNeitherCarriesNorInventsAMeasuredSelfAssessment(t *testing.T) {
	graphStore := openHeadStore(t)
	client := &fakeClient{responses: []string{
		"I'm strongest at Go parser work, with eight clean runs.",
	}}
	user := postUser(t, graphStore, "competence", "what are you good at now?")
	called := false
	if err := New(client, graphStore).
		WithCompetenceMap(func() string { called = true; return "unexpected" }).
		answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("the head still pulls the competence map behind a phrase gate")
	}
	if strings.Contains(client.seen[1].Content[0].Text, "Competence map (ground truth") {
		t.Fatalf("the competence block is still injected: %s", client.seen[1].Content[0].Text)
	}
	if strings.Contains(orchestratorPrompt, "When a competence map appears") ||
		strings.Contains(orchestratorPrompt, "When standing-watch status appears") {
		t.Error("the head's prompt still promises blocks it is never handed")
	}
	if !strings.Contains(orchestratorPrompt, "Ground every claim in something a tool showed you this turn") {
		t.Error("the prompt lost the rule against stating what nothing showed it")
	}
	// The other half: a self-assessment is not forbidden, it is grounded. The
	// read that grounds it has to exist and has to say that it is the only
	// evidence there is, or the rule above is a rule against answering at all.
	competence := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolCompetence {
			competence = definition.Function.Description
		}
	}
	if competence == "" {
		t.Fatal("there is no competence read, so the honest answer is unreachable")
	}
	if !strings.Contains(competence, "a self-assessment given without it is invention") {
		t.Errorf("the competence read no longer names the failure it prevents: %q", competence)
	}
}

func TestHeadVoicePromptPreservesEmptyBytesAndRendersPreference(t *testing.T) {
	// The empty-notebook prompt is the stable prompt PLUS the register, exactly
	// — the register is unconditional now, and the byte diff from the bare
	// prompt is that one segment and nothing else.
	t.Run("empty notebook", func(t *testing.T) {
		graphStore := openHeadStore(t)
		client := &fakeClient{responses: []string{"Ready."}}
		user := postUser(t, graphStore, "voice-empty", "answer this plainly")
		if err := New(client, graphStore).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		want := orchestratorPrompt + "\n\n" + resident.VoiceRegister
		if len(client.seen) != 2 || client.seen[0].Content[0].Text != want {
			t.Fatalf("empty-notebook head system prompt changed: %+v", client.seen)
		}
	})

	t.Run("standing preference", func(t *testing.T) {
		graphStore := openHeadStore(t)
		const preference = "keep answers short; no preamble"
		if _, err := graphStore.RecordFact("", "user", store.FactPreference, preference); err != nil {
			t.Fatal(err)
		}
		client := &fakeClient{responses: []string{"Ready."}}
		user := postUser(t, graphStore, "voice-learned", "answer this plainly")
		if err := New(client, graphStore).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if len(client.seen) != 2 || !strings.Contains(client.seen[0].Content[0].Text, preference) {
			t.Fatalf("head system prompt omitted voice preference: %+v", client.seen)
		}
	})
}

// A durable preference lands through the note tool now rather than through a
// `remember` field on a routing decision. The capture is the same fact machinery
// and the same notebook; what changed is that writing it is an ACT the reply may
// only claim because a call returned, instead of a field that could be set
// beside a sentence that never mentioned it.
func TestHeadRememberPathCapturesStatedVoicePreference(t *testing.T) {
	graphStore := openHeadStore(t)
	const preference = "keep answers short; no preamble"
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("n1", beltToolNote, map[string]any{
			"scope": "user", "kind": "preference", "body": preference})}},
		{text: "Got it."},
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
	// And it comes back on the next message, which is the whole point of durable.
	if rendered := renderNotebook(graphStore, "how should you answer me", "", notebookContextBytes); !strings.Contains(rendered, preference) {
		t.Fatalf("the notebook does not read the preference back:\n%s", rendered)
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
	client := &fakeClient{responses: []string{"Nothing is running."}}
	user := postUser(t, graph, "rail-status", "what is running?")
	if err := New(client, graph).WithDailyBudgetUSD(20).answer(context.Background(), user); err != nil {
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

// The temporal route that drafted a standing rule out of a recurring-sounding
// ask went with the v1 scheduler. Such an ask is compiled like any other — by
// the one compile call, into ordinary work that carries its sentence verbatim —
// and nothing is held back for a ratification card that no longer exists.
func TestARecurringSoundingAskCompilesAsOrdinaryWork(t *testing.T) {
	const ask = "remind me every monday at 9 to water the plants"
	client := &fakeClient{responses: []string{`{"goal":"Water the plants."}`}}
	brief, err := NewCompiler(client).Compile(context.Background(), ask, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(brief.Question) != "" || len(brief.QuestionOptions) != 0 {
		t.Fatalf("a recurring-sounding ask was asked about: %+v", brief)
	}
	if !strings.Contains(brief.Goal, ask) {
		t.Fatalf("goal = %q, want the ask carried verbatim", brief.Goal)
	}
	if client.calls != 1 {
		t.Fatalf("the ask took %d model calls, want the one compile", client.calls)
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
	const want = `{"goal":"Produce the requested summary.\n\nVerbatim request:\nSummarize this file.","assumptions":["Use the current file"],"scale":"task","builds_on":[],"question":"","trial_of":0}`
	if string(encoded) != want {
		t.Fatalf("episodic compile bytes changed:\n got %s\nwant %s", encoded, want)
	}

	// The name the compile pass now returns rides the same object and nothing
	// else moves. A compiler that says nothing about the name adds no field at
	// all, which is what keeps the shape above byte-identical.
	named := &fakeClient{responses: []string{
		`{"goal":"Produce the requested summary.","title":"File summary","assumptions":["Use the current file"]}`,
	}}
	titled, err := NewCompiler(named).Compile(context.Background(), "Summarize this file.", "root is running")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(titled)
	if err != nil {
		t.Fatal(err)
	}
	const wantTitled = `{"goal":"Produce the requested summary.\n\nVerbatim request:\nSummarize this file.","assumptions":["Use the current file"],"title":"File summary","scale":"task","builds_on":[],"question":"","trial_of":0}`
	if string(encoded) != wantTitled {
		t.Fatalf("named compile bytes changed:\n got %s\nwant %s", encoded, wantTitled)
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

// A document is not something the talk model can see. It must stay off the
// wire as a content part and still reach the command, because the whole point
// is that a worker reads it from the workspace with read_document.
func TestHeadKeepsDocumentAttachmentOffTheModelAndOnTheCommand(t *testing.T) {
	graph := openHeadStore(t)
	path := filepath.Join(t.TempDir(), "filing.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7 filing"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolTask, map[string]any{
			"instruction": "summarise the filing"})}},
		{text: "I’ll read it."},
	}}
	user, err := graph.PostMessage(store.Message{
		SessionID: "docs", Role: store.RoleUser, Body: "summarise the filing", Attachments: []string{path},
	})
	if err != nil {
		t.Fatal(err)
	}
	head := New(client, graph).WithImageInput(headModalities(true), "vision/model")
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	for _, part := range client.seen[1].Content {
		if part.Type != "text" {
			t.Fatalf("document rode as a %q content part: %+v", part.Type, client.seen[1].Content)
		}
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || len(commands[0].Attachments) != 1 || commands[0].Attachments[0] != path {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
}

func TestCompilerIsToldHowAttachedDocumentsReachWorkers(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Summarise the filing.","deliverable":"a summary","budget":"$0.40","assumptions":[]}`,
	}}
	if _, err := NewCompiler(client).Compile(context.Background(), "summarise the filing",
		"Attached documents (workspace inputs):\n- q3 filing.pdf"); err != nil {
		t.Fatalf("compile: %v", err)
	}
	system := client.seen[0].Content[0].Text
	for _, want := range []string{"attached documents", "read_document", "workspace files"} {
		if !strings.Contains(system, want) {
			t.Fatalf("compiler prompt omitted %q", want)
		}
	}
}

// The receipts that motivated the rewrite, from a real run of "give me a PR in
// draft for issue 557": the compiler declared "The PR will be in draft state" —
// the request said back — and "The user has write access" — a fact no worker
// acts on. Neither changes anything anyone does, and assumptions now travel
// with the work, so a line that changes nothing is a line nobody can honor.
func TestCompilerIsToldAssumptionsAreDecisionsTheWorkIsHeldTo(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Open a draft pull request that closes issue 557.","deliverable":"a draft pull request",` +
			`"budget":"$0.60","assumptions":["Branch issue-557-fix off main and open the PR against main",` +
			`"Run the test suite and review the diff for security regressions before opening the PR"]}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(),
		"give me a PR in draft for issue 557", "root is running")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(brief.Assumptions) != 2 {
		t.Fatalf("assumptions = %+v", brief.Assumptions)
	}
	system := client.seen[0].Content[0].Text
	for _, want := range []string{
		"a decision that changes what the workers will do",
		"an ambiguity you settled with a concrete choice",
		"a commitment about method or evidence the work will be held to",
		"Never restate the request",
		"if a line vanished and no worker would do anything differently, it was never a decision",
		"they travel with the work as standing orders",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("compiler prompt omitted %q", want)
		}
	}
}

// The compiler is the only prompt every job passes through — a task-scale or
// lookup-scale ask never reaches the planner — so the acceptance criterion it
// writes is the only one a single-leaf build is ever held to. Written as the
// builder's evidence it licences the failure it exists to catch: every part
// checked, the thing itself never used by anyone.
func TestCompilerWritesSuccessFromTheUsersSeat(t *testing.T) {
	for name, want := range map[string]string{
		"acceptance is the user's first use": "what they will do with it the first time",
		"observable, not inferred":           "what they must observe for it to count as working",
		"the harness is not their evidence":  "is the builder's evidence, never theirs",
		"shaping is proportional":            "Match the shaping to the ask",
		"and small asks stay small":          "a one-shot artefact earns none",
	} {
		if !strings.Contains(compilerSystemPrompt, want) {
			t.Errorf("the compiler prompt no longer states %s: %q missing", name, want)
		}
	}
	// The rule is about the shape of an acceptance criterion, so it must name no
	// kind of thing that could be built.
	for _, forbidden := range []string{"browser", "website", "dashboard", "spreadsheet"} {
		if strings.Contains(strings.ToLower(compilerSystemPrompt), strings.ToLower(forbidden)) {
			t.Errorf("the compiler prompt grew a domain specific: %q", forbidden)
		}
	}
}

// One value, said once in each prompt that can act on it: work about something
// already in flight changes it, and work that genuinely is separate follows it
// rather than racing it.
//
// It used to be said in three registers because there were three prompts, and
// each said it in a vocabulary the others did not have — the router in
// amend-and-splice terms, the belt in revise-and-steer terms, the compiler in
// the only term that becomes an edge. Two of those were one brain pretending to
// be two, and they are one prompt now. The value did not move; the number of
// places it has to be restated did, and every restatement is a place it can
// drift.
func TestEveryReadingIsToldNotToRaceWorkAlreadyUnderway(t *testing.T) {
	for name, pinned := range map[string]struct {
		prompt  string
		phrases []string
	}{
		"orchestrator": {orchestratorPrompt, []string{
			"A follow-up about work in flight is a change to that work before it is a second job",
		}},
		"compiler": {compilerSystemPrompt, []string{
			"A job still running is earlier work too",
			"name that job in builds_on so this work follows it",
		}},
	} {
		for _, phrase := range pinned.phrases {
			if !strings.Contains(pinned.prompt, phrase) {
				t.Errorf("%s prompt omitted %q", name, phrase)
			}
		}
	}
	// And the tool that would race it is told the same thing in its own
	// description, because that is the string a tool-calling model reads at the
	// moment it decides between "another job" and "a change to that one".
	spawnDescription := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolTask {
			spawnDescription = definition.Function.Description
		}
	}
	if !strings.Contains(spawnDescription, "after names work this follows on from") {
		t.Errorf("task no longer offers the follows-on edge at all: %q", spawnDescription)
	}
}

// A craft's money stop is answered by writing the choice down where the run
// itself will read it. The run owns the decision — it is the only thing that
// knows what it has spent — so the head's whole job is to record the option
// verbatim and say something true, never the bare "got it" that used to
// acknowledge a decision nothing acted on.
func TestHeadRecordsCraftBudgetConsentWhereTheRunWillReadIt(t *testing.T) {
	for _, probe := range []struct {
		reply string
		value string
		want  string
	}{
		{"1", "craft:continue:craft-presentation-1", "Keeping it going."},
		{"2", "craft:stop:craft-presentation-1", "Okay — it'll deliver what already landed."},
		{"yes", "craft:continue:craft-presentation-1", "Keeping it going."},
	} {
		graph := openHeadStore(t)
		options := []store.QuestionOption{
			{Label: "keep going", Value: "craft:continue:craft-presentation-1"},
			{Label: "deliver what landed", Value: "craft:stop:craft-presentation-1"},
		}
		question, err := graph.AskQuestion(store.AgentQuestion{
			SessionID: "craft-money", Urgency: store.QuestionBlocking, Options: options,
			Text: "Craft budget reached -- the presentation craft has spent $1.75 of its $1.50 bound.",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
			t.Fatal(err)
		}
		user, err := graph.PostMessage(store.Message{
			SessionID: "craft-money", Role: store.RoleUser, Body: probe.reply, QuestionSeq: question.Seq,
		})
		if err != nil {
			t.Fatal(err)
		}
		client := &fakeClient{}
		if err := New(client, graph).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		if calls := client.callCount(); calls != 0 {
			t.Fatalf("%q was routed to a model: calls=%d", probe.reply, calls)
		}
		resolved, found, err := graph.AgentQuestionBySeq(question.Seq)
		if err != nil || !found {
			t.Fatalf("read the question: found=%t err=%v", found, err)
		}
		if resolved.Status != store.QuestionAnswered || resolved.Resolution != probe.value {
			t.Fatalf("%q resolved as %q (%s)", probe.reply, resolved.Resolution, resolved.Status)
		}
		messages, err := graph.Messages("craft-money", user.Seq, 0)
		if err != nil || len(messages) != 1 || messages[0].Body != probe.want {
			t.Fatalf("%q acknowledgement = %+v err=%v", probe.reply, messages, err)
		}
	}
}

// An answer continues the ask it answers, and what the user attached to that
// ask is part of it. A continuation that drops the files is a job that never
// sees the PDF the question was about.
func TestAnsweringACompilerQuestionCarriesTheOriginalAttachments(t *testing.T) {
	for _, durable := range []bool{true, false} {
		graph := openHeadStore(t)
		source, err := graph.RequestCommand(store.Command{
			SessionID: "attached", Kind: store.CommandSplice,
			Instruction: "summarize this", Attachments: []string{"/tmp/report.pdf"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if durable {
			question, err := graph.AskQuestion(store.AgentQuestion{
				SessionID: "attached", Text: "How long should the summary be?",
				Urgency: store.QuestionWhenever, OriginCommandSeq: source.Seq,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := graph.SurfaceQuestion(question.Seq); err != nil {
				t.Fatal(err)
			}
		} else if _, err := graph.PostMessage(store.Message{
			SessionID: "attached", Role: store.RoleAgent, Body: "How long should the summary be?",
			CommandSeq: source.Seq,
			Options:    []store.QuestionOption{{Label: "a page"}, {Label: "a paragraph"}},
		}); err != nil {
			t.Fatal(err)
		}
		user, err := graph.PostMessage(store.Message{
			SessionID: "attached", Role: store.RoleUser, Body: "a paragraph",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
			t.Fatal(err)
		}
		commands, err := graph.PendingCommands(0)
		if err != nil {
			t.Fatal(err)
		}
		if len(commands) != 2 {
			t.Fatalf("durable=%t: pending commands = %+v", durable, commands)
		}
		continuation := commands[1]
		if len(continuation.Attachments) != 1 || continuation.Attachments[0] != "/tmp/report.pdf" {
			t.Fatalf("durable=%t: the continuation lost the attachment: %+v", durable, continuation)
		}
	}
}
