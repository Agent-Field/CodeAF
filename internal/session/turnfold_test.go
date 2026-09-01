package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// bigTestWindow is a roomy window for the fold fixtures below. It was once
// spelled maxTrustedWindow, the flat ceiling the compaction law used to clamp
// every claim to; that ceiling is gone (loop.go), and what these tests actually
// wanted from it was "a window big enough that nothing folds by accident".
const bigTestWindow = 256_000

const turnFoldResultTokens = 3_000

func turnFoldOutput() string {
	return "ROUND-OUTPUT\n" + strings.Repeat("x", turnFoldResultTokens*bytesPerToken)
}

func turnFoldScript(rounds int) []step {
	steps := make([]step, 0, rounds+1)
	for round := 0; round < rounds; round++ {
		round := round
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText(
				fmt.Sprintf("call-%d", round),
				"fat",
				fmt.Sprintf(`{"round":%d}`, round),
				fmt.Sprintf("working round %d", round),
			), nil
		})
	}
	return append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
}

// SIXTY SMALL RESULTS STAY BOUNDED INSIDE ONE TURN. Each result is well below
// the size that motivated the old cross-turn stubbing pass, but together they
// would make every later request carry about 180k tokens. The newest 20k-token
// tail may sit above the trigger briefly; nothing beyond that allowance may.
func TestALongTurnsToolWorkingSetStaysBounded(t *testing.T) {
	const rounds = 60
	output := turnFoldOutput()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: turnFoldScript(rounds)}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = bigTestWindow
		config.CompactEnabled = false
		config.SessionFile = journal
	})
	agent.tools = append(agent.tools, staticTool("fat", output))

	events, err := agent.Submit(context.Background(), "run sixty rounds")
	if err != nil {
		t.Fatal(err)
	}
	collected := collect(t, events)
	visibleFold := false
	for _, event := range collected {
		if event.Kind == EventCompacted && strings.HasPrefix(event.Hint, "folded ") {
			visibleFold = true
		}
	}
	if !visibleFold {
		t.Fatalf("the live turn showed no fold line: %v", kinds(collected))
	}

	limit := turnWorkingSet(agent.window()) + agent.keepRecentTokens()
	for request := 0; request < completer.requests(); request++ {
		messages := completer.request(request)
		if got := transcriptBytes(messages) / bytesPerToken; got > limit {
			t.Fatalf("request %d estimate = %d tokens, want at most working-set line plus kept tail %d", request, got, limit)
		}
	}

	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	stub := ""
	markers := 0
	for _, message := range messages {
		text := messageContentText(message)
		if message.Role == "tool" && strings.HasPrefix(text, stubMarker) && stub == "" {
			stub = text
		}
		if strings.HasPrefix(text, foldMarkerPrefix) {
			markers++
		}
	}
	if stub == "" || markers == 0 {
		t.Fatalf("long turn left %d fold notes and first stub %q", markers, stub)
	}
	for _, message := range messages {
		text := messageContentText(message)
		if strings.HasPrefix(text, foldMarkerPrefix) && !isCompactionNote(text) {
			t.Fatalf("fold note is not recognized as a compaction note: %q", text)
		}
	}
	for round := 0; round < rounds; round++ {
		if !holdsText(messages, fmt.Sprintf("working round %d", round)) {
			t.Fatalf("assistant text from round %d was folded", round)
		}
	}

	path := stubPathIn(t, stub)
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(workspace, full)
	}
	bytes, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read folded result through %q: %v", path, err)
	}
	if string(bytes) != output {
		t.Fatalf("stub path holds %d bytes, want the original %d", len(bytes), len(output))
	}
	journaled, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(journaled), strings.Repeat("x", turnFoldResultTokens*bytesPerToken)) {
		t.Fatal("the session journal lost the full tool result")
	}
	liveEarlier := agent.EarlierHistory()

	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM",
		SessionFile: journal, ContextWindow: bigTestWindow,
	}, &refusingCompleter{t: t})
	if err != nil {
		t.Fatalf("resume folded turn: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	resumed.mu.Lock()
	got := append([]ai.Message(nil), resumed.messages...)
	resumed.mu.Unlock()
	if !reflect.DeepEqual(got, messages) {
		t.Fatalf("resumed working set differs from live fold\n got: %v\nwant: %v", textsOf(got), textsOf(messages))
	}
	if resumedEarlier := resumed.EarlierHistory(); !reflect.DeepEqual(resumedEarlier, liveEarlier) {
		t.Fatalf("resumed scroll-back differs from live fold\n got: %+v\nwant: %+v", resumedEarlier, liveEarlier)
	}
}

// A turn below the line is byte-stable: no result is written out, no message is
// rewritten and no fold note appears merely because the hook ran.
func TestTurnFoldDoesNothingBelowTheWorkingSetLine(t *testing.T) {
	output := turnFoldOutput()
	completer := &scriptedCompleter{steps: turnFoldScript(3)}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = bigTestWindow
		config.CompactEnabled = false
	})
	agent.tools = append(agent.tools, staticTool("fat", output))

	events, err := agent.Submit(context.Background(), "three rounds")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	for _, text := range toolTexts(agent) {
		if strings.HasPrefix(text, stubMarker) {
			t.Fatalf("a result below the line was folded: %.80q", text)
		}
	}
	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	if holdsPrefix(messages, foldMarkerPrefix) {
		t.Fatal("a fold note appeared below the working-set line")
	}
}

// The horizon is a hard losslessness boundary. Even above the line, a batch the
// model has not received stays byte-for-byte whole while older complete batches
// may be replaced.
func TestTurnFoldNeverRewritesAnUnseenResult(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = bigTestWindow
	})
	output := turnFoldOutput()

	agent.mu.Lock()
	agent.running = true
	agent.turnFloor = len(agent.messages)
	for round := 0; round < 24; round++ {
		call := fmt.Sprintf("call-%d", round)
		agent.messages = append(agent.messages,
			ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: fmt.Sprintf("assistant %d", round)}}, ToolCalls: []ai.ToolCall{{
				ID: call, Function: ai.ToolCallFunction{Name: "fat", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{Type: "text", Text: output}}},
		)
	}
	seenThrough := len(agent.messages) - 2
	newest := len(agent.messages) - 1
	agent.mu.Unlock()

	agent.foldTurnOutputs(seenThrough, nil)

	agent.mu.Lock()
	got := messageContentText(agent.messages[newest])
	agent.mu.Unlock()
	if got != output {
		t.Fatalf("unseen result was rewritten: %.80q", got)
	}
}
