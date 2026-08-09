// Package ledgers is the Go port of the src/session ledger family:
// attempt-ledger.ts, blocker-ledger.ts, cycle-ledger.ts,
// decision-ledger.ts, and run-failure-ledger.ts.
//
// The four file-backed TS modules are best-effort: their fs and parse failures
// never perturb scheduler control flow, so the Go sinks/readers return no
// errors either. run-failure-ledger is process-local memory. Fidelity notes
// shared by the file-backed modules (deliberate, do not "fix"):
//
//   - The readers hand parsed objects back to the caller. Unknown keys, the
//     file's key order, and JS's
//     array-index-keys-first property order are therefore observable; jsjson.go
//     reproduces all three, and the record types marshal through it.
//   - The clipping helpers measure and slice in UTF-16 code units, so clipping can split an
//     astral character and leave an unpaired surrogate. That survives the
//     file round trip as "\ud83d", and the port keeps it (WTF-8 internally).
//   - Date.now() feeds attempt-ledger's tmp filename and the JSONL timestamps;
//     it is behind nowMillis/SetClockForTesting.
//
// One packaging deviation from the TS layout: the five modules share a Go
// package, so identically named private helpers are disambiguated by module.
package ledgers

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// nowMillis stands in for Date.now(). Package-global like the TS module's
// implicit dependency on the ambient clock.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// SetClockForTesting pins the millisecond clock. Returns a restore func.
func SetClockForTesting(f func() int64) func() {
	prev := nowMillis
	nowMillis = f
	return func() { nowMillis = prev }
}

// AuditFixKey mirrors auditFixKey: a run-level, task-INDEPENDENT ledger key for
// the audit-fix relay, derived purely from the goal text so the "do-not-repeat"
// memory survives fresh-context relays and full process restarts.
func AuditFixKey(goal string) string {
	sum := sha1.Sum([]byte(toUTF8(goal)))
	h := hex.EncodeToString(sum[:])[:12]
	return "audit-fix:" + h
}

// AttemptRecord mirrors the AttemptRecord interface. Records returned by
// ReadAttempts carry the raw parsed value so JSON.stringify round-trips
// byte-for-byte (extra keys and key order included); the typed fields are a
// best-effort projection for callers.
type AttemptRecord struct {
	Attempt  float64 `json:"attempt"`
	Approach string  `json:"approach"`
	Outcome  string  `json:"outcome"`
	// Evidence is an optional pointer to hard evidence, e.g. a rejected-diff
	// path or a failing command + exit.
	Evidence *string `json:"evidence,omitempty"`

	raw *jsVal
}

// AttemptInput mirrors Omit<AttemptRecord, "attempt">, the argument shape of
// appendAttempt.
type AttemptInput struct {
	Approach string  `json:"approach"`
	Outcome  string  `json:"outcome"`
	Evidence *string `json:"evidence,omitempty"`
}

func (r AttemptRecord) MarshalJSON() ([]byte, error) {
	if r.raw != nil {
		return r.raw.appendJSON(nil), nil
	}
	o := newJSObj()
	o.set("attempt", numberVal(r.Attempt))
	o.set("approach", stringVal(r.Approach))
	o.set("outcome", stringVal(r.Outcome))
	if r.Evidence != nil {
		o.set("evidence", stringVal(*r.Evidence))
	}
	return objectVal(o).appendJSON(nil), nil
}

const (
	attemptLedgerFile  = ".codeaf/attempt-ledger.json"
	maxFieldChars      = 500
	maxAttemptsPerTask = 20
)

func attemptLedgerPath(workspace string) string {
	return filepath.Join(workspace, attemptLedgerFile)
}

// clipAttemptField mirrors attempt-ledger's clip(): UTF-16 length threshold,
// UTF-16 slice, "…" (U+2026) marker.
func clipAttemptField(s string) string {
	if utf16Length(s) > maxFieldChars {
		return utf16SliceTo(s, maxFieldChars) + "…"
	}
	return s
}

// readLedger mirrors readLedger(): any fs or parse failure yields {}, and the
// `parsed && typeof parsed === "object"` guard lets an ARRAY through — typeof
// [] is "object" — which the property get/set helpers below then honour.
func readLedger(workspace string) jsVal {
	data, err := os.ReadFile(attemptLedgerPath(workspace))
	if err != nil {
		return emptyObjectVal()
	}
	parsed, ok := parseJSON(decodeUTF8Lossy(data))
	if !ok {
		return emptyObjectVal()
	}
	if parsed.isObjectish() {
		return parsed
	}
	return emptyObjectVal()
}

// ledgerGet is `ledger[key]`. found=false stands for undefined.
func ledgerGet(root jsVal, key string) (jsVal, bool) {
	if root.kind == jsArray {
		if n, ok := isArrayIndexKey(key); ok && int(n) < len(root.arr) {
			return root.arr[n], true
		}
		if key == "length" {
			return numberVal(float64(len(root.arr))), true
		}
		// Array.prototype members (push, map, …) would read back as functions
		// here; the port treats them as undefined. Unreachable for real task
		// keys, and the write path aborts either way.
		return nullVal(), false
	}
	return root.obj.get(key)
}

// ledgerSet is `ledger[key] = v`. It returns false where JS would THROW —
// assigning a non-numeric value to an array's `length` is a RangeError.
// Assigning a non-index key to an ARRAY silently succeeds in JS but the
// property is invisible to JSON.stringify, so the row is lost: kept as-is.
func ledgerSet(root *jsVal, key string, v jsVal) bool {
	if root.kind == jsArray {
		if n, ok := isArrayIndexKey(key); ok {
			idx := int(n)
			switch {
			case idx < len(root.arr):
				root.arr[idx] = v
			case idx == len(root.arr):
				root.arr = append(root.arr, v)
			default:
				for len(root.arr) < idx {
					root.arr = append(root.arr, jsVal{kind: jsHole})
				}
				root.arr = append(root.arr, v)
			}
			return true
		}
		if key == "length" {
			return false
		}
		return true
	}
	root.obj.set(key, v)
	return true
}

// AppendAttempt mirrors appendAttempt. Best-effort: every failure path is
// silent, matching the TS try/catch that wraps the whole body.
func AppendAttempt(workspace, taskKey string, record AttemptInput) {
	ledger := readLedger(workspace)
	rowsVal, found := ledgerGet(ledger, taskKey)

	var rows []jsVal
	switch {
	case !found || rowsVal.kind == jsNull:
		// `?? []` — undefined and null both fall through to a fresh array.
		rows = []jsVal{}
	case rowsVal.kind == jsArray:
		rows = rowsVal.arr
	default:
		// A non-array value stored under this key: `rows.length >= 20` is
		// either false (undefined >= 20) or a plain comparison on a string's
		// length, and `rows.push` then throws a TypeError. Both branches end
		// in "no write", which is what this return reproduces.
		return
	}

	if len(rows) >= maxAttemptsPerTask {
		return
	}

	row := newJSObj()
	row.set("attempt", numberVal(float64(len(rows))))
	row.set("approach", stringVal(clipAttemptField(record.Approach)))
	row.set("outcome", stringVal(clipAttemptField(record.Outcome)))
	// `...(record.evidence ? { evidence: clip(record.evidence) } : {})` — a
	// TRUTHY test, so an empty-string evidence is dropped, not clipped.
	if record.Evidence != nil && *record.Evidence != "" {
		row.set("evidence", stringVal(clipAttemptField(*record.Evidence)))
	}
	rows = append(rows, objectVal(row))
	if !ledgerSet(&ledger, taskKey, arrayVal(rows)) {
		return
	}

	target := attemptLedgerPath(workspace)
	if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
		return
	}
	// Atomic write: full-write + fsync a tmp file, then rename over the target,
	// so a crash mid-write or a concurrent reader never sees a truncated JSON
	// file.
	tmp := fmt.Sprintf("%s.tmp-%d-%s", target, os.Getpid(), jscompat.FormatNumber(float64(nowMillis())))
	handle, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return
	}
	writeErr := func() error {
		if _, err := handle.Write(ledger.appendJSON(nil)); err != nil {
			return err
		}
		return handle.Sync()
	}()
	handle.Close()
	if writeErr != nil {
		// TS lets the error escape the try/finally, so the rename is skipped
		// and the tmp file survives.
		return
	}
	_ = os.Rename(tmp, target)
}

// ReadAttempts mirrors readAttempts: every row recorded for taskKey, in file
// order, or [] when the ledger has none.
func ReadAttempts(workspace, taskKey string) []AttemptRecord {
	ledger := readLedger(workspace)
	v, found := ledgerGet(ledger, taskKey)
	if !found || v.kind != jsArray {
		// DIVERGENCE (documented): TS returns `ledger[taskKey] ?? []`, so a
		// hand-corrupted ledger whose value is a non-null non-array (a number,
		// a string, an object) is returned VERBATIM under an AttemptRecord[]
		// type. Go cannot express that; the port yields [].
		return []AttemptRecord{}
	}
	out := make([]AttemptRecord, 0, len(v.arr))
	for _, el := range v.arr {
		out = append(out, attemptFromVal(el))
	}
	return out
}

func attemptFromVal(v jsVal) AttemptRecord {
	el := v
	r := AttemptRecord{raw: &el}
	if v.kind != jsObject {
		return r
	}
	if x, ok := v.obj.get("attempt"); ok && x.kind == jsNumber {
		r.Attempt = x.num
	}
	if x, ok := v.obj.get("approach"); ok && x.kind == jsString {
		r.Approach = x.str
	}
	if x, ok := v.obj.get("outcome"); ok && x.kind == jsString {
		r.Outcome = x.str
	}
	if x, ok := v.obj.get("evidence"); ok && x.kind == jsString {
		s := x.str
		r.Evidence = &s
	}
	return r
}
