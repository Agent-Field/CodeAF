package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Interim speech (chat-simplify §2.8B).
//
// Everything the head said used to be the last thing it did. "Have a quick look
// and tell me what you find" was answered by a silence the length of the looking
// and then one message in the past tense, and the whole belt budget was shaped
// around a turn that had exactly one chance to speak. This is the other shape: a
// line now, the turn carrying on, the answer after.
func TestSayPostsALineAndTheTurnCarriesOn(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolSay, map[string]any{
			"text": "Having a look at the three that are running."})}},
		{calls: []ai.ToolCall{beltCall("b1", beltToolBoard, map[string]any{})}},
		{text: "The finance close is the one holding the rest up."},
	}}
	user := postUser(t, graph, "interim", "which one is holding things up?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	replies := agentReplies(t, graph, "interim")
	if len(replies) != 2 {
		t.Fatalf("the turn posted %d lines, want the interim one and the answer: %+v", len(replies), replies)
	}
	if replies[0].Body != "Having a look at the three that are running." {
		t.Fatalf("the interim line is not what was said first: %q", replies[0].Body)
	}
	if replies[1].Body != "The finance close is the one holding the rest up." {
		t.Fatalf("speaking mid-turn swallowed the answer: %q", replies[1].Body)
	}
	// The loop did not end at the say: it read the board afterwards, which is the
	// whole difference between this and the tools that speak for the turn.
	if _, tooled := client.counts(); tooled != 3 {
		t.Fatalf("the turn made %d calls, want the say, the read and the answer", tooled)
	}
}

// A turn that said everything it had to say mid-flight, and then had nothing to
// add, ends there. The floor's "try again" under a line the person is already
// reading would be the thread apologising for an answer it just gave.
func TestATurnThatSaidItsPieceDoesNotFollowItWithTheFloor(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolSay, map[string]any{
			"text": "Nothing is running just now."})}},
		{text: ""},
	}}
	user := postUser(t, graph, "quiet", "anything running?")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	replies := agentReplies(t, graph, "quiet")
	if len(replies) != 1 {
		t.Fatalf("posted %d lines, want only the one that was actually said: %+v", len(replies), replies)
	}
	if replies[0].Body != "Nothing is running just now." {
		t.Fatalf("the said line was followed or replaced: %q", replies[0].Body)
	}
}

// And it suppresses that floor and NOTHING else. A turn that spoke mid-flight
// and then acted still owes the visible receipt: prose turned into work is never
// a silent side effect, whatever was said while it was being decided.
func TestSpeakingMidTurnStillLeavesADispatchVisible(t *testing.T) {
	graph := openHeadStore(t)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("s1", beltToolSay, map[string]any{
			"text": "That is a real piece of work — handing it over."})}},
		{calls: []ai.ToolCall{beltCall("w1", beltToolTask, map[string]any{
			"instruction": "profile the four reverse proxies"})}},
		{text: ""},
	}}
	user := postUser(t, graph, "dispatch", "profile the four reverse proxies")
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	replies := agentReplies(t, graph, "dispatch")
	if len(replies) != 2 {
		t.Fatalf("posted %d lines, want the interim one and the receipt: %+v", len(replies), replies)
	}
	if replies[1].CommandSeq == 0 {
		t.Fatalf("the receipt does not tie to the work it started: %+v", replies[1])
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
}

// say is speech and not an act: it changes nothing, so a turn that only spoke
// owes no receipt and journals nothing.
func TestSayIsSpeechAndNotAnAct(t *testing.T) {
	graph := openHeadStore(t)
	head := New(nil, graph)
	user := postUser(t, graph, "speech", "hello")
	run := &beltRun{head: head, user: user}
	result, failed := run.execute(beltToolSay, beltArguments(t, map[string]any{"text": "Hello."}))
	if failed {
		t.Fatalf("say refused: %s", result)
	}
	if run.acted || run.spoke {
		t.Fatalf("say marked the turn as acted=%t spoke=%t", run.acted, run.spoke)
	}
	if !run.said {
		t.Fatal("say did not record that the turn has already spoken")
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("saying something journaled: %+v", commands)
	}
	// An empty line is refused rather than posted: silence wearing a message id
	// is the one thing the posting door exists to prevent.
	if _, failed := run.execute(beltToolSay, beltArguments(t, map[string]any{"text": "  "})); !failed {
		t.Fatal("an empty line was posted")
	}
	messages, err := graph.Messages("speech", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	said := 0
	for _, message := range messages {
		if message.Role == store.RoleAgent && strings.TrimSpace(message.Body) != "" {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("the room carries %d agent lines, want the one that was said", said)
	}
}
