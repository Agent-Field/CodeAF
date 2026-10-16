package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// readsOf is what an entry says its words were written from.
func readsOf(entry DisplayEntry) []MessageRead {
	if entry.Person == nil {
		return nil
	}
	return entry.Person.Reads
}

// wholeRead is a read of two hundred references with one gone: complete, as a
// read that counted what was gone is, and naming the first eight it returned.
func wholeRead() MessageRead {
	read := MessageRead{
		Source:   MessageReference{Kind: "collection", ID: "c1", Name: "Specs"},
		Count:    200,
		Missing:  1,
		Complete: true,
		Warnings: []string{"one reference is gone"},
	}
	for i := 1; i <= MaxReadItems; i++ {
		n := strconv.Itoa(i)
		read.Items = append(read.Items, MessageReference{Kind: "file", ID: "r" + n, Name: "spec" + n + ".md", Owner: "fs", Path: "docs/spec" + n + ".md"})
	}
	return read
}

// partialRead is a read that could not ask about two references and was cut.
func partialRead() MessageRead {
	return MessageRead{
		Source:      MessageReference{Kind: "collection", ID: "c2", Name: "Links"},
		Count:       3,
		Unavailable: 2,
		Unread:      1,
		More:        true,
		Items:       []MessageReference{{Kind: "link", URL: "https://example.com/a"}},
	}
}

// WHAT A DOOR READ TO WRITE A MESSAGE IS KEPT ON THE MESSAGE'S OWN LINE, AS IT
// READ IT. A message written from two reads — one whole, one partial — sends the
// model exactly its text; the live transcript, the record at rest, the record's
// cache and the conversation reopened from the file all say the person's words
// and both reads with every count the door observed; and no reading shares a
// slice with any other, so a caller's scribble reaches no later one.
func TestAMessagesReadsAreKeptOnItsLineAndReadBackAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := answering()
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	said := "compare these specs"
	sent := "compare these specs\n\nSpecs:\n- docs/spec1.md\n- docs/spec2.md"
	given := []MessageRead{wholeRead(), partialRead()}
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: sent, Said: &said, Reads: given})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	// The caller's reads are its own: changing them afterwards reaches no record.
	given[0].Count, given[0].Items[0].Name, given[0].Warnings[0] = 7, "changed", "changed"

	want := []MessageRead{wholeRead(), partialRead()}
	assert := func(where string, entries []DisplayEntry) {
		t.Helper()
		users := personEntries(entries)
		if len(users) != 1 || users[0].Text != said || !authored(users[0]) {
			t.Fatalf("%s: the person's messages = %#v", where, users)
		}
		if got := readsOf(users[0]); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: reads = %#v, want %#v", where, got, want)
		}
	}
	completer.mu.Lock()
	first := completer.seen[0]
	completer.mu.Unlock()
	if got := messageContentText(first[len(first)-1]); got != sent {
		t.Fatalf("the model was sent %q, want %q", got, sent)
	}
	live := agent.Transcript()
	assert("live", live)
	scribble := func(entries []DisplayEntry) {
		reads := readsOf(personEntries(entries)[0])
		reads[0].Count, reads[0].Items[0].Name, reads[0].Warnings[0] = 9, "scribbled", "scribbled"
	}
	scribble(live)
	assert("read again", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	atRest := ReadTranscript(path)
	assert("at rest", atRest.Entries)
	scribble(atRest.Entries)
	assert("from the record's cache", ReadTranscript(path).Entries)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assert("bytes", ReadTranscriptBytes(data).Entries)
	marks := journalMarks(t, path)
	if len(marks) != 1 || marks[0] == nil || marks[0]["reads"] == nil || marks[0]["attachments"] != nil {
		t.Fatalf("the person's line carries %v", marks)
	}

	reopened, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	assert("reopened", reopened.Transcript())
	reopened.mu.Lock()
	replayed := messageContentText(reopened.messages[1])
	reopened.mu.Unlock()
	if replayed != sent {
		t.Fatalf("the reopened conversation reads %q to the model, want %q", replayed, sent)
	}
}

// A READ THAT DOES NOT FIT REFUSES THE MESSAGE WHOLE, and nothing of it is
// trimmed to fit: no line is written, the model is asked nothing, and the
// session takes the next message. A whole read that counted references that
// were gone is not one of them.
func TestReadsThatDoNotFitRefuseTheMessageWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := answering()
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	with := func(change func(*MessageRead)) []MessageRead {
		read := wholeRead()
		change(&read)
		return []MessageRead{read}
	}
	many := make([]MessageRead, MaxMessageReads+1)
	for i := range many {
		many[i] = partialRead()
	}
	heavy := make([]MessageRead, MaxMessageReads)
	for i := range heavy {
		heavy[i] = MessageRead{Source: MessageReference{Kind: "collection", Path: strings.Repeat("p", maxAttachmentField)}, Count: 2,
			Items: []MessageReference{{Kind: "file", Path: strings.Repeat("q", maxAttachmentField)}, {Kind: "file", Path: strings.Repeat("r", maxAttachmentField)}}}
	}
	for name, reads := range map[string][]MessageRead{
		"too many reads":       many,
		"too many items":       with(func(r *MessageRead) { r.Items = append(r.Items, r.Items[0]) }),
		"too many warnings":    with(func(r *MessageRead) { r.Warnings = make([]string, MaxReadWarnings+1); r.Warnings[0] = "w" }),
		"a negative count":     with(func(r *MessageRead) { r.Missing = -1 }),
		"a count past bound":   with(func(r *MessageRead) { r.Count = MaxReadCount + 1 }),
		"more items than read": with(func(r *MessageRead) { r.Count = 3 }),
		"complete, unavail":    with(func(r *MessageRead) { r.Unavailable = 1 }),
		"complete, unread":     with(func(r *MessageRead) { r.Unread = 1 }),
		"complete, cut":        with(func(r *MessageRead) { r.More = true }),
		"a source of no kind":  with(func(r *MessageRead) { r.Source.Kind = " " }),
		"a nameless source":    with(func(r *MessageRead) { r.Source = MessageReference{Kind: "collection", Owner: "me"} }),
		"a nameless item":      with(func(r *MessageRead) { r.Items[3] = MessageReference{Kind: "file"} }),
		"a long field":         with(func(r *MessageRead) { r.Items[0].Path = strings.Repeat("p", maxAttachmentField+1) }),
		"a padded field":       with(func(r *MessageRead) { r.Source.Name = strings.Repeat(" ", maxAttachmentField) + "Specs" }),
		"a long warning":       with(func(r *MessageRead) { r.Warnings[0] = strings.Repeat("w", maxReadWarning+1) }),
		"an empty warning":     with(func(r *MessageRead) { r.Warnings[0] = "  " }),
		"a warning not text":   with(func(r *MessageRead) { r.Warnings[0] = "\xff" }),
		"too many bytes":       heavy,
	} {
		if _, err := agent.SubmitMessage(context.Background(), Submission{Text: "x", Reads: reads}); !errors.Is(err, ErrMessageMark) {
			t.Fatalf("%s: SubmitMessage = %v", name, err)
		}
		if _, err := agent.FollowUpMessage(Submission{Text: "x", Reads: reads}); !errors.Is(err, ErrMessageMark) {
			t.Fatalf("%s: FollowUpMessage = %v", name, err)
		}
		if _, err := agent.SteerMessage(Submission{Text: "x", Reads: reads}); !errors.Is(err, ErrMessageMark) {
			t.Fatalf("%s: SteerMessage = %v", name, err)
		}
	}
	completer.mu.Lock()
	asked := len(completer.seen)
	completer.mu.Unlock()
	if asked != 0 || len(personEntries(agent.Transcript())) != 0 {
		t.Fatalf("a refused message reached the model %d times or the record", asked)
	}
	events, err := agent.SubmitMessage(context.Background(), Submission{Text: "x", Reads: []MessageRead{wholeRead()}})
	if err != nil {
		t.Fatalf("a whole read that counted gone references = %v", err)
	}
	collect(t, events)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	marks := journalMarks(t, path)
	if len(marks) != 1 || marks[0]["reads"] == nil {
		t.Fatalf("the journal holds %v", marks)
	}
	users := personEntries(ReadTranscript(path).Entries)
	// Reads alone do not make the words someone else's: Text was theirs.
	if len(users) != 1 || users[0].Text != "x" || authored(users[0]) || !reflect.DeepEqual(readsOf(users[0]), []MessageRead{wholeRead()}) {
		t.Fatalf("read back %#v", users)
	}
}

// A READ SURVIVES EVERY OTHER WAY A PERSON'S LINE IS WRITTEN: a correction
// spliced into a running turn, a correction that fell through and became a
// message of its own, a follow-up, and a compaction pass re-journaling the
// window — including the skills-free copy the pass writes in a message's place.
func TestReadsSurviveSteeringFallThroughFollowUpAndCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	read := func(id string) []MessageRead {
		return []MessageRead{{Source: MessageReference{Kind: "collection", ID: id}, Count: 1, Complete: true, Items: []MessageReference{{Kind: "file", Path: id + ".go"}}}}
	}
	mark := func(said, id string) *messagePresentation {
		m, err := Submission{Said: &said, Reads: read(id)}.mark()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	block := "\n\n<skills>use them</skills>"
	skilled := userText("check a.go" + block)
	skilled.said = "check a.go"
	skilled.person = mark("check", "a")
	agent.presentation.remember(skilled.message, &messagePresentation{SkillsBlock: block})
	steer := userText("also b.go")
	steer.person = mark("also", "b")
	steer.crossed = &SteerMark{Consumed: true, Landing: "read at the next step"}

	// The fallen-through correction is lifted out of the steering queue the
	// way a finished turn lifts it, and drained as the follow-up it becomes.
	fell := steerMessage(&turnSteer{note: SteerNote{ID: 1, Words: "then c.go"}, stream: newEventStream()})
	fell.person = mark("then", "c")
	agent.mu.Lock()
	agent.recordUserLocked(skilled)
	agent.recordUserLocked(steer)
	agent.steering = append(agent.steering, fell)
	agent.liftSteersLocked(newEventHub())
	if len(agent.followups) != 1 || agent.followups[0].message.person == nil {
		agent.mu.Unlock()
		t.Fatalf("the fallen-through correction became %#v", agent.followups)
	}
	became := agent.followups[0].message
	agent.followups = nil
	agent.recordUserLocked(became)
	agent.mu.Unlock()

	want := []struct{ text, id string }{{"check", "a"}, {"also", "b"}, {"then", "c"}}
	assert := func(where string, entries []DisplayEntry) {
		t.Helper()
		users := personEntries(entries)
		if len(users) != len(want) {
			t.Fatalf("%s: %#v", where, users)
		}
		for i, w := range want {
			if users[i].Text != w.text || !authored(users[i]) || !reflect.DeepEqual(readsOf(users[i]), read(w.id)) {
				t.Fatalf("%s: message %d = %#v", where, i, users[i])
			}
		}
		if users[1].Steer == nil {
			t.Fatalf("%s: the correction lost its steer mark", where)
		}
	}
	assert("live", agent.Transcript())
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
	if len(replayed.messages) != len(want) || strings.Contains(messageContentText(replayed.messages[0]), "<skills>") {
		t.Fatalf("the re-journaled window = %#v", replayed.messages)
	}
	assert("after compaction", shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation, steers: replayed.steers}))
	reopened, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	assert("reopened after compaction", reopened.Transcript())
}

// A FOLLOW-UP SENT WITH READS KEEPS THEM, through the real door: an idle
// session starts its turn at once and records the mark on the message's line.
func TestAFollowUpMessageKeepsItsReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	said := "next"
	events, err := agent.FollowUpMessage(Submission{Text: "next\n\n- a.go", Said: &said, Reads: []MessageRead{partialRead()}})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	users := personEntries(ReadTranscript(path).Entries)
	if len(users) != 1 || users[0].Text != said || !reflect.DeepEqual(readsOf(users[0]), []MessageRead{partialRead()}) {
		t.Fatalf("read back %#v", users)
	}
}

// A CORRECTION SENT WITH READS INTO A RUNNING TURN KEEPS THEM on its steer
// line, through the real door: the reply is cut, the words land at the
// boundary as the model's, and the record shows the person's words and reads.
func TestASteerMessageKeepsItsReadsOnItsSteerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	started := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "starting")
			close(started)
			<-ctx.Done()
			return &ai.Response{}, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	turn := mustSubmit(t, agent, "start")
	<-started
	said := "use these"
	sent := "use these\n\n- spec1.md"
	steered, err := agent.SteerMessage(Submission{Text: sent, Said: &said, Reads: []MessageRead{wholeRead()}})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, turn)
	collect(t, steered)
	if got := userLines(completer.request(1)); len(got) != 2 || got[1] != sent {
		t.Fatalf("the model was sent %q", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	users := personEntries(ReadTranscript(path).Entries)
	if len(users) != 2 || users[1].Steer == nil || users[1].Text != said || !reflect.DeepEqual(readsOf(users[1]), []MessageRead{wholeRead()}) {
		t.Fatalf("read back %#v", users)
	}
}

// A READING'S READS ARE ITS OWN, in the record cache's copy as in a shaping:
// no slice of a clone is the original's.
func TestACloneOfAnEntrySharesNoRead(t *testing.T) {
	entries := []DisplayEntry{{Role: "user", Person: &PersonMark{Reads: []MessageRead{wholeRead()}}}}
	clone := cloneEntries(entries)
	original, copied := entries[0].Person.Reads, clone[0].Person.Reads
	if !reflect.DeepEqual(original, copied) {
		t.Fatal("the clone differs")
	}
	if &original[0] == &copied[0] || &original[0].Items[0] == &copied[0].Items[0] || &original[0].Warnings[0] == &copied[0].Warnings[0] {
		t.Fatal("the clone shares a read's slices")
	}
}
