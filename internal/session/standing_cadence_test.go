package session

// A PROBE'S CADENCE CANNOT BE FASTER THAN THE PASS THAT TAKES IT.
//
// There is exactly one clock in this build: [standing.Interval] drives the OS
// timer and every window ticker alike, and a probe_every is only the gate that
// says whether a pass may take the look once it is already running. A value
// below the interval is not a timer of its own, so a card promising one would
// be a cadence the machinery cannot reach. These tests pin the fix at the tool
// compile boundary: a NEW look below the floor is refused in plain words and
// never silently widened, a look at or above it is kept exactly, and an item
// that already stands with a shorter one stays readable.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// probeCall is one probe proposal carrying a cadence, with everything else the
// compiler needs filled in.
func probeCall(every string) standArguments {
	parsed := standArguments{Words: "tell me when the endpoint is ready", WhenWords: "when the endpoint is ready"}
	parsed.When.Kind = "probe"
	parsed.When.Probe.Command = "curl -s localhost:8080/ready"
	parsed.When.ProbeEvery = every
	parsed.Does.Kind = "say"
	parsed.Does.Say = "the endpoint is ready"
	return parsed
}

// A LOOK BELOW THE FLOOR IS REFUSED, NOT CLAMPED. The refusal names the real
// cadence so the model can offer an actual [standing.Interval] card or say the
// faster one is unsupported, and it leaves NOTHING behind: a widened item would
// be this build deciding a cadence the person did not ask for.
func TestAProbeCadenceBelowThePassIsRefusedAndNotWidened(t *testing.T) {
	agent, _ := ordersAgent(t)
	for _, every := range []string{"1m", "59s", "4m59s", "0s", "-5m"} {
		t.Run(every, func(t *testing.T) {
			item, problem := agent.standingItem(probeCall(every), time.Now())
			if problem == "" {
				t.Fatalf("a %s look was admitted: probeEvery = %s", every, item.When.ProbeEvery)
			}
			if !strings.Contains(problem, standing.IntervalWords()) {
				t.Fatalf("the refusal does not name the real cadence %q: %q", standing.IntervalWords(), problem)
			}
			if !strings.Contains(problem, standing.Interval.String()) {
				t.Fatalf("the refusal does not say what to send instead (%q): %q", standing.Interval.String(), problem)
			}
			// The dash is the em dash itself and never its escape spelled out: the
			// model reads this text, and a backslash-u run is a build slip nobody
			// would catch by reading the Go.
			if strings.Contains(problem, `\u2014`) {
				t.Fatalf("the refusal leaked an escape instead of a dash: %q", problem)
			}
			// NO SILENT ITEM. A refused call compiles no item at all, so there is
			// nothing carrying a widened cadence into the store.
			if item.When.ProbeEvery != 0 || item.Words != "" {
				t.Fatalf("a refused look left a compiled item behind: %+v", item)
			}
		})
	}
}

// THE FLOOR ITSELF AND ANYTHING WIDER ARE KEPT EXACTLY. The person's cadence is
// never rounded up to the floor, because five minutes asked for is five minutes.
func TestAProbeCadenceAtOrAboveThePassIsKeptExactly(t *testing.T) {
	agent, _ := ordersAgent(t)
	for _, probe := range []struct {
		every string
		want  time.Duration
	}{
		{standing.Interval.String(), standing.Interval},
		{"10m", 10 * time.Minute},
		{"1h", time.Hour},
	} {
		t.Run(probe.every, func(t *testing.T) {
			item, problem := agent.standingItem(probeCall(probe.every), time.Now())
			if problem != "" {
				t.Fatalf("a %s look was refused: %s", probe.every, problem)
			}
			if item.When.ProbeEvery != probe.want {
				t.Fatalf("probeEvery = %s, want %s", item.When.ProbeEvery, probe.want)
			}
		})
	}
}

// A LOOK WITH NO CADENCE RIDES THE PASS. The empty default is [standing.Interval]
// and not a zero a later pass would have to guess about.
func TestAProbeWithNoCadenceDefaultsToThePass(t *testing.T) {
	agent, _ := ordersAgent(t)
	item, problem := agent.standingItem(probeCall(""), time.Now())
	if problem != "" {
		t.Fatalf("a look with no cadence was refused: %s", problem)
	}
	if item.When.ProbeEvery != standing.Interval {
		t.Fatalf("an empty probe_every compiled to %s, want the pass %s", item.When.ProbeEvery, standing.Interval)
	}
}

// THE ONE-SHOT INTENT IS UNTOUCHED BY THE FLOOR. A look at the floor with once
// set still carries the compiled intent onto the card and the document.
func TestAProbeAtTheFloorStillCarriesOnce(t *testing.T) {
	agent, _ := ordersAgent(t)
	parsed := probeCall(standing.Interval.String())
	parsed.When.OneShot = true
	item, problem := agent.standingItem(parsed, time.Now())
	if problem != "" {
		t.Fatalf("a one-shot look at the floor was refused: %s", problem)
	}
	if !item.When.OneShot {
		t.Fatal("the one-shot intent did not ride the item")
	}
	if item.When.ProbeEvery != standing.Interval {
		t.Fatalf("probeEvery = %s, want the pass %s", item.When.ProbeEvery, standing.Interval)
	}
	if !strings.HasPrefix(item.When.Words, "once") {
		t.Fatalf("the one-shot intent is not on the card's when band: %q", item.When.Words)
	}
}

// AN ITEM THAT ALREADY STANDS WITH A SHORTER LOOK STAYS READABLE. The floor is a
// compile-time gate on NEW proposals, not a change to [standing.Item.Validate]:
// a legacy one-minute row must not be quietly rejected out of a folder somebody
// is reading, nor rewritten to a cadence nobody chose.
func TestAStoredShortLookStaysReadable(t *testing.T) {
	store, err := standing.Open(filepath.Join(t.TempDir(), "standing"))
	if err != nil {
		t.Fatalf("cannot open a store: %v", err)
	}
	made, err := store.Create(standing.Item{
		Words:     "keep an eye on the endpoint",
		Workspace: "/tmp/project",
		When: standing.When{
			Kind:       standing.WhenProbe,
			Words:      "every minute",
			Probe:      standing.Probe{Command: "curl -s localhost:8080/ready"},
			ProbeEvery: time.Minute,
			OneShot:    true,
		},
		Does:  standing.Action{Kind: standing.ActionSay, Say: "the endpoint is ready"},
		Rails: standing.Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	})
	if err != nil {
		t.Fatalf("a legacy one-minute probe could not be created: %v", err)
	}
	read, err := store.Get(made.ID)
	if err != nil {
		t.Fatalf("a legacy one-minute probe could not be read back: %v", err)
	}
	if read.When.ProbeEvery != time.Minute {
		t.Fatalf("the stored cadence was rewritten to %s", read.When.ProbeEvery)
	}
	if read.When.OneShot != made.When.OneShot {
		t.Fatalf("the stored one-shot intent was rewritten")
	}
	if err := read.Validate(); err != nil {
		t.Fatalf("the admission law now rejects a row that already stands: %v", err)
	}
}

// THE COMPILE BOUNDARY IS THE TOOL ITSELF. A propose call assembled the way the
// model sends it is refused with the failed flag set, so nothing reaches the
// card with an impossible cadence.
func TestTheStandToolRefusesAShortProbeBeforeAnyCard(t *testing.T) {
	agent, _ := ordersAgent(t)
	args := json.RawMessage(`{"op":"propose","words":"tell me when the endpoint is ready","when":{"kind":"probe","probe":{"command":"true"},"probe_every":"1m"},"does":{"kind":"say","say":"the endpoint is ready"}}`)
	out, failed, err := agent.standDispatch(t.Context(), args)
	if err != nil {
		t.Fatalf("the dispatch returned a Go error: %v", err)
	}
	if !failed {
		t.Fatalf("a short probe was not a failed call: %q", out)
	}
	if !strings.Contains(out, standing.IntervalWords()) {
		t.Fatalf("the refusal does not name the real cadence: %q", out)
	}
}

// THE SCHEMA CARRIES THE FLOOR. A model cannot be expected to respect a minimum
// the field description never states, so the contract for probe_every names it
// beside the other two fields that state theirs.
func TestTheProbeEverySchemaStatesTheFloor(t *testing.T) {
	if !strings.Contains(standSchemaJSON, `"probe_every"`) {
		t.Fatal("the stand schema has no probe_every field")
	}
	if !strings.Contains(standSchemaJSON, "faster than every 5 minutes") {
		t.Fatalf("the probe_every field does not state the floor:\n%s", standSchemaJSON)
	}
}
