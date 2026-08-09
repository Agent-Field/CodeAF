// Pure repetition and budget guard for agent action loops — port of
// src/session/loop-guard.ts.
//
// The TS original is a closure factory over six mutable locals. Go gets an
// unexported struct behind the exported LoopGuard interface; the observable
// contract (verdict objects, snapshot key order, restore leniency) is
// unchanged. There is no I/O, no Date.now and no Math.random in this module,
// so there is no injectable clock.
//
// Fidelity notes (deliberate, do not "fix"):
//   - formatNumber is `value.toFixed(4).replace(/0+$/, "").replace(/\.$/, "")`.
//     For |value| >= 1e21 toFixed falls back to ToString, so the trailing-zero
//     strip eats part of the EXPONENT: formatNumber(1e30) is "1e+3", not
//     "1e+30". Reproduced exactly (see formatNumber below).
//   - The action budget interpolates raw numbers (`${actionCount}/${maxActions}`)
//     while the cost budget goes through formatNumber. maxActions is only
//     clamped to >= 0, never floored, so a fractional budget prints as
//     "action budget reached (6/5.5)".
//   - Every knob is kept as a float64, not an int: positiveInteger only floors
//     and clamps to >= 1, so 1e21 stays 1e21 and the cycle scan is a float
//     loop, exactly like TS (a huge maxCyclePeriod hangs both ports).
//   - restore() re-derives state from an untyped blob: `version !== 1` is a
//     STRICT comparison (the string "1" is rejected), the numeric fields go
//     through JS Number() coercion, and the booleans through `=== true`. That
//     coercion lives in LoopGuardSnapshot.UnmarshalJSON so a Go caller
//     restoring from persisted JSON sees the same leniency as the TS caller.
//   - encodeAction is a hand-rolled JSON.stringify of a two-string array
//     because V8 emits U+2028/U+2029 literally inside JSON strings and Go's
//     encoding/json (hence jscompat.Stringify) escapes them unconditionally.
package loopguard

import (
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/fixflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// W5-TODO(knobs): register these defaults with the shared knob registry during
// the integration pass. They intentionally live here until then so this module
// remains independently usable and has no configuration I/O.
const (
	repeatCapDefault           = 3.0 // REPEAT_CAP           W5-TODO(knobs)
	maxCyclePeriodDefault      = 4.0 // MAX_CYCLE_PERIOD     W5-TODO(knobs)
	cycleMinOccurrencesDefault = 2.0 // CYCLE_MIN_OCCURRENCES W5-TODO(knobs)
	warnFractionDefault        = 0.8 // WARN_FRACTION        W5-TODO(knobs)
)

// LoopAction mirrors the TS interface of the same name. costUsd is optional,
// so it is a pointer: nil covers both `undefined` and `null` (TS checks
// `typeof action.costUsd === "number"`, which rejects both).
type LoopAction struct {
	Tool    string   `json:"tool"`
	ArgsKey string   `json:"argsKey"`
	CostUsd *float64 `json:"costUsd,omitempty"`
}

// LoopStatus mirrors `type LoopStatus = "ok" | "warn" | "stop"`.
type LoopStatus string

const (
	LoopStatusOK   LoopStatus = "ok"
	LoopStatusWarn LoopStatus = "warn"
	LoopStatusStop LoopStatus = "stop"
)

// LoopVerdict mirrors the TS interface. `reason` is optional: the TS code
// returns the bare literal `{ status: "ok" }`, which JSON.stringify emits
// without a reason key at all — hence the pointer plus omitempty (omitempty on
// a pointer drops exactly nil, which is JS `undefined`).
type LoopVerdict struct {
	Status LoopStatus `json:"status"`
	Reason *string    `json:"reason,omitempty"`
}

// LoopGuardOptions mirrors the TS interface. Every field is `number |
// undefined`, so every field is a pointer; nil means "not supplied".
type LoopGuardOptions struct {
	// RepeatCap is the number of identical consecutive actions that terminates the loop.
	RepeatCap *float64 `json:"repeatCap,omitempty"`
	// MaxCyclePeriod is the largest cycle period to inspect.
	MaxCyclePeriod *float64 `json:"maxCyclePeriod,omitempty"`
	// CycleMinOccurrences is the number of repeated copies required to identify a cycle.
	CycleMinOccurrences *float64 `json:"cycleMinOccurrences,omitempty"`
	// MaxCostUsd is an optional cumulative USD budget.
	MaxCostUsd *float64 `json:"maxCostUsd,omitempty"`
	// MaxActions is an optional maximum number of observed actions.
	MaxActions *float64 `json:"maxActions,omitempty"`
	// WarnFraction is the fraction of a budget at which a one-shot warning is emitted.
	WarnFraction *float64 `json:"warnFraction,omitempty"`
}

// LoopGuardSnapshot mirrors the TS interface. Field order is the TS object
// literal order in snapshot(), which is what JSON.stringify emits.
//
// The numeric fields are float64 rather than int because restore() runs them
// through Number()/Number.isFinite and only then Math.floor: a snapshot can
// legitimately carry NaN, a fraction, or 1e30.
type LoopGuardSnapshot struct {
	Version           float64  `json:"version"`
	Actions           []string `json:"actions"`
	ActionCount       float64  `json:"actionCount"`
	CumulativeCostUsd float64  `json:"cumulativeCostUsd"`
	WarnedCost        bool     `json:"warnedCost"`
	WarnedActions     bool     `json:"warnedActions"`
	Stopped           bool     `json:"stopped"`
}

// LoopGuard mirrors the TS interface returned by createLoopGuard.
type LoopGuard interface {
	Observe(action LoopAction) LoopVerdict
	// ObserveCost charges provider calls that have no tool action. It is a Go
	// scheduler wiring seam; it does not alter repetition or action counts.
	ObserveCost(costUsd *float64) LoopVerdict
	Snapshot() LoopGuardSnapshot
	// Restore takes a nil pointer for TS `null` / `undefined`.
	Restore(snapshot *LoopGuardSnapshot)
}

type loopGuard struct {
	repeatCap           float64
	maxCyclePeriod      float64
	cycleMinOccurrences float64
	maxCostUsd          *float64
	maxActions          *float64
	warnFraction        float64
	historyLimit        float64

	actions           []string
	actionCount       float64
	cumulativeCostUsd float64
	warnedCost        bool
	warnedActions     bool
	stopped           bool
}

// CreateLoopGuard creates a stateful but otherwise pure loop guard. The caller
// owns persistence of the plain snapshot returned by Snapshot().
//
// TS signature: createLoopGuard(options: LoopGuardOptions = {}). The zero
// LoopGuardOptions is that `{}`.
func CreateLoopGuard(options LoopGuardOptions) LoopGuard {
	repeatCap := positiveInteger(options.RepeatCap, repeatCapDefault)
	maxCyclePeriod := math.Max(2, positiveInteger(options.MaxCyclePeriod, maxCyclePeriodDefault))
	cycleMinOccurrences := math.Max(2, positiveInteger(options.CycleMinOccurrences, cycleMinOccurrencesDefault))
	maxCostUsd := optionalNonNegative(options.MaxCostUsd)
	maxActions := optionalNonNegative(options.MaxActions)
	warnFraction := clamp(nullish(options.WarnFraction, warnFractionDefault), 0, 1)
	historyLimit := math.Max(repeatCap, maxCyclePeriod*cycleMinOccurrences)

	return &loopGuard{
		repeatCap:           repeatCap,
		maxCyclePeriod:      maxCyclePeriod,
		cycleMinOccurrences: cycleMinOccurrences,
		maxCostUsd:          maxCostUsd,
		maxActions:          maxActions,
		warnFraction:        warnFraction,
		historyLimit:        historyLimit,
		// `let actions: string[] = []` — an empty array, never null, so
		// snapshot() marshals it as [] and not null.
		actions: []string{},
	}
}

func (g *loopGuard) Observe(action LoopAction) LoopVerdict {
	if g.stopped {
		return LoopVerdict{Status: LoopStatusStop, Reason: strptr("loop guard already stopped")}
	}

	actionKey := encodeAction(action)
	g.actions = append(g.actions, actionKey)
	if float64(len(g.actions)) > g.historyLimit {
		g.actions = sliceLast(g.actions, g.historyLimit)
	}
	g.actionCount += 1

	cost := 0.0
	if action.CostUsd != nil && isFinite(*action.CostUsd) && *action.CostUsd > 0 {
		cost = *action.CostUsd
	}
	g.cumulativeCostUsd += cost

	// A terminal repetition finding takes precedence over a budget warning.
	repeatReason := exactRepeatReason(g.actions, g.repeatCap)
	if repeatReason != nil {
		g.stopped = true
		return LoopVerdict{Status: LoopStatusStop, Reason: repeatReason}
	}

	cycleReason := cycleDetectionReason(g.actions, g.maxCyclePeriod, g.cycleMinOccurrences)
	if cycleReason != nil {
		g.stopped = true
		return LoopVerdict{Status: LoopStatusStop, Reason: cycleReason}
	}

	budgetStopReasons := []string{}
	budgetWarnReasons := []string{}

	if g.maxCostUsd != nil {
		maxCostUsd := *g.maxCostUsd
		if g.cumulativeCostUsd >= maxCostUsd {
			budgetStopReasons = append(budgetStopReasons,
				"cost budget reached ("+formatNumber(g.cumulativeCostUsd)+"/"+formatNumber(maxCostUsd)+" USD)")
		} else if !g.warnedCost && g.cumulativeCostUsd >= maxCostUsd*g.warnFraction {
			g.warnedCost = true
			budgetWarnReasons = append(budgetWarnReasons,
				"cost budget at "+formatPercent(g.cumulativeCostUsd/maxCostUsd))
		}
	}

	if g.maxActions != nil {
		maxActions := *g.maxActions
		if g.actionCount >= maxActions {
			budgetStopReasons = append(budgetStopReasons,
				"action budget reached ("+jscompat.FormatNumber(g.actionCount)+"/"+jscompat.FormatNumber(maxActions)+")")
		} else if !g.warnedActions && g.actionCount >= maxActions*g.warnFraction {
			g.warnedActions = true
			budgetWarnReasons = append(budgetWarnReasons,
				"action budget at "+formatPercent(g.actionCount/maxActions))
		}
	}

	if len(budgetStopReasons) > 0 {
		g.stopped = true
		return LoopVerdict{Status: LoopStatusStop, Reason: strptr(strings.Join(budgetStopReasons, "; "))}
	}
	if len(budgetWarnReasons) > 0 {
		return LoopVerdict{Status: LoopStatusWarn, Reason: strptr(strings.Join(budgetWarnReasons, "; "))}
	}
	return LoopVerdict{Status: LoopStatusOK}
}

func (g *loopGuard) ObserveCost(costUsd *float64) LoopVerdict {
	if g.stopped {
		return LoopVerdict{Status: LoopStatusStop, Reason: strptr("loop guard already stopped")}
	}
	if costUsd != nil && isFinite(*costUsd) && *costUsd > 0 {
		g.cumulativeCostUsd += *costUsd
	}
	if g.maxCostUsd == nil {
		return LoopVerdict{Status: LoopStatusOK}
	}
	maximum := *g.maxCostUsd
	if g.cumulativeCostUsd >= maximum {
		g.stopped = true
		return LoopVerdict{Status: LoopStatusStop, Reason: strptr(
			"cost budget reached (" + formatNumber(g.cumulativeCostUsd) + "/" +
				formatNumber(maximum) + " USD)",
		)}
	}
	if !g.warnedCost && g.cumulativeCostUsd >= maximum*g.warnFraction {
		g.warnedCost = true
		return LoopVerdict{Status: LoopStatusWarn, Reason: strptr(
			"cost budget at " + formatPercent(g.cumulativeCostUsd/maximum),
		)}
	}
	return LoopVerdict{Status: LoopStatusOK}
}

func (g *loopGuard) Snapshot() LoopGuardSnapshot {
	actions := make([]string, len(g.actions))
	copy(actions, g.actions)
	return LoopGuardSnapshot{
		Version:           1,
		Actions:           actions,
		ActionCount:       g.actionCount,
		CumulativeCostUsd: g.cumulativeCostUsd,
		WarnedCost:        g.warnedCost,
		WarnedActions:     g.warnedActions,
		Stopped:           g.stopped,
	}
}

func (g *loopGuard) Restore(snapshotValue *LoopGuardSnapshot) {
	if snapshotValue == nil || snapshotValue.Version != 1 {
		return
	}

	// `Array.isArray(snapshotValue.actions)` — a nil slice is TS `undefined` /
	// `null` / a non-array, which skips the assignment entirely. The filter for
	// `typeof value === "string"` is applied in UnmarshalJSON, where non-string
	// elements are still visible.
	if snapshotValue.Actions != nil {
		g.actions = sliceLast(snapshotValue.Actions, g.historyLimit)
	}
	restoredCount := snapshotValue.ActionCount
	restoredCost := snapshotValue.CumulativeCostUsd
	if isFinite(restoredCount) && restoredCount >= 0 {
		g.actionCount = math.Floor(restoredCount)
	}
	if isFinite(restoredCost) && restoredCost >= 0 {
		g.cumulativeCostUsd = restoredCost
	}
	g.warnedCost = snapshotValue.WarnedCost
	g.warnedActions = snapshotValue.WarnedActions
	g.stopped = snapshotValue.Stopped
}

// UnmarshalJSON reproduces what restore() sees when it is handed a blob parsed
// out of persistence rather than a value produced by snapshot(). TS reads the
// fields off an untyped object, so the coercions happen here:
//
//   - version: compared with `!==` against 1, so anything that is not the JSON
//     number 1 has to fail. Non-numbers become NaN, and NaN != 1 in Go too.
//   - actions: `Array.isArray` then `.filter(typeof === "string")`. A non-array
//     leaves the slice nil (Restore then skips it); an array keeps only its
//     string elements and is never nil.
//   - actionCount / cumulativeCostUsd: `Number(...)`. Absent is `undefined`,
//     i.e. NaN, which Restore then rejects — that is why an absent key is NOT
//     the same as 0.
//   - warnedCost / warnedActions / stopped: `=== true`, so only the literal
//     JSON true counts.
//
// A payload that is not a JSON object at all (a string, a number, false, null)
// is left in the "reject" state without an error: TS either short-circuits on
// falsiness or reads `undefined` for version, and both return immediately.
func (s *LoopGuardSnapshot) UnmarshalJSON(data []byte) error {
	*s = LoopGuardSnapshot{Version: math.NaN(), ActionCount: math.NaN(), CumulativeCostUsd: math.NaN()}

	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	raw := map[string]json.RawMessage{}
	if _, isObject := probe.(map[string]any); !isObject {
		return nil
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if version, ok := raw["version"]; ok {
		s.Version = numberLiteral(version)
	}
	if actions, ok := raw["actions"]; ok {
		var elements []json.RawMessage
		if err := json.Unmarshal(actions, &elements); err == nil {
			filtered := []string{}
			for _, element := range elements {
				var str string
				if json.Unmarshal(element, &str) == nil && len(element) > 0 && element[0] == '"' {
					filtered = append(filtered, str)
				}
			}
			s.Actions = filtered
		}
	}
	if actionCount, ok := raw["actionCount"]; ok {
		s.ActionCount = jsNumber(actionCount)
	}
	if cumulativeCostUsd, ok := raw["cumulativeCostUsd"]; ok {
		s.CumulativeCostUsd = jsNumber(cumulativeCostUsd)
	}
	s.WarnedCost = jsonIsTrue(raw["warnedCost"])
	s.WarnedActions = jsonIsTrue(raw["warnedActions"])
	s.Stopped = jsonIsTrue(raw["stopped"])
	return nil
}

// numberLiteral yields the value of a JSON number and NaN for anything else —
// the shape a strict `=== 1` comparison needs.
func numberLiteral(raw json.RawMessage) float64 {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return math.NaN()
	}
	if number, ok := value.(float64); ok {
		return number
	}
	return math.NaN()
}

// jsNumber reproduces JS Number(value) for a JSON value: null is 0, booleans
// are 1/0, strings go through the string-to-number grammar, arrays are joined
// with "," and re-coerced, and objects are NaN ("[object Object]").
func jsNumber(raw json.RawMessage) float64 {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return math.NaN()
	}
	return jsNumberValue(value)
}

func jsNumberValue(value any) float64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case bool:
		if typed {
			return 1
		}
		return 0
	case float64:
		return typed
	case string:
		return jscompat.ToNumber(typed)
	case []any:
		parts := make([]string, len(typed))
		for i, element := range typed {
			parts[i] = jsString(element)
		}
		return jscompat.ToNumber(strings.Join(parts, ","))
	default:
		return math.NaN()
	}
}

// jsString reproduces JS String(value) for the JSON value space, which is what
// Array.prototype.join uses on each element (null and undefined join as "").
func jsString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return jscompat.FormatNumber(typed)
	case string:
		return typed
	case []any:
		parts := make([]string, len(typed))
		for i, element := range typed {
			parts[i] = jsString(element)
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

func jsonIsTrue(raw json.RawMessage) bool {
	return string(raw) == "true"
}

// encodeAction is JSON.stringify([String(action?.tool ?? ""), String(action?.argsKey ?? "")]).
//
// Hand-rolled rather than jscompat.Stringify because V8's JSON quoting escapes
// only ", \, and C0 controls, while Go's encoding/json additionally escapes
// U+2028 and U+2029 unconditionally. The stored key ends up in the snapshot, so
// the difference would be observable.
func encodeAction(action LoopAction) string {
	var builder strings.Builder
	builder.WriteByte('[')
	writeJSONString(&builder, action.Tool)
	builder.WriteByte(',')
	writeJSONString(&builder, action.ArgsKey)
	builder.WriteByte(']')
	return builder.String()
}

const hexDigits = "0123456789abcdef"

func writeJSONString(builder *strings.Builder, value string) {
	builder.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			builder.WriteString("\\\"")
			continue
		case '\\':
			builder.WriteString("\\\\")
			continue
		case '\b':
			builder.WriteString("\\b")
			continue
		case '\t':
			builder.WriteString("\\t")
			continue
		case '\n':
			builder.WriteString("\\n")
			continue
		case '\f':
			builder.WriteString("\\f")
			continue
		case '\r':
			builder.WriteString("\\r")
			continue
		}
		if r < 0x20 {
			builder.WriteString("\\u00")
			builder.WriteByte(hexDigits[(r>>4)&0xf])
			builder.WriteByte(hexDigits[r&0xf])
			continue
		}
		if r == utf8.RuneError {
			// Go strings cannot hold the unpaired surrogates that V8 would
			// escape as \udXXX; invalid UTF-8 decodes to U+FFFD, which is what
			// the JSON layer would have produced anyway.
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune(r)
	}
	builder.WriteByte('"')
}

func exactRepeatReason(history []string, cap float64) *string {
	if float64(len(history)) < cap {
		return nil
	}
	last := history[len(history)-1]
	for i := len(history) - 2; float64(i) >= float64(len(history))-cap; i-- {
		if history[i] != last {
			return nil
		}
	}
	return strptr("exact action repeated " + jscompat.FormatNumber(cap) + " times consecutively")
}

func cycleDetectionReason(history []string, maxPeriod float64, minOccurrences float64) *string {
	// When the fix flag is enabled, bound the scan with integer arithmetic so
	// a huge maxPeriod (e.g. 1e21) does not cause an effectively infinite
	// float loop. The cap is min(maxPeriod, floor(len(history)/2), 512):
	// scanning for a cycle whose period exceeds half the history is pointless.
	if fixflag.Enabled("CODEAF_GO_FIX_LOOPGUARD_CYCLE_CAP") {
		cap := int(math.Min(maxPeriod, math.Min(math.Floor(float64(len(history))/2), 512)))
		for period := 2; period <= cap; period++ {
			required := float64(period) * minOccurrences
			if float64(len(history)) < required {
				continue
			}
			start := len(history) - int(required)
			matches := true
			for offset := period; float64(offset) < required && matches; offset++ {
				if history[start+offset] != history[start+(offset%period)] {
					matches = false
				}
			}
			if matches {
				return strptr("cycle detected with period " + jscompat.FormatNumber(float64(period)) +
					" (" + jscompat.FormatNumber(minOccurrences) + " occurrences)")
			}
		}
		return nil
	}

	for period := 2.0; period <= maxPeriod; period++ {
		required := period * minOccurrences
		if float64(len(history)) < required {
			continue
		}
		start := len(history) - int(required)
		matches := true
		for offset := int(period); float64(offset) < required && matches; offset++ {
			if history[start+offset] != history[start+(offset%int(period))] {
				matches = false
			}
		}
		if matches {
			return strptr("cycle detected with period " + jscompat.FormatNumber(period) +
				" (" + jscompat.FormatNumber(minOccurrences) + " occurrences)")
		}
	}
	return nil
}

func positiveInteger(value *float64, fallback float64) float64 {
	if value != nil && isFinite(*value) {
		return math.Max(1, math.Floor(*value))
	}
	return fallback
}

func optionalNonNegative(value *float64) *float64 {
	if value != nil && isFinite(*value) {
		clamped := math.Max(0, *value)
		return &clamped
	}
	return nil
}

func clamp(value float64, minimum float64, maximum float64) float64 {
	if isFinite(value) {
		return math.Max(minimum, math.Min(maximum, value))
	}
	return minimum
}

// formatNumber is `value.toFixed(4).replace(/0+$/, "").replace(/\.$/, "")`.
//
// Neither replace is anchored to the fractional part, so this eats trailing
// zeros wherever they land. For finite |value| < 1e21 toFixed always emits a
// ".", which shields the integer part — but at |value| >= 1e21 toFixed returns
// the ToString form and the strip runs into the exponent: 1e30 formats as
// "1e+3". Kept as-is; see the package header.
func formatNumber(value float64) string {
	text := jscompat.ToFixed(value, 4)
	text = strings.TrimRight(text, "0")
	return strings.TrimSuffix(text, ".")
}

// formatPercent is `${Math.round(value * 100)}%`. Math.round is floor(x + 0.5)
// (JS rounds halves toward +Infinity), which is not Go's math.Round.
func formatPercent(value float64) string {
	return jscompat.FormatNumber(math.Floor(value*100+0.5)) + "%"
}

// sliceLast is arr.slice(-limit) for a JS-number limit: a limit at or above
// the length keeps everything. Always returns a fresh backing array, like the
// TS slice, so the guard never aliases a caller's snapshot.
func sliceLast(values []string, limit float64) []string {
	start := 0
	if float64(len(values)) > limit {
		start = len(values) - int(limit)
	}
	out := make([]string, len(values)-start)
	copy(out, values[start:])
	return out
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func nullish(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func strptr(value string) *string { return &value }
