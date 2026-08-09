// Package runbudget ports src/session/run-budget.ts:1-113 from swe-pro commit
// 3b25a1a. The tracker keeps JS-number arithmetic and message formatting,
// including Number.prototype.toFixed and Math.round behavior.
package runbudget

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// RunBudget is an explicitly supplied run-level budget. Nil means the TS field
// is undefined. JSON null is retained as NaN by UnmarshalJSON so IsBounded
// preserves the TS `null !== undefined` quirk on cast-invalid inputs.
type RunBudget struct {
	MaxCostUSD *float64 `json:"maxCostUsd,omitempty"`
	MaxWallMS  *float64 `json:"maxWallMs,omitempty"`
}

// UnmarshalJSON keeps an explicit null distinct from an absent property. The
// TS type excludes null, but cast/JSON-derived callers can still supply it and
// IsBounded observes `null !== undefined` as true. NaN is the internal null
// sentinel; JSON.stringify also maps NaN back to null.
func (budget *RunBudget) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	decode := func(name string) (*float64, error) {
		raw, present := fields[name]
		if !present {
			return nil, nil
		}
		value := math.NaN()
		if string(raw) != "null" {
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
		}
		return &value, nil
	}
	var err error
	if budget.MaxCostUSD, err = decode("maxCostUsd"); err != nil {
		return err
	}
	if budget.MaxWallMS, err = decode("maxWallMs"); err != nil {
		return err
	}
	return nil
}

// MarshalJSON emits only present TS properties, in object-literal order.
func (budget RunBudget) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	wrote := false
	appendNumber := func(name string, value *float64) error {
		if value == nil {
			return nil
		}
		if wrote {
			out.WriteByte(',')
		}
		wrote = true
		out.WriteString(`"` + name + `":`)
		encoded, err := jscompat.Stringify(jscompat.JSNumber(*value))
		if err != nil {
			return err
		}
		out.Write(encoded)
		return nil
	}
	if err := appendNumber("maxCostUsd", budget.MaxCostUSD); err != nil {
		return nil, err
	}
	if err := appendNumber("maxWallMs", budget.MaxWallMS); err != nil {
		return nil, err
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// RunBudgetFlags mirrors the two CLI flag values.
type RunBudgetFlags struct {
	MaxCost  *float64 `json:"maxCost"`
	MaxHours *float64 `json:"maxHours"`
}

// BudgetExhaustion is the result of a tracker check.
type BudgetExhaustion struct {
	Yes    bool    `json:"yes"`
	Reason *string `json:"reason,omitempty"`
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func positiveNumber(n float64) (float64, bool) {
	return n, finite(n) && n > 0
}

// stringNumber is JS Number(string), with the syntax that strconv.ParseFloat
// improperly accepts (numeric separators and case-insensitive infinities)
// rejected before delegating to jscompat.
func stringNumber(raw string) float64 {
	trimmed := jscompat.Trim(raw)
	if strings.ContainsRune(trimmed, '_') {
		return math.NaN()
	}
	hasSign := strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-")
	unsigned := strings.TrimPrefix(strings.TrimPrefix(trimmed, "+"), "-")
	switch strings.ToLower(unsigned) {
	case "inf", "infinity", "nan":
		if unsigned != "Infinity" {
			return math.NaN()
		}
	}
	if len(unsigned) > 2 && unsigned[0] == '0' {
		base := 0
		switch unsigned[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			if hasSign {
				return math.NaN()
			}
			integer, ok := new(big.Int).SetString(unsigned[2:], base)
			if !ok {
				return math.NaN()
			}
			number, _ := new(big.Float).SetInt(integer).Float64()
			return number
		}
	}
	return jscompat.ToNumber(trimmed)
}

func positiveString(raw string, present bool) (float64, bool) {
	if !present || raw == "" {
		return 0, false
	}
	return positiveNumber(stringNumber(raw))
}

// ResolveRunBudget applies flag-over-environment precedence. A non-nil env map
// is used exactly (including an explicitly empty map); nil reads process env.
func ResolveRunBudget(flags *RunBudgetFlags, env map[string]string) RunBudget {
	lookup := func(name string) (string, bool) {
		if env != nil {
			value, ok := env[name]
			return value, ok
		}
		return os.LookupEnv(name)
	}

	var maxCost float64
	var hasMaxCost bool
	if flags != nil && flags.MaxCost != nil {
		maxCost, hasMaxCost = positiveNumber(*flags.MaxCost)
	}
	if !hasMaxCost {
		raw, present := lookup("CODEAF_MAX_COST_USD")
		maxCost, hasMaxCost = positiveString(raw, present)
	}

	var maxHours float64
	var hasMaxHours bool
	if flags != nil && flags.MaxHours != nil {
		maxHours, hasMaxHours = positiveNumber(*flags.MaxHours)
	}
	if !hasMaxHours {
		raw, present := lookup("CODEAF_MAX_WALL_H")
		maxHours, hasMaxHours = positiveString(raw, present)
	}

	budget := RunBudget{}
	if hasMaxCost {
		budget.MaxCostUSD = &maxCost
	}
	if hasMaxHours {
		maxWall := maxHours * 3_600_000
		budget.MaxWallMS = &maxWall
	}
	return budget
}

// IsBounded reports whether either property is present.
func IsBounded(budget RunBudget) bool {
	return budget.MaxCostUSD != nil || budget.MaxWallMS != nil
}

// BudgetTracker is the mutable accumulator returned by MakeBudgetTracker.
type BudgetTracker struct {
	Budget  RunBudget
	startTS float64
	cost    float64
	now     func() float64
}

// MakeBudgetTracker creates a tracker. The optional prior cost defaults to 0.
func MakeBudgetTracker(budget RunBudget, startTS float64, priorCostUSD ...float64) *BudgetTracker {
	prior := float64(0)
	if len(priorCostUSD) > 0 && finite(priorCostUSD[0]) && priorCostUSD[0] > 0 {
		prior = priorCostUSD[0]
	}
	return &BudgetTracker{
		Budget:  budget,
		startTS: startTS,
		cost:    prior,
		now:     func() float64 { return float64(time.Now().UnixMilli()) },
	}
}

// AddCost accumulates a positive finite provider-cost delta.
func (t *BudgetTracker) AddCost(usd float64) {
	if finite(usd) && usd > 0 {
		t.cost += usd
	}
}

// CostUSD returns the accumulated provider cost.
func (t *BudgetTracker) CostUSD() float64 { return t.cost }

func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == 0 {
		return x
	}
	if x > 0 && x < 0.5 {
		return 0
	}
	if x < 0 && x >= -0.5 {
		return math.Copysign(0, -1)
	}
	r := math.Floor(x + 0.5)
	if r-x > 0.5 {
		r--
	}
	return r
}

// Exhausted checks cost first and wall time second, preserving TS branch
// precedence. Omit nowMS to use the ambient wall clock.
func (t *BudgetTracker) Exhausted(nowMS ...float64) BudgetExhaustion {
	now := t.now()
	if len(nowMS) > 0 {
		now = nowMS[0]
	}
	if t.Budget.MaxCostUSD != nil && t.cost >= *t.Budget.MaxCostUSD {
		reason := "cost $" + jscompat.ToFixed(t.cost, 4) +
			" >= budget $" + jscompat.ToFixed(*t.Budget.MaxCostUSD, 4)
		return BudgetExhaustion{Yes: true, Reason: &reason}
	}
	if t.Budget.MaxWallMS != nil {
		elapsed := now - t.startTS
		if elapsed >= *t.Budget.MaxWallMS {
			reason := "wall " + jscompat.FormatNumber(jsRound(elapsed/1000)) +
				"s >= budget " + jscompat.FormatNumber(jsRound(*t.Budget.MaxWallMS/1000)) + "s"
			return BudgetExhaustion{Yes: true, Reason: &reason}
		}
	}
	return BudgetExhaustion{Yes: false}
}
