package provider

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── AN IGNORE LIST NEVER EMPTIES THE SET THE REQUEST IS SENT TO ─────────────
//
// The strikes narrow the serving set. They may never narrow it to nothing: a
// request whose vetoes cover everything it is allowed to land on is a request
// the router can only refuse, and a refusal this process built for itself is
// not evidence about any endpoint. Every test here asserts the object the wire
// actually carries.

// THE MEASURED SHAPE (issue #584; canary cell pallets-click-3740-do, rows
// 72-75). A demand names one lane and the ledger's veto names the same lane, on
// one request, because the demand is applied after the veto and never looked at
// it. The router answered "All providers have been ignored" in 36ms — before
// any endpoint was asked — and both arms of the hedge were refused inside 40ms.
func TestADemandedLaneIsNotAlsoRefusedOnTheSameRequest(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")
	client.velocity.pace(model, "beta", time.Minute)

	knobs := callKnobs{
		intent:     IntentInteractive,
		laneChoice: &lanes.Choice{Only: []string{"beta"}},
	}
	prefs := client.wirePreferences(model, knobs, &ai.Request{})
	if prefs == nil {
		t.Fatal("no preference object at all, want the demand")
	}
	if !equalStrings(prefs.Only, []string{"beta"}) {
		t.Fatalf("only = %v, want the demanded lane", prefs.Only)
	}
	if namesEndpoint(prefs.Ignore, "beta") {
		t.Fatalf("only = %v alongside ignore = %v: the request demands a lane it also refuses",
			prefs.Only, prefs.Ignore)
	}
}

// The hedge's second arm is the same sentence written somewhere else: it names
// one lane and inherits the ledger's vetoes whole.
func TestAHedgedArmIsNotRefusedByTheVetoItInherits(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")
	client.velocity.pace(model, "beta", time.Minute)

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive, hedgeLane: "beta"}, &ai.Request{})
	if prefs == nil || !equalStrings(prefs.Only, []string{"beta"}) {
		t.Fatalf("preferences = %#v, want the hedged arm's demand", prefs)
	}
	if namesEndpoint(prefs.Ignore, "beta") {
		t.Fatalf("the hedged arm demands %v and refuses %v on one request", prefs.Only, prefs.Ignore)
	}
}

// AND THE ROUTER IS ASKED ONCE, NOT ON EVERY REQUEST. With no demand and no
// denominator, a covering ignore is honest the first time: this process cannot
// tell a veto that narrowed a set of five from one that emptied a set of one.
// The router can, and says so. What the canary measured was that nobody was
// listening — 102 identical instant refusals across 44 cells, every one of them
// this process refusing itself and filing the result as an error.
//
// THIS IS THE DETERMINISTIC REPLICATION of the recurrence. A router-shaped fake
// that serves one endpoint and honours `provider.ignore`: once the ledger has
// struck that lane, every request empties the set, and every request is refused.
func TestACoveringIgnoreIsRefusedOnceAndNeverSentAgain(t *testing.T) {
	const only = "Solo"
	var refusals, servings int
	router := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if contains(ignoredEndpoints(decodedBody(t, request)), only) {
			refusals++
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":404,"message":"All providers have been ` +
				`ignored. To change your default ignored providers, visit your settings."}}`))
			return
		}
		servings++
		_, _ = writer.Write([]byte(answerFrom(only, 10, 0, 0)))
	})

	client := ledgerClient(t, router)
	const model = "vendor/fast-model"
	client.velocity.brisk(model, only)
	client.velocity.pace(model, only, time.Minute)

	ctx := lineage("conversation-thin")
	for attempt := range 4 {
		// The lane is struck again before each request, which is what a pool
		// answering 429 does: the ledger keeps learning the same thing, and the
		// question is whether the request keeps refusing itself over it.
		client.velocity.pace(model, only, time.Minute)
		if _, err := client.CompleteWithMessages(ctx, userMessages("carry on")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if refusals != 1 {
		t.Fatalf("the router refused %d of 4 requests, want the sentence heard once and acted on", refusals)
	}
	if servings < 4 {
		t.Fatalf("%d of 4 requests were served, want every one of them to land", servings)
	}
}

// AND IT IS THE LANE NEAREST FORGIVENESS THAT COMES BACK. Releasing the lane
// whose cooldown expires first is the smallest departure from the ledger's own
// verdict; the lane it is still surer about stays refused.
func TestTheReleasedLaneIsTheOneNearestForgiveness(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")
	client.velocity.pace(model, "alpha", 4*time.Minute)
	client.velocity.pace(model, "beta", time.Minute)
	client.velocity.refuseCoveringIgnore(model)

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	if prefs == nil {
		t.Fatal("no preference object at all")
	}
	if !equalStrings(prefs.Ignore, []string{"alpha"}) {
		t.Fatalf("ignore = %v, want the lane refused for longer kept and the other released", prefs.Ignore)
	}
}

// The single-lane model is the same law with the smallest set: one endpoint,
// one strike, and every request for the next five minutes refused before it
// leaves the process.
func TestOneKnownLaneIsNeverRefusedIntoAnEmptySet(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "only")
	client.velocity.pace(model, "only", time.Minute)
	client.velocity.refuseCoveringIgnore(model)

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	if prefs != nil && len(prefs.Ignore) > 0 {
		t.Fatalf("ignore = %v on a model with one known lane, want nothing refused", prefs.Ignore)
	}
}

// AND A VETO THAT ONLY NARROWS IS UNTOUCHED, which is the clause's whole
// discipline: a lane struck out of a set the router still has machines in is
// the ledger working, and it stays struck.
func TestAVetoThatNarrowsTheSetIsLeftAlone(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")
	client.velocity.pace(model, "beta", time.Minute)

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	if prefs == nil || !equalStrings(prefs.Ignore, []string{"beta"}) {
		t.Fatalf("ignore = %#v, want the struck lane still refused while another serves", prefs)
	}
}

// ── AND A REFUSAL THE LIST CAUSED IS THE LIST'S ─────────────────────────────

// The second half of the law, standing on the way back. A request that demanded
// one machine and was answered "all providers have been ignored" was refused by
// a LIST — nothing reached the machine, so nothing is known about it. Striking
// it would pace it, write it out of the serving set, and widen the very list
// that caused the refusal.
func TestALaneIsNotStruckForARefusalAboutAList(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")

	body := []byte(`{"error":{"code":404,"message":"All providers have been ignored. ` +
		`To change your default ignored providers, visit your settings."}}`)
	refusal := client.laneRefusalFor(model, "beta", apiError(404, body))
	if !refusal.Unasked || refusal.struck() {
		t.Fatalf("refusal = %#v, want no machine on the ledger's hook for a refusal about a list", refusal)
	}
	if client.strikeRefusal(model, refusal) {
		t.Fatal("a lane was struck for a list's verdict")
	}
	if _, ignore := client.velocity.preferences(model); len(ignore) != 0 {
		t.Fatalf("ignore = %v after a refusal nobody answered, want the list unchanged", ignore)
	}
	if !lanes.Serves(model, "beta") {
		t.Fatal("the demanded lane was written out of the serving set by a list's verdict")
	}
}

// AND A REFUSAL A MACHINE ACTUALLY GAVE IS STILL THAT MACHINE'S. The clause
// above is about one sentence, not about routing refusals in general.
func TestADemandedLaneIsStillStruckForARefusalItGave(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")

	body := []byte(`{"error":{"code":404,"message":"No endpoints found that support tool use."}}`)
	refusal := client.laneRefusalFor(model, "beta", apiError(404, body))
	if refusal.Lane != "beta" || !refusal.Terminal || refusal.Unasked || !refusal.struck() {
		t.Fatalf("refusal = %#v, want the demanded machine named and on the hook", refusal)
	}
}

// A DEMAND NAMING TWO MACHINES WITH ONE OF THEM VETOED IS ALREADY SERVABLE, and
// nothing is owed to the vetoed one. The law releases a lane only when the whole
// set is covered, so a veto that still leaves the demand somewhere to land
// stands exactly as the ledger wrote it.
func TestADemandWithASpareMachineKeepsTheVetoOnTheOther(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")
	client.velocity.pace(model, "beta", time.Minute)

	knobs := callKnobs{
		intent:     IntentInteractive,
		laneChoice: &lanes.Choice{Only: []string{"alpha", "beta"}},
	}
	prefs := client.wirePreferences(model, knobs, &ai.Request{})
	if prefs == nil || !equalStrings(prefs.Ignore, []string{"beta"}) {
		t.Fatalf("ignore = %#v, want the struck machine still refused while the other serves the demand", prefs)
	}
}

// AND A DEMAND THE VETOES COVER ENTIRELY RELEASES ONE MACHINE, not the list.
func TestACoveredDemandReleasesOneMachineAndKeepsTheRest(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")
	client.velocity.pace(model, "alpha", 4*time.Minute)
	client.velocity.pace(model, "beta", time.Minute)

	knobs := callKnobs{
		intent:     IntentInteractive,
		laneChoice: &lanes.Choice{Only: []string{"alpha", "beta"}},
	}
	prefs := client.wirePreferences(model, knobs, &ai.Request{})
	if prefs == nil || !equalStrings(prefs.Ignore, []string{"alpha"}) {
		t.Fatalf("ignore = %#v, want the machine nearest forgiveness released and the other kept", prefs)
	}
}

// AND THE MEMO IS NOT TAKEN FROM SOMEBODY ELSE'S LIST. The same sentence is
// what the router says when the ignored providers on an ACCOUNT empty the set;
// a refusal this process had no hand in teaches it nothing about its own list.
func TestARefusalWithNoVetoOfOursTeachesNothing(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")

	if client.velocity.holdsVetoes(model) {
		t.Fatal("a healthy ledger reported a veto in play")
	}
	if client.velocity.coveringIgnoreRefused(model) {
		t.Fatal("the memo was taken before anything was refused")
	}
	// And asking the question moved nothing: a diagnostic read on the way back
	// from a refusal must not expire a cooldown or redraw a sampled choice.
	client.velocity.pace(model, "alpha", time.Minute)
	if !client.velocity.holdsVetoes(model) {
		t.Fatal("a struck lane was not reported as a veto in play")
	}
	if _, ignore := client.velocity.preferences(model); !equalStrings(ignore, []string{"alpha"}) {
		t.Fatalf("ignore = %v after the question was asked, want the ledger untouched by it", ignore)
	}
}

// AND A PIN NOBODY CAN USE IS STILL STOOD DOWN. The machine is spared because it
// never got the request; the DEMAND is finished either way, so the pin path
// still sees a terminal refusal naming it (issues #456 and #533).
func TestAPinIsStillRetiredWhenAListEmptiedTheSet(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "alpha")
	client.velocity.brisk(model, "beta")

	body := []byte(`{"error":{"code":404,"message":"All providers have been ignored. ` +
		`To change your default ignored providers, visit your settings."}}`)
	refusal := client.laneRefusalFor(model, "beta", apiError(404, body))
	if refusal.Lane != "beta" || !refusal.Terminal {
		t.Fatalf("refusal = %#v, want the demand named and finished so the pin can stand down", refusal)
	}
	if !refusal.Unasked {
		t.Fatalf("refusal = %#v, want it marked as a machine that was never asked", refusal)
	}
	if refusal.struck() {
		t.Fatal("a machine that never got the request is on the ledger's hook for it")
	}
	if client.strikeRefusal(model, refusal) {
		t.Fatal("a lane was struck for a list's verdict")
	}
	if _, ignore := client.velocity.preferences(model); len(ignore) != 0 {
		t.Fatalf("ignore = %v after a refusal nobody answered, want the list unchanged", ignore)
	}
	if !lanes.Serves(model, "beta") {
		t.Fatal("the demanded lane was written out of the serving set by a list's verdict")
	}
}

// ── THE ROUTER'S REFUSAL NARROWS THE DENOMINATOR ───────────────────────────

// THE ISSUE'S MEASURED SHAPE has two known machines, one excluded by the
// account and the other struck by this process. Neither list empties the set on
// its own; together they do, and the router is the only witness that can say
// so. That sentence is paid for once, and every later request lands directly.
func TestAnAccountExclusionAndOneVetoAreRefusedOnceAndNeverSentAgain(t *testing.T) {
	const excluded, vetoed = "Alpha", "Bravo"
	var refusals, servings int
	router := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if contains(ignoredEndpoints(decodedBody(t, request)), vetoed) {
			refusals++
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":404,"message":"All providers have been ` +
				`ignored. To change your default ignored providers, visit your settings."}}`))
			return
		}
		servings++
		_, _ = writer.Write([]byte(answerFrom(vetoed, 10, 0, 0)))
	})

	client := ledgerClient(t, router)
	const model = "vendor/fast-model"
	client.velocity.brisk(model, excluded)
	client.velocity.brisk(model, vetoed)

	ctx := lineage("conversation-account-exclusion")
	for attempt := range 4 {
		// The lane is struck again before each request, which is what a pool
		// answering 429 does while the account continues to exclude the other.
		client.velocity.pace(model, vetoed, time.Minute)
		if _, err := client.CompleteWithMessages(ctx, userMessages("carry on")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if refusals != 1 {
		t.Fatalf("the router refused %d of 4 requests, want the account's exclusion learned after one sentence", refusals)
	}
	if servings < 4 {
		t.Fatalf("%d of 4 requests were served, want every one of them to land", servings)
	}
}

// A KNOWN MACHINE THE ROUTER WOULD NOT SEND TO IS NOT PART OF THE SET. Once
// the refusal has identified Alpha as unreachable, Bravo is the entire
// denominator and its covering veto is released.
func TestALaneTheRouterWillNotSendToLeavesTheDenominator(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "Alpha")
	client.velocity.brisk(model, "Bravo")
	client.velocity.pace(model, "Bravo", time.Minute)
	client.velocity.refuseCoveringIgnore(model)
	client.velocity.learnUnreachable(model)

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	if prefs == nil {
		t.Fatal("no preference object at all")
	}
	if len(prefs.Ignore) != 0 {
		t.Fatalf("ignore = %v, want Bravo released after Alpha left the serving set", prefs.Ignore)
	}
}

// THE LEDGER'S OWN VETOES TEACH IT NOTHING ABOUT REACHABILITY. Both names were
// under a live cooldown when the refusal arrived, so both remain in the set and
// only the one nearest forgiveness is released.
func TestALaneUnderVetoIsNeverLearnedUnreachable(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "Alpha")
	client.velocity.brisk(model, "Bravo")
	client.velocity.pace(model, "Alpha", 4*time.Minute)
	client.velocity.pace(model, "Bravo", time.Minute)
	client.velocity.refuseCoveringIgnore(model)
	client.velocity.learnUnreachable(model)

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	if prefs == nil || !equalStrings(prefs.Ignore, []string{"Alpha"}) {
		t.Fatalf("ignore = %#v, want both vetoed lanes kept in the denominator and Bravo released", prefs)
	}
}

// A DEMANDED REFUSAL IS DRIVEN THROUGH THE REAL SEAM. The first request names
// Alpha under `provider.only`, the fake router refuses that set, and the
// ladder's widened request answers without naming a machine so no later
// observation can conceal what the refusal taught.
func TestADemandedRefusalTeachesNothingAboutTheOtherMachines(t *testing.T) {
	const demanded, vetoed = "Alpha", "Bravo"
	var sawDemand bool
	router := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		body := decodedBody(t, request)
		prefs, _ := body["provider"].(map[string]any)
		if contains(words(prefs["only"]), demanded) {
			sawDemand = true
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":404,"message":"All providers have been ` +
				`ignored. To change your default ignored providers, visit your settings."}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"vendor/fast-model",` +
			`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],` +
			`"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	})

	client := ledgerClient(t, router)
	const model = "vendor/fast-model"
	client.velocity.brisk(model, demanded)
	client.velocity.brisk(model, vetoed)
	client.velocity.pace(model, vetoed, time.Minute)
	ctx := WithLaneChoice(lineage("conversation-demanded-refusal"), lanes.Choice{Only: []string{demanded}})
	if _, err := client.CompleteWithMessages(ctx, userMessages("carry on")); err != nil {
		t.Fatalf("the ladder did not recover the demanded refusal: %v", err)
	}
	if !sawDemand {
		t.Fatal("the refused request did not carry its demand on the wire")
	}

	client.velocity.pace(model, vetoed, time.Minute)
	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	if prefs == nil || !equalStrings(prefs.Ignore, []string{vetoed}) {
		t.Fatalf("ignore = %#v, want Alpha still counted after a refusal of its demanded set", prefs)
	}
}

// AN ANSWERING MACHINE IS REACHABLE AGAIN AT ONCE. Alpha's answer clears what
// the router taught, so Bravo's veto only narrows a two-machine set and stands.
func TestAnAnsweringMachineIsBackInTheDenominator(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "Alpha")
	client.velocity.brisk(model, "Bravo")
	client.velocity.pace(model, "Bravo", time.Minute)
	client.velocity.refuseCoveringIgnore(model)
	client.velocity.learnUnreachable(model)
	client.velocity.brisk(model, "Alpha")

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	if prefs == nil || !equalStrings(prefs.Ignore, []string{"Bravo"}) {
		t.Fatalf("ignore = %#v, want Bravo's veto left standing after Alpha answered", prefs)
	}
}

// NO MACHINE LEAVES BEFORE THE ROUTER SAYS THE SET WAS EMPTY. A healthy
// ledger's finished object stays byte-for-byte the one it wrote before this
// learning existed.
func TestNothingLeavesTheDenominatorBeforeTheRouterHasSaidSo(t *testing.T) {
	client, _ := routedClient(t, RoutingLatency, answered(plainAnswer))
	const model = "vendor/fast-model"
	client.velocity.brisk(model, "Alpha")
	client.velocity.brisk(model, "Bravo")
	client.velocity.pace(model, "Bravo", time.Minute)

	prefs := client.wirePreferences(model, callKnobs{intent: IntentInteractive}, &ai.Request{})
	encoded, err := json.Marshal(prefs)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sort":"latency","order":["Alpha"],"ignore":["Bravo"],"allow_fallbacks":true,"require_parameters":true}`
	if string(encoded) != want {
		t.Fatalf("provider preferences = %s, want the unchanged healthy object %s", encoded, want)
	}
}
