package cutpolicy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/capability"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

// Golden-fixture replay against testdata/fixtures.json, produced from the real
// src/session/cut-policy.ts by tools/fixtures/gen-cutpolicy.ts. The gate is
// byte-for-byte equality between jscompat.Stringify(goResult) and the TS
// JSON.stringify output — not a tolerance compare — so any drift in the
// hand-rolled /^file_scope:\s*.+$/im scanner, in the replacement-string
// expansion, in the bucket insertion order, in the stable band sort, or in a
// decision reason string fails loudly.

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

func decodeArgs(t *testing.T, argsJSON string) []json.RawMessage {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		t.Fatalf("decode args %q: %v", argsJSON, err)
	}
	return raw
}

func decodeInto(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

var nonFiniteValues = map[string]float64{
	"NaN":       math.NaN(),
	"Infinity":  math.Inf(1),
	"-Infinity": math.Inf(-1),
}

// jsNum decodes a fixture number that may be written as one of the non-finite
// STRING tags JSON.stringify cannot carry.
type jsNum float64

func (n *jsNum) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		v, ok := nonFiniteValues[s]
		if !ok {
			return fmt.Errorf("unknown non-finite tag %q", s)
		}
		*n = jsNum(v)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*n = jsNum(f)
	return nil
}

// ---------------------------------------------------------------------------
// decideCut

type trackerSpec struct {
	Opts         capability.CapabilityOptions `json:"opts"`
	Observations []capability.LeafOutcome     `json:"observations"`
}

func (s trackerSpec) build() *capability.CapabilityTracker {
	tr := capability.NewCapabilityTracker(&s.Opts)
	for _, o := range s.Observations {
		tr.Observe(o)
	}
	return tr
}

// decideCutArgs is the recorded `input`: DecideCutInput minus the tracker.
type decideCutArgs struct {
	Task         CutTask               `json:"task"`
	Model        capability.ModelRef   `json:"model"`
	Alternatives []capability.ModelRef `json:"alternatives"`
	Threshold    *float64              `json:"threshold"`
}

// ---------------------------------------------------------------------------
// pickProbeLeaf

// probeLeaf is the leaf shape the generator instantiates the generic with. Key
// order matches the TS object literal.
type probeLeaf struct {
	ID        string            `json:"id"`
	BandIndex jscompat.JSNumber `json:"bandIndex"`
	Real      bool              `json:"real"`
}

type probeLeafSpec struct {
	ID        string `json:"id"`
	BandIndex jsNum  `json:"bandIndex"`
	Real      bool   `json:"real"`
}

// ---------------------------------------------------------------------------
// selectCoalesceGroup

type candSpec struct {
	ID          string   `json:"id"`
	Parent      *string  `json:"parent"`
	ModelID     string   `json:"modelID"`
	BandIndex   jsNum    `json:"bandIndex"`
	FileScope   []string `json:"fileScope"`
	Description string   `json:"description"`
}

type reliableSpec struct {
	Map     map[string]jsNum `json:"map"`
	Default jsNum            `json:"default"`
}

func (s reliableSpec) fn() func(string) float64 {
	return func(modelID string) float64 {
		if v, ok := s.Map[modelID]; ok {
			return float64(v)
		}
		return float64(s.Default)
	}
}

type optsSpec struct {
	MinGroup          *float64 `json:"minGroup"`
	CombinedBandIndex *string  `json:"combinedBandIndex"`
}

// combiner mirrors the named estimators gen-cutpolicy.ts injects.
func combiner(t *testing.T, name string) func(g []CoalesceCandidate) float64 {
	t.Helper()
	switch name {
	case "summing":
		return func(g []CoalesceCandidate) float64 {
			s := 0.0
			for _, c := range g {
				s += float64(c.BandIndex)
			}
			return math.Min(BandToIndex(sizeband.BandXL), s)
		}
	case "nan":
		return func([]CoalesceCandidate) float64 { return math.NaN() }
	case "five":
		return func([]CoalesceCandidate) float64 { return 5 }
	case "zero":
		return func([]CoalesceCandidate) float64 { return 0 }
	case "count":
		return func(g []CoalesceCandidate) float64 { return float64(len(g)) }
	}
	t.Fatalf("unknown combinedBandIndex %q", name)
	return nil
}

// ---------------------------------------------------------------------------

func TestFixtures(t *testing.T) {
	// The generator deletes CODEAF_ADAPTIVE_CUTS, and one decideCut fixture
	// leaves `adaptiveCutsEnabled` absent so the live env read is exercised.
	if v, ok := os.LookupEnv("CODEAF_ADAPTIVE_CUTS"); ok {
		if err := os.Unsetenv("CODEAF_ADAPTIVE_CUTS"); err != nil {
			t.Fatalf("unsetenv: %v", err)
		}
		t.Cleanup(func() { _ = os.Setenv("CODEAF_ADAPTIVE_CUTS", v) })
	}

	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}

	seen := map[string]int{}
	for _, fx := range fixtures {
		fx := fx
		seen[fx.Fn]++
		t.Run(fx.Name, func(t *testing.T) {
			args := decodeArgs(t, fx.ArgsJSON)
			var got any

			switch fx.Fn {
			case "BANDS":
				got = BANDS

			case "bandToIndex":
				var band sizeband.SizeBand
				decodeInto(t, args[0], &band)
				got = jscompat.JSNumber(BandToIndex(band))

			case "bandWithinReliable":
				var band, reliable sizeband.SizeBand
				decodeInto(t, args[0], &band)
				decodeInto(t, args[1], &reliable)
				got = BandWithinReliable(band, reliable)

			case "shouldRootCut":
				var in ShouldRootCutInput
				decodeInto(t, args[0], &in)
				got = ShouldRootCut(in)

			case "partitionFileScope":
				var fileScope []string
				decodeInto(t, args[0], &fileScope)
				got = PartitionFileScope(fileScope)

			case "splitForConcurrency":
				var in SplitForConcurrencyInput
				decodeInto(t, args[0], &in)
				if len(args) > 1 {
					var spec map[string]string
					decodeInto(t, args[1], &spec)
					for key, name := range spec {
						v, ok := nonFiniteValues[name]
						if !ok {
							t.Fatalf("unknown non-finite tag %q", name)
						}
						if key != "idleSlots" {
							t.Fatalf("unknown non-finite field %q", key)
						}
						in.IdleSlots = v
					}
				}
				got = SplitForConcurrency(in)

			case "decideCut":
				var spec trackerSpec
				decodeInto(t, args[0], &spec)
				var in decideCutArgs
				decodeInto(t, args[1], &in)
				threshold := in.Threshold
				if len(args) > 2 {
					var nf map[string]string
					decodeInto(t, args[2], &nf)
					for key, name := range nf {
						v, ok := nonFiniteValues[name]
						if !ok {
							t.Fatalf("unknown non-finite tag %q", name)
						}
						if key != "threshold" {
							t.Fatalf("unknown non-finite field %q", key)
						}
						f := v
						threshold = &f
					}
				}
				got = DecideCut(DecideCutInput{
					Task:         in.Task,
					Model:        in.Model,
					Tracker:      spec.build(),
					Alternatives: in.Alternatives,
					Threshold:    threshold,
				})

			case "pickProbeLeaf":
				var specs []probeLeafSpec
				decodeInto(t, args[0], &specs)
				leaves := make([]probeLeaf, 0, len(specs))
				for _, s := range specs {
					leaves = append(leaves, probeLeaf{ID: s.ID, BandIndex: jscompat.JSNumber(s.BandIndex), Real: s.Real})
				}
				best, ok := PickProbeLeaf(leaves,
					func(l probeLeaf) float64 { return float64(l.BandIndex) },
					func(l probeLeaf) bool { return l.Real })
				if ok {
					got = best
				} else {
					got = nil
				}

			case "selectCoalesceGroup":
				var specs []candSpec
				decodeInto(t, args[0], &specs)
				cands := make([]CoalesceCandidate, 0, len(specs))
				for _, s := range specs {
					cands = append(cands, CoalesceCandidate{
						ID:          s.ID,
						Parent:      s.Parent,
						ModelID:     s.ModelID,
						BandIndex:   jscompat.JSNumber(s.BandIndex),
						FileScope:   s.FileScope,
						Description: s.Description,
					})
				}
				var reliable reliableSpec
				decodeInto(t, args[1], &reliable)
				var opts optsSpec
				decodeInto(t, args[2], &opts)
				built := &SelectCoalesceGroupOptions{MinGroup: opts.MinGroup}
				if opts.CombinedBandIndex != nil {
					built.CombinedBandIndex = combiner(t, *opts.CombinedBandIndex)
				}
				got = SelectCoalesceGroup(cands, reliable.fn(), built)

			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			if s := stringify(t, got); s != fx.OutJSON {
				t.Errorf("parity mismatch\n  fn:   %s\n  args: %s\n  want: %s\n  got:  %s", fx.Fn, fx.ArgsJSON, fx.OutJSON, s)
			}
		})
	}

	for _, fn := range []string{
		"BANDS", "bandToIndex", "bandWithinReliable", "shouldRootCut",
		"partitionFileScope", "splitForConcurrency", "decideCut",
		"pickProbeLeaf", "selectCoalesceGroup",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixture coverage for %q", fn)
		}
	}
}
