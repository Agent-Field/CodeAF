package session

// How far a compaction pass folds, as distinct from when one fires.
//
// The two used to be the same line, and the consequence was a pass after every
// step: fold the fewest batches that dip under the threshold, grow one step,
// cross it again, repeat — fifteen passes in six minutes on one live task, and a
// prompt cache thrown away with each. These pin the headroom that ends it.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// grownTranscript appends n exchanges whose assistant halves each weigh about
// replyBytes, so a test can push the estimate past the threshold by a known
// margin and leave plenty of foldable history above the verbatim tail.
func grownTranscript(agent *Agent, n, replyBytes int) {
	reply := strings.Repeat("reading the parser. ", replyBytes/20)
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for exchange := 1; exchange <= n; exchange++ {
		agent.messages = append(agent.messages,
			textMessage("user", fmt.Sprintf("question %d", exchange)),
			textMessage("assistant", reply))
	}
}

// growBy appends one assistant message of about the given token count — one
// step's worth of new work landing after a pass.
func growBy(agent *Agent, tokens int) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.messages = append(agent.messages,
		textMessage("assistant", strings.Repeat("step. ", tokens*bytesPerToken/6)))
}

func estimate(agent *Agent) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return agent.estimateTokensLocked()
}

func messageCount(agent *Agent) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return len(agent.messages)
}

// ONE PASS LANDS A WHOLE HEADROOM UNDER THE THRESHOLD, not just under it. With
// ample foldable history the fold keeps going past the trigger line and stops
// at the target, and what it leaves is still more than the verbatim tail.
func TestAPassFoldsAWholeHeadroomBelowTheThreshold(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 128_000
	})
	threshold, target := agent.compactThreshold(), agent.compactTargetTokens()
	headroom := threshold - target
	if headroom <= 0 {
		t.Fatalf("threshold %d, target %d: no headroom at all", threshold, target)
	}
	// Seventy replies of ~2,000 tokens is ~140k, well over the 108.8k trigger,
	// and the ~20k tail keeps only the last ten of them.
	grownTranscript(agent, 70, 8_000)
	if before := estimate(agent); before <= threshold {
		t.Fatalf("fixture estimate %d is not over the threshold %d", before, threshold)
	}

	changed, err := agent.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass that folded", changed, err)
	}
	after := estimate(agent)
	if after > threshold-headroom {
		t.Fatalf("estimate after one pass = %d, want at most %d (threshold %d − headroom %d), not merely under the threshold",
			after, threshold-headroom, threshold, headroom)
	}
	if after <= agent.keepRecentTokens() {
		t.Fatalf("estimate after one pass = %d, under the %d-token tail: the pass folded more than it should",
			after, agent.keepRecentTokens())
	}
}

// THE THRASH ITSELF. After a pass, one step's growth must not start the next
// one; only growth that uses up the headroom does. This is the regression that
// compacted fifteen times in six minutes, four to six messages a pass, with the
// estimate never dropping and the provider's prompt cache dying every step.
func TestOneStepOfGrowthAfterAPassDoesNotCompactAgain(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 128_000
		config.CompactEnabled = true
	})
	threshold := agent.compactThreshold()
	grownTranscript(agent, 70, 8_000)
	if changed, err := agent.compact(context.Background(), nil); !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass that folded", changed, err)
	}
	settled, count := estimate(agent), messageCount(agent)

	// A few thousand tokens is what one step adds — a reply and a tool result.
	// Before the headroom existed this was enough to cross the line again.
	growBy(agent, 3_000)
	agent.maybeCompact(context.Background(), nil)
	if got := messageCount(agent); got != count+1 {
		t.Fatalf("one step of growth after a pass compacted again: %d messages, want %d — the thrash is back",
			got, count+1)
	}

	// Growth past the headroom is a real reason to compact, and it still does.
	growBy(agent, threshold-settled)
	if now := estimate(agent); now <= threshold {
		t.Fatalf("fixture estimate %d did not cross the threshold %d", now, threshold)
	}
	agent.maybeCompact(context.Background(), nil)
	if got := messageCount(agent); got >= count+2 {
		t.Fatalf("growth past the headroom did not compact: %d messages, want fewer than %d", got, count+2)
	}
	if after := estimate(agent); after > threshold {
		t.Fatalf("estimate after the second pass = %d, want under the threshold %d", after, threshold)
	}
}

// The chain CompactThreshold > compactTarget > keepRecent has to hold at every
// window this surface can meet: tiny ones where the 16k floor and the
// half-window clamp collide with the quarter-window tail, the default, real
// 200k models, and the million-token claims that are now believed rather than
// clamped.
func TestTheTargetSitsBetweenTheThresholdAndTheKeptTail(t *testing.T) {
	for _, window := range []int{
		8_192, 16_384, 21_000, 21_800, 32_768, 40_000, 64_000,
		128_000, 200_000, 256_000, 300_000, 1_310_720, 2_000_000,
	} {
		threshold, target, keep := CompactThreshold(window), compactTarget(window), keepRecent(window)
		if !(threshold > target) {
			t.Fatalf("window %d: threshold %d is not above the target %d — a pass would stop on its own trigger",
				window, threshold, target)
		}
		if !(target > keep) {
			t.Fatalf("window %d: target %d is not above the kept tail %d — no pass could reach it",
				window, target, keep)
		}
		// The agent's own readings are the same numbers, whichever way the
		// window arrived.
		agent := &Agent{config: Config{ContextWindow: window}}
		if agent.compactTargetTokens() != target || agent.keepRecentTokens() != keep {
			t.Fatalf("window %d: agent reads target %d / keep %d, the law says %d / %d",
				window, agent.compactTargetTokens(), agent.keepRecentTokens(), target, keep)
		}
	}
	// And an unknown window has no target, exactly as it has no threshold.
	for _, window := range []int{0, -1} {
		if got := compactTarget(window); got != 0 {
			t.Fatalf("compactTarget(%d) = %d, want 0", window, got)
		}
	}
}

// AN 8K WINDOW STILL FOLDS AND STILL STOPS. Down here the reserve is clamped to
// half the window and the tail to a quarter, so the target has to be squeezed
// between them; the area has already produced one forever-loop.
func TestASmallWindowStillFoldsSomethingAndSettles(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 8_192
		config.CompactEnabled = true
	})
	threshold, target := agent.compactThreshold(), agent.compactTargetTokens()
	if target >= threshold || target <= agent.keepRecentTokens() {
		t.Fatalf("8k window: threshold %d, target %d, tail %d", threshold, target, agent.keepRecentTokens())
	}
	// Sixteen replies of ~300 tokens is ~4.9k, over the 4,096 trigger.
	grownTranscript(agent, 16, 1_200)
	before := messageCount(agent)

	changed, err := agent.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass that folded", changed, err)
	}
	if after := messageCount(agent); after >= before {
		t.Fatalf("the pass folded nothing: %d messages before, %d after", before, after)
	}
	if got := estimate(agent); got > target {
		t.Fatalf("estimate after the pass = %d, want at most the target %d", got, target)
	}
	// Settled: with no growth the automatic check has nothing to do, and the
	// transcript is left exactly as the pass left it.
	count := messageCount(agent)
	agent.maybeCompact(context.Background(), nil)
	if got := messageCount(agent); got != count {
		t.Fatalf("a settled 8k session compacted again with no growth: %d messages, want %d", got, count)
	}
	for _, message := range func() []ai.Message {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		return append([]ai.Message(nil), agent.messages...)
	}() {
		if message.Role == "user" && strings.HasPrefix(messageText(message), "question ") {
			return
		}
	}
	t.Fatalf("every question was folded away")
}

// stubbableTranscript is the shape the headroom regression actually needs: a
// long foldable history with tool batches in it, then heavy results old enough
// to stub, then four short turns of tail. How BIG it has to be for stubbing
// alone to land between the two lines is [stubbableFixture]'s question.
func stubbableTranscript(agent *Agent, bulk, bulkBytes, results, resultBytes int) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.messages = append(agent.messages, textMessage("user", "the first question"))
	for index := 0; index < bulk; index++ {
		// A batch every tenth message, so the fold has real tool pairs to walk
		// over rather than a wall of prose.
		if index%10 == 9 {
			call := fmt.Sprintf("old-call-%d", index)
			agent.messages = append(agent.messages,
				ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
					ID: call, Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`},
				}}},
				ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{
					Type: "text", Text: fmt.Sprintf("small result %d", index),
				}}})
			continue
		}
		agent.messages = append(agent.messages,
			textMessage("assistant", strings.Repeat("reading the parser. ", bulkBytes/20)))
	}
	for index := 0; index < results; index++ {
		call := fmt.Sprintf("heavy-call-%d", index)
		agent.messages = append(agent.messages,
			ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
				ID: call, Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"big.go"}`},
			}}},
			ai.Message{Role: "tool", ToolCallID: call, Content: []ai.ContentPart{{
				Type: "text", Text: fmt.Sprintf("HEAVY-%02d\n", index) + strings.Repeat("payload line\n", resultBytes/13),
			}}})
	}
	for turn := 2; turn <= 5; turn++ {
		agent.messages = append(agent.messages,
			textMessage("user", fmt.Sprintf("question %d", turn)),
			textMessage("assistant", "short answer"))
	}
	agent.messageReasoning = make([]provider.MessageReasoning, len(agent.messages))
}

// liveTranscript is this agent's messages, copied under its lock. The name is
// not transcriptOf: admission_test.go builds one from person turns, and two
// helpers with one name in a package is a collision the merge found.
func liveTranscript(agent *Agent) []ai.Message {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return append([]ai.Message(nil), agent.messages...)
}

// A REQUEST THAT PAIRS. Every result answers a call that is still in the window,
// and every call still has its result: the shape a provider rejects with a 400
// when a pass takes one half of it.
func assertPaired(t *testing.T, messages []ai.Message) {
	t.Helper()
	calls := map[string]bool{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			calls[call.ID] = false
		}
	}
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		answered, ok := calls[message.ToolCallID]
		if !ok {
			t.Fatalf("result for %q has no call in the window", message.ToolCallID)
		}
		if answered {
			t.Fatalf("call %q was answered twice", message.ToolCallID)
		}
		calls[message.ToolCallID] = true
	}
	for id, answered := range calls {
		if !answered {
			t.Fatalf("call %q lost its result", id)
		}
	}
}

// stubbableFixture builds the regression's transcript and returns what stubbing
// alone would leave. THE SIZE IS SEARCHED FOR RATHER THAN WRITTEN DOWN: the
// window's threshold and target are derived numbers, and a fixture pinned to
// today's arithmetic would stop testing the regression the day one of them
// moves. The search grows the history until stubbing alone lands between the
// two lines — the exact gap where a pass used to stop.
func stubbableFixture(t *testing.T, agent *Agent, threshold, target int) int {
	t.Helper()
	for bulk := 60; bulk <= 400; bulk += 2 {
		agent.mu.Lock()
		agent.messages = agent.messages[:1]
		agent.messageReasoning = agent.messageReasoning[:1]
		agent.mu.Unlock()
		stubbableTranscript(agent, bulk, 4_200, 10, 2_000)
		agent.mu.Lock()
		_, reclaim := stubCandidates(agent.messages, stubCut(agent.messages))
		before := agent.estimateTokensLocked()
		agent.mu.Unlock()
		stubbedOnly := before - reclaim/bytesPerToken
		if before > threshold && stubbedOnly <= threshold && stubbedOnly > target {
			return stubbedOnly
		}
	}
	t.Fatalf("no history size stubs to between the target %d and the threshold %d", target, threshold)
	return 0
}

// STUBBING IS NOT HEADROOM. The stub pass alone routinely lands the estimate
// just under the trigger — under the line that fired the pass, and nowhere near
// the target — and the fold was gated on the trigger, so it did not run. The
// next step crossed the line again and the session was back in the once-a-step
// thrash the target exists to end.
func TestAPassThatOnlyStubbedStillFoldsToTheTarget(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 128_000
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	threshold, target := agent.compactThreshold(), agent.compactTargetTokens()
	stubbedOnly := stubbableFixture(t, agent, threshold, target)

	changed, err := agent.compact(context.Background(), nil)
	if !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass", changed, err)
	}
	if after := estimate(agent); after > target {
		t.Fatalf("estimate after the pass = %d, want at most the target %d; stubbing alone would have left %d, under the threshold %d and above the target",
			after, target, stubbedOnly, threshold)
	}
	messages := liveTranscript(agent)
	stubbed := false
	for _, message := range messages {
		if message.Role == "tool" && strings.HasPrefix(strings.TrimSpace(messageContentText(message)), stubMarker) {
			stubbed = true
		}
	}
	if !stubbed {
		t.Fatal("no result was stubbed, so the pass under test was not the one that stubs and folds")
	}
	// The person's own words survive the extra folding, and so does the protocol.
	for turn := 2; turn <= 5; turn++ {
		if !holdsText(messages, fmt.Sprintf("question %d", turn)) {
			t.Fatalf("question %d was folded away: %v", turn, rolesOf(messages))
		}
	}
	assertPaired(t, messages)
}

// AND THE RUNNING TURN IS STILL NOT HISTORY. Folding to the target rather than
// to the trigger folds MORE, which is exactly when a pass would start eating the
// work in hand if it were allowed to.
func TestFoldingToTheTargetStillLeavesTheRunningTurnAlone(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 128_000
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	stubbableFixture(t, agent, agent.compactThreshold(), agent.compactTargetTokens())

	agent.mu.Lock()
	agent.running = true
	agent.turnFloor = len(agent.messages)
	agent.messages = append(agent.messages,
		textMessage("user", "the correction I just typed"),
		ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: "now", Function: ai.ToolCallFunction{
				Name: "edit", Arguments: `{"path":"main.go","old_text":"old","new_text":"new"}`,
			},
		}}},
		ai.Message{Role: "tool", ToolCallID: "now",
			Content: []ai.ContentPart{{Type: "text", Text: "edited main.go"}}},
		ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
			ID: "next", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"main.go"}`},
		}}},
		ai.Message{Role: "tool", ToolCallID: "next",
			Content: []ai.ContentPart{{Type: "text", Text: "package main"}}},
	)
	agent.messageReasoning = make([]provider.MessageReasoning, len(agent.messages))
	agent.mu.Unlock()

	if changed, err := agent.compact(context.Background(), nil); !changed || err != nil {
		t.Fatalf("compact = %v, %v; want a pass", changed, err)
	}
	messages := liveTranscript(agent)
	if !holdsText(messages, "the correction I just typed") {
		t.Fatalf("the person's correction was folded: %v", rolesOf(messages))
	}
	for _, id := range []string{"now", "next"} {
		call, result := false, false
		for _, message := range messages {
			for _, made := range message.ToolCalls {
				call = call || made.ID == id
			}
			result = result || message.ToolCallID == id
		}
		if !call || !result {
			t.Fatalf("running turn's %q lost its call=%v result=%v", id, call, result)
		}
	}
	assertPaired(t, messages)
}
