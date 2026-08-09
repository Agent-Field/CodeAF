package specclauses

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Golden-fixture replay against testdata/fixtures.json, produced from the real
// src/session/spec-clauses.ts by tools/fixtures/gen-specclauses.ts. The gate is
// byte-for-byte equality between jscompat.Stringify(goResult) and the TS
// JSON.stringify output — not a tolerance compare — so any regex, UTF-16
// length, split-boundary or empty-slice (`[]` vs `null`) drift fails loudly.

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
			var got string
			switch fx.Fn {
			case "countSpecClauses":
				var args []string
				if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
					t.Fatalf("decode args %q: %v", fx.ArgsJSON, err)
				}
				if len(args) != 1 {
					t.Fatalf("countSpecClauses expects 1 arg, got %d", len(args))
				}
				got = stringify(t, jscompat.JSNumber(CountSpecClauses(args[0])))
			case "clampMatrixCells":
				var args [][]any
				if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
					t.Fatalf("decode args %q: %v", fx.ArgsJSON, err)
				}
				if len(args) != 1 {
					t.Fatalf("clampMatrixCells expects 1 arg, got %d", len(args))
				}
				got = stringify(t, ClampMatrixCells(args[0]))
			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			if got != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}

	for _, fn := range []string{"countSpecClauses", "clampMatrixCells"} {
		if seen[fn] == 0 {
			t.Errorf("no fixture coverage for exported fn %q", fn)
		}
	}
}
