package exec

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// countingJournal stands in for the store: it keeps the batches it was handed,
// so a test can see WHEN the recorder wrote as well as what.
type countingJournal struct {
	mu      sync.Mutex
	batches [][]store.TranscriptEntry
	fail    error
}

func (j *countingJournal) RecordTranscript(nodeID, model string, entries []store.TranscriptEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.fail != nil {
		return j.fail
	}
	j.batches = append(j.batches, entries)
	return nil
}

func (j *countingJournal) entries() []store.TranscriptEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	var all []store.TranscriptEntry
	for _, batch := range j.batches {
		all = append(all, batch...)
	}
	return all
}

func note(text string) store.TranscriptEntry {
	return store.TranscriptEntry{Turn: 1, Kind: store.TranscriptNote, Text: text}
}

// The recorder writes DURING the run rather than at the end of it. A record
// composed at settlement time would be lost by every leaf that never settles,
// which is the failure the whole mechanism exists for.
func TestTheRecorderWritesBeforeTheRunIsOver(t *testing.T) {
	journal := &countingJournal{}
	sink := NewTranscriptRecorder(journal, "leaf", "worker/model")
	for index := 0; index < store.MaxTranscriptBatch; index++ {
		sink.Record(note("tick"))
	}
	if got := len(journal.entries()); got != store.MaxTranscriptBatch {
		t.Fatalf("a full batch was still in memory: the journal holds %d of %d entries",
			got, store.MaxTranscriptBatch)
	}
	sink.Record(note("one more"))
	if got := len(journal.entries()); got != store.MaxTranscriptBatch {
		t.Fatalf("a partial batch was written early: %d entries", got)
	}
	sink.Flush()
	if got := len(journal.entries()); got != store.MaxTranscriptBatch+1 {
		t.Fatalf("the flush left %d entries, want %d", got, store.MaxTranscriptBatch+1)
	}
	// Flushing twice is what the runner and the worker between them will always
	// do, and it must not write the same entries again.
	sink.Flush()
	if got := len(journal.entries()); got != store.MaxTranscriptBatch+1 {
		t.Fatalf("a second flush re-wrote the record: %d entries", got)
	}
}

// A runaway leaf must not turn the database into its own log file — and where
// the record stops, it has to SAY it stops, or a reader takes a truncated
// record for a short run.
func TestTheRecordSaysWhereItStops(t *testing.T) {
	journal := &countingJournal{}
	sink := NewTranscriptRecorder(journal, "leaf", "worker/model")
	for index := 0; index < store.MaxTranscriptEntries+50; index++ {
		sink.Record(note("tick"))
	}
	sink.Flush()
	entries := journal.entries()
	if len(entries) > store.MaxTranscriptEntries+1 {
		t.Fatalf("the cap let %d entries through; the bound is %d", len(entries), store.MaxTranscriptEntries)
	}
	last := entries[len(entries)-1]
	if last.Kind != store.TranscriptElided || !strings.Contains(last.Text, "cap") {
		t.Fatalf("the record stopped without saying so: %+v", last)
	}
}

// A store that refuses one batch will refuse the next sixty. A leaf that cannot
// write its record must still finish its work, and must not spend the rest of
// the run failing at the same write.
func TestAFailingJournalDoesNotStopTheLeaf(t *testing.T) {
	journal := &countingJournal{fail: errors.New("database is locked")}
	sink := NewTranscriptRecorder(journal, "leaf", "worker/model")
	for index := 0; index < 5*store.MaxTranscriptBatch; index++ {
		sink.Record(note("tick"))
	}
	sink.Flush()
}

// Nobody listening is the ordinary case, and it has to be distinguishable from
// a live sink so a loop can skip building an entry it would only throw away.
func TestAContextWithNoJournalCarriesNoSink(t *testing.T) {
	if sink := TranscriptFrom(context.Background()); sink != nil {
		t.Fatalf("a bare context produced a sink: %#v", sink)
	}
	ctx := WithTranscript(context.Background(), NewTranscriptRecorder(nil, "leaf", "model"))
	if sink := TranscriptFrom(ctx); sink != nil {
		t.Fatalf("a recorder with nowhere to write became a live sink: %#v", sink)
	}
	// And flushing a context that has none is a no-op rather than a panic: the
	// runner flushes unconditionally on every way out of a leaf.
	FlushTranscript(ctx)
}
