package appwire

import (
	"context"
	"encoding/json"
	"time"
)

// ── factory.usage_by_model: which models the factory's jobs used ───────────
//
// Spending's "Usage by model" for factory jobs: over a range of local days,
// every model the factory's jobs called on THIS COMPUTER, how it is paid, the
// calls, the tokens and the priced dollars; and the same per job. It is read
// from this machine's spend ledger, where every call made for a factory job
// names the job, so a job another computer ran is not here (scope says so).
//
// UNKNOWN IS ABSENT. paid is absent when neither the calls nor the model
// catalog said one way; cost_usd is absent when no call was priced, and
// unpriced_calls counts the calls that carried no dollars (a plan sign-in, a
// model on this machine, a model with no published price) — never $0.
//
// IT IS THIS MACHINE'S FACTORY ONLY, as factory.job_chats is.

// FeatureFactoryUsageByModel is announced in hello when the engine serves
// factory.usage_by_model.
const FeatureFactoryUsageByModel = "factory.usage_by_model"

// FactoryUsageDaysMax is the longest range one read covers.
const FactoryUsageDaysMax = 31

// FactoryUsageThisComputer is the only scope: the calls this computer made.
const FactoryUsageThisComputer = "this_computer"

// FactoryUsageParams names a factory, local days from..to (YYYY-MM-DD, both
// included, at most [FactoryUsageDaysMax]) and, optionally, one job.
type FactoryUsageParams struct {
	Factory string `json:"factory"`
	From    string `json:"from"`
	To      string `json:"to"`
	Job     int    `json:"job,omitempty"`
}

// FactoryUsage is the answer: models over every job of the range (only the
// asked job's when job was given), and jobs, each job with calls in the range.
type FactoryUsage struct {
	Factory string            `json:"factory"`
	From    string            `json:"from"`
	To      string            `json:"to"`
	Scope   string            `json:"scope" enum:"this_computer"`
	Models  []FactoryModelUse `json:"models"`
	Jobs    []FactoryJobUse   `json:"jobs"`
}

// FactoryJobUse is one job's models.
type FactoryJobUse struct {
	Job    int               `json:"job"`
	Ref    string            `json:"ref"`
	Models []FactoryModelUse `json:"models"`
}

// FactoryModelUse is one model's use. paid is how the calls were paid:
// metered (an API key, dollars per call), plan (a sign-in such as ChatGPT or
// Claude Max: tokens only), local (a model on this machine), free (a free
// pool). Model is absent for calls that named none.
type FactoryModelUse struct {
	Model         string   `json:"model,omitempty"`
	Paid          string   `json:"paid,omitempty" enum:"metered,plan,local,free"`
	Calls         int      `json:"calls"`
	TokensIn      int64    `json:"tokens_in"`
	TokensOut     int64    `json:"tokens_out"`
	CostUSD       *float64 `json:"cost_usd,omitempty"`
	UnpricedCalls int      `json:"unpriced_calls,omitempty"`
	UnbilledCalls int      `json:"unbilled_calls,omitempty"`
}

// FactoryUsageReader is the engine's half of factory.usage_by_model.
type FactoryUsageReader interface {
	FactoryUsageByModel(ctx context.Context, p FactoryUsageParams) (FactoryUsage, error)
}

func withFactoryUsageFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryUsageReader); ok {
		hello = withFeature(hello, FeatureFactoryUsageByModel)
	}
	return hello
}

func prepFactoryUsage(p *FactoryUsageParams) error {
	id, err := factoryID(p.Factory)
	if err != nil {
		return err
	}
	p.Factory = id
	from, errFrom := time.Parse(time.DateOnly, p.From)
	to, errTo := time.Parse(time.DateOnly, p.To)
	switch {
	case errFrom != nil || errTo != nil:
		return Errorf(CodeBadParams, "from and to are local days, YYYY-MM-DD")
	case to.Before(from):
		return Errorf(CodeBadParams, "to is before from")
	case int(to.Sub(from).Hours()/24)+1 > FactoryUsageDaysMax:
		return Errorf(CodeBadParams, "%s covers at most %d days", FeatureFactoryUsageByModel, FactoryUsageDaysMax)
	case p.Job < 0:
		return Errorf(CodeBadParams, "job must be a job's number")
	}
	return nil
}

func init() {
	methods[FeatureFactoryUsageByModel] = shaped[FactoryUsageParams, FactoryUsage](factoryUsage)
}

func factoryUsage(e Engine, raw json.RawMessage) (*FactoryUsage, error) {
	const name = FeatureFactoryUsageByModel
	r, ok := e.(FactoryUsageReader)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryUsageParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	if err := prepFactoryUsage(&p); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryUsageByModel(ctx, p)
	if err != nil {
		return nil, err
	}
	out.Models, out.Jobs = nonNil(out.Models), nonNil(out.Jobs)
	return &out, nil
}
