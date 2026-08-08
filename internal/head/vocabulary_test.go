package head

import (
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
			if !containsWord(prompt, word) {
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

func containsWord(text, word string) bool {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

func containsPhrase(text, phrase string) bool {
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta(phrase)).MatchString(text)
}
