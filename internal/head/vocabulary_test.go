package head

import (
	"context"
	"regexp"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
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
	assertPlain(t, "the which-one question", describedChoicePrompt(2, 0),
		describedChoicePrompt(0, 2), describedChoicePrompt(1, 1))
}

// TestTheBackstageListsNameTheWordsThatActuallyLeak pins the second half of the
// fix. Both system prompts always carried a backstage list, and neither named
// worker, charter, rail, leaf, graph or firing — precisely the set that leaks.
// A list that omits the leaking words is a list that reads as satisfied.
func TestTheBackstageListsNameTheWordsThatActuallyLeak(t *testing.T) {
	for _, word := range []string{"node", "leaf", "graph", "splice", "worker",
		"charter", "craft", "rail", "firing", "notebook"} {
		for name, prompt := range map[string]string{
			"the router prompt":       headSystemPrompt,
			"the control-loop prompt": controlSystemPrompt,
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
func TestFreshRidesTheRouteDecisionOntoTheWorkOrder(t *testing.T) {
	for name, response := range map[string]string{
		"asked for": `{"reply":"Working it out from scratch.","command":{"kind":"splice","target":"","instruction":"do the investor update, but don't use the template this time"},"remember":null,"retract":null,"fresh":true}`,
		"not asked": `{"reply":"On it.","command":{"kind":"splice","target":"","instruction":"do the investor update"},"remember":null,"retract":null,"fresh":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			graphStore := openHeadStore(t)
			user, err := graphStore.PostMessage(store.Message{
				SessionID: "fresh", Role: store.RoleUser, Body: "do the investor update",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := New(&fakeClient{responses: []string{response}}, graphStore).
				answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			commands, err := graphStore.PendingCommands(10)
			if err != nil || len(commands) != 1 {
				t.Fatalf("commands = %+v err=%v", commands, err)
			}
			if want := name == "asked for"; commands[0].Fresh != want {
				t.Fatalf("journaled command Fresh = %t, want %t", commands[0].Fresh, want)
			}
		})
	}
}
