package appwire

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// FeatureFactoryRepoIssues announces a read of one repository's GitHub issues.
const FeatureFactoryRepoIssues = "factory.repo_issues"

// FactoryRepoIssuesParams selects one bounded read, with no lower bound when
// Since is absent. A nil Page defaults to 1; an explicit zero is invalid.
type FactoryRepoIssuesParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	Since   string `json:"since,omitempty"`
	Page    *int   `json:"page,omitempty"`
}

// FactoryRepoIssues names the connection that supplied the repository issues.
type FactoryRepoIssues struct {
	Factory string `json:"factory"`
	api.RepoIssues
}

// FactoryRepoIssuesReader is the engine's read-only repository issue interface.
type FactoryRepoIssuesReader interface {
	FactoryRepoIssues(context.Context, FactoryRepoIssuesParams) (FactoryRepoIssues, error)
}

func withFactoryRepoIssuesFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRepoIssuesReader); ok {
		hello = withFeature(hello, FeatureFactoryRepoIssues)
	}
	return hello
}

// CheckRepoIssues checks the repository and cursor before any request is sent.
func CheckRepoIssues(p FactoryRepoIssuesParams) (FactoryRepoIssuesParams, error) {
	var err error
	if p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo); err != nil {
		return p, err
	}
	if p.Page == nil {
		n := 1
		p.Page = &n
	}
	if err := api.CheckRepoIssues(p.Since, *p.Page); err != nil {
		return p, Errorf(CodeBadParams, "%s", err)
	}
	return p, nil
}

func init() {
	methods[FeatureFactoryRepoIssues] = shaped[FactoryRepoIssuesParams, FactoryRepoIssues](readRepoIssues)
}

func readRepoIssues(e Engine, raw json.RawMessage) (*FactoryRepoIssues, error) {
	r, ok := e.(FactoryRepoIssuesReader)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", FeatureFactoryRepoIssues)
	}
	var p FactoryRepoIssuesParams
	if err := decodeStrict(FeatureFactoryRepoIssues, raw, &p); err != nil {
		return nil, err
	}
	p, err := CheckRepoIssues(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := r.FactoryRepoIssues(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
