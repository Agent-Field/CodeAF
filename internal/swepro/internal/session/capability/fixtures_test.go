package capability

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

// Golden-fixture replay against testdata/fixtures.json, produced from the real
// src/session/capability.ts by tools/fixtures/gen-capability.ts. The gate is
// byte-for-byte equality between jscompat.Stringify(goResult) and the TS
// JSON.stringify output — not a tolerance compare — so any drift in the decay
// accumulation order, the cross-band exponents, the running-minimum projection,
// the streak arithmetic, the snapshot's JS property order or the tolerant JSONL
// reader fails loudly.

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	var out []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fx fixture
		if err := json.Unmarshal([]byte(line), &fx); err != nil {
			t.Fatalf("decode fixture line %q: %v", line, err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func stringify(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

// fixtureOp mirrors the instruction stream documented in gen-capability.ts.
type fixtureOp struct {
	Op                 string          `json:"op"`
	Outcome            json.RawMessage `json:"outcome"`
	Model              *ModelRef       `json:"model"`
	Band               *string         `json:"band"`
	Threshold          *float64        `json:"threshold"`
	ThresholdNonFinite *string         `json:"thresholdNonFinite"`
	Snap               json.RawMessage `json:"snap"`
}

// throwSentinel is what the generator records for an op that raised — here, the
// uncaught TypeError from `PRIOR_MEAN[tier][band]`.
const throwSentinel = "__throw__"

var nonFiniteValues = map[string]float64{
	"NaN":       math.NaN(),
	"Infinity":  math.Inf(1),
	"-Infinity": math.Inf(-1),
}

// applyNonFinite re-injects option values JSON.stringify cannot carry.
func applyNonFinite(t *testing.T, opts *CapabilityOptions, spec map[string]string) {
	t.Helper()
	for key, name := range spec {
		v, ok := nonFiniteValues[name]
		if !ok {
			t.Fatalf("unknown non-finite spec %q", name)
		}
		f := v
		switch key {
		case "decay":
			opts.Decay = &f
		case "priorStrength":
			opts.PriorStrength = &f
		case "crossBandStrong":
			opts.CrossBandStrong = &f
		case "crossBandWeak":
			opts.CrossBandWeak = &f
		case "promotionStreakK":
			opts.PromotionStreakK = &f
		case "promotionStreakBonus":
			opts.PromotionStreakBonus = &f
		default:
			t.Fatalf("unknown non-finite option %q", key)
		}
	}
}

// runOp executes one op, converting a panic into the throw sentinel the way the
// generator's try/catch does.
func runOp(tr *CapabilityTracker, op fixtureOp) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = throwSentinel
		}
	}()
	switch op.Op {
	case "observe":
		var o LeafOutcome
		if err := json.Unmarshal(op.Outcome, &o); err != nil {
			panic(err)
		}
		tr.Observe(o)
		return nil
	case "restore":
		if len(op.Snap) == 0 || string(op.Snap) == "null" {
			tr.Restore(nil)
			return nil
		}
		var snap CapabilitySnapshot
		if err := json.Unmarshal(op.Snap, &snap); err != nil {
			panic(err)
		}
		tr.Restore(&snap)
		return nil
	case "pSuccess":
		return jscompat.JSNumber(tr.PSuccess(*op.Model, sizeband.SizeBand(*op.Band)))
	case "curve":
		out := make([]jscompat.JSNumber, len(BANDS))
		for i, b := range BANDS {
			out[i] = jscompat.JSNumber(tr.PSuccess(*op.Model, b))
		}
		return out
	case "maxReliableBand":
		if op.ThresholdNonFinite != nil {
			v, ok := nonFiniteValues[*op.ThresholdNonFinite]
			if !ok {
				panic("unknown non-finite threshold " + *op.ThresholdNonFinite)
			}
			return tr.MaxReliableBand(*op.Model, v)
		}
		if op.Threshold != nil {
			return tr.MaxReliableBand(*op.Model, *op.Threshold)
		}
		return tr.MaxReliableBand(*op.Model)
	case "hasObservations":
		return tr.HasObservations(*op.Model)
	case "snapshot":
		return tr.Snapshot()
	}
	panic("unknown op " + op.Op)
}

func runOps(tr *CapabilityTracker, ops []fixtureOp) []any {
	out := make([]any, 0, len(ops))
	for _, op := range ops {
		out = append(out, runOp(tr, op))
	}
	return out
}

func writeJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "outcomes.jsonl")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return file
}

func decodeArgs(t *testing.T, argsJSON string) []json.RawMessage {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		t.Fatalf("decode args %q: %v", argsJSON, err)
	}
	return raw
}

func TestFixtures(t *testing.T) {
	// The generator deletes CODEAF_ADAPTIVE_CUTS, and one fixture leaves
	// `adaptiveCutsEnabled` absent so the live env read is exercised.
	if v, ok := os.LookupEnv("CODEAF_ADAPTIVE_CUTS"); ok {
		if err := os.Unsetenv("CODEAF_ADAPTIVE_CUTS"); err != nil {
			t.Fatalf("unsetenv: %v", err)
		}
		t.Cleanup(func() { _ = os.Setenv("CODEAF_ADAPTIVE_CUTS", v) })
	}

	fixtures := loadFixtures(t)
	if len(fixtures) < 60 {
		t.Fatalf("expected at least 60 fixture cases, got %d", len(fixtures))
	}

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			args := decodeArgs(t, fx.ArgsJSON)
			var got any

			switch fx.Fn {
			case "BANDS":
				got = BANDS

			case "scenario":
				if len(args) < 2 {
					t.Fatalf("scenario expects >=2 args, got %d", len(args))
				}
				var opts CapabilityOptions
				if err := json.Unmarshal(args[0], &opts); err != nil {
					t.Fatalf("decode opts: %v", err)
				}
				var ops []fixtureOp
				if err := json.Unmarshal(args[1], &ops); err != nil {
					t.Fatalf("decode ops: %v", err)
				}
				if len(args) > 2 {
					var spec map[string]string
					if err := json.Unmarshal(args[2], &spec); err != nil {
						t.Fatalf("decode nonFinite: %v", err)
					}
					applyNonFinite(t, &opts, spec)
				}
				got = runOps(NewCapabilityTracker(&opts), ops)

			case "loadOutcomes":
				var lines []string
				if err := json.Unmarshal(args[0], &lines); err != nil {
					t.Fatalf("decode lines: %v", err)
				}
				got = LoadOutcomes(writeJSONL(t, lines))

			case "loadOutcomes:path":
				var path string
				if err := json.Unmarshal(args[0], &path); err != nil {
					t.Fatalf("decode path: %v", err)
				}
				got = LoadOutcomes(path)

			case "capabilityFromRun":
				var lines []string
				if err := json.Unmarshal(args[0], &lines); err != nil {
					t.Fatalf("decode lines: %v", err)
				}
				var opts CapabilityOptions
				if err := json.Unmarshal(args[1], &opts); err != nil {
					t.Fatalf("decode opts: %v", err)
				}
				var ops []fixtureOp
				if err := json.Unmarshal(args[2], &ops); err != nil {
					t.Fatalf("decode ops: %v", err)
				}
				got = runOps(CapabilityFromRun(writeJSONL(t, lines), &opts), ops)

			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			if s := stringify(t, got); s != fx.OutJSON {
				t.Errorf("parity mismatch\n  fn:   %s\n  args: %s\n  want: %s\n  got:  %s", fx.Fn, fx.ArgsJSON, fx.OutJSON, s)
			}
		})
	}
}
