package outcomecache

import (
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
)

// outcome-cache.ts imports its payload type from the sibling module:
//
//	import type { LeafOutcome } from "./leaf-outcome"
//
// so the port aliases the already-ported package's types rather than declaring
// its own — one type identity, one JSON contract. The field ORDER of
// LeafOutcome is load-bearing here specifically: put() JSON.stringify's the
// outcome verbatim into the store entry, and the entry's bytes are the value a
// later get() re-parses and hands back.
type (
	LeafOutcome  = leafoutcome.LeafOutcome
	LeafModel    = leafoutcome.LeafOutcomeModel
	LeafEvidence = leafoutcome.LeafOutcomeEvidence
)

// outcomeToJSVal renders an outcome the way JSON.stringify(outcome) would: keys
// in declaration order, `evidence` omitted entirely when undefined, and numbers
// through V8's Number→string form (NaN/±Infinity → null).
func outcomeToJSVal(o LeafOutcome) jsVal {
	obj := newJSObj()
	obj.set("taskID", stringVal(o.TaskID))
	model := newJSObj()
	model.set("providerID", stringVal(o.Model.ProviderID))
	model.set("modelID", stringVal(o.Model.ModelID))
	obj.set("model", objectVal(model))
	obj.set("sizeBand", stringVal(string(o.SizeBand)))
	obj.set("verdict", stringVal(string(o.Verdict)))
	obj.set("repairRounds", numberVal(float64(o.RepairRounds)))
	obj.set("turns", numberVal(float64(o.Turns)))
	obj.set("toolErrors", numberVal(float64(o.ToolErrors)))
	obj.set("costUsd", numberVal(float64(o.CostUsd)))
	obj.set("wallMs", numberVal(float64(o.WallMs)))
	obj.set("mergeConflict", boolVal(o.MergeConflict))
	obj.set("timestamp", numberVal(float64(o.Timestamp)))
	if o.Evidence != nil {
		ev := newJSObj()
		ev.set("auditCommandsRun", numberVal(float64(o.Evidence.AuditCommandsRun)))
		ev.set("auditBlockers", numberVal(float64(o.Evidence.AuditBlockers)))
		ev.set("inRunTestsPassed", boolVal(o.Evidence.InRunTestsPassed))
		obj.set("evidence", objectVal(ev))
	}
	return objectVal(obj)
}

// leafOutcomeFromJSVal is the best-effort typed projection of a parsed cache
// entry. It is NOT a validator: get() returns whatever JSON.parse produced, so
// the raw value (not this projection) is what re-serializes. Fields whose
// parsed type does not match are left at their zero value.
func leafOutcomeFromJSVal(v jsVal) LeafOutcome {
	var o LeafOutcome
	str := func(k string) string {
		if p, ok := v.prop(k); ok && p.kind == jsString {
			return p.str
		}
		return ""
	}
	num := func(k string) jscompat.JSNumber {
		if p, ok := v.prop(k); ok && p.kind == jsNumber {
			return jscompat.JSNumber(p.num)
		}
		return 0
	}
	o.TaskID = str("taskID")
	if m, ok := v.prop("model"); ok {
		if p, ok2 := m.prop("providerID"); ok2 && p.kind == jsString {
			o.Model.ProviderID = p.str
		}
		if p, ok2 := m.prop("modelID"); ok2 && p.kind == jsString {
			o.Model.ModelID = p.str
		}
	}
	o.SizeBand = leafoutcome.SizeBand(str("sizeBand"))
	o.Verdict = leafoutcome.LeafVerdict(str("verdict"))
	o.RepairRounds = num("repairRounds")
	o.Turns = num("turns")
	o.ToolErrors = num("toolErrors")
	o.CostUsd = num("costUsd")
	o.WallMs = num("wallMs")
	if p, ok := v.prop("mergeConflict"); ok && p.kind == jsBool {
		o.MergeConflict = p.b
	}
	o.Timestamp = num("timestamp")
	if ev, ok := v.prop("evidence"); ok && ev.kind == jsObject {
		var e LeafEvidence
		if p, ok2 := ev.prop("auditCommandsRun"); ok2 && p.kind == jsNumber {
			e.AuditCommandsRun = jscompat.JSNumber(p.num)
		}
		if p, ok2 := ev.prop("auditBlockers"); ok2 && p.kind == jsNumber {
			e.AuditBlockers = jscompat.JSNumber(p.num)
		}
		if p, ok2 := ev.prop("inRunTestsPassed"); ok2 && p.kind == jsBool {
			e.InRunTestsPassed = p.b
		}
		o.Evidence = &e
	}
	return o
}
