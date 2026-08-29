package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE INCIDENT, END TO END. A craft leaf died on an OpenRouter routing 404 and
// the room got the wrapper chain whole:
//
//	⚑ did not finish — node craft-3799~launch: after 3 node call attempts:
//	API error (404): {"error":{"message":"No endpoints found that support tool
//	use. Try disabling \"sh\"...
//
// This drives the real error through the real wrappers and reads back the two
// things that reach a person: what the node records, and what the board note
// says. Neither may carry a node id, an attempt count or a brace; both must
// carry the provider's own sentence; and the transport must still be there,
// under the line, for whoever wants it.
func TestTheIncidentsErrorReachesPeopleAsASentence(t *testing.T) {
	refusal := provider.APIError{
		Status: 404,
		Message: `No endpoints found that support tool use. Try disabling "sh" or choosing ` +
			`a different model.`,
		Body: `{"error":{"message":"No endpoints found that support tool use.","code":404}}`,
	}
	// The wrappers the executor actually stamps, in the order it stamps them.
	wrapped := fmt.Errorf("node craft-3799~launch: %w",
		fmt.Errorf("after %d node call attempts: %w", 3, &refusal))

	node := store.Node{
		ID: "craft-3799~launch", Title: "Launch", Brief: "Launch the analysis fan",
		Provenance: store.Provenance{Intent: "put together a deep dive on the Q3 numbers"},
	}
	recorded := humanFailure(node, wrapped, nil, "").Error()
	headline := firstLine(recorded)

	if headline != "No endpoints found that support tool use." {
		t.Fatalf("the recorded reason is not the provider's own clause: %q", headline)
	}
	// The board note a sibling reads is the same clause with the same guarantee.
	boardNote := "did not finish — " + firstLine(recorded)
	// And the room row the person reads, composed from the node the store now
	// holds — which is what the reconciler builds it from.
	node.Error = recorded
	roomRow := resident.FailedNode(node, resident.CraftFallbackLine).Room()
	roomHeadline := firstLine(roomRow)

	for name, line := range map[string]string{
		"the recorded reason": headline,
		"the board note":      boardNote,
		"the room row":        roomHeadline,
	} {
		for _, machinery := range []string{"{", "}", "craft-3799", "node call attempts", "API error", `\"`} {
			if strings.Contains(line, machinery) {
				t.Errorf("%s leaked %q: %s", name, machinery, line)
			}
		}
		if !strings.Contains(line, "No endpoints found that support tool use.") {
			t.Errorf("%s does not carry the provider's own words: %s", name, line)
		}
	}
	// The room row is the only one that owes the person their own ask and what
	// happens next; the board note is workers talking to workers.
	if !strings.Contains(roomHeadline, "put together a deep dive on the Q3 numbers") {
		t.Fatalf("the room row does not say what was being attempted: %q", roomHeadline)
	}
	if !strings.Contains(roomHeadline, resident.CraftFallbackLine) {
		t.Fatalf("the room row does not say what happens next: %q", roomHeadline)
	}
	// And the transport is still reachable under both of them.
	if !strings.Contains(recorded, wrapped.Error()) {
		t.Fatalf("the raw error was deleted rather than folded:\n%s", recorded)
	}
	if !strings.Contains(roomRow, wrapped.Error()) {
		t.Fatalf("the room row dropped the evidence:\n%s", roomRow)
	}
}
