package session

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A CONTINUATION IS CONTEXT, NOT ANOTHER QUESTION. Reopening an older journal
// must retain the real exchange without putting the engine's prompt in it.
func TestReopenedConversationKeepsCarryOnOutOfTheExchange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	messages := []ai.Message{
		textMessage("user", "Please finish the work."),
		textMessage("assistant", "The first part is ready."),
		textMessage("user", checkpointCarryOnLead+"Check the remaining file."),
		textMessage("assistant", "The work is finished."),
		textMessage("user", "[carry on] Please continue with the next item."),
	}
	for _, message := range messages {
		agent.record(message)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	for name, entries := range map[string][]DisplayEntry{
		"memory":           displayEntries(messages),
		"reopened journal": ReadTranscript(path).Entries,
	} {
		t.Run(name, func(t *testing.T) {
			if len(entries) != 4 {
				t.Fatalf("exchange has %d entries, want four", len(entries))
			}
			for _, e := range entries {
				if strings.Contains(e.Text, checkpointCarryOnLead) {
					t.Fatal("the engine's continuation appeared as conversation")
				}
			}
			if entries[0].Text != "Please finish the work." || entries[2].Text != "The work is finished." || entries[3].Text != "[carry on] Please continue with the next item." {
				t.Fatalf("the person's words or the final answer changed: %+v", entries)
			}
		})
	}
}

// Compaction measures the same display list that replay later pages through.
func TestDisplayCountExcludesInternalContinuationContext(t *testing.T) {
	messages := []ai.Message{
		textMessage("user", "Please finish the work."),
		textMessage("user", checkpointCarryOnLead+"Check the remaining file."),
		textMessage("user", volatileNoteOpening+"temporary context"),
		textMessage("assistant", "The work is finished."),
	}
	if got, want := countEntries(messages), len(displayEntries(messages)); got != want {
		t.Fatalf("compaction counted %d display entries, replay draws %d", got, want)
	}
}

// EVERY CONTINUATION THE HARNESS WRITES IS CONTEXT, NOT ONLY THE CARRY-ON. The
// load and ask nudges and the note after a reply was cut off at the output
// limit are user-role for the model's sake exactly as the carry-on is, and a
// reopened conversation drew each one as a line the person typed — a bubble of
// words they never said, and one more person's message moving the place of
// every turn after it. A person's own words that merely open the same way stay.
func TestReopenedConversationKeepsEveryContinuationOutOfTheExchange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	continuations := []string{
		checkpointLoadNudgeLead([]string{"ask"}),
		checkpointAskNudgeLead("an option needs a label"),
		truncationContinuationNote,
	}
	messages := []ai.Message{textMessage("user", "Please finish the work.")}
	for _, continuation := range continuations {
		messages = append(messages, textMessage("assistant", "Let me make the question."), textMessage("user", continuation))
	}
	mine := "[carry on] You loaded the wrong file, use the other one."
	messages = append(messages, textMessage("assistant", "The work is finished."), textMessage("user", mine))
	for _, message := range messages {
		agent.record(message)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	for name, entries := range map[string][]DisplayEntry{
		"memory":           displayEntries(messages),
		"reopened journal": ReadTranscript(path).Entries,
	} {
		t.Run(name, func(t *testing.T) {
			users := 0
			for _, e := range entries {
				for _, continuation := range continuations {
					if e.Text == continuation {
						t.Fatalf("the engine's continuation appeared as conversation: %q", e.Text)
					}
				}
				if e.Role == "user" {
					users++
				}
			}
			if users != 2 || entries[len(entries)-1].Text != mine {
				t.Fatalf("the person's own messages changed: %d of them, %+v", users, entries)
			}
		})
	}
	if got, want := countEntries(messages), len(displayEntries(messages)); got != want {
		t.Fatalf("compaction counted %d display entries, replay draws %d", got, want)
	}
}

// THE LINES SAID AS A TURN IS SENT BACK ARE ONE READING, and the lines that
// end a turn are not among them: a surface that leaves the first out keeps
// every one of the second.
func TestOnlyTheLinesThatSendATurnBackAreCarryOnNotices(t *testing.T) {
	for _, line := range []string{checkpointCarryOnNote, checkpointLoadNudgeNote, checkpointAskNudgeNote, " " + checkpointCarryOnNote + "\n"} {
		if !IsCarryOnNotice(line) {
			t.Errorf("%q is not read as a carry-on", line)
		}
	}
	for _, line := range []string{
		checkpointCarriedOnNote(nil), checkpointCarriedOnNote([]string{"the tests"}),
		checkpointStoppedNote + "the budget is spent", checkpointDoneNote, checkpointCeilingNote,
		checkpointHeldWholeNote, taskEscalationNote, loopLeftUndoneNote, stoppedSoupNote,
		"Retry 1/3: removed max_tokens", checkpointCarryOnNote + " · and more", "",
	} {
		if IsCarryOnNotice(line) {
			t.Errorf("%q is read as a carry-on", line)
		}
	}
}
