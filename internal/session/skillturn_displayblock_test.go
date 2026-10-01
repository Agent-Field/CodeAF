package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plan"
)

// THE DISPLAY DOORS SHOW THE WORDS, NOT THE BLOCK. The skills a turn carries
// are model context ([attachTurnSkillsLocked]); the copy in a.messages keeps
// them because it is also the history the provider reads, so every display door
// — Transcript and AttachReplay, both through shapeEntries — must strip the
// trailing block itself. The journal and store already held the person's words
// alone (#1504 pinned that half); this is the display half it never touched.
func TestTheTranscriptShowsTheWordsWhenTheBlockRidesTheModelCopy(t *testing.T) {
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

	// THE MODEL STILL READS IT: the strip is a display projection, never a cut
	// to the provider-bound copy.
	sent := userTextIn(completer.request(0))
	if !strings.Contains(sent, "Skills suited to this message:") {
		t.Fatalf("the model's copy lost the block:\n%s", sent)
	}

	entries := agent.Transcript()
	saw := false
	for _, entry := range entries {
		if entry.Role != "user" {
			continue
		}
		if strings.Contains(entry.Text, "Skills suited to this message:") {
			t.Fatalf("a display entry printed the skills block:\n%s", entry.Text)
		}
		if entry.Text == words {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("the transcript never showed the person's words whole: %#v", entries)
	}
}

// IT IS EXACTLY THE BLOCK, OR IT IS THE PERSON'S OWN TEXT. A message containing
// the marker, or even the closing sentence, that is not a whole trailing render
// is left untouched — the reporter pasted this text themselves, and their words
// survive.
func TestTheStripLeavesAnyFragmentThatIsNotTheWholeBlock(t *testing.T) {
	rendered := turnSkillsLead + plan.RenderSkillsBlock([]plan.SkillEntry{
		{Name: "lint", Doc: "checks the lint rules for this repo", ShelfPath: "/shelf/lint"},
	})
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "a real rendered block is removed",
			text: "how should I lint this repo?" + rendered,
			want: "how should I lint this repo?",
		},
		{
			name: "the marker with no closing line is the person's",
			text: "I saw this at the top:\n\nSkills suited to this message:\nand it confused me",
			want: "I saw this at the top:\n\nSkills suited to this message:\nand it confused me",
		},
		{
			name: "the marker and closing line with words after are the person's",
			text: "quote:\n\nSkills suited to this message:\n- nothing\nEarlier-listed skills win when two skills conflict.\nnever mind",
			want: "quote:\n\nSkills suited to this message:\n- nothing\nEarlier-listed skills win when two skills conflict.\nnever mind",
		},
		{
			name: "a closing line with no entry body is the person's",
			text: "notes:\n\nSkills suited to this message:\nthe end.\nEarlier-listed skills win when two skills conflict.",
			want: "notes:\n\nSkills suited to this message:\nthe end.\nEarlier-listed skills win when two skills conflict.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := shapeEntries([]ai.Message{textMessage("user", tc.text)}, nil)
			if len(entries) != 1 || entries[0].Role != "user" {
				t.Fatalf("want one user entry, got %#v", entries)
			}
			if entries[0].Text != tc.want {
				t.Fatalf("display text is\n%q\nwant\n%q", entries[0].Text, tc.want)
			}
		})
	}
}
