package claudemodel

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ── CLAUDE CODE'S OWN STEPS, WRITTEN DOWN IN CODEAF'S FORMAT ───────────────
//
// Claude Code runs its own Bash, Read and Edit inside one request, so codeaf's
// turn loop never sees them as calls: left alone, a conversation on Claude
// Code would keep only Claude's final words, its page would show no rows for
// the work, and a model the conversation moved to next would read "done" with
// no account of what was done.
//
// So each of its steps, when its result arrives, is translated into the call
// codeaf's own belt would have made — `bash`, `read`, `edit`, `write`, `grep`,
// `find`, `ls`, `web_fetch`, `web_search`, with codeaf's argument names — and
// handed up the stream as a finished step (internal/provider's
// StreamStepsRan). The turn loop records the call and its result as it records
// any tool's, so the row is drawn, the conversation's file keeps it, and the
// next model reads it in its history as if codeaf had run it.
//
// A STEP WITH NO CODEAF COUNTERPART IS NOT RECORDED: a sub-agent, a to-do
// list, a skill being loaded. Its line still shows while it runs; what it
// changed on disk is on disk, and the next model can read it there.

// stepPrefix starts the id of every call Claude Code made, its own and
// codeaf's alike, so a turn can tell the history it already holds from what
// was said while another model had the conversation.
const stepPrefix = "claude_code_"

// stepOutputBudget bounds one step's recorded result. A step's whole output
// already reached Claude Code; this is the account of it that is kept, so a
// long file read is not kept twice at full size.
const stepOutputBudget = 16 * 1024

// ranStep is one finished step, as the stream hands it up.
type ranStep struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Output    string          `json:"output"`
	IsError   bool            `json:"is_error,omitempty"`
	TookMS    int64           `json:"took_ms"`
}

// startedStep is a call of Claude Code's own that has not had its result yet.
type startedStep struct {
	name  string
	input json.RawMessage
	at    time.Time
}

// stepFor translates one of Claude Code's tool calls and its result into
// codeaf's spelling, or answers false for a tool codeaf has no counterpart of.
func stepFor(id string, started startedStep, output string, isError bool) (ranStep, bool) {
	var in map[string]any
	_ = json.Unmarshal(started.input, &in)
	text := func(key string) string {
		value, _ := in[key].(string)
		return value
	}
	var name string
	var args map[string]any
	switch started.name {
	case "Bash":
		name, args = "bash", map[string]any{"command": text("command")}
	case "Read":
		name, args = "read", map[string]any{"path": text("file_path")}
		for _, key := range []string{"offset", "limit"} {
			if n, ok := in[key].(float64); ok {
				args[key] = int(n)
			}
		}
	case "Write":
		name, args = "write", map[string]any{"path": text("file_path"), "content": text("content")}
	case "Edit":
		name, args = "edit", map[string]any{"path": text("file_path"), "edits": []map[string]string{{"oldText": text("old_string"), "newText": text("new_string")}}}
	case "MultiEdit":
		var edits []map[string]string
		list, _ := in["edits"].([]any)
		for _, item := range list {
			if edit, ok := item.(map[string]any); ok {
				old, _ := edit["old_string"].(string)
				replacement, _ := edit["new_string"].(string)
				edits = append(edits, map[string]string{"oldText": old, "newText": replacement})
			}
		}
		name, args = "edit", map[string]any{"path": text("file_path"), "edits": edits}
	case "Grep":
		name, args = "grep", map[string]any{"pattern": text("pattern")}
		if path := text("path"); path != "" {
			args["path"] = path
		}
		if glob := text("glob"); glob != "" {
			args["glob"] = glob
		}
	case "Glob":
		name, args = "find", map[string]any{"pattern": text("pattern")}
		if path := text("path"); path != "" {
			args["path"] = path
		}
	case "LS":
		name, args = "ls", map[string]any{"path": text("path")}
	case "WebFetch":
		name, args = "web_fetch", map[string]any{"url": text("url")}
	case "WebSearch":
		name, args = "web_search", map[string]any{"query": text("query")}
	default:
		return ranStep{}, false
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ranStep{}, false
	}
	return ranStep{
		ID: stepPrefix + id, Name: name, Arguments: raw,
		Output: clip(output), IsError: isError, TookMS: time.Since(started.at).Milliseconds(),
	}, true
}

// clip keeps the head and the tail of a long output, which is where a
// command's command line and its verdict are.
func clip(output string) string {
	if len(output) <= stepOutputBudget {
		return output
	}
	head, tail := output[:stepOutputBudget*3/4], output[len(output)-stepOutputBudget/4:]
	for !utf8.ValidString(head) && len(head) > 0 {
		head = head[:len(head)-1]
	}
	for !utf8.ValidString(tail) && len(tail) > 0 {
		tail = tail[1:]
	}
	return head + "\n… (" + strconv.Itoa(len(output)-len(head)-len(tail)) + " bytes not kept) …\n" + tail
}

// toolResult is one result in Claude Code's stream.
type toolResult struct {
	id      string
	text    string
	isError bool
}

// toolResults reads the results in a "user" event of Claude Code's stream: the
// id of the call each answers, its text, and whether it failed.
func toolResults(raw json.RawMessage) []toolResult {
	var line struct {
		Message struct {
			Content []struct {
				Type      string          `json:"type"`
				ToolUseID string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
				IsError   bool            `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
	}
	_ = json.Unmarshal(raw, &line)
	var out []toolResult
	for _, block := range line.Message.Content {
		if block.Type != "tool_result" || block.ToolUseID == "" {
			continue
		}
		out = append(out, toolResult{block.ToolUseID, contentText(block.Content), block.IsError})
	}
	return out
}

// ownHistory says a message is one of Claude Code's own steps or codeaf
// calls, which its session already holds and must not be told again.
func ownHistory(m chatMessage) bool {
	if m.Role == "tool" {
		return strings.HasPrefix(m.ToolCallID, stepPrefix)
	}
	if m.Role != "assistant" || len(m.ToolCalls) == 0 {
		return false
	}
	for _, call := range m.ToolCalls {
		if !strings.HasPrefix(call.ID, stepPrefix) {
			return false
		}
	}
	return true
}

// spoken is the conversation without Claude Code's own steps: what was SAID,
// which is what a session's count of what it has seen counts.
func spoken(messages []chatMessage) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		if !ownHistory(m) {
			out = append(out, m)
		}
	}
	return out
}
