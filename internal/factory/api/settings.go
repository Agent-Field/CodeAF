package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// FactorySettings carries only runner policy, never host credentials.
type FactorySettings = store.RunnerSettings

type PatchFactorySettings struct {
	RunnerTimeoutSeconds *int  `json:"runner_timeout_s,omitempty"`
	KeepOutputDays       *int  `json:"keep_output_days,omitempty"`
	KeepOutputMB         *int  `json:"keep_output_mb,omitempty"`
	ReadOnArrival        *bool `json:"read_on_arrival,omitempty"`
}

// Spending is GET /v1/spending and the answer to a PATCH of it: the factory's
// default budget, its cap on what starts on its own, and its default model,
// each absent when not set (factory.Spending).
type Spending = factory.Spending

// SpendingPatch is the whole PATCH /v1/spending body. A field that is absent
// keeps the factory's value; a figure sent as null, and a model sent as "",
// clear it. Expect is the values as the writer last read them: sent, a factory
// that keeps anything else writes nothing and answers 409.
type SpendingPatch struct {
	DefaultBudgetUSD json.RawMessage `json:"default_budget_usd,omitempty"`
	AutoStartMaxUSD  json.RawMessage `json:"auto_start_max_usd,omitempty"`
	DefaultModel     *string         `json:"default_model,omitempty"`
	Expect           *Spending       `json:"expect,omitempty"`
}

// Empty says the patch changes nothing.
func (p SpendingPatch) Empty() bool {
	return len(p.DefaultBudgetUSD) == 0 && len(p.AutoStartMaxUSD) == 0 && p.DefaultModel == nil
}

// Apply is cur with the patch's fields in place, or why a figure cannot be read.
func (p SpendingPatch) Apply(cur factory.Spending) (factory.Spending, error) {
	var err error
	if cur.DefaultBudgetUSD, err = patchFigure(p.DefaultBudgetUSD, cur.DefaultBudgetUSD); err != nil {
		return cur, fmt.Errorf("default_budget_usd: %w", err)
	}
	if cur.AutoStartMaxUSD, err = patchFigure(p.AutoStartMaxUSD, cur.AutoStartMaxUSD); err != nil {
		return cur, fmt.Errorf("auto_start_max_usd: %w", err)
	}
	if p.DefaultModel != nil {
		cur.DefaultModel = strings.TrimSpace(*p.DefaultModel)
	}
	return cur, nil
}

// patchFigure is one money field of a patch: absent keeps was, null clears it.
func patchFigure(raw json.RawMessage, was *float64) (*float64, error) {
	switch strings.TrimSpace(string(raw)) {
	case "":
		return was, nil
	case "null":
		return nil, nil
	}
	var usd float64
	if err := json.Unmarshal(raw, &usd); err != nil {
		return was, errors.New("a figure is a number of dollars, or null for none")
	}
	return &usd, nil
}
