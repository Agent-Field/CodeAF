package capability

import (
	"encoding/json"
	"math"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

// The LeafOutcome family is declared in src/session/leaf-outcome.ts (T1) and
// only TYPE-imported by capability.ts. LeafVerdict, SizeBand and the inline
// `model` object are Go type ALIASES of the leafoutcome port's declarations, so
// they are literally the same types.
//
// LeafOutcome itself is NOT an alias, deliberately. capability.ts consumes the
// interface far more defensively than leaf-outcome.ts produces it, and two of
// those behaviors are unrepresentable in leafoutcome.LeafOutcome:
//
//  1. `outcome.evidence?.auditCommandsRun === 0` must be FALSE when the evidence
//     object exists but the key does not (a truncated JSONL record). Value-typed
//     jscompat.JSNumber fields decode a missing key to 0, which would wrongly
//     trigger observe()'s skip; the fields here are pointers.
//  2. `loadOutcomes` returns the raw JSON.parse results, so re-stringifying one
//     reproduces the ORIGINAL line's key set and key order. That needs the
//     parsed value kept alongside the decoded fields (see `raw` below).
//
// FromLeafOutcome bridges the two. Both are exercised by the golden fixtures.

// LeafVerdict is leaf-outcome.ts's
// `type LeafVerdict = "pass" | "pass_after_repair" | "fail" | "escalated"`.
// A plain string type, because capability.ts routinely receives values outside
// the union (loadOutcomes parses arbitrary JSONL) and its guards depend on those
// reaching the lookup tables.
type LeafVerdict = leafoutcome.LeafVerdict

const (
	VerdictPass            = leafoutcome.VerdictPass
	VerdictPassAfterRepair = leafoutcome.VerdictPassAfterRepair
	VerdictFail            = leafoutcome.VerdictFail
	VerdictEscalated       = leafoutcome.VerdictEscalated
)

// LeafOutcomeModel is the inline `{ providerID: string; modelID: string }` of
// LeafOutcome.model. It is a POINTER inside LeafOutcome because capability.ts
// reads it through `outcome.model?.modelID` — an absent object must behave
// like JS `undefined`, not like a zero struct.
type LeafOutcomeModel = leafoutcome.LeafOutcomeModel

// LeafEvidence is LeafOutcome["evidence"]. The two numeric fields are POINTERS
// so that `outcome.evidence?.auditCommandsRun === 0` can tell "the number 0"
// apart from "absent", "null" and "not a number" — all three of which are
// `!== 0` in JS and so must NOT trigger the observe() skip.
type LeafEvidence struct {
	AuditCommandsRun *jscompat.JSNumber `json:"auditCommandsRun"`
	AuditBlockers    *jscompat.JSNumber `json:"auditBlockers"`
	InRunTestsPassed *bool              `json:"inRunTestsPassed"`
}

// isZero is `e?.<field> === 0` for a nil-safe receiver: strict equality against
// the number 0, false for a nil evidence object, a missing key, JSON null and
// any non-number value.
func (e *LeafEvidence) commandsRunIsZero() bool {
	return e != nil && e.AuditCommandsRun != nil && float64(*e.AuditCommandsRun) == 0
}

func (e *LeafEvidence) blockersIsZero() bool {
	return e != nil && e.AuditBlockers != nil && float64(*e.AuditBlockers) == 0
}

// LeafOutcome mirrors leaf-outcome.ts's interface, field order = declaration
// order so a Go-built record stringifies identically to a TS-built one.
//
// Numeric fields are jscompat.JSNumber (NaN/±Infinity marshal as null, like
// JSON.stringify). RepairRounds additionally carries NaN when the parsed value
// was present but not a number: capability.ts reads it as
// `(outcome.repairRounds ?? 0) === 0`, where "0", true and {} are all `!== 0`
// while absent and null both coalesce to 0 — NaN reproduces the former and the
// zero value reproduces the latter.
type LeafOutcome struct {
	TaskID        string            `json:"taskID"`
	Model         *LeafOutcomeModel `json:"model"`
	SizeBand      sizeband.SizeBand `json:"sizeBand"`
	Verdict       LeafVerdict       `json:"verdict"`
	RepairRounds  jscompat.JSNumber `json:"repairRounds"`
	Turns         jscompat.JSNumber `json:"turns"`
	ToolErrors    jscompat.JSNumber `json:"toolErrors"`
	CostUsd       jscompat.JSNumber `json:"costUsd"`
	WallMs        jscompat.JSNumber `json:"wallMs"`
	MergeConflict bool              `json:"mergeConflict"`
	Timestamp     jscompat.JSNumber `json:"timestamp"`
	Evidence      *LeafEvidence     `json:"evidence,omitempty"`

	// raw is the JSON.parse result when this record came off a JSONL line.
	// loadOutcomes returns the PARSED OBJECTS (typed as LeafOutcome by a
	// structural type predicate), so re-stringifying one reproduces its own key
	// set and key order — not this struct's. Keeping the parsed value is the
	// only way to be byte-identical for partial or extra-keyed records.
	raw    any
	rawSet bool
}

// leafOutcomeFields exists purely to give MarshalJSON a non-recursive
// declaration-order encoding for Go-constructed records.
type leafOutcomeFields struct {
	TaskID        string            `json:"taskID"`
	Model         *LeafOutcomeModel `json:"model"`
	SizeBand      sizeband.SizeBand `json:"sizeBand"`
	Verdict       LeafVerdict       `json:"verdict"`
	RepairRounds  jscompat.JSNumber `json:"repairRounds"`
	Turns         jscompat.JSNumber `json:"turns"`
	ToolErrors    jscompat.JSNumber `json:"toolErrors"`
	CostUsd       jscompat.JSNumber `json:"costUsd"`
	WallMs        jscompat.JSNumber `json:"wallMs"`
	MergeConflict bool              `json:"mergeConflict"`
	Timestamp     jscompat.JSNumber `json:"timestamp"`
	Evidence      *LeafEvidence     `json:"evidence,omitempty"`
}

func (o LeafOutcome) MarshalJSON() ([]byte, error) {
	if o.rawSet {
		return jscompat.Stringify(o.raw)
	}
	return jscompat.Stringify(leafOutcomeFields{
		TaskID:        o.TaskID,
		Model:         o.Model,
		SizeBand:      o.SizeBand,
		Verdict:       o.Verdict,
		RepairRounds:  o.RepairRounds,
		Turns:         o.Turns,
		ToolErrors:    o.ToolErrors,
		CostUsd:       o.CostUsd,
		WallMs:        o.WallMs,
		MergeConflict: o.MergeConflict,
		Timestamp:     o.Timestamp,
		Evidence:      o.Evidence,
	})
}

// UnmarshalJSON is deliberately TOLERANT and never fails on a shape mismatch:
// JSON.parse succeeds for any syntactically valid line and it is
// `isUsableOutcome` — not the parser — that rejects records. A struct decode
// that errored on `"turns": "five"` would drop a line TS keeps.
func (o *LeafOutcome) UnmarshalJSON(b []byte) error {
	v, err := parseJSValue(b)
	if err != nil {
		return err
	}
	*o = LeafOutcome{raw: v, rawSet: true}
	obj, ok := v.(*jsObject)
	if !ok {
		return nil // non-object line: every field stays zero, isUsableOutcome rejects it
	}
	o.TaskID, _ = obj.Get("taskID").(string)
	if m, ok := obj.Get("model").(*jsObject); ok {
		o.Model = &LeafOutcomeModel{}
		o.Model.ProviderID, _ = m.Get("providerID").(string)
		o.Model.ModelID, _ = m.Get("modelID").(string)
	}
	if s, ok := obj.Get("sizeBand").(string); ok {
		o.SizeBand = sizeband.SizeBand(s)
	}
	if s, ok := obj.Get("verdict").(string); ok {
		o.Verdict = LeafVerdict(s)
	}
	o.RepairRounds = jscompat.JSNumber(nullishNumber(obj, "repairRounds"))
	o.Turns = jscompat.JSNumber(nullishNumber(obj, "turns"))
	o.ToolErrors = jscompat.JSNumber(nullishNumber(obj, "toolErrors"))
	o.CostUsd = jscompat.JSNumber(nullishNumber(obj, "costUsd"))
	o.WallMs = jscompat.JSNumber(nullishNumber(obj, "wallMs"))
	o.Timestamp = jscompat.JSNumber(nullishNumber(obj, "timestamp"))
	o.MergeConflict, _ = obj.Get("mergeConflict").(bool)
	if ev, ok := obj.Get("evidence").(*jsObject); ok {
		o.Evidence = &LeafEvidence{
			AuditCommandsRun: strictNumber(ev, "auditCommandsRun"),
			AuditBlockers:    strictNumber(ev, "auditBlockers"),
		}
		if bl, ok := ev.Get("inRunTestsPassed").(bool); ok {
			o.Evidence.InRunTestsPassed = &bl
		}
	}
	return nil
}

// nullishNumber models `x ?? 0` folded into a float: absent and null give 0,
// a number gives itself, anything else gives NaN (so `=== 0` stays false).
func nullishNumber(obj *jsObject, key string) float64 {
	v := obj.Get(key)
	if v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return math.NaN()
}

// strictNumber returns a pointer only when the key holds an actual JSON
// number — the only value for which `x === 0` can be true.
func strictNumber(obj *jsObject, key string) *jscompat.JSNumber {
	f, ok := obj.Get(key).(float64)
	if !ok {
		return nil
	}
	n := jscompat.JSNumber(f)
	return &n
}

// FromLeafOutcome adapts a record produced by the leaf-outcome port (T1) into
// the shape this package observes.
//
// NOT IN TS: capability.ts type-imports leaf-outcome.ts's interface, so the two
// modules share one runtime object and no conversion exists there. This is the
// Go seam that keeps that sharing while letting this package model the
// `evidence?.x === 0` distinctions leafoutcome.LeafOutcome cannot (see the note
// at the top of this file). Nothing is lost in the direction TS actually
// flows — a leafoutcome record always carries a fully populated evidence object
// when it carries one at all.
func FromLeafOutcome(o leafoutcome.LeafOutcome) LeafOutcome {
	model := o.Model
	out := LeafOutcome{
		TaskID:        o.TaskID,
		Model:         &model,
		SizeBand:      o.SizeBand,
		Verdict:       o.Verdict,
		RepairRounds:  o.RepairRounds,
		Turns:         o.Turns,
		ToolErrors:    o.ToolErrors,
		CostUsd:       o.CostUsd,
		WallMs:        o.WallMs,
		MergeConflict: o.MergeConflict,
		Timestamp:     o.Timestamp,
	}
	if o.Evidence != nil {
		cmds := o.Evidence.AuditCommandsRun
		blockers := o.Evidence.AuditBlockers
		passed := o.Evidence.InRunTestsPassed
		out.Evidence = &LeafEvidence{
			AuditCommandsRun: &cmds,
			AuditBlockers:    &blockers,
			InRunTestsPassed: &passed,
		}
	}
	return out
}

var _ json.Marshaler = LeafOutcome{}
var _ json.Unmarshaler = (*LeafOutcome)(nil)
