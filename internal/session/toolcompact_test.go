package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const toolCompactResultBytes = 4000

func toolCompactOutput(round int) string {
	// THE MARKER IS AT THE END, which is where [checkpointResultTail] looks
	// and where a real tool's verdict lives. A marker at the head would be
	// the first thing the digest cut away.
	return strings.Repeat("x", toolCompactResultBytes) + fmt.Sprintf("\nROUND-%02d-UNIQUE-TAIL", round)
}

func toolCompactMessages(rounds, newestSiblings int) []ai.Message {
	messages := []ai.Message{
		textMessage("system", "SYSTEM-PROMPT-MUST-NOT-MOVE"),
		textMessage("user", "do the work"),
	}
	if newestSiblings < 1 {
		newestSiblings = 1
	}
	for round := 0; round < rounds; round++ {
		call := fmt.Sprintf("call-%d", round)
		messages = append(messages,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: call, Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{
				Type: "text", Text: toolCompactOutput(round),
			}}},
		)
	}
	for extra := 1; extra < newestSiblings; extra++ {
		call := fmt.Sprintf("call-new-%d", extra)
		messages = append(messages, ai.Message{
			Role:       "tool",
			ToolCallID: call,
			Content:    []ai.ContentPart{{Type: "text", Text: toolCompactOutput(100 + extra)}},
		})
	}
	return messages
}

func toolTextsOf(messages []ai.Message) []string {
	var texts []string
	for _, message := range messages {
		if message.Role == "tool" {
			texts = append(texts, messageContentText(message))
		}
	}
	return texts
}

func toolResultPayloadBytes(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		if message.Role == "tool" {
			total += messageBytes(message)
		}
	}
	return total
}

func TestCompactToolHistoryKeepsNewestVerbatimAndSystemUntouched(t *testing.T) {
	original := toolCompactMessages(6, 1)
	got := compactToolHistory(original, len(original))

	if messageContentText(got[0]) != messageContentText(original[0]) {
		t.Fatalf("system prompt moved: %q", messageContentText(got[0]))
	}
	texts := toolTextsOf(got)
	if len(texts) != 6 {
		t.Fatalf("dropped a tool result: got %d, want 6", len(texts))
	}
	if texts[5] != toolCompactOutput(5) {
		t.Fatalf("newest result was compacted: %.80q", texts[5])
	}
	for index := 0; index < 5; index++ {
		if texts[index] == toolCompactOutput(index) {
			t.Fatalf("old result %d stayed verbatim", index)
		}
		if !strings.Contains(texts[index], fmt.Sprintf("ROUND-%02d-UNIQUE-TAIL", index)) {
			t.Fatalf("old result %d lost its tail: %.80q", index, texts[index])
		}
		if len(texts[index]) > checkpointResultBytes+len("…") {
			t.Fatalf("old result %d is %d bytes, want a digest tail", index, len(texts[index]))
		}
	}
	for _, message := range original {
		if message.Role != "tool" {
			continue
		}
		if !strings.Contains(messageContentText(message), strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatal("compactToolHistory mutated an input result")
		}
	}
}

func TestCompactToolHistoryKeepsAParallelNewestBatchVerbatim(t *testing.T) {
	original := toolCompactMessages(4, 3)
	got := compactToolHistory(original, len(original))
	texts := toolTextsOf(got)
	if len(texts) != 6 {
		t.Fatalf("tool results = %d, want 6", len(texts))
	}
	for _, text := range texts[len(texts)-3:] {
		if !strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("a newest-batch sibling was compacted: %.80q", text)
		}
	}
	for _, text := range texts[:len(texts)-3] {
		if strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("an older result stayed verbatim: %.80q", text)
		}
	}
}

func TestCompactToolHistoryBoundsOldResultsToTheDigestBudget(t *testing.T) {
	// Enough long results that tails alone would blow the 5k-token account.
	// The second pass must shrink the far end so consumed evidence stays inside
	// the same budget the checkpoint reader already proved.
	const rounds = 80
	original := toolCompactMessages(rounds, 1)
	got := compactToolHistory(original, len(original))
	texts := toolTextsOf(got)
	if texts[len(texts)-1] != toolCompactOutput(rounds-1) {
		t.Fatal("newest result was compacted to make the budget")
	}
	var old []int
	cut := newestToolBatchStart(got)
	for index, message := range got {
		if message.Role == "tool" && index < cut {
			old = append(old, index)
		}
	}
	if spent := toolResultBytes(got, old); spent > checkpointDigestBytes {
		t.Fatalf("old tool results weigh %d bytes, want at most the digest budget %d", spent, checkpointDigestBytes)
	}
}

func TestALongFrozenToolHistoryCompactsToSubLinearPrompt(t *testing.T) {
	const rounds = 16
	steps := make([]step, 0, rounds+2)
	for round := 0; round < rounds; round++ {
		round := round
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			// Unique arguments and visible assistant text keep the loop
			// detector and the silent-streak watch from ending the turn
			// before the history has had a chance to grow.
			return toolResponseWithText(
				fmt.Sprintf("call-%d", round),
				"read",
				fmt.Sprintf(`{"round":%d}`, round),
				fmt.Sprintf("working round %d", round),
			), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("still done"), nil
	})
	completer := &scriptedCompleter{
		steps: steps,
		aside: func(messages []ai.Message) (*ai.Response, bool) {
			// THE TURN'S SYSTEM PROMPT IS THE LITERAL "SYSTEM". Every errand
			// beside it — title, memory, route judge, checkpoint reader — has
			// a page of its own, and answering those here keeps them from
			// stealing a scripted tool round.
			if len(messages) == 0 || messageContentText(messages[0]) != "SYSTEM" {
				return textResponse("aside"), true
			}
			return nil, false
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = bigTestWindow
		config.CompactEnabled = false
		config.TaskAudit = false
	})
	issued := 0
	for index := range agent.tools {
		if agent.tools[index].Name != "read" {
			continue
		}
		tool := staticTool("read", "")
		tool.Execute = func(context.Context, json.RawMessage) (string, bool, error) {
			text := toolCompactOutput(issued)
			issued++
			return text, false, nil
		}
		agent.tools[index] = tool
		break
	}

	events, err := agent.Submit(context.Background(), "read sixteen times")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	if completer.requests() < rounds {
		t.Fatalf("turn made %d requests, want at least %d tool rounds", completer.requests(), rounds)
	}
	events, err = agent.Submit(context.Background(), "recap that work")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	lastReq := completer.request(completer.requests() - 1)
	if messageContentText(lastReq[0]) != "SYSTEM" {
		t.Fatalf("system prompt was rewritten: %q", messageContentText(lastReq[0]))
	}
	sent := toolTextsOf(lastReq)
	if len(sent) < rounds {
		t.Fatalf("request lost tool results: got %d, want %d", len(sent), rounds)
	}
	if !strings.Contains(sent[len(sent)-1], strings.Repeat("x", toolCompactResultBytes)) {
		t.Fatalf("newest sent result was not verbatim: %.80q", sent[len(sent)-1])
	}
	oldBytes := 0
	for _, text := range sent[:len(sent)-1] {
		if strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("an old sent result stayed verbatim: %.80q", text)
		}
		oldBytes += len(text)
	}
	if oldBytes > checkpointDigestBytes {
		t.Fatalf("old sent results weigh %d bytes, want at most the digest budget %d", oldBytes, checkpointDigestBytes)
	}

	// RAW GROWTH IS LINEAR IN THE RESULT BODY. Sixteen full results would put
	// tens of kilobytes of tool payload on the next turn's request; the
	// compacted frozen view must stay a small fraction of that.
	raw := 0
	for round := 0; round < rounds; round++ {
		raw += len(toolCompactOutput(round))
	}
	got := toolResultPayloadBytes(lastReq)
	if got*2 > raw {
		t.Fatalf("last request tool payload %d bytes is not sub-linear against raw %d", got, raw)
	}

	// THE JOURNAL AND THE LIVE TRANSCRIPT KEEP THE BYTES. A view that shrank
	// the record would fail the stub.go law this pass is written on.
	live := toolTexts(agent)
	if len(live) < rounds {
		t.Fatalf("live transcript lost results: %d", len(live))
	}
	for index, text := range live {
		if !strings.Contains(text, strings.Repeat("x", toolCompactResultBytes)) {
			t.Fatalf("live result %d was rewritten: %.80q", index, text)
		}
	}
}

func TestLiveCompactToolHistoryKeepsNewestReadable(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("OPENROUTER_API_KEY missing")
	}
	model := strings.TrimSpace(os.Getenv("AFORGE_LIVE_MODEL"))
	if model == "" {
		model = "deepseek/deepseek-v4-flash-0731"
	}
	client, err := provider.NewClient(provider.Config{
		APIKey:  key,
		BaseURL: "https://openrouter.ai/api/v1",
		Model:   model,
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := toolCompactMessages(5, 1)
	history := compactToolHistory(messages, len(messages))
	history = append(history, textMessage("user",
		"Reply with only the last line of the newest tool result, nothing else."))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	response, err := client.CompleteWithMessages(ctx, history,
		ai.WithModel(model), ai.WithMaxTokens(64))
	if err != nil {
		t.Fatalf("live compacted history was refused: %v", err)
	}
	if response == nil || len(response.Choices) == 0 {
		t.Fatal("live call returned no choice")
	}
	got := messageContentText(response.Choices[0].Message)
	if !strings.Contains(got, "ROUND-04-UNIQUE-TAIL") {
		t.Fatalf("live model did not see the newest verbatim tail: %q", got)
	}
}
