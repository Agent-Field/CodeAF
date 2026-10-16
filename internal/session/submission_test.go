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
	"unsafe"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// answering is a completer that answers every request "done".
func answering() *scriptedCompleter {
	answer := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }
	return &scriptedCompleter{steps: []step{answer, answer, answer, answer, answer, answer}}
}

// authored says an entry's Text is the person's own words as their door said them.
func authored(entry DisplayEntry) bool { return entry.Person != nil && entry.Person.Authored }

// An ordinary new message carries only its actual identity. It must not gain
// invented authorship, attachments, reads, media or delivery facts.
func ordinaryIdentity(entry DisplayEntry, queued bool) bool {
	p := entry.Person
	if p == nil || p.Authored || p.Queued != queued || len(p.Attachments) != 0 || len(p.Reads) != 0 || p.Composed == nil || p.Composed.MessageID == "" {
		return false
	}
	copy := *p.Composed
	copy.MessageID = ""
	return reflect.DeepEqual(copy, ComposedMark{})
}
func journalIdentity(mark map[string]any) string {
	composed, ok := mark["composed"].(map[string]any)
	if !ok || len(composed) != 1 {
		return ""
	}
	id, _ := composed["messageId"].(string)
	return id
}

// attachmentsOf is what an entry says rode on the person's message.
func attachmentsOf(entry DisplayEntry) []MessageAttachment {
	if entry.Person == nil {
		return nil
	}
	return entry.Person.Attachments
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
		{ID: "at2", Kind: "paste", Name: "Pasted text", Bytes: ptr(int64(31)), Lines: ptr(2)},
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
	if users[0].Text != "why does this fail?" || !authored(users[0]) || !reflect.DeepEqual(attachmentsOf(users[0]), submittedFiles) {
		t.Fatalf("%s: the message with words = %#v", where, users[0])
	}
	if users[1].Text != "" || !authored(users[1]) || !reflect.DeepEqual(attachmentsOf(users[1]), submittedAlone) {
		t.Fatalf("%s: the attachments alone = %#v", where, users[1])
	}
	if users[2].Text != "see Attached file: other.go" || authored(users[2]) || !ordinaryIdentity(users[2], false) {
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
	personEntries(live)[0].Person.Attachments[0].Name = "scribbled"
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
	if len(marks) != 3 || marks[0] == nil || marks[1] == nil || journalIdentity(marks[2]) == "" {
		t.Fatalf("the person's lines carry marks %v", marks)
	}
	for _, mark := range marks[:2] {
		for key := range mark {
			if key != "audience" && key != "text" && key != "attachments" && key != "composed" {
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
		"negative size": {Text: "x", Attachments: []MessageAttachment{{Kind: "paste", Bytes: ptr(int64(-1))}}},
		"long field":    {Text: "x", Attachments: []MessageAttachment{{Kind: "file", Path: strings.Repeat("p", maxAttachmentField+1)}}},
	} {
		if _, err := agent.SubmitMessage(context.Background(), message); !errors.Is(err, ErrMessageMark) {
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
	if len(users) != 1 || users[0].Text != "plain" || authored(users[0]) || !ordinaryIdentity(users[0], false) {
		t.Fatalf("a submission with no mark read back as %#v", users)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if marks := journalMarks(t, path); len(marks) != 1 || journalIdentity(marks[0]) == "" || len(marks[0]) != 2 {
		t.Fatalf("the journal holds %v", marks)
	}
}

// A LINE WRITTEN BEFORE THE MARK, OR WITHOUT ONE, HAS NO ATTACHMENTS, however
// many paths and attachment-shaped lines its words hold.
func TestALegacyLineWithPathsHasNoAttachments(t *testing.T) {
	text := "Attached file: notes.md\n\nLink: https://example.com\n\nPasted text:\n```\nx\n```"
	data, _ := json.Marshal(sessionEntry{Type: "message", Role: "user", Content: text})
	record := ReadTranscriptBytes(append(data, '\n'))
	if len(record.Entries) != 1 || record.Entries[0].Text != text || record.Entries[0].Person != nil {
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
			if users[i].Text != want.text || !authored(users[i]) || len(attachmentsOf(users[i])) != 1 || attachmentsOf(users[i])[0].Path != want.path {
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

// indexHeld is what a presentation index holds alive: its marks, and the bytes
// of the first part each mark's key keeps reachable.
func indexHeld(p *presentationIndex) (marks, bytes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key := range p.entries {
		bytes += len(key.Text)
	}
	return len(p.entries), bytes
}

// liveMarked is how many of the transcript's messages carry a mark, and their
// first parts' bytes.
func liveMarked(agent *Agent) (marks, bytes int) {
	for _, message := range agent.messages {
		if agent.presentation.of(message) != nil {
			marks++
			bytes += len(message.Content[0].Text)
		}
	}
	return marks, bytes
}

// A SESSION HOLDS THE MARKS OF WHAT IT HOLDS, AND NO MORE. Round after round of
// marked messages carrying large pastes is compacted away, and one more is cut
// by a rewind; after every boundary the index holds exactly the marks of
// messages still in the transcript — never the folded or cut ones, whose
// pastes it would otherwise keep alive for the life of the session — and the
// record, read again, still holds every message's own words and attachments.
func TestMarksAreLetGoWithTheMessagesTheyDescribe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &summarizer{}, func(c *Config) { c.SessionFile = path; c.ContextWindow = 16_384 })
	paste := strings.Repeat("pasted log line ", 24_000/16)
	send := func(said string) {
		t.Helper()
		user := userText(said + "\n\nPasted text:\n```\n" + paste + "\n```")
		var err error
		user.person, err = Submission{Said: &said, Attachments: []MessageAttachment{{ID: said, Kind: "paste", Bytes: ptr(int64(len(paste)))}}}.mark()
		if err != nil {
			t.Fatal(err)
		}
		agent.mu.Lock()
		agent.recordUserLocked(user)
		agent.recordLocked(textMessage("assistant", "answer to "+said))
		agent.mu.Unlock()
	}
	bounded := func(when string) {
		t.Helper()
		agent.mu.Lock()
		defer agent.mu.Unlock()
		marks, bytes := indexHeld(agent.presentation)
		live, liveBytes := liveMarked(agent)
		if marks != live || bytes != liveBytes {
			t.Fatalf("%s: the index holds %d marks over %d bytes; the transcript holds %d over %d", when, marks, bytes, live, liveBytes)
		}
	}
	var sent []string
	const rounds = 5
	for round := 1; round <= rounds; round++ {
		for i := 1; i <= 5; i++ {
			said := "question " + string(rune('0'+round)) + "." + string(rune('0'+i))
			send(said)
			sent = append(sent, said)
		}
		before, _ := indexHeld(agent.presentation)
		if err := agent.Compact(context.Background()); err != nil {
			t.Fatalf("round %d: compact = %v", round, err)
		}
		if after, _ := indexHeld(agent.presentation); after >= before {
			t.Fatalf("round %d: the pass let go of no marks (%d before, %d after)", round, before, after)
		}
		bounded("after compaction")
	}
	send("cut")
	if _, err := agent.Rewind(); err != nil {
		t.Fatal(err)
	}
	bounded("after a rewind")
	if marks, _ := indexHeld(agent.presentation); marks > 5 {
		t.Fatalf("after %d rounds the index still holds %d marks", rounds, marks)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	record := ReadTranscript(path)
	told := append(append([]DisplayEntry(nil), record.Earlier...), record.Entries[record.Floor:]...)
	seen := map[string]int{}
	for _, entry := range personEntries(told) {
		if !authored(entry) || len(attachmentsOf(entry)) != 1 || attachmentsOf(entry)[0].ID != entry.Text || attachmentsOf(entry)[0].Bytes == nil || *attachmentsOf(entry)[0].Bytes != int64(len(paste)) {
			t.Fatalf("a message read back as %#v", entry)
		}
		seen[entry.Text]++
	}
	for _, said := range sent {
		if seen[said] != 1 {
			t.Fatalf("%q was read back %d times; the record holds %v", said, seen[said], seen)
		}
	}
	reopened, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	reopened.mu.Lock()
	marks, _ := indexHeld(reopened.presentation)
	live, _ := liveMarked(reopened)
	reopened.mu.Unlock()
	if marks != live {
		t.Fatalf("a reopened session holds %d marks for %d messages it carries", marks, live)
	}
}

// A BOUND IS A BOUND ON WHAT WAS GIVEN. Padding cannot carry a field or the
// person's words past their bound by being trimmed off, and what is kept is a
// copy that holds nothing of the caller's string alive.
func TestAMarksBoundsAreOnTheBytesGiven(t *testing.T) {
	padded := strings.Repeat(" ", maxAttachmentField) + "a.go"
	if _, err := (Submission{Attachments: []MessageAttachment{{Kind: "file", Path: padded}}}).mark(); !errors.Is(err, ErrMessageMark) {
		t.Fatalf("a padded path = %v", err)
	}
	said := strings.Repeat("\n", maxSaidBytes) + "words"
	if _, err := (Submission{Said: &said}).mark(); !errors.Is(err, ErrMessageMark) {
		t.Fatalf("padded words = %v", err)
	}
	path := "  " + strings.Repeat("d/", 100) + "a.go  "
	words := "  hello  "
	mark, err := Submission{Said: &words, Attachments: []MessageAttachment{{Kind: " file ", Path: path}}}.mark()
	if err != nil {
		t.Fatal(err)
	}
	kept := mark.Attachments[0].Path
	given := unsafe.StringData(path)
	at := uintptr(unsafe.Pointer(unsafe.StringData(kept)))
	if kept != strings.TrimSpace(path) || mark.Attachments[0].Kind != "file" || *mark.Text != "hello" ||
		(at >= uintptr(unsafe.Pointer(given)) && at < uintptr(unsafe.Pointer(given))+uintptr(len(path))) {
		t.Fatalf("the kept fields = %#v, %q", mark.Attachments[0], *mark.Text)
	}
}

// A MEASURED ZERO IS NOT AN UNMEASURED SIZE. An empty file the door measured
// reads back as zero bytes, and one it did not measure reads back with no size.
func TestAMeasuredZeroSizeIsKeptApartFromAnUnknownOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	said := "two files"
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: "two files\n\nAttached file: empty\n\nAttached file: big", Said: &said, Attachments: []MessageAttachment{
		{Kind: "file", Path: "empty", Bytes: ptr(int64(0)), Lines: ptr(0)},
		{Kind: "file", Path: "big"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	users := personEntries(ReadTranscript(path).Entries)
	if len(users) != 1 || len(attachmentsOf(users[0])) != 2 {
		t.Fatalf("read back %#v", users)
	}
	empty, big := attachmentsOf(users[0])[0], attachmentsOf(users[0])[1]
	if empty.Bytes == nil || *empty.Bytes != 0 || empty.Lines == nil || *empty.Lines != 0 || big.Bytes != nil || big.Lines != nil {
		t.Fatalf("the sizes read back as %#v and %#v", empty, big)
	}
}

// AN ENTRY DOES NOT GROW FOR A MARK MOST ENTRIES NEVER CARRY. A record holds
// one entry per journal line and the record cache's reserve is a few times the
// file, so eight bytes more on every entry is a journal of short lines that
// stops being kept. The mark is one pointer, paid for by the padding the bools
// beside it used to waste.
func TestAPersonsMarkCostsAnEntryNothing(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("the figure is a 64-bit layout")
	}
	// RunID adds 16 bytes; packing Answered with the other bools recovers 8.
	if size := unsafe.Sizeof(DisplayEntry{}); size > 296 {
		t.Fatalf("DisplayEntry is %d bytes; budget is 296 including the producing turn", size)
	}
}

// A MESSAGE THE DOOR DELIVERED FROM THE PERSON'S QUEUE SAYS SO ON ITS OWN LINE,
// and nothing else does: the same words sent at once carry no mark at all. The
// live reading, the record at rest and the reopened conversation agree, and
// the model reads the words alone.
func TestAQueuedMessageSaysSoOnItsLineAndASentOneDoesNot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := answering()
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	for _, message := range []Submission{{Text: "run the tests"}, {Text: "then the lint", Queued: true}} {
		events, err := agent.SubmitMessage(context.Background(), message)
		if err != nil {
			t.Fatal(err)
		}
		collect(t, events)
	}
	completer.mu.Lock()
	last := completer.seen[len(completer.seen)-1]
	completer.mu.Unlock()
	if got := messageContentText(last[len(last)-1]); got != "then the lint" {
		t.Fatalf("the model read %q", got)
	}
	check := func(where string, entries []DisplayEntry) {
		t.Helper()
		users := personEntries(entries)
		if len(users) != 2 || !ordinaryIdentity(users[0], false) || !ordinaryIdentity(users[1], true) || users[1].Person == nil || !users[1].Person.Queued ||
			users[1].Person.Authored || users[1].Text != "then the lint" {
			t.Fatalf("%s: the person's messages = %#v", where, users)
		}
	}
	check("live", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	check("at rest", ReadTranscript(path).Entries)
	if marks := journalMarks(t, path); len(marks) != 2 || journalIdentity(marks[0]) == "" || len(marks[0]) != 2 || journalIdentity(marks[1]) == "" || journalIdentity(marks[0]) == journalIdentity(marks[1]) || marks[1]["queued"] != true || marks[1]["text"] != nil {
		t.Fatalf("the person's lines carry marks %v", marks)
	}
	reopened, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	check("reopened", reopened.Transcript())
}
