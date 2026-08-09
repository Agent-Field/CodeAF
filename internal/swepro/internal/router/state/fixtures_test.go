package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-routerstate.ts
// from the REAL src/router/state.ts. The gate is BYTE equality between
// jscompat.Stringify(goResult) and the JSON.stringify the TS run recorded —
// and because the recorded value contains the raw `[router] …\n` stderr line,
// it also gates the NDJSON bytes themselves.

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// numSpec decodes the {"$num":"NaN"} sentinel the generator uses for values
// JSON cannot carry natively (NaN, ±Infinity, -0).
type numSpec struct {
	raw json.RawMessage
}

func (n *numSpec) UnmarshalJSON(b []byte) error {
	n.raw = append([]byte(nil), b...)
	return nil
}

func (n numSpec) value(t *testing.T) float64 {
	t.Helper()
	if len(n.raw) == 0 {
		t.Fatalf("missing number in fixture spec")
	}
	if n.raw[0] == '{' {
		var sentinel struct {
			Num string `json:"$num"`
		}
		if err := json.Unmarshal(n.raw, &sentinel); err != nil {
			t.Fatalf("decode sentinel %s: %v", n.raw, err)
		}
		switch sentinel.Num {
		case "NaN":
			return math.NaN()
		case "Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		case "-0":
			return math.Copysign(0, -1)
		}
		t.Fatalf("unknown number sentinel %q", sentinel.Num)
	}
	var value float64
	if err := json.Unmarshal(n.raw, &value); err != nil {
		t.Fatalf("decode number %s: %v", n.raw, err)
	}
	return value
}

type eventSpec struct {
	Slot          string  `json:"slot"`
	Tier          string  `json:"tier"`
	Model         string  `json:"model"`
	PreviousModel string  `json:"previous_model"`
	Switched      bool    `json:"switched"`
	Reason        string  `json:"reason"`
	Score         numSpec `json:"score"`
	ElapsedS      numSpec `json:"elapsed_s"`
	Attempts      numSpec `json:"attempts"`
	Successes     numSpec `json:"successes"`
	Failures      numSpec `json:"failures"`
	RateLimits    numSpec `json:"rate_limits"`
	LatencyEwma   numSpec `json:"latency_ewma"`
	ToksecEwma    numSpec `json:"toksec_ewma"`
	Error         string  `json:"error"`
}

func (s eventSpec) decode(t *testing.T) RouteEvent {
	t.Helper()
	return RouteEvent{
		Slot:          s.Slot,
		Tier:          s.Tier,
		Model:         s.Model,
		PreviousModel: s.PreviousModel,
		Switched:      s.Switched,
		Reason:        s.Reason,
		Score:         jscompat.JSNumber(s.Score.value(t)),
		ElapsedS:      jscompat.JSNumber(s.ElapsedS.value(t)),
		Attempts:      jscompat.JSNumber(s.Attempts.value(t)),
		Successes:     jscompat.JSNumber(s.Successes.value(t)),
		Failures:      jscompat.JSNumber(s.Failures.value(t)),
		RateLimits:    jscompat.JSNumber(s.RateLimits.value(t)),
		LatencyEwma:   jscompat.JSNumber(s.LatencyEwma.value(t)),
		ToksecEwma:    jscompat.JSNumber(s.ToksecEwma.value(t)),
		Error:         s.Error,
	}
}

type listenerSpec struct {
	Name         string   `json:"name"`
	Throws       bool     `json:"throws"`
	Unsubscribes []string `json:"unsubscribes"`
}

type caseSpec struct {
	Name              string         `json:"name"`
	Listeners         []listenerSpec `json:"listeners"`
	PreUnsubscribe    []string       `json:"preUnsubscribe"`
	DoubleUnsubscribe []string       `json:"doubleUnsubscribe"`
	Events            []eventSpec    `json:"events"`
}

type emitOut struct {
	Stderr string   `json:"stderr"`
	Calls  []string `json:"calls"`
}

func runEmitCase(t *testing.T, spec caseSpec) emitOut {
	t.Helper()
	ResetListenersForTesting()
	t.Cleanup(ResetListenersForTesting)

	var sink bytes.Buffer
	previousStderr := Stderr
	Stderr = &sink
	t.Cleanup(func() { Stderr = previousStderr })

	calls := []string{}
	unsubscribe := map[string]func(){}
	fired := map[string]bool{}

	for _, listener := range spec.Listeners {
		listener := listener
		off := OnRouteEvent(func(event RouteEvent) {
			calls = append(calls, fmt.Sprintf("%s:%s", listener.Name, event.Model))
			if len(listener.Unsubscribes) > 0 && !fired[listener.Name] {
				fired[listener.Name] = true
				for _, target := range listener.Unsubscribes {
					if off, ok := unsubscribe[target]; ok {
						off()
					}
				}
			}
			if listener.Throws {
				panic(listener.Name + " exploded")
			}
		})
		unsubscribe[listener.Name] = off
	}

	for _, name := range spec.PreUnsubscribe {
		if off, ok := unsubscribe[name]; ok {
			off()
		}
	}
	for _, name := range spec.DoubleUnsubscribe {
		if off, ok := unsubscribe[name]; ok {
			off()
			off()
		}
	}

	for _, event := range spec.Events {
		EmitRouteEvent(event.decode(t))
	}

	return emitOut{Stderr: sink.String(), Calls: calls}
}

func TestFixtureParity(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	cases := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		cases++
		t.Run(fixture.Name, func(t *testing.T) {
			if fixture.Fn != "emitRouteEvent" {
				t.Fatalf("unknown fn %q", fixture.Fn)
			}
			var spec caseSpec
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &spec); err != nil {
				t.Fatalf("decode args_json: %v", err)
			}

			encoded, err := jscompat.Stringify(runEmitCase(t, spec))
			if err != nil {
				t.Fatalf("stringify go result: %v", err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Errorf("byte parity failure\n  args: %s\n  want: %s\n  got:  %s",
					fixture.ArgsJSON, fixture.OutJSON, encoded)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if cases < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}
