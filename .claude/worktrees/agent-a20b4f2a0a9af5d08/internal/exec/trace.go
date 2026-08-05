package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// tracer writes one node's turn-by-turn transcript to a file in the
// workspace's observation directory.
//
// It exists because the loop's failures were invisible. A node that ran 139
// turns and wrote nothing reported only "exhausted its token budget" — every
// hypothesis about what those turns contained had to be tested by re-running
// the task at full price. The trace is the flight recorder: cheap, local,
// always on, and read only when something needs explaining.
type tracer struct {
	file *os.File
}

func newTracer(workspace *Workspace, nodeID int) *tracer {
	relative := filepath.Join(obsDir, fmt.Sprintf("%d.trace.log", nodeID))
	full, err := workspace.Resolve(relative)
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
	return &tracer{file: file}
}

func (t *tracer) close() {
	if t.file != nil {
		t.file.Close()
	}
}

// note records a free-form line, for run-level facts that belong in the
// recorder but are not a turn — the contract in force, a nudge, a stop.
func (t *tracer) note(body string) {
	if t.file == nil {
		return
	}
	t.file.WriteString(body + "\n")
}

// turn records one round: what the model said, what it called, what came back.
func (t *tracer) turn(turn int, response *ai.Response, calls []ai.ToolCall, results []Result, note string) {
	if t.file == nil {
		return
	}
	var block strings.Builder
	finish := ""
	if response != nil && len(response.Choices) > 0 {
		finish = response.Choices[0].FinishReason
	}
	in, out := 0, 0
	if response != nil && response.Usage != nil {
		in, out = response.Usage.PromptTokens, response.Usage.CompletionTokens
	}
	fmt.Fprintf(&block, "── turn %d  finish=%s  in=%d out=%d", turn, finish, in, out)
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
	t.file.WriteString(block.String())
}

func snip(text string, limit int) string {
	text = strings.ReplaceAll(text, "\n", "⏎")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
