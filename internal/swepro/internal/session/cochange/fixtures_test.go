package cochange

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Replay of testdata/fixtures.json, generated from the real TS module by
// tools/fixtures/gen-cochange.ts. The gate is byte-for-byte equality between
// jscompat.Stringify(goResult) and the recorded JSON.stringify output.
//
// Wire conventions (mirrored in the generator):
//   - a CoChangeGraph argument is {totals:[[k,v],…], pairs:[[k,v],…]}, entries
//     in Map insertion order;
//   - an argument number may be the string "NaN"/"Infinity"/"-Infinity",
//     because JSON has no literal for them;
//   - an omitted optional argument is encoded as a shorter args array. A JSON
//     `null` in the k slot maps to 0.0: TS default parameters only fire for
//     `undefined`, so k stays null and `null <= 0` is true — observationally
//     identical to 0.

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type graphWireIn struct {
	Totals [][]json.RawMessage `json:"totals"`
	Pairs  [][]json.RawMessage `json:"pairs"`
}

type graphWireOut struct {
	Totals [][]any `json:"totals"`
	Pairs  [][]any `json:"pairs"`
}

func decodeNumber(t *testing.T, raw json.RawMessage) float64 {
	t.Helper()
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, `"`) {
		var lit string
		if err := json.Unmarshal(raw, &lit); err != nil {
			t.Fatalf("bad number literal %q: %v", s, err)
		}
		switch lit {
		case "NaN":
			return math.NaN()
		case "Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		}
		t.Fatalf("unknown number sentinel %q", lit)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("bad number %q: %v", s, err)
	}
	return f
}

func decodeString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("bad string %q: %v", string(raw), err)
	}
	return s
}

func decodeGraph(t *testing.T, raw json.RawMessage) *CoChangeGraph {
	t.Helper()
	var w graphWireIn
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatalf("bad graph %q: %v", string(raw), err)
	}
	g := &CoChangeGraph{
		Totals: jscompat.NewOrderedMap[string, float64](),
		Pairs:  jscompat.NewOrderedMap[string, float64](),
	}
	for _, e := range w.Totals {
		g.Totals.Set(decodeString(t, e[0]), decodeNumber(t, e[1]))
	}
	for _, e := range w.Pairs {
		g.Pairs.Set(decodeString(t, e[0]), decodeNumber(t, e[1]))
	}
	return g
}

func encodeGraph(g *CoChangeGraph) graphWireOut {
	out := graphWireOut{Totals: [][]any{}, Pairs: [][]any{}}
	for _, e := range g.Totals.Entries() {
		out.Totals = append(out.Totals, []any{e.Key, jscompat.JSNumber(e.Val)})
	}
	for _, e := range g.Pairs.Entries() {
		out.Pairs = append(out.Pairs, []any{e.Key, jscompat.JSNumber(e.Val)})
	}
	return out
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
			t.Fatalf("bad fixture line: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
				t.Fatalf("bad args_json: %v", err)
			}
			var got any
			switch fx.Fn {
			case "parseGitLog":
				got = ParseGitLog(decodeString(t, args[0]))

			case "buildCoChangeGraph":
				var commits []CoChangeCommit
				if err := json.Unmarshal(args[0], &commits); err != nil {
					t.Fatalf("bad commits: %v", err)
				}
				opts := BuildCoChangeOptions{}
				if len(args) >= 2 {
					var m map[string]json.RawMessage
					if err := json.Unmarshal(args[1], &m); err != nil {
						t.Fatalf("bad opts: %v", err)
					}
					if raw, ok := m["maxCommitFiles"]; ok && strings.TrimSpace(string(raw)) != "null" {
						v := decodeNumber(t, raw)
						opts.MaxCommitFiles = &v
					}
				}
				got = encodeGraph(BuildCoChangeGraph(commits, opts))

			case "coupling":
				g := decodeGraph(t, args[0])
				got = jscompat.JSNumber(Coupling(g, decodeString(t, args[1]), decodeString(t, args[2])))

			case "relatedFiles":
				g := decodeGraph(t, args[0])
				var k *float64
				if len(args) >= 3 {
					if strings.TrimSpace(string(args[2])) == "null" {
						z := 0.0
						k = &z
					} else {
						v := decodeNumber(t, args[2])
						k = &v
					}
				}
				got = RelatedFiles(g, decodeString(t, args[1]), k)

			case "pairKey":
				got = PairKey(decodeString(t, args[0]), decodeString(t, args[1]))

			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			b, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(b) != fx.OutJSON {
				t.Errorf("parity mismatch\n args: %s\n  got: %s\n want: %s", fx.ArgsJSON, b, fx.OutJSON)
			}
		})
	}
}
