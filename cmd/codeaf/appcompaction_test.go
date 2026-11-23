package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// contextConversation is a fake conversation that can say its weight.
type contextConversation struct {
	*fakeConversation
	use session.ContextUse
}

func (c *contextConversation) ContextUse() session.ContextUse { return c.use }

// A PASS IN A TURN IS ITS FACTS ON THE WIRE. The summarizing entry is replaced
// by how it ended under the same id; a pass that only stubbed never says it
// summarized; a failure says why and whether the context is full; and the
// agent's context rides agent.changed.
func TestAppLivePassesAreEntriesNamedByTheirSequence(t *testing.T) {
	secret := "sk-or-v1-" + strings.Repeat("a1b2", 16)
	conv := &contextConversation{fakeConversation: &fakeConversation{script: []session.Event{
		{Kind: session.EventSummaryStarted, Compaction: &session.CompactionFact{Seq: 1, TokensBefore: 182_000}},
		{Kind: session.EventCompacting, Hint: "compacting ~182k tokens"},
		{Kind: session.EventCompacted, Summarized: 40, Compaction: &session.CompactionFact{
			Seq: 1, TokensBefore: 182_000, TokensAfter: 21_000, Summarized: 40,
			Summary: "Goal. Retry webhooks with key " + secret, Model: "deepseek/deepseek-v4.1-flash", Turns: 18,
		}},
		{Kind: session.EventCompacted, Compaction: &session.CompactionFact{Seq: 2, TokensBefore: 60_000, TokensAfter: 40_000, Stubbed: 3}},
		{Kind: session.EventSummaryFailed, Compaction: &session.CompactionFact{Seq: 3, TokensBefore: 120_000, TokensAfter: 120_000, Failed: session.SummaryFailedError, Full: true}},
		{Kind: session.EventTurnDone},
	}}, use: session.ContextUse{Tokens: 21_000, Window: 200_000, Threshold: 180_000}}
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), Model: "deepseek/deepseek-v4.1-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Context == nil || agent.Context.Window != 200_000 || agent.Context.Tokens != 21_000 {
		t.Fatalf("held agent context = %+v", agent.Context)
	}
	if _, err := backend.Send(agent.ID, "go", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)

	rec.mu.Lock()
	var passes []appwire.Entry
	for i, event := range rec.events {
		if added, ok := rec.data[i].(appwire.EntryAdded); ok && event == appwire.EventEntryAdded && added.Entry.Kind == appwire.EntrySummary {
			passes = append(passes, added.Entry)
		}
	}
	rec.mu.Unlock()
	if len(passes) != 4 {
		t.Fatalf("summary entries = %d, want summarizing, settled, mechanical, failed", len(passes))
	}
	live, settled, mechanical, failed := passes[0], passes[1], passes[2], passes[3]
	if live.ID != "cs1" || settled.ID != "cs1" || live.Compaction.State != appwire.CompactionSummarizing || settled.Compaction.State != appwire.CompactionSettled {
		t.Fatalf("the live entry is not replaced by its settling: %+v / %+v", live, settled)
	}
	if live.Text != "" {
		t.Fatalf("a summary still being written claims words: %q", live.Text)
	}
	kept := settled.Compaction.Kept
	if strings.Contains(kept, secret) || !strings.HasPrefix(kept, "Goal. Retry webhooks") {
		t.Fatalf("kept = %q; want the summary with its secret redacted", kept)
	}
	if settled.Compaction.TokensBefore != 182_000 || settled.Compaction.TokensAfter != 21_000 || settled.Compaction.Turns != 18 || settled.Compaction.Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("settled facts = %+v", settled.Compaction)
	}
	if mechanical.ID != "cs2" || mechanical.Text != "" || mechanical.Compaction.Summarized != 0 || mechanical.Compaction.Kept != "" {
		t.Fatalf("a stub-only pass claims a summary: %+v", mechanical)
	}
	if failed.ID != "cs3" || failed.Compaction.State != appwire.CompactionFailed || failed.Compaction.Failed != "error" || !failed.Compaction.Full {
		t.Fatalf("failure = %+v", failed.Compaction)
	}
}

// THE LISTING TELLS EVERY LINE ONCE AND EVERY PASS IN ITS PLACE, under the
// same ids the live pump used, with a pass whose region could not be placed
// left out rather than piled at the top.
func TestAppListingPlacesEveryPassBetweenTheLinesItFollows(t *testing.T) {
	record := session.Record{
		Earlier: []session.DisplayEntry{
			{Role: "user", Text: "q1"}, {Role: "assistant", Text: "a1"},
			{Role: "user", Text: "q2"}, {Role: "assistant", Text: "a2"},
		},
		Entries: []session.DisplayEntry{{Role: "user", Text: "[context compacted] note"}, {Role: "user", Text: "q2"}, {Role: "assistant", Text: "a2"}, {Role: "user", Text: "q3"}},
		Floor:   3,
		Passes: []session.CompactionFact{
			{Seq: 1, Entry: 2, Summarized: 2, Summary: "first", TokensBefore: 10, TokensAfter: 5},
			{Seq: 2, Entry: -1, Stubbed: 1},
			{Seq: 3, Entry: 4, Summarized: 4, Summary: "second"},
			{Seq: 4, Entry: 5, Failed: session.SummaryFailedDeclined},
		},
	}
	entries := appEntriesOf("a", appRecordItems(record), time.Now(), nil)
	var got []string
	for _, entry := range entries {
		if entry.Kind == appwire.EntrySummary {
			got = append(got, entry.ID+":"+entry.Compaction.State)
		} else {
			got = append(got, entry.ID+":"+entry.Text)
		}
	}
	want := "u0:q1 a0.0:a1 cs1:settled u1:q2 a1.0:a2 cs3:settled u2:q3 cs4:failed"
	if strings.Join(got, " ") != want {
		t.Fatalf("listing:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	for _, entry := range entries {
		if entry.ID == "cs3" && entry.Compaction.Kept != "second" {
			t.Fatalf("pass 3 kept = %q", entry.Compaction.Kept)
		}
	}
}

// A KEPT SUMMARY PAST THE WIRE'S LIMIT IS CUT ON A RUNE AND SAYS SO.
func TestAppKeptSummaryIsBoundedAndSaysWhenCut(t *testing.T) {
	long := strings.Repeat("é", appwire.KeptLimit)
	entry := appCompactionEntry("a", session.CompactionFact{Seq: 1, Summarized: 3, Summary: long}, appwire.CompactionSettled)
	kept := entry.Compaction.Kept
	if !entry.Compaction.KeptClipped || len(kept) > appwire.KeptLimit || !strings.HasSuffix(kept, "é") {
		t.Fatalf("clipped = %v, %d bytes, ends %q", entry.Compaction.KeptClipped, len(kept), kept[len(kept)-2:])
	}
}

// EVERY ASKED-FOR PASS ENDS TRUTHFULLY. Over a connection an error is only
// its words, so the two refusals are matched by them; anything else is an
// error and is never called "nothing to compact".
func TestAppCompactEndsTruthfully(t *testing.T) {
	cases := []struct {
		err     error
		outcome string
		message string
	}{
		{nil, appwire.CompactionCompacted, ""},
		{errors.New(session.ErrCompactionInFlight.Error()), appwire.CompactionBusy, ""},
		{&session.NothingToCompact{Why: "only 300 tokens before your latest message"}, appwire.CompactionNothing, "only 300 tokens before your latest message"},
		{errors.New((&session.NothingToCompact{Why: "nothing new since the last summary"}).Error()), appwire.CompactionNothing, "nothing new since the last summary"},
		{&session.SummarySkipped{Why: "the model declined"}, appwire.CompactionCompacted, ""},
		{errors.New("remote: the connection closed"), appwire.CompactionError, "remote: the connection closed"},
	}
	for _, c := range cases {
		ended := appCompactEnded("a", c.err)
		if ended.Outcome != c.outcome || (c.message != "" && ended.Message != c.message) {
			t.Errorf("%v → %+v, want %s %q", c.err, ended, c.outcome, c.message)
		}
	}
}

// compactConversation is a fake whose pass waits until it is let go.
type compactConversation struct {
	*fakeConversation
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (c *compactConversation) Compact(ctx context.Context) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ONE PASS PER AGENT, ADMITTED BEFORE ANYTHING STARTS, AND EVERY CALL ENDS.
// A second ask while the first runs is refused busy without a second call;
// the first ends on compaction.ended and frees the agent; and a pass lane the
// connection closes stops its reader instead of spinning on it.
func TestAppCompactAdmitsOnePassPerAgentAndAlwaysEnds(t *testing.T) {
	conv := &compactConversation{fakeConversation: &fakeConversation{}, release: make(chan struct{})}
	lane := make(chan session.Event)
	close(lane)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {},
			Passes: func() <-chan session.Event { return lane }}, nil
	})
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir(), Model: "deepseek/deepseek-v4.1-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.CompactAgent(agent.ID); err != nil {
		t.Fatalf("first compact: %v", err)
	}
	var refused *appwire.Error
	if err := backend.CompactAgent(agent.ID); !errors.As(err, &refused) || refused.Code != appwire.CodeBusy {
		t.Fatalf("second compact while the first runs = %v, want busy", err)
	}
	close(conv.release)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		ended, ok := data.(appwire.CompactionEnded)
		return event == appwire.EventCompactionEnded && ok && ended.Outcome == appwire.CompactionCompacted && ended.Agent == agent.ID
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := backend.CompactAgent(agent.ID)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent was never freed after its pass ended: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	ends := 0
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		if ended, ok := data.(appwire.CompactionEnded); ok && event == appwire.EventCompactionEnded && ended.Agent == agent.ID {
			ends++
		}
		return ends == 2
	})
	conv.mu.Lock()
	calls := conv.calls
	conv.mu.Unlock()
	if calls != 2 {
		t.Fatalf("engine passes = %d, want 2 (the refused ask made none)", calls)
	}
}

// A CLOSED PASS LANE ENDS ITS READER; it is never read as endless zero events.
func TestAppPassReaderEndsOnAClosedLane(t *testing.T) {
	lane := make(chan session.Event, 1)
	lane <- session.Event{Kind: session.EventCompacted, Compaction: &session.CompactionFact{Seq: 1}}
	close(lane)
	taken := 0
	done := make(chan struct{})
	go func() {
		appReadPasses(make(chan struct{}), lane, func(session.Event) { taken++ })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the reader kept reading a closed lane")
	}
	if taken != 1 {
		t.Fatalf("took %d passes, want the one sent before the close", taken)
	}
}
