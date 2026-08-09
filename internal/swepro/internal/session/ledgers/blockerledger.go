package ledgers

// Port of src/session/blocker-ledger.ts — the append-only, per-blocker
// LIFECYCLE record (open → fixed → verified) keyed by a stable content hash,
// so the durable OPEN-blocker set survives context compaction and
// fresh-context relays.
//
// Fidelity notes (deliberate, do not "fix"):
//   - reconcileVerdictBlockers/markBlockersFixed snapshot `latest` ONCE and
//     never update it inside their loops, so a verdict listing the same
//     blocker text twice appends the row twice. Kept.
//   - `latest` is a JS Map: iteration is insertion order, i.e. the order of
//     each blockerId's FIRST appearance in the file. jscompat.OrderedMap.
//   - Records come back from JSON.parse untouched, so unknown keys and the
//     file's key order are observable — see jsjson.go.

import (
	"crypto/sha1"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// BlockerStatus is one of BlockerStatuses.
type BlockerStatus string

// BlockerStatuses mirrors BLOCKER_STATUSES.
var BlockerStatuses = []BlockerStatus{"open", "fixed", "verified"}

// BlockerRecord mirrors the BlockerRecord interface. Field order follows the
// object LITERAL built in appendBlockerRecord (ts, blockerId, status,
// cycleOpened, text, cycleClosed) — which is the serialization order — not the
// interface's declaration order, where cycleClosed precedes text.
//
// Records returned by the readers carry the raw parsed value so
// JSON.stringify round-trips byte-for-byte; the typed fields are the
// projection the TS callers use.
type BlockerRecord struct {
	Ts float64 `json:"ts"`
	// BlockerID is a stable content hash of the blocker's identifying text.
	BlockerID string        `json:"blockerId"`
	Status    BlockerStatus `json:"status"`
	// CycleOpened is the audit cycle at which this blocker was (re)opened.
	CycleOpened float64 `json:"cycleOpened"`
	// Text is the blocker's identifying text (clipped).
	Text string `json:"text"`
	// CycleClosed is the audit cycle at which it moved to fixed/verified;
	// absent while open.
	CycleClosed *float64 `json:"cycleClosed,omitempty"`

	raw *jsVal
}

// BlockerInput mirrors Omit<BlockerRecord, "ts">, the argument shape of
// appendBlockerRecord.
type BlockerInput struct {
	BlockerID   string        `json:"blockerId"`
	Status      BlockerStatus `json:"status"`
	CycleOpened float64       `json:"cycleOpened"`
	Text        string        `json:"text"`
	CycleClosed *float64      `json:"cycleClosed,omitempty"`
}

func (r BlockerRecord) MarshalJSON() ([]byte, error) {
	if r.raw != nil {
		return r.raw.appendJSON(nil), nil
	}
	o := newJSObj()
	o.set("ts", numberVal(r.Ts))
	o.set("blockerId", stringVal(r.BlockerID))
	o.set("status", stringVal(string(r.Status)))
	o.set("cycleOpened", numberVal(r.CycleOpened))
	o.set("text", stringVal(r.Text))
	if r.CycleClosed != nil {
		o.set("cycleClosed", numberVal(*r.CycleClosed))
	}
	return objectVal(o).appendJSON(nil), nil
}

// OpenBlocker is a currently-unresolved blocker, as re-injected into
// briefs/compaction.
type OpenBlocker struct {
	BlockerID   string  `json:"blockerId"`
	Text        string  `json:"text"`
	CycleOpened float64 `json:"cycleOpened"`
}

func (b OpenBlocker) MarshalJSON() ([]byte, error) {
	o := newJSObj()
	o.set("blockerId", stringVal(b.BlockerID))
	o.set("text", stringVal(b.Text))
	o.set("cycleOpened", numberVal(b.CycleOpened))
	return objectVal(o).appendJSON(nil), nil
}

const (
	blockerLedgerFile = ".codeaf/blocker-ledger.jsonl"
	maxRows           = 5000
	maxTextChars      = 500
	maxCycle          = 1_000_000
)

var statusSet = func() map[BlockerStatus]bool {
	m := map[BlockerStatus]bool{}
	for _, s := range BlockerStatuses {
		m[s] = true
	}
	return m
}()

func blockerLedgerPath(workspace string) string {
	return filepath.Join(workspace, blockerLedgerFile)
}

// isJSSpace is the JS regex \s class, expanded because Go RE2's \s is only
// [\t\n\f\r ].
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// canonicalText collapses a blocker's identifying text to a canonical form for
// hashing: trim + single-space runs (`.replace(/\s+/g, " ").trim()`). Case is
// preserved. Hand-scanned rather than regex'd: RE2's \s is a strict subset of
// JS's.
func canonicalText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	inRun := false
	for i := 0; i < len(text); {
		cp, size := decodeWTF8(text, i)
		i += size
		if isJSSpace(cp) {
			if !inRun {
				b.WriteByte(' ')
				inRun = true
			}
			continue
		}
		inRun = false
		b.WriteString(text[i-size : i])
	}
	return jscompat.Trim(b.String())
}

// BlockerID mirrors blockerId: a stable 12-hex-char identity derived from the
// canonicalized identifying text.
func BlockerID(text string) string {
	sum := sha1.Sum([]byte(toUTF8(canonicalText(text))))
	return hex.EncodeToString(sum[:])[:12]
}

// clipBlockerText mirrors blocker-ledger's clip().
func clipBlockerText(s string) string {
	if utf16Length(s) > maxTextChars {
		return utf16SliceTo(s, maxTextChars) + "…"
	}
	return s
}

// clampCycle mirrors clampCycle: non-finite or non-positive → 0, otherwise
// floor capped at MAX_CYCLE.
func clampCycle(n float64) float64 {
	if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 0
	}
	return math.Min(math.Floor(n), maxCycle)
}

func isFiniteJS(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func countRows(text string) int {
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		if jscompat.Trim(line) != "" {
			rows++
		}
	}
	return rows
}

// AppendBlockerRecord mirrors appendBlockerRecord: append a single lifecycle
// row. Best-effort — a bad status is dropped, the file is capped at MAX_ROWS,
// and any fs failure is swallowed.
func AppendBlockerRecord(workspace string, record BlockerInput) {
	if !statusSet[record.Status] {
		return
	}
	p := blockerLedgerPath(workspace)
	if existing, err := os.ReadFile(p); err == nil {
		// No file yet (or unreadable) — treat as empty and proceed to write.
		if countRows(decodeUTF8Lossy(existing)) >= maxRows {
			return
		}
	}
	id := record.BlockerID
	if id == "" {
		id = BlockerID(record.Text)
	}
	row := newJSObj()
	row.set("ts", numberVal(float64(nowMillis())))
	row.set("blockerId", stringVal(id))
	row.set("status", stringVal(string(record.Status)))
	row.set("cycleOpened", numberVal(clampCycle(record.CycleOpened)))
	row.set("text", stringVal(clipBlockerText(record.Text)))
	if record.CycleClosed != nil {
		row.set("cycleClosed", numberVal(clampCycle(*record.CycleClosed)))
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

// isUsable mirrors isUsable(). An ARRAY passes the `typeof v === "object"`
// guard in TS but then fails on `o.ts`, since "ts" is not an array index —
// observationally identical to rejecting non-objects outright.
func isUsable(v jsVal) bool {
	if v.kind != jsObject {
		return false
	}
	ts, ok := v.obj.get("ts")
	if !ok || ts.kind != jsNumber || !isFiniteJS(ts.num) {
		return false
	}
	id, ok := v.obj.get("blockerId")
	if !ok || id.kind != jsString || utf16Length(id.str) == 0 {
		return false
	}
	st, ok := v.obj.get("status")
	if !ok || st.kind != jsString || !statusSet[BlockerStatus(st.str)] {
		return false
	}
	co, ok := v.obj.get("cycleOpened")
	if !ok || co.kind != jsNumber || !isFiniteJS(co.num) {
		return false
	}
	tx, ok := v.obj.get("text")
	if !ok || tx.kind != jsString {
		return false
	}
	return true
}

func blockerFromVal(v jsVal) BlockerRecord {
	el := v
	r := BlockerRecord{raw: &el}
	if x, ok := v.obj.get("ts"); ok && x.kind == jsNumber {
		r.Ts = x.num
	}
	if x, ok := v.obj.get("blockerId"); ok && x.kind == jsString {
		r.BlockerID = x.str
	}
	if x, ok := v.obj.get("status"); ok && x.kind == jsString {
		r.Status = BlockerStatus(x.str)
	}
	if x, ok := v.obj.get("cycleOpened"); ok && x.kind == jsNumber {
		r.CycleOpened = x.num
	}
	if x, ok := v.obj.get("text"); ok && x.kind == jsString {
		r.Text = x.str
	}
	if x, ok := v.obj.get("cycleClosed"); ok && x.kind == jsNumber {
		n := x.num
		r.CycleClosed = &n
	}
	return r
}

// ReadBlockerRecords mirrors readBlockerRecords: every parseable row, in file
// order. Never fails; a missing file yields [].
func ReadBlockerRecords(workspace string) []BlockerRecord {
	data, err := os.ReadFile(blockerLedgerPath(workspace))
	if err != nil {
		return []BlockerRecord{}
	}
	out := []BlockerRecord{}
	for _, line := range strings.Split(decodeUTF8Lossy(data), "\n") {
		trimmed := jscompat.Trim(line)
		if trimmed == "" {
			continue
		}
		parsed, ok := parseJSON(trimmed)
		if !ok {
			continue
		}
		if isUsable(parsed) {
			out = append(out, blockerFromVal(parsed))
		}
	}
	return out
}

// LatestByBlocker mirrors latestByBlocker: the latest record per blocker_id
// (append-order wins), in first-appearance insertion order.
func LatestByBlocker(workspace string) *jscompat.OrderedMap[string, BlockerRecord] {
	latest := jscompat.NewOrderedMap[string, BlockerRecord]()
	for _, rec := range ReadBlockerRecords(workspace) {
		latest.Set(rec.BlockerID, rec)
	}
	return latest
}

// LoadOpenBlockers mirrors loadOpenBlockers: latest status per blocker_id,
// keeping only those whose latest status is "open".
func LoadOpenBlockers(workspace string) []OpenBlocker {
	out := []OpenBlocker{}
	for _, rec := range LatestByBlocker(workspace).Values() {
		if rec.Status == "open" {
			out = append(out, OpenBlocker{BlockerID: rec.BlockerID, Text: rec.Text, CycleOpened: rec.CycleOpened})
		}
	}
	return out
}

// ReconcileVerdictBlockers mirrors reconcileVerdictBlockers: (re)open every
// blocker present in this cycle's verdict whose latest status is not already
// "open", and mark "verified" every open/fixed blocker absent from it.
func ReconcileVerdictBlockers(workspace string, cycle float64, blockerTexts []string) {
	latest := LatestByBlocker(workspace)
	currentIDs := map[string]bool{}
	for _, text := range blockerTexts {
		canonical := canonicalText(text)
		if len(canonical) == 0 {
			continue
		}
		id := BlockerID(canonical)
		currentIDs[id] = true
		prior, has := latest.Get(id)
		// `latest` is deliberately NOT refreshed inside this loop, so a verdict
		// that repeats the same text appends a duplicate "open" row.
		if !has || prior.Status != "open" {
			AppendBlockerRecord(workspace, BlockerInput{BlockerID: id, Status: "open", CycleOpened: cycle, Text: canonical})
		}
	}
	for _, e := range latest.Entries() {
		if currentIDs[e.Key] {
			continue
		}
		prior := e.Val
		if prior.Status == "open" || prior.Status == "fixed" {
			closed := cycle
			AppendBlockerRecord(workspace, BlockerInput{
				BlockerID:   e.Key,
				Status:      "verified",
				CycleOpened: prior.CycleOpened,
				CycleClosed: &closed,
				Text:        prior.Text,
			})
		}
	}
}

// MarkBlockersFixed mirrors markBlockersFixed: record that a fix was DISPATCHED
// for a set of blockers (open → fixed).
func MarkBlockersFixed(workspace string, cycle float64, blockerTexts []string) {
	latest := LatestByBlocker(workspace)
	for _, text := range blockerTexts {
		canonical := canonicalText(text)
		if len(canonical) == 0 {
			continue
		}
		id := BlockerID(canonical)
		prior, has := latest.Get(id)
		if has && prior.Status == "open" {
			closed := cycle
			AppendBlockerRecord(workspace, BlockerInput{
				BlockerID:   id,
				Status:      "fixed",
				CycleOpened: prior.CycleOpened,
				CycleClosed: &closed,
				Text:        prior.Text,
			})
		}
	}
}
