package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Journey #19 without a phrase list. "what can you do?" arrives in a hundred
// shapes and none of them are worth matching on, so the product's account of
// itself rides in the stable prompt and the model decides when it is relevant.
// This asserts the two properties that make that affordable: it is there, and
// it is there exactly once, in bytes that do not move between calls.
func TestTheProductPitchRidesTheStablePromptExactlyOnce(t *testing.T) {
	graph := openHeadStore(t)

	render := func(body string) string {
		t.Helper()
		client := &fakeClient{responses: []string{"noted"}}
		message := postUser(t, graph, "pitch", body)
		if err := New(client, graph).answer(context.Background(), message); err != nil {
			t.Fatalf("answer %q: %v", body, err)
		}
		if len(client.seen) < 1 {
			t.Fatalf("%q never reached a model", body)
		}
		return client.seen[0].Content[0].Text
	}

	first := render("what can you do?")
	if _, err := graph.RecordFact("", "user", store.FactPlain, "the release branch is stable"); err != nil {
		t.Fatal(err)
	}
	second := render("and what else?")

	if count := strings.Count(first, manual.Pitch); count != 1 {
		t.Fatalf("the pitch appears %d times in the head's system message", count)
	}
	if !strings.Contains(orchestratorPrompt, manual.Pitch) {
		t.Fatal("the pitch is not part of the head's constant prompt")
	}
	// A state change moved the notebook, which lives below the system message.
	// The block itself is bytes, and bytes that move are bytes paid for twice.
	if first != second {
		t.Fatalf("the system message is not byte-stable across two renders:\n%q\n%q", first, second)
	}
	// The law and the catalog are both in there: an answer quoted from this
	// block can say what aforge is and what a person may say to it.
	for _, phrase := range []string{
		"the thread is the only mouth", "commission work", "stand up a rule",
	} {
		if !strings.Contains(first, phrase) {
			t.Fatalf("the pitch reached the prompt without %q", phrase)
		}
	}
	// And it is grounding, not scripture: the law the head is held to still comes
	// after it. There is one prompt now, so "after it" is a byte offset inside
	// that prompt rather than a claim about which of two prompts got the pitch.
	if strings.Index(first, manual.Pitch) > strings.Index(first, "Law you do not get to bend.") {
		t.Fatal("the pitch was appended past the law it is grounding for")
	}
}

// The catalog the ? overlay lists and the catalog the head carries are the same
// authored lines. Parsing is what enforces that; a second table would agree only
// on the day it was written.
func TestSayCatalogIsReadOutOfTheAuthoredPitch(t *testing.T) {
	says := manual.Says()
	if len(says) < 10 {
		t.Fatalf("the catalog parsed to %d rows", len(says))
	}
	for _, say := range says {
		if strings.TrimSpace(say.Verb) == "" || strings.TrimSpace(say.Example) == "" {
			t.Fatalf("a catalog row parsed empty: %+v", say)
		}
		if !strings.Contains(manual.Pitch, say.Verb) {
			t.Fatalf("catalog row %q is not in the authored text", say.Verb)
		}
	}
}
