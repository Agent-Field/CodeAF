package ratehead

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Replays testdata/fixtures.json, produced by tools/fixtures/gen-ratehead.ts
// from the real src/router/openrouter-rate-headers.ts. The gate is BYTE
// equality between jscompat.Stringify(goResult) and the JSON.stringify the TS
// run recorded.

func init() {
	// gen-ratehead.ts pins process.env.TZ="UTC" before building any Date,
	// because `new Date(str)` reads a zone-less date string as LOCAL time.
	// The Go twin has to agree.
	time.Local = time.UTC
}

type fixtureLine struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// ── parseRateLimitHeaders arguments ──────────────────────────────────────

// headerOp is one ["set"|"append", name, value] triple from the gen script.
type headerOp [3]string

// buildHeaders replays the ops onto an http.Header the way `new Headers()`
// would. Two Fetch-spec behaviours live here rather than in the package,
// because http.Header does not have them:
//   - "normalize a header value": leading and trailing SP/HTAB are stripped
//     at write time. Other whitespace (U+00A0, U+FEFF…) is NOT, which is what
//     leaves work for jscompat.Trim inside the parsers.
//   - set replaces every previous value; append adds one, and get() joins the
//     list with ", ".
func buildHeaders(t *testing.T, ops []headerOp) http.Header {
	t.Helper()
	headers := http.Header{}
	for _, op := range ops {
		name, value := op[1], normalizeHeaderValue(op[2])
		switch op[0] {
		case "set":
			headers.Set(name, value)
		case "append":
			headers.Add(name, value)
		default:
			t.Fatalf("unknown header op %q", op[0])
		}
	}
	return headers
}

func normalizeHeaderValue(value string) string {
	return strings.Trim(value, " \t")
}

// ── isLowRemaining arguments ─────────────────────────────────────────────

// infoSpec mirrors the gen script's InfoSpec: every field is a JSON number, a
// string sentinel for a value JSON cannot carry, or absent.
type infoSpec struct {
	Limit             json.RawMessage `json:"limit"`
	Remaining         json.RawMessage `json:"remaining"`
	Reset             json.RawMessage `json:"reset"`
	RetryAfterSeconds json.RawMessage `json:"retryAfterSeconds"`
}

func specNumber(t *testing.T, raw json.RawMessage) *jscompat.JSNumber {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var sentinel string
		if err := json.Unmarshal(raw, &sentinel); err != nil {
			t.Fatalf("decode sentinel %s: %v", raw, err)
		}
		switch sentinel {
		case "NaN":
			return jsNum(math.NaN())
		case "Infinity":
			return jsNum(math.Inf(1))
		case "-Infinity":
			return jsNum(math.Inf(-1))
		}
		t.Fatalf("unknown number sentinel %q", sentinel)
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode number %s: %v", raw, err)
	}
	return jsNum(value)
}

func buildInfo(t *testing.T, raw json.RawMessage) *RateLimitInfo {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var spec infoSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode info spec: %v", err)
	}
	return &RateLimitInfo{
		Limit:             specNumber(t, spec.Limit),
		Remaining:         specNumber(t, spec.Remaining),
		Reset:             specNumber(t, spec.Reset),
		RetryAfterSeconds: specNumber(t, spec.RetryAfterSeconds),
	}
}

func TestFixtureParity(t *testing.T) {
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	cases := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var fixture fixtureLine
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		cases++
		t.Run(fixture.Name, func(t *testing.T) {
			var result any
			switch fixture.Fn {
			case "parseRateLimitHeaders":
				var args []json.RawMessage
				if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
					t.Fatalf("decode args_json: %v", err)
				}
				if len(args) != 2 {
					t.Fatalf("expected 2 args, got %d", len(args))
				}
				var ops []headerOp
				if err := json.Unmarshal(args[0], &ops); err != nil {
					t.Fatalf("decode header ops: %v", err)
				}
				var nowMs float64
				if err := json.Unmarshal(args[1], &nowMs); err != nil {
					t.Fatalf("decode now: %v", err)
				}
				restore := SetNowMSForTesting(func() float64 { return nowMs })
				defer restore()
				result = ParseRateLimitHeaders(buildHeaders(t, ops))
			case "isLowRemaining":
				var args []json.RawMessage
				if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
					t.Fatalf("decode args_json: %v", err)
				}
				if len(args) != 1 {
					t.Fatalf("expected 1 arg, got %d", len(args))
				}
				result = IsLowRemaining(buildInfo(t, args[0]))
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}

			encoded, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify go result: %v", err)
			}
			if string(encoded) != fixture.OutJSON {
				t.Errorf("byte parity failure\n  fn:   %s\n  args: %s\n  want: %s\n  got:  %s",
					fixture.Fn, fixture.ArgsJSON, fixture.OutJSON, encoded)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if cases < 30 {
		t.Fatalf("expected at least 30 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}
