package exec

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// systemSeenBy runs one leaf and returns the system message it was sent.
func systemSeenBy(t *testing.T, task Task) string {
	t.Helper()
	client := &scriptedCompleter{}
	linear := NewLinear(client, workspace(t), nil, 4, 150_000, time.Minute)
	if _, err := linear.Run(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) == 0 {
		t.Fatal("the leaf never reached a model call")
	}
	return client.seen[0][0].Content[0].Text
}

// The system message is the widest cached prefix the product has, and it is
// only worth anything if two leaves agree on it byte for byte.
//
// Four leaves of one job launch at once, against one model, reading one set of
// invariants — several thousand tokens each, identical in every leaf. Appending
// the planner's per-node contract to that message made it per-node, so no two
// leaves shared a prefix and each of the four wrote the whole thing cold. This
// pins the property the placement exists for: same run, different nodes,
// different contracts, different goals, different inputs — one system message.
func TestEveryLeafOfARunSharesOneSystemMessage(t *testing.T) {
	first := systemSeenBy(t, Task{
		NodeID: 1, Goal: "ship the release", Brief: "write the migration notes",
		Contract: "read the changelog before the source",
	})
	second := systemSeenBy(t, Task{
		NodeID: 2, Goal: "ship the release", Brief: "check the upgrade path",
		Contract: "run the upgrade on a copy first, never on the fixture",
		Inputs:   []Input{{Title: "task-1-n1", Result: "three breaking changes"}},
	})
	if first != second {
		t.Fatalf("two leaves of one run were sent different system messages;"+
			" the shared prefix ends at byte %d of %d", sharedPrefix(first, second), len(first))
	}
	// A leaf with no contract at all must land on that same message too,
	// otherwise a job that mixes contracted and bare leaves splits its cache.
	if bare := systemSeenBy(t, Task{NodeID: 3, Brief: "tidy up"}); bare != first {
		t.Fatalf("a leaf without a contract was sent a different system message;"+
			" the shared prefix ends at byte %d", sharedPrefix(first, bare))
	}
}

// sharedPrefix is how far two assemblies of the same message agree before the
// first byte that moved — everything past it is re-billed at full price.
func sharedPrefix(first, second string) int {
	limit := len(first)
	if len(second) < limit {
		limit = len(second)
	}
	for index := 0; index < limit; index++ {
		if first[index] != second[index] {
			return index
		}
	}
	return limit
}

func toolBlock(t *testing.T, toolbox *Toolbox) string {
	t.Helper()
	encoded, err := json.Marshal(toolbox.Definitions())
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// The tool block rides ahead of the entire transcript, so the first byte of it
// that moves re-bills everything behind it. Arming is allowed to cost one
// invalidation and the design has always said so — what it must never cost is a
// RESHUFFLE, where a definition leaves or enters the middle of the list and
// every schema after it shifts.
//
// The property is therefore strictly stronger than "the block changed": the
// block before arming must be a whole prefix of the block after it.
func TestArmingAppendsToTheToolBlockAndNeverReshufflesIt(t *testing.T) {
	space := workspace(t)
	for _, family := range []string{FamilyMedia, FamilyDocument} {
		before := newToolbox(space, "1", nil, nil, offersEverything(t), 0)
		after := newToolbox(space, "1", nil, nil, offersEverything(t), 0)
		after.Arm(family)

		cold, warm := toolBlock(t, before), toolBlock(t, after)
		// Both blocks are JSON arrays; compare the definitions inside, since the
		// closing bracket is the one byte an append legitimately moves.
		coldBody, warmBody := cold[:len(cold)-1], warm[:len(warm)-1]
		if shared := sharedPrefix(coldBody, warmBody); shared < len(coldBody) {
			t.Fatalf("arming %s rewrote the tool block at byte %d of %d — everything behind it is re-billed cold:\n before %s\n after  %s",
				family, shared, len(coldBody), cold, warm)
		}
	}

	// And arming both, in either order, still only ever appends.
	space2 := workspace(t)
	steps := newToolbox(space2, "2", nil, nil, offersEverything(t), 0)
	block := toolBlock(t, steps)
	for _, family := range []string{FamilyDocument, FamilyMedia} {
		steps.Arm(family)
		next := toolBlock(t, steps)
		if shared := sharedPrefix(block[:len(block)-1], next[:len(next)-1]); shared < len(block)-1 {
			t.Fatalf("arming %s second reshuffled the block at byte %d:\n before %s\n after  %s",
				family, shared, block, next)
		}
		block = next
	}
}

// The head of the block is the same five tools in the same order for every leaf
// on every machine, whatever else that machine has configured. It is the widest
// agreement the tool list can offer and it is worth stating as a fact rather
// than leaving to the order someone happens to write the literal in.
func TestTheToolBlockLeadsWithTheSameFiveToolsEverywhere(t *testing.T) {
	space := workspace(t)
	head := []string{"sh", "job", "write", "edit", "web"}
	for name, toolbox := range map[string]*Toolbox{
		"a bare machine":   NewToolbox(space, "1", nil),
		"everything wired": newToolbox(space, "2", nil, nil, offersEverything(t), 0),
		"everything wired and armed": func() *Toolbox {
			full := newToolbox(space, "3", nil, nil, offersEverything(t), 0)
			full.Arm(FamilyMedia, FamilyDocument)
			return full
		}(),
	} {
		definitions := toolbox.Definitions()
		if len(definitions) < len(head) {
			t.Fatalf("%s carries only %d tools", name, len(definitions))
		}
		for index, want := range head {
			if got := definitions[index].Function.Name; got != want {
				t.Fatalf("%s has %q at slot %d, want %q", name, got, index, want)
			}
		}
	}
}

// The one thing the loop must never do is silently stop reading the number the
// ceiling now depends on. Both spellings of the cache-read count are recorded,
// because a provider reporting the one this code did not read is
// indistinguishable from a provider with no cache at all.
func TestBothSpellingsOfTheCacheReadCountAreRecorded(t *testing.T) {
	nested := &ai.Response{Usage: &ai.Usage{
		PromptTokens: 10_000, CompletionTokens: 500,
		PromptTokensDetails: &ai.PromptTokensDetails{CachedTokens: 8_000},
	}}
	native := &ai.Response{Usage: &ai.Usage{
		PromptTokens: 10_000, CompletionTokens: 500, CacheReadInputTokens: 8_000,
	}}
	for name, response := range map[string]*ai.Response{
		"prompt_tokens_details.cached_tokens": nested,
		"cache_read_input_tokens":             native,
	} {
		var usage Usage
		addUsage(&usage, response)
		if usage.CachedTokens != 8_000 {
			t.Fatalf("%s recorded %d cache reads, want 8000", name, usage.CachedTokens)
		}
	}
}
