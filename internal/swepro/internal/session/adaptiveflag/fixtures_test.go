package adaptiveflag

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// fixture is one line of testdata/fixtures.json, produced by
// tools/fixtures/gen-adaptiveflag.ts running against the REAL TS module.
// args_json is [envValue]; null means the variable was deleted.
type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T, path string) []fixture {
	t.Helper()
	f, err := os.Open(path)
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

// TestFixtureParity is the parity gate: for every env value the TS module was
// driven with, the Go port must produce a byte-identical JSON answer.
func TestFixtureParity(t *testing.T) {
	fx := loadFixtures(t, "testdata/fixtures.json")
	if len(fx) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fx))
	}
	for _, c := range fx {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			if c.Fn != "adaptiveCutsEnabled" {
				t.Fatalf("unknown fn %q", c.Fn)
			}
			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(c.ArgsJSON), &raw); err != nil {
				t.Fatalf("decode args_json %q: %v", c.ArgsJSON, err)
			}
			if len(raw) != 1 {
				t.Fatalf("adaptiveCutsEnabled wants 1 arg, got %d", len(raw))
			}
			if string(raw[0]) == "null" {
				// `delete process.env.CODEAF_ADAPTIVE_CUTS` on the TS side.
				restore, had := os.LookupEnv(envVar)
				os.Unsetenv(envVar)
				defer func() {
					if had {
						os.Setenv(envVar, restore)
					}
				}()
			} else {
				var v string
				if err := json.Unmarshal(raw[0], &v); err != nil {
					t.Fatalf("decode env value %s: %v", raw[0], err)
				}
				t.Setenv(envVar, v)
			}
			b, err := jscompat.Stringify(AdaptiveCutsEnabled())
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(b) != c.OutJSON {
				t.Fatalf("parity mismatch\n args: %s\n  got: %s\n want: %s", c.ArgsJSON, b, c.OutJSON)
			}
		})
	}
}
