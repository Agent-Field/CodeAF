package exec

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// tracer writes one node's turn-by-turn transcript to a file under the
// workspace's harness directory. See traceDir for why it is not the observation
// directory, which is where it used to live and where leaves kept finding it.
//
// It exists because the loop's failures were invisible. A node that ran 139
// turns and wrote nothing reported only "exhausted its token budget" — every
// hypothesis about what those turns contained had to be tested by re-running
// the task at full price. The trace is the flight recorder: cheap, local,
// always on, and read only when something needs explaining.
//
// Always on is what makes the buffering necessary. A subharness stream can
// deliver a thousand NDJSON deltas a second and every one of them is a line
// here, which was a string concatenation and a write syscall each. The writes
// are batched and flushed at record boundaries instead — the end of a turn, the
// end of a poll interval, and the close — so a crash costs the tail of the
// current batch and nothing before it. The mutex is not new caution: the child's
// stdout and its stderr are traced from two goroutines at once, and a shared
// buffer is not a file handle.
// THE RECORDER IS PROSE AND THE STREAM IS NOT. A subharness drives a child that
// speaks NDJSON, and for as long as those lines were `note`d the recorder was
// two documents in one file: a few dozen sentences a person reads, buried under
// thousands of `{"id":"evt_…","type":"message.part.delta",…}`. The room that
// draws this file draws one row per line it cannot parse, so a 2MB stream became
// screens of raw JSON presented as content (internal/tui2/chat/trace.go's
// parseTrace, default branch). The stream is not deleted — it is the flight
// recorder's most valuable half — it is moved one file sideways, to
// `<node>.stream.ndjson`, and the recorder keeps a single pointer note to it.
// What a person reads and what a debugger greps are two documents, and they get
// two files.
type tracer struct {
	mutex  sync.Mutex
	file   *os.File
	writer *bufio.Writer

	// The machine stream's sidecar, opened on the first line that needs it: a
	// node whose worker never speaks NDJSON leaves no empty file behind.
	streamPath   string
	streamFile   *os.File
	streamWriter *bufio.Writer
	streamOpened bool
	streamNoted  bool

	// sink is the store's transcript, when this attempt has one.
	//
	// THE RECORD OF A TURN GOES BOTH PLACES OR IT GOES NOWHERE USEFUL. This
	// recorder writes a file in the workspace scratch: excellent for reading
	// over a run's shoulder, and useless afterwards — it is not addressable by
	// node, it is not there when somebody asks a week later, and a continuation
	// cannot be seeded from it. The store's transcript is the durable half, and
	// until this it had exactly one writer in the whole tree, and not this one
	// (see exec/transcript.go). So the leaf belt — the belt every node gets —
	// recorded nothing durable at all: the ink run of 2026-08-29 ran three
	// leaves on it and left a store with zero transcript rows, so BankedRun
	// found nothing, every continuation started cold, and the resumption the
	// lease lane had built could never fire.
	//
	// It is wired HERE rather than in the loop because every turn the worker
	// takes and every harness note already passes through this one object —
	// turn() has the response, the calls and the results in hand, and note() has
	// the machinery's own account of itself. One seam, and anything else that
	// builds a tracer gets it in the same change.
	sink TranscriptSink
	// turnOf is the turn number a note belongs to, kept because note() is
	// called between turns and a note filed under turn zero is a note an
	// autopsy cannot place.
	turnOf int
}

// traceBuffer is a batch of lines rather than a page. Small enough that a run
// killed between flushes has lost almost nothing, large enough that the
// per-line syscall is gone.
const traceBuffer = 32 << 10

// traceName is the one place the recorder's file name is spelled, and every
// writer and reader in the process goes through it or through the two exported
// helpers below.
//
// Single-sourcing it is not tidiness. The recorder moved out of .obs, and the
// surfaces that read it — the live node view in the chat window, the note a
// build owes a node whose promised worker it does not have — each built the
// same path by hand from their own directory. A reader left behind does not
// fail: it opens nothing, renders empty, and looks exactly like a worker that
// is thinking rather than writing. A writer left behind is worse, appending a
// sentence nobody will ever open.
// leaf is the recorder's identity, and it is a string because a store node's
// creation sequence — which is what the resident surface had to offer — belongs
// to the whole splice. Every sibling of a four-part job opened the same recorder
// and appended into it, so a reader asking what one worker did was handed four
// workers' turns interleaved. Whoever names a recorder must name one worker.
func traceName(leaf string) string {
	return filepath.Join(traceDir, fmt.Sprintf("%s.trace.log", pathSlug(leaf)))
}

// streamName is the sidecar the raw machine stream is spilled to, beside the
// recorder it was cut out of. One spelling, exactly as [traceName] is one
// spelling, and for the same reason: a reader that built the path by hand would
// silently open nothing.
func streamName(leaf string) string {
	return filepath.Join(traceDir, fmt.Sprintf("%s.stream.ndjson", pathSlug(leaf)))
}

// patchName is where a node's own change set is written out in full, beside the
// recorder and under the same git-excluded directory — a patch file written into
// the workspace would be part of the next diff, and the engine's auditor would
// correctly refuse to ship it. One spelling, for [streamName]'s reason.
func patchName(leaf string) string {
	return filepath.Join(traceDir, fmt.Sprintf("%s.patch", pathSlug(leaf)))
}

// StreamFile is where a node's raw subharness event stream is written. It is
// referenced by the recorder and read by whoever is debugging a run; no surface
// renders it, which is the whole point of it being a separate file.
func StreamFile(home, leaf string) string {
	return filepath.Join(home, streamName(leaf))
}

// legacyTraceName is where recorders written before the move still are. It is
// read from and never written to.
func legacyTraceName(leaf string) string {
	return filepath.Join(obsDir, fmt.Sprintf("%s.trace.log", pathSlug(leaf)))
}

// TraceFile is where a node's recorder is written, under the directory the
// harness keeps its own files in for that job. Writers use this and only this.
func TraceFile(home, leaf string) string {
	return filepath.Join(home, traceName(leaf))
}

// TracePath is where a node's recorder can be read from: the current location,
// falling back to the pre-move .obs spelling when only that file exists.
//
// The fallback is what keeps a finished run readable after the move. A trace is
// written once and read for as long as anyone is still asking what a node did,
// and a relocation that silently emptied every existing run's view would be a
// worse defect than the contamination it was fixing. When neither file exists
// the current path is returned, so an error names where the recorder should
// have been rather than where it used to be.
func TracePath(home, leaf string) string {
	current := TraceFile(home, leaf)
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := filepath.Join(home, legacyTraceName(leaf))
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

// newRecordingTracer is newTracer with the attempt's durable transcript wired
// in. The two are separate constructors because most callers of newTracer are
// tests and sub-openers with no attempt behind them, and a sink they cannot
// supply should not become a parameter they have to pass nil for.
func newRecordingTracer(ctx context.Context, workspace *Workspace, leaf string) *tracer {
	t := newTracer(workspace, leaf)
	t.sink = TranscriptFrom(ctx)
	return t
}

func newTracer(workspace *Workspace, leaf string) *tracer {
	full, _, err := workspace.ScratchPath(traceName(leaf))
	if err != nil {
		return &tracer{}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return &tracer{}
	}
	file, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return &tracer{}
	}
	stream, _, err := workspace.ScratchPath(streamName(leaf))
	if err != nil {
		stream = ""
	}
	return &tracer{
		file: file, writer: bufio.NewWriterSize(file, traceBuffer), streamPath: stream,
	}
}

func (t *tracer) close() {
	// The durable half first and outside the lock, because it is the half that
	// matters after the process is gone: a run that ends any way at all — a
	// landing, a fault, a cancellation — has its turns on disk before the file
	// handle is let go. The runner flushes again on its own side; both are
	// no-ops on an empty buffer.
	t.flushRecord()
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.streamWriter != nil {
		t.streamWriter.Flush()
		t.streamFile.Close()
		t.streamFile, t.streamWriter = nil, nil
	}
	if t.file == nil {
		return
	}
	t.writer.Flush()
	t.file.Close()
	t.file, t.writer = nil, nil
}

// flush lands everything written so far. It is called where a batch of lines
// ends rather than where a line does: what a reader of a live trace wants is
// the last complete thing that happened, and what a crash must not lose is
// anything older than the batch in hand.
func (t *tracer) flush() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.writer != nil {
		t.writer.Flush()
	}
	if t.streamWriter != nil {
		t.streamWriter.Flush()
	}
}

// stream spills one raw machine line to the sidecar.
//
// It writes the recorder ONE line about the sidecar, the first time it is
// needed, and never another: a pointer is a fact, and a fact repeated per event
// is the noise this whole seam exists to remove. Everything after that goes to
// the sidecar alone, so the recorder's line count stays a count of things a
// person would read.
//
// A sidecar that cannot be opened is not an error worth ending a run over — the
// raw feed is a debugging convenience and the summary above it is what the
// record draws — so the failure is recorded once, in the recorder, and the run
// carries on.
func (t *tracer) stream(line string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.writer == nil || t.streamPath == "" {
		return
	}
	if !t.streamOpened {
		t.streamOpened = true
		if err := os.MkdirAll(filepath.Dir(t.streamPath), 0o755); err == nil {
			file, err := os.OpenFile(t.streamPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err == nil {
				t.streamFile = file
				t.streamWriter = bufio.NewWriterSize(file, traceBuffer)
			}
		}
	}
	if !t.streamNoted {
		t.streamNoted = true
		where := filepath.Base(t.streamPath)
		if t.streamWriter == nil {
			t.writer.WriteString(streamNote + "could not be opened — " + where + "\n")
		} else {
			t.writer.WriteString(streamNote + where + "\n")
		}
	}
	if t.streamWriter == nil {
		return
	}
	t.streamWriter.WriteString(line)
	t.streamWriter.WriteByte('\n')
}

// streamNote is the one sentence the recorder spends on the sidecar. It reads as
// the plain fact it is, and it is a prefix rather than a shape so the recorder
// stays line-based (parseTrace draws anything it does not recognise as the dim
// note it is).
const streamNote = "stream: the engine's own event feed is in "

// note records a free-form line, for run-level facts that belong in the
// recorder but are not a turn — the contract in force, a nudge, a stop.
func (t *tracer) note(body string) {
	// THE DURABLE HALF IS NOT GATED ON THE FILE HALF. newTracer answers three
	// different failures — no scratch path, no directory, no file — with the
	// same empty tracer, and every one of them would otherwise take the store's
	// transcript down with a file nobody was reading. The two records exist for
	// different readers and they fail independently. It is also written before
	// the lock, because the sink is required to be safe for concurrent use and
	// holding a file lock across a database write is how a slow disk becomes a
	// stalled leaf.
	t.record(store.TranscriptEntry{
		Turn: t.turnNumber(), Kind: store.TranscriptNote, Text: body,
	})
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.writer == nil {
		return
	}
	t.writer.WriteString(body)
	t.writer.WriteByte('\n')
}

// turnNumber is the turn a note belongs to. A note written between turns
// belongs to the turn that has just happened, which is what a reader collapsing
// a transcript by turn expects to find it under.
func (t *tracer) turnNumber() int {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.turnOf
}

// record files one entry in the durable transcript, and nothing when this
// attempt has no sink — which is the ordinary case for a test and for a bench
// harness with no store. It is called with the tracer's own lock held in note()
// and without it in turn(), so it takes none of its own: TranscriptSink is
// required to be safe for concurrent use (see transcript.go).
func (t *tracer) record(entry store.TranscriptEntry) {
	if t.sink == nil || strings.TrimSpace(entry.Text) == "" {
		return
	}
	t.sink.Record(entry)
}

// flushRecord makes the durable half durable. The file half flushes per turn on
// its own; this is called where a run ends, and it is safe to call twice.
func (t *tracer) flushRecord() {
	if t.sink != nil {
		t.sink.Flush()
	}
}

// turn records one round: what the model said, what it called, what came back.
func (t *tracer) turn(turn int, response *ai.Response, calls []ai.ToolCall, results []Result, note string) {
	var block strings.Builder
	finish := ""
	if response != nil && len(response.Choices) > 0 {
		finish = response.Choices[0].FinishReason
	}
	// cached is the part of in= the provider billed at the cached rate, and it
	// is on the line because its absence is what let a whole class of defect
	// hide. Every cache-shape discipline in this codebase — the stable prefix,
	// the batched decay, the frozen tool block — is unfalsifiable without it:
	// a run whose affinity key was never set and a run whose prefix was perfect
	// produce identical traces when the only numbers written down are in and
	// out. It goes after them rather than replacing in=, because it is a share
	// of that number and reads as one.
	in, out, cached := 0, 0, 0
	if response != nil && response.Usage != nil {
		in, out = response.Usage.PromptTokens, response.Usage.CompletionTokens
		// Both spellings, through the accessor that knows both: reading only the
		// OpenAI-shaped nesting made the trace print cached=0 for every turn of
		// every run against an endpoint that reports the Anthropic-native field,
		// which is exactly the blindness this number exists to remove.
		cached = response.Usage.CacheReadTokens()
	}
	// hit= is cached as a share of in=, and it is the number a benchmark asserts
	// on. The two counts beside it are absolute, so they move with the
	// transcript's size and cannot be compared between turns, between leaves or
	// between runs: a turn that cached 40k of 50k and a turn that cached 40k of
	// 400k read the same until the division is done. The ratio is the discipline
	// itself, stated once per turn — a leaf whose breakpoints, affinity key and
	// frozen prefix are all working reads in the nineties from its second turn
	// on, and a leaf whose prefix is being invalidated reads near zero on the
	// turn it happened. Neither is visible in cached= alone.
	fmt.Fprintf(&block, "── turn %d  finish=%s  in=%d out=%d cached=%d hit=%d%%",
		turn, finish, in, out, cached, hitPercent(cached, in))
	if note != "" {
		fmt.Fprintf(&block, "  [%s]", note)
	}
	block.WriteString(" ──\n")
	if response != nil {
		if text := strings.TrimSpace(response.Text()); text != "" {
			fmt.Fprintf(&block, "text: %s\n", snip(text, 600))
		}
	}
	for index, call := range calls {
		fmt.Fprintf(&block, "call %s %s\n", call.Function.Name, snip(call.Function.Arguments, 300))
		if index < len(results) {
			status := ""
			if results[index].IsError {
				status = " ERROR"
			}
			fmt.Fprintf(&block, "  → %dB%s: %s\n", len(results[index].Content), status, snip(results[index].Content, 300))
		}
	}
	// A turn is a whole record, so it is also a flush point: the linear loop
	// writes one every few seconds and a trace that is a turn behind is a trace
	// nobody can read over a run's shoulder.
	// The durable half first and outside the lock, for the reason note() gives:
	// a trace file that could not be opened must not silence the record under
	// the node, and a database write must not be taken with a file lock held.
	t.setTurn(turn)
	t.recordTurn(turn, response, calls, results, note)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.writer == nil {
		return
	}
	t.writer.WriteString(block.String())
	t.writer.Flush()
}

// setTurn remembers which turn the loop is on, so a note written after it can
// be filed under it.
func (t *tracer) setTurn(turn int) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.turnOf = turn
}

// recordTurn is the durable half of turn(): what the model said, what it asked
// for, and what came back, in the order it happened.
//
// The order matters and it is one order deliberately — a reader of two belts'
// transcripts must not have to learn two shapes. The assistant's words go down
// before the calls they explain, because a turn whose tools hang is otherwise a
// turn nobody can see the reasoning for.
func (t *tracer) recordTurn(turn int, response *ai.Response, calls []ai.ToolCall, results []Result, note string) {
	if t.sink == nil {
		return
	}
	if response != nil {
		t.record(store.TranscriptEntry{
			Turn: turn, Kind: store.TranscriptAssistant,
			Text: strings.TrimSpace(response.Text()),
		})
	}
	for index, call := range calls {
		t.record(store.TranscriptEntry{
			Turn: turn, Kind: store.TranscriptToolCall,
			Tool: call.Function.Name, CallID: call.ID, Text: call.Function.Arguments,
		})
		// A batch that was never executed — a reply cut at the output limit,
		// a promotion — has calls and no results, and saying so is the honest
		// record of it. The note carries why.
		if index >= len(results) {
			continue
		}
		t.record(store.TranscriptEntry{
			Turn: turn, Kind: store.TranscriptToolResult,
			Tool: call.Function.Name, CallID: call.ID,
			Text: results[index].Content, Failed: results[index].IsError,
		})
	}
	if strings.TrimSpace(note) != "" {
		t.record(store.TranscriptEntry{Turn: turn, Kind: store.TranscriptNote, Text: note})
	}
}

// hitPercent is the share of a turn's prompt the provider served from its cache,
// rounded down to a whole percent.
//
// A turn with no prompt at all reads 0 rather than being omitted, because a
// missing number and a zero mean different things to a reader and only one of
// them is true here: nothing was sent, so nothing was cached. The count is
// clamped to the prompt for the same reason spent() clamps it — a provider
// reporting more cache reads than prompt tokens is reporting something this
// arithmetic cannot use, and a ratio over 100% would read as a defect in the
// discipline rather than in the report.
func hitPercent(cached, prompt int) int {
	if prompt <= 0 || cached <= 0 {
		return 0
	}
	if cached > prompt {
		cached = prompt
	}
	return cached * 100 / prompt
}

// snip folds a value onto one line and cuts it to a byte ceiling WITHOUT
// SPLITTING A CHARACTER.
//
// The back-off is here rather than borrowed because every package in this tree
// keeps its own — internal/plan's clipRunes, internal/search, internal/tui3's
// reveal, internal/resident's revise, and five more in internal/session all
// spell these two lines locally rather than depend on each other for them.
//
// It matters more than it did: this cut used to reach only the call tail in a
// trace, and now reaches [Account.Commands], which is rendered under "What the
// work ran itself:" and read by a person. A command clipped mid-rune put
// invalid bytes in front of them.
func snip(text string, limit int) string {
	text = strings.ReplaceAll(text, "\n", "⏎")
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}
