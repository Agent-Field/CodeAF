package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A LINE THE SESSION WROTE IS NOT A LINE THE PERSON TYPED.
//
// A task landing, a job exiting, a resume's account of what an interrupt left
// behind: all of them reach the model as user-role text, because that is the
// only role a model can be told something in ([Agent.enqueueNote]). None of them
// is anybody's words. Live, no surface draws them as such — a woken turn writes
// no user line at all (tui3's startFollow) — and this is what makes a RESUMED
// conversation agree with the live one: the journal marks the line, and the
// display shaping hands it back as "aside" instead of "user".

// enqueuedNote drains one session-authored note into the transcript and hands
// back the shaped entries a surface would replay.
func enqueuedNote(t *testing.T, agent *Agent, text string) []DisplayEntry {
	t.Helper()
	// The ambient door rather than the waking one: the mark is the same — both
	// go through [Agent.enqueueNote] — and this one does not spend a turn.
	agent.enqueueAmbientNote(text)
	if landed := agent.drainSteering(nil); landed != 1 {
		t.Fatalf("%d notes drained, want 1", landed)
	}
	return agent.Transcript()
}

func TestASessionsOwnLineReplaysAsAnAsideAndNotAsThePersons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.SessionFile = path })

	const note = "task 7 could not be verified: finished, but needs your look · transcript file:///tmp/7.jsonl"
	entries := enqueuedNote(t, agent, note)
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1: %#v", len(entries), entries)
	}
	if entries[0].Role != "aside" {
		t.Fatalf("role = %q, want aside — the session wrote this line, not the person", entries[0].Role)
	}
	if entries[0].Text != note {
		t.Fatalf("the note's words changed: %q", entries[0].Text)
	}
	// THE MODEL STILL READS IT AS IT ALWAYS DID. The role is a display fact; the
	// transcript on the wire is untouched.
	agent.mu.Lock()
	role := agent.messages[len(agent.messages)-1].Role
	agent.mu.Unlock()
	if role != "user" {
		t.Fatalf("the message on the wire is %q, want user", role)
	}

	// AND IT SURVIVES THE RESUME, which is the only place this matters: the mark
	// is on the journal's line, and the resumed session reads it back.
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	resumed, err := newAgent(Config{
		Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })

	found := false
	for _, entry := range resumed.Transcript() {
		if entry.Text != note {
			continue
		}
		found = true
		if entry.Role != "aside" {
			t.Fatalf("the resumed note is %q, want aside — a resume drew it as the person's words", entry.Role)
		}
	}
	if !found {
		t.Fatalf("the note is not in the resumed transcript: %#v", resumed.Transcript())
	}
}

// A PERSON'S OWN MESSAGE IS UNTOUCHED, journaled and resumed as what it is.
func TestAPersonsMessageIsNotAnAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("done")}},
		func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "port the parser"))

	for _, entry := range agent.Transcript() {
		if entry.Text == "port the parser" && entry.Role != "user" {
			t.Fatalf("a typed message replays as %q, want user", entry.Role)
		}
	}
}

// A FILE WRITTEN BEFORE THE MARK EXISTED REPLAYS EXACTLY AS IT ALWAYS DID. The
// old line has no mark on it, nothing can be inferred from the text, and a build
// that guessed would be re-labelling somebody's words on a hunch.
func TestAnUnmarkedNoteInAnOlderJournalStillReplaysAsUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	const note = "task 7 finished · transcript file:///tmp/7.jsonl"
	lines := strings.Join([]string{
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test/model","timestamp":"t"}`,
		`{"type":"message","role":"user","content":"` + note + `","timestamp":"t"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatalf("writing the older journal: %v", err)
	}

	agent, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	entries := agent.Transcript()
	if len(entries) != 1 || entries[0].Role != "user" {
		t.Fatalf("an older journal's line replayed as something new: %#v", entries)
	}
}
