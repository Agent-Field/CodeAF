package adaptive

// Focused tests for the pieces the fixture replay exercises only indirectly:
// the JS value model behind `err: unknown`, ModelCandidate's key-order
// round trip, and the production sleeper.

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

func TestErrorTextShapes(t *testing.T) {
	cases := []struct {
		name string
		in   *JSValue
		want string
	}{
		{"nil", nil, ""},
		{"undefined", Undefined(), ""},
		{"null", Null(), ""},
		{"string", Str("boom"), "boom"},
		{"error", Err("boom"), "boom"},
		{"error with truthy cause", func() *JSValue {
			e := Err("outer")
			e.Cause = Err("inner")
			return e
		}(), "outer cause=inner"},
		{"error with falsy cause is bare", func() *JSValue {
			e := Err("outer")
			e.Cause = Num(0) // `cause ? ... : ""` — 0 is falsy
			return e
		}(), "outer"},
		{"error with empty-string cause is bare", func() *JSValue {
			e := Err("outer")
			e.Cause = Str("")
			return e
		}(), "outer"},
		{"nested cause chain", func() *JSValue {
			inner := Err("c")
			mid := Err("b")
			mid.Cause = inner
			outer := Err("a")
			outer.Cause = mid
			return outer
		}(), "a cause=b cause=c"},
		{"number", Num(429), "429"},
		{"NaN stringifies as null", Num(math.NaN()), "null"},
		{"bool", Bool(true), "true"},
		{"object", Obj("code", Num(520), "message", Str("Provider returned error")),
			`{"code":520,"message":"Provider returned error"}`},
		{"object drops undefined values", Obj("a", Num(1), "b", Undefined(), "c", Num(2)),
			`{"a":1,"c":2}`},
		{"array renders holes as null", Arr(Str("x"), Undefined(), Num(3)), `["x",null,3]`},
		{"nested", Obj("inner", Arr(Obj("k", Bool(false)))), `{"inner":[{"k":false}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errorText(tc.in); got != tc.want {
				t.Errorf("errorText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStatusCodeDrivenClassification(t *testing.T) {
	// An Error carrying a numeric `status` is the shape the OpenRouter client
	// throws; statusCodeOf reads it even though the message says nothing.
	if !IsLikelyRateLimit(ErrWithStatus("slow down", 429)) {
		t.Error("status 429 should classify as a rate limit")
	}
	if IsLikelyRateLimit(ErrWithStatus("slow down", 430)) {
		t.Error("status 430 with a benign message should not be a rate limit")
	}
	if !IsLikelyTransientProviderError(ErrWithStatus("kaput", 503)) {
		t.Error("status 503 should classify as transient")
	}
	if IsLikelyTransientProviderError(ErrWithStatus("kaput", 600)) {
		t.Error("status 600 is outside 5xx")
	}
	// The key order matters: `status` is checked before `statusCode`.
	both := Obj("statusCode", Num(503), "status", Num(429))
	if !IsLikelyRateLimit(both) {
		t.Error("status should win over statusCode")
	}
	// A string-valued status is ignored by statusCodeOf (typeof v === "number").
	// The probe must use an Error, whose own props are invisible to errorText —
	// on a plain object JSON.stringify would put "429" into the text and the
	// substring rule would fire anyway.
	stringStatus := Err("kaput")
	stringStatus.Props = jscompat.NewOrderedMap[string, *JSValue]()
	stringStatus.Props.Set("status", Str("429"))
	if IsLikelyRateLimit(stringStatus) {
		t.Error(`status "429" is a string and must be ignored`)
	}
	// ...and the same object as a plain object DOES match, through the text.
	if !IsLikelyRateLimit(Obj("status", Str("429"))) {
		t.Error("a plain object stringifies its props, so 429 is found in the text")
	}
	// A plain object with name AbortError trips the timeout probe, like an
	// actual AbortError instance.
	if !IsLikelyTimeout(Obj("name", Str("AbortError"))) {
		t.Error("an object named AbortError should classify as a timeout")
	}
	named := Err("cancelled")
	named.Name = "AbortError"
	if !IsLikelyTimeout(named) {
		t.Error("an Error renamed AbortError should classify as a timeout")
	}
}

func TestJSStringFallback(t *testing.T) {
	// jsString is errorText's catch arm — unreachable for the modelled kinds,
	// but kept so the port mirrors the TS line for line.
	cases := []struct {
		in   *JSValue
		want string
	}{
		{nil, "undefined"},
		{Undefined(), "undefined"},
		{Null(), "null"},
		{Str("x"), "x"},
		{Num(1.5), "1.5"},
		{Bool(false), "false"},
		{Err("boom"), "Error: boom"},
		{Err(""), "Error"},
		{Arr(Num(1), Null(), Num(2)), "1,,2"},
		{Obj(), "[object Object]"},
	}
	for _, tc := range cases {
		if got := jsString(tc.in); got != tc.want {
			t.Errorf("jsString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	named := Err("nope")
	named.Name = "TypeError"
	if got := jsString(named); got != "TypeError: nope" {
		t.Errorf("jsString(renamed) = %q", got)
	}
}

func TestModelCandidateKeyOrderRoundTrip(t *testing.T) {
	// adaptive.ts produces TWO property orders for the same interface, and both
	// reach output through RouteChoice.candidate. Decoding must preserve
	// whichever order arrived so a re-encode is byte-identical.
	interfaceOrder := `{"id":"openrouter/qwen/a","tier":"high","family":"qwen",` +
		`"prompt_usd_per_mtok":0.325,"completion_usd_per_mtok":1.95,"priority":2}`
	defaultsOrder := `{"id":"openrouter/qwen/a","prompt_usd_per_mtok":0.325,` +
		`"completion_usd_per_mtok":1.95,"tier":"high","family":"qwen","priority":2}`

	for _, raw := range []string{interfaceOrder, defaultsOrder} {
		var c ModelCandidate
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if c.ID != "openrouter/qwen/a" || c.Family != "qwen" || c.Priority != 2 {
			t.Errorf("decoded fields wrong: %+v", c)
		}
		encoded, err := jscompat.Stringify(c)
		if err != nil {
			t.Fatalf("stringify: %v", err)
		}
		if string(encoded) != raw {
			t.Errorf("round trip lost the key order\n  in:  %s\n  out: %s", raw, encoded)
		}
	}

	// A zero-value candidate falls back to the interface order.
	encoded, err := jscompat.Stringify(ModelCandidate{ID: "x", Tier: ModelTierLow, Family: "x"})
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	want := `{"id":"x","tier":"low","family":"x","prompt_usd_per_mtok":0,"completion_usd_per_mtok":0,"priority":0}`
	if string(encoded) != want {
		t.Errorf("default order\n  got:  %s\n  want: %s", encoded, want)
	}

	// The two default pools carry the defaults order all the way through
	// normalizeConfig into a RouteChoice.
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{RandomSeed: seed(5)})
	pool := router.CandidatesForTier(ModelTierHigh)
	if len(pool) != 8 {
		t.Fatalf("default high pool size = %d, want 8", len(pool))
	}
	encoded, err = jscompat.Stringify(pool[0])
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	wantFirst := `{"id":"openrouter/deepseek/deepseek-v4-flash-0731","prompt_usd_per_mtok":0.09,` +
		`"completion_usd_per_mtok":0.18,"tier":"high","family":"deepseek","priority":0}`
	if string(encoded) != wantFirst {
		t.Errorf("normalized default candidate\n  got:  %s\n  want: %s", encoded, wantFirst)
	}
}

func TestTryPickResultUnionMarshalling(t *testing.T) {
	ok := TryPickResult{Ok: true, Choice: RouteChoice{Slot: "s", Tier: ModelTierHigh, Reason: "initial"}}
	encoded, err := jscompat.Stringify(ok)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	want := `{"ok":true,"choice":{"slot":"s","tier":"high","candidate":` +
		`{"id":"","tier":"","family":"","prompt_usd_per_mtok":0,"completion_usd_per_mtok":0,"priority":0},` +
		`"score":0,"previous_model":"","switched":false,"reason":"initial"}}`
	if string(encoded) != want {
		t.Errorf("ok arm\n  got:  %s\n  want: %s", encoded, want)
	}

	// The failure arm must not leak a `choice` key, and a nil family slice must
	// marshal as [] rather than null.
	bad := TryPickResult{Ok: false, Reason: "all-acceptable-busy", RetryAfterMs: 1000}
	encoded, err = jscompat.Stringify(bad)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	want = `{"ok":false,"reason":"all-acceptable-busy","excludedFamilies":[],"retryAfterMs":1000}`
	if string(encoded) != want {
		t.Errorf("failure arm\n  got:  %s\n  want: %s", encoded, want)
	}
}

func TestDefaultSleeper(t *testing.T) {
	start := time.Now()
	defaultSleep(5, nil)
	if elapsed := time.Since(start); elapsed < time.Millisecond {
		t.Errorf("defaultSleep(5) returned after %v; expected it to actually wait", elapsed)
	}
	// A non-positive or NaN delay is setTimeout's "run on the next tick".
	defaultSleep(0, nil)
	defaultSleep(math.NaN(), nil)

	// An already-aborted signal short-circuits a long sleep, the way the TS
	// abort listener resolves the promise early.
	signal := NewAbortSignal()
	signal.Abort()
	start = time.Now()
	defaultSleep(60_000, signal)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("aborted sleep took %v; expected an immediate return", elapsed)
	}
	// Abort is idempotent (a second close would panic).
	signal.Abort()
	if !signal.Aborted() {
		t.Error("signal should stay aborted")
	}
	var nilSignal *AbortSignal
	if nilSignal.Aborted() {
		t.Error("a nil signal is never aborted")
	}
	if nilSignal.Done() != nil {
		t.Error("a nil signal has no done channel")
	}
	nilSignal.Abort() // must not panic
}

func TestOnEventPanicIsSwallowed(t *testing.T) {
	// The TS wraps `this.cfg.on_event(event)` in try/catch and swallows.
	pinRuntime(t)
	router := NewAdaptiveModelRouter(AdaptiveRouterConfig{
		HighModels: []ModelCandidate{cand("openrouter/qwen/a")},
		RandomSeed: seed(1),
		OnEvent:    func(AdaptiveRouteEvent) { panic("listener blew up") },
	})
	result := router.TryPick("s", ModelTierHigh)
	event := router.Register(result.Choice, 1, 10, nil) // must not panic
	if event.Attempts != 1 || event.Successes != 1 {
		t.Errorf("event = %+v", event)
	}
}

func TestToUint32SeedCoercion(t *testing.T) {
	cases := []struct {
		in   float64
		want uint32
	}{
		{0, 0},
		{42, 42},
		{42.9, 42},
		{-1, 4294967295},
		{-42.9, 4294967254},
		{4294967296, 0},
		{4294967338, 42},
		{math.NaN(), 0},
		{math.Inf(1), 0},
		{math.Inf(-1), 0},
	}
	for _, tc := range cases {
		if got := toUint32(tc.in); got != tc.want {
			t.Errorf("toUint32(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
