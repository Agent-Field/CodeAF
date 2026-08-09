package luby

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
// src/session/luby.ts by tools/fixtures/gen-luby.ts. The gate is byte-for-byte
// equality between jscompat.Stringify(goResult) and the TS JSON.stringify
// output — not a tolerance compare — so any V8-number-formatting or
// empty-slice (`[]` vs `null`) drift fails loudly.

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
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
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

// decodeNum reverses the generator's argument encoding. JSON.stringify cannot
// round-trip NaN / ±Infinity / -0, so gen-luby.ts writes them as the sentinel
// strings "@NaN" / "@Infinity" / "@-Infinity" / "@-0". A literal JSON null
// stays 0 — that is exactly what JS numeric coercion does with an explicit
// null argument, which the budgets/unit/explicit-null fixture pins.
func decodeNum(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, `"`) {
		var tag string
		if err := json.Unmarshal(raw, &tag); err != nil {
			t.Fatalf("decode sentinel %q: %v", s, err)
		}
		switch tag {
		case "@NaN":
			return math.NaN()
		case "@Infinity":
			return math.Inf(1)
		case "@-Infinity":
			return math.Inf(-1)
		case "@-0":
			return math.Copysign(0, -1)
		}
		t.Fatalf("unknown numeric sentinel %q", tag)
	}
	var f float64 // JSON null unmarshals as a no-op, leaving 0.
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode number %q: %v", s, err)
	}
	return f
}

func stringify(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
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
			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &raw); err != nil {
				t.Fatalf("decode args %q: %v", fx.ArgsJSON, err)
			}

			var got string
			switch fx.Fn {
			case "lubySequence":
				if len(raw) != 1 {
					t.Fatalf("lubySequence expects 1 arg, got %d", len(raw))
				}
				got = stringify(t, jscompat.JSNumber(LubySequence(decodeNum(t, raw[0]))))
			case "lubyBudgets":
				if len(raw) == 0 {
					t.Fatalf("lubyBudgets expects at least 1 arg")
				}
				n := decodeNum(t, raw[0])
				var res []float64
				if len(raw) == 1 {
					// The TS default-parameter path (`unit: number = 1`).
					res = LubyBudgets(n)
				} else {
					// Extra args past `unit` are ignored, as in JS.
					res = LubyBudgets(n, decodeNum(t, raw[1]))
				}
				nums := make([]jscompat.JSNumber, len(res))
				for i, v := range res {
					nums[i] = jscompat.JSNumber(v)
				}
				got = stringify(t, nums)
			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			if got != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}

	for _, fn := range []string{"lubySequence", "lubyBudgets"} {
		if seen[fn] == 0 {
			t.Errorf("no fixture coverage for exported fn %q", fn)
		}
	}
}
