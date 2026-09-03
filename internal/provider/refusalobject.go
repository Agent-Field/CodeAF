package provider

import (
	"net/http"
	"strings"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── ONE REFUSAL OBJECT, THREE READERS ───────────────────────────────────────
//
// A refusal used to be classified three times, by three pieces of code that had
// each been told a different half of the truth, and the class that mattered
// most fell through all three.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// A chat run on 2026-09-01 collected six HTTP 404s of one shape:
//
//	Providers serving <model>: digitalocean, streamlake, baidu, deepinfra,
//	but your request's provider.only preference permits only: coreweave
//
// That sentence means ONE thing — the machine this request demanded cannot
// serve this model — and it is the one refusal class that is certain about a
// lane. Nothing acted on it. The strike ([Client.refuseUpstream]) required
// `provider_name` in the error metadata, which the router omits precisely when
// it is answering for itself; the ladder's first rung was offered only for
// `require_parameters`, `ignore` and `max_price`, so a pinned request climbed
// six rungs still pinned to the lane that had said no; the frontier went on
// scoring the lane because nothing wrote the refusal back; and the surface drew
// the whole thing as `· slow · trying nextbit…`, a sentence about a wait. The
// same lane was chosen three separate times in one run.
//
// Three mirror-image defects and one mute surface, and every one of them is a
// reading of the same fact. So the fact is decided ONCE, here, and read three
// times:
//
//	(a) the strike        velocity.go's [Client.refuseUpstream]
//	(b) the ladder/walk   endpoints.go's [Client.relaxationPlan], hedge.go's walk
//	(c) the screen        [RescueNews] → internal/session → internal/tui3
//
// ── THE LANE COMES FROM OUR OWN REQUEST, NEVER FROM THEIR PROSE ─────────────
//
// The refusal above names the lane twice in English, and reading it out of
// there would work until the day the router rewords it — which is exactly the
// failure docs/design/failsafe/FAILSAFE.md rule 1 is about, and exactly what
// endpoints.go's phrase list was demoted to a hint for. THE LANE A REFUSAL IS
// ABOUT IS THE LANE OUR REQUEST DEMANDED, which this process wrote itself, knows
// exactly, and can read with no parsing at all ([Client.onlyLane]).

// refusalKind is what a refusal IS, decided from structure alone.
type refusalKind uint8

const (
	// refusalNone is everything this classifier has no opinion about: a
	// success, a 429 (which is pacing and has its own patience, retry.go), a
	// transport error, a refusal on a model the catalog has never heard of.
	refusalNone refusalKind = iota
	// refusalUpstream is the endpoint the ROUTER CHOSE saying no. Another
	// endpoint may well serve the same request, and the lane is paced rather
	// than written off ([APIError.FromUpstream]).
	refusalUpstream
	// refusalRouting is the ROUTER ITSELF saying nothing it can reach will
	// serve this request's shape (endpoints.go's [Client.routingRefusal]).
	refusalRouting
)

// laneRefusal is one refusal as every reader of it needs it.
//
// THREE FIELDS AND NO MORE, because three is what the three readers want
// between them: what happened, which machine it is about, and whether that
// machine is finished for this model. A field a reader has to interpret would
// be this classification happening a second time somewhere else.
type laneRefusal struct {
	Kind refusalKind
	// Lane is the machine the refusal is ABOUT, empty when it is about none.
	// For an upstream refusal it is the endpoint the router named; for a
	// routing refusal it is the endpoint our own request demanded.
	Lane string
	// Terminal says this lane cannot serve this model at all, so it is not
	// worth another second of anybody's deadline: the strike writes it out of
	// the serving set, the walk skips it, and the screen says refused rather
	// than slow.
	//
	// ONLY A ROUTING REFUSAL AGAINST A DEMANDED LANE IS TERMINAL. An upstream's
	// 4xx is that upstream's verdict on one request and it recovers; "the
	// router will not serve this model from that machine" is a fact about the
	// pairing, and asking again produces the identical 404 for nothing.
	Terminal bool
}

// struck reports whether there is a lane here for the ledger to act on.
func (r laneRefusal) struck() bool { return r.Kind != refusalNone && r.Lane != "" }

// RescueNews is a rescue as a SURFACE may read it, narrowed from [laneRefusal]
// so that a status line never has to interpret a transport's vocabulary.
//
// It travels through [HedgeReport.OnHedgeStart] — see waitreport.go — and it is
// derived from the classifier rather than assembled by hand, so the word a
// person reads and the decision the ledger took cannot drift apart.
type RescueNews struct {
	// Alt is the machine this news is about: the one a rescue is going to, or
	// the one whose rescue has just died.
	Alt string
	// Reason is why a rescue went out, in the two words a surface draws:
	// [RescueSlow] when a lane was merely late, [RescueRefused] when a lane
	// said no. Empty is a rescue nobody classified, which draws as slow.
	Reason string
	// Failed retracts a claim rather than making one: the `trying X…` this
	// surface is showing is about a machine that has now failed, and a status
	// line that goes on promising it is lying about the present tense.
	Failed bool
}

// The two words a rescue is drawn with. They are constants because three
// packages spell them — the transport writes them, internal/session carries
// them, internal/tui3 draws them — and a word spelled in three places is a word
// that gets reworded in one.
const (
	// RescueSlow is a lane that was late. Something is being done about it.
	RescueSlow = "slow"
	// RescueRefused is a lane that said no. Nothing more will be asked of it.
	RescueRefused = "refused"
)

// news is this refusal as the surface reads it, for a rescue going to alt.
func (r laneRefusal) news(alt string) RescueNews {
	reason := RescueSlow
	if r.Kind == refusalRouting || r.Terminal {
		reason = RescueRefused
	}
	return RescueNews{Alt: alt, Reason: reason}
}

// refusalObject is THE classifier. Everything this process does about a refusal
// is decided here, once, from the request that earned it.
//
// It is a method on the client because two of the three structural questions —
// is this a router at all, does the catalog know this model — are the client's
// own ([Client.routingRefusal] holds the whole argument).
func (c *Client) refusalObject(request *ai.Request, knobs callKnobs, err error) laneRefusal {
	if request == nil {
		return laneRefusal{}
	}
	model := c.modelFor(request)
	return c.laneRefusalFor(model, c.onlyLane(model, knobs, request), err)
}

// laneRefusalFor is the same classification asked by a caller that already
// knows which machine the request demanded.
//
// IT IS AN ENTRANCE AND NOT A SECOND CLASSIFIER. The race's walk holds an arm
// rather than a request — an arm IS its demanded lane (hedge.go's start) — so
// it answers the `only` question by construction and there is nothing for it to
// re-derive. Every rule about what a refusal means is here, once.
func (c *Client) laneRefusalFor(model, demanded string, err error) laneRefusal {
	refusal, ok := RefusalFrom(err)
	if !ok || refusal.Status == http.StatusTooManyRequests {
		// A 429 is pacing rather than a verdict on the request, and it is
		// answered by the wait the provider itself named (retry.go).
		return laneRefusal{}
	}
	// THE UPSTREAM'S OWN REFUSAL IS READ FIRST, because a named provider is the
	// router telling us it found something to try and that something said no —
	// which is the opposite class from an empty endpoint set, whatever status
	// the two arrive under.
	if refusal.FromUpstream() {
		return laneRefusal{Kind: refusalUpstream, Lane: refusal.Provider}
	}
	if !c.routingRefusal(model, refusal.Status, []byte(refusal.Body)) {
		return laneRefusal{}
	}
	// A REFUSAL ABOUT A LIST IMPLICATES NO MACHINE — the second half of the law
	// [velocityLedger.keepTheSetServable] enforces on the way out, standing here
	// on the way back. This sentence says an ignore list emptied the set, so the
	// demanded lane was never asked and never answered; filing it as that lane's
	// refusal would pace the machine, write it out of the serving set, and widen
	// the very list that caused the refusal, so the next request is refused
	// sooner. The list is what has to change, and it does, at the seam above.
	if ignoredEverything([]byte(refusal.Body)) {
		return laneRefusal{Kind: refusalRouting}
	}
	demanded = strings.TrimSpace(demanded)
	return laneRefusal{Kind: refusalRouting, Lane: demanded, Terminal: demanded != ""}
}

// onlyLane is the ONE machine this request demanded on the wire, empty when it
// demanded none.
//
// IT READS THE FIELD RATHER THAN THE REASONS FOR IT. Three different decisions
// can put `provider.only` on a body — a rescue demanding the machine the
// primary is not on, a person's strict lane pin, and a choice already made for
// a streamed call — and enumerating them here would be a fourth place that has
// to be kept in step with the encoder. So the composed object the request
// actually went out with is asked ([Client.wirePreferences]), which is also why
// a request that has climbed the ladder's first rung answers empty: that rung
// takes the whole provider object off, so there was no demand, and the refusal
// that follows is about the model rather than about a machine.
//
// SEVERAL MACHINES IMPLICATE NONE. `only` with two names in it is a refusal
// about a SET, and striking one of them would be this process guessing which.
// Nothing in this build sends more than one today; this is what it means if
// something ever does.
//
// AND RE-DERIVING THE OBJECT IS SAFE FOR THIS ONE FIELD, which is worth saying
// because the chooser underneath it is a SAMPLED decision and two draws are two
// different answers (lanes.go's [Client.laneChoiceFor]). A streamed call
// carries the choice it was decided on and re-derives nothing; an unstreamed
// one draws again, and the ranking it draws may differ — but `only` is never
// the ranking. It is set from a strict pin or from a rescue's own demand, both
// of which are facts rather than draws, so the machine named here is the
// machine that was named on the wire.
func (c *Client) onlyLane(model string, knobs callKnobs, request *ai.Request) string {
	prefs := c.wirePreferences(model, knobs, request)
	// ONE NAME OR NO NAME, because a strike has to name the machine it takes
	// away. A demand listing two machines and refused as a whole says only that
	// the pair could not serve the model between them, and striking either on
	// that evidence would be striking on a guess — the same reason a refusal
	// that names no upstream at all is left to the breaker in #138 rather than
	// struck here. Nothing on the wire builds such a demand today: a person's
	// pin is one machine (lanes.go) and a rescue's demand is the single arm it
	// walked to, so this arm is a guard against a future caller rather than a
	// case anybody can reach.
	if prefs == nil || len(prefs.Only) != 1 {
		return ""
	}
	return strings.TrimSpace(prefs.Only[0])
}

// refuseServing writes a terminal refusal into the negative half of the serving
// set, so that the NEXT choice cannot pick the machine the wire has just
// refused (internal/lane's sheet.go).
//
// IT FOLDS THE MODEL ON THE WAY IN and never files under the spelling that
// happened to be on the wire ([laneModel]). The defect this closes had the bare
// id and the dated slug disagreeing about who serves a model; a refusal written
// under one and read under the other would rebuild that disagreement one layer
// down.
func (c *Client) refuseServing(model string, refusal laneRefusal) {
	if !refusal.Terminal {
		return
	}
	lanes.RefuseServing(laneModel(model), refusal.Lane)
}
