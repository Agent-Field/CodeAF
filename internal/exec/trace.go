package exec

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
type tracer struct {
	mutex  sync.Mutex
	file   *os.File
	writer *bufio.Writer
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
func traceName(nodeID int64) string {
	return filepath.Join(traceDir, fmt.Sprintf("%d.trace.log", nodeID))
}

// legacyTraceName is where recorders written before the move still are. It is
// read from and never written to.
func legacyTraceName(nodeID int64) string {
	return filepath.Join(obsDir, fmt.Sprintf("%d.trace.log", nodeID))
}

// TraceFile is where a node's recorder is written, under the directory the
// harness keeps its own files in for that job. Writers use this and only this.
func TraceFile(home string, nodeID int64) string {
	return filepath.Join(home, traceName(nodeID))
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
func TracePath(home string, nodeID int64) string {
	current := TraceFile(home, nodeID)
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := filepath.Join(home, legacyTraceName(nodeID))
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return current
}

func newTracer(workspace *Workspace, nodeID int) *tracer {
	full, _, err := workspace.ScratchPath(traceName(int64(nodeID)))
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
	return &tracer{file: file, writer: bufio.NewWriterSize(file, traceBuffer)}
}

func (t *tracer) close() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
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
}

// note records a free-form line, for run-level facts that belong in the
// recorder but are not a turn — the contract in force, a nudge, a stop.
func (t *tracer) note(body string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.writer == nil {
		return
	}
	t.writer.WriteString(body)
	t.writer.WriteByte('\n')
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
		if details := response.Usage.PromptTokensDetails; details != nil {
			cached = details.CachedTokens
		}
	}
	fmt.Fprintf(&block, "── turn %d  finish=%s  in=%d out=%d cached=%d", turn, finish, in, out, cached)
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
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.writer == nil {
		return
	}
	t.writer.WriteString(block.String())
	t.writer.Flush()
}

func snip(text string, limit int) string {
	text = strings.ReplaceAll(text, "\n", "⏎")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
