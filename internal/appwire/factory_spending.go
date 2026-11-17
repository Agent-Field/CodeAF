package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── THE FACTORY'S OWN SPENDING DEFAULTS ────────────────────────────────────
//
// factory.spending reads, and factory.spending_set changes, three values a
// factory keeps for every repository it watches ([factory.Spending]):
//
//   - default_budget_usd: the budget a job gets when it has none and its
//     repository's file names no default_budget. A positive amount, or null
//     for not set (the built-in $5). A job always has a budget, so 0 is refused.
//   - auto_start_max_usd: the most a job may be budgeted and still start on
//     its own; above it the job asks first. Zero or more, or null for not set
//     (the built-in $5). A repository's own stricter cap still holds. Zero
//     makes every job that would start on its own ask first.
//   - default_model: the model a step runs on when neither the step nor its
//     job names one, a model models.list offers, or "" for not set (the
//     runner's own default). A runner on one model for every task keeps it.
//
// factory.spending_set PARAMS ARE {factory} AND THE FIELDS TO CHANGE, at
// least one: an absent field keeps the factory's value, null clears a figure
// and "" clears the model. `expect` is the three values as last read; sent, a
// factory that keeps anything else changes nothing and the call is
// [CodeStale]. Values are checked here as the factory checks them, so a
// request it would refuse is refused before anything is sent.
//
// ONLY THE OPERATOR MAY CHANGE THEM, because a default binds every
// repository and every person's work. A WRITE IS SENT ONCE; an outcome the
// engine cannot confirm is [CodeUncertain], and factory.spending says what was
// actually kept.

// The hello words of the three settings. An engine that serves factory.spending
// and factory.spending_set announces all three.
const (
	FeatureFactoryDefaultBudget = "factory.default_budget"
	FeatureFactoryAutoStartCap  = "factory.auto_start_cap"
	FeatureFactoryDefaultModel  = "factory.default_model"
)

// FactorySpendingValues are the three values, each null or "" when not set.
type FactorySpendingValues struct {
	DefaultBudgetUSD *float64 `json:"default_budget_usd" nullable:"true"`
	AutoStartMaxUSD  *float64 `json:"auto_start_max_usd" nullable:"true"`
	DefaultModel     string   `json:"default_model"`
}

// FactorySpending is both methods' answer: the connection and the values the
// factory keeps.
type FactorySpending struct {
	Factory          string   `json:"factory"`
	DefaultBudgetUSD *float64 `json:"default_budget_usd" nullable:"true"`
	AutoStartMaxUSD  *float64 `json:"auto_start_max_usd" nullable:"true"`
	DefaultModel     string   `json:"default_model"`
}

// FactorySpendingSetParams is factory.spending_set's params as they travel.
// The figures are null to clear; each field is optional.
type FactorySpendingSetParams struct {
	Factory          string                 `json:"factory"`
	DefaultBudgetUSD *float64               `json:"default_budget_usd,omitempty" nullable:"true"`
	AutoStartMaxUSD  *float64               `json:"auto_start_max_usd,omitempty" nullable:"true"`
	DefaultModel     *string                `json:"default_model,omitempty"`
	Expect           *FactorySpendingValues `json:"expect,omitempty"`
}

// FactorySpendingSet is factory.spending_set as the engine is handed it: the
// connection and the change, already checked.
type FactorySpendingSet struct {
	Factory string
	Patch   api.SpendingPatch
}

// FactorySpendingKeeper is the engine's half of factory.spending and
// factory.spending_set.
type FactorySpendingKeeper interface {
	FactorySpending(ctx context.Context, id string) (FactorySpending, error)
	FactorySpendingSet(ctx context.Context, p FactorySpendingSet) (FactorySpending, error)
}

// FactorySpendingOf is one factory's [factory.Spending] as the wire says it.
func FactorySpendingOf(id string, s factory.Spending) FactorySpending {
	return FactorySpending{Factory: id, DefaultBudgetUSD: s.DefaultBudgetUSD, AutoStartMaxUSD: s.AutoStartMaxUSD, DefaultModel: s.DefaultModel}
}

func withFactorySpendingFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactorySpendingKeeper); ok {
		for _, word := range []string{FeatureFactoryDefaultBudget, FeatureFactoryAutoStartCap, FeatureFactoryDefaultModel} {
			hello = withFeature(hello, word)
		}
	}
	return hello
}

// spendingSetRaw is factory.spending_set's params as read: the figures stay
// raw so an absent one and a null one differ.
type spendingSetRaw struct {
	Factory          string                 `json:"factory"`
	DefaultBudgetUSD json.RawMessage        `json:"default_budget_usd"`
	AutoStartMaxUSD  json.RawMessage        `json:"auto_start_max_usd"`
	DefaultModel     *string                `json:"default_model"`
	Expect           *FactorySpendingValues `json:"expect"`
}

// DecodeSpendingSet reads factory.spending_set's params STRICTLY and checks
// them without asking anyone: a connection, at least one field, and values the
// factory would keep.
func DecodeSpendingSet(raw json.RawMessage) (FactorySpendingSet, error) {
	var p spendingSetRaw
	if err := decodeStrict("factory.spending_set", raw, &p); err != nil {
		return FactorySpendingSet{}, err
	}
	id, err := factoryID(strings.TrimSpace(p.Factory))
	if err != nil {
		return FactorySpendingSet{}, err
	}
	patch := api.SpendingPatch{DefaultBudgetUSD: p.DefaultBudgetUSD, AutoStartMaxUSD: p.AutoStartMaxUSD, DefaultModel: p.DefaultModel}
	if patch.Empty() {
		return FactorySpendingSet{}, Errorf(CodeBadParams, "name at least one of default_budget_usd, auto_start_max_usd and default_model")
	}
	if err := checkSpending(patch.Apply(factory.Spending{})); err != nil {
		return FactorySpendingSet{}, err
	}
	if e := p.Expect; e != nil {
		expect := factory.Spending{DefaultBudgetUSD: e.DefaultBudgetUSD, AutoStartMaxUSD: e.AutoStartMaxUSD, DefaultModel: strings.TrimSpace(e.DefaultModel)}
		if err := expect.Check(); err != nil {
			return FactorySpendingSet{}, Errorf(CodeBadParams, "expect: %s", err.Error())
		}
		patch.Expect = &expect
	}
	return FactorySpendingSet{Factory: id, Patch: patch}, nil
}

// checkSpending is bad_params for a patch that cannot be read or kept.
func checkSpending(next factory.Spending, err error) error {
	if err == nil {
		err = next.Check()
	}
	if err != nil {
		return Errorf(CodeBadParams, "%s", err.Error())
	}
	return nil
}

func spendingKeeper(e Engine, name string) (FactorySpendingKeeper, error) {
	k, ok := e.(FactorySpendingKeeper)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return k, nil
}

func init() {
	methods["factory.spending"] = typed(func(e Engine, p FactoryRef) (*FactorySpending, error) {
		k, err := spendingKeeper(e, "factory.spending")
		if err != nil {
			return nil, err
		}
		id, err := factoryID(strings.TrimSpace(p.Factory))
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactorySpending(ctx, id)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	methods["factory.spending_set"] = shaped[FactorySpendingSetParams, FactorySpending](func(e Engine, raw json.RawMessage) (*FactorySpending, error) {
		k, err := spendingKeeper(e, "factory.spending_set")
		if err != nil {
			return nil, err
		}
		p, err := DecodeSpendingSet(raw)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactorySpendingSet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
