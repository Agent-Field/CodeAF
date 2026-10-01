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
// — Transcript and AttachReplay, both through shapeEntries — takes the block
// off the row it draws. The journal and store already held the person's words
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

// THE STRIP READS PROVENANCE, NEVER THE TEXT. Only a message
// [attachTurnSkillsLocked] marked — and only the exact bytes it appended —
// come off a displayed row. A block the person typed or pasted themselves has
// no mark and keeps every word, however well-formed it is: suffix matching
// cannot tell the two apart, and a restored message never had an injection at
// all (the journal keeps the typed words; the mark is memory-only).
func TestTheStripReadsProvenanceNeverTheText(t *testing.T) {
	rendered := turnSkillsLead + plan.RenderSkillsBlock([]plan.SkillEntry{
		{Name: "lint", Doc: "checks the lint rules for this repo", ShelfPath: "/shelf/lint"},
	})
	words := "how should I lint this repo?"
	pasted := "\n\nSkills suited to this message:\n" +
		"- the sheet as I received it [/elsewhere/SKILL.md — body in this file]\n" +
		"Earlier-listed skills win when two skills conflict."

	mark := func(text, block string) (ai.Message, *presentationIndex) {
		msg := textMessage("user", text)
		index := &presentationIndex{}
		index.remember(msg, &messagePresentation{SkillsBlock: block})
		return msg, index
	}
	injected, injectedIndex := mark(words+rendered, rendered)
	both, bothIndex := mark(words+pasted+rendered, rendered)

	cases := []struct {
		name  string
		msg   ai.Message
		index *presentationIndex
		want  string
	}{
		{
			name:  "the block this session injected comes off",
			msg:   injected,
			index: injectedIndex,
			want:  words,
		},
		{
			name:  "the same bytes without a mark are the person's own",
			msg:   textMessage("user", words+rendered),
			index: nil,
			want:  words + rendered,
		},
		{
			name:  "a whole well-formed block and nothing else is still the person's",
			msg:   textMessage("user", rendered),
			index: nil,
			want:  rendered,
		},
		{
			name:  "only the injected copy comes off a pasted one",
			msg:   both,
			index: bothIndex,
			want:  words + pasted,
		},
		{
			name:  "the marker with no closing line is the person's",
			msg:   textMessage("user", "I saw this at the top:\n\nSkills suited to this message:\nand it confused me"),
			index: nil,
			want:  "I saw this at the top:\n\nSkills suited to this message:\nand it confused me",
		},
		{
			name:  "the marker and closing line with words after are the person's",
			msg:   textMessage("user", "quote:\n\nSkills suited to this message:\n- nothing\nEarlier-listed skills win when two skills conflict.\nnever mind"),
			index: nil,
			want:  "quote:\n\nSkills suited to this message:\n- nothing\nEarlier-listed skills win when two skills conflict.\nnever mind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var entries []DisplayEntry
			if tc.index != nil {
				entries = shapeEntries([]ai.Message{tc.msg}, nil, tc.index)
			} else {
				entries = shapeEntries([]ai.Message{tc.msg}, nil)
			}
			if len(entries) != 1 || entries[0].Role != "user" {
				t.Fatalf("want one user entry, got %#v", entries)
			}
			if entries[0].Text != tc.want {
				t.Fatalf("display text is\n%q\nwant\n%q", entries[0].Text, tc.want)
			}
		})
	}
}
