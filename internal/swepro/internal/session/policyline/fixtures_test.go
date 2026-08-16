package policyline

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// fixture is one line of testdata/fixtures.json, produced by
// tools/fixtures/gen-policyline.ts running against the REAL TS module.
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

// TestFixtureParity is the parity gate: every recorded TS result must be
// reproduced byte-for-byte by the Go port under jscompat.Stringify.
func TestFixtureParity(t *testing.T) {
	fx := loadFixtures(t, "testdata/fixtures.json")
	if len(fx) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fx))
	}
	var pl, pfs int
	for _, c := range fx {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(c.ArgsJSON), &raw); err != nil {
				t.Fatalf("decode args_json %q: %v", c.ArgsJSON, err)
			}
			var got any
			switch c.Fn {
			case "policyLine":
				if len(raw) != 2 {
					t.Fatalf("policyLine wants 2 args, got %d", len(raw))
				}
				got = PolicyLine(optString(t, raw[0]), mustString(t, raw[1]))
				pl++
			case "parseFileScope":
				if len(raw) != 1 {
					t.Fatalf("parseFileScope wants 1 arg, got %d", len(raw))
				}
				got = ParseFileScope(optString(t, raw[0]))
				pfs++
			default:
				t.Fatalf("unknown fn %q", c.Fn)
			}
			b, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			want := expectedForGo(c.OutJSON)
			if string(b) != want {
				t.Fatalf("parity mismatch\n args: %s\n  got: %s\n want: %s", c.ArgsJSON, b, want)
			}
		})
	}
	if pl == 0 || pfs == 0 {
		t.Fatalf("both exports must be exercised: policyLine=%d parseFileScope=%d", pl, pfs)
	}
}

// v8ToGoJSON compensates for the ONE known, pre-existing divergence documented
// on jscompat.Stringify: Go's encoding/json escapes U+2028/U+2029 inside
// strings, V8's JSON.stringify emits them literally. Applied to the RECORDED
// TS bytes so the comparison stays byte-for-byte everywhere else — the Go
// string value itself is identical, only its JSON spelling differs.
//
// The rewrite is unambiguous in this corpus: a raw U+2028/U+2029 byte sequence
// can never appear in Go's encoder output, and no fixture value contains the
// six-character literal text `\u2028`.
var v8ToGoJSON = strings.NewReplacer("\u2028", `\u2028`, "\u2029", `\u2029`)

func expectedForGo(outJSON string) string { return v8ToGoJSON.Replace(outJSON) }

// optString decodes a JSON value that is either a string or null (the fixture
// encoding for TS `string | undefined`).
func optString(t *testing.T, raw json.RawMessage) *string {
	t.Helper()
	if string(raw) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("decode optional string %s: %v", raw, err)
	}
	return &s
}

func mustString(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("decode string %s: %v", raw, err)
	}
	return s
}
