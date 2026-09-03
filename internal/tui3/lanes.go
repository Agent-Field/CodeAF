package tui3

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
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
//     speed on its row, no lanes to unfold and no `via`. A blank row is
//     readable; an invented number is a router steering on a measurement
//     nobody took (design-law-v2 §16).
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

// rescued reports whether this answer was finished somewhere other than where
// it started. It is the only reading of "rescued" the surface has, and it is
// deliberately strict: a hedge that lost is not a rescue, it is a measurement.
func (n LaneNews) rescued() bool {
	return n.Hedged && n.Winner != "" && !strings.EqualFold(n.Winner, n.Lane)
}

// laneNewsMsg wakes the loop so a frame is drawn for news that arrived from
// somewhere other than a keystroke. It carries nothing — [PostLaneNews] has
// already put the news on the desk — because a message that carried the news
// would be a second copy of it, arriving after the first.
type laneNewsMsg struct{}

// laneSightings is the most of our own answers one lane's sparkline draws. Eight
// because that is what fits in the tail of a row a person is scanning, and
// because a sparkline longer than the eye takes in at once is a chart nobody is
// reading anyway.
const laneSightings = 8

// laneDesk is what this process has been TOLD, as opposed to what it believes.
//
// It is two small things: the latest news per model, which is what the status
// line draws, and a ring of our own recent first-token waits per lane, which is
// what the sparkline draws. Neither is a belief and neither pretends to be —
// the ledger owns believing, and it publishes no history, so the ring lives
// here rather than being re-derived from a posterior that has forgotten it.
type laneDesk struct {
	mu     sync.RWMutex
	latest map[string]LaneNews
	rings  map[string][]int
}

var desk = laneDesk{latest: map[string]LaneNews{}, rings: map[string][]int{}}

// PostLaneNews is how the layer that sent an answer tells the surface what the
// lanes did. It is safe from any goroutine, it never blocks on a draw, and it
// keeps nothing about an answer that could not say which lane served it: a
// sighting credited to nobody is a fact about a machine that was not involved.
func PostLaneNews(news LaneNews) {
	news.Model = strings.TrimSpace(news.Model)
	news.Lane = strings.TrimSpace(news.Lane)
	if news.Model == "" {
		return
	}
	if news.At.IsZero() {
		news.At = time.Now()
	}
	desk.mu.Lock()
	defer desk.mu.Unlock()
	desk.latest[news.Model] = news
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

// laneNewsFor is the latest news about one model, false when none has arrived.
func laneNewsFor(model string) (LaneNews, bool) {
	desk.mu.RLock()
	defer desk.mu.RUnlock()
	news, ok := desk.latest[strings.TrimSpace(model)]
	return news, ok
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
		// The tail is the p99 in seconds, and it is only a tail when it is far
		// enough past the median to be a different experience: five times.
		if p99 := ttft.Quantile(laneTailZ) / 1000; view.TTFT > 0 && p99 > laneTailRatio*view.TTFT {
			view.Tail = p99
		}
		views = append(views, view)
	}
	sortLanes(views)
	return views
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

// laneTalkTokens is the answer a talk turn is scored against: four hundred
// tokens a person reads, and nothing they do not.
const laneTalkTokens = 400

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

// laneNow is the lane a request for this model would go to, and false when
// nothing here can say. THE RULE IS THREE RUNGS AND IT IS STATED ON PURPOSE,
// because a `via` on a picker row that disagreed with the `via` on the status
// line would be two answers to one question:
//
//  1. A PIN WINS. If the slot's row names a machine, that is the machine, and
//     the surface never second-guesses a person who has chosen.
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
	if name, pinned := config.LanePinned(a.profileDir, laneSlotFor(model)); pinned {
		return name, true
	}
	name := laneAuto(model, views, a.now())
	return name, name != ""
}

// laneAuto is rungs two and three of that rule — the chooser, then the belief —
// without the pin, which needs a profile to read and is therefore the app's.
// It is a free function because a picker row is drawn from places that hold no
// session (the settings panel's slot rows, the composer's), and a row that said
// nothing there would be the same list telling two stories.
func laneAuto(model string, views []laneView, now time.Time) string {
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
// has no machine to choose" — a task's model, a role, a media slot.
func (a *app) armLanes(p *picker, slot string) {
	if slot == "" {
		return
	}
	p.laneSlot = slot
	p.pin = config.LaneAt(a.profileDir, slot)
	p.guard = config.LaneGuardAt(a.profileDir)
	p.ascii = a.pal.ascii
}

// applyLaneChoice is enter on a row INSIDE an open fold: the lane the cursor is
// on, written for the model that fold belongs to.
//
// It is one function because the fold now has two doors — /model
// (palette.go's [app.pickerKey]) and the settings panel's model and lane rows
// (settings.go's [app.sheetSelectKey]) — and a pin written two ways is a pin
// that drifts the first time one of the two is fixed.
func (a *app) applyLaneChoice(model string, row pickRow, lanes []laneView) {
	switch {
	case row.lane == laneAutoAt:
		a.clearLanePin(model)
	case row.lane == laneRoutAt:
		a.setLaneRouterOnly(model)
	case row.lane >= 0 && row.lane < len(lanes):
		a.pinLane(model, lanes[row.lane].Name)
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
func laneSpeedWord(views []laneView, now string) string {
	best, ok := laneShown(views, now)
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
func laneShown(views []laneView, now string) (laneView, bool) {
	if best, ok := laneExactly(views, now); ok {
		return best, true
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
	if rate <= 0 {
		return ""
	}
	return strconv.Itoa(int(math.Round(rate))) + "t/s"
}

// laneExactly is one lane's view by its whole name, false when nothing is
// believed about a lane by that name — which is every reading of an empty name.
// It is the strict sibling of [laneNamed], which matches the part of a name a
// person half-remembers; a row's own numbers may not be found that loosely.
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

// laneWhy is the dim line under the cursor: what this lane is, in a sentence,
// with where the claim came from.
//
//	cloudflare: first token 0.8s, steady 58 t/s, no tail — from the sheet + your last 12 answers
//
// IT SAYS WHERE THE NUMBER CAME FROM because that is the difference between a
// figure a person can argue with and one they have to trust. "The sheet" is the
// public prior everybody gets; "your last n answers" is ours, and when there
// are none it says the sheet alone rather than claiming a history.
func laneWhy(view laneView) string {
	if !view.Known {
		return ""
	}
	parts := make([]string, 0, 3)
	if word := laneSecondsWord(view.TTFT); word != "" {
		parts = append(parts, "first token "+word)
	}
	if word := laneRateWord(view.Rate); word != "" {
		parts = append(parts, "steady "+word)
	}
	if view.Tail == 0 {
		parts = append(parts, "no tail")
	}
	line := strings.ToLower(view.Name) + ": " + strings.Join(parts, ", ")
	switch n := len(view.Sightings); {
	case n == 1:
		return line + " — from the sheet + your last answer"
	case n > 1:
		return line + " — from the sheet + your last " + strconv.Itoa(n) + " answers"
	}
	return line + " — from the sheet"
}

// ── THE FILTER GRAMMAR ──────────────────────────────────────────────────────
//
// The picker's box has always been a fuzzy search over model ids, and it stays
// one: EVERY TOKEN THAT DOES NOT PARSE AS ONE OF THESE FALLS THROUGH TO
// [picker.rank] UNCHANGED, so nothing a person types today ranks differently
// tomorrow. What is added is a handful of tokens that are not names at all —
// they are questions about the machines behind the name, and a fuzzy search
// over ids can never answer them.
//
// A term either KEEPS rows or ORDERS them, never both. Keeping is ANDed with
// every other token, which is the only thing typing more can sensibly do; the
// two ordering words come last and win over the text score, because a person
// who typed `fast` asked for an order out loud.

// laneTermKind says what a parsed token does.
type laneTermKind uint8

const (
	// termLane keeps models served by a lane whose name carries the word, and
	// unfolds the first of them with that lane at the top.
	termLane laneTermKind = iota
	// termTTFT keeps models whose best lane starts within a bound.
	termTTFT
	// termRate keeps models whose best lane writes at least this fast.
	termRate
	// termPrice keeps models whose best lane charges under this per million
	// output tokens.
	termPrice
	// termQuant keeps models with a lane serving at least this precision.
	termQuant
	// termTools keeps models with a lane that honours a tool call.
	termTools
	// termSees and termDraws are the model's own modalities, which the row
	// already says — they are here so that one grammar answers the whole row.
	termSees
	termDraws
	// termFast and termCheap order what is left.
	termFast
	termCheap
)

// laneTerm is one parsed token.
type laneTerm struct {
	kind  laneTermKind
	word  string
	value float64
}

// parseLaneTerm reads one lowercased token, and reports false for everything
// that is not one of the forms above — which is how an ordinary search word
// reaches the ranking it has always reached.
func parseLaneTerm(token string) (laneTerm, bool) {
	switch token {
	case "":
		return laneTerm{}, false
	case "tools":
		return laneTerm{kind: termTools}, true
	case "sees":
		return laneTerm{kind: termSees}, true
	case "draws":
		return laneTerm{kind: termDraws}, true
	case "fast":
		return laneTerm{kind: termFast}, true
	case "cheap":
		return laneTerm{kind: termCheap}, true
	case "fp4", "fp8", "int8", "bf16", "fp16", "fp32":
		return laneTerm{kind: termQuant, word: token}, true
	}
	if strings.HasPrefix(token, "@") {
		if name := strings.TrimPrefix(token, "@"); name != "" {
			return laneTerm{kind: termLane, word: name}, true
		}
		return laneTerm{}, false
	}
	// `$<0.3` — the dollar leads because that is how a person writes a price,
	// and the comparison follows it.
	if rest, ok := strings.CutPrefix(token, "$<"); ok {
		if value, err := strconv.ParseFloat(rest, 64); err == nil && value > 0 {
			return laneTerm{kind: termPrice, value: value}, true
		}
		return laneTerm{}, false
	}
	// `<1s` and `<800ms` — a bound on the wait before the first word, kept in
	// SECONDS because that is the unit the row is drawn in.
	if rest, ok := strings.CutPrefix(token, "<"); ok {
		if seconds, ok := parseLaneSeconds(rest); ok {
			return laneTerm{kind: termTTFT, value: seconds}, true
		}
		return laneTerm{}, false
	}
	// `>50t/s` — a floor under how fast it writes.
	if rest, ok := strings.CutPrefix(token, ">"); ok {
		rest = strings.TrimSuffix(strings.TrimSuffix(rest, "t/s"), "tok/s")
		if value, err := strconv.ParseFloat(rest, 64); err == nil && value > 0 {
			return laneTerm{kind: termRate, value: value}, true
		}
		return laneTerm{}, false
	}
	return laneTerm{}, false
}

// parseLaneSeconds reads `1s`, `800ms` or a bare number of seconds.
func parseLaneSeconds(text string) (float64, bool) {
	switch {
	case strings.HasSuffix(text, "ms"):
		value, err := strconv.ParseFloat(strings.TrimSuffix(text, "ms"), 64)
		if err != nil || value <= 0 {
			return 0, false
		}
		return value / 1000, true
	case strings.HasSuffix(text, "s"):
		text = strings.TrimSuffix(text, "s")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

// laneQuantRank orders the weight precisions a sheet publishes, coarsest first.
// Zero is "nobody said", which no `fp8` filter can satisfy and no row is judged
// by: a lane that published no precision has not published a bad one.
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

// keeps reports whether one model survives this term. Terms that only order
// keep everything.
func (t laneTerm) keeps(model Model, views []laneView) bool {
	best, known := bestLane(views)
	switch t.kind {
	case termLane:
		_, found := laneNamed(views, t.word)
		return found
	case termTTFT:
		return known && best.TTFT > 0 && best.TTFT <= t.value
	case termRate:
		return known && best.Rate >= t.value
	case termPrice:
		// THE CHEAPEST LANE ANSWERS THIS ONE, not the best. `$<0.3` is a
		// question about what this model CAN be served for — the same shape as
		// `tools` and `fp8` — while `<1s` and `>50t/s` are about the lane you
		// would actually land on.
		for _, view := range views {
			if view.PriceOut > 0 && view.PriceOut*1_000_000 < t.value {
				return true
			}
		}
		return false
	case termQuant:
		want := laneQuantRank(t.word)
		for _, view := range views {
			if laneQuantRank(view.Quant) >= want {
				return true
			}
		}
		return false
	case termTools:
		for _, view := range views {
			if view.Tools {
				return true
			}
		}
		return false
	case termSees:
		return hasModality(model.Input, "image")
	case termDraws:
		return hasModality(model.Output, "image")
	}
	return true
}

// laneNamed finds the first lane whose name CARRIES the word, so `@cloud` finds
// cloudflare — a person filtering types the part they remember, exactly as they
// do for a model id.
func laneNamed(views []laneView, word string) (laneView, bool) {
	for _, view := range views {
		if strings.Contains(strings.ToLower(view.Name), strings.ToLower(word)) {
			return view, true
		}
	}
	return laneView{}, false
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
// has never meant in internal/config: `AFORGE_PROFILE_DIR` unset is the
// ORDINARY launch, and every reader and writer in that package resolves an
// empty directory to ~/.aforge. So a pin from the picker, and `/model
// @cloudflare`, refused to write on every machine nobody had exported that
// variable on — while the settings row beside them wrote fine, because it goes
// through the registry, which passes the same empty string down. One fact, two
// spellings of "where does this land", and only one of them was right.
func (a *app) pinLane(model, name string) {
	slot := laneSlotFor(model)
	if a.hosted() {
		a.note(a.host + " owns the lane · change it on that machine")
		return
	}
	if err := config.SetLane(a.profileDir, slot, name); err != nil {
		a.note(err.Error())
		return
	}
	_ = config.SetLaneBorrow(a.profileDir, slot, false)
	a.laneRowChanged()
	a.noteFacts("lane · "+strings.ToLower(name), name)
	a.touch()
}

// clearLanePin puts the row back to auto.
func (a *app) clearLanePin(model string) {
	if a.hosted() {
		a.note(a.host + " owns the lane · change it on that machine")
		return
	}
	if err := config.SetLane(a.profileDir, laneSlotFor(model), config.LaneAuto); err != nil {
		a.note(err.Error())
		return
	}
	a.laneRowChanged()
	a.noteFacts("lane · auto", config.LaneAuto)
	a.touch()
}

// setLaneRouterOnly is enter on the `openrouter` row: this model asks for no
// lane at all and lets the router balance on price. It is a lane answer and NOT
// the routing row — [config.KeyRouting] is about every request this session
// makes, and this is about the machines behind one model.
func (a *app) setLaneRouterOnly(model string) {
	if a.hosted() {
		a.note(a.host + " owns the lane · change it on that machine")
		return
	}
	if err := config.SetLane(a.profileDir, laneSlotFor(model), config.LaneOpenRouter); err != nil {
		a.note(err.Error())
		return
	}
	a.laneRowChanged()
	a.noteFacts("lane · openrouter", config.LaneOpenRouter)
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
func (a *app) laneRowChanged() {
	if a.hosted() {
		return
	}
	slot := laneSlotFor(a.model)
	if name, pinned := config.LanePinned(a.profileDir, slot); pinned {
		provider.SetLanePin(provider.LanePin{Lane: name, Borrow: config.LaneBorrowAt(a.profileDir, slot)})
		return
	}
	if strings.EqualFold(config.LaneAt(a.profileDir, slot), config.LaneOpenRouter) {
		provider.SetLanePin(provider.LanePin{OpenRouter: true})
		return
	}
	provider.SetLanePin(provider.LanePin{})
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
func (a *app) laneRider() string {
	news, ok := laneNewsFor(a.model)
	if !ok || a.now().Sub(news.At) > servedWindow {
		return ""
	}
	// A ROLE NOBODY IS READING DOES NOT MOVE THIS LINE. See [LaneNews.Role]:
	// the errands that run beside a talk turn each finish on some lane, and the
	// last of them to finish is not the one the person is waiting on.
	if !news.Role.Visible() {
		return ""
	}
	if news.Trying && news.Alt != "" {
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
	// THE RETRACTION. The machine this line was promising has failed, so the
	// promise comes off and the only thing left worth saying is what it did.
	if news.Failed && news.Alt != "" {
		if news.Reason == provider.RescueRefused {
			return " · " + strings.ToLower(news.Alt) + " refused"
		}
		return ""
	}
	if news.rescued() {
		return " · via " + strings.ToLower(news.Winner) + " · rescued"
	}
	if news.Lane == "" {
		return ""
	}
	// AND A LANE THE MODEL ID ALREADY NAMES IS NOT SAID TWICE, which is the
	// rule the rider this one extends has always kept: "gpt-4.1 · via openai"
	// spends a cell a frame on a word the reader already has. A rescue is
	// exempt above, because THAT is news whoever the vendor is.
	served := strings.ToLower(news.Lane)
	if strings.Contains(strings.ToLower(a.model), served) {
		return ""
	}
	rider := " · via " + served
	if word := laneSecondsWord(news.TTFT.Seconds()); word != "" {
		rider += " · " + word
	}
	// THE RATE RIDES ONLY WHILE A TURN IS RUNNING, exactly as it does on the
	// rider this one extends ([app.servedRider]): who served is attribution and
	// stays; how fast they were writing is a claim about now.
	if word := laneRateWord(news.Rate); word != "" && a.state == stateWorking {
		rider += " · " + word
	}
	return rider
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
