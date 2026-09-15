package provider

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/home"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
)

// ── THE MEASURED RACE OF 2026-09-10 19:32 EDT ───────────────────────────────
//
// Chat turn on deepseek/deepseek-v4.1-flash, build 839884259. Three arms: the
// primary's set was narrowed to Fireworks and the account's paid-training
// setting excluded it (404 in 35 ms); the walk demanded DeepSeek and got the
// same refusal (404 in 39 ms); the walk demanded Io Net, which accepted, went
// silent for ten seconds and then handed over a 429 inside the 200. Every arm
// was dead, nobody had committed, and the race settled on the PRIMARY'S 404 —
// which the session read as "the request itself was refused" and ended the
// turn on. The ladder that would have answered it (relax the endpoint filter,
// let the router choose) never ran, because each door that saw a routing
// refusal believed the walk would carry it and the last arm's 429 never
// reached a door at all.
//
// These scenarios replay that wire through the whole client. The lanes are the
// measured ones and the router's bodies are the live router's, metadata and
// all (lanestub's accountRefusal).

// measuredLanes is the §1 serving set: two machines the account excludes, one
// whose pool is full, and one the router's free choice lands on.
func measuredLanes(freeChoice bool) []lanestub.Lane {
	offered := []lanestub.Lane{
		{Name: "Fireworks", AccountExcluded: true, Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000}},
		{Name: "DeepSeek", AccountExcluded: true, Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000}},
		{Name: "Io Net", Profile: lanestub.Profile{Paced: true, PacedAfter: 30 * time.Millisecond}},
	}
	if freeChoice {
		offered = append(offered, lanestub.Lane{Name: "Novita", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 31,
		}})
	}
	return offered
}

// measuredChoice is the primary as the chooser ranked it: Fireworks first, with
// the two machines the walk demanded next on its frontier.
func measuredChoice(model string) lanes.Choice {
	choice := lanes.Choice{Order: []string{"Fireworks"}}
	for _, lane := range []string{"Fireworks", "DeepSeek", "Io Net"} {
		choice.Frontier = append(choice.Frontier, lanes.Scored{
			ID: lanes.ID{Model: model, Lane: lane}, TTFT: 2, Rate: 2000, Price: 0.01,
		})
	}
	return choice
}

// vetoEverythingBut is the strike ledger as it stood at 19:32: every other
// machine this process knew was held off for a wait it had been told, so the
// set the router was left with was the one machine not on the list — "0
// endpoints out of 1 requested". The chooser's frontier is a different list
// and still named the vetoed machines as places to walk, which is exactly how
// the walk came to demand them.
func vetoEverythingBut(rig *laneRig, vetoed ...string) {
	for _, lane := range vetoed {
		rig.client.velocity.pace(rig.model, lane, time.Minute)
	}
}

// TestTheMeasuredRefusalRaceEndsInAnAnswerOnTheFirstAttempt is A1: the exact
// §1 sequence — policy 404 on the primary, policy 404 on the first demanded
// rescue, an accepted stream that 429s on the second — ends in an answer from
// the router's free choice on the FIRST call, with the ladder narrated, and
// never hands the caller the 404.
func TestTheMeasuredRefusalRaceEndsInAnAnswerOnTheFirstAttempt(t *testing.T) {
	rig := newLaneRig(t, "recovery/measured", measuredLanes(true)...)

	vetoEverythingBut(rig, "DeepSeek", "Io Net", "Novita")
	told := &notices{}
	ctx := WithStreamObserver(WithLaneChoice(talking(), measuredChoice(rig.model)), told.observe)
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("what eems like early names emerging"))
	if err != nil {
		if refusal, ok := RefusalFrom(err); ok && refusal.Status == 404 {
			t.Fatalf("the caller was handed the router's 404: %v", err)
		}
		t.Fatalf("the measured race ended without an answer: %v", err)
	}
	if got := answerTokens(response); got != 31 {
		t.Fatalf("the answer is %d tokens, want Novita's 31 from the router's free choice", got)
	}

	var said []string
	for _, event := range told.kinds(StreamNotice) {
		said = append(said, event.Delta)
	}
	if !containsLine(said, "relaxed the endpoint filter") {
		t.Fatalf("notices = %q, want the ladder's `Retry n/N: relaxed the endpoint filter`", said)
	}
	for _, line := range said {
		if strings.Contains(line, "API error") {
			t.Fatalf("a notice carried the raw wire error: %q", line)
		}
	}

	// NO EXCLUDED MACHINE IS ASKED TWICE. DeepSeek was demanded once by the
	// walk; Fireworks was never demanded at all (the primary's set was a veto
	// list, not a demand), and nothing that came after may demand either.
	if got := rig.server.Requests("DeepSeek"); got != 1 {
		t.Fatalf("Requests(DeepSeek) = %d, want exactly the one demand the walk made", got)
	}
	if got := rig.server.Requests("Fireworks"); got > 1 {
		t.Fatalf("Requests(Fireworks) = %d, want at most one", got)
	}
	last := rig.server.Asks()[len(rig.server.Asks())-1]
	if len(last.Only) != 0 || len(last.Ignore) != 0 {
		t.Fatalf("the answering request carried only=%v ignore=%v, want the router's free choice", last.Only, last.Ignore)
	}
}

// TestAnExhaustedRaceSettlesOnTheMostActionableError is A3: when the ladder
// cannot land either — nothing but the full pool is left behind the model —
// the caller is handed the 429 an accepted stream produced, never the primary's
// routing 404 that the walk had already acted on.
func TestAnExhaustedRaceSettlesOnTheMostActionableError(t *testing.T) {
	rig := newLaneRig(t, "recovery/actionable", measuredLanes(false)...)

	vetoEverythingBut(rig, "DeepSeek", "Io Net")
	ctx := WithLaneChoice(talking(), measuredChoice(rig.model))
	_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err == nil {
		t.Fatal("the race answered with no machine able to serve it")
	}
	refusal, ok := RefusalFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a typed refusal", err)
	}
	if refusal.Status != 429 {
		t.Fatalf("settled on status %d (%v), want the accepted stream's 429 rather than the primary's 404", refusal.Status, err)
	}
	if !strings.EqualFold(refusal.Provider, "Io Net") {
		t.Fatalf("the 429 names %q, want the pool that was full", refusal.Provider)
	}
}

// TestARoutingRefusalIsTypedForTheCaller is the provider half of A2: the 404
// the router answers when a list or a policy emptied the endpoint set leaves the
// refusal door marked as a ROUTING refusal — another machine or another model
// can serve it — and is therefore never "our own request", which is the reading
// that ended the measured turn. A 400 about the request's own bytes stays ours.
func TestARoutingRefusalIsTypedForTheCaller(t *testing.T) {
	rig := newLaneRig(t, "recovery/typed",
		lanestub.Lane{Name: "Fireworks", AccountExcluded: true, Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000}},
	)
	request, err := rig.client.newRequest(userMessages("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := apiError(404, []byte(`{"error":{"message":"0 endpoints out of 1 requested are available matching your guardrail restrictions and data policy. We removed them for the following reasons (an endpoint may have matched multiple reasons):\nPaid model training violation (account settings): 1 endpoint excluded; configurable at https://openrouter.ai/settings/privacy","code":404,"metadata":{"input_endpoint_count":1,"ineligibility_reasons":[{"reason":"paid-model-training-violation-by-account","endpoint_count":1,"configure_url":"https://openrouter.ai/settings/privacy"}],"failed_routing_step":"Filter by Guardrails"}}}`))
	rig.client.refuseUpstream(request, callKnobs{}, policy, "", 0)
	refusal, ok := RefusalFrom(policy)
	if !ok {
		t.Fatal("the policy refusal did not decode")
	}
	if !refusal.Routing || refusal.OurRequest() {
		t.Fatalf("routing=%v ourRequest=%v, want a routing refusal that is not our own request", refusal.Routing, refusal.OurRequest())
	}
	if !RoutingRefusal(policy) {
		t.Fatal("RoutingRefusal says no about the router emptying the set")
	}
	// THE LADDER'S OWN END IS NOT ONE. A RefusalError is every rung spent, and
	// what it carries is a diagnosis for a person rather than somewhere to go.
	if RoutingRefusal(&RefusalError{Model: rig.model, Refusal: refusal}) {
		t.Fatal("a spent ladder read as a routing refusal with somewhere left to go")
	}

	malformed := apiError(400, []byte(`{"error":{"message":"messages: at least one message is required","code":400}}`))
	rig.client.refuseUpstream(request, callKnobs{}, malformed, "", 0)
	ours, _ := RefusalFrom(malformed)
	if ours.Routing || !ours.OurRequest() {
		t.Fatalf("a 400 about our own bytes read as routing=%v ourRequest=%v", ours.Routing, ours.OurRequest())
	}
}

// TestAnAccountExclusionIsLearnedOnceForEveryModel is A4: after ONE policy 404
// about a demanded machine on model A, no request on model B demands that
// machine — and a client opened on the same home, the way the next process
// would open it, does not demand it either.
func TestAnAccountExclusionIsLearnedOnceForEveryModel(t *testing.T) {
	serving := func() []lanestub.Lane {
		return []lanestub.Lane{
			{Name: "Wafer", Profile: lanestub.Profile{FailWith: 503}},
			{Name: "Fireworks", AccountExcluded: true, Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000}},
			{Name: "Novita", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 13}},
		}
	}
	rig := newLaneRig(t, "recovery/account-a", serving()...)
	modelB := "openrouter/recovery/account-b"
	rig.server.Model(modelB, serving()...)

	// The primary lands on Wafer and fails, and the walk's next machine is
	// Fireworks — which on model A is the one policy 404 this process should
	// ever pay, and on every later model is a machine it must skip.
	walkOnto := func(model string) lanes.Choice {
		choice := lanes.Choice{Order: []string{"Wafer"}}
		for _, lane := range []string{"Wafer", "Fireworks", "Novita"} {
			choice.Frontier = append(choice.Frontier, lanes.Scored{
				ID: lanes.ID{Model: model, Lane: lane}, TTFT: 2, Rate: 2000, Price: 0.01,
			})
		}
		return choice
	}
	ask := func(client *Client, model string) {
		t.Helper()
		ctx := WithLaneChoice(talking(), walkOnto(model))
		if _, err := client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithModel(model)); err != nil {
			t.Fatalf("%s did not answer: %v", model, err)
		}
	}

	ask(rig.client, rig.model)
	if got := rig.server.Requests("Fireworks"); got != 1 {
		t.Fatalf("Requests(Fireworks) after model A = %d, want the one demand that taught us", got)
	}
	ask(rig.client, modelB)
	if got := rig.server.Requests("Fireworks"); got != 1 {
		t.Fatalf("Requests(Fireworks) after model B = %d, want still 1: the account's exclusion holds for every model", got)
	}
	if lanes.Serves(modelB, "Fireworks") {
		t.Fatal("the frontier's gate still believes the account can reach Fireworks for model B")
	}

	// THE NEXT PROCESS. Memory is forgotten the way a restart forgets it; the
	// home is the same, so the file this process wrote is what the next one
	// reads when it wires its sheet.
	lanes.ForgetAccountExclusionsInMemory()
	again, err := NewClient(rankedRoad(Config{APIKey: "test-key", BaseURL: rig.server.URL(), Model: modelB}))
	if err != nil {
		t.Fatal(err)
	}
	if lanes.Serves(modelB, "Fireworks") {
		t.Fatalf("a client opened on the same home (%s) believes the account can reach Fireworks again", home.Dir())
	}
	ask(again, modelB)
	if got := rig.server.Requests("Fireworks"); got != 1 {
		t.Fatalf("Requests(Fireworks) after the second client = %d, want still 1", got)
	}
}

func containsLine(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// TestARescueOnAFullPoolGoesBackToTheRaceAtOnce is the live replay's lesson: a
// rescue demands one machine, and when that machine answers 429 in its own
// name, retrying the same body is the same request to the same queue. The race
// takes it back immediately and climbs the ladder it owes, and the full pool is
// asked once.
func TestARescueOnAFullPoolGoesBackToTheRaceAtOnce(t *testing.T) {
	rig := newLaneRig(t, "recovery/full-pool",
		lanestub.Lane{Name: "DeepSeek", AccountExcluded: true, Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000}},
		lanestub.Lane{Name: "Fireworks", Profile: lanestub.Profile{Paced: true}},
		lanestub.Lane{Name: "Novita", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 19}},
	)
	vetoEverythingBut(rig, "Fireworks", "Novita")
	choice := lanes.Choice{Order: []string{"DeepSeek"}}
	for _, lane := range []string{"DeepSeek", "Fireworks"} {
		choice.Frontier = append(choice.Frontier, lanes.Scored{
			ID: lanes.ID{Model: rig.model, Lane: lane}, TTFT: 2, Rate: 2000, Price: 0.01,
		})
	}
	began := time.Now()
	response, err := rig.client.CompleteWithMessages(WithLaneChoice(talking(), choice), userMessages("hello"))
	if err != nil {
		t.Fatalf("the race did not land: %v", err)
	}
	if got := answerTokens(response); got != 19 {
		t.Fatalf("the answer is %d tokens, want Novita's 19 from the router's free choice", got)
	}
	// The full pool is DEMANDED once. (The router's own free choice may still
	// try it and fall past it on the relaxed request; that is the router's
	// business and costs this process nothing.)
	demanded := 0
	for _, ask := range rig.server.Asks() {
		if demandedOnly(ask, "Fireworks") {
			demanded++
		}
	}
	if demanded != 1 {
		t.Fatalf("Fireworks was demanded %d times, want once and never waited on", demanded)
	}
	if spent := time.Since(began); spent >= time.Second {
		t.Fatalf("the race took %s, want no 429 backoff spent on a queue the rescue could not leave", spent)
	}
}

// TestAPinOnAMachineTheAccountExcludesIsRetiredOnEveryModel is the account's
// exclusion meeting a person's own strict pin. On model A the pin is refused,
// retired and widened exactly as before — and the account learns the machine.
// On model B the pin is retired BEFORE it is sent, with the same sentence,
// rather than paying the identical 404. Pinning it again is the person saying
// "try again", and the next request demands it.
func TestAPinOnAMachineTheAccountExcludesIsRetiredOnEveryModel(t *testing.T) {
	serving := func() []lanestub.Lane {
		return []lanestub.Lane{
			{Name: "DeepSeek", AccountExcluded: true, Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000}},
			{Name: "Novita", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 7}},
		}
	}
	rig := newLaneRig(t, "recovery/pin-a", serving()...)
	modelB := "openrouter/recovery/pin-b"
	rig.server.Model(modelB, serving()...)
	RepinLane(LanePin{Lane: "DeepSeek"})
	t.Cleanup(func() { RepinLane(LanePin{}) })

	demands := func() int {
		count := 0
		for _, ask := range rig.server.Asks() {
			if demandedOnly(ask, "DeepSeek") {
				count++
			}
		}
		return count
	}
	ask := func(model string) *noticeLog {
		t.Helper()
		told := &noticeLog{}
		ctx := WithStreamObserver(talking(), told.observe)
		if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithModel(model)); err != nil {
			t.Fatalf("%s did not answer: %v", model, err)
		}
		return told
	}

	ask(rig.model)
	if got := demands(); got != 1 {
		t.Fatalf("DeepSeek demanded %d times on model A, want the one that taught us", got)
	}
	onB := ask(modelB)
	if got := demands(); got != 1 {
		t.Fatalf("DeepSeek demanded %d times after model B, want still 1: the account excludes it for every model", got)
	}
	if len(onB.retired()) == 0 {
		t.Fatalf("model B's pin was stood down without telling the person; notices = %q", onB.lines)
	}

	RepinLane(LanePin{Lane: "DeepSeek"})
	if !lanes.Serves(modelB, "DeepSeek") {
		t.Fatal("pinning the machine again did not take back the account's exclusion")
	}
}
