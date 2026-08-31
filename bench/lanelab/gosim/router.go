package main

// ── THIS PROGRAM'S FIFTY LINES OF TRANSPORT ─────────────────────────────────
//
// The contract in docs/ARCHITECTURE.md Decision 10, written out: ask, send,
// watch, fold back. It is the same shape as `e2eRouter` in
// `internal/lane/e2e_test.go` and it is written again here rather than imported
// because a test file's helpers belong to the test binary — and because this
// one has to do three things that file does not: run three named policies
// against one world, drive the real [lane.Watch] rather than only the choice's
// deadline, and keep books a benchmark can be argued with from.
//
// ── THE TWO CLOCKS, AND WHY THE FAST ONE IS NOT USED ────────────────────────
//
// A scenario written in the units of the world takes hours. So THE WIRE RUNS
// [speedup] TIMES FASTER THAN THE WORLD IT DESCRIBES, and nothing else does:
// every duration that crosses into the ledger, every moment a request carries,
// and every figure printed is in the world's own units, and only the timers
// armed against the socket are divided.
//
// `lanestub.Fast` — the clock that costs nothing — is NOT USED, and the reason
// is worth stating because the obvious reading of "use the fast clock" is that
// it would make this run in seconds. It cannot, for two independent reasons.
// The first is the one `lanestub.go` states itself: it is ONE TIMELINE shared by
// every request in flight, and the hedge arm exists precisely to put two
// streams in flight at once. The second is stronger and is a property of the
// stub rather than of this program: A STREAMED ANSWER CARRIES NO CLOCK. Its
// chunks are stamped `created: 0`, so a client reading a stream served on the
// fast clock sees every token arrive at once and can measure neither a first
// token nor a rate — the two quantities this whole bench is about. Only
// `serveWhole`, the non-streaming path, stamps the clock's own moment. So the
// fast clock is unusable by anything that times a stream off the wire, and the
// speedup discipline is the only one available.
//
// What that costs is measured rather than assumed: every request records how
// much of its first token was the socket rather than the script, and the median
// of that is printed with the table.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// The three arms. `strike` is absent and its absence is reported rather than
// faked — see [strikeNote] in main.go.
const (
	policyDefault = "default"
	policyBelief  = "belief"
	policyHedge   = "belief+hedge"
)

// record is what one request came back with, in the world's own units.
type record struct {
	lane     string
	ttftMs   float64 // the first token the CALLER waited for, world milliseconds
	wallS    float64 // the whole answer, N̂ tokens long
	waitS    float64 // lane.PerceivedSeconds of the same answer
	usd      float64
	hedged   bool
	wasteUSD float64
	retries  int
	socketMs float64 // how much of the first token was the socket, world ms
}

// router is one arm running one scenario against one scripted world.
type router struct {
	world   *world
	scen    scenario
	policy  string
	seed    int
	stub    *lanestub.Server
	ledger  lane.Ledger
	budget  *lane.Budget
	client  *http.Client
	speedup int
	total   int
	victim  string
	breakAt int
	healAt  int

	// scripted is what each lane was told to wait before its first token on the
	// WIRE's clock for the request now in flight. It is written once per attempt
	// before any stream starts and read by the streams themselves, which is why
	// no lock guards it, and a companion map holds the rate each was told to
	// write at, in the WORLD's tokens per second.
	scripted     map[string]time.Duration
	scriptedRate map[string]float64

	// at is the moment in the WORLD, which advances by what each request
	// actually took plus the time somebody spends reading the answer. A belief
	// ages, and ageing is what lets a lane come back.
	at time.Time

	// trace writes one line per request to standard error: what was asked for,
	// what answered, and how long it took. It is off unless -trace says
	// otherwise, and it is here because the first question of every autopsy on a
	// number in this table is WHICH LANE, WHEN.
	trace bool
}

// scaled turns a duration of the world into a duration of the wire.
func (r *router) scaled(d time.Duration) time.Duration {
	return d / time.Duration(r.speedup)
}

// worldly turns a duration measured on the wire back into the world's units,
// which are the only ones anything here reports.
func (r *router) worldly(d time.Duration) time.Duration {
	return d * time.Duration(r.speedup)
}

// one is a whole request: the preference, the send, the retries a refused
// answer earns, and everything folded back.
func (r *router) one(index int) (record, error) {
	req := r.scen.request(r.world.model, r.at, r.total-index)

	var choice lane.Choice
	var order []string
	if r.policy == policyDefault {
		// NO PREFERENCE AT ALL. Nothing is asked of the chooser and no
		// `provider` block goes on the wire; the order below only decides which
		// lane the fake router finds at the head of its list, which is what
		// stands in for the router's own 1/price² default.
		order = r.world.defaultOrder(r.scen, r.seed, index)
	} else {
		choice = lane.Default().Chooser().Choose(req)
		order = choice.Order
		if len(order) == 0 {
			order = choice.Only
		}
	}
	if len(order) == 0 {
		return record{}, fmt.Errorf("request %d of %s/%s was given nowhere to go",
			index, r.scen.name, r.policy)
	}

	broken := ""
	if index >= r.breakAt && index < r.healAt {
		broken = r.victim
	}

	out := record{}
	elapsed := 0.0
	for attempt := 0; attempt < maxAttempts && attempt < len(order); attempt++ {
		shots := r.world.shots(r.seed, r.scen.name, index, attempt)
		scripted := r.world.stubLanes(shots, order[attempt], broken, r.speedup)
		r.scripted = make(map[string]time.Duration, len(scripted))
		r.scriptedRate = make(map[string]float64, len(scripted))
		for _, one := range scripted {
			r.scripted[one.Name] = one.TTFT
		}
		// The rate each lane is writing at comes from the draw and not from the
		// profile, because the profile's own rate is [instantRate] — the wire
		// carries the tokens and the world carries the speed.
		for at, l := range r.world.lanes {
			r.scriptedRate[l.name] = shots[at].rate
		}
		r.stub.Model(r.world.model, scripted...)

		var answer answered
		var err error
		if attempt == 0 && r.policy == policyHedge {
			answer, err = r.race(choice, req)
		} else if attempt == 0 && r.policy == policyBelief {
			answer, err = r.plain(choice.Order, nil, choice.Ignore, req)
		} else if attempt == 0 {
			answer, err = r.plain(nil, nil, nil, req)
		} else {
			// A retry is a demand and not a preference: the answer this lane
			// gave could not be used, so the next one in the order is asked
			// for by name.
			answer, err = r.plain(nil, []string{order[attempt]}, nil, req)
		}
		if err != nil {
			return record{}, err
		}

		served, index2 := r.world.find(answer.lane)
		if index2 < 0 {
			return record{}, fmt.Errorf("%s answered and is not on the sheet", answer.lane)
		}
		// The bill, from the usage frame the stub sent, with the rest of the
		// answer added at the serving lane's own published output tariff. The
		// frame carries the EXACT cost of the tokens that were written; N̂ is
		// how long the answer really is, and the two together are the same
		// figure `sim.py` charges through its one price function.
		cost := answer.cost + served.priceOut*float64(r.scen.answer()-answer.tokens)
		out.usd += cost
		if answer.hedged {
			out.hedged = true
			// The loser was cancelled and never reported what it cost, so it is
			// charged the whole request. That is what `sim.py` charges for a
			// hedge and it is the conservative direction: a stream cancelled
			// after its prompt was read has already been billed for the prompt.
			waste := answer.loserPrice
			out.usd += waste
			out.wasteUSD += waste
		}

		rate := answer.rate()
		wall := answer.ttftWorld.Seconds() + float64(r.scen.answer())/rate
		out.socketMs = answer.socketMs

		// A HEDGE IS A MEASUREMENT: whichever half of the pair answered taught
		// us something about the lane it went to, and it is folded in like any
		// other sighting. The baseline holds no beliefs and is told nothing,
		// because nothing would read it.
		if r.policy != policyDefault {
			r.ledger.Note(lane.Sighting{
				ID:           lane.ID{Model: r.world.model, Lane: answer.lane},
				TTFT:         answer.ownTTFTWorld,
				Gen:          answer.gen(),
				Tokens:       answer.tokens,
				PromptTokens: promptTokens,
				At:           r.at,
			})
		}

		accepted := shots[index2].roll < acceptOf(served, r.scen)
		if r.policy != policyDefault {
			r.ledger.NoteOutcome(lane.Outcome{
				ID:       lane.ID{Model: r.world.model, Lane: answer.lane},
				Accepted: accepted,
				Reason:   "tool_json",
				At:       r.at,
			})
		}

		if accepted {
			out.lane = answer.lane
			out.ttftMs = elapsed*1000 + float64(answer.ttftWorld)/float64(time.Millisecond)
			elapsed += wall
			out.wallS = elapsed
			out.waitS = lane.PerceivedSeconds(out.ttftMs/1000, rate, r.scen.visible, r.scen.hidden)
			break
		}
		// A REFUSED ANSWER COSTS THE WHOLE CALL: the tokens were written and
		// the wait was waited. Charging only the retry would make a lane that
		// returns unusable JSON look fast.
		elapsed += wall
		out.retries++
		out.lane = answer.lane
		out.ttftMs = elapsed * 1000
		out.wallS = elapsed
		out.waitS = lane.PerceivedSeconds(out.ttftMs/1000, rate, r.scen.visible, r.scen.hidden)
	}

	if r.trace {
		fmt.Fprintf(os.Stderr, "trace %s/%s/%d  %4d  asked %-14s served %-14s "+
			"ttft %7.0f ms  wait %8.2f s  $%.6f%s%s\n",
			r.scen.name, r.policy, r.seed, index, order[0], out.lane,
			out.ttftMs, out.waitS, out.usd,
			map[bool]string{true: "  HEDGED", false: ""}[out.hedged],
			map[bool]string{true: "  BROKEN:" + broken, false: ""}[broken != ""])
	}
	// THE BUDGET'S DENOMINATOR IS REQUESTS, so it is told about every one of
	// them and not only about the ones that needed rescuing (Part III, C4).
	r.budget.NoteRequest(r.at)
	r.budget.NoteSpend(out.usd, r.at)

	// The world moves on by what this took plus the seconds somebody spends
	// reading whatever of it they read.
	reading := float64(r.scen.visible) / lane.ReadRate
	r.at = r.at.Add(time.Duration((out.wallS + reading) * float64(time.Second)))
	return out, nil
}

// acceptOf is the share of this lane's answers that come back usable. A lane
// that cannot take a tool call returns nothing usable to a request carrying
// one, which is the gate's whole argument stated as a probability.
func acceptOf(l worldLane, s scenario) float64 {
	if s.tools && !l.tools {
		return 0
	}
	return l.accept
}

// find is the sheet row of a lane by name.
func (w *world) find(name string) (worldLane, int) {
	for index, l := range w.lanes {
		if l.name == name {
			return l, index
		}
	}
	return worldLane{}, -1
}

// answered is one finished stream, plus what a hedge on it cost.
type answered struct {
	lane   string
	tokens int
	cost   float64
	// scriptedRate is how fast the lane that answered was writing, in the
	// world's tokens per second. It is the world's own draw and not a
	// measurement — see [instantRate] for the measurement that made that
	// necessary.
	scriptedRate float64
	// ttftWorld is the wait the CALLER experienced, measured from the moment
	// the first request went out — so a hedge that won carries the deadline and
	// the second handshake inside it.
	ttftWorld time.Duration
	// ownTTFTWorld is the wait measured from the moment the stream that
	// answered went out, which is the only first token a belief may be charged
	// for.
	ownTTFTWorld time.Duration
	hedged       bool
	loserPrice   float64
	socketMs     float64
}

// rate is how fast the answering lane wrote, in the world's tokens per second.
func (a answered) rate() float64 { return math.Max(rateFloor, a.scriptedRate) }

// gen is the generation window the answer really had, in the world's units. It
// is derived from the rate rather than timed, for the reason [instantRate]
// gives, and it is what the ledger's own rate filter is fed.
func (a answered) gen() time.Duration {
	return time.Duration(float64(a.tokens) / a.rate() * float64(time.Second))
}

// plain is one request with no rescue: the preference on the wire, the stream
// read to the end.
func (r *router) plain(order, only, ignore []string, req lane.Request) (answered, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	began := time.Now()
	one := r.start(ctx, order, only, ignore, req)
	result := <-one.done
	if result.err != nil {
		return answered{}, result.err
	}
	return r.settle(result, began, began), nil
}

// race is one request with the shipped rescue on it: the real [lane.Watch] over
// the real [lane.Choice], and the real [lane.DefaultBudget].
//
// THE WINNER IS THE FIRST STREAM TO SPEAK, which is what the shipped transport
// does (`internal/provider/hedge.go` picks the speaker). The loser is cancelled
// at once, because cancelling is what stops the bill.
func (r *router) race(choice lane.Choice, req lane.Request) (answered, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// What was believed about the lane expected to serve, which is what turns a
	// gap into a surprise rather than into a threshold.
	belief, _ := r.ledger.Belief(lane.ID{Model: req.Model, Lane: choice.Order[0]})
	watch := lane.NewWatch(choice, belief, r.at)
	watch.SetExpectedTokens(streamTokens)

	began := time.Now()
	primary := r.start(ctx, choice.Order, nil, choice.Ignore, req)

	var alt *stream
	var altFirst <-chan time.Time
	var altDone <-chan streamResult
	var hedgeAt <-chan time.Time
	var timer *time.Timer
	if watch.Deadline() > 0 && choice.Alt != "" {
		timer = time.NewTimer(r.scaled(watch.Deadline()))
		defer timer.Stop()
		hedgeAt = timer.C
	}

	hedged := false
	altBegan := began
	var winner, loser *stream
	primaryFirst := primary.first
	primaryDone := primary.done
	var held streamResult
	haveHeld := false

	for winner == nil {
		select {
		case <-primaryFirst:
			// It started talking before its deadline, so there is nothing to
			// rescue.
			primaryFirst = nil
			hedgeAt = nil
			winner, loser = primary, alt

		case <-altFirst:
			altFirst = nil
			winner, loser = alt, primary

		case <-hedgeAt:
			hedgeAt = nil
			estimate := r.estimate(choice.Alt)
			if !r.budget.Allow(r.at, estimate) {
				continue
			}
			hedged = true
			// A hedge is charged when it is FIRED and at the estimate it was
			// allowed on, because the stream that loses is cancelled and never
			// reports what it cost.
			r.budget.NoteHedge(estimate, r.at)
			altBegan = time.Now()
			alt = r.start(ctx, nil, []string{choice.Alt}, nil, req)
			altFirst, altDone = alt.first, alt.done

		case result := <-primaryDone:
			// A stream that finished without ever speaking is an error, and
			// there is nothing to race.
			primaryDone, primaryFirst = nil, nil
			if alt == nil {
				if result.err != nil {
					return answered{}, result.err
				}
				out := r.settle(result, began, began)
				out.hedged = hedged
				return out, nil
			}
			held, haveHeld = result, true

		case result := <-altDone:
			altDone, altFirst = nil, nil
			if result.err != nil {
				continue
			}
			out := r.settle(result, began, altBegan)
			out.hedged = hedged
			out.loserPrice = r.priceOfServing(choice.Order[0])
			primary.cancel()
			return out, nil
		}
	}

	if loser != nil {
		loser.cancel()
	}
	var result streamResult
	if winner == primary && haveHeld {
		result = held
	} else {
		result = <-winner.done
	}
	if result.err != nil {
		return answered{}, result.err
	}
	start := began
	if winner == alt {
		start = altBegan
	}
	out := r.settle(result, began, start)
	out.hedged = hedged
	if hedged {
		if winner == primary {
			out.loserPrice = r.priceOfServing(choice.Alt)
		} else {
			out.loserPrice = r.priceOfServing(choice.Order[0])
		}
	}
	// The watch is told how the answer ended.  //nolint:staticcheck
	//
	// WHAT IT IS AND IS NOT DOING HERE, said plainly. The deadline this race was
	// armed on is the shipped one — [lane.NewWatch] over the real [lane.Choice],
	// falling back to the lane's own believed p90 first token when the chooser
	// named none — and that is the half of the watch this bench can honestly
	// exercise. Its CUSUM drift half is not fed token by token, because the stub
	// writes forty tokens where the scenario's answer is four hundred or two
	// thousand: a drift alarm accumulated over forty scripted gaps a thousandth
	// of a second apart would be measuring this machine's socket jitter and
	// calling it a lane going bad.
	watch.Token(result.tokens, r.at.Add(out.gen()))
	return out, nil
}

// priceOfServing is what a whole request would cost on one lane. It is what a
// hedge is weighed against and what a cancelled loser is charged.
func (r *router) priceOfServing(name string) float64 {
	l, index := r.world.find(name)
	if index < 0 {
		return 0
	}
	return l.price(r.scen)
}

// estimate is what the alternative is expected to cost, which is what the
// budget is asked about.
func (r *router) estimate(name string) float64 { return r.priceOfServing(name) }

// settle turns one finished stream into the answer, in the world's units.
func (r *router) settle(result streamResult, callBegan, streamBegan time.Time) answered {
	return answered{
		lane:         result.lane,
		tokens:       result.tokens,
		cost:         result.cost,
		scriptedRate: r.scriptedRate[result.lane],
		ttftWorld:    r.worldly(result.firstAt.Sub(callBegan)),
		ownTTFTWorld: r.worldly(result.firstAt.Sub(streamBegan)),
		socketMs: float64(r.worldly(result.firstAt.Sub(streamBegan)-r.scriptedWire(result.lane))) /
			float64(time.Millisecond),
	}
}

// scriptedWire is what the lane that answered was scripted to wait on the
// WIRE's clock for the request now in flight.
//
// The difference between it and what the socket actually delivered is the only
// honest answer to "how much of this table is the loopback", and it is reported
// with the table rather than argued about.
func (r *router) scriptedWire(name string) time.Duration {
	return r.scripted[name]
}

// ── THE WIRE ────────────────────────────────────────────────────────────────

// streamResult is one finished stream, in the wire's units.
type streamResult struct {
	lane    string
	gen     time.Duration
	firstAt time.Time
	tokens  int
	cost    float64
	err     error
}

// stream is one request in flight.
type stream struct {
	first  chan time.Time
	done   chan streamResult
	cancel context.CancelFunc
}

// start puts one request on the wire and reads it in the background.
func (r *router) start(ctx context.Context, order, only, ignore []string, req lane.Request) *stream {
	inner, cancel := context.WithCancel(ctx)
	one := &stream{first: make(chan time.Time, 1), done: make(chan streamResult, 1), cancel: cancel}
	go func() {
		one.done <- r.read(inner, one.first, order, only, ignore, req)
	}()
	return one
}

// read is the wire itself: the preference in the router's own field names, and
// the stream back with the serving lane named on every chunk.
func (r *router) read(ctx context.Context, first chan time.Time, order, only, ignore []string, req lane.Request) streamResult {
	body := map[string]any{
		"model":      req.Model,
		"stream":     true,
		"max_tokens": streamTokens,
		"messages": []map[string]string{
			// The stub counts a prompt at four characters to the token off the
			// RAW JSON, quotes included, so this is exactly promptTokens.
			{"role": "user", "content": strings.Repeat("x", 4*promptTokens-2)},
		},
	}
	preference := map[string]any{}
	if len(order) > 0 {
		preference["order"] = order
	}
	if len(only) > 0 {
		preference["only"] = only
	}
	if len(ignore) > 0 {
		preference["ignore"] = ignore
	}
	if len(preference) > 0 {
		body["provider"] = preference
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return streamResult{err: err}
	}
	post, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.stub.URL()+"/chat/completions", strings.NewReader(string(encoded)))
	if err != nil {
		return streamResult{err: err}
	}
	post.Header.Set("Content-Type", "application/json")

	response, err := r.client.Do(post)
	if err != nil {
		return streamResult{err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		drained, _ := io.ReadAll(response.Body)
		return streamResult{err: fmt.Errorf("the router answered %d: %s",
			response.StatusCode, strings.TrimSpace(string(drained)))}
	}

	result := streamResult{}
	var firstToken, lastToken time.Time
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			// A comment line is the router's own heartbeat: proof about the
			// PATH and never about the endpoint.
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Provider string `json:"provider"`
			Choices  []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				CompletionTokens int     `json:"completion_tokens"`
				Cost             float64 `json:"cost"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return streamResult{err: err}
		}
		if chunk.Provider != "" {
			result.lane = chunk.Provider
		}
		if chunk.Usage != nil {
			result.tokens = chunk.Usage.CompletionTokens
			result.cost = chunk.Usage.Cost
			continue
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		if firstToken.IsZero() {
			firstToken = time.Now()
			select {
			case first <- firstToken:
			default:
			}
		}
		lastToken = time.Now()
	}
	if err := scanner.Err(); err != nil {
		return streamResult{err: err}
	}
	if firstToken.IsZero() {
		return streamResult{err: fmt.Errorf("a stream from %q never wrote a token", result.lane)}
	}
	result.firstAt = firstToken
	result.gen = lastToken.Sub(firstToken)
	return result
}
