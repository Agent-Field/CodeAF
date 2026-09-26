package tui3

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── THE SURFACE'S SIDE OF LANES ─────────────────────────────────────────────
//
// A model id is an address; the LANE is the machine behind it. One id is served
// by a dozen endpoints that differ by seven times on the wait before the first
// word and by twelve times on how fast they write, at roughly the same price —
// so which lane answers is often a bigger difference than which model does, and
// until this file the surface showed none of it. internal/lane holds the belief;
// this file is the only place that DRAWS one.
//
// Three laws, and they are the package's own said again where a person can see
// them (internal/lane's lane.go):
//
//   - EVERY NUMBER SHOWN IS THE POSTERIOR. Not the sheet's row, not the last
//     answer: the belief that combined them, aged to this moment. Two surfaces
//     computing their own would drift the first time one was fixed.
//   - UNKNOWN DRAWS NOTHING. A model whose lanes nobody has measured gets no
//     speed on its row and no machines in its fold — only the two answers
//     that name none, `auto` and `openrouter` (palette.go's [picker.unfoldAt]).
//     A blank row is readable; an invented number is a router steering on a
//     measurement nobody took (design-law-v2 §16).
//   - THE WORD "SLOW" IS ONLY SAID WHILE SOMETHING IS BEING DONE ABOUT IT.
//     That is the whole of [app.laneRider]'s middle state.
//
// Nothing here fetches, and nothing here chooses. The ledger read is memory
// only by its own contract, and the chooser is pure — both are safe on a draw
// path, which is where every function below is called from.

// ── WHAT THE LAYER THAT SENT THE ANSWER TELLS US ────────────────────────────

// LaneNews is one answer's lane story, as the layer that sent it knows it.
//
// IT IS THE ONE HOOK THIS PACKAGE OFFERS and it is deliberately small. The
// surface cannot see a stream — that belongs to internal/session and
// internal/provider — so the three things it draws that a belief cannot answer
// arrive here: who served, whether a rescue went out, and who finished.
//
// The fields expected from the response boundary, by name:
//
//	Lane      the lane the request was sent to (a chunk's `provider`)
//	Hedged    Response.Hedged — a second request went out for this answer
//	Alt       the lane that second request went to
//	Winner    Response.Winner — the lane whose answer was actually read
//	Trying    the in-flight event: a hedge is out and nobody has committed yet
//	Reason    why that rescue went out — "slow" or "refused"
//	Failed    the retraction: the machine named in Alt has failed
//
// A caller posts twice for a rescued answer: once with Trying while the hedge
// is in flight, and once at the end with Hedged and Winner. It may post once
// for every other answer. Anything it does not know is left zero, and a zero
// field draws nothing.
//
// AND A RESCUE THAT DIES POSTS AGAIN TO TAKE ITS OWN SENTENCE BACK. `trying
// coreweave…` is a claim about the present tense, and until Failed existed
// nothing withdrew it: a refused arm left the promise on the status line until
// the ten-minute window aged it out (issue #266).
type LaneNews struct {
	Model  string
	Lane   string
	Alt    string
	Winner string

	TTFT time.Duration
	Rate float64

	Hedged bool
	Trying bool

	// Reason is why the rescue went out, in the transport's own two words
	// ([provider.RescueSlow], [provider.RescueRefused]) — or
	// [provider.RescueRetired], which is not about a rescue at all but about a
	// person's pin, refused for this model and stood down until they pin again
	// (issue #456). It is CARRIED and never
	// decided here: a surface that formed its own opinion about a refusal would
	// be a second classifier, drifting from the one the routing acted on
	// (internal/provider's refusalobject.go). An empty word reads as slow, which
	// is what every rescue was called before there was a word at all.
	Reason string
	// Failed says the machine named in Alt has failed, so whatever this surface
	// last said about it is no longer true.
	Failed bool

	// Subject is WHAT THIS SIGHTING IS ABOUT — the conversation, or one task
	// node of it — and it is [provider.PhaseNews.Subject]'s twin, spelled the
	// same way for the same reason: a window draws its own subject's news, and
	// the two desks in this package must not key differently ([newsDeskKeys]).
	//
	// EMPTY MEANS THE CONVERSATION, which is what every producer that predates
	// the field and every older peer across a connection sends.
	Subject string

	// Session is WHOSE sighting this is — the conversation it belongs to, in the
	// engine's own spelling ([provider.PhaseNews.Session]'s twin). A
	// conversation's own sighting is filed under it first and under its model
	// beside it ([newsDeskKeys]), so a model that moved between the engine and
	// this window cannot file the machine under a name nobody is reading. Empty
	// is an older peer, and files under the model alone, as it always did.
	Session string

	// Role is who the answer was for (internal/lane's roles.go), carried from
	// the seam that already knows it (internal/session's lanenews.go).
	//
	// ONLY A ROLE A PERSON IS READING MOVES THE STATUS LINE. A talk turn also
	// asks for a title, a memory reflex and a reply check, each of which comes
	// back on its own lane at its own speed; a rider that took whichever
	// finished last told somebody about a machine that had nothing to do with
	// the answer they were waiting for. A role nobody named reads as hidden,
	// which is the conservative half of that reading and the one the role table
	// itself takes.
	Role lane.Role

	At time.Time
}

// rescueNews reports whether this post is about a RESCUE rather than about an
// answer that finished: one in flight, or the retraction of one, or a pin the
// wire has retired. Those are kept in a slot of their own beside the last
// sighting ([laneDesk]) rather than in its place.
func (n LaneNews) rescueNews() bool { return n.Trying || n.Failed }

// rescued reports whether this answer was finished somewhere other than where
// it started. It is the only reading of "rescued" the surface has, and it is
// deliberately strict: a hedge that lost is not a rescue, it is a measurement.
func (n LaneNews) rescued() bool {
	return n.Hedged && n.Winner != "" && !strings.EqualFold(n.Winner, n.Lane)
}

// laneSightings is the most of our own answers one lane's sparkline draws. Eight
// because that is what fits in the tail of a row a person is scanning, and
// because a sparkline longer than the eye takes in at once is a chart nobody is
// reading anyway.
const laneSightings = 8

// laneDesk is what this process has been TOLD, as opposed to what it believes.
//
// It is two small things: the latest news per SUBJECT, which is what the status
// line draws, and a ring of our own recent first-token waits per lane, which is
// what the sparkline draws. Neither is a belief and neither pretends to be —
// the ledger owns believing, and it publishes no history, so the ring lives
// here rather than being re-derived from a posterior that has forgotten it.
//
// THE TWO HALVES ARE KEYED BY DIFFERENT THINGS ON PURPOSE. The latest news is a
// claim about one piece of WORK — this conversation, or this node — so it is
// filed under its subject (phase.go's [newsDeskKeys] and the law above it): a
// desk keyed by model let two nodes on one model id overwrite each other. The
// ring is a claim about one MACHINE serving one model, which is what the
// sparkline is drawn from and what the ledger's own belief is about, so it
// stays keyed by the pair it measures. A subject in that key would split one
// machine's history across every node that ever used it.
//
// AND A RESCUE HAS A SLOT OF ITS OWN BESIDE THE SIGHTING, rather than
// overwriting it. There used to be one entry per subject, so the news that a
// rescue had gone out REPLACED the news of which machine last answered — and
// when that rescue then failed without being refused, the rider had nothing
// left to say and `via` went blank. Nothing put it back: the next sighting is
// posted only when an answer finishes, and an interrupted or errored step never
// finishes one. So the rescue goes in `rescue`, the sighting stays in `latest`,
// and the next sighting clears the rescue, because a finished answer is the end
// of every rescue that was about it.
type laneDesk struct {
	mu     sync.RWMutex
	latest map[string]LaneNews
	rescue map[string]LaneNews
	rings  map[string][]int
}

var desk = laneDesk{latest: map[string]LaneNews{}, rescue: map[string]LaneNews{}, rings: map[string][]int{}}

// laneStory is what one subject's desk entry holds: the last answer's sighting,
// and a rescue posted since it, each with whether there is one at all.
type laneStory struct {
	seen, rescue       LaneNews
	hasSeen, hasRescue bool
}

// PostLaneNews is how the layer that sent an answer tells the surface what the
// lanes did. It is safe from any goroutine, it never blocks on a draw, and it
// keeps nothing about an answer that could not say which lane served it: a
// sighting credited to nobody is a fact about a machine that was not involved.
//
// A ROLE NOBODY IS READING NEVER REACHES THE STATUS LINE'S HALF, which is the
// phase desk's own door said again ([PostPhaseNews]). The errands beside a talk
// turn — a title, a memory reflex — share the conversation's subject and finish
// on some lane of their own; filed here, the last of them to finish REPLACED the
// answer's sighting, and the rider, which draws only a visible role, then drew
// nothing at all. That was one more way `via` vanished after an answer. Their
// first-token waits still feed the ring below, because the ring is about the
// machine, and a machine is exactly as fast for a title as for an answer.
func PostLaneNews(news LaneNews) {
	news.Model = strings.TrimSpace(news.Model)
	news.Lane = strings.TrimSpace(news.Lane)
	news.Subject = strings.TrimSpace(news.Subject)
	news.Session = strings.TrimSpace(news.Session)
	if news.Model == "" {
		return
	}
	if news.At.IsZero() {
		news.At = time.Now()
	}
	desk.mu.Lock()
	defer desk.mu.Unlock()
	if news.Role.Visible() {
		for _, key := range newsDeskKeys(news.Subject, news.Session, news.Model) {
			if news.rescueNews() {
				desk.rescue[key] = news
				continue
			}
			desk.latest[key] = news
			delete(desk.rescue, key)
		}
	}
	if news.Lane == "" || news.TTFT <= 0 {
		return
	}
	key := lane.ID{Model: news.Model, Lane: strings.ToLower(news.Lane)}.String()
	ring := append(desk.rings[key], int(news.TTFT.Milliseconds()))
	if len(ring) > laneSightings {
		ring = ring[len(ring)-laneSightings:]
	}
	desk.rings[key] = ring
}

// laneNewsFor is the last answer's sighting for one subject, false when none
// has arrived. The key is one of phase.go's [newsDeskKeys], so this desk and the
// phase desk cannot come to two ideas of what a piece of news is called.
func laneNewsFor(key string) (LaneNews, bool) {
	desk.mu.RLock()
	defer desk.mu.RUnlock()
	news, ok := desk.latest[strings.TrimSpace(key)]
	return news, ok
}

// laneStoryFor is the whole of what the desk holds for one name: the single
// name a room has, or one of the conversation's two ([app.talkLaneStory]).
func laneStoryFor(key string) (laneStory, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return laneStory{}, false
	}
	desk.mu.RLock()
	defer desk.mu.RUnlock()
	seen, hasSeen := desk.latest[key]
	rescue, hasRescue := desk.rescue[key]
	return laneStory{seen: seen, rescue: rescue, hasSeen: hasSeen, hasRescue: hasRescue}, hasSeen || hasRescue
}

// talkLaneStory is the conversation's story, asked in [app.talkKeys]' order:
// the first of its names with anything filed under it answers.
func (a *app) talkLaneStory() laneStory {
	conversation, model := a.talkKeys()
	if story, ok := laneStoryFor(conversation); ok {
		return story
	}
	story, _ := laneStoryFor(model)
	return story
}

// laneSpark is our own last few first-token waits on one lane, oldest first, in
// milliseconds. Nothing when we have never timed it — which draws nothing.
func laneSpark(model, name string) []int {
	desk.mu.RLock()
	defer desk.mu.RUnlock()
	ring := desk.rings[lane.ID{Model: model, Lane: strings.ToLower(name)}.String()]
	if len(ring) == 0 {
		return nil
	}
	return append([]int(nil), ring...)
}

// forgetLanes empties the desk. It is for tests, which must not inherit another
// test's answers.
func forgetLanes() {
	desk.mu.Lock()
	defer desk.mu.Unlock()
	desk.latest = map[string]LaneNews{}
	desk.rescue = map[string]LaneNews{}
	desk.rings = map[string][]int{}
}

// timeNow is the wall clock, named once so that the handful of draw sites that
// need a moment and hold no session read the same one. Ageing a belief by the
// milliseconds between two of these cannot move a figure rounded to a tenth of
// a second, which is why they are not threaded a moment from the frame.
func timeNow() time.Time { return time.Now() }

// ── WHAT WE BELIEVE, AS A ROW ───────────────────────────────────────────────

// laneView is one lane as the picker draws it: the posterior, aged, with the
// facts the gate would have judged it on.
//
// TTFT is in SECONDS and Rate in tokens per second — the units on the row,
// converted once here rather than at three drawing sites. PriceOut is dollars
// per token, as the sheet publishes it and as [perMillion] expects it.
type laneView struct {
	Name string

	TTFT float64
	Rate float64
	Tail float64
	// Vague says the belief has aged past saying anything about this lane's
	// worst case ([laneTail]), so neither a tail nor its absence is drawn.
	Vague bool
	// Wait is the first token at the NINETIETH PERCENTILE, in seconds. It is
	// what the ordering is done on and it is never drawn: a person remembers
	// the twelve-second wait and not the four-hundred-millisecond one, so a
	// lane that is quickest at the median and among the worst at the tail has
	// to lose here (the ideation's second fact).
	Wait float64

	Uptime   float64
	PriceOut float64
	Quant    string
	MaxOut   int
	Tools    bool

	// Known is whether the belief carries any timing at all. A lane the gate
	// could judge and the score could not is still a row — the facts are true —
	// but it draws no numbers.
	Known bool
	// Poor says this lane's ANSWERS have been going wrong: enough replies this
	// process could not use that the belief's quality bound has fallen under
	// what a conversation asks for.
	//
	// It is a bool rather than a share because a row is not the place to argue
	// with a posterior — a person wants to know whether to send work here, and
	// "0.71" is not that answer. False for a lane nobody has judged, which is
	// the emptiness law: an unjudged lane is not a suspect.
	Poor bool
	// Sightings is our own recent first-token waits on this lane, in
	// milliseconds and oldest first — the sparkline's readings, and the count
	// the why line names. Empty for a lane the sheet alone knows.
	Sightings []int
}

// laneViews is every lane believed in for one model, best first.
//
// THE AGEING HAPPENS HERE AND ONLY HERE. A belief written eleven minutes ago is
// worth about half what it was, and the p99 that decides whether a row says
// `tail` is the aged one — otherwise a lane that misbehaved once at breakfast
// would wear the word all day.
func laneViews(model string, now time.Time) []laneView {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	beliefs := lane.Default().Ledger().Beliefs(model)
	if len(beliefs) == 0 {
		return nil
	}
	views := make([]laneView, 0, len(beliefs))
	for _, belief := range beliefs {
		if belief.ID.Lane == "" {
			continue
		}
		ttft, rate := belief.TTFT, belief.Rate
		if !belief.At.IsZero() {
			ttft = ttft.Predict(now.Sub(belief.At), lane.HalfLife)
			rate = rate.Predict(now.Sub(belief.At), lane.HalfLife)
		}
		view := laneView{
			Name:      belief.ID.Lane,
			Uptime:    belief.Facts.Uptime5m,
			PriceOut:  belief.Facts.PriceOut,
			Quant:     belief.Facts.Quant,
			MaxOut:    belief.Facts.MaxOut,
			Tools:     belief.Facts.Tools,
			Known:     belief.Known(),
			Poor:      poorlyServing(belief, now),
			Sightings: laneSpark(model, belief.ID.Lane),
		}
		view.TTFT = ttft.Mean() / 1000
		view.Rate = rate.Mean()
		view.Wait = ttft.Quantile(laneWaitZ) / 1000
		view.Tail, view.Vague = laneTailOf(ttft, view.TTFT)
		views = append(views, view)
	}
	sortLanes(views)
	return views
}

// laneTailOf is [laneTail] asked of a belief rather than of a figure, and it is
// the door [laneViews] uses.
//
// A BELIEF THAT HAS WIDENED AS FAR AS IT IS ALLOWED TO SAYS NOTHING ABOUT ITS
// WORST CASE, and the spread is the only place that fact still shows. Ageing
// doubles the spread every half-life ([lane.Posterior.Predict]) and
// [lane.MaxSpread] is where that doubling stops — a floor under the arithmetic
// and not a judgement, put there so the one unbounded expression in that package
// cannot reach a number no reader of it can hold. The row used to read the
// overflow itself: before the clamp a lane nobody had heard from since yesterday
// arrived with a p99 of +Inf, which [laneTail] caught. Now it arrives at exactly
// the ceiling, where σ ≈ 2.4 makes the p99 a couple of hundred times the median —
// a perfectly finite figure that is still arithmetic and not a measurement. So
// the emptiness law is applied to the BELIEF, where the doubt lives, rather than
// to the one symptom of it that a later clamp took away.
func laneTailOf(ttft lane.Posterior, median float64) (tail float64, vague bool) {
	if ttft.P >= lane.MaxSpread {
		return 0, true
	}
	return laneTail(ttft.Quantile(laneTailZ)/1000, median)
}

// laneTail is the p99 first token in seconds when it is worth a word on a row,
// and zero when it is not; vague says the belief has aged past saying anything
// about its worst case at all.
//
// A TAIL IS ONLY A TAIL WHEN IT IS FAR ENOUGH PAST THE MEDIAN to be a different
// experience — five times — AND WHEN IT IS A WAIT SOMEBODY COULD HAVE HAD. A
// figure that is not finite, or longer than any request is allowed to stay open
// ([provider.WallCeiling]), is arithmetic and not a measurement, and the
// emptiness law draws it as nothing — including not as `no tail`, which is a
// claim about the worst case too.
func laneTail(p99, median float64) (tail float64, vague bool) {
	if math.IsNaN(p99) || math.IsInf(p99, 0) || p99 > provider.WallCeiling.Seconds() {
		return 0, true
	}
	if median > 0 && p99 > laneTailRatio*median {
		return p99, false
	}
	return 0, false
}

// laneTailZ is the standard-normal quantile of the 99th percentile, and
// laneTailRatio is how much worse than the median a tail has to be before it is
// worth a word on a row. Five is the point where a person stops reading the
// answer and starts watching the cursor.
const (
	laneTailZ     = 2.3263
	laneTailRatio = 5.0
	// laneWaitZ is the ninetieth percentile, which is what [laneView.Wait]
	// orders on.
	laneWaitZ = 1.2816
	// laneQualityZ is the same ninetieth percentile read on the QUALITY belief,
	// and it is the gate's own figure: the frontier drops a lane when
	// Quality.Upper(z90) falls under the role's need, so a row that used the
	// mean would be describing a different rule from the one doing the
	// dropping (internal/lane's frontier.go).
	laneQualityZ = 1.2816
)

// sortLanes puts the lane a talk turn would feel fastest on first.
//
// IT IS THE PACKAGE'S OWN OBJECTIVE AND NOT A SECOND ONE ([lane.PerceivedSeconds]):
// the first token, plus four hundred visible tokens at no more than reading
// speed. That is what makes the `auto` row's "{lane} now" and the model row's
// `via` agree with each other — one rule, asked twice.
func sortLanes(views []laneView) {
	sort.SliceStable(views, func(a, b int) bool {
		return laneFeel(views[a]) < laneFeel(views[b])
	})
}

// laneTalkTokens is the answer a talk turn is scored against: the tokens a
// person reads, and nothing they do not.
//
// IT IS THE CHOOSER'S OWN FIGURE and not a second one. The row's sort and the
// chooser's price both stand for the same imagined answer — an ordinary turn
// nobody has measured yet ([lane.AssumedAnswerTokens]) — so a number written
// out here would be a second answer to "how long is a talk answer", drifting
// the moment either side moved.
const laneTalkTokens = lane.AssumedAnswerTokens

// laneFeel is how long a talk answer would feel on this lane, in seconds, and
// it is scored on the WAIT rather than on the median first token: two lanes are
// told apart by their bad days, and the one whose bad day is twelve seconds is
// not the fast one however good its median is.
//
// A lane with no timing feels infinitely long, which sorts it last without ever
// claiming a number for it.
func laneFeel(view laneView) float64 {
	if !view.Known || view.Rate <= 0 {
		return math.Inf(1)
	}
	wait := view.Wait
	if wait <= 0 {
		wait = view.TTFT
	}
	return lane.PerceivedSeconds(wait, view.Rate, laneTalkTokens, 0)
}

// bestLane is the lane `auto` would land on right now, and false when nothing
// is believed about any of them.
func bestLane(views []laneView) (laneView, bool) {
	for _, view := range views {
		if view.Known {
			return view, true
		}
	}
	return laneView{}, false
}

// laneFor finds one lane's row by name, case-folded the way every comparison in
// this file is: the wire spells a lane however it likes and a person types it
// however they like.
func laneFor(views []laneView, name string) (laneView, bool) {
	for _, view := range views {
		if strings.EqualFold(view.Name, name) {
			return view, true
		}
	}
	return laneView{}, false
}

// ── WHICH LANE IS ANSWERING ─────────────────────────────────────────────────

// laneForce is WHAT THE WIRE WILL DO WITH THE PIN, read once: the machine the
// next request for this model will demand, and the machine the person's row
// still names after the wire has stopped asking for it.
type laneForce struct {
	// name is the machine the next request demands, and empty when it demands
	// none — `auto`, `openrouter`, a pairing the wire has retired, or a base
	// that carries no lane choice at all.
	name string
	// standDown is the machine the `lane` row names while nothing asks for it,
	// and empty while the row and the wire agree. It is what lets a row say
	// `auto` and still say whose name came off it.
	standDown string
}

// laneInForce is THE ONE READING every place on this surface that names the
// machine behind the conversation's model makes — the chip on the model word,
// the settings panel's `lane` tail, the mark inside the picker's fold, and the
// `via` on a row.
//
// IT IS THE TRANSPORT'S ANSWER AND NEVER THE PROFILE ROW'S ([provider.PinNow]).
// The chip was drawn from the row while the wire asked the transport, and on
// 2026-09-13 a pairing the wire had retired mid-session left `@morph` on the
// model word over three turns another machine answered (issue #1022). A surface
// may name a machine only while a request would demand it. The row a person
// wrote is still theirs and still unchanged on disk — it is on the `lane` row,
// which is where they go to look at it, and pinning again puts it straight back.
//
// AND THE MODEL IS FOLDED BY THE TRANSPORT AND NOT HERE, which is the other
// half of that defect: the retirement is written under the ledger's spelling of
// the model and this surface holds its own, so the question goes through the
// door that folds rather than against a key built on this side.
func laneInForce(model string) laneForce {
	demanded, standDown := provider.PinNow(model)
	return laneForce{name: demanded, standDown: standDown}
}

// laneNow is the lane a request for this model would go to, and false when
// nothing here can say. THE RULE IS THREE RUNGS AND IT IS STATED ON PURPOSE,
// because a `via` on a picker row that disagreed with the `via` on the status
// line would be two answers to one question:
//
//  1. A PIN WINS — the pin THE WIRE WILL ACT ON ([laneInForce]) and not the row
//     on disk, which are the same fact until the wire retires a pairing and the
//     row goes on naming a machine nothing asks for.
//  2. THEN THE CHOOSER. It is pure and cheap, so the picker asks it exactly
//     what a request would ask it, and draws the top of the order it gets
//     back. An empty chooser has no opinion, which is a real answer.
//  3. THEN THE BELIEF. The lane [bestLane] names — the same one the `auto` row
//     says is "now" — so the fold and the row agree without either consulting
//     the other.
//
// It is deliberately not "whoever served last": the last lane is a fact about
// the previous answer, and this row is a claim about the next one.
func (a *app) laneNow(model string, views []laneView) (string, bool) {
	if name := laneInForce(model).name; name != "" {
		return name, true
	}
	name := laneAuto(a.routing, model, views, a.now())
	return name, name != ""
}

// laneAuto is rungs two and three of that rule — the chooser, then the belief —
// without the pin, which needs a profile to read and is therefore the app's.
// It is a free function because a picker row is drawn from places that hold no
// session (the settings panel's slot rows, the composer's), and a row that said
// nothing there would be the same list telling two stories.
//
// AND IT ANSWERS NOTHING WHERE NOTHING ON THIS SIDE CHOOSES. The name it gives
// is a prediction — the machine the chooser would send the next turn to — and
// under a routing row that runs no chooser (`simple`, the row this build
// ships) there is no such machine: the request goes out with no preference
// and the router answers from wherever it likes. A `via modal` on the model
// row while the status line and the record said Sail Research was the surface
// predicting a choice nobody was making (2026-09-13). The one door that says
// what `auto` may claim under a row ([laneAutoSaid]) is asked first.
func laneAuto(routing, model string, views []laneView, now time.Time) string {
	if !laneAutoSaid(routing).chooses {
		return ""
	}
	// THE QUESTION IS THE TURN'S OWN, and it is asked through the transport's
	// spelling of it ([provider.LaneTalkAsk]) rather than one written here. A
	// request built on this side with λ left at zero asks "which is CHEAPEST",
	// which is a perfectly correct answer to a question a conversation never
	// asks — and the row then named a machine the very next turn did not use.
	// One shape, stated where the wire states it, so the `auto` row and the
	// request it predicts cannot drift.
	//
	// AND IT ASKS TYPICALLY, not for this nanosecond's exploration draw.
	// Choose seeds on Now; a picker that asked the send-path question on
	// every frame named a different `via` each time, and the numbers
	// beside it jumped with it. The send still samples. The list shows
	// the machine auto would pick if it were not exploring this turn.
	ask := provider.LaneTalkAsk(model, now)
	ask.Typical = true
	choice := lane.Default().Chooser().Choose(ask)
	if len(choice.Order) > 0 && choice.Order[0] != "" {
		return choice.Order[0]
	}
	if best, ok := bestLane(views); ok {
		return best.Name
	}
	return ""
}

// laneSlotFor is which lane row a model's pin is written on. Every model the
// conversation talks to shares the conversation's slot: the row is about the
// machine behind the model you are on, and a person who pins one and switches
// model has not pinned a different endpoint for the new one.
//
// It takes the model so that a later wave can key pins per model without every
// call site changing, which is the whole reason it is a function.
func laneSlotFor(string) string { return talkSlot }

// laneSlotForRow is the lane slot ONE SETTINGS ROW answers for, and empty for a
// row that has no machine to name. It is [laneSlotFor]'s row-side twin and the
// whole map: the registry carries exactly one lane row ([config.LaneSlotTalk]),
// so the conversation's model row folds and the media slots — drawing,
// speaking, looking — do not, because there is no lane row their enter could
// write and no sheet of lanes published for them. A second lane slot is one
// case here and no new list anywhere.
func laneSlotForRow(key string) string {
	if key == config.ModelSettingKey(talkSlot) {
		return talkSlot
	}
	return ""
}

// armLanes gives one open picker everything it needs to DRAW lanes and to WRITE
// one: the slot its enter would write, the pin that slot is at, the promise the
// speed guard is making, and this terminal's glyphs.
//
// EVERY ONE OF THEM IS A SNAPSHOT, for the reason the model in use is: they
// answer "what am I on", and none of them can change while a modal list owns
// the keyboard.
//
// A list nobody arms folds nothing, which is the honest reading of "this row
// has no machine to choose" — a task's model, a role, a media slot — and a
// session launched with routing `off`, which sends no lane choice and measures
// nothing, so a fold would offer machines no request asks for and promise
// measurements that never come.
func (a *app) armLanes(p *picker, slot string) {
	// THE ROUTING ROW RIDES ALONG EVEN WHERE IT CLOSES THE FOLD, because it is
	// the one fact here that is about what codeaf PROMISES rather than about
	// what it has measured, and the row that makes that promise reads it
	// ([laneAutoSaid]).
	p.routing = a.routing
	if slot == "" || a.routingOff() {
		return
	}
	p.laneSlot = slot
	p.pin = config.LaneAt(a.profileDir, slot)
	// AND THE MACHINE THE WIRE WOULD ACTUALLY ASK FOR, beside the row and not
	// instead of it. The row tells the fold's three rungs apart and this is what
	// any row that draws a machine's NAME may draw ([picker.force]).
	p.force = a.pinnedNow()
	p.guard = config.LaneGuardAt(a.profileDir)
}

// routingOff is whether this session's routing row is `off` — the one answer
// that sends no lane choice at all and measures nothing (internal/provider's
// lanes.go), which leaves nothing for a fold to draw and nothing to pin.
//
// It asks [app.routing] rather than keeping a boolean of its own: the row has
// four answers now, and each place that cares cares about a different one of
// them.
func (a *app) routingOff() bool { return a.routing == config.RoutingOff }

// applyLaneChoice is enter on a row INSIDE an open fold: the lane the cursor is
// on, written for the model that fold belongs to.
//
// It is one function because the fold now has two doors — /model
// (palette.go's [app.pickerKey]) and the settings panel's model and lane rows
// (settings.go's [app.sheetSelectKey]) — and a pin written two ways is a pin
// that drifts the first time one of the two is fixed.
//
// ENTER ON THE MACHINE ALREADY IN FORCE TAKES THE PIN OFF. It is a toggle on
// the same key that put the pin on, which is the only way back to `auto` a
// person can find without being told: the `auto` row sits above every machine in
// the fold, so clearing a pin was sixteen presses of `↑` past all of them — and
// one press too many lands on another model's row, where enter switches the
// model instead (issue #1022). The `auto` row is still there and still the
// explicit way to say it.
//
// IT IS KEYED ON WHAT THE WIRE WOULD DEMAND ([laneInForce]) AND NOT ON THE ROW,
// which is what keeps the other promise the manual makes: once the wire has
// retired a pairing the row still names that machine and nothing is asking for
// it, so enter there is a person saying "try again" and puts the pin straight
// back rather than quietly clearing the row they were re-stating.
func (a *app) applyLaneChoice(model string, row pickRow, lanes []laneView) {
	switch {
	case row.lane == laneAutoAt:
		a.clearLanePin(model)
	case row.lane == laneRoutAt, row.lane == laneDefaultAt:
		// THE CONTAINER AND THE ROW INSIDE IT WRITE THE SAME THING, which is
		// what makes `enter` on `openrouter` a shortcut rather than a fourth
		// answer: it is the same choice reached one press earlier.
		a.setLaneRouterOnly(model)
	case row.lane >= 0 && row.lane < len(lanes):
		if name := lanes[row.lane].Name; strings.EqualFold(laneInForce(model).name, name) {
			a.clearLanePin(model)
		} else {
			a.pinLane(model, name)
		}
	}
}

// ── THE WORDS ───────────────────────────────────────────────────────────────

// laneSecondsWord is a wait in seconds, one decimal: `0.8s`. Nothing at all for
// a wait nobody measured.
func laneSecondsWord(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	return strconv.FormatFloat(seconds, 'f', 1, 64) + "s"
}

// laneRateWord is a throughput, whole tokens per second: `58 t/s`.
func laneRateWord(rate float64) string {
	if rate <= 0 {
		return ""
	}
	return strconv.Itoa(int(math.Round(rate))) + " t/s"
}

// laneSpeedWord is what a model row gains when its lanes are known:
// `▲0.8s 58t/s · via cloudflare`. Every part is dropped when nobody measured
// it, and the whole thing is dropped when nothing is.
//
// THE NUMBERS BELONG TO THE LANE THE ROW NAMES, and that is the whole of this
// function's care. It used to draw the numbers of whichever lane this file's
// own sort put first and then write somebody else's name after them — which was
// invisible while the chooser had no opinion and the two always agreed, and
// became a row saying one machine's speed under another machine's name the
// moment the chooser landed. A row like that is worse than a blank one: it is a
// measurement attributed to a machine that did not make it.
func laneSpeedWord(routing string, views []laneView, now string) string {
	best, ok := laneShown(routing, views, now)
	if !ok {
		return ""
	}
	parts := make([]string, 0, 2)
	if word := laneSecondsWord(best.TTFT); word != "" {
		parts = append(parts, laneUpMark+word)
	}
	if word := laneRateTight(best.Rate); word != "" {
		parts = append(parts, word)
	}
	speed := strings.Join(parts, " ")
	if now == "" {
		return speed
	}
	via := "via " + strings.ToLower(now)
	if speed == "" {
		return via
	}
	return speed + " · " + via
}

// laneShown is THE LANE A MODEL'S ROW SPEAKS FOR: the one the chooser would
// send the next turn to when anything is believed about it, and the best-known
// lane otherwise. False when nothing is believed at all, which is every row on
// a machine that has just started.
//
// It is one function because the row's numbers and the row's NAME have to come
// from the same view. They did not always: the tail drew whichever lane this
// file's own sort put first and then wrote the chooser's name after them, which
// was invisible while the two agreed and became a measurement attributed to a
// machine that did not make it the moment they stopped.
//
// AND WITH NO NAME AND NO CHOOSER THERE IS NO LANE TO SPEAK FOR. The best-known
// fallthrough is the chooser's claim — "this is where auto would send you" —
// and under a routing row that runs no chooser (`simple`, the row this build
// ships) it is nobody's: the row drew `▲1.0s · 30t/s`, which were parasail's
// numbers with parasail's name taken off (the 2026-09-13 acceptance drive), a
// measurement the next request would not be routed by. A pinned or named
// machine still speaks, because there the numbers are its own.
func laneShown(routing string, views []laneView, now string) (laneView, bool) {
	if best, ok := laneExactly(views, now); ok {
		return best, true
	}
	if !laneAutoSaid(routing).chooses {
		return laneView{}, false
	}
	return bestLane(views)
}

// laneRateTight is a throughput in the picker row's own spelling — `58t/s`,
// with no space in it.
//
// IT IS TIGHTER THAN THE FOLD'S ([laneRateWord]) ON PURPOSE. Inside an unfolded
// model the rate sits in a row of four numbers with room around them and reads
// as a measurement; on the model's own line it is one ranked fact among seven
// competing for a sixty-cell frame, and the space would be a cell spent on air.
func laneRateTight(rate float64) string {
	if bare := laneRateBare(rate); bare != "" {
		return bare + laneRateUnit
	}
	return ""
}

// laneRateUnit is what a throughput is measured in, and it is a constant
// because the table writes it in a column head while the tail writes it on
// every row ([modelFacts]) — one spelling, in one place.
const laneRateUnit = "t/s"

// laneRateBare is a throughput with no unit on it, "58", for a row whose column
// head carries the unit instead.
func laneRateBare(rate float64) string {
	if rate <= 0 {
		return ""
	}
	return strconv.Itoa(int(math.Round(rate)))
}

// laneExactly is one lane's view by its whole name, false when nothing is
// believed about a lane by that name — which is every reading of an empty name.
// IT IS THE WHOLE NAME AND NEVER A PART OF ONE: a row's own numbers may not be
// found by the fragment a person half-remembers, because the wrong row's figures
// are worse than no figures.
func laneExactly(views []laneView, name string) (laneView, bool) {
	if strings.TrimSpace(name) == "" {
		return laneView{}, false
	}
	for _, view := range views {
		if strings.EqualFold(view.Name, name) {
			return view, view.Known
		}
	}
	return laneView{}, false
}

// laneUpMark is the one glyph on a model's row that says the number after it is
// a speed rather than a price or a window. It is drawn unconditionally, the way
// the middle dot every row is built out of is: a terminal that cannot draw it
// cannot draw the separator either.
const laneUpMark = "▲"

// ── ONE LANE'S ROW, RANKED ──────────────────────────────────────────────────
//
// laneRowPlan is one lane under an unfolded model as a row the fitter can lay
// out at any width (rowfit.go): the name, and what is believed about it in the
// order it would be given up.
//
//	cloudflare · 0.8s · 58 t/s · $1.32/M · no tools · 100% · ▁▂▁▃▁▂
//
// THE ORDER IS WHAT WOULD CHANGE THE ANSWER YOU GET:
//
//	1  the name        which machine this is — the primary, never given up
//	2  the first token the wait you will feel, and the number the whole fold
//	                   exists to compare
//	3  the throughput  how fast it writes once it has started
//	4  the price out   what an answer from it costs
//	5  the note        THE ONE THING WRONG WITH IT, and it outranks uptime
//	                   deliberately: `no tools` means the answer comes back
//	                   WRONG and `out ≤ 65k` means it comes back HALF, while a
//	                   99% uptime is a failure you find out about immediately
//	                   and retry through. A defect that is invisible in the
//	                   answer beats a defect that announces itself.
//	6  the uptime      how often it is there at all
//	7  the sparkline   our own last sightings — the most expensive cells on the
//	                   row and the least readable at a glance, so they are the
//	                   first thing a narrow frame spends
//
// EVERY FIELD DISAPPEARS WHEN IT IS UNKNOWN and the row stays readable without
// it, which is why the tail is joined rather than laid out in fixed columns
// past the name: a row of empty columns is a row that looks broken.
func laneRowPlan(view laneView) rowPlan {
	// THE NAME IS LOWERCASED, as every name this surface draws is. The wire
	// spells a lane however its vendor felt that morning — `Cloudflare`,
	// `DeepInfra`, `GMICloud` — and a column of three different capitalisation
	// styles is a column that reads as three different kinds of thing.
	plan := rowPlan{primary: strings.ToLower(view.Name)}
	uptime := rowField{}
	if view.Uptime > 0 {
		uptime = rowSay(strconv.Itoa(int(math.Round(view.Uptime))) + "%")
	}
	price := rowField{}
	if view.PriceOut > 0 {
		price = rowSay("$"+perMillion(view.PriceOut)+"/M", "$"+perMillion(view.PriceOut))
	}
	plan.fields = []rowField{
		rowSay(laneSecondsWord(view.TTFT)),
		rowSay(laneRateWord(view.Rate), laneRateTight(view.Rate)),
		price,
		rowSay(laneNote(view)),
		uptime,
		rowSay(barSpark(view.Sightings, 0, laneSightings)),
	}
	return plan
}

// laneCells is one provider's facts in [laneColumns] order, each in the
// spelling its column's head has already accounted for — the unit off the rate
// and the price, because a head says those once for the whole list.
//
// IT IS laneRowPlan's OWN READING with the labels taken off, which is the
// arrangement models.go keeps for the same two shapes: the table's cells and
// the ranked tail are one reading of one provider, dressed twice.
func laneCells(view laneView) []string {
	price := ""
	if view.PriceOut > 0 {
		price = "$" + perMillion(view.PriceOut)
	}
	uptime := ""
	if view.Uptime > 0 {
		uptime = strconv.Itoa(int(math.Round(view.Uptime))) + "%"
	}
	// THE ORDER IS [laneColumns]' ORDER and there is no second spelling of it:
	// this slice is indexed by column, so `up` before `note` here is the same
	// decision as `up` before `note` there, and the two cannot drift.
	return []string{
		laneSecondsWord(view.TTFT),
		laneRateBare(view.Rate),
		price,
		uptime,
		laneNote(view),
		barSpark(view.Sightings, 0, laneSightings),
	}
}

// laneRowText is that plan in room cells, as the row's two halves.
func laneRowText(view laneView, room int) (string, string) {
	return laneRowPlan(view).fit(room)
}

// laneMaxOutFloor is the output ceiling below which a lane is worth warning
// about. A lane that will not write more than this cuts a long answer off, and
// the answer that comes back looks like the model gave up rather than like the
// machine did.
const laneMaxOutFloor = 131_072

// poorlyServing reports whether this lane's ANSWERS have been failing, aged to
// this moment the way the chooser ages them.
//
// IT IS THE GATE'S OWN QUESTION AND NOT A SECOND OPINION. The frontier drops a
// lane when the ninetieth-percentile bound on its quality falls under the role's
// need, so this asks exactly that, of the role a conversation runs in — and a
// lane the surface says nothing about is one the chooser would still send to.
//
// The two live in different packages and must not drift, which is why the need
// is read off the role's own facts rather than written down here.
func poorlyServing(belief lane.Belief, now time.Time) bool {
	quality := belief.Quality
	if !quality.Known() {
		return false
	}
	if since := now.Sub(belief.QualityAt); since > 0 && !belief.QualityAt.IsZero() {
		quality = quality.Toward(lane.QualityPrior(belief.Facts.Quant), since, lane.QualityHalfLife)
	}
	return quality.Upper(laneQualityZ) < lane.RoleTalk.Facts().QualityNeed
}

// laneNote is the ONE thing worth saying about a lane past its numbers, and it
// is one thing on purpose: a row carrying four warnings is a row nobody reads.
//
// The order is what would ruin the answer first. A lane whose replies cannot be
// used gives NO answer at all; one that drops the tool call gives a WRONG
// answer; one that truncates gives half an answer; one serving four-bit weights
// gives a worse answer; a tail only makes you wait.
func laneNote(view laneView) string {
	switch {
	case view.Poor:
		return "bad replies"
	case !view.Tools:
		return "no tools"
	case view.MaxOut > 0 && view.MaxOut < laneMaxOutFloor:
		return "out ≤ " + contextWord(view.MaxOut)
	case laneQuantRank(view.Quant) > 0 && laneQuantRank(view.Quant) < laneQuantRank("fp8"):
		return strings.ToLower(view.Quant)
	case view.Tail > 0:
		return "tail " + strconv.Itoa(int(math.Round(view.Tail))) + "s"
	}
	return ""
}

// laneQuantRank orders the weight precisions a sheet publishes, coarsest first,
// so [laneNote] can ask whether a lane is serving BELOW the floor without
// comparing the words themselves. Zero is "nobody said", and no row is judged by
// it: a lane that published no precision has not published a bad one.
func laneQuantRank(quant string) int {
	switch strings.ToLower(strings.TrimSpace(quant)) {
	case "fp4", "int4", "nf4":
		return 1
	case "fp6":
		return 2
	case "fp8", "int8":
		return 3
	case "bf16", "fp16", "float16":
		return 4
	case "fp32", "float32":
		return 5
	}
	return 0
}

// ── PINNING ─────────────────────────────────────────────────────────────────

// pinLane writes one model's lane down and says so. It is the one road every
// pin takes — enter on a lane row in the picker, and `/model @cloudflare` —
// for [app.switchModel]'s reason: a second write site is the drift where the
// picker persisted and the command did not.
//
// A SURFACE WITH NOWHERE TO WRITE STILL SAYS WHAT IT DID, and what it did is
// nothing. Over a connection the profile is the far machine's, so the note is
// the honest half of the truth rather than a claim about a file this laptop
// never has.
//
// AND "OVER A CONNECTION" IS [app.hosted] AND NOT AN EMPTY PROFILE PATH. These
// three sites read the empty string as "no profile", which is the one thing it
// has never meant in internal/config: `CODEAF_PROFILE_DIR` unset is the
// ORDINARY launch, and every reader and writer in that package resolves an
// empty directory to ~/.codeaf. So a pin from the picker, and `/model
// @cloudflare`, refused to write on every machine nobody had exported that
// variable on — while the settings row beside them wrote fine, because it goes
// through the registry, which passes the same empty string down. One fact, two
// spellings of "where does this land", and only one of them was right.
func (a *app) pinLane(model, name string) {
	slot := laneSlotFor(model)
	if a.hosted() {
		a.note(a.host + " owns the host · change it on that machine")
		return
	}
	if err := config.SetLane(a.profileDir, slot, name); err != nil {
		a.note(err.Error())
		return
	}
	_ = config.SetLaneBorrow(a.profileDir, slot, false)
	a.laneRowChanged()
	a.noteFacts("host · "+strings.ToLower(name), name)
	a.touch()
}

// clearLanePin puts the row back to auto.
func (a *app) clearLanePin(model string) {
	if a.hosted() {
		a.note(a.host + " owns the host · change it on that machine")
		return
	}
	if err := config.SetLane(a.profileDir, laneSlotFor(model), config.LaneAuto); err != nil {
		a.note(err.Error())
		return
	}
	a.laneRowChanged()
	a.noteFacts("host · auto", config.LaneAuto)
	a.touch()
}

// setLaneRouterOnly is enter on the `openrouter` row: this model asks for no
// lane at all and lets the router balance on price. It is a lane answer and NOT
// the routing row — [config.KeyRouting] is about every request this session
// makes, and this is about the machines behind one model.
func (a *app) setLaneRouterOnly(model string) {
	if a.hosted() {
		a.note(a.host + " owns the host · change it on that machine")
		return
	}
	if err := config.SetLane(a.profileDir, laneSlotFor(model), config.LaneOpenRouter); err != nil {
		a.note(err.Error())
		return
	}
	a.laneRowChanged()
	a.noteFacts("host · openrouter", config.LaneOpenRouter)
	a.touch()
}

// laneRowChanged hands the row this surface just wrote to the layer that sends
// requests, so the very next turn goes where the person said.
//
// IT IS THE WRITE AND NOT A READ, and that is the whole design of the seam
// (internal/provider's lanepin.go). The transport may not read a settings file
// in front of somebody's first token, so a pin that only took effect at the
// next launch was the alternative — and a picker that says `pinned` over a
// conversation that is still going somewhere else is a surface lying about a
// setting a person is looking at.
//
// A SURFACE OVER A CONNECTION WRITES NOTHING HERE, because it has already
// written nothing at all: the pin sites above refuse a hosted session, and the
// far machine's own launch resolved its own row. It is [app.hosted] and not an
// empty profile path for the reason stated over [app.pinLane] — an empty path
// is the ordinary launch, not the absence of one.
//
// AND IT IS THE PERSON'S OWN ENTRANCE AND NOT THE RESOLVERS'
// ([provider.RepinLane], not [provider.SetLanePin]). Every path that reaches
// here is somebody's act — enter on a lane in the picker, `/model @cloudflare`,
// `/model auto`, the `lane` row in the settings panel — so a refusal the wire
// collected earlier is forgotten whatever the row now says, INCLUDING when they
// have chosen the machine they had already chosen. Re-picking coreweave is a
// row that did not change and an instruction that did, and "pinning again puts
// it straight back" is what the manual promises them (issue #456).
func (a *app) laneRowChanged() {
	if a.hosted() {
		return
	}
	slot := laneSlotFor(a.model)
	provider.RepinLane(config.LanePinAt(a.profileDir, slot))
}

// routingRowChanged is [app.laneRowChanged] for the row ABOVE the lane: the
// routing word this surface just wrote, handed to the transport and read back
// into everything on this side that explains it.
//
// A ROUTING CHANGE LANDS ON THE NEXT MESSAGE AND NOT THE NEXT SESSION. The row
// went to disk and nowhere else, and the session's own clients had been handed
// the word that was there at launch — so a person cycled `routing` to `simple`,
// watched the row say so, and went on being routed by the old word until they
// relaunched, with the `lane` row beside it still explaining `auto` in the old
// word's terms on the same screen (issue #1022). The transport's own knob is
// process-wide for exactly this ([config.InstallRoutingRow]), the conversation's
// clients read it live, and the two fields below are what this surface says
// about it.
//
// THE FIELDS ARE RE-READ AND NOT ASSUMED. [app.routing] is the row RESOLVED —
// an unwritten or unknown word is the shipped row — so the surface asks the one
// door that resolves it rather than keeping the raw word it just applied.
func (a *app) routingRowChanged() {
	if a.hosted() {
		return
	}
	config.InstallRoutingRow(a.profileDir)
	a.routing = config.RoutingAt(a.profileDir)
	// The panel holds its own copy for the reason it holds the session's model
	// (settings.go's [sheet.routing]), and the copy is what the `lane` row's
	// explanation is drawn from — so a row that changed on this frame must not
	// leave the sentence under it describing the row before it.
	a.sheet.routing = a.routing
}

// ── THE PIN, WRITTEN ON THE MODEL ───────────────────────────────────────────

// laneAtSign is what joins a model to the machine it is pinned to. It is the `@`
// of `/model @cloudflare`, so what a person reads on the chrome is what they
// would type to put it there. The picker's filter box once took the same `@` and
// no longer does — it searches names only ([picker.rank]) — which leaves the
// command as the one place a person types this character.
const laneAtSign = "@"

// pinnedNow is the machine this conversation's requests are held to, as the
// transport will act on it ([laneInForce]), lowercased the way every lane name
// this surface draws is — and empty on `auto`, on `openrouter`, on a pin the
// wire has retired for this model, under routing `off` (which sends no lane at
// all, [app.routingOff]), and over a connection, where the pin in force is the
// far machine's and this process cannot see it.
func (a *app) pinnedNow() string {
	return strings.ToLower(a.laneForceNow().name)
}

// laneForceNow is [laneInForce] for the model this conversation is on, with the
// four states where this surface may name no machine at all answered first: a
// session over a connection (the pin is the far machine's and this process
// cannot see it), routing `off` (no lane is sent), no model, and a model served
// by a connected service direct rather than through the router.
func (a *app) laneForceNow() laneForce {
	if a.hosted() || a.routingOff() || a.model == "" || a.modelIsDirect(a.model) {
		return laneForce{}
	}
	return laneInForce(a.model)
}

// modelIdentity keeps the complete model address on home and conversation seams.
// Connected services qualify their own models; without a catalog the stored id
// still keeps its organization prefix, so the two surfaces cannot disagree.
func (a *app) modelIdentity(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || a.sources.Empty() {
		return id
	}
	service, bare := a.sources.For(id)
	return service.Qualify(bare)
}

// modelWord is THE MODEL AS THE CHROME NAMES IT: its complete address,
// and — while a lane is pinned — `@` and that lane:
// `deepseek/deepseek-v4-flash@cloudflare`.
//
// A PIN IS AN INSTRUCTION THAT CHANGES EVERY FUTURE REQUEST, and until this the
// chrome never said it. The picker's row said `via inception` only while the
// picker was open, the status rider said `via …` only for ten minutes after an
// answer, and the frame's head read `mercury-2.5 · ⠿ auto` — whose `auto` is the
// thinking rung, which the owner read as "lane: auto" and concluded the pin had
// failed (2026-09-10). So the pin rides the one word every place that names the
// model already draws: the seam, the status row's identity and the phone deck's
// chip all take it from here, and none of them spells it.
//
// On `auto` and `openrouter` it adds nothing — the emptiness law: a choice left
// to the router is the unremarkable state and says no word.
func (a *app) modelWord() string { return a.modelWordAt("") }

// modelWordAt is [app.modelWord] with a reasoning level spelled onto the id —
// `deepseek/deepseek-v4-flash:high@cloudflare` — which is how the phone deck's chip names
// the conversation's model (statusdeck.go's [app.deckModelRow]). An empty level
// is the word [app.modelWord] draws.
//
// THE LEVEL IS PASSED IN, NEVER LENT THROUGH [app.model]. It used to be spliced
// onto that field for the length of the status row's draw (view.go's
// [app.statusRow]), and every news desk asked inside that draw then asked for
// the model by a name nothing is filed under — which is how a person who had set
// a level lost the live rate from the right edge of the row.
func (a *app) modelWordAt(level string) string {
	model := a.modelIdentity(a.model)
	if model == "" {
		return ""
	}
	if level != "" {
		model += ":" + level
	}
	if pin := a.pinnedNow(); pin != "" {
		return model + laneAtSign + pin
	}
	return model
}

// openPickerFromChip is a press on the model's name wherever the chrome draws
// it. It is /model's list; and while a lane is pinned it opens with the model's
// fold already open and the cursor on that lane, because the name pressed was
// `model@lane` — the door to the provider is the door to the model, and a press
// on a word that names a machine should land on that machine.
func (a *app) openPickerFromChip() {
	a.openPicker()
	if a.pinnedNow() == "" {
		return
	}
	// THE FOLD HAS TO BE THE ONE THE CHIP NAMES, for [app.openLaneList]'s
	// reason: a model the list does not carry leaves the cursor on row zero,
	// and unfolding whatever sorted first would open somebody else's machines.
	if chosen, ok := a.pick.choice(); ok && chosen.ID == a.model {
		a.pick.unfoldHere()
	}
}

// ── THE STATUS LINE ─────────────────────────────────────────────────────────

// laneRider is the served segment when the lane layer has something to say, and
// empty when it has not — in which case render.go keeps the rider it has always
// drawn from the velocity ledger.
//
// THREE STATES AND NO FOURTH:
//
//	via cloudflare · 0.6s · 61 t/s     an ordinary answer, and who wrote it
//	slow · trying coreweave…           a lane is late and a rescue is in flight
//	refused · trying nextbit…          a lane said no and a rescue is in flight
//	coreweave refused                  the rescue itself was refused
//	via coreweave · rescued            it worked, for this answer only
//	coreweave cannot serve this model; routing on auto for this model until you pin again
//	                                   the machine you pinned said no, so the pin is retired
//
// AND THE LAST OF THOSE IS SAID IN THE CONVERSATION TOO, which is where a
// person really reads it. This row is outranked by the phase clock for as long
// as a request is in flight ([app.servedRiderAt]) and holds only the NEWEST
// news about a model, which the answer's own arrival overwrites — so a sentence
// that only ever lived here was a sentence nobody saw in a measured run of
// eight cells. The transport says it as a note as well, once
// (internal/provider's lanepin.go), and that one stays.
//
// THE SURFACE SAYS WHAT THE WIRE SAID. "slow" is a claim about a wait and
// "refused" is a claim about a machine, and for a whole measured run this line
// said the first about the second: a 404 meaning `your request's provider.only
// preference permits only: coreweave` was drawn as `· slow · trying nextbit…`
// (issue #266). Neither word is decided here — both are carried on the news
// from the layer that read the refusal — and the line says slow only while
// something is already being done about it, because a status line that called
// an answer slow and then sat there would be a complaint.
//
// AND A CLAIM THAT HAS STOPPED BEING TRUE IS TAKEN BACK. The fourth state is a
// retraction: the rescue this line promised has itself been refused, so the
// promise goes and what is left is the fact.
//
// AND ALL THREE ARE ABOUT AN ANSWER THAT IS FINISHED, which is why the phase
// clock takes the segment away from them while a request is actually in flight
// (render.go's [app.servedRider]). These read the past tense; that reads the
// present one.
// timed says whether the answer's own figures — the wait before its first word
// and, while a turn runs, how fast it was written — ride after the machine's
// name. The sheet's `served` row wants them (statusdeck.go); THE SEAM DOES NOT.
// On 2026-09-10 the owner read `via relace · 1.3s · 79 t/s` on the seam while
// an answer was being written and took the figure for the live rate, which by
// then stood at the right edge of the status row as `34 tok/s` (render.go's
// [app.liveRiderAt]) — one frame, two rates, and the one beside the model was
// the LAST answer's average. Who served is attribution and belongs beside the
// model; how fast is a claim about now and has one place on the frame.
//
// IT IS THE SHEET'S READING, AND THE SHEET KEEPS THE OLD SUPPRESSION: the row
// above `served` there is the model's whole routing address, vendor and all, so
// a machine the address already names is not said twice. The seam asks
// [app.talkLaneRider] instead, which never suppresses.
func (a *app) laneRider(timed bool) string {
	return a.laneRiderFor(a.talkLaneStory(), a.model, a.model, "", riderVia, timed, a.state == stateWorking)
}

// riderStyle is how a lane rider is spelled.
type riderStyle uint8

const (
	// riderVia is ` · via relace`: a segment of its own, on the sheet's
	// `served` row and the phone deck's identity, said shorter when the row
	// is tight.
	riderVia riderStyle = iota
	// riderBeside is ` (relace)`: the machine written onto the model's own
	// cell on the seam — `deepseek-v4-flash (relace)` — whole or nothing, and
	// never aged out. The owner's ruling of 2026-09-17: the model and the
	// machine answering for it are one fact, read as one word, and the last
	// machine to answer stays named until another does.
	riderBeside
)

// riderWords spells a machine in one style, with an optional tail inside the
// same cell (` · rescued`).
func riderWords(style riderStyle, machine, tail string) string {
	if style == riderBeside {
		return " (" + machine + tail + ")"
	}
	return " · via " + machine + tail
}

// talkLaneRider is the conversation's rider ON THE SEAM: who is answering, with
// no figures, and WHOEVER SERVED.
//
// live is the machine the request in flight has named ([PhaseNews.Lane]), and
// it is asked for here because the sighting is only posted when a whole step
// has FINISHED. So the first answer in a conversation, and a follow-up sent
// after the last sighting had aged out ([servedWindow]), drew no `via` at all
// for as long as the answer was being written — while the phase clock already
// knew exactly who was writing it. The owner's ruling is that `via` is always on
// the seam; a sighting that is not posted yet is not a reason to break it.
func (a *app) talkLaneRider() string {
	live := ""
	if news, ok := a.livePhase(); ok {
		live = news.Lane
	}
	return a.laneRiderFor(a.talkLaneStory(), a.model, "", live, riderBeside, false, a.state == stateWorking)
}

// roomLaneRider is that rider for THE OPEN ROOM'S NODE: which machine answered
// the node's own last request, beside the node's own model.
//
// IT IS ATTRIBUTION ONLY AND CARRIES NO FIGURES, which is the seam's own rule
// said in the one place a node's model is drawn ([app.identityParts]): who
// served rides the model, and how fast it is writing right now has one home on
// the frame — the right edge ([app.liveRiderAt]).
//
// It was impossible before the subject existed. The desk was keyed by model, so
// the only sighting this row could find was whichever answer on that model id
// landed last — the conversation's, most often — and a node's room drawing the
// conversation's machine is exactly the lie [app.roomModelWord] refused to
// tell by drawing nothing at all.
//
// AND IT IS DRAWN WHOEVER SERVED, for the seam's reason ([app.modelRiderAt]): the
// room's chip spells the node's model as its basename after a `task` lead, so
// the vendor half of the id is not on the screen, and a rider that vanished
// whenever the vendor served its own model read as the sighting having been
// lost. The node's live phase names the machine while its first answer is still
// being written, exactly as the conversation's does.
func (a *app) roomLaneRider() string {
	subject := a.roomSubject()
	if subject == "" || a.roomNode() == nil {
		return ""
	}
	news, working := a.roomPhase()
	story, _ := laneStoryFor(subject)
	return a.laneRiderFor(story, a.roomNode().model, "", news.Lane, riderBeside, false, working)
}

// laneRiderFor is that rider for one window's story: the piece of work the
// window asking is a window onto (phase.go's law and [newsDeskKeys]).
//
// model is the id the work runs on, asked one thing: whether it is a directly
// connected service's, which has one road and no machine to name. It is
// ALWAYS a real id: until 2026-09-17 the seam and a room asked this of the
// empty string they pass as named, which [modelsource.Set.For] answers with
// the default service — right by accident, and wrong the day the default is
// a direct one.
//
// named is the id the row beside it spells IN FULL, read for one thing only —
// the rule that a machine the id already carries is not said twice. Only the
// sheet passes one ([app.laneRider]); the seam and a room spell a basename, so
// the vendor is not on the screen and they pass nothing (the owner's ruling of
// 2026-09-09: the machine always on the seam).
//
// live is the machine the request in flight has named, or "" — see
// [app.talkLaneRider] for why it outranks the last answer's sighting.
//
// style is the spelling ([riderStyle]): the sheet's segment ages out with
// [servedWindow], because a figure about an answer that finished ten minutes
// ago is not a reading; the seam's word beside the model does not, because
// who answered last is a fact until somebody else answers.
//
// working says whether the work this rider is about is RUNNING, because that is
// what decides whether the rate may ride along and whether a rescue may still
// be promised. It is the caller's answer and not `a.state`: the session's state
// is the conversation's liveness, and a window onto a node must not go quiet
// because the conversation it was launched from is idle.
func (a *app) laneRiderFor(story laneStory, model, named, live string, style riderStyle, timed, working bool) string {
	// A DIRECTLY CONNECTED SERVICE HAS ONE ROAD, so there is no machine to name
	// and the lane desk holds none for it. The sighting this would otherwise
	// find is the DEFAULT service's, matched on the full model id the sheet row
	// names — which is how an ollama row came to say `via akashml`.
	if a.modelIsDirect(model) {
		return ""
	}
	now := a.now()
	if story.hasRescue && now.Sub(story.rescue.At) <= servedWindow {
		if line := rescueRider(story.rescue, working); line != "" {
			return line
		}
	}
	// WHO IS WRITING NOW OUTRANKS WHO WROTE LAST, which is the tense law the
	// served rider has always kept ([app.servedRider]): a phase is this request,
	// a sighting is the last one.
	if live = strings.ToLower(strings.TrimSpace(live)); live != "" {
		if named == "" || !strings.Contains(strings.ToLower(named), live) {
			return riderWords(style, live, "")
		}
	}
	news := story.seen
	if !story.hasSeen || (style == riderVia && now.Sub(news.At) > servedWindow) {
		return ""
	}
	if news.rescued() {
		return riderWords(style, strings.ToLower(news.Winner), " · rescued")
	}
	if news.Lane == "" {
		return ""
	}
	// AND WHERE THE ROW SPELLS THE WHOLE ID, A LANE IT ALREADY NAMES IS NOT SAID
	// TWICE — "openai/gpt-4.1 · via openai" spends a cell a frame on a word the
	// reader already has. That is the sheet alone now (see named above). A
	// rescue is exempt above, because THAT is news whoever the vendor is.
	served := strings.ToLower(news.Lane)
	if named != "" && strings.Contains(strings.ToLower(named), served) {
		return ""
	}
	rider := riderWords(style, served, "")
	if !timed {
		return rider
	}
	if word := laneSecondsWord(news.TTFT.Seconds()); word != "" {
		rider += " · " + word
	}
	// THE RATE RIDES ONLY WHILE THE WORK IS RUNNING, exactly as it does on the
	// rider this one extends ([app.servedRider]): who served is attribution and
	// stays; how fast they were writing is a claim about now.
	if word := laneRateWord(news.Rate); word != "" && working {
		rider += " · " + word
	}
	return rider
}

// rescueRider is what a rescue's own news says, or "" when it has nothing to
// say and the sighting underneath should speak instead.
//
// A PROMISE IS KEPT ONLY WHILE THE WORK IS RUNNING. `trying coreweave…` is a
// claim about the present tense, and a turn that was interrupted or errored
// while the rescue was out never posts the answer that would have cleared it —
// so without this the line stood saying `slow · trying coreweave…` over an idle
// conversation until the ten-minute window aged it out.
//
// AND A RESCUE THAT FAILED WITHOUT BEING REFUSED SAYS NOTHING, which hands the
// row back to the last sighting rather than blanking it: the machine that
// answered last is still the machine that answered last.
func rescueRider(news LaneNews, working bool) string {
	if news.Trying && news.Alt != "" {
		if !working {
			return ""
		}
		// BOTH SENTENCES ARE WRITTEN OUT. Composing them from a word would save
		// a line and cost the gate that keeps this surface's vocabulary honest:
		// internal/e2e's tuiwords table reads these sources back for the exact
		// strings its tmux suite waits for, and a sentence assembled at runtime
		// is a sentence that table cannot find.
		if news.Reason == provider.RescueRefused {
			return " · refused · trying " + strings.ToLower(news.Alt) + "…"
		}
		return " · slow · trying " + strings.ToLower(news.Alt) + "…"
	}
	// THE PIN THAT WAS RETIRED (issue #456). It is neither a rescue in flight
	// nor a retraction of one: it is the row a person wrote changing what it
	// means for the rest of the run, and the only sentence on this line that
	// tells somebody what will happen NEXT. So it is written out whole — the
	// machine as they spelled it when they pinned it, what the wire said about
	// it, and how to get their pin back — rather than compressed into two words
	// like the states above it, which are all about the answer in front of
	// them. The words are the transport's own, so that this row and the note it
	// leaves in the conversation cannot come to disagree.
	if news.Reason == provider.RescueRetired && news.Alt != "" {
		return " · " + provider.RetiredPinLine(news.Alt)
	}
	// THE RETRACTION. The machine this line was promising has been refused, so
	// the promise comes off and the only thing left worth saying is what it did.
	if news.Failed && news.Alt != "" && news.Reason == provider.RescueRefused {
		return " · " + strings.ToLower(news.Alt) + " refused"
	}
	return ""
}

// ── THE KEYSTROKE ───────────────────────────────────────────────────────────

// typingAgent is the OPTIONAL half of [Agent]: an engine that can be told a
// person has started writing.
//
// It is a separate interface rather than a method on [Agent] for the reason the
// task door is one (app.go's taskCommandAgent): probing is a thing only a
// session with a real transport under it has, and a door that does not offer it
// — a connection to another machine, a test double — makes the capability
// ABSENT rather than present and failing.
type typingAgent interface{ Typing() }

// laneTyping tells the engine somebody is writing, and does nothing at all for
// a door that cannot hear it. See [app.key] for why it is called on every
// character rather than on the first.
func (a *app) laneTyping() {
	typing, ok := a.agent.(typingAgent)
	if !ok {
		return
	}
	typing.Typing()
}
