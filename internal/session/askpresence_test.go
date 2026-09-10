package session

// A SESSION STOPPED ON THE MODEL'S OWN `ask` SAYS SO WHERE EVERY OTHER WINDOW
// READS IT.
//
// The presence file is the one place home, another window and the `--host` link
// look to find out what a conversation is doing, and the whole question goes on
// it — not the head alone — because a row that offers no answers is a row a
// person can read and not act on. The consent lane is covered beside the gate
// (question_test.go); this is the lane a model raises, which is the one that
// crosses the wire on the road a plain `aforge` takes.

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestASessionStoppedOnAnAskPutsTheWholeQuestionOnThePresenceFile(t *testing.T) {
	agent, dir := questionSession(t, "qqqq2222qqqq2222", func(config *Config) {
		config.Interactive = true
	})

	done := make(chan string, 1)
	go func() {
		out, _, _ := agent.executeAsk(context.Background(), json.RawMessage(`{
			"head":"which storage shape should this use?",
			"kind":"choice",
			"reason":"two shapes are viable and the record does not choose between them",
			"stakes":"costly",
			"options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}]}`))
		done <- out
	}()

	presence := waitForQuestion(t, dir)
	if presence.Kind != QuestionAsk {
		t.Fatalf("the lane on the presence file reads %q, and an answer left in another window is applied through it", presence.Kind)
	}
	if presence.Text != "which storage shape should this use?" {
		t.Fatalf("the presence file carries %q", presence.Text)
	}
	if len(presence.Options) != 2 {
		t.Fatalf("the presence file offers %d answers, so a row drawn from it can be read and not acted on", len(presence.Options))
	}
	// AND THE WHOLE OBJECT IS ON IT, beside the four fields an older window
	// reads: the reason, the stakes and the shape of the decision are what a
	// second window needs to draw the same question this one is drawing.
	if presence.Full == nil {
		t.Fatal("presence carries the line and not the question")
	}
	switch {
	case presence.Full.Ask != AskChoice:
		t.Fatalf("the shape of the decision reads %q", presence.Full.Ask)
	case presence.Full.Reason != "two shapes are viable and the record does not choose between them":
		t.Fatalf("the reason did not reach the file: %q", presence.Full.Reason)
	case presence.Full.Stakes != StakesCostly:
		t.Fatalf("the stakes read %q", presence.Full.Stakes)
	}

	// AND ANSWERING IT TAKES IT OFF AGAIN, so a conversation nobody is waiting on
	// stops saying it is waiting.
	if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: presence.ID, Picked: []string{"2"}}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the ask never came back with its answer")
	}
	waitForNoQuestion(t, dir)
}
