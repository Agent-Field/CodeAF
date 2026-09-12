package session

// Held reads: a `read` call whose bytes are already verbatim in this
// conversation's transcript is answered with a one-line pointer instead of a
// fresh fetch from the file system.
//
// The measured defect: one planning chat issued ten reads of the same 5,180-line
// file as seventeen small slices, each a 30–90 second model round trip, and every
// slice was then re-billed on every later request. The model slices small because
// it prices tokens, not latency, and the tool contract says nothing about either
// — nothing told it a call costs a round trip, and nothing told it it already
// held the bytes. The ledger below is the answer to the second fact; the tool's
// own description (bare/tools.go's [readDescription]) is the answer to the first.
//
// ── ONLY THE LIVE TRANSCRIPT COUNTS AS HELD ──
//
// A result whose bytes left the transcript does not count as held, however the
// ledger remembers the read. STUBBING REWRITES THE RESULT'S TEXT (stub.go's
// [stubMarker]) AND FOLDING REPLACES THE MESSAGE OUTRIGHT (turnfold.go), so the
// claim does not trust its own record: each held span carries the fingerprint of
// the message text that carried those bytes, and a span whose fingerprint no
// longer names a live tool message is dropped right there. An incremental
// ledger that trusted its record would need every one of those rewrite seams to
// notify it; checking the transcript at the claim keeps the rule in one place.
//
// ── THE STAMP IS THE FILE'S OWN CLOCK ──
//
// Coverage is claimed only while mtime+size match what the last read saw. A
// changed file invalidates its whole entry by itself, so the fresh read the
// claim then falls through to REPLACES the entry rather than accumulating two
// generations of spans. Nothing here is persisted or crosses a session boundary
// — the ledger is this agent's, in memory, and a resume rebuilds it the way it
// rebuilt the transcript.

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// heldLedger maps a resolved path to what this conversation already holds of
// its bytes. It is one mutex because dispatch is the only road in and the claim
// must never hold it while taking the transcript's lock.
type heldLedger struct {
	mu    sync.Mutex
	files map[string]*heldFile
}

// heldFile is one path's entry: the file's stamp at the last recorded read and
// the line spans that read — together or alone — returned.
type heldFile struct {
	stamp heldStamp
	spans []heldSpan
}

// heldStamp is the two numbers os.Stat answers; comparable, so a mismatch is a
// whole-file invalidation with no clock-reading.
type heldStamp struct {
	modified time.Time
	size     int64
}

// heldSpan is one recorded read's range: one-based first line, last line (zero
// when the read reached the end of the file), whether it reached the end, and
// the fingerprint of the message text that carried it.
type heldSpan struct {
	start int
	end   int
	eof   bool
	proof uint64
}

// heldSpec is one call's request, normalized out of its arguments.
type heldSpec struct {
	path  string // as the model spelled it, for the pointer answer
	start int    // one-based first line
	end   int    // one-based last line; zero asks for the file to its end
	abs   string // resolved against the agent's workspace
}

// heldClaim is the dispatch-door half of the ledger: ALL coverage questions are
// answered here. A fully covered range is returned as the pointer sentence;
// anything else — no entry, a changed stamp, a stubbed away result, a gap in
// the union of spans — falls through to the ordinary fetch.
func (a *Agent) heldClaim(name string, args json.RawMessage) (string, bool) {
	if name != "read" {
		return "", false
	}
	spec, ok := a.heldSpecOf(args)
	if !ok {
		return "", false
	}
	stamp, ok := heldStampOf(spec.abs)
	if !ok {
		// A file nothing can stat is the read tool's own error to report, and
		// it is never the ledger's answer to give.
		return "", false
	}
	a.held.mu.Lock()
	file := a.held.files[spec.abs]
	a.held.mu.Unlock()
	if file == nil || file.stamp != stamp {
		return "", false
	}
	// The fingerprint of every live tool message, taken under mu rather than
	// the ledger's — the inverse order would see the ledger's claim and
	// the transcript's rewriters praying for one another.
	live := a.toolFingerprints()
	var spans []heldSpan
	for _, span := range file.spans {
		if live[span.proof] {
			spans = append(spans, span)
		}
	}
	if !heldCovers(spans, spec) {
		return "", false
	}
	// ONE SENTENCE, NO MACHINERY IN IT: the model has the bytes and says where.
	if spec.end > 0 {
		return fmt.Sprintf("[already read] %s lines %d–%d are in this conversation above — use those bytes.", spec.path, spec.start, spec.end), true
	}
	return fmt.Sprintf("[already read] %s from line %d to the end is in this conversation above — use those bytes.", spec.path, spec.start), true
}

// heldNote is the record half, called with the finished result: errors and the
// tool's one successful-but-empty answer (bare.ReadContentless) never leave a
// span. A span is recorded for an explicit limit without any footer question —
// the limit names the range exactly, and the footer is what then says whether
// the file ended inside it (bare.ReadPagingFooter). A whole-file read that was
// cut by the caps is never recorded at all, because there is no honest end to
// hold.
func (a *Agent) heldNote(name string, args json.RawMessage, text string, isError bool) {
	if name != "read" || isError || bare.ReadContentless(text) {
		return
	}
	spec, ok := a.heldSpecOf(args)
	if !ok {
		return
	}
	eof := !bare.ReadPagingFooter(text)
	if spec.end == 0 && !eof {
		// A PAGED WHOLE-FILE READ HAS NO END TO OFFER, and a span without one
		// would claim the file's tail for ever. It simply contributes nothing.
		return
	}
	stamp, ok := heldStampOf(spec.abs)
	if !ok {
		return
	}
	span := heldSpan{start: spec.start, end: spec.end, eof: eof, proof: heldFingerprint(text)}
	a.held.mu.Lock()
	defer a.held.mu.Unlock()
	if a.held.files == nil {
		a.held.files = make(map[string]*heldFile)
	}
	file := a.held.files[spec.abs]
	if file == nil || file.stamp != stamp {
		// A changed stamp retires the entry whole: two generations of spans
		// against one file would merge into coverage no version ever had.
		file = &heldFile{stamp: stamp}
		a.held.files[spec.abs] = file
	}
	// The two reads that together cover a range are MERGED AT THE CLAIM, not
	// here: the spans append, and the union arithmetic that decides coverage
	// runs over whatever the entry holds when the model asks.
	file.spans = append(file.spans, span)
}

// heldSpecOf normalizes one call's path, offset and limit into the range it
// asks for, resolved against the agent's workspace exactly the way the tool
// resolves it. An unparseable argument is not a ledger question at all.
func (a *Agent) heldSpecOf(args json.RawMessage) (heldSpec, bool) {
	var p struct {
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" {
		return heldSpec{}, false
	}
	spec := heldSpec{path: p.Path, start: 1, end: 0}
	if p.Offset != nil && *p.Offset > 1 {
		spec.start = *p.Offset
	}
	if p.Limit != nil && *p.Limit > 0 {
		spec.end = spec.start + *p.Limit - 1
	}
	spec.abs = bare.ResolvePath(p.Path, a.config.Workspace)
	return spec, true
}

// heldStampOf reads the two numbers an entry compares, or says nothing can be
// known about the file.
func heldStampOf(path string) (heldStamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return heldStamp{}, false
	}
	return heldStamp{modified: info.ModTime(), size: info.Size()}, true
}

// heldCovers is the union arithmetic: sorted spans must run contiguously from
// the request's first line to its last, and a request for the rest of the file
// needs one reach-EOF span to close it. A gap at any point is a miss, and a
// miss is a fresh read — the claim never half-holds a range.
func heldCovers(spans []heldSpan, spec heldSpec) bool {
	if len(spans) == 0 {
		return false
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	reached := spec.start // one-based next line still owed
	for _, span := range spans {
		if span.start > reached {
			// A gap before this span: sorted by start, nothing later closes it.
			return false
		}
		if span.eof {
			// This span ran to the end of the file, and its start was already
			// owed — every request it touches is answered.
			return true
		}
		if span.end >= reached {
			reached = span.end + 1
		}
		if spec.end > 0 && reached > spec.end {
			return true
		}
	}
	return spec.end > 0 && reached > spec.end
}

// toolFingerprints answers which message texts the live transcript still
// carries, computed under the transcript's own lock so a fold or a stub mid-
// claim cannot split the answer.
func (a *Agent) toolFingerprints() map[uint64]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[uint64]bool, len(a.messages))
	for index := range a.messages {
		if a.messages[index].Role != "tool" {
			continue
		}
		out[heldFingerprint(messageContentText(a.messages[index]))] = true
	}
	return out
}

// heldFingerprint weights one message's text with an 8-byte hash, so the claim
// can hold the proof of a 50KB read without holding the read.
func heldFingerprint(text string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(text))
	return hash.Sum64()
}
