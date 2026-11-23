package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FeatureFactoryJobSetRepo is announced in hello when the engine serves
// factory.job_set_repo: giving a job that never ran its repository. It is how
// outside work that arrived with none (a Jira ticket no routing rule sent
// anywhere) stops waiting in Backlog.
const FeatureFactoryJobSetRepo = "factory.job_set_repo"

// JobActSetRepo names factory.job_set_repo in its action receipt.
const JobActSetRepo = "set_repo"

// FactoryJobSetRepoParams is factory.job_set_repo. Repo is `owner/name`;
// Expect optionally guards the job the person read.
type FactoryJobSetRepoParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Repo    string      `json:"repo"`
	Expect  *api.Expect `json:"expect,omitempty"`
}

// FactoryJobRepoSetter is the engine's half of factory.job_set_repo. The
// factory sets the job's repository and that repository's steps, and routes
// it again; only the operator or the job's person may, and a job that has
// started keeps its repository (a conflict in the factory's words).
type FactoryJobRepoSetter interface {
	FactoryJobSetRepo(context.Context, FactoryJobSetRepoParams) (FactoryJobActed, error)
}

func withFactoryJobSetRepoFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(FactoryJobRepoSetter); ok {
		if _, factory := e.(Factory); factory {
			hello = withFeature(hello, FeatureFactoryJobSetRepo)
		}
	}
	return hello
}

func init() {
	methods[FeatureFactoryJobSetRepo] = shaped[FactoryJobSetRepoParams, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		a, ok := e.(FactoryJobRepoSetter)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryJobSetRepo)
		}
		var p FactoryJobSetRepoParams
		if err := decodeStrict(FeatureFactoryJobSetRepo, raw, &p); err != nil {
			return nil, err
		}
		if err := checkJobTarget(&p.Factory, p.Job); err != nil {
			return nil, err
		}
		if p.Expect != nil {
			if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
				return nil, err
			}
		}
		p.Repo = strings.TrimSpace(p.Repo)
		if !factory.RepoName(p.Repo) {
			return nil, Errorf(CodeBadParams, "repo must be owner/name")
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryJobSetRepo(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
