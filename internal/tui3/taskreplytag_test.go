package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
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

func TestTaskReplySourcesUseSharedDisclosureAcrossSurfaces(t *testing.T) {
	tags := []session.TaskReplyTag{
		{ID: 1, Title: "Reading source one", Request: strings.Repeat("PRIVATE_ORIGINAL_REQUEST ", 20)},
		{ID: 2, Title: "Reading source two", Request: "SECOND_SOURCE_REQUEST"},
	}
	for _, width := range []int{40, 140} {
		for _, mode := range []lens{participantLens, overseerLens, transcriptLens} {
			for _, live := range []bool{false, true} {
				a := newTestApp(&fakeAgent{model: "m"})
				a.turn = 1
				a.entries = []entry{{kind: entryUser, text: "Build the report", turn: 1}}
				a.ingest(session.Event{Kind: session.EventTaskReplyTags, TaskReplyTags: tags})
				a.ingest(session.Event{Kind: session.EventTextDelta, Text: "[update] The totals are verified."})
				a.ingest(session.Event{Kind: session.EventAssistantDone})
				d := deck{entries: a.entries, lens: mode, workOpen: map[int]bool{}, capOpen: map[int]bool{}}
				if live {
					d.runningTurn = 1
				}
				text := func() string {
					rendered, _ := a.deckRows(d, width)
					var lines []string
					for _, r := range rendered {
						lines = append(lines, plain(r.text))
					}
					return strings.Join(lines, "\n")
				}
				closed := text()
				if strings.Contains(closed, "PRIVATE_ORIGINAL_REQUEST") || strings.Contains(closed, "SECOND_SOURCE_REQUEST") || strings.Contains(closed, "Reading source") {
					t.Fatalf("width %d live %v: source metadata escaped work disclosure:\n%s", width, live, closed)
				}
				if !strings.Contains(closed, "The totals are verified.") {
					t.Fatalf("reply disappeared: %s", closed)
				}
				if key, ok := a.liveWorkOf(d); ok {
					d.workOpen[key] = true
				}
				for _, fold := range a.deckFolds(d) {
					d.workOpen[fold.key] = true
				}
				opened := strings.Join(strings.Fields(text()), " ")
				for _, tag := range tags {
					if !strings.Contains(opened, strings.Join(strings.Fields(tag.Request), " ")) || !strings.Contains(opened, tag.Title) {
						t.Fatalf("width %d live %v: disclosed source lost data: %s", width, live, opened)
					}
				}
			}
		}
	}
}

func TestTaskReplySourcesResumeBehindWorkDisclosure(t *testing.T) {
	tag := session.TaskReplyTag{ID: 4, Title: "Reading source", Request: strings.Repeat("PRIVATE_RESUMED_REQUEST ", 10)}
	a := resumedApp(t,
		session.DisplayEntry{Role: "user", Text: "Produce a handoff"},
		session.DisplayEntry{Role: "assistant", Text: "The handoff is ready.", Answer: true, ReplyTags: []session.TaskReplyTag{tag}},
	)
	closed := strings.Join(plainRows(a), "\n")
	if strings.Contains(closed, "PRIVATE_RESUMED_REQUEST") || !strings.Contains(closed, "The handoff is ready.") {
		t.Fatalf("resumed sources escaped disclosure or reply disappeared:\n%s", closed)
	}
	drive(t, a, key("ctrl+e"))
	opened := strings.Join(strings.Fields(strings.Join(plainRows(a), "\n")), " ")
	if !strings.Contains(opened, strings.TrimSpace(tag.Request)) {
		t.Fatalf("resumed source cannot be opened: %s", opened)
	}
	drive(t, a, key("ctrl+e"))
	if strings.Contains(strings.Join(plainRows(a), "\n"), "PRIVATE_RESUMED_REQUEST") {
		t.Fatal("closing the work disclosure left source details visible")
	}
}

func TestTaskReplySourcesArrivingDuringProseKeepTheLiveAnswerWhole(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.turn = 1
	a.entries = []entry{{kind: entryUser, text: "Review the totals", turn: 1}}
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "The report "})
	live := a.live
	tag := session.TaskReplyTag{ID: 7, Title: "Reading task", Request: "PRIVATE_LATE_REQUEST"}
	a.ingest(session.Event{Kind: session.EventTaskReplyTags, TaskReplyTags: []session.TaskReplyTag{tag}})
	if a.live != live {
		t.Fatal("source metadata displaced the live answer")
	}
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "is ready."})
	a.ingest(session.Event{Kind: session.EventAssistantDone})
	if a.entries[live].text != "The report is ready." || len(a.entries[live].replyTags) != 1 {
		t.Fatal("late source metadata split the answer or lost provenance")
	}
	closed := strings.Join(plainRows(a), "\n")
	if strings.Contains(closed, tag.Request) || strings.Count(closed, "The report is ready.") != 1 {
		t.Fatalf("late source escaped disclosure or changed the answer:\n%s", closed)
	}
	drive(t, a, key("ctrl+e"))
	if !strings.Contains(strings.Join(plainRows(a), "\n"), tag.Request) {
		t.Fatal("late source cannot be inspected")
	}
}

func TestTaskReplySourcesKeepTaskLinksWhenOpened(t *testing.T) {
	a, _, _ := roomApp(t)
	a.turn = 1
	a.entries = []entry{{kind: entryUser, text: "Inspect the result", turn: 1}}
	a.ingest(session.Event{Kind: session.EventTaskReplyTags, TaskReplyTags: []session.TaskReplyTag{{ID: 7, Title: "Source", Request: "Inspect task 7"}}})
	a.ingest(session.Event{Kind: session.EventTextDelta, Text: "The result is ready."})
	a.ingest(session.Event{Kind: session.EventAssistantDone})
	drive(t, a, key("ctrl+e"))
	if _, _, ok := linkedRow(a); !ok {
		t.Fatal("disclosed source lost its task link")
	}
}
