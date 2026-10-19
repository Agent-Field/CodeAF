package session

// THE /helpers NOTE, AS TESTS: a conversation a person sits in, in no team, is
// told once — after it has gone on — that it may suggest /helpers; and nobody
// else is told at all.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// helpersNotes is every /helpers note in one request, in order.
func helpersNotes(messages []ai.Message) []string {
	var notes []string
	for _, message := range messages {
		if text := messageText(message); message.Role == "user" && strings.HasPrefix(text, helpersNoteOpening) {
			notes = append(notes, text)
		}
	}
	return notes
}

// loneConversation is a transcript beside the fixture's team and in no team
// of its own: an ordinary conversation on the same profile.
func loneConversation(t *testing.T, fixture teamFixture) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(filepath.Dir(fixture.manager)), "lone")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, placeTranscript)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// interactive is a conversation a person is sitting in, as the chat door
// builds one.
func interactive(config *Config) { config.Interactive = true }

// THE NOTE LANDS ONCE, AFTER THE PERSON HAS SAID [helpersNoteAfter] THINGS, and
// stays where it landed: the requests before carry none, and every request
// after carries the one note, never a second. It is the session's note and
// not the person's words, and it names the command and no tool.
func TestTheHelpersNoteLandsOnceAfterTheConversationHasGoneOn(t *testing.T) {
	fixture := newTeamFixture(t, true)
	turns := helpersNoteAfter + 2
	completer := oneAnswer(turns)
	agent := teamAgent(t, fixture, loneConversation(t, fixture), completer, interactive)
	for i := 0; i < turns; i++ {
		submitAndWait(t, agent, "and the next part of the work")
	}
	for i := 0; i < turns; i++ {
		notes := helpersNotes(completer.request(i))
		want := 0
		if i >= helpersNoteAfter-1 {
			want = 1
		}
		if len(notes) != want {
			t.Fatalf("request %d carried %d /helpers notes, want %d:\n%s", i, len(notes), want, userTextIn(completer.request(i)))
		}
	}
	note := helpersNotes(completer.request(turns - 1))[0]
	if !isVolatileNote(note) {
		t.Fatal("the note would be drawn as something the person typed")
	}
	for _, want := range []string{"/helpers", "Only the person can make the team", "once", "Not for small or quick work"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not say %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "team_") {
		t.Errorf("the note names a tool the conversation does not have:\n%s", note)
	}
}

// THE MANUAL SAYS "after your fourth message" (team-manager.md, *Why the chat
// suggested bringing in helpers*). Move the threshold and that sentence moves
// with it, in the same change.
func TestTheHelpersNoteThresholdIsTheOneTheManualStates(t *testing.T) {
	const manualSays = 4
	if helpersNoteAfter != manualSays {
		t.Fatalf("the note lands after %d messages and the manual says the fourth; change both", helpersNoteAfter)
	}
	if !manual.Chat().Mentions("after your fourth message") {
		t.Fatal("the manual no longer says when the note lands")
	}
}

// A CONVERSATION IN A TEAM IS NEVER TOLD: its role note says what it is, and
// a manager suggesting a second team would be the nesting /helpers refuses.
func TestAConversationInATeamIsNeverToldToSuggestHelpers(t *testing.T) {
	fixture := newTeamFixture(t, true)
	turns := helpersNoteAfter + 1
	for _, transcript := range []string{fixture.manager, fixture.web} {
		completer := oneAnswer(turns)
		agent := teamAgent(t, fixture, transcript, completer, interactive)
		for i := 0; i < turns; i++ {
			submitAndWait(t, agent, "and the next part of the work")
		}
		if notes := helpersNotes(completer.request(turns - 1)); len(notes) != 0 {
			t.Fatalf("%s, in a team, was told to suggest helpers:\n%s", filepath.Base(filepath.Dir(transcript)), notes[0])
		}
	}
}

// NOBODY BUT A CONVERSATION A PERSON SITS IN IS TOLD: a headless one, an
// automation's run, home's `ask here` exchange, a task's node, and one with no
// profile a team could be kept in.
func TestOnlyAConversationAPersonSitsInMayOfferHelpers(t *testing.T) {
	fixture := newTeamFixture(t, true)
	base := Config{Interactive: true, ProfileDir: fixture.profile, SessionFile: loneConversation(t, fixture)}
	if !base.mayOfferHelpers() {
		t.Fatal("an interactive conversation on a profile may not offer helpers")
	}
	for name, change := range map[string]func(*Config){
		"headless":       func(c *Config) { c.Interactive = false },
		"automation run": func(c *Config) { c.AutomationRun = &AutomationRun{} },
		"ask here":       func(c *Config) { c.Errand = true },
		"task node":      func(c *Config) { c.InTask = true },
		"no transcript":  func(c *Config) { c.SessionFile = "" },
	} {
		config := base
		change(&config)
		if config.mayOfferHelpers() {
			t.Errorf("a %s conversation may offer helpers", name)
		}
	}

	completer := oneAnswer(helpersNoteAfter + 1)
	agent := teamAgent(t, fixture, loneConversation(t, fixture), completer, nil)
	for i := 0; i <= helpersNoteAfter; i++ {
		submitAndWait(t, agent, "and the next part of the work")
	}
	if notes := helpersNotes(completer.request(helpersNoteAfter)); len(notes) != 0 {
		t.Fatal("a headless conversation was told to suggest helpers")
	}
}
