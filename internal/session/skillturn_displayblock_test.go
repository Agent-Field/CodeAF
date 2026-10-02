package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plan"
)

// THE DISPLAY DOORS SHOW THE WORDS, NOT THE BLOCK. The skills a turn carries
// are model context ([attachTurnSkillsLocked]); the copy in a.messages keeps
// them because it is also the history the provider reads, so every display door
// — Transcript and AttachReplay, both through shapeEntries — takes the block
// off the row it draws. Ordinary journal lines already held the person's words
// alone (#1410); #1627 pinned the store's copy too.
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
// cannot tell the two apart. New journal writes keep the typed words, and
// restoring a saved message does not manufacture a memory-only injection mark.
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

// THE IN-FLIGHT DOOR KEEPS THE STRIP TOO. AttachReplay reads the same
// presentation index Transcript does, but it draws the running turn's opening
// message off its own slice ([attachReplayLocked]'s `kept`, copied out of
// a.messages[:floor]), so the provenance mark has to survive THAT copy as well.
// This is the door a surface arrives through mid-turn — the second half of the
// report's "very often when chatting" — and nothing else exercises it: both
// tests above read the settled record through Transcript alone.
func TestAttachReplayMidTurnKeepsTheStrip(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "tool:lint", "checks the lint rules for this repo", "/shelf/lint")

	// midTurn is closed inside the turn's one step, so the assertions run while
	// a.running and a.hub are both live — the state AttachReplay splits on.
	midTurn := make(chan struct{})
	carryOn := make(chan struct{})
	var release sync.Once
	resume := func() { release.Do(func() { close(carryOn) }) }
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			close(midTurn)
			<-carryOn
			return textResponse("run the lint check"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
	})
	// Release before the agent's cleanup so a failed assertion cannot leave
	// its provider waiting while Close tries to settle the turn.
	t.Cleanup(resume)

	words := "how should I lint this repo?"
	events, err := agent.Submit(context.Background(), words)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	select {
	case <-midTurn:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never reached the provider")
	}

	entries, stream, stop := agent.AttachReplay()
	if stream == nil {
		t.Fatal("AttachReplay mid-turn handed back no stream")
	}
	defer stop()
	saw := false
	for _, entry := range entries {
		if entry.Role != "user" {
			continue
		}
		if strings.Contains(entry.Text, "Skills suited to this message:") {
			t.Fatalf("the in-flight replay drew the skills block:\n%s", entry.Text)
		}
		if entry.Text == words {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("the in-flight replay never showed the person's words whole: %#v", entries)
	}

	resume()
	collect(t, events)
	collect(t, stream)
}

// THE MARK IS KEYED TO THE MESSAGE'S CONTENT ALLOCATION, AND THE USER PATH MUST
// NOT MOVE IT. [presentationIndex.remember] keys on &message.Content[0], and
// [Agent.recordUserLocked] appends `user.message` by value WITHOUT
// re-allocating Content — so the address the mark was recorded under is the
// address [shapeEntries] later looks up. The assistant path is the opposite:
// [recordPresentedAssistant] deep-copies Content first, because its callers may
// reuse the value. If a deep copy were ever introduced between
// [attachTurnSkillsLocked] and recordUserLocked's append, the mark would be
// dropped SILENTLY — `of` returns nil, no error is raised, and the model's
// skills block is drawn back as the person's own words. This is the tripwire.
func TestTheUserMessagesProvenanceMarkSurvivesItsAppendToTheTranscript(t *testing.T) {
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

	// THE RECORDED MESSAGE STILL CARRIES ITS MARK, found by the address the
	// append published. A deep copy on the way in would have made this nil.
	agent.mu.Lock()
	index := agent.presentation
	recorded := ai.Message{}
	found := false
	for _, msg := range agent.messages {
		if msg.Role == "user" && strings.Contains(messageContentText(msg), words) {
			recorded, found = msg, true
		}
	}
	agent.mu.Unlock()
	if !found {
		t.Fatal("the person's message is not in the transcript")
	}
	if mark := index.of(recorded); mark == nil || mark.SkillsBlock == "" {
		t.Fatal("the recorded user message lost its provenance mark — the user path must not deep-copy Content")
	}
	if !strings.Contains(messageContentText(recorded), "Skills suited to this message:") {
		t.Fatal("the model's copy in the transcript lost the block")
	}
}
