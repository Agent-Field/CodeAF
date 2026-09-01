package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestAClaimedWindowIsBelievedUntilAnEndpointRefusesIt is the 386k incident
// written down a second time, now that the answer to it has changed.
//
// The catalog row for ~deepseek/deepseek-v4-flash-latest claims 1,310,720
// tokens. The threshold followed that claim to 1,114,112, a session grew to
// 386,309 tokens with the automatic pass checking after every step and never
// once firing, and what came back at that size was the model's own template
// turned inside out.
//
// The first answer was a flat ceiling over every claim, and it cost every model
// that was telling the truth. The answer now is that the claim IS believed —
// and that the refusal, when one comes, is believed harder and for good.
func TestAClaimedWindowIsBelievedUntilAnEndpointRefusesIt(t *testing.T) {
	// The exact figure the row published, and the exact size the session
	// reached. Neither is a round number because neither was invented here.
	const claimed = 1_310_720
	const reached = 386_309

	// BELIEVED: nothing has been learned about this model, so its claim stands
	// and the threshold follows it.
	if got := TrustedWindow(claimed); got != claimed {
		t.Fatalf("TrustedWindow(%d) = %d, want the claim believed", claimed, got)
	}
	if threshold := CompactThreshold(claimed); threshold <= reached {
		t.Fatalf("threshold on the claimed window is %d; the run that reached %d would have folded",
			threshold, reached)
	}

	// REFUSED: an endpoint has said that much did not fit, so that is the
	// ceiling from here on — for this session and for every later one.
	if got := trustedWindow(claimed, reached); got != reached {
		t.Fatalf("trustedWindow(%d, refused %d) = %d, want the refusal", claimed, reached, got)
	}
	if got := trustedWindow(claimed, 0); got != claimed {
		t.Fatalf("trustedWindow(%d, nothing learned) = %d, want the claim", claimed, got)
	}
	// A refusal WIDER than the claim teaches nothing: the cap may only narrow.
	if got := trustedWindow(200_000, 900_000); got != 200_000 {
		t.Fatalf("trustedWindow(200k, refused 900k) = %d, want the smaller of the two", got)
	}

	// An unknown window is still unknown — zero is "nobody said", and a
	// threshold against it means nothing (the emptiness law).
	if got := TrustedWindow(0); got != 0 {
		t.Fatalf("TrustedWindow(0) = %d, want 0", got)
	}
	if got := CompactThreshold(0); got != 0 {
		t.Fatalf("CompactThreshold(0) = %d, want 0", got)
	}

	// And the agent's own threshold follows the refusal, whichever road the
	// claim came in by: the configured window at construction, or a /model
	// switch mid-session.
	configured := &Agent{config: Config{ContextWindow: claimed}}
	configured.servedWindow.Store(reached)
	if got := configured.compactThreshold(); got != CompactThresholdFor("", reached) {
		t.Fatalf("a session configured with the claim thresholds at %d, want %d",
			got, CompactThresholdFor("", reached))
	}
	switched := &Agent{config: Config{ContextWindow: 128_000}}
	switched.SetContextWindow(claimed)
	switched.servedWindow.Store(reached)
	if got := switched.compactThreshold(); got != CompactThresholdFor("", reached) {
		t.Fatalf("a session switched onto the claim thresholds at %d, want %d",
			got, CompactThresholdFor("", reached))
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
	// The claim that started all this, capped by what an endpoint has since
	// refused — which is where the guard's bar now comes from.
	const claimed = 1_310_720
	const refused = 256_000
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		// The claim, and the automatic pass switched off the way a person can
		// switch it off.
		config.ContextWindow = claimed
		config.CompactEnabled = false
	})
	agent.servedWindow.Store(refused)

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
	if before <= refused {
		t.Fatalf("the fixture is only %d tokens; it has to exceed the learned ceiling %d to prove anything",
			before, refused)
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
	if sent/bytesPerToken > refused {
		t.Fatalf("the first request carried ~%d tokens, past the ceiling %d — it went out blind",
			sent/bytesPerToken, refused)
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
