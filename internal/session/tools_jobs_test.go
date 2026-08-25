package session

// The foreground timeout law, read from the wire args the model actually sends.
// Nothing here starts a process: the law is a rewrite of one JSON object, and
// the fault it exists to stop was a value that rode through that rewrite
// untouched.

import (
	"encoding/json"
	"testing"
)

// timeoutOf reads back the timeout the law left on one call's arguments, and
// says whether it left one at all. A missing key and a null are the same
// answer here on purpose: both are a call bare would arm no timer for.
func timeoutOf(t *testing.T, args json.RawMessage) (float64, bool) {
	t.Helper()
	var fields struct {
		Timeout *float64 `json:"timeout"`
	}
	if err := json.Unmarshal(args, &fields); err != nil {
		t.Fatalf("the law returned arguments that do not decode: %v (%s)", err, args)
	}
	if fields.Timeout == nil {
		return 0, false
	}
	return *fields.Timeout, true
}

// AN UNSET TIMEOUT IS EVERY WAY OF NOT SAYING ONE. A model that spells its
// optional arguments out — `"timeout": null` — used to defeat the law entirely:
// the null took the "set" branch, decoded as zero, passed the cap test and
// reached bare as a nil timeout, which arms no timer at all. An unbounded
// foreground call is a turn that never ends.
func TestAnUnreadableTimeoutIsTheSameAsNoTimeoutAtAll(t *testing.T) {
	for _, c := range []struct {
		which string
		args  string
	}{
		{"no timeout key at all", `{"command":"cd /x"}`},
		{"an explicit null", `{"command":"cd /x","timeout":null}`},
		{"a zero", `{"command":"cd /x","timeout":0}`},
		{"a negative", `{"command":"cd /x","timeout":-5}`},
		{"a string", `{"command":"cd /x","timeout":"30"}`},
		{"a false", `{"command":"cd /x","timeout":false}`},
	} {
		got, set := timeoutOf(t, withTimeoutLaw(json.RawMessage(c.args)))
		if !set || got != BashCeilingSeconds {
			t.Fatalf("%s left the call bounded by %v (set: %v), want the %d second ceiling",
				c.which, got, set, BashCeilingSeconds)
		}
	}
}

// THE CAP IS THE CAP, and a call already inside the law is not touched.
func TestTheTimeoutLawClampsTooMuchAndLeavesAWorkableFigureAlone(t *testing.T) {
	got, set := timeoutOf(t, withTimeoutLaw(json.RawMessage(`{"command":"go test ./...","timeout":900}`)))
	if !set || got != BashCeilingSeconds {
		t.Fatalf("900 seconds came back as %v (set: %v), want the %d second ceiling",
			got, set, BashCeilingSeconds)
	}
	asked := json.RawMessage(`{"command":"go test ./...","timeout":30}`)
	if got, set := timeoutOf(t, withTimeoutLaw(asked)); !set || got != 30 {
		t.Fatalf("a workable 30 seconds came back as %v (set: %v)", got, set)
	}
	// The bytes themselves ride through: a call that already sits inside the law
	// is not re-marshalled, so nothing the model sent moves under it.
	if out := string(withTimeoutLaw(asked)); out != string(asked) {
		t.Fatalf("a lawful call was rewritten:\n got %s\nwant %s", out, asked)
	}
	// A fraction survives as a fraction. Rounding it down to whole seconds would
	// turn half a second into bare's "Invalid timeout" — a call refused for a
	// figure nobody typed.
	if got, _ := timeoutOf(t, withTimeoutLaw(json.RawMessage(`{"command":"true","timeout":0.5}`))); got != 0.5 {
		t.Fatalf("half a second came back as %v", got)
	}
}

// The surface counts down against this same function (internal/tui3's
// toolLimit), so the figure it returns is the figure the command dies on.
func TestTheBoundTheSurfaceDrawsIsTheBoundTheCommandDiesOn(t *testing.T) {
	for _, c := range []struct {
		args string
		want float64
	}{
		{`{"command":"cd /x"}`, BashCeilingSeconds},
		{`{"command":"cd /x","timeout":null}`, BashCeilingSeconds},
		{`{"command":"cd /x","timeout":0}`, BashCeilingSeconds},
		{`{"command":"cd /x","timeout":900}`, BashCeilingSeconds},
		{`{"command":"cd /x","timeout":30}`, 30},
	} {
		if got := BashTimeoutSeconds(json.RawMessage(c.args)); got != c.want {
			t.Fatalf("%s reads as %v seconds, want %v", c.args, got, c.want)
		}
		// And the two agree: whatever the surface would draw is what the law
		// writes onto the wire, for every call that reaches bare with one.
		if bounded, set := timeoutOf(t, withTimeoutLaw(json.RawMessage(c.args))); set && bounded != c.want {
			t.Fatalf("%s is drawn as %v seconds and run under %v", c.args, c.want, bounded)
		}
	}
	// Arguments that do not decode at all belong to bare's own error wording,
	// not to this wrapper's: the law passes them through untouched, and the row
	// draws the ceiling rather than nothing, because the call it is about to
	// refuse is still a call somebody may be watching.
	if got := BashTimeoutSeconds(json.RawMessage(`not json at all`)); got != BashCeilingSeconds {
		t.Fatalf("undecodable arguments read as %v seconds, want the ceiling", got)
	}
}
