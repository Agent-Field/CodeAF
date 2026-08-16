package exec

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// keyedCompleter is scriptedCompleter plus the one thing a routing key can be
// checked with: what the context said at the moment of the call. The key is
// carried, not passed, so nothing below the adapter would otherwise see it.
type keyedCompleter struct {
	scriptedCompleter
	keys []string
}

func (k *keyedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	k.keys = append(k.keys, provider.CacheKeyFrom(ctx))
	return k.scriptedCompleter.CompleteWithMessages(ctx, messages, options...)
}

// TestTwoConsecutiveTurnsOfALeafSendAByteIdenticalPrefix is the freeze
// regression, and it is the property everything else in this discipline rests
// on. A cache breakpoint marks a position; an affinity key picks a replica.
// Neither is worth anything if the bytes before the marker moved, because a
// prefix cache is keyed on those bytes and one changed character invalidates
// every token after it.
//
// The transcript is append-only by construction — see the message table in
// Run — but "by construction" is exactly the kind of claim that survives a
// refactor in prose and dies in the code. So this asserts the encoded bytes
// rather than the intent: turn N's whole request must reappear, unchanged and
// in order, at the head of turn N+1's.
func TestTwoConsecutiveTurnsOfALeafSendAByteIdenticalPrefix(t *testing.T) {
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "sh", `{"cmd":"echo one"}`)},
		{call("c2", "sh", `{"cmd":"echo two"}`)},
	}}
	linear := NewLinear(client, workspace(t), nil, 6, 150_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{
		NodeID: 1, Goal: "ship the release", Brief: "check the upgrade path",
	}); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) < 3 {
		t.Fatalf("the leaf made %d calls, want at least three turns to compare", len(client.seen))
	}
	for turn := 1; turn < len(client.seen); turn++ {
		previous, current := encodedMessages(t, client.seen[turn-1]), encodedMessages(t, client.seen[turn])
		if len(current) < len(previous) {
			t.Fatalf("turn %d sent %d messages, fewer than turn %d's %d: the transcript shrank",
				turn, len(current), turn-1, len(previous))
		}
		for index := range previous {
			if previous[index] != current[index] {
				t.Fatalf("turn %d rewrote message %d, so every token after it is re-billed cold\n"+
					"was:  %s\nnow:  %s", turn, index, previous[index], current[index])
			}
		}
	}
}

// encodedMessages is the comparison the provider actually makes: the wire bytes
// of each message, not the Go value. Two transcripts that differ only in a field
// the encoder drops are the same prefix, and two that agree in Go but encode
// differently are not.
func encodedMessages(t *testing.T, messages []ai.Message) []string {
	t.Helper()
	encoded := make([]string, len(messages))
	for index, message := range messages {
		raw, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		encoded[index] = string(raw)
	}
	return encoded
}

// TestALeafHoldsOneAffinityKeyAndSiblingsHoldDifferentOnes is the routing half,
// asserted where it is actually applied rather than where it is defined.
//
// Six leaves of one fan-out used to run under one key, which asks a router to
// send six unrelated and separately growing transcripts to one replica. That is
// the read of the miss line the ledgers showed: a third of the re-sent tokens
// paying full price against a cache a stable prompt should have hit.
func TestALeafHoldsOneAffinityKeyAndSiblingsHoldDifferentOnes(t *testing.T) {
	run := provider.RunCacheKey("ship the release", "sim/model")
	ctx := provider.WithCacheKey(context.Background(), run)

	leafKeys := func(node int, key string) []string {
		client := &keyedCompleter{scriptedCompleter: scriptedCompleter{turns: [][]ai.ToolCall{
			{call("c1", "sh", `{"cmd":"echo one"}`)},
			{call("c2", "sh", `{"cmd":"echo two"}`)},
		}}}
		linear := NewLinear(client, workspace(t), nil, 6, 150_000, time.Minute)
		if _, err := linear.Run(ctx, Task{NodeID: node, NodeKey: key, Brief: "work"}); err != nil {
			t.Fatal(err)
		}
		return client.keys
	}

	first := leafKeys(1, "task-1-n1")
	second := leafKeys(2, "task-1-n2")
	if len(first) < 3 || len(second) < 3 {
		t.Fatalf("leaves made %d and %d calls, want several turns each", len(first), len(second))
	}
	// One leaf, one destination, every turn.
	for turn, key := range first {
		if key != first[0] {
			t.Fatalf("turn %d of one leaf asked for a different replica: %q, want %q", turn, key, first[0])
		}
	}
	if first[0] == second[0] {
		t.Fatalf("two leaves of one run share the affinity key %q", first[0])
	}
	// And neither is the bare run key any more, which is what let them collide.
	if first[0] == run || second[0] == run {
		t.Fatalf("a leaf still runs under the bare run key %q", run)
	}
	if first[0] != provider.LeafCacheKey(run, "task-1-n1") {
		t.Fatalf("leaf key %q is not derived from the run key and the leaf's own name", first[0])
	}
}

// TestTheHitRatioIsWrittenDownForEveryTurn: the discipline is unfalsifiable
// without a number a benchmark can read, and the absolute counts beside it are
// not that number — they move with the transcript's size, so they cannot be
// compared between turns or between runs.
func TestTheHitRatioIsWrittenDownForEveryTurn(t *testing.T) {
	tests := []struct {
		name           string
		cached, prompt int
		want           int
	}{
		{name: "cold first turn", cached: 0, prompt: 12_000, want: 0},
		{name: "warm loop", cached: 47_000, prompt: 50_000, want: 94},
		{name: "nothing sent", cached: 0, prompt: 0, want: 0},
		// A provider reporting more reads than it was sent is reporting
		// something this arithmetic cannot use; over 100% would read as a
		// defect in the discipline rather than in the report.
		{name: "impossible report", cached: 60_000, prompt: 50_000, want: 100},
		{name: "negative report", cached: -1, prompt: 50_000, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hitPercent(test.cached, test.prompt); got != test.want {
				t.Fatalf("hitPercent(%d, %d) = %d, want %d", test.cached, test.prompt, got, test.want)
			}
		})
	}
}
