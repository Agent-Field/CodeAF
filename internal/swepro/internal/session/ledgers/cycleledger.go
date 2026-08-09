package ledgers

// Port of src/session/cycle-ledger.ts:1-119 — the append-only JSONL history of
// audit/fix/confirmation cycles.
//
// Rows are assembled with the TS object-literal order, strings are clipped in
// UTF-16 units, every number follows JS clamp semantics, and records loaded
// from disk retain their raw JSON.parse shape and property order.

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// CycleKind mirrors the CycleKind union.
type CycleKind string

const (
	CycleFixCycle     CycleKind = "fix-cycle"
	CycleFreshAttempt CycleKind = "fresh-attempt"
	CycleConfirmation CycleKind = "confirmation"
)

// CycleKinds mirrors CYCLE_KINDS in declaration order.
var CycleKinds = []CycleKind{CycleFixCycle, CycleFreshAttempt, CycleConfirmation}

var cycleKindSet = func() map[CycleKind]bool {
	out := make(map[CycleKind]bool, len(CycleKinds))
	for _, kind := range CycleKinds {
		out[kind] = true
	}
	return out
}()

// CycleRecord mirrors CycleRecord. Field order follows the appendCycleRecord
// row literal: verdict precedes the optional spreads.
type CycleRecord struct {
	Ts             float64   `json:"ts"`
	Kind           CycleKind `json:"kind"`
	Cycle          float64   `json:"cycle"`
	BlockersBefore float64   `json:"blockersBefore"`
	Verdict        string    `json:"verdict"`
	BlockersAfter  *float64  `json:"blockersAfter,omitempty"`
	CostUsdApprox  *float64  `json:"costUsdApprox,omitempty"`
	WallMs         *float64  `json:"wallMs,omitempty"`

	raw *jsVal
}

// CycleInput mirrors Omit<CycleRecord, "ts">.
type CycleInput struct {
	Kind           CycleKind `json:"kind"`
	Cycle          float64   `json:"cycle"`
	BlockersBefore float64   `json:"blockersBefore"`
	Verdict        string    `json:"verdict"`
	BlockersAfter  *float64  `json:"blockersAfter,omitempty"`
	CostUsdApprox  *float64  `json:"costUsdApprox,omitempty"`
	WallMs         *float64  `json:"wallMs,omitempty"`
}

// MarshalJSON retains raw reader output or emits the exact append row shape.
func (r CycleRecord) MarshalJSON() ([]byte, error) {
	if r.raw != nil {
		return r.raw.appendJSON(nil), nil
	}
	o := newJSObj()
	o.set("ts", numberVal(r.Ts))
	o.set("kind", stringVal(string(r.Kind)))
	o.set("cycle", numberVal(r.Cycle))
	o.set("blockersBefore", numberVal(r.BlockersBefore))
	o.set("verdict", stringVal(r.Verdict))
	if r.BlockersAfter != nil {
		o.set("blockersAfter", numberVal(*r.BlockersAfter))
	}
	if r.CostUsdApprox != nil {
		o.set("costUsdApprox", numberVal(*r.CostUsdApprox))
	}
	if r.WallMs != nil {
		o.set("wallMs", numberVal(*r.WallMs))
	}
	return objectVal(o).appendJSON(nil), nil
}

const (
	cycleLedgerFile    = ".codeaf/cycle-ledger.jsonl"
	cycleMaxRows       = 2000
	maxVerdictChars    = 200
	jsMaxSafeInteger   = 9_007_199_254_740_991
	cycleMaximumNumber = 1_000_000
)

func cycleLedgerPath(workspace string) string {
	return filepath.Join(workspace, cycleLedgerFile)
}

func clampCount(n, maximum float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 0
	}
	return math.Min(math.Floor(n), maximum)
}

func validOptional(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0
}

func clipCycleVerdict(s string) string {
	if utf16Length(s) > maxVerdictChars {
		return utf16SliceTo(s, maxVerdictChars) + "…"
	}
	return s
}

// AppendCycleRecord mirrors appendCycleRecord. Every failure is swallowed.
func AppendCycleRecord(workspace string, record CycleInput) {
	if !cycleKindSet[record.Kind] {
		return
	}
	p := cycleLedgerPath(workspace)
	if existing, err := os.ReadFile(p); err == nil {
		if countRows(decodeUTF8Lossy(existing)) >= cycleMaxRows {
			return
		}
	}

	row := newJSObj()
	row.set("ts", numberVal(float64(nowMillis())))
	row.set("kind", stringVal(string(record.Kind)))
	row.set("cycle", numberVal(clampCount(record.Cycle, cycleMaximumNumber)))
	row.set("blockersBefore", numberVal(clampCount(record.BlockersBefore, jsMaxSafeInteger)))
	row.set("verdict", stringVal(clipCycleVerdict(record.Verdict)))
	if record.BlockersAfter != nil {
		row.set("blockersAfter", numberVal(clampCount(*record.BlockersAfter, jsMaxSafeInteger)))
	}
	if record.CostUsdApprox != nil && validOptional(*record.CostUsdApprox) {
		// The TS guard calls clampOptional but the row stores the original
		// number. They are equal for every value that survives the guard.
		row.set("costUsdApprox", numberVal(*record.CostUsdApprox))
	}
	if record.WallMs != nil && validOptional(*record.WallMs) {
		row.set("wallMs", numberVal(*record.WallMs))
	}

	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		return
	}
	_, _ = f.Write(append(objectVal(row).appendJSON(nil), '\n'))
	_ = f.Close()
}

func isUsableCycle(v jsVal) bool {
	if v.kind != jsObject {
		return false
	}
	ts, ok := v.obj.get("ts")
	if !ok || ts.kind != jsNumber || !isFiniteJS(ts.num) {
		return false
	}
	kind, ok := v.obj.get("kind")
	if !ok || kind.kind != jsString || !cycleKindSet[CycleKind(kind.str)] {
		return false
	}
	for _, field := range []string{"cycle", "blockersBefore"} {
		value, found := v.obj.get(field)
		if !found || value.kind != jsNumber || !isFiniteJS(value.num) {
			return false
		}
	}
	verdict, ok := v.obj.get("verdict")
	return ok && verdict.kind == jsString
}

func cycleFromVal(v jsVal) CycleRecord {
	el := v
	r := CycleRecord{raw: &el}
	if x, ok := v.obj.get("ts"); ok {
		r.Ts = x.num
	}
	if x, ok := v.obj.get("kind"); ok {
		r.Kind = CycleKind(x.str)
	}
	if x, ok := v.obj.get("cycle"); ok {
		r.Cycle = x.num
	}
	if x, ok := v.obj.get("blockersBefore"); ok {
		r.BlockersBefore = x.num
	}
	if x, ok := v.obj.get("verdict"); ok {
		r.Verdict = x.str
	}
	if x, ok := v.obj.get("blockersAfter"); ok && x.kind == jsNumber {
		n := x.num
		r.BlockersAfter = &n
	}
	if x, ok := v.obj.get("costUsdApprox"); ok && x.kind == jsNumber {
		n := x.num
		r.CostUsdApprox = &n
	}
	if x, ok := v.obj.get("wallMs"); ok && x.kind == jsNumber {
		n := x.num
		r.WallMs = &n
	}
	return r
}

// ReadCycleRecords mirrors readCycleRecords. Missing files and bad lines
// produce as many valid records as can be recovered.
func ReadCycleRecords(workspace string) []CycleRecord {
	data, err := os.ReadFile(cycleLedgerPath(workspace))
	if err != nil {
		return []CycleRecord{}
	}
	out := []CycleRecord{}
	for _, line := range strings.Split(decodeUTF8Lossy(data), "\n") {
		trimmed := jscompatTrim(line)
		if trimmed == "" {
			continue
		}
		parsed, ok := parseJSON(trimmed)
		if ok && isUsableCycle(parsed) {
			out = append(out, cycleFromVal(parsed))
		}
	}
	return out
}

// jscompatTrim is kept local to make the JSONL read loop read like the TS
// source while sharing the exact JS whitespace definition used by countRows.
func jscompatTrim(s string) string {
	return jscompat.Trim(s)
}
