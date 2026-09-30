//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// WORDS FROM THE PERSON'S SIDE REACH THE MODEL BETWEEN ITS STEPS.
//
// senior-dev used to read nothing after its brief, so a person watching it
// spend an hour on the wrong file could only stop it. codeaf now hands a
// listening program messages while it works (delegate's inbox.go): the person's
// words from the task's page and the conversation's `say`. senior-dev takes them
// at the two places its own process already speaks to its model:
//
//   - before each model call inside a turn, through the step loop's reminder
//     hook (turn.BetweenStepReminder), which saves the words into the session
//     as a message of their own, exactly as the loop saves any other; and
//   - when the model ends a turn without handing in, in place of the nudge —
//     and without counting as one, because the model did not stall, somebody
//     spoke to it.
//
// A MESSAGE IS NEVER TAKEN MID-CALL. Cutting a call the model is answering
// throws away half an answer and lands in the turn-error road; the longest a
// message waits is one call and the tool it asked for.
//
// EVERY MESSAGE TAKEN IS KEPT IN .senior-dev/steering.md, and compaction pins
// that file beside the brief (session/compaction's steering pin), because a
// summary drops the loop's own messages and a direction given at minute ten is
// still the direction at minute ninety. The file sits in senior-dev's own
// notes, which are never part of what it hands in.
//
// AND SENIOR-DEV SAYS IT HEARD. The receipt is what lets codeaf mark a note
// delivered; the inbox closes when it hands in, because a frozen tree cannot
// take direction, and codeaf refuses later words with that reason.

// steeringFile is where the messages taken are kept, relative to the workspace.
const steeringFile = ".senior-dev/steering.md"

// steeringStage is the stage status a taken message is reported under, which
// senior-dev's page draws as a steering line (internal/seniordev's actions.go).
const steeringStage = "steered"

// takeSteering reads the messages waiting in the inbox and answers the words
// the model is handed, "" when there are none or nobody can send any.
func (runner *pipeline) takeSteering() string {
	// THE INBOX IS CLOSED FROM INSIDE THE SUBMIT TOOL, which need not run on
	// the loop's goroutine, so reading it and closing it hold one lock.
	runner.inboxMu.Lock()
	inbox := runner.inbox
	var messages []delegate.Message
	if inbox != nil {
		messages = inbox.Messages()
	}
	if len(messages) > 0 {
		ids := make([]string, 0, len(messages))
		for _, message := range messages {
			ids = append(ids, message.ID)
		}
		inbox.Heard(ids)
	}
	runner.inboxMu.Unlock()
	if len(messages) == 0 {
		return ""
	}
	runner.keepSteering(messages)
	last := messages[len(messages)-1]
	runner.events.stage("implement", steeringStage, map[string]any{
		"messages": len(messages), "from": last.From, "detail": last.Text,
	})
	runner.note(fmt.Sprintf("[senior-dev] implement: handed its model %d message(s) from %s\n",
		len(messages), messageFromWord(last.From)))
	return steeringSpoken(messages)
}

// keepSteering appends the messages to the steering file compaction pins. A
// file that cannot be written costs the pin, never the message: the model is
// handed the words either way.
func (runner *pipeline) keepSteering(messages []delegate.Message) {
	path := filepath.Join(runner.workspace, steeringFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	stamp := runner.now().UTC().Format(time.RFC3339)
	for _, message := range messages {
		_, _ = fmt.Fprintf(file, "- %s, %s: %s\n", messageFromWord(message.From), stamp,
			strings.Join(strings.Fields(message.Text), " "))
	}
}

// closeInbox tells codeaf senior-dev reads no more messages, once.
func (runner *pipeline) closeInbox(reason string) {
	runner.inboxMu.Lock()
	defer runner.inboxMu.Unlock()
	if runner.inbox == nil {
		return
	}
	runner.inbox.CloseInbox(reason)
	runner.inbox = nil
}

// steeringSpoken is the messages as the model reads them.
func steeringSpoken(messages []delegate.Message) string {
	var b strings.Builder
	if len(messages) == 1 {
		b.WriteString("While you work, a message reached you from the people you are working for:\n")
	} else {
		fmt.Fprintf(&b, "While you work, %d messages reached you from the people you are working for:\n", len(messages))
	}
	for _, message := range messages {
		b.WriteString("- " + messageFromWord(message.From) + ": " + strings.TrimSpace(message.Text) + "\n")
	}
	b.WriteString("Take it as direction about how to do the work: where to look, what to stop " +
		"chasing, what they now know. .senior-dev/spec.md is still what the work must achieve; " +
		"where a message asks for something the spec does not, do what the spec asks and say in " +
		"your submit reason what you did about the message. Nobody can answer a reply: carry on.")
	return b.String()
}

// messageFromWord names who a message is from, for the model and the notes.
func messageFromWord(from string) string {
	switch from {
	case delegate.FromPerson:
		return "the person"
	case delegate.FromConversation:
		return "the conversation that handed you this work"
	}
	return "another worker on this run"
}
