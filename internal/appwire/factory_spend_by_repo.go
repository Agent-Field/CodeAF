package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.spend_by_repo: what each repository spent, by the factory ───────
//
// factory.spend_by_repo {factory, from, to} reads the factory's
// GET /v1/spend/repos: each repository's job spend over the factory's own
// calendar days, from and to inclusive (YYYY-MM-DD, at most
// [factory.SpendDaysMax] days), for the repositories the caller can read.
//
// IT IS THE FACTORY'S FIGURE, NOT THIS ENGINE'S LEDGER. In a team the jobs run
// on other people's runners, whose model calls this engine never records;
// spend.report has no repository and cannot say this. The factory keeps every
// job's spend on the job's document, so it answers for every runner.
//
// EACH JOB'S WHOLE SPEND IS PLACED ON ONE DAY, the day its run started (a job
// that never ran: the day it was created), because the document keeps one
// running figure and no spend by day. A run that crosses a month counts
// entirely in the month it started, and a job that ran again counts only its
// latest run. [api.SpendByRepo] is the factory's statement of the rule.

// FeatureFactorySpendByRepo is announced in hello when the engine serves
// factory.spend_by_repo.
const FeatureFactorySpendByRepo = "factory.spend_by_repo"

// FactorySpendByRepoParams names one factory and a range of its days.
type FactorySpendByRepoParams struct {
	Factory string `json:"factory"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// FactorySpendByRepo is the factory's answer with the connection it came from.
type FactorySpendByRepo struct {
	Factory string `json:"factory"`
	api.SpendByRepo
}

// FactorySpender is the engine's half of factory.spend_by_repo.
type FactorySpender interface {
	FactorySpendByRepo(ctx context.Context, p FactorySpendByRepoParams) (FactorySpendByRepo, error)
}

func withFactorySpendByRepoFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactorySpender); ok {
		hello = withFeature(hello, FeatureFactorySpendByRepo)
	}
	return hello
}

func init() {
	const name = "factory.spend_by_repo"
	methods[name] = shaped[FactorySpendByRepoParams, FactorySpendByRepo](func(e Engine, raw json.RawMessage) (*FactorySpendByRepo, error) {
		s, ok := e.(FactorySpender)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactorySpendByRepoParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		p.Factory, p.From, p.To = id, strings.TrimSpace(p.From), strings.TrimSpace(p.To)
		// The factory reads the days in its own zone; the bounds do not depend on it.
		if _, _, err := factory.SpendRange(p.From, p.To, time.UTC); err != nil {
			return nil, Errorf(CodeBadParams, "%s", err.Error())
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := s.FactorySpendByRepo(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
