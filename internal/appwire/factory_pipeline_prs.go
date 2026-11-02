package appwire

import (
	"context"
	"encoding/json"
	"time"
)

// ── THE OPEN PULL REQUESTS THAT CHANGE A REPOSITORY'S PIPELINE FILE ─────────
//
// factory.pipeline_prs lists one repository's OPEN pull requests into its
// default branch that change .codeaf/factory.yaml, from any author, so the app
// can show "pull request #N open" in every window and after a restart, not
// only for one it opened itself. It asks the factory's caller-scoped route
// GET /v1/repos/{owner}/{repo}/pipeline/prs ONCE, as the person who holds the
// connection now; the engine supplies no GitHub token of its own. It writes
// nothing anywhere.
//
// FactoryBranch marks a pull request whose head is a branch of the repository
// named as the factory names the branches it suggests ("codeaf/pipeline-...").
// A person can name a branch that way too, so it is a hint, not authorship.
//
// THE FACTORY'S READ IS BOUNDED (one page of 100 open pull requests, the files
// of at most 20 of them). Complete is false when that bound may have hidden
// one; the list is then what was found, never a claim that there is no other.
//
// Failures are plain errors: github_expired ("Reconnect GitHub") when the
// caller holds no usable GitHub access, not_found or forbidden when that access
// cannot see the repository, and engine when GitHub did not answer. An engine
// that is not a [FactoryPipelinePRLister] answers unknown_method and does not
// announce [FeatureFactoryPipelinePRs].

// FeatureFactoryPipelinePRs is announced in hello when the engine serves
// factory.pipeline_prs.
const FeatureFactoryPipelinePRs = "factory.pipeline_prs"

// factoryPipelinePRsBudget covers the route's own sixty seconds and a margin.
const factoryPipelinePRsBudget = 75 * time.Second

// FactoryPipelinePRsParams names the repository whose pull requests are listed.
type FactoryPipelinePRsParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
}

// FactoryPipelineOpenPR is one open pull request that changes the pipeline
// file. HeadSHA is the 40-hex commit its branch is at.
type FactoryPipelineOpenPR struct {
	Number        int       `json:"number"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	Branch        string    `json:"branch"`
	HeadSHA       string    `json:"head_sha"`
	Author        string    `json:"author"`
	Draft         bool      `json:"draft"`
	FactoryBranch bool      `json:"factory_branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// FactoryPipelinePRs is the answer. PRs is always a list, empty for none, in
// GitHub's order (most recently updated first). CheckedAt is when the factory
// read GitHub.
type FactoryPipelinePRs struct {
	Factory   string                  `json:"factory"`
	Repo      string                  `json:"repo"`
	PRs       []FactoryPipelineOpenPR `json:"prs"`
	Complete  bool                    `json:"complete"`
	CheckedAt time.Time               `json:"checked_at"`
}

// FactoryPipelinePRLister is the engine's half of factory.pipeline_prs.
type FactoryPipelinePRLister interface {
	FactoryPipelinePRs(ctx context.Context, p FactoryPipelinePRsParams) (FactoryPipelinePRs, error)
}

func withFactoryPipelinePRsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPipelinePRLister); ok {
		hello = withFeature(hello, FeatureFactoryPipelinePRs)
	}
	return hello
}

func init() {
	methods["factory.pipeline_prs"] = shaped[FactoryPipelinePRsParams, FactoryPipelinePRs](listPipelinePRs)
}

func listPipelinePRs(e Engine, raw json.RawMessage) (*FactoryPipelinePRs, error) {
	const name = "factory.pipeline_prs"
	l, ok := e.(FactoryPipelinePRLister)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	if !json.Valid(raw) {
		return nil, Errorf(CodeBadParams, "%s: the params are not one JSON value", name)
	}
	var p FactoryPipelinePRsParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	var err error
	if p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), factoryPipelinePRsBudget)
	defer cancel()
	out, err := l.FactoryPipelinePRs(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
