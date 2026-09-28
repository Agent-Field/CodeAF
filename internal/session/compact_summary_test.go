package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// summaryWords is a summary the prose check accepts.
const summaryWords = "The person asked for a long story about a watchmaker and the assistant wrote it."

// summarizer answers every request with a summary and keeps what it was sent.
type summarizer struct {
	mu       sync.Mutex
	asks     [][]ai.Message
	answer   func(call int, messages []ai.Message) (*ai.Response, error)
	requests int
}

func (s *summarizer) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	s.mu.Lock()
	s.asks = append(s.asks, append([]ai.Message(nil), messages...))
	s.requests++
	call := s.requests
	answer := s.answer
	s.mu.Unlock()
	if answer != nil {
		return answer(call, messages)
	}
	return textResponse(fmt.Sprintf("%s (summary %d)", summaryWords, call)), nil
}

func (s *summarizer) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// personHeavy appends conversation whose weight is the person's own words:
// long questions, short answers, nothing a fold or a stub may take.
func personHeavy(agent *Agent, turns, bytes int) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for turn := 1; turn <= turns; turn++ {
		agent.messages = append(agent.messages,
			textMessage("user", fmt.Sprintf("question %d: %s", turn, strings.Repeat("pasted log line ", bytes/16))),
			textMessage("assistant", fmt.Sprintf("answer %d", turn)))
	}
}

func summaryNotes(messages []ai.Message) int {
	notes := 0
	for _, message := range messages {
		if message.Role == "user" && strings.HasPrefix(messageText(message), summaryNotePrefix) {
			notes++
		}
	}
	return notes
}

// THE DEAD END ENDS IN A SUMMARY. A conversation that is mostly the person's
// own words has nothing the free rungs may take; the pass used to report
// nothing to compact while the window filled.
func TestAPassTheFreeRungsCannotShrinkEndsWithASummary(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(config *Config) {
		config.ContextWindow = 65_536
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	personHeavy(agent, 12, 20_000)
	before := estimate(agent)
	if before <= agent.compactThreshold() {
		t.Fatalf("fixture %d is under the threshold %d", before, agent.compactThreshold())
	}

	hub := newEventHub()
	changed, err := agent.compact(context.Background(), hub)
	if err != nil || !changed {
		t.Fatalf("compact = %v, %v; want a pass that summarized", changed, err)
	}
	if model.calls() != 1 {
		t.Fatalf("summary requests = %d, want 1", model.calls())
	}
	messages := liveTranscript(agent)
	if summaryNotes(messages) != 1 || !strings.HasPrefix(messageText(messages[1]), summaryNotePrefix) {
		t.Fatalf("the summary note is not where the region was: %q", messageText(messages[1]))
	}
	if !strings.Contains(messageText(messages[1]), summaryWords) {
		t.Fatal("the note does not carry what the model wrote")
	}
	// THE MOST RECENT PERSON MESSAGES STAY WORD FOR WORD, with what follows.
	for turn := 10; turn <= 12; turn++ {
		if !holdsText(messages, fmt.Sprintf("question %d:", turn)) || !holdsText(messages, fmt.Sprintf("answer %d", turn)) {
			t.Fatalf("recent turn %d was summarized", turn)
		}
	}
	if holdsText(messages, "question 1:") {
		t.Fatal("the oldest question survived the summary")
	}
	if after := estimate(agent); after >= agent.compactTargetTokens() {
		t.Fatalf("estimate after the summary = %d, want under the target %d", after, agent.compactTargetTokens())
	}
	// The summarizer read the region as text, with no tools to call.
	ask := model.asks[0]
	if len(ask) != 2 || ask[0].Role != "system" || !strings.Contains(messageText(ask[1]), "PERSON: question 1:") {
		t.Fatalf("summary request = %+v", ask)
	}
	if strings.Contains(messageText(ask[1]), "question 10:") {
		t.Fatal("a kept message was sent to be summarized")
	}
	if event := lastCompacted(t, hub); !strings.Contains(event.Hint, "summarized 18 messages") {
		t.Fatalf("hint = %q", event.Hint)
	}
}

// A FAILED SUMMARY CHANGES NOTHING. The pass reports what the free rungs did,
// which here is nothing, and the conversation keeps every word.
func TestAFailedSummaryLeavesTheConversationAlone(t *testing.T) {
	model := &summarizer{answer: func(int, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("provider unavailable")
	}}
	agent, _ := newTestAgent(t, model, func(config *Config) { config.ContextWindow = 65_536 })
	personHeavy(agent, 12, 20_000)
	before := liveTranscript(agent)

	agent.compact(context.Background(), newEventHub())
	after := liveTranscript(agent)
	if summaryNotes(after) != 0 {
		t.Fatal("a failed summary left a note")
	}
	// The fold may still take the short answers; every question stays.
	for turn := 1; turn <= 12; turn++ {
		if !holdsText(after, fmt.Sprintf("question %d:", turn)) {
			t.Fatalf("question %d was lost to a summary that failed (%d messages, was %d)", turn, len(after), len(before))
		}
	}
}

// AN ANSWER THAT IS NOT A SUMMARY IS REFUSED like a failed call.
func TestASummaryThatIsNotProseIsRefused(t *testing.T) {
	model := &summarizer{answer: func(int, []ai.Message) (*ai.Response, error) {
		return textResponse("<|tool_call|>"), nil
	}}
	agent, _ := newTestAgent(t, model, func(config *Config) { config.ContextWindow = 65_536 })
	personHeavy(agent, 12, 20_000)
	agent.compact(context.Background(), newEventHub())
	messages := liveTranscript(agent)
	if summaryNotes(messages) != 0 || !holdsText(messages, "question 1:") {
		t.Fatal("markup was spliced in as a summary")
	}
}

// THE CALL IS MADE WITHOUT THE LOCK, so the transcript can move under it. A
// region that is no longer what was summarized keeps its shape.
func TestASummaryIsDroppedWhenTheConversationMovedUnderIt(t *testing.T) {
	var agent *Agent
	model := &summarizer{answer: func(int, []ai.Message) (*ai.Response, error) {
		agent.mu.Lock()
		agent.messages[1] = textMessage("user", "question 1: rewritten while the summary was written")
		agent.mu.Unlock()
		return textResponse(summaryWords), nil
	}}
	agent, _ = newTestAgent(t, model, func(config *Config) { config.ContextWindow = 65_536 })
	personHeavy(agent, 12, 20_000)
	agent.compact(context.Background(), newEventHub())
	messages := liveTranscript(agent)
	if summaryNotes(messages) != 0 || !holdsText(messages, "rewritten while the summary was written") {
		t.Fatal("a summary of a region that changed was spliced over the change")
	}
}

// A SECOND SUMMARY EXTENDS THE FIRST rather than stacking a second note, and
// it is handed the first as the summary so far.
func TestASecondSummaryFoldsTheFirstIn(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(config *Config) { config.ContextWindow = 65_536 })
	personHeavy(agent, 12, 20_000)
	if _, err := agent.compact(context.Background(), newEventHub()); err != nil {
		t.Fatal(err)
	}
	personHeavy(agent, 12, 20_000)
	if _, err := agent.compact(context.Background(), newEventHub()); err != nil {
		t.Fatal(err)
	}
	// The second region may take more than one request; the first of them is
	// the one that has to be handed the earlier summary.
	if model.calls() < 2 {
		t.Fatalf("summary requests = %d, want at least 2", model.calls())
	}
	messages := liveTranscript(agent)
	if summaryNotes(messages) != 1 {
		t.Fatalf("summary notes = %d, want the one rolling note", summaryNotes(messages))
	}
	second := messageText(model.asks[1][1])
	if !strings.Contains(second, "Summary so far:") || !strings.Contains(second, "(summary 1)") {
		t.Fatalf("the second summary was not handed the first: %.300q", second)
	}
	if !strings.Contains(messageText(messages[1]), fmt.Sprintf("(summary %d)", model.calls())) {
		t.Fatal("the note does not carry the newest summary")
	}
}

// A NOTE ALONE IS NOT WORTH SUMMARIZING AGAIN. With nothing new above the kept
// messages, the pass must not pay the model to rewrite its own summary.
func TestAnEarlierSummaryAloneIsNotSummarizedAgain(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) { config.ContextWindow = 65_536 })
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", summaryNote(strings.Repeat("an earlier summary sentence. ", 400), "grep or read x")))
	agent.mu.Unlock()
	personHeavy(agent, 1, 200_000)
	if _, err := agent.compact(context.Background(), newEventHub()); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("compact = %v, want ErrNothingToCompact", err)
	}
}

// A REGION LARGER THAN ONE REQUEST IS SUMMARIZED IN CHUNKS, each handed the
// summary so far, and no request is larger than the window allows.
func TestALargeRegionIsSummarizedInChunks(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(config *Config) { config.ContextWindow = 16_384 })
	personHeavy(agent, 10, 24_000)
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if model.calls() < 2 {
		t.Fatalf("summary requests = %d, want the region split", model.calls())
	}
	limit := (16_384 - summaryAnswerTokens(16_384) - provider.ContextSafetyTokens(16_384)) * bytesPerToken
	for index, ask := range model.asks {
		size := len(messageText(ask[0])) + len(messageText(ask[1]))
		if size > limit {
			t.Fatalf("request %d is %d bytes, over the %d the window allows", index+1, size, limit)
		}
		if index > 0 && !strings.Contains(messageText(ask[1]), "Summary so far:") {
			t.Fatalf("request %d was not handed the summary so far", index+1)
		}
	}
	messages := liveTranscript(agent)
	if summaryNotes(messages) != 1 || !holdsText(messages, "question 10:") {
		t.Fatal("the chunked summary did not land, or took the newest question")
	}
}

// THE REPORTED CASE, end to end: one question, one long answer, the next
// question refused for size. The free rungs protect all of it; the recovery's
// summary takes the first exchange and the refused request goes again.
func TestARefusedRequestIsRecoveredByASummary(t *testing.T) {
	story := strings.Repeat("The watchmaker listened as the clocks kept time. ", 400)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, &provider.APIError{Status: 400, Overflow: true, Local: true, ContextLimit: 16_384,
				InputTokens: 15_383, OutputTokens: 512, Message: "context needs shortening before sending"}
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if len(messages) != 2 || !strings.Contains(messageText(messages[1]), "PERSON: write me a story") {
				t.Errorf("the second request was not the summary: %d messages", len(messages))
			}
			return textResponse(summaryWords), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if holdsText(messages, "The watchmaker listened") {
				t.Error("the retried request still carried the story")
			}
			if !holdsText(messages, "now work in the streisand effect") {
				t.Error("the retried request lost the question it was answering")
			}
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.ContextWindow = 16_384 })
	agent.mu.Lock()
	agent.messages = append(agent.messages, textMessage("user", "write me a story"), textMessage("assistant", story))
	agent.mu.Unlock()
	for _, event := range collect(t, mustSubmit(t, agent, "now work in the streisand effect")) {
		if event.Kind == EventError {
			t.Fatal(event.Err)
		}
	}
	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want refusal, summary, retry", completer.requests())
	}
}

// A SUMMARY IS DRAWN AS THE SESSION'S NOTE, never as words the person typed.
func TestASummaryIsDrawnAsANoteNotAPersonsMessage(t *testing.T) {
	entries := shapeEntries([]ai.Message{
		textMessage("system", "prompt"),
		textMessage("user", summaryNote(summaryWords, "grep or read x")),
		textMessage("user", "the next question"),
	}, nil)
	if len(entries) != 2 || entries[0].Role != "note" || entries[1].Role != "user" {
		t.Fatalf("entries = %+v", entries)
	}
}

// A SUMMARIZED CONVERSATION RESUMES AS ITSELF. The note rides the rebuilt
// window behind the compaction marker like any other line, so the reopened
// transcript is the one the pass left.
func TestASummarizedConversationResumesWithItsSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	live, workspace := newTestAgent(t, &summarizer{}, func(config *Config) {
		config.ContextWindow = 65_536
		config.SessionFile = path
	})
	personHeavy(live, 12, 20_000)
	if _, err := live.compact(context.Background(), newEventHub()); err != nil {
		t.Fatal(err)
	}
	want := liveTranscript(live)
	if summaryNotes(want) != 1 {
		t.Fatal("the live conversation was not summarized")
	}
	if err := live.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	resumed, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	got := liveTranscript(resumed)
	if len(got) != len(want) || messageText(got[1]) != messageText(want[1]) {
		t.Fatalf("resumed %d messages starting %.80q, want %d starting %.80q",
			len(got), messageText(got[1]), len(want), messageText(want[1]))
	}
	for index := 2; index < len(want); index++ {
		if messageText(got[index]) != messageText(want[index]) {
			t.Fatalf("message %d differs after resume", index)
		}
	}
}
