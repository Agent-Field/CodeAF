package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// scriptEvents is a script that made two calls, the second of which failed.
func scriptEvents() []session.Event {
	return []session.Event{
		{Kind: session.EventToolBegin, Tool: "script", CallID: "s", Hint: "script"},
		{Kind: session.EventToolBegin, Tool: "bash", CallID: "s-script-1", Parent: "s", Hint: "ls", Args: `{"command":"ls"}`},
		{Kind: session.EventToolEnd, Tool: "bash", CallID: "s-script-1", Parent: "s"},
		{Kind: session.EventToolBegin, Tool: "bash", CallID: "s-script-2", Parent: "s", Hint: "cat x", Args: `{"command":"cat x"}`},
		{Kind: session.EventToolFailed, Tool: "bash", CallID: "s-script-2", Parent: "s", Hint: "no such file"},
		{Kind: session.EventToolEnd, Tool: "script", CallID: "s"},
	}
}

// A STAGE'S LOG CAPTIONS A SCRIPT AS ONE CALL. The calls it made are the
// script's own data, and a failure the script caught is not the stage's.
func TestAStageCaptionsAScriptAsOneCall(t *testing.T) {
	c := &stageConversation{captions: make(chan string, 16)}
	events := make(chan session.Event, 16)
	for _, event := range scriptEvents() {
		events <- event
	}
	close(events)
	c.drain(events)
	close(c.captions)
	var tools []string
	for caption := range c.captions {
		if strings.HasPrefix(caption, factory.ToolCaptionPrefix) {
			tools = append(tools, caption)
		}
	}
	if len(tools) != 1 || !strings.Contains(tools[0], "script") {
		t.Errorf("tool captions = %q; want the script's one", tools)
	}
}

// A HEADLESS TURN PRINTS A SCRIPT'S CALLS INDENTED UNDER IT, so the log reads
// as one step with its calls rather than as steps of their own.
func TestAHeadlessTurnIndentsAScriptsCalls(t *testing.T) {
	events := make(chan session.Event, 16)
	for _, event := range scriptEvents() {
		events <- event
	}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	var stdout, stderr bytes.Buffer
	if err := drainOnceEvents(events, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	var top, nested int
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "tool:   "):
			nested++
		case strings.HasPrefix(line, "tool: "):
			top++
		}
	}
	if top != 1 || nested != 3 {
		t.Errorf("stderr has %d top-level and %d indented tool lines; want 1 and 3:\n%s", top, nested, stderr.String())
	}
}
