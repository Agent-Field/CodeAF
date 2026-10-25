package main

// COMPACTION FOR THE APP (V78): the conversation's weight against its window
// on agent.context, each pass as a summary entry's facts, and agents.compact —
// the same pass /compact runs — answered at once with the pass saying itself
// as entries.
//
// Everything here is the engine's facts moved onto the wire. No figure is
// derived from a hint's words, and no word here is for a person: the app
// words every state.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appContextSource is a conversation that can say what it weighs against its
// window: a [remote.Agent] answers from the engine's last stated facts.
type appContextSource interface {
	ContextUse() session.ContextUse
}

// appContextOf is a conversation's context as the wire carries it, and nil
// when it cannot say.
func appContextOf(conv appConversation) *appwire.AgentContext {
	source, ok := conv.(appContextSource)
	if !ok {
		return nil
	}
	use := source.ContextUse()
	return &appwire.AgentContext{Tokens: use.Tokens, Window: use.Window, Threshold: use.Threshold, Summarizing: use.Summarizing}
}

// appSameContext says two readings would draw the same.
func appSameContext(a, b *appwire.AgentContext) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// noteContext reads the conversation's context once and says it when it
// moved. It is called at step and pass boundaries, never per token.
func (b *appBackend) noteContext(a *appAgent, live *appLive) {
	if live == nil || live.conn == nil {
		return
	}
	next := appContextOf(live.conn.Conv)
	var view appwire.Agent
	moved := false
	b.locked(func() {
		if a.live != live || appSameContext(a.view.Context, next) {
			return
		}
		a.view.Context = next
		view, moved = a.view, true
	})
	if moved {
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}

// appCompactionEntryID names a pass's entry by its sequence, the same live
// and read back.
func appCompactionEntryID(seq int) string { return "cs" + strconv.Itoa(seq) }

// appCompactionWords is the text an older app drew on a summary entry. It is
// set only where it is true — a pass that summarized — so a mechanical pass
// is never called a summary even by an app that reads no facts.
const appCompactionWords = "Earlier messages summarized"

// appCompactionEntry is one pass as a summary entry.
func appCompactionEntry(agent string, fact session.CompactionFact, state string) appwire.Entry {
	at := fact.At
	if at.IsZero() {
		at = time.Now()
	}
	facts := &appwire.Compaction{
		Seq: fact.Seq, State: state, TokensBefore: fact.TokensBefore, TokensAfter: fact.TokensAfter,
		Stubbed: fact.Stubbed, Folded: fact.Folded, Summarized: fact.Summarized,
		Model: fact.Model, Turns: fact.Turns, Failed: fact.Failed, Full: fact.Full,
	}
	if kept := redact.Secrets(fact.Summary); kept != "" {
		if len(kept) > appwire.KeptLimit {
			kept, facts.KeptClipped = appClipUTF8(kept, appwire.KeptLimit), true
		}
		facts.Kept = kept
	}
	if !fact.CoversFrom.IsZero() {
		from := fact.CoversFrom
		facts.CoversFrom = &from
	}
	if !fact.CoversUntil.IsZero() {
		until := fact.CoversUntil
		facts.CoversUntil = &until
	}
	// The entry stands where the pass began, so its settling replaces it in
	// place rather than at the moment it ended.
	if !fact.StartedAt.IsZero() {
		started := fact.StartedAt
		facts.StartedAt, at = &started, started
	}
	if state != appwire.CompactionSummarizing && !fact.At.IsZero() {
		ended := fact.At
		facts.EndedAt = &ended
	}
	entry := appwire.Entry{ID: appCompactionEntryID(fact.Seq), Agent: agent, Kind: appwire.EntrySummary, At: at, Compaction: facts}
	if state == appwire.CompactionSettled && fact.Summarized > 0 {
		entry.Text = appCompactionWords
	}
	return entry
}

// appClipUTF8 cuts text to at most limit bytes on a rune boundary.
func appClipUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut]
}

// appCompactionState is the state an ended pass is drawn in.
func appCompactionState(fact session.CompactionFact) string {
	if fact.Failed != "" {
		return appwire.CompactionFailed
	}
	return appwire.CompactionSettled
}

// appPassEvent is one pass event as the entry it makes, and false for any
// other event or a pass from an engine too old to say its facts.
func appPassEvent(agent string, event session.Event) (appwire.Entry, bool) {
	if event.Compaction == nil || event.Compaction.Seq <= 0 {
		return appwire.Entry{}, false
	}
	switch event.Kind {
	case session.EventSummaryStarted:
		return appCompactionEntry(agent, *event.Compaction, appwire.CompactionSummarizing), true
	case session.EventCompacted, session.EventSummaryFailed:
		return appCompactionEntry(agent, *event.Compaction, appCompactionState(*event.Compaction)), true
	}
	return appwire.Entry{}, false
}

// appCompactConv is a conversation that runs a pass when asked.
type appCompactConv interface {
	Compact(ctx context.Context) error
}

// appCompactJobs bounds the passes this process runs for the app at once,
// across every agent; each agent runs at most one ([appLive.compacting]).
const appCompactJobs = 4

// appCompactSlots is that bound.
var appCompactSlots = make(chan struct{}, appCompactJobs)

// watchPasses is the one reader of a connection's pass lane for as long as the
// conversation is held: every pass asked for outside a turn — by this app or
// by another window — becomes the entry it is. It answers the function that
// stops it; a lane the connection closes ends it too.
func (b *appBackend) watchPasses(a *appAgent, live *appLive) func() {
	if live.conn.Passes == nil {
		return func() {}
	}
	passes := live.conn.Passes()
	if passes == nil {
		return func() {}
	}
	stop := make(chan struct{})
	id := ""
	b.locked(func() { id = a.view.ID })
	guard.Go("app/passes", func() {
		appReadPasses(stop, passes, func(event session.Event) {
			if entry, said := appPassEvent(id, event); said {
				live.box.Event(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
			}
			b.noteContext(a, live)
		})
	})
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

// appReadPasses hands each pass to take until stop is closed or the lane is:
// a closed lane ends the reading rather than answering zero values forever.
func appReadPasses(stop <-chan struct{}, passes <-chan session.Event, take func(session.Event)) {
	for {
		select {
		case <-stop:
			return
		case event, ok := <-passes:
			if !ok {
				return
			}
			take(event)
		}
	}
}

// CompactAgent starts the pass /compact runs and answers at once. It is
// admitted BEFORE anything starts: one pass per agent and [appCompactJobs]
// in the process, refused busy otherwise. One worker owns the call and its
// answer, releases both admissions on every road out, and ends the call on
// compaction.ended; the pass's own events reach the page through the
// connection's pass lane ([appBackend.watchPasses]), ahead of that answer.
func (b *appBackend) CompactAgent(id string) error {
	a, live, endLive, err := b.borrowLive(id)
	if err != nil {
		return err
	}
	defer endLive()
	conv, ok := live.conn.Conv.(appCompactConv)
	if !ok {
		return appwire.Errorf(appwire.CodeUnknownMethod, "this conversation cannot be compacted from here")
	}
	admitted := false
	b.locked(func() {
		if !live.compacting && !live.closed {
			live.compacting, admitted = true, true
		}
	})
	if !admitted {
		return appwire.Errorf(appwire.CodeBusy, "a compaction of this conversation is already running")
	}
	select {
	case appCompactSlots <- struct{}{}:
	default:
		b.locked(func() { live.compacting = false })
		return appwire.Errorf(appwire.CodeBusy, "too many compactions are running; ask again in a moment")
	}
	a.gate.Lock()
	endWorker := b.pinReader(a)
	a.gate.Unlock()
	guard.Go("app/compact", func() {
		defer endWorker()
		defer func() {
			<-appCompactSlots
			b.locked(func() { live.compacting = false })
		}()
		ctx, cancel := context.WithTimeout(context.Background(), session.CompactPatience)
		result := conv.Compact(ctx)
		cancel()
		b.noteContext(a, live)
		live.box.Event(appwire.EventCompactionEnded, appCompactEnded(id, result))
	})
	return nil
}

// appCompactEnded is how one asked-for pass ended. Over a connection an
// engine's error arrives as its words alone, so the two refusals are matched
// by the words their own Error methods say; anything else is an error and is
// never called "nothing to compact".
func appCompactEnded(id string, err error) appwire.CompactionEnded {
	ended := appwire.CompactionEnded{Agent: id, Outcome: appwire.CompactionCompacted}
	if err == nil {
		return ended
	}
	words := err.Error()
	var nothing *session.NothingToCompact
	switch {
	case errors.Is(err, session.ErrCompactionInFlight) || words == session.ErrCompactionInFlight.Error():
		ended.Outcome = appwire.CompactionBusy
	case errors.As(err, &nothing):
		ended.Outcome, words = appwire.CompactionNothing, nothing.Why
	case errors.Is(err, session.ErrNothingToCompact) || strings.HasPrefix(words, session.ErrNothingToCompact.Error()):
		ended.Outcome = appwire.CompactionNothing
		words = strings.TrimPrefix(strings.TrimPrefix(words, session.ErrNothingToCompact.Error()), ": ")
	default:
		// A pass that changed the conversation but whose summary was skipped
		// still compacted; its entry says the summary did not land.
		if _, skipped := session.SummarySkippedWhy(err); skipped {
			return ended
		}
		ended.Outcome = appwire.CompactionError
	}
	ended.Message = redact.Secrets(strings.TrimSpace(words))
	return ended
}
