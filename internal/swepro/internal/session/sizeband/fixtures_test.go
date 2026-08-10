package sizeband

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Golden-fixture replay against testdata/fixtures.json, produced from the real
// src/session/size-band.ts by tools/fixtures/gen-sizeband.ts. The gate is
// byte-for-byte equality between jscompat.Stringify(goResult) and the TS
// JSON.stringify output — not a tolerance compare — so any drift in the two
// hand-rolled /gm scanners, in the UTF-16 `.length` thresholds, in the JS
// whitespace class, or in the toLowerCase table lookup fails loudly.

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

func decodeInput(t *testing.T, argsJSON string) EstimateSizeBandInput {
	t.Helper()
	var args []EstimateSizeBandInput
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("decode args %q: %v", argsJSON, err)
	}
	if len(args) != 1 {
		t.Fatalf("estimateSizeBand expects 1 arg, got %d", len(args))
	}
	return args[0]
}

func TestFixtures(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}

	seen := map[string]int{}
	for _, fx := range fixtures {
		fx := fx
		seen[fx.Fn]++
		t.Run(fx.Name, func(t *testing.T) {
			in := decodeInput(t, fx.ArgsJSON)
			// NaN / ±Infinity cannot survive JSON.stringify (all three become
			// `null`), so the generator tags them by `fn` and the value is
			// re-injected here. The expected output still came from TS.
			switch fx.Fn {
			case "estimateSizeBand":
			case "estimateSizeBand:fanInNaN":
				v := math.NaN()
				in.DependencyFanIn = &v
			case "estimateSizeBand:fanInPosInf":
				v := math.Inf(1)
				in.DependencyFanIn = &v
			case "estimateSizeBand:fanInNegInf":
				v := math.Inf(-1)
				in.DependencyFanIn = &v
			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			got := stringify(t, EstimateSizeBand(in))
			if got != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}

	for _, fn := range []string{
		"estimateSizeBand",
		"estimateSizeBand:fanInNaN",
		"estimateSizeBand:fanInPosInf",
		"estimateSizeBand:fanInNegInf",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixture coverage for %q", fn)
		}
	}
}
