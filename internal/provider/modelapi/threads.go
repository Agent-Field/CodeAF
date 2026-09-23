package modelapi

// What the program said that it had not said before.
//
// A program talks to its model the way every chat client does: each request
// carries the whole conversation so far. Written down whole, one call's record
// would repeat every call before it, and the task page would draw the same
// brief forty times. So each thread's previous request is remembered — as one
// fingerprint per message, never the text — and a turn records only what came
// after it: the brief the first time, then the tools' results and the
// program's own words.
//
// A PROGRAM THAT REWRITES ITS HISTORY IS SAID TO HAVE DONE SO. When a request
// is not the previous one with more added — a compaction, a summary of old
// turns in place of the turns — nothing is a delta of anything, so the turn is
// marked Restarted and records what the program sent, whole (capped where the
// log is written, delegate.AppendTurn).
//
// THE MODEL'S OWN REPLIES ARE NOT SENT WORDS. An assistant message on a request
// is the program handing the model's last answer back to it; the page already
// drew that answer on the turn that produced it, so it is skipped here.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
)

// threads is every thread's previous request, as fingerprints. It is guarded by
// the server's lock.
type threads struct {
	previous map[string][]string
}

// delta answers what this request adds to the thread's previous one, and
// remembers this request as the thread's previous from now on — whatever the
// call comes to, because "previous" is the previous request, not the previous
// answer.
func (t *threads) delta(thread string, messages []ai.Message) (sent []delegate.Said, restarted bool) {
	if t.previous == nil {
		t.previous = map[string][]string{}
	}
	prints := make([]string, len(messages))
	for index, message := range messages {
		prints[index] = fingerprint(message)
	}
	before := t.previous[thread]
	start := len(before)
	if !extends(prints, before) {
		restarted, start = true, 0
	}
	t.previous[thread] = prints
	names := toolNames(messages)
	for _, message := range messages[start:] {
		if message.Role == "assistant" {
			continue
		}
		sent = append(sent, said(message, names))
	}
	return sent, restarted
}

// extends reports whether a request is the previous one with messages added.
func extends(now, before []string) bool {
	if len(before) > len(now) {
		return false
	}
	for index, print := range before {
		if now[index] != print {
			return false
		}
	}
	return true
}

// fingerprint is one message's identity: its role, every part of its content,
// its tool calls and the call it answers. It is a hash so a thread's memory is
// a few dozen bytes a message whatever the message weighed.
func fingerprint(message ai.Message) string {
	encoded, _ := json.Marshal(struct {
		Role       string           `json:"r"`
		Content    []ai.ContentPart `json:"c"`
		ToolCalls  []ai.ToolCall    `json:"t"`
		ToolCallID string           `json:"i"`
	}{message.Role, message.Content, message.ToolCalls, message.ToolCallID})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:12])
}

// toolNames maps every tool call on the request to the tool it named, so a
// tool's result can say which tool it answers.
func toolNames(messages []ai.Message) map[string]string {
	names := map[string]string{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.ID != "" {
				names[call.ID] = call.Function.Name
			}
		}
	}
	return names
}

// said is one message as the log writes it: whose, which tool it answers, and
// its words, with a word in brackets standing for anything that is not text.
func said(message ai.Message, names map[string]string) delegate.Said {
	var words []string
	for _, part := range message.Content {
		switch part.Type {
		case "text":
			if part.Text != "" {
				words = append(words, part.Text)
			}
		case "image_url":
			words = append(words, "[image]")
		case "video_url":
			words = append(words, "[video]")
		case "input_audio":
			words = append(words, "[audio]")
		case "file":
			words = append(words, "[file]")
		default:
			if part.Type != "" {
				words = append(words, "["+part.Type+"]")
			}
		}
	}
	entry := delegate.Said{Role: message.Role, Text: strings.Join(words, "\n")}
	if message.Role == "tool" {
		entry.Tool = names[message.ToolCallID]
	}
	return entry
}
