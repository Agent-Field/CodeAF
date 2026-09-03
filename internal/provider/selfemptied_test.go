package provider

import (
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
	if refusal.Lane != "" || refusal.Terminal {
		t.Fatalf("refusal = %#v, want no machine named by a refusal about a list", refusal)
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
	if refusal.Lane != "beta" || !refusal.Terminal {
		t.Fatalf("refusal = %#v, want the demanded machine named", refusal)
	}
}
