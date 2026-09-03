package tui3

import (
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE PHASE CLOCK: WHAT IS HAPPENING, AND WHAT HAPPENS NEXT ───────────────
//
// The layer that holds the wire knows which of ten slownesses a request is in
// — a handshake, a queue before the first word, a run of thought, an answer
// arriving, a rate-limit wait, a relaxed re-ask, a rescue in flight, a tool
// running, a compaction, a turn being written down for whoever takes it over —
// and it says so (internal/provider's phase.go, forwarded by
// internal/session's phasenews.go). This file is the only place
// that DRAWS one. Before it the surface could see a single bit of any of that,
// whether a delta had arrived in the last ten seconds, so a stalled thinking
// pass read "still working" while the row beside it quoted the LAST answer's
// lane and throughput as though they were happening now.
//
// A DEADLINE IS NEVER INVENTED. The countdown a person reads — "→ parasail at
// 4.4s" — is the watch's own hedge deadline or the pacing wait the router asked
// for: a real moment at which this build really acts. Where no alternative lane
// exists, or the guard is off, or routing is off, the clock still says the phase
// and still counts up, and simply names no consequence. A countdown that expires
// and does nothing is the surface lying about the machinery, and the emptiness
// law already says what to draw in place of a figure nobody measured, which is
// nothing.
//
// ONLY A ROLE A PERSON IS READING OWNS THE CLOCK, and that is a law rather than
// a preference (internal/lane's roles.go). A talk turn makes several calls that
// are nobody's business but the machine's — a title, a memory reflex, a route
// question, a reply check — and each of them posts its own phases. A desk that
// kept whichever answered last would put a naming errand's "writing · 61 t/s"
// under an answer somebody was still waiting for, which is the other half of the
// defect this file exists for. So a phase from a hidden role is dropped HERE, at
// the door, and never reaches a drawing site to be filtered by whoever
// remembered to.

// PhaseNews is one moment of one turn's life. It is [session.PhaseNews] under
// this package's own name, and that type is in turn internal/provider's — ONE
// VOCABULARY the whole way down, because two packages with two spellings of
// "thinking" is two surfaces that disagree the first time one is corrected.
type PhaseNews = session.PhaseNews

// phaseWindow is how long a phase still describes the present.
//
// A POSTING LAYER THAT DIED MUST NOT LEAVE A CLOCK RUNNING, and a layer that is
// STILL ALIVE must not be dropped. Both halves are one bargain, so the number is
// the posting side's own ([provider.PhaseWindow]) rather than a second figure
// kept here: a window this file could tune on its own is a stage that goes dark
// while it is still running, which is exactly what a quarter-hour route judge
// did on the screen before the beats were derived from it.
//
// EVERY LIVE PHASE SAYS ITSELF AGAIN WHILE IT LASTS — a request off its own
// stream, once a second (internal/provider's phaseBeat), and a turn's own stage
// off a timer, every third of this window (internal/session's phaseHeldBeat) —
// so fifteen seconds is a wide margin on either, and past it the segment draws
// nothing and the older readings underneath take over again.
const phaseWindow = provider.PhaseWindow

// phaseNewsMsg wakes the loop so a frame is drawn for a phase that changed
// somewhere other than a keystroke. It carries nothing — [PostPhaseNews] has
// already put the news on the desk — because a message that carried the news
// would be a second copy of it, arriving after the first. It is the lane news's
// own shape (lanes.go's [laneNewsMsg]) said again for the other seam.
type phaseNewsMsg struct{}

// phaseDesk is the latest phase per model, and nothing else.
//
// It is one entry per model rather than a history because a phase is a claim
// about NOW: the one before it is not a smaller truth, it is a former one, and
// keeping it would only give a drawing site something wrong to fall back on.
type phaseDesk struct {
	mu     sync.RWMutex
	latest map[string]PhaseNews
	// waits is when the WAIT a model is currently inside began — the moment a
	// person asked for something and nothing of it has come back since. It is
	// kept beside the phase rather than inside it because a phase is one stage
	// of that wait and the wait outlives every one of them ([phaseWaiting]).
	waits map[string]time.Time
}

var phases = phaseDesk{latest: map[string]PhaseNews{}, waits: map[string]time.Time{}}

// phaseWaiting reports whether a phase is part of ONE WAIT: the stretch that
// begins when a person asks for something and ends when the first of it comes
// back. A handshake, the queue before the first word, a pacing wait, a retry,
// a rescue, a fallback model and the two sentences the surface adds to a stall
// are all the same wait wearing different words.
//
// Everything else — thinking, writing, a tool running, a check, a compaction —
// is WORK IN PROGRESS, and its clock is honestly its own: a person reading
// "running go test · 41s" is asking how long that test has been going, not how
// long the turn has.
func phaseWaiting(phase provider.Phase) bool {
	switch phase {
	case provider.PhaseConnecting, provider.PhaseFirstWord, provider.PhasePaced,
		provider.PhaseRetrying, provider.PhaseSwitching, provider.PhaseSwitchingModel,
		session.PhaseAsking, session.PhaseAllSlow:
		return true
	}
	return false
}

// PostPhaseNews is how the layer that holds the turn tells the surface what it
// is doing. It is safe from any goroutine and it never blocks on a draw: it is
// called from the stream's read loop, between two deltas, against the
// connection's own idle watchdog.
//
// THREE THINGS ARE DECIDED HERE AND NOWHERE ELSE. A phase for a model nobody
// named belongs to nobody and is dropped. A phase from a role a person is not
// reading is dropped, per the law in the header. And an EMPTY phase is the end
// of the story rather than a phase called "": the turn posts one when it stops,
// and it clears that model's entry so the surface stops drawing a clock for work
// that is over.
func PostPhaseNews(news PhaseNews) {
	news.Model = strings.TrimSpace(news.Model)
	news.Lane = strings.TrimSpace(news.Lane)
	news.Detail = strings.TrimSpace(news.Detail)
	news.Then = strings.TrimSpace(news.Then)
	if news.Model == "" || !news.Role.Visible() {
		return
	}
	if news.At.IsZero() {
		news.At = time.Now()
	}
	if news.Since.IsZero() {
		news.Since = news.At
	}
	phases.mu.Lock()
	defer phases.mu.Unlock()
	if news.Phase == "" {
		delete(phases.latest, news.Model)
		delete(phases.waits, news.Model)
		return
	}
	// ONE INSTANT FOR THE WHOLE WAIT, AND THE CLOCK NEVER RUNS BACKWARDS.
	//
	// THE DEFECT THIS FIXES, measured: a turn nineteen seconds old read `all
	// lanes slow · still waiting · 10s`, and ten seconds after that it read
	// `via openinference · first word 4.5s`. Every posting layer is honest —
	// each phase carries when THAT phase began, and a retry builds a whole new
	// clock for its attempt — but a person is not reading a stage, they are
	// reading how long they have been waiting, and a figure that halves while
	// they watch it is read as the program having lost track of itself. It sits
	// next to an elapsed clock that is still climbing, which is what makes it
	// unmissable.
	//
	// So the desk carries the wait's own start across every phase of it and
	// hands it to the drawing sites, which go on counting up from the moment
	// the request left. It is done HERE, at the one door every phase comes
	// through, rather than at the two places that draw one — two answers to
	// "when did this wait begin" is how it came to have two.
	//
	// A wait ends the moment something that is not a wait is posted, and a
	// poster that knows an EARLIER start than the desk does wins: internal
	// provider's allSlow deliberately keeps the wait's own instant, and this
	// must never round that forward.
	switch began := phases.waits[news.Model]; {
	case !phaseWaiting(news.Phase):
		delete(phases.waits, news.Model)
	case began.IsZero() || news.Since.Before(began):
		phases.waits[news.Model] = news.Since
	default:
		news.Since = began
	}
	phases.latest[news.Model] = news
}

// phaseNewsFor is the latest phase of one model, false when none is running. It
// does not judge freshness — [app.livePhase] does, because staleness is a
// question about the moment a frame is painted and not about the moment the news
// arrived.
func phaseNewsFor(model string) (PhaseNews, bool) {
	phases.mu.RLock()
	defer phases.mu.RUnlock()
	news, ok := phases.latest[strings.TrimSpace(model)]
	return news, ok
}

// forgetPhases empties the desk. It is for tests, which must not inherit
// another test's turn.
func forgetPhases() {
	phases.mu.Lock()
	defer phases.mu.Unlock()
	phases.latest = map[string]PhaseNews{}
	phases.waits = map[string]time.Time{}
}

// livePhase is the phase this conversation's model is in right now, false when
// there is none or when the one on the desk has gone stale ([phaseWindow]).
func (a *app) livePhase() (PhaseNews, bool) {
	news, ok := phaseNewsFor(a.model)
	if !ok {
		return PhaseNews{}, false
	}
	// AND ONLY THIS WINDOW'S OWN WORK IS DRAWN ON THIS WINDOW'S ROW. The desk is
	// keyed by MODEL, which is the right key for "what is that model doing" and
	// the wrong one for "what is this conversation doing": a task node running
	// on the same model id posts phases of its own, and they are somebody
	// else's errand however visible their role is. The row a person reads here
	// is the conversation's, so the conversation's role is the one it takes. A
	// node's own phases are drawn where a node is drawn — its room — which
	// reaches this row through [app.roomChip] and never through here.
	if news.Role != lane.RoleTalk {
		return PhaseNews{}, false
	}
	if a.now().Sub(news.At) > phaseWindow {
		return PhaseNews{}, false
	}
	return news, true
}

// ── THE WORDS ───────────────────────────────────────────────────────────────

// THE FITTING IS ROWFIT.GO'S AND NOT THIS FILE'S. This file decides what the
// facts ARE and how they rank; `internal/tui3/rowfit.go` decides how many of
// them a row has room for, in the one field-priority fitter this surface has
// ([rowLed] for a segment led by a name, [rowTail] for one that is only facts).
// A second width ladder here would be a second answer to a question with one.

// phaseWords is the phase clock as a person reads it at a width nobody is
// short of, WITHOUT the leading separator — a caller adds the " · " or the
// space its own line wants:
//
//	connecting · 1.2s
//	first word · 3.1s → parasail at 4.4s
//	via coreweave · first word 3.1s → parasail at 4.4s
//	thinking · 12s · friendli 38 t/s
//	writing · 4s · friendli 61 t/s
//	coreweave is slow · switch to auto? (y)
//	all lanes slow · still waiting
//	paced · retry in 6s
//	trying again · 2 of 6
//	stalled 9s · switching to parasail
//	running go test · 41s
//	checking · 3s
//	taking stock · 14s
//	tidying · 6s
//
// EVERY PART IS DROPPED WHEN IT IS NOT KNOWN, which is the emptiness law read
// segment by segment: no lane, no lane; no rate, no rate; no real deadline, no
// arrow. What is left is still true.
func phaseWords(news PhaseNews, now time.Time) string {
	return rowLed(phaseFields(news, now), rowUnbounded)
}

// phaseFields is every phase this surface has learned to say, as the ranked
// facts a width can be applied to (rowfit.go's [rowField]). It is the vocabulary
// in one place: a phase's parts, which of them leads, and what each of them is
// worth when the row runs out.
//
// THE ORDER IS THE DATA HIERARCHY the owner asked for — the machine's name, then
// the phase and its clock and whatever the build will do about it, then the
// rate. A field is never cut, only said shorter: "3.1s" is "first word 3.1s"
// with the label taken off, where a clip at the same width would leave
// "first word 3.…", which is a fact about nothing. The fitting itself belongs to
// rowfit.go and is not repeated here.
//
// TWO SPELLINGS OF A CLOCK, and the difference is what the number is for. The
// phases a person is WAITING THROUGH with nothing arriving — the handshake and
// the queue before the first word, and the deadline hung off them — are read in
// tenths ([tookWord]), because the difference between 1.2s and 3.1s is the whole
// of what those seconds tell you. Every other phase is work in progress and is
// read in whole seconds ([countUpWord], the spelling every other live clock on
// this surface uses), because a tenth on a `go test` is a digit that changes
// under the eye and means nothing.
func phaseFields(news PhaseNews, now time.Time) []rowField {
	since := now.Sub(news.Since)
	if since < 0 {
		since = 0
	}
	word := string(news.Phase)
	switch news.Phase {
	case provider.PhaseConnecting:
		return phaseWaitFields(news, tookWord(since), false)
	case provider.PhaseFirstWord:
		return phaseWaitFields(news, tookWord(since), true)
	case provider.PhaseThinking, provider.PhaseWriting:
		return []rowField{rowSay(word), rowSay(countUpWord(since)), phaseServing(news)}
	case provider.PhasePaced:
		// THE PACING WAIT IS THE ROUTER'S OWN `Retry-After` and is therefore a
		// real moment, so it is spelled as the countdown it is. Without one the
		// only true thing left is how long the wait has run.
		clock := countUpWord(since)
		if !news.Deadline.IsZero() {
			if countdown := countUpWord(news.Deadline.Sub(now)); countdown != "" {
				clock = "retry in " + countdown
			}
		}
		return []rowField{rowSay(word), rowSay(clock)}
	case provider.PhaseRetrying:
		// The rung of the ladder is better than the clock when the ladder said
		// which rung it is on: "2 of 6" answers "is this going anywhere?" and a
		// count-up does not.
		rung := news.Detail
		if rung == "" {
			rung = countUpWord(since)
		}
		return []rowField{rowSay(word), rowSay(rung)}
	case provider.PhaseSwitching:
		// THE STALL COMES FIRST BECAUSE IT IS THE REASON. A rescue reads as an
		// answer to something, and the something is how long the first machine
		// had gone quiet; the switch alone is the same sentence with the cause
		// taken out of it. So the cause is what a narrow row spends, and the
		// rescue itself — the one part a person would act on — is what is kept.
		if news.Then != "" {
			word += " to " + strings.ToLower(news.Then)
		}
		if news.Detail == "" {
			return []rowField{rowSay(word)}
		}
		return []rowField{rowSay(news.Detail+" · "+word, word)}
	case provider.PhaseSwitchingModel:
		// AND THE LAST RUNG SAYS SO BY NAME. Changing which machine writes an
		// answer is bookkeeping and reads as "switching"; changing which MODEL
		// writes it is a different answer to the question that was asked, and a
		// person who chose one model and is being answered by another is owed
		// that sentence while it happens rather than in the transcript
		// afterwards (the ladder, in docs/ARCHITECTURE.md). The name is the
		// model's own base, spelled as the model segment beside it spells it.
		if news.Then == "" {
			return []rowField{rowSay(word)}
		}
		return []rowField{rowSay(word+" → "+modelBase(news.Then), word)}
	case provider.PhaseRunning, provider.PhaseBriefing:
		// THE NOUN IS THE SUBSTANCE AND THE VERB IS THE FRAME, so a narrow row
		// keeps "running" or "briefing" and lets the noun go before the clock
		// does. The two phases share this arm because they share the shape: a
		// call that is running is named by the tool it is running, and the
		// harness writing a handover is named by who the writing is FOR —
		// "briefing" alone is it naming its own paperwork, and "briefing a
		// worker" is the sentence that tells somebody watching their turn stop
		// what is about to happen to it.
		return []rowField{rowSay(phaseJoinWord(word, news.Detail), word), rowSay(countUpWord(since))}
	case session.PhaseAsking:
		// A WAIT A PERSON CAN END, and the only sentence on this surface that
		// asks for a keystroke while a turn is running.
		//
		// THE MACHINE'S NAME LEADS because it is the fact a person acts on:
		// which of their pinned machines has gone quiet is what tells them
		// whether to answer at all, and the question is the label on it. A narrow
		// row therefore keeps "coreweave is slow" and shortens the question, and
		// the key never goes: an offer whose key was cut is a question nobody
		// can answer.
		question := "switch to " + strings.ToLower(news.Then) + "? (y)"
		if news.Then == "" {
			question = "switch? (y)"
		}
		if news.Detail == "" {
			return []rowField{rowSay(question, "(y)")}
		}
		return []rowField{rowSay(news.Detail), rowSay(question, "switch? (y)", "(y)")}
	case session.PhaseAllSlow:
		// EVERY REACHABLE LANE IS BELIEVED SLOW, so there is nowhere better to
		// go and acting would buy nothing. Saying so is the act: this row is the
		// visible half of the controller's report, and the alternative — which is
		// what this surface did before — is a person watching a line that says
		// nothing while a real wait runs.
		return []rowField{rowSay("all lanes slow"), rowSay("still waiting"), rowSay(countUpWord(since))}
	case provider.PhaseChecking, provider.PhaseTidying, provider.PhaseTakingStock:
		// THREE WORDS WITH ONE SHAPE: a stage named by nothing but itself, and
		// the clock a person is reading it against. `taking stock` shares the arm
		// because it shares that shape, not because it means the same thing —
		// what it means is on the manual's own page and in the phase's doc
		// comment, and a third arm with an identical body is the second one
		// drifting the first time somebody improves either.
		return []rowField{rowSay(word), rowSay(countUpWord(since))}
	}
	// A PHASE THIS SURFACE HAS NEVER HEARD OF DRAWS NOTHING, rather than its own
	// machine word with a clock after it. The vocabulary is closed and spelled
	// in one place; a name that is not in it is a seam that has grown a word
	// this file has not learned to say, and the older readings underneath are a
	// better answer than a stranger's noun.
	return nil
}

// phaseWaitFields is a phase a person is waiting through: the handshake and the
// queue before the first word.
//
// THE MACHINE ANSWERING LEADS THE SEGMENT WHEN ONE HAS NAMED ITSELF, and that
// is the data hierarchy: which machine a person is waiting on is the fact they
// would act on — the one that tells them whether this wait is normal — and the
// name of the phase is the label on the clock beside it. So "via coreweave"
// takes the head and "first word" demotes to the clock's own label, where a
// narrow row can drop it and leave a figure that is still true. Where no lane
// has said who it is, the phase's word leads instead and the segment is exactly
// what it has always been.
//
// THE CONSEQUENCE RIDES THE CLOCK IT BELONGS TO rather than standing beside it,
// because "3.1s → parasail at 4.4s" is one clock read twice and a separator
// between them would claim they were two facts. That is what gives the clock
// three spellings — with the label, without it, and without the consequence —
// and those three are exactly the rungs the segment climbs down.
//
// Only the queue before the first word carries one. The handshake has no
// alternative armed behind it, and a countdown to nothing is the one thing this
// file exists to refuse.
func phaseWaitFields(news PhaseNews, clock string, consequence bool) []rowField {
	full, short := clock, clock
	if consequence {
		if long, brief := phaseConsequence(news); long != "" {
			full, short = clock+" "+long, clock+" "+brief
		}
	}
	if lane := strings.ToLower(news.Lane); lane != "" {
		return []rowField{
			rowSay("via "+lane, lane),
			rowSay(phaseJoinWord(string(news.Phase), full), short, clock),
		}
	}
	return []rowField{rowSay(string(news.Phase)), rowSay(full, short, clock)}
}

// phaseJoinWord is the phase's word with its own noun after it — "running go
// test" — and the word alone when the noun is unknown.
func phaseJoinWord(word, detail string) string {
	if detail == "" {
		return word
	}
	return word + " " + detail
}

// phaseServing is who is answering and how fast: `friendli 38 t/s` whole, and
// `friendli` when the row has no room for the measurement. Either half alone is
// a whole truth and draws on its own; neither draws a placeholder for the
// other.
//
// A RATE WITH NO MACHINE BEHIND IT HAS NO SHORT SPELLING, so it is the first
// thing a narrow row spends: "61 t/s" attributed to nobody is the least of the
// four things this segment can say.
//
// The lane is lower-cased for the reason [app.laneRider] lower-cases it: a
// vendor's own capitalisation of its own name is a decision about their brand
// and this row is a decision about a person's eye.
func phaseServing(news PhaseNews) rowField {
	served := strings.ToLower(news.Lane)
	rate := laneRateWord(news.Rate)
	switch {
	case served != "" && rate != "":
		return rowSay(served+" "+rate, served)
	case served != "":
		return rowSay(served)
	default:
		return rowSay(rate)
	}
}

// phaseConsequence is the arrow: what will be done about this wait, and when,
// in both of its spellings — `→ parasail at 4.4s` and `→ parasail 4.4s`. The
// short one drops the preposition and keeps every fact.
//
// IT IS DRAWN ONLY WHEN BOTH HALVES ARE REAL — a moment the build will act at,
// and something it will do — because that is the header's law said at the one
// place that could break it. A deadline with nothing behind it is a countdown
// to nothing; a name with no deadline is a promise with no time on it. Neither
// is worth a cell, at ANY width: a narrow row drops the consequence whole
// rather than keeping the countdown and losing what it counts towards.
//
// The moment is spelled as SECONDS SINCE THE PHASE BEGAN rather than as a
// countdown, so it sits on the same ruler as the figure right before it:
// "first word · 3.1s → parasail at 4.4s" is one clock read twice, and a person
// can see the gap without doing arithmetic.
func phaseConsequence(news PhaseNews) (full, short string) {
	if news.Deadline.IsZero() || news.Then == "" {
		return "", ""
	}
	word := tookWord(news.Deadline.Sub(news.Since))
	if word == "" {
		return "", ""
	}
	then := strings.ToLower(news.Then)
	return "→ " + then + " at " + word, "→ " + then + " " + word
}
