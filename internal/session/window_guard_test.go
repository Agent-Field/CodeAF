package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestAnAbsurdClaimedWindowStillCompacts is the whole of the 386k incident,
// written down.
//
// The catalog row for ~deepseek/deepseek-v4-flash-latest claims 1,310,720
// tokens. Before the ceiling existed the threshold followed that claim to
// 1,114,112, so a session grew to 386,309 tokens with the automatic pass
// checking after every single step and never once firing — and the answer that
// came back at that size was the model's own template turned inside out rather
// than an error the loop could see.
//
// The claim is the thing that was wrong, so the claim is what is clamped.
func TestAnAbsurdClaimedWindowStillCompacts(t *testing.T) {
	// The exact figure the row published, and the exact size the session
	// reached. Neither is a round number because neither was invented here.
	const claimed = 1_310_720
	const reached = 386_309

	if got := TrustedWindow(claimed); got != maxTrustedWindow {
		t.Fatalf("TrustedWindow(%d) = %d, want the ceiling %d", claimed, got, maxTrustedWindow)
	}
	threshold := CompactThreshold(claimed)
	if threshold >= reached {
		t.Fatalf("threshold on the claimed window is %d; a transcript of %d tokens would still sail past it",
			threshold, reached)
	}
	if threshold != CompactThreshold(maxTrustedWindow) {
		t.Fatalf("a claimed %d gives threshold %d but the ceiling itself gives %d — the clamp is not the whole answer",
			claimed, threshold, CompactThreshold(maxTrustedWindow))
	}

	// A window that fits under the ceiling is untouched: this is a guard
	// against an absurd claim, not a new policy for models with real room.
	for _, window := range []int{128_000, 200_000, maxTrustedWindow} {
		if got := TrustedWindow(window); got != window {
			t.Fatalf("TrustedWindow(%d) = %d, want it left alone", window, got)
		}
	}

	// An unknown window is still unknown — zero is "nobody said", and a
	// threshold against it means nothing (the emptiness law).
	if got := TrustedWindow(0); got != 0 {
		t.Fatalf("TrustedWindow(0) = %d, want 0", got)
	}
	if got := CompactThreshold(0); got != 0 {
		t.Fatalf("CompactThreshold(0) = %d, want 0", got)
	}

	// And the agent's own threshold follows, whichever road the claim came in
	// by: the configured window at construction, or a mid-session /model switch.
	configured := &Agent{config: Config{ContextWindow: claimed}}
	if got := configured.compactThreshold(); got != threshold {
		t.Fatalf("a session configured with the claim thresholds at %d, want %d", got, threshold)
	}
	switched := &Agent{config: Config{ContextWindow: 128_000}}
	switched.SetContextWindow(claimed)
	if got := switched.compactThreshold(); got != threshold {
		t.Fatalf("a session switched onto the claim thresholds at %d, want %d", got, threshold)
	}
	// The claim itself is still reported honestly: the status meter is
	// describing the model, and the model really does say that.
	if got := switched.window(); got != claimed {
		t.Fatalf("window() = %d, want the model's own claim %d", got, claimed)
	}
}

// TestAnOversizeRequestIsNeverSentBlind pins the guard that stands between the
// transcript and the wire.
//
// Automatic compaction is a preference and a person may switch it off. Fitting
// is not. With the automatic pass off, a transcript past the ceiling used to go
// out whole and the loop learned its size from the refusal — or, on an endpoint
// that neither refused nor served it, did not learn at all.
func TestAnOversizeRequestIsNeverSentBlind(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		// The claim that started all this, and the automatic pass switched off
		// the way a person can switch it off.
		config.ContextWindow = 1_310_720
		config.CompactEnabled = false
	})

	// A transcript comfortably past the ceiling: 300k tokens of estimator at
	// four bytes each, which is under the claimed window and over the guard.
	long := strings.Repeat("thinking about the parser again. ", 2_000)
	agent.mu.Lock()
	for range 20 {
		agent.messages = append(agent.messages,
			textMessage("user", "keep going"),
			textMessage("assistant", long))
	}
	before := agent.estimateTokensLocked()
	agent.mu.Unlock()
	if before <= maxTrustedWindow {
		t.Fatalf("the fixture is only %d tokens; it has to exceed the ceiling %d to prove anything",
			before, maxTrustedWindow)
	}

	collect(t, mustSubmit(t, agent, "and now?"))

	completer.mu.Lock()
	seen := append([][]ai.Message(nil), completer.seen...)
	completer.mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no request was made at all; the guard must shrink a turn, never swallow it")
	}
	sent := 0
	for _, message := range seen[0] {
		sent += messageBytes(message)
	}
	if sent/bytesPerToken > maxTrustedWindow {
		t.Fatalf("the first request carried ~%d tokens, past the ceiling %d — it went out blind",
			sent/bytesPerToken, maxTrustedWindow)
	}

	// The pass really was the compaction pass and not a truncation somewhere
	// else: the person's own words are all still there.
	agent.mu.Lock()
	kept := 0
	for _, message := range agent.messages {
		if message.Role == "user" && messageText(message) == "keep going" {
			kept++
		}
	}
	agent.mu.Unlock()
	if kept != 20 {
		t.Fatalf("%d of the person's 20 messages survived the guard, want all of them", kept)
	}
}

// TestTheOversizeGuardLeavesAnOrdinaryTurnAlone is the other half: the guard is
// a floor under a pathological case and must be invisible everywhere else. A
// short conversation on a huge-claim model makes exactly the requests it always
// did, and nothing is compacted for it.
func TestTheOversizeGuardLeavesAnOrdinaryTurnAlone(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ContextWindow = 1_310_720
		config.CompactEnabled = true
	})

	for _, event := range collect(t, mustSubmit(t, agent, "hello")) {
		if event.Kind == EventCompacted {
			t.Fatalf("a two-message conversation was compacted: %q", event.Hint)
		}
	}
	agent.guardOversizeRequest(context.Background(), nil)
}
