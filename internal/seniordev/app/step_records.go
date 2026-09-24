//go:build !windows

package app

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
)

// The `step` record projects one finished tool call into a shape a reader can
// display without understanding the message model: what was run, what came
// back, the tool, a command's exit code, and the step of senior-dev's process
// it served. Every byte but the last is already inside the
// `message.part.updated` payload for the same call, and the last is read off
// those same payloads in the order the calls finished (step_ids.go) — this
// rearranges what the run already knows, and learns nothing new.
//
// Nothing here reaches the model. The record is written by the event layer
// after the tool result has been produced; it is not a prompt, not a tool
// result, and not a message. The model's transcript is identical whether or
// not anyone reads these.
const (
	// stepObservationMax caps the observation. A tool result can be a whole
	// file or a full test log, and a reader that only renders steps should not
	// have to hold one.
	stepObservationMax = 2048
	// stepCommandMax caps the argument rendered beside the tool name, which is
	// a label rather than a payload.
	stepCommandMax = 200
)

// stepRecord is one finished tool call. key deduplicates: a tool part is
// republished as its state moves, so the same call arrives more than once in
// the same terminal state. action is what the step classifier reads
// (step_ids.go), step is the step it named, and exit is a command's exit code,
// which the bash tool keeps in its metadata (tool/bash.go) and nowhere else.
type stepRecord struct {
	key         string
	command     string
	observation string
	action      stepAction
	step        string
	exit        *int
}

// toolStepRecord reads a bus payload and reports the finished tool call in it,
// if it holds one. Pending and running states are ignored: a step is a thing
// that happened, and only `completed` and `error` have happened.
func toolStepRecord(value bus.Payload) (stepRecord, bool) {
	if value.Type != "message.part.updated" {
		return stepRecord{}, false
	}
	part := mapAt(object(value.Properties), "part")
	if stringAt(part, "type") != "tool" {
		return stepRecord{}, false
	}
	state := mapAt(part, "state")
	status := stringAt(state, "status")
	if status != "completed" && status != "error" {
		return stepRecord{}, false
	}
	tool := stringAt(part, "tool")
	input := mapAt(state, "input")
	record := stepRecord{
		key:    "tool:" + stringAt(part, "callID") + ":" + status,
		action: stepAction{tool: tool, target: stepTarget(input), failed: status == "error"},
		exit:   exitCode(mapAt(state, "metadata")),
	}
	if argument := toolArgument(input); argument != "" {
		record.command = tool + ": " + argument
	} else {
		record.command = tool
	}
	if status == "error" {
		record.observation = stringAt(state, "error")
	} else {
		record.observation = stringAt(state, "output")
	}
	record.observation = clipBytes(record.observation, stepObservationMax)
	return record, true
}

// stepTargetKeys are the inputs that say what an action was aimed at, for the
// step classifier: the file a file tool named, a shell's command, a patch's
// whole text, where a search looked. It reads the input whole — the label on
// the record is cut to 200 bytes, and a patch names its files after its first
// line.
var stepTargetKeys = []string{"filePath", "command", "patchText", "path", "pattern"}

func stepTarget(input map[string]any) string {
	for _, key := range stepTargetKeys {
		if text, ok := input[key].(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

// exitCode is a tool's exit code from its metadata, nil when it reported none:
// every tool but a shell, and a shell command killed at its ceiling.
func exitCode(metadata map[string]any) *int {
	var code int
	switch value := metadata["exitCode"].(type) {
	case float64:
		if value != float64(int(value)) {
			return nil
		}
		code = int(value)
	case int:
		code = value
	default:
		return nil
	}
	return &code
}

// toolArgumentKeys are the input fields that identify what a call was about,
// most identifying first. A tool that names none of them falls back to its
// first string input in key order, so a new tool still renders something.
var toolArgumentKeys = []string{
	"command", "filePath", "path", "pattern", "query", "url", "description",
}

func toolArgument(input map[string]any) string {
	if input == nil {
		return ""
	}
	for _, key := range toolArgumentKeys {
		if text, ok := input[key].(string); ok && strings.TrimSpace(text) != "" {
			return clipBytes(oneLine(text), stepCommandMax)
		}
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if text, ok := input[key].(string); ok && strings.TrimSpace(text) != "" {
			return clipBytes(oneLine(text), stepCommandMax)
		}
	}
	return ""
}

// oneLine flattens a multi-line argument so the command reads as a label.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// clipBytes truncates to at most max bytes without splitting a rune, so the
// result is always valid UTF-8 and always encodes.
func clipBytes(text string, max int) string {
	if len(text) <= max {
		return text
	}
	clipped := text[:max]
	for len(clipped) > 0 && !utf8.ValidString(clipped) {
		clipped = clipped[:len(clipped)-1]
	}
	return clipped
}

// The payload readers below were the stderr trace's (trace.go, which stayed
// behind with the rest of senior-dev's command line); a step is read out of
// the same loosely typed bus payloads, so they came with it.

// object reads a payload value as a JSON object, converting a typed value
// through its JSON form when it is not already a map.
func object(value any) map[string]any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var mapped map[string]any
	if json.Unmarshal(raw, &mapped) != nil {
		return nil
	}
	return mapped
}

func mapAt(value map[string]any, key string) map[string]any { return object(valueAt(value, key)) }

func valueAt(value map[string]any, key string) any {
	if value == nil {
		return nil
	}
	return value[key]
}

func stringAt(value map[string]any, key string) string {
	result, _ := valueAt(value, key).(string)
	return result
}
