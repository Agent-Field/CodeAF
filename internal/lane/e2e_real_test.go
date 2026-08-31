//go:build e2e

package lane_test

// ── THE SAME FIVE CLAIMS, AGAINST THE REAL ROUTER ───────────────────────────
//
// `e2e_test.go` proves the mechanism against a fake router whose lanes behave
// exactly as they were scripted. That is the right instrument for a claim about
// arithmetic and the wrong one for a claim about the world: the fake router's
// sheet is a table somebody typed, its lanes never queue behind anybody else's
// prompts, and its idea of a heavy tail is a number in a struct.
//
// This file spends real money to check the four things a fake router cannot
// say anything about:
//
//  1. THE SHEET IS REAL AND IT IS RICH. `deepseek/deepseek-v4-flash` really is
//     served by more than a handful of machines, and the router really does
//     publish four percentiles for each of them. If either of those stops being
//     true, every prior in this design is invented.
//  2. THE PREFERENCE IS HONOURED. What the chooser asks for reaches the router
//     in a shape it acts on, and the lane that answers is one the sheet knows.
//  3. THE FIRST TOKEN IS MEASURABLE FROM OUT HERE. The whole belief rests on
//     timing the wait before the first token separately from the writing after
//     it, and that separation has to survive a real connection.
//  4. A SLOW LANE CAN BE RESCUED. Pinned on purpose to the slowest machine that
//     still passes the gate, a hundred and fifty tokens is a wait long enough to
//     be worth a second request — and the watch either takes it or says, on the
//     day, that it was not needed.
//
// ── WHAT IT COSTS ───────────────────────────────────────────────────────────
//
// Every call here is capped at a small `max_tokens` and the whole run prints
// the exact `cost` the router billed, added up from the usage frames. On the
// model it is written against that total is a fraction of a cent. Nothing here
// retries and nothing here loops.
//
// ── HOW TO RUN IT ───────────────────────────────────────────────────────────
//
//	go test -tags e2e ./internal/lane/ -run TestReal -v
//
// It skips, green and loudly, with no key. The key is resolved exactly as a
// session resolves it (`internal/config`.APIKeyAt): the OpenRouter variable,
// then the OpenAI one, then the profile file — so a machine that can run aforge
// can run this, and one that cannot is told which of the three to set.
//
// It is an EXTERNAL test package on purpose. It imports `internal/provider`,
// and the dependency between these two packages points that way: a file in
// package `lane` that imported the transport would be an import cycle the day
// the transport starts asking the registry for a preference.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// realModel is the model the design was measured on, and the one whose sheet
// the numbers in `e2e_test.go` were copied from.
const realModel = "deepseek/deepseek-v4-flash"

// realBase is the router's own URL. It is spelled here rather than taken from
// `internal/config` because this file is about the ROUTER and not about
// whatever a person has pointed their session at.
const realBase = "https://openrouter.ai/api/v1"

// realMaxTokens is the ceiling on the five ordinary calls. Two dozen tokens is
// enough for a stream to have a first token and a rate and few enough that five
// of them cost less than a hundredth of a cent.
const realMaxTokens = 24

// realRescueTokens is the ceiling on the one deliberately slow call. It is
// large enough that a six-tokens-a-second lane takes half a minute over it,
// which is the point: a wait nobody would sit through is the only honest test
// of a mechanism for not sitting through waits.
const realRescueTokens = 150

// realRatedFloor is the shortest answer worth taking a rate from. Below it the
// generation window is a handful of milliseconds and what gets measured is the
// handshake — the same floor the transport applies to its own sightings.
const realRatedFloor = 8

// ── THE TEST ────────────────────────────────────────────────────────────────

// TestRealRouterServesASheetAndHonoursAPreference is the whole of this file.
//
// It is one test rather than five because every stage depends on the one before
// it — there is no sheet to choose from until it has been fetched, and nothing
// to rescue until a belief exists — and because five tests would each pay for
// their own sheet fetch and their own warm connection.
func TestRealRouterServesASheetAndHonoursAPreference(t *testing.T) {
	key := strings.TrimSpace(config.APIKeyAt(os.Getenv("AFORGE_PROFILE_DIR")))
	if key == "" {
		t.Skipf("no provider key: set %s (or OPENAI_API_KEY, or the profile's api_key)", config.APIKeyEnv)
	}
	// A home of its own, so that a belief written by this test cannot reach the
	// state root a person is actually using — and so that lanes.json being
	// there afterwards means THIS run wrote it.
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(lane.Default().Reset)
	lane.Default().Reset()

	// ── 1. the sheet ────────────────────────────────────────────────────────
	//
	// The sheet client is installed through the registry's own seam rather than
	// built here, which is what the seam is for. In a shipped process it is
	// `lane.WireSheet` that installs the real one on a beat; this test installs
	// its own so that a change to that constructor's shape cannot turn a
	// question about the router into a compile error about a helper.
	sheet := &realSheetClient{key: key, rows: map[string][]lane.Row{}}
	lane.Default().SetSheet(sheet)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := lane.Default().Sheet().Refresh(ctx, realModel); err != nil {
		t.Fatalf("fetch the sheet for %s: %v", realModel, err)
	}
	rows := lane.Default().Sheet().Rows(realModel)
	if len(rows) < 5 {
		t.Fatalf("%s came back with %d lanes; this design is only worth having because "+
			"one model id is a dozen machines", realModel, len(rows))
	}
	known := 0
	for _, row := range rows {
		if row.Known() {
			known++
		}
	}
	if known < 5 {
		t.Fatalf("%d of %d lanes published enough percentiles to fit a prior from; "+
			"a prior invented from the rest would be this build refusing lanes on a number "+
			"nobody measured", known, len(rows))
	}
	t.Logf("sheet: %d lanes, %d with percentiles", len(rows), known)
	for _, row := range sortedRows(rows) {
		t.Logf("  %-16s %6.0f ms p50 / %6.0f p90 / %7.0f p99   %5.0f tok/s   up %.1f%%   $%.2f/M out   tools=%v",
			row.ID.Lane, row.TTFTp50, row.TTFTp90, row.TTFTp99, row.Ratep50,
			row.Facts.Uptime5m, row.Facts.PriceOut*1e6, row.Facts.Tools)
	}

	ledger := lane.Default().Ledger()
	for _, row := range rows {
		ledger.Prime(row, lane.SheetWeight)
	}

	// ── 2. five real calls, with the chooser on ─────────────────────────────
	client, err := provider.NewClient(provider.Config{APIKey: key, BaseURL: realBase, Model: realModel})
	if err != nil {
		t.Fatalf("build a client: %v", err)
	}
	onTheSheet := map[string]bool{}
	for _, row := range rows {
		onTheSheet[strings.ToLower(row.ID.Lane)] = true
	}

	spent := 0.0
	chose := 0
	timed := 0
	for call := 0; call < 5; call++ {
		now := time.Now()
		choice := lane.Default().Chooser().Choose(realTalk(now))
		if !choice.Empty() {
			chose++
		}

		served := &provider.ServedEndpoint{}
		callCtx := provider.WithServedEndpoint(ctx, served)
		// The first token, timed from out here. It is the one measurement this
		// whole design rests on and the only one a caller can take without the
		// transport's help.
		began := time.Now()
		var firstToken, lastToken time.Time
		var tokens int
		callCtx = provider.WithStreamObserver(callCtx, func(event provider.StreamEvent) {
			if event.Delta == "" {
				return
			}
			if firstToken.IsZero() {
				firstToken = time.Now()
			}
			lastToken = time.Now()
			tokens++
		})

		response, err := client.CompleteWithMessages(callCtx,
			[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "List the first eight prime numbers, one per line."}}}},
			ai.WithMaxTokens(realMaxTokens))
		if err != nil {
			t.Fatalf("call %d: %v", call+1, err)
		}
		spent += costOf(response)

		lane_ := strings.TrimSpace(served.Name())
		if lane_ == "" {
			t.Fatalf("call %d came back without naming the machine that served it; "+
				"the whole of this package is keyed on that name", call+1)
		}
		// Fallbacks are left on for every choice that is not a pin, because a
		// slow answer beats no answer — so the lane that answers need not be the
		// one at the top of the order. What it may never be is a machine the
		// sheet has never heard of.
		if !onTheSheet[strings.ToLower(lane_)] {
			t.Fatalf("call %d was served by %q, which is on no row of the sheet", call+1, lane_)
		}
		if !choice.Empty() {
			t.Logf("call %d: asked for %v, served by %s", call+1, choice.Order, lane_)
		} else {
			t.Logf("call %d: no preference (the chooser has no opinion yet), served by %s", call+1, lane_)
		}

		if firstToken.IsZero() {
			// A paid request that came back with nothing in it is a real thing
			// that really happens, and it is not evidence about how fast the
			// lane is. It teaches the quality axis and nothing else.
			t.Logf("call %d: %s answered with no content at all", call+1, lane_)
			ledger.NoteOutcome(lane.Outcome{
				ID: lane.ID{Model: realModel, Lane: lane_}, Accepted: false,
				Reason: "empty", At: time.Now(),
			})
			continue
		}
		timed++
		sighting := lane.Sighting{
			ID:           lane.ID{Model: realModel, Lane: lane_},
			TTFT:         firstToken.Sub(began),
			PromptTokens: 8,
			At:           time.Now(),
		}
		// A RATE NEEDS SOMETHING TO RATE. An answer a handful of tokens long
		// times the handshake, not the machine, and the design skips the rate
		// observation there rather than folding a number in the thousands into
		// a belief about tokens per second.
		if tokens >= realRatedFloor {
			sighting.Gen, sighting.Tokens = lastToken.Sub(firstToken), tokens
		}
		ledger.Note(sighting)
		ledger.NoteOutcome(lane.Outcome{
			ID: sighting.ID, Accepted: true, Reason: "", At: time.Now(),
		})
		t.Logf("           first token %v, %d deltas, rated at %.1f tok/s",
			sighting.TTFT.Round(time.Millisecond), tokens, sighting.Rate())

		if belief, ok := ledger.Belief(sighting.ID); ok && !belief.TTFT.Known() {
			t.Fatalf("call %d was timed at %v and the ledger believes nothing about its first token",
				call+1, sighting.TTFT)
		}
	}
	if timed == 0 {
		t.Fatal("five calls and not one of them streamed a token that could be timed")
	}
	if chose == 0 {
		t.Logf("the chooser had no opinion on any of the five calls " +
			"(lane chooser not landed yet, wave 1 L-B) — the preference half of this test " +
			"observed the router's own default instead")
	}

	// ── 3. the belief sleeps somewhere ──────────────────────────────────────
	//
	// A new process starts from yesterday's belief, aged. The file is the whole
	// of that promise, so this checks it was written where [lane.StorePath]
	// says — under the temporary home above, which is what makes its presence
	// evidence about this run.
	if err := lane.Default().Store().Save(ledger.Beliefs(realModel)); err != nil {
		t.Logf("beliefs were not written (%v) — the store is lane L-A's and is not in this tree yet", err)
	} else if info, err := os.Stat(lane.StorePath()); err != nil {
		t.Fatalf("the store reported a write and %s is not there: %v", lane.StorePath(), err)
	} else {
		t.Logf("beliefs slept in %s (%d bytes)", lane.StorePath(), info.Size())
	}

	// ── 4. one call pinned to the slowest lane that still passes ────────────
	slow, alternate := slowestPassing(rows)
	if slow.ID.Lane == "" {
		t.Fatal("no lane on the sheet passes a plain gate, so there is nothing to pin")
	}
	expected := time.Duration(slow.TTFTp50)*time.Millisecond +
		time.Duration(float64(time.Second)*realRescueTokens/slow.Ratep50)
	t.Logf("pinning %s: %.0f ms to the first token and %.0f tok/s means about %v for %d tokens; "+
		"the alternative is %s", slow.ID.Lane, slow.TTFTp50, slow.Ratep50,
		expected.Round(time.Millisecond), realRescueTokens, alternate)

	pin := lane.Default().Chooser().Choose(realTalk(time.Now()))
	pin.Only, pin.Order, pin.Ignore = []string{slow.ID.Lane}, nil, nil
	if pin.Alt == "" {
		pin.Alt = alternate
	}
	belief, _ := ledger.Belief(slow.ID)
	watch := lane.NewWatch(pin, belief, time.Now())
	// The shipped budget, so that this test is measuring the router somebody
	// gets rather than an allowance written here: two hedges in any twenty
	// requests and a tenth of recent spend. One request needs one of them.
	budget := lane.DefaultBudget()

	rescue, err := streamPinned(ctx, key, pin, watch, budget, ledger)
	if err != nil {
		t.Fatalf("the pinned call: %v", err)
	}
	spent += rescue.cost
	t.Logf("pinned answer: %v wall, first token %v, %d tokens, served by %s, hedged=%v",
		rescue.wall.Round(time.Millisecond), rescue.ttft.Round(time.Millisecond),
		rescue.tokens, rescue.lane, rescue.hedged)

	switch {
	case rescue.hedged:
		// The claim: a rescue is only worth its second bill if it materially
		// beats what waiting would have cost. Seven tenths is the margin the
		// design is willing to call a rescue.
		if ceiling := time.Duration(0.7 * float64(expected)); rescue.wall >= ceiling {
			t.Fatalf("a hedge fired and the answer still took %v, against %v of simply waiting: "+
				"a rescue that does not beat the wait is a second bill for nothing",
				rescue.wall.Round(time.Millisecond), expected.Round(time.Millisecond))
		}
	case !budget.Allow(time.Now(), 0.001):
		t.Logf("no rescue was attempted: the hedge budget refuses everything " +
			"(lane watch and hedge budget not landed yet, wave 1 L-C)")
	case rescue.wall <= expected:
		t.Logf("no rescue was needed: %s answered in %v against the %v its own sheet row "+
			"predicted, so the watch was right not to spend a second request on it",
			rescue.lane, rescue.wall.Round(time.Millisecond), expected.Round(time.Millisecond))
	default:
		// THE ONE OUTCOME THIS BRANCH MAY NOT CALL A SUCCESS. The answer was
		// slower than its own sheet row predicted and no second request went
		// out — which is the watch missing exactly the case it exists for. It
		// is logged rather than failed because on a real router it is also what
		// a budget that has already been spent looks like, and a live test that
		// failed on somebody else's traffic would be a test nobody could run.
		// The deterministic version of this claim is S2 in e2e_test.go, which
		// does fail.
		t.Logf("NO RESCUE AND IT WAS NEEDED: %s took %v against the %v its own sheet row "+
			"predicted, and no second request went out — the watch did not fire on a "+
			"lane that was %.1fx its own prediction",
			rescue.lane, rescue.wall.Round(time.Millisecond), expected.Round(time.Millisecond),
			rescue.wall.Seconds()/expected.Seconds())
	}

	t.Logf("total billed by the router across %d calls: $%.6f", 6, spent)
}

// ── WHAT THE REQUESTS LOOK LIKE ─────────────────────────────────────────────

// realTalk is a turn somebody is watching, at the attention value of time.
func realTalk(now time.Time) lane.Request {
	return lane.Request{
		Model: realModel, PromptTokens: 8,
		Visible: 400, Hidden: 0, MaxTokens: realMaxTokens,
		ValueOfTime: 90, QualityNeed: 0.90, Horizon: 6, Now: now,
	}
}

// slowestPassing is the slowest WRITER on the sheet that a plain request would
// still be allowed to use, and the fastest one to hedge it to.
//
// Slowest by rate rather than by first token because the thing being staged is
// a long wait somebody could be rescued from, and a lane that starts late and
// then writes quickly is over before a hedge could help.
func slowestPassing(rows []lane.Row) (lane.Row, string) {
	var slow, quick lane.Row
	for _, row := range rows {
		if !row.Known() || row.Facts.Uptime5m < 95 || row.Facts.MaxOut < realRescueTokens {
			continue
		}
		if slow.ID.Lane == "" || row.Ratep50 < slow.Ratep50 {
			slow = row
		}
		if quick.ID.Lane == "" || row.Ratep50 > quick.Ratep50 {
			quick = row
		}
	}
	return slow, quick.ID.Lane
}

// sortedRows puts the sheet in the order the design's own table is in: fastest
// to the first token first, so a log of it reads like the document.
func sortedRows(rows []lane.Row) []lane.Row {
	sorted := append([]lane.Row(nil), rows...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].TTFTp50 < sorted[b].TTFTp50 })
	return sorted
}

// costOf is what the router says a call cost, and zero when it said nothing —
// which is a different fact from free, and the reason nothing here adds a
// price table of its own.
func costOf(response *ai.Response) float64 {
	if response == nil || response.Usage == nil || response.Usage.Cost == nil {
		return 0
	}
	return *response.Usage.Cost
}

// ── THE SHEET, FETCHED FOR REAL ─────────────────────────────────────────────

// realSheetClient reads the router's published endpoint sheet. It is the same
// two halves the seam demands: [Rows] answers from memory with no connection,
// and [Refresh] is the only thing here that goes to the network.
type realSheetClient struct {
	key  string
	rows map[string][]lane.Row
}

// Rows files and reads under the BARE id, which is what the shipped sheet does
// and why: one model served by one set of machines has one endpoints page, and
// the router publishes it under the id without the tier suffix.
func (s *realSheetClient) Rows(model string) []lane.Row { return s.rows[lane.BareModel(model)] }

func (s *realSheetClient) Refresh(ctx context.Context, model string) error {
	model = lane.BareModel(model)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, realBase+"/models/"+model+"/endpoints", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+s.key)
	provider.ApplyAttribution(request.Header)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		drained, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("the sheet answered %d: %s", response.StatusCode, strings.TrimSpace(string(drained)))
	}
	var body struct {
		Data struct {
			Endpoints []struct {
				ProviderName        string `json:"provider_name"`
				Quantization        string `json:"quantization"`
				ContextLength       int    `json:"context_length"`
				MaxCompletionTokens int    `json:"max_completion_tokens"`
				Pricing             struct {
					Prompt         string `json:"prompt"`
					Completion     string `json:"completion"`
					InputCacheRead string `json:"input_cache_read"`
				} `json:"pricing"`
				SupportsToolChoice struct {
					Function bool `json:"function"`
				} `json:"supports_tool_choice"`
				SupportsImplicitCaching bool    `json:"supports_implicit_caching"`
				UptimeLast5m            float64 `json:"uptime_last_5m"`
				LatencyLast30m          struct {
					P50, P75, P90, P99 float64
				} `json:"latency_last_30m"`
				ThroughputLast30m struct {
					P50, P75, P90, P99 float64
				} `json:"throughput_last_30m"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return err
	}
	rows := make([]lane.Row, 0, len(body.Data.Endpoints))
	for _, endpoint := range body.Data.Endpoints {
		rows = append(rows, lane.Row{
			ID: lane.ID{Model: model, Lane: endpoint.ProviderName},
			Facts: lane.Facts{
				Tools: endpoint.SupportsToolChoice.Function, Quant: endpoint.Quantization,
				MaxOut: endpoint.MaxCompletionTokens, Context: endpoint.ContextLength,
				Uptime5m: endpoint.UptimeLast5m, Caches: endpoint.SupportsImplicitCaching,
				PriceIn:    realMoney(endpoint.Pricing.Prompt),
				PriceOut:   realMoney(endpoint.Pricing.Completion),
				PriceCache: realMoney(endpoint.Pricing.InputCacheRead),
			},
			TTFTp50: endpoint.LatencyLast30m.P50, TTFTp75: endpoint.LatencyLast30m.P75,
			TTFTp90: endpoint.LatencyLast30m.P90, TTFTp99: endpoint.LatencyLast30m.P99,
			Ratep50: endpoint.ThroughputLast30m.P50, Ratep75: endpoint.ThroughputLast30m.P75,
			Ratep90: endpoint.ThroughputLast30m.P90, Ratep99: endpoint.ThroughputLast30m.P99,
		})
	}
	s.rows[model] = rows
	return nil
}

// realMoney reads a per-token price, which travels as a string because it has
// more digits than a JSON float survives being read back by everybody's decoder.
func realMoney(price string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(price), 64)
	if err != nil {
		return 0
	}
	return value
}

// ── ONE PINNED STREAM, WATCHED ──────────────────────────────────────────────

// realRescue is what the pinned call came back as.
type realRescue struct {
	lane   string
	ttft   time.Duration
	wall   time.Duration
	tokens int
	cost   float64
	hedged bool
}

// streamPinned sends the pinned request, drives [lane.Watch] over it, and
// spends at most one hedge under the budget.
//
// It talks to the router directly rather than through `internal/provider`
// because the thing being tested is the WATCH — the contract in this package —
// and the transport's own half of it is tested where the transport lives.
func streamPinned(ctx context.Context, key string, choice lane.Choice, watch *lane.Watch, budget *lane.Budget, ledger lane.Ledger) (realRescue, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	began := time.Now()
	primary := make(chan realRescue, 1)
	primaryErr := make(chan error, 1)
	go func() {
		answer, err := realStream(streamCtx, key, choice.Only, realRescueTokens, watch)
		if err != nil {
			primaryErr <- err
			return
		}
		primary <- answer
	}()

	var hedge <-chan realRescue
	var hedgeErr <-chan error
	var hedgeCancel context.CancelFunc
	deadline := choice.Deadline
	if deadline <= 0 || choice.Alt == "" {
		// No deadline is "do not hedge this request", which is the honest
		// answer while the arithmetic that computes one has not landed.
		deadline = 0
	}
	var alarm <-chan time.Time
	if deadline > 0 {
		timer := time.NewTimer(deadline)
		defer timer.Stop()
		alarm = timer.C
	}

	hedged := false
	for {
		select {
		case answer := <-primary:
			answer.wall = time.Since(began)
			answer.hedged = hedged
			ledger.Note(lane.Sighting{
				ID: lane.ID{Model: realModel, Lane: answer.lane}, TTFT: answer.ttft,
				Gen: answer.wall - answer.ttft, Tokens: answer.tokens, PromptTokens: 8, At: time.Now(),
			})
			return answer, nil

		case answer := <-hedge:
			answer.wall = time.Since(began)
			answer.hedged = true
			cancel()
			// A HEDGE IS A MEASUREMENT: the alternative lane taught us
			// something too, and it is the only cheap way to learn it.
			ledger.Note(lane.Sighting{
				ID: lane.ID{Model: realModel, Lane: answer.lane}, TTFT: answer.ttft,
				Gen: answer.wall - answer.ttft, Tokens: answer.tokens, PromptTokens: 8, At: time.Now(),
			})
			return answer, nil

		case err := <-primaryErr:
			if hedge != nil {
				continue
			}
			return realRescue{}, err

		case err := <-hedgeErr:
			_ = err
			hedge, hedgeErr = nil, nil

		case <-alarm:
			alarm = nil
			if verdict := watch.Silence(time.Now()); !verdict.Hedge {
				continue
			}
			if !budget.Allow(time.Now(), 0.001) {
				continue
			}
			hedged = true
			out := make(chan realRescue, 1)
			fail := make(chan error, 1)
			var alternateCtx context.Context
			alternateCtx, hedgeCancel = context.WithCancel(ctx)
			defer hedgeCancel()
			go func() {
				answer, err := realStream(alternateCtx, key, []string{choice.Alt}, realRescueTokens, nil)
				if err != nil {
					fail <- err
					return
				}
				out <- answer
			}()
			hedge, hedgeErr = out, fail

		case <-ctx.Done():
			return realRescue{}, ctx.Err()
		}
	}
}

// realStream is one streamed completion against the real router, with the
// preference on it and the watch, when there is one, fed every heartbeat and
// every token.
func realStream(ctx context.Context, key string, only []string, maxTokens int, watch *lane.Watch) (realRescue, error) {
	body := map[string]any{
		"model":      realModel,
		"stream":     true,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": "Count slowly from one to sixty, one number per line."},
		},
		"usage": map[string]any{"include": true},
	}
	if len(only) > 0 {
		body["provider"] = map[string]any{"only": only}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return realRescue{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, realBase+"/chat/completions", strings.NewReader(string(encoded)))
	if err != nil {
		return realRescue{}, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	provider.ApplyAttribution(request.Header)

	began := time.Now()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return realRescue{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		drained, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return realRescue{}, fmt.Errorf("the router answered %d: %s", response.StatusCode, strings.TrimSpace(string(drained)))
	}

	answer := realRescue{}
	var firstToken time.Time
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ":") {
			// The router's own comment line: proof about the PATH and never
			// about the endpoint, which is exactly what the watch needs to tell
			// a dead connection from a slow lane.
			if watch != nil {
				watch.Heartbeat(time.Now())
			}
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
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
			continue
		}
		if chunk.Provider != "" {
			answer.lane = chunk.Provider
		}
		if chunk.Usage != nil {
			answer.tokens = chunk.Usage.CompletionTokens
			answer.cost = chunk.Usage.Cost
			continue
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		if firstToken.IsZero() {
			firstToken = time.Now()
			answer.ttft = firstToken.Sub(began)
		}
		answer.tokens++
		if watch != nil {
			watch.Token(answer.tokens, answer.tokens, time.Now())
		}
	}
	if err := scanner.Err(); err != nil {
		return realRescue{}, err
	}
	answer.wall = time.Since(began)
	return answer, nil
}

// ── THE TIER, THE TOOLS, AND THE REAL ROUTER ────────────────────────────────

// realTierModel is the model the 2026-08-30 incident happened on, spelled the
// way the person's own settings spell it. The suffix is a reasoning tier: the
// router serves the same endpoints for it and publishes the same sheet under
// the bare id, which is the fact [lane.BareModel] states and this test checks
// against the live router rather than against a fixture.
const realTierModel = "moonshotai/kimi-k3:high"

// realToolTokens is the ceiling on the one tool-bearing call. Forty is enough
// for a small tool call to be emitted whole and few enough that the whole test
// costs a fraction of a cent.
const realToolTokens = 40

// TestRealRouterChoosesForATierAndATurnThatCarriesTools is the incident, run
// against the machine it happened on.
//
// A turn carrying tools, on a model asked for by its tier, must come back with
// a preference — and be served by a machine the sheet knows. Before this wave
// the sheet was fetched and primed under one id and every question was asked of
// another, so the chooser had fewer than two lanes to rank and answered with no
// opinion at all; the request then went out on the router's own sort with
// nothing watching it.
//
//	go test -tags e2e ./internal/lane/ -run TestRealRouterChooses -v
func TestRealRouterChoosesForATierAndATurnThatCarriesTools(t *testing.T) {
	key := strings.TrimSpace(config.APIKeyAt(os.Getenv("AFORGE_PROFILE_DIR")))
	if key == "" {
		t.Skipf("no provider key: set %s (or OPENAI_API_KEY, or the profile's api_key)", config.APIKeyEnv)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(lane.Default().Reset)
	lane.Default().Reset()

	sheet := &realSheetClient{key: key, rows: map[string][]lane.Row{}}
	lane.Default().SetSheet(sheet)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// ── 1. the sheet, fetched for the TIER ──────────────────────────────────
	//
	// The refresh is asked for with the suffix on, exactly as a session's beat
	// would ask for the model the person configured, and what it must come back
	// with is the bare model's endpoints page.
	if err := lane.Default().Sheet().Refresh(ctx, realTierModel); err != nil {
		t.Skipf("no sheet for %s today (%v) — this test is about the id and not about the router's uptime", realTierModel, err)
	}
	rows := lane.Default().Sheet().Rows(realTierModel)
	if len(rows) < 2 {
		t.Fatalf("%s came back with %d lanes; there is nothing to choose between", realTierModel, len(rows))
	}
	bare := lane.BareModel(realTierModel)
	if len(lane.Default().Sheet().Rows(bare)) != len(rows) {
		t.Fatalf("the tier and the bare id do not read the same sheet: %d against %d",
			len(rows), len(lane.Default().Sheet().Rows(bare)))
	}
	tooled := 0
	for _, row := range rows {
		if row.Facts.Tools {
			tooled++
		}
	}
	t.Logf("sheet: %d lanes for %s, %d of them take a tool call", len(rows), bare, tooled)
	if tooled == 0 {
		t.Skip("no lane of this model takes a tool call today, so there is no tool-bearing turn to route")
	}

	ledger := lane.Default().Ledger()
	for _, row := range rows {
		ledger.Prime(row, lane.SheetWeight)
	}
	// The primes landed under the BARE id, which is the whole fix, and the
	// beliefs are readable through the tier the person actually configured.
	//
	// It is counted against the DISTINCT lane names rather than against the
	// rows, because a real endpoints page lists the same provider more than
	// once — on the day this was written `moonshotai/kimi-k3` published
	// seventeen endpoints under fourteen names, with Morph and Fireworks each
	// appearing at two quantizations. A belief is keyed on (model, lane), so
	// fourteen is the right answer and seventeen would be the wrong one.
	names := map[string]bool{}
	for _, row := range rows {
		names[row.ID.Lane] = true
	}
	if got := len(ledger.Beliefs(realTierModel)); got != len(names) {
		t.Fatalf("%d rows under %d names primed and the tier reads %d beliefs", len(rows), len(names), got)
	}

	// ── 2. one real turn, carrying a tool ───────────────────────────────────
	client, err := provider.NewClient(provider.Config{APIKey: key, BaseURL: realBase, Model: realTierModel})
	if err != nil {
		t.Fatalf("build a client: %v", err)
	}
	choice := lane.Default().Chooser().Choose(realTools(time.Now()))
	if choice.Empty() {
		t.Fatalf("a turn carrying tools, on a model with %d primed lanes, got no preference at all — "+
			"which is the incident this test is written from", len(rows))
	}
	t.Logf("choice: order %v, alt %q, hedge at %v, why %q", choice.Order, choice.Alt, choice.Deadline, choice.Why)
	head, ok := ledger.Belief(lane.ID{Model: bare, Lane: choice.Order[0]})
	if !ok || !head.Facts.Tools {
		t.Fatalf("a turn carrying tools was ranked first onto %q, which the sheet does not say takes one", choice.Order[0])
	}

	served := &provider.ServedEndpoint{}
	callCtx := provider.WithServedEndpoint(ctx, served)
	began := time.Now()
	var firstToken time.Time
	var tokens int
	callCtx = provider.WithStreamObserver(callCtx, func(event provider.StreamEvent) {
		if event.Delta == "" {
			return
		}
		if firstToken.IsZero() {
			firstToken = time.Now()
		}
		tokens++
	})
	response, err := client.CompleteWithMessages(callCtx,
		[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "What is the weather in Paris? Use the tool."}}}},
		ai.WithMaxTokens(realToolTokens),
		ai.WithTools([]ai.ToolDefinition{{
			Type: "function",
			Function: ai.ToolFunction{
				Name:        "get_weather",
				Description: "The current weather in one city.",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
					"required":   []string{"city"},
				},
			},
		}}))
	if err != nil {
		t.Fatalf("the tool-bearing call failed: %v", err)
	}
	t.Logf("this run billed $%.6f", costOf(response))

	lane_ := strings.TrimSpace(served.Name())
	if lane_ == "" {
		t.Fatal("the answer did not name the machine that served it")
	}
	onTheSheet := false
	for _, row := range rows {
		if strings.EqualFold(row.ID.Lane, lane_) {
			onTheSheet = true
		}
	}
	// Fallbacks are left on for every choice that is not a pin — a slow answer
	// beats no answer — so the lane that answers need not be the one asked for
	// first. What it may never be is a machine on no row of the sheet.
	if !onTheSheet {
		t.Fatalf("served by %q, which is on no row of %s's sheet", lane_, bare)
	}
	t.Logf("asked for %v, served by %s", choice.Order, lane_)

	// ── 3. and the wait was measured, under the bare id ──────────────────────
	if firstToken.IsZero() {
		t.Skipf("%s answered with no streamed content at all, so there is no first token to time", lane_)
	}
	ledger.Note(lane.Sighting{
		ID: lane.ID{Model: realTierModel, Lane: lane_}, TTFT: firstToken.Sub(began),
		PromptTokens: 16, At: time.Now(),
	})
	belief, ok := ledger.Belief(lane.ID{Model: bare, Lane: lane_})
	if !ok || !belief.TTFT.Known() {
		t.Fatalf("a turn timed at %v left no first-token belief under %s",
			firstToken.Sub(began).Round(time.Millisecond), bare)
	}
	t.Logf("first token %v over %d deltas; the belief now reads %.0fms",
		firstToken.Sub(began).Round(time.Millisecond), tokens, belief.TTFT.Mean())
}

// realTools is the turn this test routes: a person waiting, a short answer they
// will read, and a tool on the request.
func realTools(now time.Time) lane.Request {
	return lane.Request{
		Model:        realTierModel,
		PromptTokens: 16,
		Visible:      400,
		Tools:        true,
		MaxTokens:    realToolTokens,
		QualityNeed:  0.9,
		ValueOfTime:  90,
		Horizon:      40,
		Now:          now,
	}
}
