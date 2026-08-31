package tui3

import (
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE PHASE CLOCK: WHAT IS HAPPENING, AND WHAT HAPPENS NEXT ───────────────
//
// The layer that holds the wire knows which of nine slownesses a request is in
// — a handshake, a queue before the first word, a run of thought, an answer
// arriving, a rate-limit wait, a relaxed re-ask, a rescue in flight, a tool
// running, a compaction — and it says so (internal/provider's phase.go,
// forwarded by internal/session's phasenews.go). This file is the only place
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
// A POSTING LAYER THAT DIED MUST NOT LEAVE A CLOCK RUNNING. Every live phase
// says itself again about once a second while it lasts (internal/provider's
// phaseBeat), so fifteen seconds is a very wide margin on a heartbeat that
// quick — wide enough that a busy frame or a slow machine never blinks the
// segment, and short enough that a request whose goroutine was killed without
// posting its end takes the clock off the screen while a person is still
// looking at it. Past it the segment draws nothing and the older readings
// underneath take over again.
const phaseWindow = 15 * time.Second

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
}

var phases = phaseDesk{latest: map[string]PhaseNews{}}

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
		return
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

// phaseField is one part of the served segment in the spellings it is honest
// in: the whole of it, and the least of it that is still true.
//
// A FIELD IS NEVER CUT, IT IS ONLY SAID SHORTER. "3.1s" is the same fact as
// "first word 3.1s" with the label taken off, and "friendli" is the same fact
// as "friendli 38 t/s" with the measurement taken off — where a clip at the
// same width would leave "first word 3.…", which is a fact about nothing. An
// empty spelling is the field's third and last answer, which is the emptiness
// law: a part nobody has room for draws NOTHING rather than a stub of itself.
//
// A TIGHT FIELD RIDES THE ONE BEFORE IT ON A SPACE rather than on the surface's
// separator, because some fields are already joined by their own grammar. The
// arrow in "3.1s → parasail at 4.4s" is the joint; a dot in front of it would
// be a second one saying the same thing.
type phaseField struct {
	full  string
	short string
	tight bool
}

// phaseWord is a field with one spelling, which is most of them: a clock, a
// phase's own word, a rung of a ladder. It says the same thing at every width
// or it says nothing.
func phaseWord(word string) phaseField { return phaseField{full: word, short: word} }

// phaseSegment is the served segment AS DATA rather than as a finished string,
// so that the row can decide how much of it there is room for without cutting
// any of it.
//
// THE PRIMARY IS THE HEAD OF THE SEGMENT and it is kept whole for as long as
// anything is drawn at all: the machine that is answering when one has named
// itself, and the phase's own word when none has. Everything else is a field in
// PRIORITY ORDER, most important first, and the fields are what the width takes
// — from the back, one spelling at a time. That order is the data hierarchy the
// owner asked for: the lane name, then the phase and its clock and whatever the
// build will do about it, then the first-word figure, then the rate.
//
// The primary carries two spellings for the same reason a field does. "via
// coreweave" and "coreweave" name the same machine; the lead word is grammar
// and is the first thing a narrow row spends, and the NAME under it is never
// cut — a segment with no room for the whole name draws nothing at all.
type phaseSegment struct {
	primary phaseField
	fields  []phaseField
}

// fitPhaseSegment is the segment in the widest spelling that fits, and nothing
// at all when even the bare primary does not. A width below zero is no bound.
//
// THIS FUNCTION IS A PLACEHOLDER FOR internal/tui3/rowfit.go's FITTER, which is
// being built on its own branch as the one field-priority fitter this surface
// has. It is written to the same shape on purpose — a primary that survives the
// longest, telemetry fields with a full, a short and an absent spelling, added
// by priority — so that the rebase is a single call and a deletion: whoever
// lands rowfit.go replaces this body with a call to it, deletes these two
// functions, and changes nothing else in this file. Do not grow a second width
// ladder here; grow rowfit.go instead.
//
// The rungs, widest first: everything; the lead word spent to keep every field
// whole; every field said short; then the fields dropped from the back, one at
// a time, until only the primary is left.
func fitPhaseSegment(seg phaseSegment, width int) string {
	rungs := []string{
		seg.say(seg.primary.full, len(seg.fields), false),
		seg.say(seg.primary.short, len(seg.fields), false),
	}
	for kept := len(seg.fields); kept >= 0; kept-- {
		rungs = append(rungs, seg.say(seg.primary.short, kept, true))
	}
	for _, rung := range rungs {
		if rung != "" && (width < 0 || ansi.StringWidth(rung) <= width) {
			return rung
		}
	}
	return ""
}

// say is one rung: the primary in the spelling it was handed, and the first
// kept fields after it, each in its short spelling or its full one. Every empty
// part is dropped, which is the emptiness law as an operation — no rung has to
// write the same four `if word != ""` lines.
func (seg phaseSegment) say(primary string, kept int, short bool) string {
	words := primary
	for _, field := range seg.fields[:kept] {
		word := field.full
		if short {
			word = field.short
		}
		switch {
		case word == "":
		case words == "":
			words = word
		case field.tight:
			words += " " + word
		default:
			words += " · " + word
		}
	}
	return words
}

// phaseWords is the phase clock as a person reads it at a width nobody is
// short of, WITHOUT the leading separator — a caller adds the " · " or the
// space its own line wants:
//
//	connecting · 1.2s
//	first word · 3.1s → parasail at 4.4s
//	via coreweave · first word 3.1s → parasail at 4.4s
//	thinking · 12s · friendli 38 t/s
//	writing · 4s · friendli 61 t/s
//	paced · retry in 6s
//	trying again · 2 of 6
//	stalled 9s · switching to parasail
//	running go test · 41s
//	checking · 3s
//	tidying · 6s
//
// EVERY PART IS DROPPED WHEN IT IS NOT KNOWN, which is the emptiness law read
// segment by segment: no lane, no lane; no rate, no rate; no real deadline, no
// arrow. What is left is still true.
func phaseWords(news PhaseNews, now time.Time) string {
	return fitPhaseSegment(phaseSegmentOf(news, now), -1)
}

// phaseSegmentOf is every phase this surface has learned to say, as the data a
// width can be applied to. It is the vocabulary in one place: a phase's parts,
// which of them leads, and what each of them is worth when the row runs out.
//
// TWO SPELLINGS OF A CLOCK, and the difference is what the number is for. The
// phases a person is WAITING THROUGH with nothing arriving — the handshake and
// the queue before the first word, and the deadline hung off them — are read in
// tenths ([tookWord]), because the difference between 1.2s and 3.1s is the whole
// of what those seconds tell you. Every other phase is work in progress and is
// read in whole seconds ([countUpWord], the spelling every other live clock on
// this surface uses), because a tenth on a `go test` is a digit that changes
// under the eye and means nothing.
func phaseSegmentOf(news PhaseNews, now time.Time) phaseSegment {
	since := now.Sub(news.Since)
	if since < 0 {
		since = 0
	}
	word := string(news.Phase)
	switch news.Phase {
	case provider.PhaseConnecting:
		return phaseWaitSegment(news, tookWord(since), false)
	case provider.PhaseFirstWord:
		return phaseWaitSegment(news, tookWord(since), true)
	case provider.PhaseThinking, provider.PhaseWriting:
		return phaseSegment{
			primary: phaseWord(word),
			fields:  []phaseField{phaseWord(countUpWord(since)), phaseServing(news)},
		}
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
		return phaseSegment{primary: phaseWord(word), fields: []phaseField{phaseWord(clock)}}
	case provider.PhaseRetrying:
		// The rung of the ladder is better than the clock when the ladder said
		// which rung it is on: "2 of 6" answers "is this going anywhere?" and a
		// count-up does not.
		rung := news.Detail
		if rung == "" {
			rung = countUpWord(since)
		}
		return phaseSegment{primary: phaseWord(word), fields: []phaseField{phaseWord(rung)}}
	case provider.PhaseSwitching:
		// THE STALL COMES FIRST BECAUSE IT IS THE REASON. A rescue reads as an
		// answer to something, and the something is how long the first machine
		// had gone quiet; the switch alone is the same sentence with the cause
		// taken out of it. So the cause is what a narrow row spends, and the
		// rescue itself — the one part a person would act on — is the primary.
		if news.Then != "" {
			word += " to " + strings.ToLower(news.Then)
		}
		if news.Detail == "" {
			return phaseSegment{primary: phaseWord(word)}
		}
		return phaseSegment{primary: phaseField{full: news.Detail + " · " + word, short: word}}
	case provider.PhaseSwitchingModel:
		// AND THE LAST RUNG SAYS SO BY NAME. Changing which machine writes an
		// answer is bookkeeping and reads as "switching"; changing which MODEL
		// writes it is a different answer to the question that was asked, and a
		// person who chose one model and is being answered by another is owed
		// that sentence while it happens rather than in the transcript
		// afterwards (the ladder, in docs/ARCHITECTURE.md). The name is the
		// model's own base, spelled as the model segment beside it spells it.
		seg := phaseSegment{primary: phaseWord(word)}
		if news.Then != "" {
			named := "→ " + modelBase(news.Then)
			seg.fields = []phaseField{{full: named, short: named, tight: true}}
		}
		return seg
	case provider.PhaseRunning:
		// The tool's own name is the substance and the verb is the frame, so a
		// narrow row keeps "running" and lets the noun go before the clock does.
		return phaseSegment{
			primary: phaseField{full: phaseJoinWord(word, news.Detail), short: word},
			fields:  []phaseField{phaseWord(countUpWord(since))},
		}
	case provider.PhaseChecking, provider.PhaseTidying:
		return phaseSegment{primary: phaseWord(word), fields: []phaseField{phaseWord(countUpWord(since))}}
	}
	// A PHASE THIS SURFACE HAS NEVER HEARD OF DRAWS NOTHING, rather than its own
	// machine word with a clock after it. The vocabulary is closed and spelled
	// in one place; a name that is not in it is a seam that has grown a word
	// this file has not learned to say, and the older readings underneath are a
	// better answer than a stranger's noun.
	return phaseSegment{}
}

// phaseWaitSegment is a phase a person is waiting through: the handshake and
// the queue before the first word.
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
// Only the queue before the first word carries a consequence. The handshake has
// no alternative armed behind it, and a countdown to nothing is the one thing
// this file exists to refuse.
func phaseWaitSegment(news PhaseNews, clock string, consequence bool) phaseSegment {
	var seg phaseSegment
	if lane := strings.ToLower(news.Lane); lane != "" {
		seg.primary = phaseField{full: "via " + lane, short: lane}
		seg.fields = []phaseField{{full: phaseJoinWord(string(news.Phase), clock), short: clock}}
	} else {
		seg.primary = phaseWord(string(news.Phase))
		seg.fields = []phaseField{phaseWord(clock)}
	}
	if !consequence {
		return seg
	}
	if full, short := phaseConsequence(news); full != "" {
		seg.fields = append(seg.fields, phaseField{full: full, short: short, tight: true})
	}
	return seg
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
func phaseServing(news PhaseNews) phaseField {
	served := strings.ToLower(news.Lane)
	rate := laneRateWord(news.Rate)
	switch {
	case served != "" && rate != "":
		return phaseField{full: served + " " + rate, short: served}
	case served != "":
		return phaseWord(served)
	default:
		return phaseField{full: rate}
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
