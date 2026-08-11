package head

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The whole of 8.2.10's amendment is "on demand, never ambient", so what has to
// be proved is a pair: another room's words are REACHABLE through a tool call,
// and they are ABSENT from a turn that did not make one. The second half is the
// one that matters — an ambient version of this read would put every room in
// every prompt and answer this room's question with that room's context.
func TestAnotherRoomIsReadableOnDemandAndNeverAmbient(t *testing.T) {
	graph := openHeadStore(t)
	if _, err := graph.OpenSession("other", "Lisbon trip", "chat"); err != nil {
		t.Fatal(err)
	}
	postUser(t, graph, "other", "we said the hotel has to be walkable to the office")
	if _, err := graph.PostMessage(store.Message{
		SessionID: "other", Role: store.RoleAgent, Body: "Noted — walkable, under 200 a night.",
	}); err != nil {
		t.Fatal(err)
	}

	client := &beltClient{turns: []beltTurn{{text: "I'll go and look."}}}
	head := New(client, graph)
	user := postUser(t, graph, "here", "what did we agree about the hotel?")
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	// Nothing about the other room reached the prompt on its own.
	if opening := client.openingPrompt(); strings.Contains(opening, "walkable to the office") {
		t.Fatalf("another room's conversation arrived ambiently:\n%s", opening)
	}

	// And the read reaches it.
	run := &beltRun{head: head, user: user}
	rooms, failed := run.execute(beltToolThread, `{}`)
	if failed || !strings.Contains(rooms, "Lisbon trip") || !strings.Contains(rooms, "other") {
		t.Fatalf("the room list does not offer the other conversation: %q failed=%t", rooms, failed)
	}
	if strings.Contains(rooms, `"here"`) || strings.Contains(rooms, "- here |") {
		t.Fatalf("the room list offered the room being stood in: %q", rooms)
	}
	transcript, failed := run.execute(beltToolThread, `{"room":"other"}`)
	if failed {
		t.Fatalf("the read failed: %s", transcript)
	}
	for _, want := range []string{"## Lisbon trip", "**them:** we said the hotel", "**you, in that room:** Noted"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("the transcript is missing %q:\n%s", want, transcript)
		}
	}
	// It journals nothing: reading another conversation changed neither.
	if run.acted {
		t.Fatal("a read recorded an act")
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a read journaled commands: %+v", commands)
	}
}

// The room the loop is standing in is refused rather than served, because its
// transcript is the first block of the prompt: serving it would spend a call to
// repeat the turn's own opening and would put the same words in the context
// twice under two headings.
func TestTheReadRefusesTheRoomItIsStandingIn(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	user := postUser(t, graph, "here", "what were we saying?")
	run := &beltRun{head: head, user: user}

	result, failed := run.execute(beltToolThread, `{"room":"here"}`)
	if !failed || !strings.Contains(result, "already in front of you") {
		t.Fatalf("this room was served to itself: %q failed=%t", result, failed)
	}
	missing, failed := run.execute(beltToolThread, `{"room":"nowhere"}`)
	if !failed || !strings.Contains(missing, "there is no conversation with id") {
		t.Fatalf("an invented id was served: %q failed=%t", missing, failed)
	}
}

// Bounded twice and sanitized once. These bytes were written by another room's
// model and by workers' output, and they land in a prompt and then in a
// transcript: the chokepoint every other body crosses is not optional for the
// one body that arrives from somewhere else.
func TestTheTranscriptReadIsBoundedAndSanitized(t *testing.T) {
	graph := openHeadStore(t)
	if _, err := graph.OpenSession("noisy", "Noisy room", "chat"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < transcriptTurnCap*3; index++ {
		postUser(t, graph, "noisy", fmt.Sprintf("turn %d", index))
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "noisy", Role: store.RoleAgent,
		Body: "\x1b[31mred\x1b[0m and " + strings.Repeat("long ", 400),
	}); err != nil {
		t.Fatal(err)
	}

	head := New(nil, graph)
	rendered, err := head.renderTranscript("noisy")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "\x1b[") {
		t.Fatalf("escape sequences from another room reached the prompt: %q", rendered)
	}
	if len(rendered) > transcriptBytes {
		t.Fatalf("the read is %d bytes, over its %d ceiling", len(rendered), transcriptBytes)
	}
	if turns := strings.Count(rendered, "\n**"); turns > transcriptTurnCap {
		t.Fatalf("the read carried %d turns, over its cap of %d", turns, transcriptTurnCap)
	}
	// The END of the room, not its beginning: the reason to open another room is
	// almost always what was last said in it.
	if strings.Contains(rendered, "turn 0\n") {
		t.Fatalf("the read handed back the front of the room:\n%s", rendered)
	}
	if !strings.Contains(rendered, "red and long") {
		t.Fatalf("the newest turn is missing from the tail:\n%s", rendered)
	}
}
