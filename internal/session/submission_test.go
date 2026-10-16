package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// answering is a completer that answers every request "done".
func answering() *scriptedCompleter {
	answer := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }
	return &scriptedCompleter{steps: []step{answer, answer, answer, answer, answer, answer}}
}

// personEntries is a reading's entries of the person's own.
func personEntries(entries []DisplayEntry) []DisplayEntry {
	var users []DisplayEntry
	for _, entry := range entries {
		if entry.Role == "user" {
			users = append(users, entry)
		}
	}
	return users
}

// journalMarks is every message line's presentation mark, in file order, nil
// where the line carries none.
func journalMarks(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var marks []map[string]any
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var line struct {
			Type         string         `json:"type"`
			Role         string         `json:"role"`
			Presentation map[string]any `json:"presentation"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Type == "message" && line.Role == "user" {
			marks = append(marks, line.Presentation)
		}
	}
	return marks
}

var (
	submittedFiles = []MessageAttachment{
		{ID: "at1", Kind: "file", Name: "notes.md", Path: "notes.md"},
		{ID: "at2", Kind: "paste", Name: "Pasted text", Bytes: 31, Lines: 2},
	}
	submittedAlone = []MessageAttachment{{ID: "at3", Kind: "link", Name: "example.com", URL: "https://example.com/7"}}
)

// assertSubmitted is the three messages of the round-trip test, as any reading
// of them must say them.
func assertSubmitted(t *testing.T, where string, entries []DisplayEntry) {
	t.Helper()
	users := personEntries(entries)
	if len(users) != 3 {
		t.Fatalf("%s: %d messages of the person's: %#v", where, len(users), users)
	}
	if users[0].Text != "why does this fail?" || !users[0].Authored || !reflect.DeepEqual(users[0].Attachments, submittedFiles) {
		t.Fatalf("%s: the message with words = %#v", where, users[0])
	}
	if users[1].Text != "" || !users[1].Authored || !reflect.DeepEqual(users[1].Attachments, submittedAlone) {
		t.Fatalf("%s: the attachments alone = %#v", where, users[1])
	}
	if users[2].Text != "see Attached file: other.go" || users[2].Authored || users[2].Attachments != nil {
		t.Fatalf("%s: the plain words = %#v", where, users[2])
	}
}

// WHAT A DOOR SAID ABOUT A MESSAGE IS KEPT ON THE MESSAGE'S OWN LINE. Two
// messages are sent with marks — words and attachments, then attachments alone
// with no words — and one is plain words that mention a path. The model reads
// exactly the text it was handed. The live transcript, the record read at
// rest, and the conversation reopened from the file all say the person's words
// and attachments; the plain words are words, with nothing read out of them;
// and only the two marked lines carry a mark, holding no message bytes.
func TestASubmittedMessageKeepsThePersonsWordsAndAttachmentsOnItsLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := answering()
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	said, none := "why does this fail?", ""
	sent := "why does this fail?\n\nAttached file: notes.md\n\nPasted text:\n```\n--- FAIL: TestLoad\nexpected 1\n```"
	files := append([]MessageAttachment(nil), submittedFiles...)
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: sent, Said: &said, Attachments: files})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	// The caller's slice is its own: changing it afterwards reaches no record.
	files[0].Name = "changed"
	events, err = agent.SubmitMessage(context.Background(), Submission{Text: "Link: https://example.com/7", Said: &none, Attachments: submittedAlone})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	events, err = agent.Submit(context.Background(), "see Attached file: other.go")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)

	completer.mu.Lock()
	first := completer.seen[0]
	completer.mu.Unlock()
	if got := messageContentText(first[len(first)-1]); got != sent {
		t.Fatalf("the model was sent %q, want %q", got, sent)
	}
	live := agent.Transcript()
	assertSubmitted(t, "live", live)
	// A reading's attachments are its own: changing them changes no later one.
	personEntries(live)[0].Attachments[0].Name = "scribbled"
	assertSubmitted(t, "read again", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	assertSubmitted(t, "at rest", ReadTranscript(path).Entries)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertSubmitted(t, "bytes", ReadTranscriptBytes(data).Entries)
	marks := journalMarks(t, path)
	if len(marks) != 3 || marks[0] == nil || marks[1] == nil || marks[2] != nil {
		t.Fatalf("the person's lines carry marks %v", marks)
	}
	for _, mark := range marks[:2] {
		for key := range mark {
			if key != "audience" && key != "text" && key != "attachments" {
				t.Fatalf("a mark carries %q: %v", key, mark)
			}
		}
	}

	reopened, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	assertSubmitted(t, "reopened", reopened.Transcript())
	reopened.mu.Lock()
	replayed := messageContentText(reopened.messages[1])
	reopened.mu.Unlock()
	if replayed != sent {
		t.Fatalf("the reopened conversation reads %q to the model, want %q", replayed, sent)
	}
}

// A MARK THAT DOES NOT FIT REFUSES THE MESSAGE WHOLE: nothing is recorded,
// nothing is asked of the model, and the session takes the next message.
func TestASubmissionThatDoesNotFitRecordsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := answering()
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	words := "words"
	many := make([]MessageAttachment, MaxMessageAttachments+1)
	for i := range many {
		many[i] = MessageAttachment{Kind: "link"}
	}
	for name, message := range map[string]Submission{
		"too many":      {Text: "x", Attachments: many},
		"no kind":       {Text: "x", Said: &words, Attachments: []MessageAttachment{{Name: "a"}}},
		"negative size": {Text: "x", Attachments: []MessageAttachment{{Kind: "paste", Bytes: -1}}},
		"long field":    {Text: "x", Attachments: []MessageAttachment{{Kind: "file", Path: strings.Repeat("p", maxAttachmentField+1)}}},
	} {
		if _, err := agent.SubmitMessage(context.Background(), message); !errors.Is(err, errSubmissionMark) {
			t.Fatalf("%s: SubmitMessage = %v", name, err)
		}
	}
	completer.mu.Lock()
	asked := len(completer.seen)
	completer.mu.Unlock()
	if asked != 0 || len(personEntries(agent.Transcript())) != 0 {
		t.Fatalf("a refused message reached the model %d times or the record", asked)
	}
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	users := personEntries(agent.Transcript())
	if len(users) != 1 || users[0].Text != "plain" || users[0].Authored || users[0].Attachments != nil {
		t.Fatalf("a submission with no mark read back as %#v", users)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if marks := journalMarks(t, path); len(marks) != 1 || marks[0] != nil {
		t.Fatalf("the journal holds %v", marks)
	}
}

// A LINE WRITTEN BEFORE THE MARK, OR WITHOUT ONE, HAS NO ATTACHMENTS, however
// many paths and attachment-shaped lines its words hold.
func TestALegacyLineWithPathsHasNoAttachments(t *testing.T) {
	text := "Attached file: notes.md\n\nLink: https://example.com\n\nPasted text:\n```\nx\n```"
	data, _ := json.Marshal(sessionEntry{Type: "message", Role: "user", Content: text})
	record := ReadTranscriptBytes(append(data, '\n'))
	if len(record.Entries) != 1 || record.Entries[0].Text != text || record.Entries[0].Authored || record.Entries[0].Attachments != nil {
		t.Fatalf("the legacy line reads %#v", record.Entries)
	}
}

// A MARK SURVIVES EVERY OTHER WAY A PERSON'S LINE IS WRITTEN: a correction
// spliced into a running turn, and a compaction pass re-journaling the window
// — including a message whose model copy carries the skills block, which the
// pass re-writes as a new message without it.
func TestAMarkSurvivesSteeringAndCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	mark := func(said string, attachments ...MessageAttachment) *messagePresentation {
		m, err := Submission{Said: &said, Attachments: attachments}.mark()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	plain := userText("look\n\nAttached file: a.go")
	plain.person = mark("look", MessageAttachment{ID: "at1", Kind: "file", Path: "a.go"})
	block := "\n\n<skills>use them</skills>"
	skilled := userText("check\n\nAttached file: b.go" + block)
	skilled.said = "check\n\nAttached file: b.go"
	skilled.person = mark("check", MessageAttachment{ID: "at2", Kind: "file", Path: "b.go"})
	agent.presentation.remember(skilled.message, &messagePresentation{SkillsBlock: block})
	steer := userText("also\n\nAttached file: c.go")
	steer.person = mark("also", MessageAttachment{ID: "at3", Kind: "file", Path: "c.go"})
	steer.crossed = &SteerMark{Consumed: true, Landing: "read at the next step"}
	agent.mu.Lock()
	agent.recordUserLocked(plain)
	agent.recordUserLocked(skilled)
	agent.recordUserLocked(steer)
	agent.mu.Unlock()
	assert := func(where string, entries []DisplayEntry) {
		t.Helper()
		users := personEntries(entries)
		if len(users) != 3 {
			t.Fatalf("%s: %#v", where, users)
		}
		for i, want := range []struct{ text, path string }{{"look", "a.go"}, {"check", "b.go"}, {"also", "c.go"}} {
			if users[i].Text != want.text || !users[i].Authored || len(users[i].Attachments) != 1 || users[i].Attachments[0].Path != want.path {
				t.Fatalf("%s: message %d = %#v", where, i, users[i])
			}
		}
		if users[2].Steer == nil {
			t.Fatalf("%s: the correction lost its mark", where)
		}
	}
	assert("live", agent.Transcript())
	if got := agent.presentation.of(skilled.message); got == nil || got.SkillsBlock != block {
		t.Fatal("the person's mark took the skills block's provenance off the model's copy")
	}
	assert("at rest", ReadTranscript(path).Entries)
	window := append([]ai.Message(nil), agent.snapshot()[1:]...)
	agent.file.appendCompaction(compactionPass{}, 0, window)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed.messages) != 3 || strings.Contains(messageContentText(replayed.messages[1]), "<skills>") {
		t.Fatalf("the re-journaled window = %#v", replayed.messages)
	}
	assert("after compaction", shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation, steers: replayed.steers}))
}
