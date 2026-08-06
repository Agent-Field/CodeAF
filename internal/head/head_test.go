package head

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type fakeClient struct {
	mutex     sync.Mutex
	responses []string
	calls     int
}

func (client *fakeClient) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.calls++
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
	if _, err := graphStore.RecordFact("", "file:internal/resident/notebook.go", store.FactQuirk,
		"scope-only memory with unrelated vocabulary"); err != nil {
		t.Fatalf("record fact: %v", err)
	}
	rendered := renderNotebook(graphStore, "Please inspect internal/resident/notebook.go")
	if !strings.Contains(rendered, "scope-only memory with unrelated vocabulary") {
		t.Fatalf("scope-exact notebook fact did not reach head: %q", rendered)
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
