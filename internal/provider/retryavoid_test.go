package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// ── WHAT A RETRY ASKS THE ROUTER TO AVOID ───────────────────────────────────
//
// The caller that owns a retry names the lanes that already failed the call
// (retryavoid.go). These tests assert where the name lands: in `provider.ignore`
// on the next body, beside whatever else the request already asked for, and
// nowhere else — not beside a person's pin, not on a road that asked for no
// preference object, and not on the body of a later call.

// okAnswerHandler is the plain successful answer every fixture in this file
// needs: the ask is what is under test, not the answer.
func okAnswerHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"vendor/fast-model","provider":"Alpha","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
}

// ── THE LANE A FAILURE NAMES ───────────────────────────────────────────────
//
// FailedLane reads the wire fact a retry's veto is built from: who the
// failure implicates, nobody when the failure names nobody. What counts as a
// name is an upstream fault (a 5xx relayed from a named upstream) or a cut
// stream; what does not is a 4xx, a plain error, and — a name is not lost —
// a wrapped one.
func TestFailedLaneNamesWhoTheFailureImplicated(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"a 5xx relayed from a named upstream names it",
			&APIError{Status: 502, Message: "upstream broke", Provider: "Alpha"}, "Alpha"},
		{"the name arrives trimmed",
			&APIError{Status: 500, Message: "upstream broke", Provider: "  Alpha  "}, "Alpha"},
		{"a 4xx names nobody",
			&APIError{Status: 400, Message: "bad ask", Provider: "Alpha"}, ""},
		{"a cut stream names the provider it named",
			&StreamCut{Reason: CutOverrun, Provider: "Alpha"}, "Alpha"},
		{"a plain error names nobody",
			errors.New("connection reset by peer"), ""},
		{"a wrapped 5xx still names its provider",
			fmt.Errorf("after node call attempts: %w",
				&APIError{Status: 503, Message: "upstream broke", Provider: "Beta"}), "Beta"},
	}
	for _, test := range tests {
		if got := FailedLane(test.err); got != test.want {
			t.Errorf("%s: FailedLane = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestARetryAvoidListGoesOutOnTheNextBody(t *testing.T) {
	client, recorded := newTestClient(t, Config{Routing: StaticRouting(RoutingSimple)})
	ctx := WithRetryAvoid(context.Background(), []string{"Alpha"})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs, _ := recorded.body(0)["provider"].(map[string]any)
	ignore, _ := prefs["ignore"].([]any)
	if len(ignore) != 1 || ignore[0] != "Alpha" {
		t.Fatalf("provider = %#v, want ignore carrying the lane the retry named", prefs)
	}
	// The veto alone is the whole ask: no sort word, no ranking, no demand rode
	// beside it, because nothing else about the request wanted a preference.
	for _, field := range []string{"sort", "order", "only", "allow_fallbacks", "require_parameters"} {
		if _, present := prefs[field]; present {
			t.Fatalf("provider = %#v, want nothing beside the veto (saw %q)", prefs, field)
		}
	}
}

func TestARetryAvoidListCarriesEveryLaneItsCallerNamed(t *testing.T) {
	client, recorded := newTestClient(t, Config{Routing: StaticRouting(RoutingSimple)})
	ctx := WithRetryAvoid(context.Background(), []string{"Alpha", "Beta", "alpha", ""})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs, _ := recorded.body(0)["provider"].(map[string]any)
	ignore, _ := prefs["ignore"].([]any)
	if len(ignore) != 2 || ignore[0] != "Alpha" || ignore[1] != "Beta" {
		t.Fatalf("provider = %#v, want the accumulated lanes once each, blanks dropped", prefs)
	}
}

func TestARetryAvoidListRidesBesideARankedRoad(t *testing.T) {
	// A ranked road already composes a preference object; the veto joins it
	// rather than replacing it, so the sort word the row asks for survives.
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false,
		okAnswerHandler())
	ctx := WithRetryAvoid(context.Background(), []string{"Alpha"})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs, _ := recorded.body(0)["provider"].(map[string]any)
	if prefs["sort"] != "latency" {
		t.Fatalf("provider = %#v, want the row's own sort word kept", prefs)
	}
	ignore, _ := prefs["ignore"].([]any)
	if len(ignore) != 1 || ignore[0] != "Alpha" {
		t.Fatalf("provider = %#v, want the veto beside the sort word, not instead of it", prefs)
	}
}

func TestAPersonsPinKeepsTheRetryAvoidListOffTheWire(t *testing.T) {
	pinned(t, LanePin{Lane: "Alpha"})
	client, recorded := pinningClient(t, StaticRouting(RoutingSimple), 0, 0, false,
		okAnswerHandler())
	// The pin's scope is the calls a person reads (lanepin.go), so the pin's
	// own turn is the call to read the row on.
	ctx := WithRole(context.Background(), lanes.RoleTalk)
	ctx = WithRetryAvoid(ctx, []string{"Beta"})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs, _ := recorded.body(0)["provider"].(map[string]any)
	if _, vetoed := prefs["ignore"]; vetoed {
		t.Fatalf("provider = %#v, want a pin's demand untouched by the retry's veto", prefs)
	}
	if only, _ := prefs["only"].([]any); len(only) != 1 || only[0] != "Alpha" {
		t.Fatalf("provider = %#v, want the pin's own demand", prefs)
	}
}

func TestARetryAvoidListStaysOffARoadThatAskedForNone(t *testing.T) {
	// A direct service has one road, which resolves to routing off: the person
	// behind it asked for no preference object at all, and a veto does not
	// overrule that.
	client, recorded := newTestClient(t, Config{Direct: true})
	ctx := WithRetryAvoid(context.Background(), []string{"Alpha"})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(0)["provider"]; present {
		t.Fatalf("request body = %#v, want no preference object on a direct road", recorded.body(0))
	}
	client, recorded = newTestClient(t, Config{Routing: StaticRouting(RoutingOff)})
	ctx = WithRetryAvoid(context.Background(), []string{"Alpha"})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, present := recorded.body(1)["provider"]; present {
		t.Fatalf("request body = %#v, want no preference object under routing off", recorded.body(1))
	}
}

func TestARetryAvoidListDoesNotOutliveTheCallThatNamedIt(t *testing.T) {
	client, recorded := newTestClient(t, Config{Routing: StaticRouting(RoutingSimple)})
	retry := WithRetryAvoid(context.Background(), []string{"Alpha"})
	if _, err := client.CompleteWithMessages(retry, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs, _ := recorded.body(1)["provider"].(map[string]any)
	ignore, _ := prefs["ignore"].([]any)
	if len(ignore) != 0 {
		t.Fatalf("provider = %#v, want the next call's body carrying none of the last call's veto", prefs)
	}
}

func TestARetryAvoidListNeverEmptiesADemandItJoined(t *testing.T) {
	// The choice admitted two machines and the retry's veto named one of them:
	// the admitted set narrows a machine at a time, the veto takes the name, and
	// a machine the demand still holds is never also written into `ignore`.
	client, recorded := pinningClient(t, StaticRouting(RoutingSimple), 0, 0, false,
		okAnswerHandler())
	ctx := WithLaneChoice(context.Background(), lanes.Choice{Only: []string{"Alpha", "Beta"}})
	ctx = WithRetryAvoid(ctx, []string{"Alpha"})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	prefs, _ := recorded.body(0)["provider"].(map[string]any)
	only, _ := prefs["only"].([]any)
	ignore, _ := prefs["ignore"].([]any)
	if len(only) != 1 || only[0] != "Beta" {
		t.Fatalf("provider = %#v, want the demand narrowed to the machine the veto left", prefs)
	}
	if len(ignore) != 1 || ignore[0] != "Alpha" {
		t.Fatalf("provider = %#v, want the vetoed lane beside the narrowed demand", prefs)
	}
}
