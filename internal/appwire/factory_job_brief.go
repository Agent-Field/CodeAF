package appwire

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── factory.job_brief: the job's brief, on the job's own page ──────────────
//
// The brief is the job's current state in one screen (internal/factory's
// brief.go). This method reads the factory's GET /v1/jobs/{id}/brief and
// answers it as it came: the newest version (a stub made from engine data
// alone before the job has one), the status the page shows, and the keys
// that changed since the rev the person last saw.
//
// THE FACTS ARE THE ENGINE'S. Each fact chip is computed by the factory from
// the steps' structured results, never by a model and never by this wire, so
// the page never shows a number a step did not report.
//
// SEEN IS THE CALLER'S. `since` is the brief rev the person last saw, where
// the app has it; without it, changed_since_seen is what the newest version
// changed from the one before.
//
// A FACTORY OLDER THAN THE ROUTE answers it not found; that is
// [CodeConflict] with the factory's too-old sentence, and the page keeps its
// brief hidden.

// FeatureFactoryJobBrief is announced in hello when the engine serves
// factory.job_brief.
const FeatureFactoryJobBrief = "factory.job_brief"

// FactoryJobBriefParams names one job of one factory, and optionally the
// brief rev the person last saw.
type FactoryJobBriefParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
	Since   int    `json:"since,omitempty"`
}

// FactoryJobBrief is a job's brief as its factory answered it. Status is
// ready, generating, failed or stub; Failure and FailedAfter are set only
// while it is failed.
type FactoryJobBrief struct {
	Factory          string               `json:"factory"`
	Job              int                  `json:"job"`
	Status           string               `json:"status" enum:"ready,generating,failed,stub"`
	Failure          string               `json:"failure,omitempty"`
	FailedAfter      string               `json:"failed_after,omitempty"`
	Since            int                  `json:"since,omitempty"`
	Brief            factory.BriefVersion `json:"brief"`
	ChangedSinceSeen []string             `json:"changed_since_seen"`
}

// FactoryJobBriefer is the engine's half of factory.job_brief.
type FactoryJobBriefer interface {
	FactoryJobBrief(ctx context.Context, p FactoryJobBriefParams) (FactoryJobBrief, error)
}

func withFactoryJobBriefFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobBriefer); ok {
		hello = withFeature(hello, FeatureFactoryJobBrief)
	}
	return hello
}

func init() {
	methods["factory.job_brief"] = shaped[FactoryJobBriefParams, FactoryJobBrief](jobBrief)
}

func jobBrief(e Engine, raw json.RawMessage) (*FactoryJobBrief, error) {
	const name = "factory.job_brief"
	b, ok := e.(FactoryJobBriefer)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryJobBriefParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	id, err := factoryID(p.Factory)
	if err != nil {
		return nil, err
	}
	p.Factory = id
	if p.Job <= 0 {
		return nil, Errorf(CodeBadParams, "job must be a job's number")
	}
	if p.Since < 0 {
		return nil, Errorf(CodeBadParams, "since must be a brief's rev")
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := b.FactoryJobBrief(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
