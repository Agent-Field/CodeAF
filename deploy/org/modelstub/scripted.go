package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// scriptedPrefix opens a message that is a shell command to run, not a request
// to a model: `bash: echo hi > f && git status --short`.
const scriptedPrefix = "bash:"

// scriptedLimit is how much of a command's output is quoted back; a transcript
// line, not a log.
const scriptedLimit = 4000

// scriptedCommand is the command in a `bash:` message, "" when the message is
// anything else.
func scriptedCommand(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, scriptedPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(text, scriptedPrefix))
}

// scriptedBash answers a conversation whose last turn is a scripted command:
// a `bash:` user message gets the tool call, and the tool's answer to it gets
// the quote. It reports whether it answered.
func scriptedBash(w http.ResponseWriter, messages []message, n int64) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	switch strings.ToLower(last.Role) {
	case "user":
		command := scriptedCommand(last.text())
		if command == "" {
			return false
		}
		args, _ := json.Marshal(map[string]string{"command": command})
		stream(w, fmt.Sprintf("call_%d", n), "bash", string(args), "")
		return true
	case "tool":
		// Only a tool answer to a scripted command is quoted: walk back to the
		// user message that started this exchange.
		for i := len(messages) - 2; i >= 0; i-- {
			if !strings.EqualFold(messages[i].Role, "user") {
				continue
			}
			if scriptedCommand(messages[i].text()) == "" {
				return false
			}
			out := last.text()
			if len(out) > scriptedLimit {
				out = out[:scriptedLimit]
			}
			stream(w, "", "", "", "STUB ran: "+out)
			return true
		}
	}
	return false
}
