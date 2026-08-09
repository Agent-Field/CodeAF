package criticalpath

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
)

// fixture is one line of testdata/fixtures.json, produced by
// tools/fixtures/gen-criticalpath.ts running the REAL TS module under bun.
type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// outcome unifies "returned an analysis" and "threw" behind one Marshaler so a
// single byte-for-byte assertion covers both paths. The thrown shape mirrors
// what the generator records:
//
//	{"__error":{"name":..., "message":..., "taskIds": [...] | null}}
type outcome struct {
	analysis CriticalPathAnalysis
	err      error
}

func (o outcome) MarshalJSON() ([]byte, error) {
	if o.err == nil {
		return o.analysis.MarshalJSON()
	}
	var buf bytes.Buffer
	buf.WriteString(`{"__error":{"name":`)
	switch e := o.err.(type) {
	case *CriticalPathCycleError:
		appendJSString(&buf, e.Name())
		buf.WriteString(`,"message":`)
		appendJSString(&buf, e.Error())
		buf.WriteString(`,"taskIds":[`)
		for i, id := range e.TaskIDs {
			if i > 0 {
				buf.WriteByte(',')
			}
			appendJSString(&buf, id)
		}
		buf.WriteString(`]`)
	case *CriticalPathInputError:
		appendJSString(&buf, e.Name())
		buf.WriteString(`,"message":`)
		appendJSString(&buf, e.Error())
		buf.WriteString(`,"taskIds":null`)
	default:
		return nil, e
	}
	buf.WriteString(`}}`)
	return buf.Bytes(), nil
}

// parseDurationToken mirrors the generator's parseDuration: "undefined" (and a
// missing key) becomes undefined on the TS side, which is indistinguishable
// from NaN here — both fail Number.isFinite and yield the identical message.
func parseDurationToken(token string, present bool) float64 {
	if !present || token == "undefined" {
		return math.NaN()
	}
	return jscompat.ToNumber(token)
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
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(line, &fx); err != nil {
			t.Fatalf("decode fixture line %d: %v", len(out)+1, err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

// TestFixtureParity is THE parity gate: for every recorded case the Go result
// must serialize to the exact bytes JSON.stringify produced in bun.
func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			if fx.Fn != "analyzeCriticalPath" {
				t.Fatalf("unknown fn %q", fx.Fn)
			}

			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &raw); err != nil {
				t.Fatalf("decode args_json: %v", err)
			}
			if len(raw) != 3 {
				t.Fatalf("expected 3 args, got %d", len(raw))
			}

			var tasks []*plandb.Task
			if err := json.Unmarshal(raw[0], &tasks); err != nil {
				t.Fatalf("decode tasks: %v", err)
			}
			var edges []plandb.Dependency
			if err := json.Unmarshal(raw[1], &edges); err != nil {
				t.Fatalf("decode edges: %v", err)
			}
			var durations map[string]string
			if err := json.Unmarshal(raw[2], &durations); err != nil {
				t.Fatalf("decode durations: %v", err)
			}

			analysis, err := AnalyzeCriticalPath(tasks, edges, func(task *plandb.Task) float64 {
				token, ok := durations[task.ID]
				return parseDurationToken(token, ok)
			})

			got, merr := jscompat.Stringify(outcome{analysis: analysis, err: err})
			if merr != nil {
				t.Fatalf("stringify result: %v", merr)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("parity mismatch\n args: %s\n  got: %s\n want: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}

// TestIsArrayIndexKey pins the JS own-property hoisting boundary that
// SlackRecord depends on. Verified against V8: keys 0..2^32-2 are hoisted and
// sorted ascending; 2^32-1 and everything non-canonical stays in insertion
// order.
func TestIsArrayIndexKey(t *testing.T) {
	yes := []string{"0", "1", "9", "10", "4294967294"}
	no := []string{
		"", "01", "-0", "-1", "1.0", "0.5", "+1", "1e2", "1 ", " 1", "1_0",
		"4294967295", "4294967296", "9007199254740992", "18446744073709551616", "a",
	}
	for _, k := range yes {
		if !isArrayIndexKey(k) {
			t.Errorf("isArrayIndexKey(%q) = false, want true", k)
		}
	}
	for _, k := range no {
		if isArrayIndexKey(k) {
			t.Errorf("isArrayIndexKey(%q) = true, want false", k)
		}
	}
}
