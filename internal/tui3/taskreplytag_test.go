package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestTaskReplyTagsStackAboveTheReplyWithVerbatimRequests(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	tags := []session.TaskReplyTag{
		{ID: 2, Title: "AgentField parallel search", Request: "find more details about agentfield parrallely"},
		{ID: 5, Title: "Check docs", Request: "Keep THIS capitalization"},
	}
	a.event(session.Event{Kind: session.EventTaskReplyTags, TaskReplyTags: tags})
	a.event(session.Event{Kind: session.EventTextDelta, Text: "The searches landed."})
	if len(a.entries) == 0 || len(a.entries[len(a.entries)-1].replyTags) != 2 {
		t.Fatalf("reply did not keep both tags: %#v", a.entries)
	}
	joined := plain(strings.Join(a.taskReplyTagRows(tags, 120), "\n"))
	for _, want := range []string{
		"find more details about agentfield parrallely",
		"Keep THIS capitalization",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tag changed or omitted the request %q:\n%s", want, joined)
		}
	}
	if strings.Index(joined, tags[0].Request) > strings.Index(joined, tags[1].Request) {
		t.Fatalf("tags are out of drain order:\n%s", joined)
	}
}

func TestEmptyTaskRequestRendersTheTitleWithoutEmptyQuotes(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	joined := plain(strings.Join(a.taskReplyTagRows([]session.TaskReplyTag{{ID: 3, Title: "Index sources"}}, 80), "\n"))
	if !strings.Contains(joined, "Index sources") || strings.Contains(joined, `""`) {
		t.Fatalf("empty request row = %q", joined)
	}
}

func TestPersonPromptedReplyHasNoTaskTag(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.event(session.Event{Kind: session.EventTextDelta, Text: "An ordinary answer."})
	if len(a.entries) == 0 || len(a.entries[len(a.entries)-1].replyTags) != 0 {
		t.Fatalf("ordinary answer has a task tag: %#v", a.entries)
	}
}

func TestResumedReplyRestoresItsTaskTag(t *testing.T) {
	tag := session.TaskReplyTag{ID: 8, Title: "Resume proof", Request: "show it after resume"}
	a := resumedApp(t,
		session.DisplayEntry{Role: "aside", Text: "task finished"},
		session.DisplayEntry{Role: "assistant", Text: "It is back.", ReplyTags: []session.TaskReplyTag{tag}},
	)
	for _, entry := range a.entries {
		if entry.kind == entryAssistant && len(entry.replyTags) == 1 && entry.replyTags[0] == tag {
			return
		}
	}
	t.Fatalf("resumed assistant did not restore its tag: %#v", a.entries)
}
