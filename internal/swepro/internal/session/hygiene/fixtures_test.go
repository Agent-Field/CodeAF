package hygiene

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// fixture is one line of testdata/fixtures.json, produced by
// tools/fixtures/gen-hygiene.ts against the real TS module.
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
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(line, &fx); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

// callFixture dispatches one fixture to its Go counterpart.
func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args_json %q: %v", fx.ArgsJSON, err)
	}
	switch fx.Fn {
	case "classifyLeftovers":
		if len(args) != 1 {
			t.Fatalf("classifyLeftovers wants 1 arg, got %d", len(args))
		}
		var in ClassifyInput
		if err := json.Unmarshal(args[0], &in); err != nil {
			t.Fatalf("decode ClassifyInput: %v", err)
		}
		return ClassifyLeftovers(in)
	case "hygienePromptBlock":
		if len(args) != 1 {
			t.Fatalf("hygienePromptBlock wants 1 arg, got %d", len(args))
		}
		var v HygieneVerdict
		if err := json.Unmarshal(args[0], &v); err != nil {
			t.Fatalf("decode HygieneVerdict: %v", err)
		}
		return HygienePromptBlock(v)
	case "isTreeClean":
		if len(args) < 1 || len(args) > 2 {
			t.Fatalf("isTreeClean wants 1-2 args, got %d", len(args))
		}
		var files []string
		if err := json.Unmarshal(args[0], &files); err != nil {
			t.Fatalf("decode changedFiles: %v", err)
		}
		var allow []string // nil stands in for the omitted TS argument
		if len(args) == 2 {
			if err := json.Unmarshal(args[1], &allow); err != nil {
				t.Fatalf("decode allow: %v", err)
			}
			if allow == nil {
				allow = []string{}
			}
		}
		return IsTreeClean(files, allow)
	default:
		t.Fatalf("unknown fn %q", fx.Fn)
		return nil
	}
}

// TestFixtureParity is the parity gate: every Go result must serialize to the
// exact bytes JSON.stringify produced for the TS result.
func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got := callFixture(t, fx)
			b, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(b) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, b, fx.OutJSON)
			}
		})
	}
}

// TestFixtureCoverage keeps the generator honest: every exported entry point
// must be represented.
func TestFixtureCoverage(t *testing.T) {
	seen := map[string]int{}
	for _, fx := range loadFixtures(t) {
		seen[fx.Fn]++
	}
	for _, fn := range []string{"classifyLeftovers", "hygienePromptBlock", "isTreeClean"} {
		if seen[fn] == 0 {
			t.Errorf("no fixture cases for %s", fn)
		}
	}
}
