package auditconvergence

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Byte-for-byte parity gate against golden output captured from the REAL TS
// module (tools/fixtures/gen-auditconvergence.ts). Every case asserts
// jscompat.Stringify(goResult) == the exact bytes JSON.stringify produced.

type fixtureRow struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixtureRow {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	var rows []fixtureRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r fixtureRow
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return rows
}

// splitArgs decodes the JSON array in args_json into per-argument raw values.
func splitArgs(t *testing.T, argsJSON string) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		t.Fatalf("decode args_json %q: %v", argsJSON, err)
	}
	return args
}

func decodeArg[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode arg %s: %v", raw, err)
	}
	return v
}

func dispatch(t *testing.T, row fixtureRow) any {
	t.Helper()
	args := splitArgs(t, row.ArgsJSON)
	switch row.Fn {
	case "classifyBlockerSeverity":
		return ClassifyBlockerSeverity(decodeArg[string](t, args[0]))
	case "effectiveSeverity":
		return EffectiveSeverity(decodeArg[*SeverityBlocker](t, args[0]))
	case "partitionBlockers":
		return PartitionBlockers(decodeArg[[]*SeverityBlocker](t, args[0]))
	case "blockerKey":
		return BlockerKey(decodeArg[*SeverityBlocker](t, args[0]))
	case "assessConvergence":
		return AssessConvergence(decodeArg[AssessConvergenceInput](t, args[0]))
	case "HYGIENE_PATTERN.test":
		return HYGIENE_PATTERN.Test(decodeArg[string](t, args[0]))
	case "POLISH_PATTERN.test":
		return POLISH_PATTERN.Test(decodeArg[string](t, args[0]))
	case "HYGIENE_PATTERN.source":
		return HYGIENE_PATTERN.Source()
	case "POLISH_PATTERN.source":
		return POLISH_PATTERN.Source()
	case "AUDIT_CLEANUP_MAX_CYCLES_DEFAULT":
		return jscompat.JSNumber(AUDIT_CLEANUP_MAX_CYCLES_DEFAULT)
	default:
		t.Fatalf("unknown fn %q", row.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	rows := loadFixtures(t)
	if len(rows) < 25 {
		t.Fatalf("expected >= 25 fixture cases, got %d", len(rows))
	}
	byFn := map[string]int{}
	for _, row := range rows {
		byFn[row.Fn]++
		t.Run(row.Name, func(t *testing.T) {
			got := dispatch(t, row)
			b, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(b) != row.OutJSON {
				t.Errorf("parity mismatch\n  fn:   %s\n  args: %s\n  want: %s\n  got:  %s",
					row.Fn, row.ArgsJSON, row.OutJSON, string(b))
			}
		})
	}
	// Every export must be covered by at least one fixture case.
	for _, fn := range []string{
		"classifyBlockerSeverity", "effectiveSeverity", "partitionBlockers", "blockerKey",
		"assessConvergence", "HYGIENE_PATTERN.test", "POLISH_PATTERN.test",
		"HYGIENE_PATTERN.source", "POLISH_PATTERN.source", "AUDIT_CLEANUP_MAX_CYCLES_DEFAULT",
	} {
		if byFn[fn] == 0 {
			t.Errorf("no fixture coverage for %s", fn)
		}
	}
	t.Logf("replayed %d fixture cases across %d entry points", len(rows), len(byFn))
}
