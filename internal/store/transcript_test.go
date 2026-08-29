package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func transcriptStore(t *testing.T) *Store {
	t.Helper()
	graph := openTestStore(t, filepath.Join(t.TempDir(), "transcript.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "leaf", Brief: "run a long loop", Title: "Leaf"},
	}}, Provenance{Origin: OriginUser, Intent: "run a long loop"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

var oneTurn = []TranscriptEntry{
	{Turn: 1, Kind: TranscriptAssistant, Text: "I'll read the file first."},
	{Turn: 1, Kind: TranscriptToolCall, Tool: "read", CallID: "c1", Text: `{"filePath":"/x/notes.md"}`},
	{Turn: 1, Kind: TranscriptToolResult, Tool: "read", CallID: "c1", Text: "the notes", Millis: 12},
}

// The whole point of the table: a leaf's turns come back in the order they
// happened, told apart by kind, filed under the node that ran them.
func TestATranscriptComesBackInTheOrderItHappened(t *testing.T) {
	graph := transcriptStore(t)
	if err := graph.RecordTranscript("leaf", "worker/model", oneTurn); err != nil {
		t.Fatal(err)
	}
	// A second flush, the way a live leaf writes: the record is appended to
	// during the run, not composed at the end.
	if err := graph.RecordTranscript("leaf", "worker/model", []TranscriptEntry{
		{Turn: 2, Kind: TranscriptAssistant, Text: "Done."},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		turn int
		kind TranscriptKind
		tool string
	}{
		{1, TranscriptAssistant, ""},
		{1, TranscriptToolCall, "read"},
		{1, TranscriptToolResult, "read"},
		{2, TranscriptAssistant, ""},
	}
	if len(entries) != len(want) {
		t.Fatalf("the record has %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for index, entry := range entries {
		if entry.Turn != want[index].turn || entry.Kind != want[index].kind || entry.Tool != want[index].tool {
			t.Fatalf("entry %d is turn %d %s %q, want turn %d %s %q",
				index, entry.Turn, entry.Kind, entry.Tool,
				want[index].turn, want[index].kind, want[index].tool)
		}
	}
	if entries[1].CallID != entries[2].CallID {
		t.Fatalf("the call and its result carry different ids (%q, %q); nothing can pair them",
			entries[1].CallID, entries[2].CallID)
	}
	if entries[2].Millis != 12 {
		t.Fatalf("the result forgot how long the tool took: %dms", entries[2].Millis)
	}
}

// The additive law, and it is the reason this is a table rather than more rows
// in messages. Every reader of a node's messages — a running leaf's steering
// mailbox, the pickup bank, the narrator, both node views — means "what was
// said about this work" by them. A worker's internal loop is not that, and a
// transcript that moved any of those readers would have broken all of them
// silently.
func TestATranscriptStaysOutOfTheConversation(t *testing.T) {
	graph := transcriptStore(t)
	if _, err := graph.PostMessage(Message{Role: RoleUser, NodeID: "leaf",
		Body: "prefer the shorter form"}); err != nil {
		t.Fatal(err)
	}
	before, err := graph.NodeMessages("leaf", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTranscript("leaf", "worker/model", oneTurn); err != nil {
		t.Fatal(err)
	}
	after, err := graph.NodeMessages("leaf", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("recording a transcript moved the node's conversation from %d messages to %d",
			len(before), len(after))
	}
}

// A tool that printed fifty kilobytes is journaled as its head and its tail
// with the gap named. Head-only would keep the part a reader already guessed
// and throw away the exit code they opened the record for.
func TestATranscriptEntryKeepsItsHeadAndItsTail(t *testing.T) {
	graph := transcriptStore(t)
	body := "START-OF-OUTPUT\n" + strings.Repeat("filler line that says nothing\n", 4000) + "EXIT-CODE-1"
	if len(body) <= MaxTranscriptTextBytes {
		t.Fatalf("the fixture is only %d bytes; it has to exceed the %d-byte bound to test it",
			len(body), MaxTranscriptTextBytes)
	}
	if err := graph.RecordTranscript("leaf", "worker/model", []TranscriptEntry{
		{Turn: 1, Kind: TranscriptToolResult, Tool: "bash", CallID: "c1", Text: body},
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the record has %d entries, want 1", len(entries))
	}
	kept := entries[0].Text
	if len(kept) > MaxTranscriptTextBytes {
		t.Fatalf("the entry is %d bytes; the bound is %d", len(kept), MaxTranscriptTextBytes)
	}
	if !strings.HasPrefix(kept, "START-OF-OUTPUT") {
		t.Fatalf("the head is gone: %.40q", kept)
	}
	if !strings.HasSuffix(kept, "EXIT-CODE-1") {
		t.Fatalf("the tail is gone, which is the half that says how it ended: %.40q", kept[len(kept)-40:])
	}
	if !strings.Contains(kept, "bytes elided") {
		t.Fatal("the entry was cut without saying so, which is how a truncated record reads as a short one")
	}
}

// A batch larger than one event's bound is refused rather than clipped.
// Silently dropping the tail of a batch is precisely the quiet loss this table
// exists to end.
func TestAnOversizeBatchIsRefusedRatherThanClipped(t *testing.T) {
	graph := transcriptStore(t)
	batch := make([]TranscriptEntry, MaxTranscriptBatch+1)
	for index := range batch {
		batch[index] = TranscriptEntry{Turn: 1, Kind: TranscriptNote, Text: "tick"}
	}
	err := graph.RecordTranscript("leaf", "worker/model", batch)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("an oversize batch was accepted or failed wrongly: %v", err)
	}
}

// Journal-is-truth: the table is a projection, and a rebuild that replays the
// journal has to put every entry back exactly where it was.
func TestATranscriptSurvivesARebuild(t *testing.T) {
	graph := transcriptStore(t)
	if err := graph.RecordTranscript("leaf", "worker/model", oneTurn); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(oneTurn) {
		t.Fatalf("the rebuild kept %d entries of %d", len(entries), len(oneTurn))
	}
	for index, entry := range entries {
		if entry.Kind != oneTurn[index].Kind || entry.Text != oneTurn[index].Text {
			t.Fatalf("entry %d came back as %+v, want %+v", index, entry, oneTurn[index])
		}
	}
}

// The transcript is bulk evidence carried in the journal, not a decision in it.
// Four readers page the whole journal from sequence one to derive a decision,
// and none of them wants a megabyte of tool output per leaf — while a rebuild,
// which reads the events table directly, still has to see every entry or the
// record would not survive one.
func TestTheJournalsDecisionReadersDoNotPayForTranscripts(t *testing.T) {
	graph := transcriptStore(t)
	before, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTranscript("leaf", "worker/model", oneTurn); err != nil {
		t.Fatal(err)
	}
	after, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a transcript put %d extra events in front of every decision reader",
			len(after)-len(before))
	}
	watermark, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	windowed, err := graph.EventsThrough(0, watermark)
	if err != nil {
		t.Fatal(err)
	}
	if len(windowed) != len(before) {
		t.Fatalf("the bounded reader carried %d transcript events", len(windowed)-len(before))
	}
	// And the rebuild, which reads the table rather than this function, still
	// puts every entry back. TestATranscriptSurvivesARebuild is the assertion;
	// this line is the reason the exclusion above is safe.
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(oneTurn) {
		t.Fatalf("hiding transcripts from Events also hid them from the rebuild: %d entries of %d",
			len(entries), len(oneTurn))
	}
}
