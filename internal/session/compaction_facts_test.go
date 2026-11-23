package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// journaledPersonHeavy is [personHeavy] written through the journal, as a
// real turn writes it, so the original lines are in the file above a marker.
func journaledPersonHeavy(agent *Agent, from, turns, bytes int) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for turn := from; turn < from+turns; turn++ {
		for _, message := range []ai.Message{
			textMessage("user", fmt.Sprintf("question %d: %s", turn, strings.Repeat("pasted log line ", bytes/16))),
			textMessage("assistant", fmt.Sprintf("answer %d", turn)),
		} {
			agent.messages = append(agent.messages, message)
			agent.file.appendMessage(message)
		}
	}
}

// observed runs one observed pass and answers what it said, in order.
func observed(t *testing.T, agent *Agent, ctx context.Context) ([]Event, error) {
	t.Helper()
	var events []Event
	err := agent.CompactObserved(ctx, func(event Event) { events = append(events, event) })
	return events, err
}

func passKinds(events []Event) []EventKind {
	out := make([]EventKind, 0, len(events))
	for _, event := range events {
		out = append(out, event.Kind)
	}
	return out
}

func summaryAgent(t *testing.T, model Completer) (*Agent, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, model, func(config *Config) {
		config.ContextWindow = 65_536
		config.SessionFile = path
	})
	return agent, path
}

// A SUMMARY IS SAID WHILE IT IS WRITTEN AND KEPT AS FACTS ONCE IT LANDS. The
// started event comes before the request and the settled one carries the
// summary the pass spliced, its model and what it stands for; the journal
// gives the same fact back.
func TestASummaryPassIsAnnouncedAndKeptAsFacts(t *testing.T) {
	var startedBeforeRequest bool
	var agent *Agent
	model := &summarizer{}
	agent, path := summaryAgent(t, model)
	journaledPersonHeavy(agent, 1, 12, 20_000)

	var events []Event
	err := agent.CompactObserved(context.Background(), func(event Event) {
		if event.Kind == EventSummaryStarted {
			startedBeforeRequest = model.calls() == 0
			if use := agent.ContextUse(); !use.Summarizing {
				t.Error("ContextUse does not say a summary is being written")
			}
		}
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("CompactObserved: %v", err)
	}
	got := passKinds(events)
	if len(got) != 3 || got[0] != EventSummaryStarted || got[1] != EventCompacting || got[2] != EventCompacted {
		t.Fatalf("events = %v, want started, compacting, compacted", got)
	}
	if !startedBeforeRequest {
		t.Fatal("the summary was announced after the request had been made")
	}
	if agent.ContextUse().Summarizing {
		t.Fatal("ContextUse still says summarizing after the pass")
	}
	fact := events[2].Compaction
	if fact == nil || fact.Seq != 1 || events[0].Compaction.Seq != 1 {
		t.Fatalf("settled fact = %+v; want sequence 1 on both ends", fact)
	}
	if fact.Summarized == 0 || !strings.HasPrefix(fact.Summary, summaryWords) || fact.Model != "test/model" || fact.Failed != "" {
		t.Fatalf("settled fact = %+v; want the spliced summary and its model", fact)
	}
	// Questions 10–12 are kept word for word; the summary stands for 1–9.
	if fact.Turns != 9 {
		t.Fatalf("Turns = %d, want the 9 person messages the summary replaced", fact.Turns)
	}
	if fact.TokensAfter <= 0 || fact.TokensAfter >= fact.TokensBefore || fact.CoversUntil.IsZero() {
		t.Fatalf("fact = %+v; want a real before → after and the region's last line's time", fact)
	}
	if !strings.Contains(messageText(liveTranscript(agent)[1]), fact.Summary) {
		t.Fatal("the fact's summary is not the note the model now reads")
	}

	record := ReadTranscript(path)
	if len(record.Passes) != 1 {
		t.Fatalf("journal passes = %d, want 1", len(record.Passes))
	}
	read := record.Passes[0]
	if read.Summary != fact.Summary || read.Turns != fact.Turns || read.Model != fact.Model ||
		read.TokensAfter != fact.TokensAfter || !read.At.Equal(fact.At) {
		t.Fatalf("journal fact = %+v\nlive fact    = %+v", read, *fact)
	}
	// The marker sits after every original line the pass replaced.
	if read.Entry != countDisplayed(record.Earlier) {
		t.Fatalf("marker at %d, want after the %d earlier entries", read.Entry, len(record.Earlier))
	}
}

func countDisplayed(entries []DisplayEntry) int { return len(entries) }

// REPEATED SUMMARIES KEEP EVERY ORIGINAL LINE IN THE RECORD AND NOTHING IN
// MEMORY. A reading of the file tells the whole conversation once, each pass
// in its place; the live agent and a resumed one hold only the latest hop.
func TestRepeatedSummariesKeepTheWholeRecordOnDiskOnly(t *testing.T) {
	model := &summarizer{}
	agent, path := summaryAgent(t, model)
	journaledPersonHeavy(agent, 1, 12, 20_000)
	if _, err := observed(t, agent, context.Background()); err != nil {
		t.Fatal(err)
	}
	journaledPersonHeavy(agent, 13, 12, 20_000)
	events, err := observed(t, agent, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second := events[len(events)-1].Compaction
	if second == nil || second.Seq != 2 || second.Summarized == 0 {
		t.Fatalf("second pass = %+v", second)
	}
	// The second summary folds the first in, so it stands for both regions.
	if second.Turns != 9+12 {
		t.Fatalf("second summary stands for %d turns, want 21", second.Turns)
	}

	record := ReadTranscript(path)
	if len(record.Passes) != 2 || record.Passes[0].Seq != 1 || record.Passes[1].Seq != 2 {
		t.Fatalf("passes = %+v", record.Passes)
	}
	told := append(append([]DisplayEntry(nil), record.Earlier...), record.Entries[record.Floor:]...)
	for turn := 1; turn <= 24; turn++ {
		seen := 0
		for _, entry := range told {
			if entry.Role == "user" && strings.HasPrefix(entry.Text, fmt.Sprintf("question %d:", turn)) {
				seen++
			}
		}
		if seen != 1 {
			t.Fatalf("question %d is told %d times in the record, want once", turn, seen)
		}
	}
	for _, entry := range told {
		if strings.Contains(entry.Text, summaryNotePrefix) {
			t.Fatal("a summary note is told as conversation in the record")
		}
	}
	first, last := record.Passes[0].Entry, record.Passes[1].Entry
	if !(0 < first && first < last && last <= len(record.Earlier)) {
		t.Fatalf("pass places %d, %d in an earlier region of %d", first, last, len(record.Earlier))
	}

	// THE LIVE AGENT HOLDS ONE HOP. Question 1 went into the first summary,
	// so the region the second pass replaced opens on that summary's note.
	live := agent.EarlierHistory()
	for _, entry := range live.Entries {
		if strings.HasPrefix(entry.Text, "question 1:") {
			t.Fatal("the live agent holds the whole history across passes")
		}
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: path, ContextWindow: 65_536}, &summarizer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if got := resumed.EarlierHistory(); len(got.Entries) != len(live.Entries) {
		t.Fatalf("resumed region %d entries, live %d: a resume must hold the same one hop", len(got.Entries), len(live.Entries))
	}
	// The sequence and the rolling provenance survive the restart.
	journaledPersonHeavy(resumed, 25, 12, 20_000)
	events, err = observed(t, resumed, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	third := events[len(events)-1].Compaction
	if third.Seq != 3 || third.Turns != 21+12 || !third.CoversFrom.Equal(second.CoversFrom) {
		t.Fatalf("third pass after resume = %+v; want sequence 3 standing for 33 turns from %v", third, second.CoversFrom)
	}
}

// A SUMMARY THAT FAILS IN A PASS THAT CHANGED NOTHING IS A FACT, NOT SILENCE.
// The conversation is untouched, the surface hears the failure, and the
// journal keeps it without a marker.
func TestAFailedSummaryIsSaidAndKeptWithoutTouchingTheConversation(t *testing.T) {
	model := &summarizer{answer: func(int, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("provider unavailable")
	}}
	agent, path := summaryAgent(t, model)
	agent.mu.Lock()
	for turn := 1; turn <= 12; turn++ {
		// Nothing a fold may take: every assistant line is as long as it
		// can be without folding being worth it, so only a summary helps.
		for _, message := range []ai.Message{
			textMessage("user", fmt.Sprintf("question %d: %s", turn, strings.Repeat("pasted log line ", 1250))),
		} {
			agent.messages = append(agent.messages, message)
			agent.file.appendMessage(message)
		}
	}
	agent.mu.Unlock()
	before := liveTranscript(agent)

	events, err := observed(t, agent, context.Background())
	var nothing *NothingToCompact
	if !errors.As(err, &nothing) {
		t.Fatalf("err = %v, want nothing to compact", err)
	}
	got := passKinds(events)
	if len(got) != 2 || got[0] != EventSummaryStarted || got[1] != EventSummaryFailed {
		t.Fatalf("events = %v, want started then failed", got)
	}
	failed := events[1].Compaction
	if failed.Failed != SummaryFailedError || failed.Seq != 1 || failed.Summarized != 0 || failed.Summary != "" {
		t.Fatalf("failure = %+v", failed)
	}
	if after := liveTranscript(agent); len(after) != len(before) {
		t.Fatalf("a failed summary changed the conversation: %d → %d messages", len(before), len(after))
	}
	record := ReadTranscript(path)
	if len(record.Passes) != 1 || record.Passes[0].Failed != SummaryFailedError || len(record.Earlier) != 0 {
		t.Fatalf("record passes = %+v, earlier %d; want one failure and no marker", record.Passes, len(record.Earlier))
	}
	if record.Passes[0].Entry != len(record.Entries) {
		t.Fatalf("failure placed at %d, want the end of the %d entries", record.Passes[0].Entry, len(record.Entries))
	}
}

// AN INTERRUPTED SUMMARY SAYS SO, and the conversation keeps every word.
func TestAnInterruptedSummaryEndsAsInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	model := &summarizer{answer: func(int, []ai.Message) (*ai.Response, error) {
		cancel()
		return nil, context.Canceled
	}}
	agent, _ := summaryAgent(t, model)
	agent.mu.Lock()
	for turn := 1; turn <= 12; turn++ {
		message := textMessage("user", fmt.Sprintf("question %d: %s", turn, strings.Repeat("pasted log line ", 1250)))
		agent.messages = append(agent.messages, message)
	}
	agent.mu.Unlock()
	before := len(liveTranscript(agent))
	events, _ := observed(t, agent, ctx)
	last := events[len(events)-1]
	if last.Kind != EventSummaryFailed || last.Compaction.Failed != SummaryFailedInterrupted {
		t.Fatalf("last event = %v %+v, want an interrupted summary", last.Kind, last.Compaction)
	}
	if after := len(liveTranscript(agent)); after != before {
		t.Fatalf("an interrupted summary changed the conversation: %d → %d", before, after)
	}
	if agent.ContextUse().Summarizing {
		t.Fatal("still summarizing after the interrupt")
	}
}

// A PASS THAT ONLY STUBS OR FOLDS NEVER CLAIMS A SUMMARY. No model was asked,
// so nothing says one is being written and the fact carries no summary.
func TestAMechanicalPassClaimsNoSummary(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 131_072
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	grownTranscript(agent, 70, 8_000)
	hub := newEventHub()
	if changed, err := agent.compact(context.Background(), hub); err != nil || !changed {
		t.Fatalf("compact = %v, %v; want a fold", changed, err)
	}
	for _, event := range hub.backlog {
		if event.Kind == EventSummaryStarted {
			t.Fatal("a mechanical pass announced a summary")
		}
	}
	fact := lastCompacted(t, hub).Compaction
	if fact == nil || !fact.Mechanical() || fact.Summary != "" || fact.Model != "" {
		t.Fatalf("mechanical fact = %+v", fact)
	}
}

// AN UNKNOWN WINDOW IS UNKNOWN. With no catalog, no configuration and no
// endpoint saying what the window is, the default the package guards with is
// never reported as the model's window.
func TestContextUseNeverReportsAGuessedWindow(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, nil)
	if use := agent.ContextUse(); use.Window != 0 || use.Threshold != 0 {
		t.Fatalf("ContextUse = %+v, want an unknown window", use)
	}
	known, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) { config.ContextWindow = 65_536 })
	use := known.ContextUse()
	if use.Window != 65_536 || use.Threshold <= 0 || use.Threshold >= use.Window {
		t.Fatalf("ContextUse = %+v, want the configured window and its threshold", use)
	}
	if use.Tokens != known.ContextTokens() {
		t.Fatalf("ContextUse tokens %d, ContextTokens %d: one figure", use.Tokens, known.ContextTokens())
	}
}

// A PASS STANDS WHERE IT BEGAN. A message that arrives while the summary is
// being written came after the pass started, and the record places the pass
// before it — the same place a live surface drew it when it began.
func TestAMessageArrivingDuringASummaryIsPlacedAfterThePass(t *testing.T) {
	var agent *Agent
	model := &summarizer{answer: func(_ int, _ []ai.Message) (*ai.Response, error) {
		// The person steers while the request is out: journaled like any
		// message, under the lock the pass released.
		journaledPersonHeavy(agent, 13, 1, 64)
		return textResponse(summaryWords), nil
	}}
	agent, path := summaryAgent(t, model)
	journaledPersonHeavy(agent, 1, 12, 20_000)
	events, err := observed(t, agent, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	started, settled := events[0].Compaction, events[len(events)-1].Compaction
	if started.StartedAt.IsZero() || !settled.StartedAt.Equal(started.StartedAt) || settled.Arrived != 2 {
		t.Fatalf("started %+v settled %+v; want one start instant and the 2 arrived messages", started, settled)
	}
	record := ReadTranscript(path)
	if len(record.Passes) != 1 || !record.Passes[0].StartedAt.Equal(started.StartedAt) || record.Passes[0].Arrived != 2 {
		t.Fatalf("journal pass = %+v", record.Passes)
	}
	told := append(append([]DisplayEntry(nil), record.Earlier...), record.Entries[record.Floor:]...)
	place := record.Passes[0].Entry
	if place <= 0 || place >= len(told) {
		t.Fatalf("pass placed at %d of %d", place, len(told))
	}
	if !strings.HasPrefix(told[place].Text, "question 13:") || !strings.HasPrefix(told[place-2].Text, "question 12:") {
		t.Fatalf("pass placed before %.20q after %.20q; want between question 12's turn and the message that arrived", told[place].Text, told[place-2].Text)
	}
	seen := 0
	for _, entry := range told {
		if strings.HasPrefix(entry.Text, "question 13:") {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the arrived message is told %d times, want once", seen)
	}
}
