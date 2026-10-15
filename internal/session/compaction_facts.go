package session

// A COMPACTION PASS, SAID AS FACTS.
//
// A surface used to learn about a pass from two events and a sentence: the
// Hint on [EventCompacted] ("compacted · stubbed 14 tool results · …"). The
// sentence is for a person, and a surface that wanted the numbers out of it
// would break the first time the wording improved. So a pass now carries what
// it did as fields ([CompactionFact]), the journal keeps the same fields on its
// marker, and a resumed conversation hands back every pass it ever made in
// order ([Record.Passes]) read from the journal, never from memory.
//
// NOTHING HERE IS INFERRED FROM THE MODEL'S PROSE. The summary text a fact
// carries is the summary the pass spliced in, taken at the splice; the counts
// are the pass's own; the window and the threshold are the figures the trigger
// is checked against.

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// CompactionFact is one compaction pass, or one summary that did not land.
type CompactionFact struct {
	// Seq numbers the passes of one conversation from 1, in the order they
	// were made, failures included. It is the same live and after a resume,
	// so a surface can name a pass by it.
	Seq int `json:"seq"`
	// At is when the pass ended (or, on [EventSummaryStarted], began), and
	// StartedAt when it began. Arrived is how many messages joined the
	// conversation while its summary was being written: they came AFTER the
	// pass began, so the pass is placed before them ([CompactionFact.Entry]),
	// live and after a resume alike.
	At        time.Time `json:"at"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Arrived   int       `json:"arrived,omitempty"`
	// Entry is where the pass sits in the conversation told once and whole
	// ([Record]): how many display entries of it come before the pass. Set
	// only on [Record.Passes]; zero on an event, which a surface places at the
	// end of what it has drawn.
	Entry int `json:"entry,omitempty"`
	// TokensBefore and TokensAfter are what the conversation weighed going in
	// and coming out, in the meter's own figure ([Agent.ContextTokens]).
	// TokensAfter is zero when it is not known: a summary still being
	// written, or a marker journaled before the field existed.
	TokensBefore int `json:"tokensBefore,omitempty"`
	TokensAfter  int `json:"tokensAfter,omitempty"`
	// Stubbed, Folded and Summarized are what the pass's three rungs did:
	// tool results turned into pointers, assistant messages folded into one
	// line, and messages a summary note replaced. All three zero is a summary
	// that failed in a pass that changed nothing.
	Stubbed    int `json:"stubbed,omitempty"`
	Folded     int `json:"folded,omitempty"`
	Summarized int `json:"summarized,omitempty"`
	// Summary is the note the model wrote, exactly as it was spliced into the
	// conversation, without the note's framing. Empty for a pass that wrote
	// none. Model is the model that wrote it.
	Summary string `json:"summary,omitempty"`
	Model   string `json:"model,omitempty"`
	// Turns is how many of the person's messages the summary now stands for,
	// an earlier summary's own count included (a later summary folds the
	// earlier one in). CoversFrom and CoversUntil are when the first and the
	// last line it replaced were written; zero when the journal does not say.
	Turns       int       `json:"turns,omitempty"`
	CoversFrom  time.Time `json:"coversFrom,omitempty"`
	CoversUntil time.Time `json:"coversUntil,omitempty"`
	// Failed says a summary was asked for and did not land, as one of the
	// SummaryFailed* words; empty when no summary was attempted or it landed.
	// Full says the conversation was still at or over the window after the
	// pass, which is the only case a surface may call the context full.
	Failed string `json:"failed,omitempty"`
	Full   bool   `json:"full,omitempty"`
}

// The reasons a summary did not land, as [CompactionFact.Failed] spells them.
const (
	SummaryFailedInterrupted = "interrupted"
	SummaryFailedDeclined    = "declined"
	SummaryFailedEmpty       = "empty"
	SummaryFailedChanged     = "changed"
	SummaryFailedNotShorter  = "not_shorter"
	SummaryFailedError       = "error"
)

// Mechanical says the pass only stubbed or folded: no model wrote anything.
func (f CompactionFact) Mechanical() bool { return f.Summarized == 0 && f.Failed == "" }

// summaryFailedWord is the reason a summary did not land, as a word.
func summaryFailedWord(ctx context.Context, err error, spliceWhy string) string {
	switch {
	case err == nil && spliceWhy == summaryChangedWhy:
		return SummaryFailedChanged
	case err == nil:
		return SummaryFailedNotShorter
	case ctx.Err() != nil:
		return SummaryFailedInterrupted
	case errors.Is(err, errSummaryDeclined):
		return SummaryFailedDeclined
	case errors.Is(err, errEmptyAnswer):
		return SummaryFailedEmpty
	default:
		return SummaryFailedError
	}
}

// ContextUse is what the conversation weighs against the window it runs in,
// read under one lock so the three figures belong together.
type ContextUse struct {
	// Tokens is [Agent.ContextTokens].
	Tokens int `json:"tokens"`
	// Window is the model's context window and Threshold the weight at which
	// the loop compacts on its own. Both are zero when no catalog, no
	// configuration and no endpoint has said what the window is: the
	// package's conservative default is a guess and is never reported.
	Window    int `json:"window,omitempty"`
	Threshold int `json:"threshold,omitempty"`
	// Summarizing says a summary request is in flight right now.
	Summarizing bool `json:"summarizing,omitempty"`
}

// ContextUse answers what the conversation weighs, the window and the
// threshold. It walks the live transcript's sizes once, under the session
// lock, which is never held across a model call; call it at a step boundary,
// never per streamed token.
func (a *Agent) ContextUse() ContextUse {
	window, threshold := 0, 0
	if a.contextWindow.Load() > 0 || a.config.ContextWindow > 0 {
		window = a.trustedWindow()
		threshold = compactThresholdOf(window)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return ContextUse{Tokens: a.meterTokensLocked(), Window: window, Threshold: threshold, Summarizing: a.summarizing}
}

// CompactObserved is [Agent.Compact] with the pass's own events handed to
// observe as they happen — [EventSummaryStarted], then [EventCompacting] and
// [EventCompacted] or [EventSummaryFailed] — for a surface that asked for the
// pass outside any turn and has no turn stream to hear it on. observe is
// called on this goroutine and must not block.
//
// A PASS ASKED FOR OUTSIDE A TURN IS STILL STOPPED BY STOP. It has no turn
// context for [Agent.Interrupt] to cut, so its own cut is held on the agent
// for the length of the pass ([Agent.manualPass]) and Interrupt, StopWork and
// Close cut it too: the model request ends, not only what a surface draws.
func (a *Agent) CompactObserved(ctx context.Context, observe func(Event)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cut := &manualPassCut{cancel: cancel}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return errAgentClosed
	}
	// Reserve before releasing the lock: another manual caller must not
	// enter the gap before compactWithPolicyResult sets compacting, or its
	// request could run while Stop still holds this caller's cancellation.
	if a.manualPass != nil {
		a.mu.Unlock()
		return ErrCompactionInFlight
	}
	a.manualPass = cut
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.manualPass == cut {
			a.manualPass = nil
		}
		a.mu.Unlock()
	}()
	var hub *eventHub
	if observe != nil {
		hub = &eventHub{observe: observe}
	}
	_, skipped, err := a.compactWithPolicyResult(ctx, hub, a.requestedCompactPolicy())
	if err == nil && skipped != "" {
		return &SummarySkipped{Why: skipped}
	}
	return err
}

// manualPassCut is the cut of one pass a surface asked for outside a turn.
type manualPassCut struct{ cancel context.CancelFunc }

// cutManualPassLocked takes the running manual pass's cut, to be called with
// the lock released.
func (a *Agent) cutManualPassLocked() func() {
	if a.manualPass == nil {
		return func() {}
	}
	return a.manualPass.cancel
}

// summaryProvenanceLocked is who wrote a planned summary and what it stands
// for: the person's messages in its region, plus an earlier summary's own.
func (a *Agent) summaryProvenanceLocked(plan summaryPlan) (turns int, from, until time.Time) {
	for index := 1; index < plan.end && index < len(a.messages); index++ {
		if a.messages[index].Role == "user" && !a.sessionNoteLocked(a.messages[index]) {
			turns++
		}
	}
	if plan.previous != "" {
		turns += a.keptTurns
		from = a.keptFrom
	}
	if len(plan.region) > 0 && a.file != nil {
		if from.IsZero() {
			from = a.file.written(plan.region[0])
		}
		until = a.file.written(plan.region[len(plan.region)-1])
	}
	return turns, from, until
}

// contextFullLocked says the conversation does not fit its window with the
// provider's safety allowance taken off: the next request would be refused.
func (a *Agent) contextFullLocked(tokens int) bool {
	window := a.trustedWindow()
	if window <= 0 || (a.contextWindow.Load() <= 0 && a.config.ContextWindow <= 0) {
		return false
	}
	return tokens >= window-provider.ContextSafetyTokens(window)
}

// recordPassLocked numbers a pass and remembers the little a LATER pass needs
// of it — the sequence and the newest summary's provenance — and nothing
// else. THE LIVE AGENT KEEPS NO LIST OF PASSES AND NO SUMMARY BODIES: the
// journal holds every pass, and a reader that wants them all reads the file
// ([ReadTranscript], [Record.Passes]). Memory held for a conversation stays
// what compaction made it, however many passes it has had.
func (a *Agent) recordPassLocked(fact CompactionFact) CompactionFact {
	a.passSeq++
	fact.Seq = a.passSeq
	if fact.At.IsZero() {
		fact.At = passInstant()
	}
	if fact.Summarized > 0 {
		a.keptTurns, a.keptFrom = fact.Turns, fact.CoversFrom
	}
	return fact
}

// restorePasses takes a replayed journal's passes the same way: their count
// and the newest summary's provenance.
func (a *Agent) restorePasses(passes []CompactionFact) {
	a.passSeq = len(passes)
	for index := len(passes) - 1; index >= 0; index-- {
		if passes[index].Summarized > 0 {
			a.keptTurns, a.keptFrom = passes[index].Turns, passes[index].CoversFrom
			return
		}
	}
}

// passInstant is now as the journal will read it back: UTC, no monotonic
// reading, so a pass is the same fact live and after a resume.
func passInstant() time.Time { return time.Now().UTC().Round(0) }

// passFact is a pass that edited the transcript, as the fact it is kept as.
func passFact(pass compactionPass, tokensBefore int) CompactionFact {
	return CompactionFact{
		At: pass.at, StartedAt: pass.startedAt, Arrived: pass.arrived, TokensBefore: tokensBefore, TokensAfter: pass.tokensAfter,
		Stubbed: pass.stubbed, Folded: pass.folded, Summarized: pass.summarized,
		Summary: pass.summary, Model: pass.summaryModel, Turns: pass.turns,
		CoversFrom: pass.coversFrom, CoversUntil: pass.coversUntil,
		Failed: pass.failed, Full: pass.full,
	}
}
