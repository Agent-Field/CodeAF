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
	"strings"
	"testing"

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
