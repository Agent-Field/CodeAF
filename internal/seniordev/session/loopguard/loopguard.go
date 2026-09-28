//go:build !windows

// Package loopguard is a pure repetition and budget guard for agent action
// loops: it stops a run that repeats the same action, cycles through a short
// sequence of actions, or exceeds a cost or action budget, and warns once
// before a budget runs out. It has no I/O and no clock; the caller persists
// the snapshot it returns.
package loopguard

import (
	"math"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

const (
	repeatCapDefault           = 3
	maxCyclePeriodDefault      = 4
	cycleMinOccurrencesDefault = 2
	warnFractionDefault        = 0.8
	// maxCyclePeriodScan bounds the cycle scan regardless of configuration:
	// a cycle longer than half the retained history cannot be observed twice.
	maxCyclePeriodScan = 512
)

// LoopAction is one observed tool invocation. CostUsd is optional.
type LoopAction struct {
	Tool    string   `json:"tool"`
	ArgsKey string   `json:"argsKey"`
	CostUsd *float64 `json:"costUsd,omitempty"`
}

// LoopStatus is the verdict severity.
type LoopStatus string

const (
	LoopStatusOK   LoopStatus = "ok"
	LoopStatusWarn LoopStatus = "warn"
	LoopStatusStop LoopStatus = "stop"
)

// LoopVerdict is the result of observing one action. Reason is nil for an
// "ok" verdict.
type LoopVerdict struct {
	Status LoopStatus `json:"status"`
	Reason *string    `json:"reason,omitempty"`
}

// LoopGuardOptions configures a guard. A nil field selects the default.
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

// LoopGuardSnapshot is the guard's persistable state.
type LoopGuardSnapshot struct {
	Version           int      `json:"version"`
	Actions           []string `json:"actions"`
	ActionCount       int      `json:"actionCount"`
	CumulativeCostUsd float64  `json:"cumulativeCostUsd"`
	WarnedCost        bool     `json:"warnedCost"`
	WarnedActions     bool     `json:"warnedActions"`
	Stopped           bool     `json:"stopped"`
}

// LoopGuard observes actions and costs and reports when the loop should stop.
type LoopGuard interface {
	Observe(action LoopAction) LoopVerdict
	// ObserveCost charges provider calls that have no tool action. It does
	// not alter repetition or action counts.
	ObserveCost(costUsd *float64) LoopVerdict
	Snapshot() LoopGuardSnapshot
	// Restore ignores a nil snapshot and one with an unknown version.
	Restore(snapshot *LoopGuardSnapshot)
}

type loopGuard struct {
	repeatCap           int
	maxCyclePeriod      int
	cycleMinOccurrences int
	maxCostUsd          *float64
	maxActions          *int
	warnFraction        float64
	historyLimit        int

	actions           []string
	actionCount       int
	cumulativeCostUsd float64
	warnedCost        bool
	warnedActions     bool
	stopped           bool
}

// CreateLoopGuard creates a stateful but otherwise pure loop guard.
func CreateLoopGuard(options LoopGuardOptions) LoopGuard {
	repeatCap := positiveInteger(options.RepeatCap, repeatCapDefault)
	maxCyclePeriod := max(2, positiveInteger(options.MaxCyclePeriod, maxCyclePeriodDefault))
	cycleMinOccurrences := max(2, positiveInteger(options.CycleMinOccurrences, cycleMinOccurrencesDefault))
	var maxActions *int
	if options.MaxActions != nil && isFinite(*options.MaxActions) {
		n := int(math.Ceil(math.Max(0, *options.MaxActions)))
		maxActions = &n
	}
	warnFraction := warnFractionDefault
	if options.WarnFraction != nil {
		warnFraction = math.Min(1, math.Max(0, *options.WarnFraction))
	}
	return &loopGuard{
		repeatCap:           repeatCap,
		maxCyclePeriod:      maxCyclePeriod,
		cycleMinOccurrences: cycleMinOccurrences,
		maxCostUsd:          optionalNonNegative(options.MaxCostUsd),
		maxActions:          maxActions,
		warnFraction:        warnFraction,
		historyLimit:        max(repeatCap, maxCyclePeriod*cycleMinOccurrences),
		actions:             []string{},
	}
}

func (g *loopGuard) Observe(action LoopAction) LoopVerdict {
	if g.stopped {
		return LoopVerdict{Status: LoopStatusStop, Reason: strptr("loop guard already stopped")}
	}

	g.actions = append(g.actions, encodeAction(action))
	if len(g.actions) > g.historyLimit {
		g.actions = sliceLast(g.actions, g.historyLimit)
	}
	g.actionCount++

	if action.CostUsd != nil && isFinite(*action.CostUsd) && *action.CostUsd > 0 {
		g.cumulativeCostUsd += *action.CostUsd
	}

	// A terminal repetition finding takes precedence over a budget warning.
	if reason := exactRepeatReason(g.actions, g.repeatCap); reason != nil {
		g.stopped = true
		return LoopVerdict{Status: LoopStatusStop, Reason: reason}
	}
	if reason := cycleDetectionReason(g.actions, g.maxCyclePeriod, g.cycleMinOccurrences); reason != nil {
		g.stopped = true
		return LoopVerdict{Status: LoopStatusStop, Reason: reason}
	}

	var stopReasons, warnReasons []string
	if g.maxCostUsd != nil {
		maxCostUsd := *g.maxCostUsd
		if g.cumulativeCostUsd >= maxCostUsd {
			stopReasons = append(stopReasons,
				"cost budget reached ("+formatNumber(g.cumulativeCostUsd)+"/"+formatNumber(maxCostUsd)+" USD)")
		} else if !g.warnedCost && g.cumulativeCostUsd >= maxCostUsd*g.warnFraction {
			g.warnedCost = true
			warnReasons = append(warnReasons, "cost budget at "+formatPercent(g.cumulativeCostUsd/maxCostUsd))
		}
	}
	if g.maxActions != nil {
		maxActions := *g.maxActions
		if g.actionCount >= maxActions {
			stopReasons = append(stopReasons,
				"action budget reached ("+strconv.Itoa(g.actionCount)+"/"+strconv.Itoa(maxActions)+")")
		} else if !g.warnedActions && float64(g.actionCount) >= float64(maxActions)*g.warnFraction {
			g.warnedActions = true
			warnReasons = append(warnReasons,
				"action budget at "+formatPercent(float64(g.actionCount)/float64(maxActions)))
		}
	}

	if len(stopReasons) > 0 {
		g.stopped = true
		return LoopVerdict{Status: LoopStatusStop, Reason: strptr(strings.Join(stopReasons, "; "))}
	}
	if len(warnReasons) > 0 {
		return LoopVerdict{Status: LoopStatusWarn, Reason: strptr(strings.Join(warnReasons, "; "))}
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
			"cost budget reached (" + formatNumber(g.cumulativeCostUsd) + "/" + formatNumber(maximum) + " USD)",
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

func (g *loopGuard) Restore(snapshot *LoopGuardSnapshot) {
	if snapshot == nil || snapshot.Version != 1 {
		return
	}
	if snapshot.Actions != nil {
		g.actions = sliceLast(snapshot.Actions, g.historyLimit)
	}
	if snapshot.ActionCount >= 0 {
		g.actionCount = snapshot.ActionCount
	}
	if isFinite(snapshot.CumulativeCostUsd) && snapshot.CumulativeCostUsd >= 0 {
		g.cumulativeCostUsd = snapshot.CumulativeCostUsd
	}
	g.warnedCost = snapshot.WarnedCost
	g.warnedActions = snapshot.WarnedActions
	g.stopped = snapshot.Stopped
}

// encodeAction is the history key for an action: the JSON array of its tool
// name and argument key.
func encodeAction(action LoopAction) string {
	encoded, err := jsonutil.Marshal([2]string{action.Tool, action.ArgsKey})
	if err != nil {
		return action.Tool + "\x00" + action.ArgsKey
	}
	return string(encoded)
}

func exactRepeatReason(history []string, cap int) *string {
	if len(history) < cap {
		return nil
	}
	last := history[len(history)-1]
	for i := len(history) - 2; i >= len(history)-cap; i-- {
		if history[i] != last {
			return nil
		}
	}
	return strptr("exact action repeated " + strconv.Itoa(cap) + " times consecutively")
}

func cycleDetectionReason(history []string, maxPeriod int, minOccurrences int) *string {
	limit := min(maxPeriod, len(history)/2, maxCyclePeriodScan)
	for period := 2; period <= limit; period++ {
		required := period * minOccurrences
		if len(history) < required {
			continue
		}
		start := len(history) - required
		matches := true
		for offset := period; offset < required && matches; offset++ {
			if history[start+offset] != history[start+offset%period] {
				matches = false
			}
		}
		if matches {
			return strptr("cycle detected with period " + strconv.Itoa(period) +
				" (" + strconv.Itoa(minOccurrences) + " occurrences)")
		}
	}
	return nil
}

func positiveInteger(value *float64, fallback int) int {
	if value != nil && isFinite(*value) {
		return int(math.Max(1, math.Min(math.Floor(*value), math.MaxInt32)))
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

func strptr(value string) *string { return &value }

// formatNumber prints a USD amount with at most four decimals.
func formatNumber(value float64) string {
	text := strconv.FormatFloat(value, 'f', 4, 64)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(text, "0")
		text = strings.TrimSuffix(text, ".")
	}
	return text
}

// formatPercent prints a ratio as a whole percentage.
func formatPercent(value float64) string {
	return strconv.Itoa(int(math.Round(value*100))) + "%"
}

// sliceLast returns a copy of the last limit values.
func sliceLast(values []string, limit int) []string {
	start := 0
	if len(values) > limit {
		start = len(values) - limit
	}
	out := make([]string, len(values)-start)
	copy(out, values[start:])
	return out
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
