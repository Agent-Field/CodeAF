package appwire

import (
	"cmp"
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.repo_facts: one repository's facts, counted by the factory ──────
//
// factory.repo_facts {factory, repo, days?, months?} reads the factory's
// GET /v1/repos/{owner}/{repo}/facts: deterministic facts about one
// repository counted over every job document the factory keeps, not only the
// recent jobs factory.queue holds. Each fact is the desk's fact shape
// ([api.Fact]): a stable id, a number, its own phrase (never a bare number)
// and a source beginning "engine:". It answers jobs finished and pull
// requests merged in the last days and in this calendar month, spend by
// calendar month (factory.spend_by_repo's placement rule), jobs, rounds and
// questions by area (job.area's rule), and jobs stuck on one step for a day
// or more, as of the factory's now in its zone (time_zone).
//
// days is the recent window, 1 to [factory.SpendDaysMax] calendar days ending
// today (30 when absent); months is how many calendar months of spend, this
// one included, 1 to [api.RepoFactsMonthsMost] (3 when absent). The answer is
// bounded: at most [api.RepoFactsAreasMost] areas and as many stuck steps,
// the busiest first, so at most 97 facts; truncated says how many were left
// out. A caller who cannot read the repository is answered not found.

// FeatureFactoryRepoFacts is announced in hello when the engine serves
// factory.repo_facts.
const FeatureFactoryRepoFacts = "factory.repo_facts"

// FactoryRepoFactsParams names one factory, one repository and the window.
// Zero days or months is the default.
type FactoryRepoFactsParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	Days    int    `json:"days,omitempty"`
	Months  int    `json:"months,omitempty"`
}

// FactoryRepoFacts is the factory's answer with the connection it came from.
type FactoryRepoFacts struct {
	Factory string `json:"factory"`
	api.RepoFacts
}

// FactoryRepoFactsReader is the engine's half of factory.repo_facts.
type FactoryRepoFactsReader interface {
	FactoryRepoFacts(ctx context.Context, p FactoryRepoFactsParams) (FactoryRepoFacts, error)
}

func withFactoryRepoFactsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRepoFactsReader); ok {
		hello = withFeature(hello, FeatureFactoryRepoFacts)
	}
	return hello
}

// CheckRepoFacts trims the connection and repository and fills the default
// window, or refuses the read in words a person can act on.
func CheckRepoFacts(p FactoryRepoFactsParams) (FactoryRepoFactsParams, error) {
	var err error
	if p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo); err != nil {
		return p, err
	}
	p.Days = cmp.Or(p.Days, api.RepoFactsDaysDefault)
	p.Months = cmp.Or(p.Months, api.RepoFactsMonthsDefault)
	if p.Days < 1 || p.Days > factory.SpendDaysMax {
		return p, Errorf(CodeBadParams, "days is from 1 to %d", factory.SpendDaysMax)
	}
	if p.Months < 1 || p.Months > api.RepoFactsMonthsMost {
		return p, Errorf(CodeBadParams, "months is from 1 to %d", api.RepoFactsMonthsMost)
	}
	return p, nil
}

func init() {
	const name = "factory.repo_facts"
	methods[name] = shaped[FactoryRepoFactsParams, FactoryRepoFacts](func(e Engine, raw json.RawMessage) (*FactoryRepoFacts, error) {
		r, ok := e.(FactoryRepoFactsReader)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactoryRepoFactsParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckRepoFacts(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := r.FactoryRepoFacts(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
