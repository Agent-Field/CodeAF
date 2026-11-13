package appwire

import (
	"context"
	"encoding/json"
	"strings"
)

// ── WHICH BRIEF REVISION THE PERSON LAST SAW ────────────────────────────────
//
// factory.job_brief_seen records, for the CALLER ONLY, the Brief revision they
// last saw on one job (POST /v1/jobs/{id}/brief_seen). The job read answers it
// back as `brief_seen` to that person and to nobody else, so the page can mark
// what changed since. Who the person is comes from the factory's own
// authentication; no field here can name anyone.
//
// IT TAKES NO `expect`. A seen mark is not a decision about the job: it neither
// needs the page to be current nor changes what anyone else reads. The factory
// keeps the job's version as it was, so another person's open page is not
// made stale by it. A revision lower than the one already kept is kept as the
// higher one, never moved back.

// FeatureFactoryJobBriefSeen is announced in hello when the engine serves
// factory.job_brief_seen and the job read answers the caller's `brief_seen`.
const FeatureFactoryJobBriefSeen = "factory.job_brief_seen"

// JobActBriefSeen is the action a [FactoryJobActed] of factory.job_brief_seen
// names.
const JobActBriefSeen = "brief_seen"

// FactoryJobBriefSeenParams names the job and the Brief revision the person
// saw, a whole number from 1.
type FactoryJobBriefSeenParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
	Rev     int    `json:"rev"`
}

// FactoryJobBriefSeer is the engine's half of factory.job_brief_seen. An engine
// without it refuses the method as unknown_method and does not announce the
// feature. Its answer's Current carries the caller's brief_seen as it is now.
type FactoryJobBriefSeer interface {
	FactoryJobBriefSeen(ctx context.Context, p FactoryJobBriefSeenParams) (FactoryJobActed, error)
}

func withFactoryJobBriefSeenFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobBriefSeer); ok {
		hello = withFeature(hello, FeatureFactoryJobBriefSeen)
	}
	return hello
}

func init() {
	methods["factory.job_brief_seen"] = shaped[FactoryJobBriefSeenParams, FactoryJobActed](markBriefSeen)
}

func markBriefSeen(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	s, ok := e.(FactoryJobBriefSeer)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.job_brief_seen")
	}
	var p FactoryJobBriefSeenParams
	if err := decodeStrict("factory.job_brief_seen", raw, &p); err != nil {
		return nil, err
	}
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	if p.Job < 1 {
		return nil, Errorf(CodeBadParams, "job must be a whole number from 1")
	}
	if p.Rev < 1 {
		return nil, Errorf(CodeBadParams, "rev must be the Brief revision the person saw, a whole number from 1")
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryJobBriefSeen(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
