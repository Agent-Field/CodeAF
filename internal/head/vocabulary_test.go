package head

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// jargonWords is docs/JOURNEY.md's design filter as something a test can run.
// The head is the mouth: every string below is composed deterministically and
// posted verbatim, so a word out of the store schema here is a word out of the
// store schema in the conversation.
var jargonWords = regexp.MustCompile(`(?i)\b(worker|workers|charter|charters|rail|rails|leaf|leaves|graph|graphs|firing|firings|craft|crafts|splice|splices|spliced|node|nodes)\b`)

func assertPlain(t *testing.T, where string, surfaces ...string) {
	t.Helper()
	for _, surface := range surfaces {
		if found := jargonWords.FindString(surface); found != "" {
			t.Errorf("%s speaks the implementation's language (%q): %q", where, found, surface)
		}
	}
}

// TestTheDeterministicRepliesSpeakPlainly covers the sentences the head writes
// without asking a model: the floor under every revision, the consent line for
// a change big enough to need one, and the status word beside every candidate
// row in a "which one did you mean?" question. That status word came straight
// off the store — "claimed · 2h ago" — and it was the first thing a person read
// when the head had to ask them something.
func TestTheDeterministicRepliesSpeakPlainly(t *testing.T) {
	assertPlain(t, "the revision floor reply",
		revisionFloorReply(store.CommandAmend, "the parser rewrite", 0),
		revisionFloorReply(store.CommandAmend, "the parser rewrite", 1),
		revisionFloorReply(store.CommandAmend, "the parser rewrite", 3),
		revisionFloorReply(store.CommandExpedite, "the parser rewrite", 2))

	statuses := []store.Status{store.Pending, store.Claimed, store.Running,
		store.Done, store.Failed, store.Cancelled}
	for _, status := range statuses {
		assertPlain(t, "a candidate row hint",
			surgeryTargetHint(store.SurgeryTarget{Node: store.Node{Status: status}, Age: "2h ago"}))
	}

	assertPlain(t, "the consent line",
		surgeryLoss(store.CommandCancel,
			store.SurgeryImpact{Nodes: 40, OpenNodes: 40, Running: 2}))

	// The sentences that replace a reply the head can no longer stand behind.
	// They are the only thing the person sees on those turns, which makes them
	// the worst possible place to say "charter" or "node".
	assertPlain(t, "the honest refusal", unclearCommandReply, noSuchTargetReply,
		commandErrorReply, providerErrorReply)
	// The which-one question is the ask tool's now and its words are the model's,
	// so what is pinned here is the sentence the belt hands it to ask FROM.
	assertPlain(t, "the standing-rule receipt",
		charterAcknowledgement(store.CommandCharterRetire),
		charterAcknowledgement(store.CommandCharterPause),
		charterAcknowledgement(store.CommandCharterCadence))
}

// TestTheBackstageListsNameTheWordsThatActuallyLeak pins the second half of the
// fix. Both system prompts always carried a backstage list, and neither named
// worker, charter, rail, leaf, graph or firing — precisely the set that leaks.
// A list that omits the leaking words is a list that reads as satisfied.
func TestTheBackstageListsNameTheWordsThatActuallyLeak(t *testing.T) {
	for _, word := range []string{"node", "leaf", "graph", "splice", "worker",
		"charter", "craft", "rail", "firing", "notebook"} {
		for name, prompt := range map[string]string{
			"the orchestrator prompt": orchestratorPrompt,
			"the revision prompt":     revisionVoicePrompt,
		} {
			if !promptCarriesWord(prompt, word) {
				t.Errorf("%s never tells the model to keep %q backstage", name, word)
			}
		}
	}
	// The revision composer used to ASK for the jargon outright — "how many
	// workers heard it" — in the same breath as banning node and graph.
	if containsPhrase(revisionVoicePrompt, "how many workers") {
		t.Error("the revision composer still asks for a worker count")
	}
}

func promptCarriesWord(text, word string) bool {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

func containsPhrase(text, phrase string) bool {
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta(phrase)).MatchString(text)
}

// TestFreshRidesTheRouteDecisionOntoTheWorkOrder is the opt-out that stopped
// needing a word we invented. Skipping learned know-how used to be six frozen
// phrases, and the reliable ones required saying "craft" — the one escape hatch
// in the product that demanded the internal noun, in direct violation of the
// design filter that forbids it. It is a reading of intent now, made where
// every other reading of a message is made, and carried to the engine as a flag
// on the work order.
func TestFreshRidesTheReadingOntoTheWorkOrder(t *testing.T) {
	for name, fresh := range map[string]bool{"asked for": true, "not asked": false} {
		t.Run(name, func(t *testing.T) {
			graphStore := openHeadStore(t)
			ask := "do the investor update"
			if fresh {
				ask += ", but don't use the template this time"
			}
			user, err := graphStore.PostMessage(store.Message{
				SessionID: "fresh", Role: store.RoleUser, Body: ask,
			})
			if err != nil {
				t.Fatal(err)
			}
			client := &beltClient{turns: []beltTurn{
				{calls: []ai.ToolCall{beltCall("c1", beltToolSpawn, map[string]any{
					"instruction": ask, "fresh": fresh})}},
				{text: "On it."},
			}}
			if err := New(client, graphStore).answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			commands, err := graphStore.PendingCommands(10)
			if err != nil || len(commands) != 1 {
				t.Fatalf("commands = %+v err=%v", commands, err)
			}
			if commands[0].Fresh != fresh {
				t.Fatalf("journaled command Fresh = %t, want %t", commands[0].Fresh, fresh)
			}
			if commands[0].Instruction != ask {
				t.Fatalf("the person's own words did not survive: %q", commands[0].Instruction)
			}
		})
	}

	// And the reading is offered where the model reads it, in the words people
	// actually use, so the escape hatch never again requires saying "craft".
	spawn := ""
	for _, definition := range beltDefinitions() {
		if definition.Function.Name == beltToolSpawn {
			spawn = definition.Function.Parameters["properties"].(map[string]any)["fresh"].(map[string]any)["description"].(string)
		}
	}
	for _, phrase := range []string{"first principles", "don't use the template this time", "It is about method, never content"} {
		if !strings.Contains(spawn, phrase) {
			t.Errorf("the fresh reading is not stated where the model makes it: %q", spawn)
		}
	}
	if strings.Contains(strings.ToLower(spawn), "craft") {
		t.Error("the escape hatch is asking for the internal noun again")
	}
}
