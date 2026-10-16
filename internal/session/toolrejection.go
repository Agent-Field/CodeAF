package session

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// A TOOL CALL THAT NEVER RAN, SAID AS A FACT AND NOT AS A SENTENCE.
//
// Every tool refuses a malformed or incomplete call in the same words
// ("Invalid arguments: …", toolargs.go), and a surface that wants to tell a call
// that was turned away at the door from one that ran and failed has exactly one
// honest way to know: the code that turned it away says so. Reading the text
// back is not that way — a command can print "Invalid arguments" and a server
// can answer with it, and both of those RAN.
//
// So the mark is typed and call-local, and it is the SAME mark the bare belt's
// tools set ([bare.ArgumentRejection]) so a read, a write or a grep and a
// session tool are told apart from a ran-and-failed call by one owner.
// dispatchTool plants it in the context it hands the handler; a handler's known
// argument-parse or validation return goes through [rejectToolArguments], which
// sets the flag and answers the very same text, so no handler's result, no
// model-facing byte and no error contract moves. There is no global map and no
// goroutine: the flag lives and dies with the one call whose context carries it.

// withToolRejection plants a fresh mark for one call.
func withToolRejection(ctx context.Context) (context.Context, *bare.ArgumentRejection) {
	return bare.WithArgumentRejection(ctx)
}

// rejectToolArguments records that the call was refused before it did its work
// and returns text UNCHANGED. A context with no mark (a unit test calling a
// handler directly, a sessionless path) is safe and simply records nothing.
func rejectToolArguments(ctx context.Context, text string) string {
	return bare.RejectArguments(ctx, text)
}

// The two typed notices the engine produces. Any other EventNotice carries no
// kind and stays an ordinary line.
const (
	NoticeKindSkills = "skills"
	NoticeKindMemory = "memory"
)

// recordToolResult binds metadata to the immutable result occurrence before
// recording it. Provider IDs may repeat even within a turn. Existing message
// presentation append/replay/compaction keeps this mark with the actual bytes;
// no additional history index or call-ID keyed state is introduced.
func (a *Agent) recordToolResult(message ai.Message, result toolResult) {
	if !result.rejected && !result.harness && !result.invoked {
		a.record(message)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presentation == nil {
		a.presentation = &presentationIndex{}
	}
	message.Content = append([]ai.ContentPart(nil), message.Content...)
	a.presentation.remember(message, &messagePresentation{Audience: "tool", ToolInvoked: result.invoked, RejectedBeforeRun: result.rejected, HarnessMade: result.harness, Refused: result.harness && result.refusedBy != ""})
	a.recordLocked(message)
}

type toolResultFacts struct {
	text     string
	rejected bool
	harness  bool
	refused  bool
	invoked  bool
}

func toolResultsWithFacts(messages []ai.Message, presentation *presentationIndex) map[*ai.ToolCall]toolResultFacts {
	paired := toolResultCalls(messages)
	results := make(map[*ai.ToolCall]toolResultFacts, len(paired))
	for index, call := range paired {
		message := messages[index]
		mark := presentation.of(message)
		rejected := message.Role == "tool" && mark != nil && mark.Audience == "tool" && mark.RejectedBeforeRun
		facts := toolResultFacts{text: messageContentText(message), rejected: rejected}
		if message.Role == "tool" && mark != nil && mark.Audience == "tool" {
			facts.harness, facts.refused, facts.invoked = mark.HarnessMade, mark.Refused, mark.ToolInvoked
		}
		results[call] = facts
	}
	return results
}

// observeToolInvocation wraps only the handler entry. Task boundaries and quick
// task promotion/capture refusals can return before this entry and must not
// falsely claim the tool was dispatched. The wrapper owns no queue or goroutine.
func observeToolInvocation(tool bare.Tool) (bare.Tool, *atomic.Bool) {
	invoked := &atomic.Bool{}
	execute := tool.Execute
	tool.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		invoked.Store(true)
		return execute(ctx, args)
	}
	return tool, invoked
}

// ToolResultFlags is a compact true-only set of producer facts. Zero means
// unknown, never proof that a handler ran. This stays in DisplayEntry padding.
type ToolResultFlags uint8

const (
	ToolResultInvoked ToolResultFlags = 1 << iota
	ToolResultRejectedBeforeRun
	ToolResultHarnessMade
	ToolResultRefused
)

func (f ToolResultFlags) Invoked() bool           { return f&ToolResultInvoked != 0 }
func (f ToolResultFlags) RejectedBeforeRun() bool { return f&ToolResultRejectedBeforeRun != 0 }
func (f ToolResultFlags) HarnessMade() bool       { return f&ToolResultHarnessMade != 0 }
func (f ToolResultFlags) Refused() bool           { return f&ToolResultRefused != 0 }
func (f toolResultFacts) flags() ToolResultFlags {
	var flags ToolResultFlags
	if f.invoked {
		flags |= ToolResultInvoked
	}
	if f.rejected {
		flags |= ToolResultRejectedBeforeRun
	}
	if f.harness {
		flags |= ToolResultHarnessMade
	}
	if f.refused {
		flags |= ToolResultRefused
	}
	return flags
}
