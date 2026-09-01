package exec

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The seam by which a worker's turns reach the record.
//
// A leaf used to be observable only by what it left on disk. When it left
// nothing — and a leaf could burn 1.29M prompt tokens and fifty cents and leave
// nothing — there was no way to say what it had done,
// because its transcript lived in a slice that went away with the process. The
// fix is not a log file: a log file is not addressable by node, is not there
// when someone asks a week later, and is not what the rest of this system means
// by a record. The fix is that the loop tells somebody, every turn, and that
// somebody writes it down under the node.
//
// WHY A CONTEXT VALUE AND NOT A FIELD ON THE EXECUTOR. The sink has to reach
// every worker on both surfaces — the chat's leaves and `aforge do`'s — and
// those two reach their workers through different doors: cmd/aforge's
// executorFor on one side and an exec.Registry built once per run on the other.
// A constructor field would have to be threaded through both, through
// leafBuild, and through every executor that does not want one; and it would be
// wrong for the registry, whose executors are built once and shared by every
// node while a sink belongs to ONE attempt at ONE node. The context is already
// per-attempt and already carries the call's shape (provider.WithCallShape) and
// its observer (provider.WithStreamObserver) for exactly these reasons. This is
// that idiom, one rung up.
//
// Nobody listening is the normal case — a unit test, a bench harness, any
// caller with no store — and it costs one type assertion and a nil check per
// turn.

// TranscriptSink receives one leaf's turns as they happen.
//
// Record is called from the loop goroutine, and may be called concurrently by
// tools that run in parallel, so an implementation must be safe for concurrent
// use. Flush makes everything recorded so far durable and must be safe to call
// more than once, including after the run is over: the runner flushes on every
// way out of a leaf, and a leaf that is abandoned on the clock goes on
// recording into a sink that has already been flushed once.
type TranscriptSink interface {
	Record(entry store.TranscriptEntry)
	Flush()
}

type transcriptContextKey struct{}

// WithTranscript arms one attempt's transcript. The sink belongs to the
// attempt, not to the worker: a retried leaf gets a fresh one, so its second
// run appends behind its first rather than merging with it.
func WithTranscript(ctx context.Context, sink TranscriptSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, transcriptContextKey{}, sink)
}

// TranscriptFrom is the sink this context carries, or nil when nobody is
// listening. Nil is returned rather than a no-op sink on purpose: a caller that
// holds nil can skip building the entry at all, and building an entry means
// copying and truncating a tool result that may be fifty kilobytes.
func TranscriptFrom(ctx context.Context) TranscriptSink {
	sink, _ := ctx.Value(transcriptContextKey{}).(TranscriptSink)
	return sink
}

// FlushTranscript makes this context's transcript durable, if it has one. It is
// what a runner calls on every way out of a leaf — the ordinary return, the
// watchdog, the recovered panic — so that whatever the worker had already done
// survives the way it ended.
func FlushTranscript(ctx context.Context) {
	if sink := TranscriptFrom(ctx); sink != nil {
		sink.Flush()
	}
}

// TranscriptJournal is the narrow slice of *store.Store the recorder writes
// through. It is an interface so a test can hold the batches without a
// database, and so this package's dependency on the store stays one method
// wide.
type TranscriptJournal interface {
	RecordTranscript(nodeID, model string, entries []store.TranscriptEntry) error
}

// TranscriptRecorder is the store-backed sink: it batches a leaf's entries and
// writes each batch to the journal under the node.
//
// IT BATCHES, AND IT DOES NOT WAIT UNTIL THE END. One event at settlement time
// would be cheaper and would lose the whole record of every leaf that died
// before settling — which is the failure this machinery exists for. So a full
// batch is written the moment it fills, and the runner flushes the remainder on
// every exit. A hard kill of the process costs at most the last partial batch.
//
// THE WRITE HAPPENS UNDER THE LOCK, synchronously, in the caller's goroutine.
// An async writer would buy back a few milliseconds per sixty-four entries and
// would owe a shutdown handshake that a panicking leaf cannot perform; the
// turns it records are separated by provider round-trips measured in seconds,
// so there is no hot path here to protect.
type TranscriptRecorder struct {
	journal TranscriptJournal
	nodeID  string
	model   string

	mu      sync.Mutex
	pending []store.TranscriptEntry
	// written counts entries already handed to the journal, and bytes counts
	// the payload of every entry accepted. Together they are the cap.
	written int
	bytes   int
	// sealed says the cap was reached and the elision marker already recorded.
	// Everything after it is dropped, silently by design: the marker has
	// already said that the record stops here.
	sealed bool
	// faulted keeps a failing journal from becoming a failing log. A store that
	// refuses one batch will refuse the next sixty, and a leaf that cannot
	// write its record must still finish its work.
	faulted bool
}

// NewTranscriptRecorder builds the sink for one attempt at one node. The model
// is the one the leaf was dispatched on, journaled beside the entries so a
// reader of a re-run node can tell which attempt was which.
//
// It returns the INTERFACE rather than the concrete type, and returns a literal
// nil when there is nothing to write to. A *TranscriptRecorder(nil) handed to
// WithTranscript would be a non-nil interface holding a nil pointer — safe to
// call, because every method here guards its receiver, but indistinguishable
// from a live sink to the loop, which would then pay to build and truncate
// every entry so the sink could throw it away.
func NewTranscriptRecorder(journal TranscriptJournal, nodeID, model string) TranscriptSink {
	if journal == nil || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	return &TranscriptRecorder{journal: journal, nodeID: nodeID, model: model}
}

// Record takes one entry, bounding it and writing the batch out when it fills.
func (r *TranscriptRecorder) Record(entry store.TranscriptEntry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return
	}
	// The bound is applied here as well as in the store because the byte
	// accounting below has to count what will actually be written, and because
	// an entry that never reaches the journal — one dropped at the cap — should
	// still not have been carried around at full size.
	entry.Text = store.TruncateTranscriptText(entry.Text)
	if r.written+len(r.pending) >= store.MaxTranscriptEntries || r.bytes >= store.MaxTranscriptBytes {
		r.seal()
		return
	}
	r.bytes += len(entry.Text)
	r.pending = append(r.pending, entry)
	if len(r.pending) >= store.MaxTranscriptBatch {
		r.write()
	}
}

// Flush makes everything recorded so far durable. It is a no-op when there is
// nothing pending, which is what makes it safe to call from several exits.
func (r *TranscriptRecorder) Flush() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.write()
}

// seal records the cap admitting itself and stops the recorder. The marker is
// written where the record stops so a reader does not mistake a truncated
// record for a short run — which is the whole difference between a bound and a
// silent loss.
func (r *TranscriptRecorder) seal() {
	r.sealed = true
	r.pending = append(r.pending, store.TranscriptEntry{
		Kind: store.TranscriptElided,
		Text: "the transcript cap was reached; the rest of this run is not recorded",
	})
	r.write()
}

// write hands the pending batch to the journal. The caller holds the lock.
func (r *TranscriptRecorder) write() {
	if len(r.pending) == 0 {
		return
	}
	batch := r.pending
	r.pending = nil
	r.written += len(batch)
	if r.faulted {
		return
	}
	if err := r.journal.RecordTranscript(r.nodeID, r.model, batch); err != nil {
		r.faulted = true
		log.Printf("note: %s could not record its transcript (%v); the rest of this leaf's turns are not journaled",
			r.nodeID, err)
	}
}
