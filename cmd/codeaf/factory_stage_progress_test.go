package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestStageProseProgressRelaysCommittedModelMessagesOnly(t *testing.T) {
	var updates []string
	c := &stageConversation{captions: make(chan string, 20), progress: func(text string) { updates = append(updates, text) }}
	events := make(chan session.Event, 20)
	for _, event := range []session.Event{
		{Kind: session.EventReasoning, Text: "Private reasoning must stay private"},
		{Kind: session.EventTextDelta, Text: "I found the audit failure. "},
		{Kind: session.EventTextDelta, Text: "I am checking the safe upgrade."},
		{Kind: session.EventToolBegin, Tool: "read", Hint: "package.json"},
		{Kind: session.EventToolFailed, Tool: "read", Hint: "output is never model progress"},
		{Kind: session.EventTextDelta, Text: "discarded retry fragment"},
		{Kind: session.EventRetrying},
		{Kind: session.EventTextDelta, Text: "The focused checks passed locally."},
		{Kind: session.EventAssistantDone},
		{Kind: session.EventTextDelta, Text: "discarded failed response"},
		{Kind: session.EventError, Err: errors.New("transport failed")},
		{Kind: session.EventTextDelta, Text: "unfinished tail"},
	} {
		events <- event
	}
	close(events)
	c.drain(events)
	if len(updates) != 2 || updates[0] != "I found the audit failure. I am checking the safe upgrade." || updates[1] != "The focused checks passed locally." {
		t.Fatalf("updates: %q", updates)
	}
}

func TestStageProseProgressBufferIsBoundedAndOtherStagesRemainQuiet(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		var updates []string
		c := &stageConversation{captions: make(chan string, 1)}
		if enabled {
			c.progress = func(text string) { updates = append(updates, text) }
		}
		events := make(chan session.Event, 4)
		events <- session.Event{Kind: session.EventTextDelta, Text: strings.Repeat("x", 100000)}
		events <- session.Event{Kind: session.EventTextDelta, Text: "more"}
		events <- session.Event{Kind: session.EventAssistantDone}
		close(events)
		c.drain(events)
		if enabled && (len(updates) != 1 || len(updates[0]) != 4096) {
			t.Fatalf("unbounded capture: %d", len(updates))
		}
		if !enabled && len(updates) > 0 {
			t.Fatal("ordinary stage acquired update relay")
		}
	}
}
