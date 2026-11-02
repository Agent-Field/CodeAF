package appwire

import (
	"context"
	"encoding/json"
	"strings"
)

// ── FOLLOWING A JOB ─────────────────────────────────────────────────────────
//
// factory.job_follow switches the CALLER's following of one job on or off
// (POST /v1/jobs/{id}/follow). The job read answers it back as `following` to
// that person only; nobody learns who else follows a job. Who the person is
// comes from the factory's own authentication; no field here can name anyone.
//
// IT TAKES NO `expect`, for the reason factory.job_brief_seen takes none: it is
// the person's own switch, not a decision about the job, and the factory keeps
// the job's version as it was so nobody else's open page goes stale.
//
// THIS IS NOT follow_pr. The job's `follow_pr` is the factory's own step that
// keeps a published pull request healthy (factory.FollowPRStep); following is
// a person asking to be told. The engine stores the switch and answers it; it
// does not deliver notifications for followed jobs yet (no factory job event
// reaches the app over this wire today).

// FeatureFactoryJobFollow is announced in hello when the engine serves
// factory.job_follow and the job read answers the caller's `following`.
const FeatureFactoryJobFollow = "factory.job_follow"

// JobActFollow is the action a [FactoryJobActed] of factory.job_follow names.
const JobActFollow = "follow"

// FactoryJobFollowParams names the job and whether the caller follows it. On
// is required: a request that forgot it is refused, never read as off.
type FactoryJobFollowParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
	On      *bool  `json:"on"`
}

// FactoryJobFollower is the engine's half of factory.job_follow. An engine
// without it refuses the method as unknown_method and does not announce the
// feature. Its answer's Current carries the caller's following as it is now.
type FactoryJobFollower interface {
	FactoryJobFollow(ctx context.Context, p FactoryJobFollowParams) (FactoryJobActed, error)
}

func withFactoryJobFollowFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobFollower); ok {
		hello = withFeature(hello, FeatureFactoryJobFollow)
	}
	return hello
}

func init() {
	methods["factory.job_follow"] = shaped[FactoryJobFollowParams, FactoryJobActed](followJob)
}

func followJob(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	s, ok := e.(FactoryJobFollower)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.job_follow")
	}
	var p FactoryJobFollowParams
	if err := decodeStrict("factory.job_follow", raw, &p); err != nil {
		return nil, err
	}
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	if p.Job < 1 {
		return nil, Errorf(CodeBadParams, "job must be a whole number from 1")
	}
	if p.On == nil {
		return nil, Errorf(CodeBadParams, "on is required: true to follow the job, false to stop")
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryJobFollow(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
