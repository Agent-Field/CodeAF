// Golden-fixture replay for the loop-guard port. Every line of
// testdata/fixtures.json was produced by running the REAL TS module
// (tools/fixtures/gen-loopguard.ts); this test re-runs the same script against
// the Go port and demands byte-for-byte identical JSON.
//
// A fixture case is a script because createLoopGuard is a stateful factory:
//   args_json = [guards, ops]   out_json = [result per op]
// See the generator header for the op grammar.

package loopguard

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixtureCase struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// numArg decodes either a JSON number or the {"$num":"NaN"} sentinel the
// generator uses for values JSON.stringify cannot carry.
type numArg struct {
	Value float64
}

func (n *numArg) UnmarshalJSON(data []byte) error {
	var sentinel struct {
		Num *string `json:"$num"`
	}
	if err := json.Unmarshal(data, &sentinel); err == nil && sentinel.Num != nil {
		switch *sentinel.Num {
		case "NaN":
			n.Value = math.NaN()
		case "Infinity":
			n.Value = math.Inf(1)
		case "-Infinity":
			n.Value = math.Inf(-1)
		default:
			return json.Unmarshal([]byte(*sentinel.Num), &n.Value)
		}
		return nil
	}
	return json.Unmarshal(data, &n.Value)
}

func (n *numArg) float() *float64 {
	if n == nil {
		return nil
	}
	value := n.Value
	return &value
}

type rawOptions struct {
	RepeatCap           *numArg `json:"repeatCap"`
	MaxCyclePeriod      *numArg `json:"maxCyclePeriod"`
	CycleMinOccurrences *numArg `json:"cycleMinOccurrences"`
	MaxCostUsd          *numArg `json:"maxCostUsd"`
	MaxActions          *numArg `json:"maxActions"`
	WarnFraction        *numArg `json:"warnFraction"`
}

func (o *rawOptions) options() LoopGuardOptions {
	if o == nil {
		// The generator writes null for "constructed with no argument", which
		// is the TS default parameter `= {}`.
		return LoopGuardOptions{}
	}
	return LoopGuardOptions{
		RepeatCap:           o.RepeatCap.float(),
		MaxCyclePeriod:      o.MaxCyclePeriod.float(),
		CycleMinOccurrences: o.CycleMinOccurrences.float(),
		MaxCostUsd:          o.MaxCostUsd.float(),
		MaxActions:          o.MaxActions.float(),
		WarnFraction:        o.WarnFraction.float(),
	}
}

type rawAction struct {
	Tool    *string `json:"tool"`
	ArgsKey *string `json:"argsKey"`
	CostUsd *numArg `json:"costUsd"`
}

func (a *rawAction) action() LoopAction {
	out := LoopAction{}
	if a == nil {
		return out
	}
	// `String(action?.tool ?? "")`: null and undefined both collapse to "".
	if a.Tool != nil {
		out.Tool = *a.Tool
	}
	if a.ArgsKey != nil {
		out.ArgsKey = *a.ArgsKey
	}
	out.CostUsd = a.CostUsd.float()
	return out
}

type rawOp struct {
	G        *int            `json:"g"`
	Op       string          `json:"op"`
	Action   *rawAction      `json:"action"`
	Index    *int            `json:"index"`
	Snapshot json.RawMessage `json:"snapshot"`
}

func loadFixtures(t *testing.T) []fixtureCase {
	t.Helper()
	data, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	cases := []fixtureCase{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var fixture fixtureCase
		if err := json.Unmarshal([]byte(line), &fixture); err != nil {
			t.Fatalf("decode fixture line %q: %v", line, err)
		}
		cases = append(cases, fixture)
	}
	if len(cases) == 0 {
		t.Fatal("no fixture cases loaded")
	}
	return cases
}

func runFixtureScript(t *testing.T, argsJSON string) []any {
	t.Helper()

	var args []json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("expected [guards, ops], got %d args", len(args))
	}

	var guardOptions []*rawOptions
	if err := json.Unmarshal(args[0], &guardOptions); err != nil {
		t.Fatalf("decode guard options: %v", err)
	}
	var ops []rawOp
	if err := json.Unmarshal(args[1], &ops); err != nil {
		t.Fatalf("decode ops: %v", err)
	}

	guards := make([]LoopGuard, len(guardOptions))
	for i, raw := range guardOptions {
		guards[i] = CreateLoopGuard(raw.options())
	}

	results := []any{}
	for _, op := range ops {
		index := 0
		if op.G != nil {
			index = *op.G
		}
		guard := guards[index]
		switch op.Op {
		case "observe":
			results = append(results, guard.Observe(op.Action.action()))
		case "snapshot":
			results = append(results, guard.Snapshot())
		case "restoreResult":
			if op.Index == nil {
				t.Fatalf("restoreResult without index")
			}
			previous, ok := results[*op.Index].(LoopGuardSnapshot)
			if !ok {
				t.Fatalf("results[%d] is not a snapshot", *op.Index)
			}
			guard.Restore(&previous)
			results = append(results, nil)
		case "restoreLiteral":
			// An absent key is TS `undefined`, an explicit null is TS `null`;
			// both are the nil pointer.
			if op.Snapshot == nil || string(op.Snapshot) == "null" {
				guard.Restore(nil)
			} else {
				var snapshot LoopGuardSnapshot
				if err := json.Unmarshal(op.Snapshot, &snapshot); err != nil {
					t.Fatalf("decode restore literal %s: %v", op.Snapshot, err)
				}
				guard.Restore(&snapshot)
			}
			results = append(results, nil)
		default:
			t.Fatalf("unknown op %q", op.Op)
		}
	}
	return results
}

func TestFixtureParity(t *testing.T) {
	for _, fixture := range loadFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			if fixture.Fn != "createLoopGuard" {
				t.Fatalf("unknown fn %q", fixture.Fn)
			}
			results := runFixtureScript(t, fixture.ArgsJSON)
			encoded, err := jscompat.Stringify(results)
			if err != nil {
				t.Fatalf("stringify results: %v", err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Errorf("output mismatch\nargs: %s\n go: %s\n ts: %s",
					fixture.ArgsJSON, encoded, fixture.OutJSON)
			}
		})
	}
}

// TestFixtureCoverage keeps the corpus from silently shrinking.
func TestFixtureCoverage(t *testing.T) {
	cases := loadFixtures(t)
	if len(cases) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(cases))
	}
	seen := map[string]bool{}
	for _, fixture := range cases {
		if seen[fixture.Name] {
			t.Errorf("duplicate fixture name %q", fixture.Name)
		}
		seen[fixture.Name] = true
	}
}
