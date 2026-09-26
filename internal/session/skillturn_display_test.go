package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE TRANSCRIPT SHOWS ONLY WHAT THE PERSON TYPED. The skills a turn carries
// are model context, chosen for one message ([attachTurnSkillsLocked]); the
// block rides the copy the model reads, and the dim `skills carried` notice is
// the one channel a surface draws. The owner, 2026-09-25, #1504: the block was
// printed as part of the person's message, so every surface that renders the
// conversation printed a list nobody asked for.
//
// This is the END-TO-END reading of that law: through the real chat door, with
// an active shelf and a message that matches one of its skills, the record the
// surface draws — the store thread the replay reads — keeps the person's words
// and nothing else.
func TestTheTranscriptKeepsThePersonsWordsWholeWhenSkillsRide(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "tool:lint", "checks the lint rules for this repo", "/shelf/lint")

	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("run the lint check"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
	})

	words := "how should I lint this repo?"
	events, err := agent.Submit(context.Background(), words)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	drainSkillsNotice(t, events)

	// The model's copy carries the block; the record the surface draws does not.
	sent := userTextIn(completer.request(0))
	if !strings.Contains(sent, "Skills suited to this message:") {
		t.Fatalf("the model's copy lost the block:\n%s", sent)
	}
	messages, err := brain.Messages(agent.threadID(), 0, 0)
	if err != nil {
		t.Fatalf("read the transcript back: %v", err)
	}
	said := false
	for _, message := range messages {
		if message.Role == "user" && strings.Contains(message.Body, words) {
			said = true
		}
		if strings.Contains(message.Body, "Skills suited to this message:") {
			t.Fatalf("the transcript printed the skills block:\n%s", message.Body)
		}
		if message.Role == "user" && strings.Contains(message.Body, words) {
			if !strings.HasPrefix(message.Body, words) {
				t.Fatalf("the transcript's copy of the message does not start with the person's words:\n%s", message.Body)
			}
		}
	}
	if !said {
		t.Fatalf("the transcript never carried the person's message; thread has %d line(s)", len(messages))
	}
}
