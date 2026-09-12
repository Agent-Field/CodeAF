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
	steps := make([]step, 0, rounds+3)
	for round := 0; round < rounds; round++ {
		round := round
		// A successful edit is the consumption boundary: everything the model
		// had read before deciding to write has now produced work and may become
		// a recoverable pointer. Repeating the boundary keeps a genuinely long
		// edit/read turn bounded without declaring untouched research disposable.
		if round == 24 || round == 44 {
			steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponseWithText(
					fmt.Sprintf("save-%d", round),
					"write",
					fmt.Sprintf(`{"path":"checkpoint-%d.txt","content":"saved"}`, round),
					fmt.Sprintf("saving checkpoint %d", round),
				), nil
			})
		}
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText(
				fmt.Sprintf("call-%d", round),
				"read",
				fmt.Sprintf(`{"round":%d}`, round),
				fmt.Sprintf("working round %d", round),
			), nil
		})
	}
	return append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
}

func installTurnFoldReader(t *testing.T, agent *Agent, output string) {
	t.Helper()
	for index := range agent.tools {
		if agent.tools[index].Name == "read" {
			agent.tools[index] = staticTool("read", output)
			return
		}
	}
	t.Fatal("test agent has no read tool to replace")
}

func consumedTurnReads(messages []ai.Message, start, end int) map[*ai.ToolCall]bool {
	consumed := make(map[*ai.ToolCall]bool)
	if end > len(messages) {
		end = len(messages)
	}
	for _, message := range messages[start:end] {
		for index := range message.ToolCalls {
			call := &message.ToolCalls[index]
			if earlyTools[call.Function.Name] {
				consumed[call] = true
			}
		}
	}
	return consumed
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
	installTurnFoldReader(t, agent, output)

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
	installTurnFoldReader(t, agent, output)

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
				ID: call, Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{Type: "text", Text: output}}},
		)
	}
	seenThrough := len(agent.messages) - 2
	newest := len(agent.messages) - 1
	consumed := consumedTurnReads(agent.messages, agent.turnFloor, len(agent.messages))
	agent.mu.Unlock()

	agent.foldTurnOutputs(seenThrough, consumed, nil)

	agent.mu.Lock()
	got := messageContentText(agent.messages[newest])
	agent.mu.Unlock()
	if got != output {
		t.Fatalf("unseen result was rewritten: %.80q", got)
	}
}

// Provider context is not the tool working set. A large system prompt or tool
// schema must not trigger a rewrite of a few small observations merely because
// the provider reports more than 64k input tokens.
func TestTurnFoldIgnoresNonObservationContextPressure(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = bigTestWindow
	})

	agent.mu.Lock()
	agent.running = true
	agent.turnFloor = len(agent.messages)
	for round := 0; round < 3; round++ {
		call := fmt.Sprintf("call-%d", round)
		agent.messages = append(agent.messages,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: call, Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{Type: "text", Text: turnFoldOutput()}}},
		)
	}
	horizon := len(agent.messages)
	consumed := consumedTurnReads(agent.messages, agent.turnFloor, horizon)
	agent.contextTokens = turnWorkingSet(agent.window()) + 10_000
	before := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()

	agent.foldTurnOutputs(horizon, consumed, nil)

	agent.mu.Lock()
	after := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	if !reflect.DeepEqual(after, before) {
		t.Fatal("small tool working set was rewritten because unrelated provider context crossed the line")
	}
}

// Reading is not consumption. Until a successful file change proves the model
// used those observations, even an oversized research pass remains verbatim.
func TestTurnFoldKeepsUnactedResearchVerbatim(t *testing.T) {
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
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: call, Function: ai.ToolCallFunction{Name: "read", Arguments: "{}"},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{Type: "text", Text: output}}},
		)
	}
	horizon := len(agent.messages)
	before := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()

	agent.foldTurnOutputs(horizon, nil, nil)

	agent.mu.Lock()
	after := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	if !reflect.DeepEqual(after, before) {
		t.Fatal("research that had not produced work was folded")
	}
}

// appendToolRound writes one call-and-result pair into the agent's live
// transcript, the way the fixtures above spell their rounds out one at a time.
func appendToolRound(agent *Agent, id, name, args, output string) {
	agent.messages = append(agent.messages,
		ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: id, Function: ai.ToolCallFunction{Name: name, Arguments: args},
		}}},
		ai.Message{Role: "tool", ToolCallID: id, Content: []ai.ContentPart{{Type: "text", Text: output}}},
	)
}

// repeatedReadAgent builds a turn whose transcript holds 24 consumed, seen
// tool rounds — about three times the working-set line — ready for the fold.
func repeatedReadAgent(t *testing.T, rounds func(agent *Agent)) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = bigTestWindow
	})
	agent.mu.Lock()
	agent.running = true
	agent.turnFloor = len(agent.messages)
	rounds(agent)
	agent.mu.Unlock()
	return agent
}

func foldWholeTurn(t *testing.T, agent *Agent) []ai.Message {
	t.Helper()
	agent.mu.Lock()
	consumed := consumedTurnReads(agent.messages, agent.turnFloor, len(agent.messages))
	horizon := len(agent.messages)
	agent.mu.Unlock()
	agent.foldTurnOutputs(horizon, consumed, nil)
	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	floor := agent.turnFloor
	agent.mu.Unlock()
	if len(messages) == floor {
		t.Fatal("the fixture built no rounds")
	}
	return messages
}

// THREE OVERLAPPING SLICES OF ONE FILE: the oldest two become pointers naming
// the path and the range each held — a recipe in the sentence family the
// prompt already teaches — while the newest slice, the one the model is still
// working from, stays byte-for-byte whole even though its batch was old enough
// to fold. Distinct reads beside them keep the filed pointer they always got.
func TestTurnFoldKeepsTheNewestSliceOfARepeatedlyReadFile(t *testing.T) {
	output := turnFoldOutput()
	slices := []string{
		`{"path":"checkpoint.go","offset":10,"limit":50}`,
		`{"path":"checkpoint.go","offset":40,"limit":50}`,
		`{"path":"checkpoint.go","offset":80,"limit":50}`,
	}
	agent := repeatedReadAgent(t, func(agent *Agent) {
		for round, args := range slices {
			appendToolRound(agent, fmt.Sprintf("slice-%d", round), "read", args, output)
		}
		for round := 3; round < 24; round++ {
			appendToolRound(agent, fmt.Sprintf("call-%d", round), "read",
				fmt.Sprintf(`{"path":"round-%d.txt"}`, round), output)
		}
	})
	agent.mu.Lock()
	floor := agent.turnFloor
	agent.mu.Unlock()

	messages := foldWholeTurn(t, agent)

	// The results of the three slices sit one after the other from the floor:
	// assistant, result, assistant, result, assistant, result.
	for round, want := range []string{"offset=10 limit=50", "offset=40 limit=50"} {
		text := messageContentText(messages[floor+1+round*2])
		if !strings.HasPrefix(text, stubMarker) {
			t.Fatalf("slice %d was not folded: %.80q", round, text)
		}
		if !strings.Contains(text, "full: read checkpoint.go "+want) {
			t.Fatalf("slice %d pointer = %q, want the read that brings the slice back", round, text)
		}
	}
	if got := messageContentText(messages[floor+5]); got != output {
		t.Fatalf("the newest slice of checkpoint.go was rewritten: %.80q", got)
	}
	// A read of a file named once is none of this rule's business: round 3 was
	// old enough to fold and got the ordinary filed pointer, not a recipe.
	distinct := messageContentText(messages[floor+7])
	if !strings.HasPrefix(distinct, stubMarker) || strings.Contains(distinct, "full: read round-3.txt") {
		t.Fatalf("a singly-read file folded into a recipe: %.80q", distinct)
	}
	if !holdsPrefix(messages, foldMarkerPrefix) {
		t.Fatal("no fold note was written")
	}
	// AND THE ARITHMETIC IS THE OLD ARITHMETIC: the pass stops at the same
	// headroom target with the kept-whole slice counted as buying nothing.
	agent.mu.Lock()
	after := turnToolBytes(agent.messages, floor)
	agent.mu.Unlock()
	if target := turnWorkingTarget(agent.window()) * bytesPerToken; after > target {
		t.Fatalf("working set after the fold = %d bytes, want at most the %d-byte target", after, target)
	}
}

// TWENTY-FOUR DIFFERENT FILES, READ ONCE EACH: no path repeats, so the
// repeated-read rule fires nowhere and every folded result keeps the filed
// pointer the pass has always written.
func TestTurnFoldFilesReadsOfDistinctFilesAsBefore(t *testing.T) {
	output := turnFoldOutput()
	agent := repeatedReadAgent(t, func(agent *Agent) {
		for round := range 24 {
			appendToolRound(agent, fmt.Sprintf("call-%d", round), "read",
				fmt.Sprintf(`{"path":"distinct-%d.txt"}`, round), output)
		}
	})

	messages := foldWholeTurn(t, agent)

	stubs := 0
	for _, message := range messages {
		text := messageContentText(message)
		if message.Role != "tool" || !strings.HasPrefix(text, stubMarker) {
			continue
		}
		stubs++
		if strings.Contains(text, "full: read distinct-") {
			t.Fatalf("a file read once folded into a re-read recipe: %.80q", text)
		}
	}
	if stubs == 0 {
		t.Fatal("a turn over the line folded nothing")
	}
}

// A SINGLE read of a path is not a repeat: it folds under the old rule, with a
// filed copy to point at, and never a recipe naming the file itself.
func TestTurnFoldGivesASingleReadNoRecipe(t *testing.T) {
	output := turnFoldOutput()
	agent := repeatedReadAgent(t, func(agent *Agent) {
		appendToolRound(agent, "solo", "read", `{"path":"solo.txt"}`, output)
		for round := 1; round < 24; round++ {
			appendToolRound(agent, fmt.Sprintf("call-%d", round), "read", `{"path":"daily.txt"}`, output)
		}
	})
	agent.mu.Lock()
	floor := agent.turnFloor
	agent.mu.Unlock()

	messages := foldWholeTurn(t, agent)

	solo := messageContentText(messages[floor+1])
	if !strings.HasPrefix(solo, stubMarker) {
		t.Fatalf("the oldest result was not folded: %.80q", solo)
	}
	if strings.Contains(solo, "full: read solo.txt") {
		t.Fatalf("a file read once points at itself: %.80q", solo)
	}
	if older := messageContentText(messages[floor+3]); !strings.Contains(older, "full: read daily.txt") {
		t.Fatalf("an older slice of a repeated read = %.80q, want the recipe", older)
	}
}

// BASH RESULTS ARE THE OLD RULE'S ALONE: they were never eligible batches in
// this pass, and the repeated-read rule changes nothing about that — a fold
// triggered by the reads beside them leaves every one byte-for-byte.
func TestTurnFoldLeavesBashResultsToTheOlderRules(t *testing.T) {
	output := turnFoldOutput()
	var bashWanted []string
	// Thirty rounds, every fourth a small bash result: the reads carry the
	// working set over the line, the bash results only watch.
	agent := repeatedReadAgent(t, func(agent *Agent) {
		for round := range 30 {
			call := fmt.Sprintf("call-%d", round)
			if round%4 == 3 {
				result := fmt.Sprintf("bash said %d", round)
				bashWanted = append(bashWanted, result)
				appendToolRound(agent, call, "bash", fmt.Sprintf(`{"command":"echo %d"}`, round), result)
				continue
			}
			appendToolRound(agent, call, "read",
				fmt.Sprintf(`{"path":"file-%d.txt"}`, round), output)
		}
	})

	messages := foldWholeTurn(t, agent)

	var bashGot []string
	stubs := 0
	for _, message := range messages {
		text := messageContentText(message)
		if message.Role != "tool" {
			continue
		}
		if strings.HasPrefix(text, "bash said ") {
			bashGot = append(bashGot, text)
		}
		if strings.HasPrefix(text, stubMarker) {
			stubs++
		}
	}
	if !reflect.DeepEqual(bashGot, bashWanted) {
		t.Fatalf("bash results were touched\n got: %v\nwant: %v", bashGot, bashWanted)
	}
	if stubs == 0 {
		t.Fatal("a turn over the line folded nothing")
	}
}

// BELOW THE LINE, REPEATS CHANGE NOTHING. Three slices of one file are a
// repeat the new rule can see, but the working-set trigger is the only trigger
// this pass has: under it the transcript is byte-stable, fold note and all.
func TestTurnFoldLeavesRepeatedReadsBelowTheLine(t *testing.T) {
	output := turnFoldOutput()
	agent := repeatedReadAgent(t, func(agent *Agent) {
		for round, offset := range []int{10, 40, 80} {
			appendToolRound(agent, fmt.Sprintf("slice-%d", round), "read",
				fmt.Sprintf(`{"path":"checkpoint.go","offset":%d,"limit":50}`, offset), output)
		}
	})
	agent.mu.Lock()
	before := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()

	_ = foldWholeTurn(t, agent)

	agent.mu.Lock()
	after := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	if !reflect.DeepEqual(after, before) {
		t.Fatal("repeated reads below the working-set line were folded")
	}
}
