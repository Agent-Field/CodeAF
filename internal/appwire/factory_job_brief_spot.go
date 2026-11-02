package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── factory.job_brief_spot: one place to look, with its hunks ─────────────
//
// A where-to-look spot of the brief opens a peek on that spot's hunks only:
// the hunks of its file that name its symbol (all of the file's when none
// does), with the writer's notes as rows between them. The factory reads them
// from the job's own change (GET /v1/jobs/{id}/brief/spots/{spot}); this wire
// carries the answer as it came. A spot the newest brief does not have is
// not found, so a peek opened on an older version's spot asks the page to
// read the brief again.

// FeatureFactoryJobBriefSpot is announced in hello when the engine serves
// factory.job_brief_spot.
const FeatureFactoryJobBriefSpot = "factory.job_brief_spot"

// FactoryJobBriefSpotParams names one spot (a brief_spot's id) of one job.
type FactoryJobBriefSpotParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
	Spot    string `json:"spot"`
}

// FactoryJobBriefSpot is the spot with its hunks. Rev is the brief version
// it is a spot of; Truncated says the job's patch was cut before its end.
type FactoryJobBriefSpot struct {
	Factory     string             `json:"factory"`
	Job         int                `json:"job"`
	Rev         int                `json:"rev"`
	Spot        string             `json:"spot"`
	File        string             `json:"file"`
	Symbol      string             `json:"symbol,omitempty"`
	Why         string             `json:"why"`
	Lines       int                `json:"lines"`
	Hunks       []factory.SpotHunk `json:"hunks"`
	Annotations []factory.SpotNote `json:"annotations"`
	Truncated   bool               `json:"truncated,omitempty"`
}

// FactoryJobBriefSpotter is the engine's half of factory.job_brief_spot.
type FactoryJobBriefSpotter interface {
	FactoryJobBriefSpot(ctx context.Context, p FactoryJobBriefSpotParams) (FactoryJobBriefSpot, error)
}

func withFactoryJobBriefSpotFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobBriefSpotter); ok {
		hello = withFeature(hello, FeatureFactoryJobBriefSpot)
	}
	return hello
}

func init() {
	methods["factory.job_brief_spot"] = shaped[FactoryJobBriefSpotParams, FactoryJobBriefSpot](jobBriefSpot)
}

// briefSpotMost bounds a spot's id; the engine numbers them "1" to "3".
const briefSpotMost = 16

func jobBriefSpot(e Engine, raw json.RawMessage) (*FactoryJobBriefSpot, error) {
	const name = "factory.job_brief_spot"
	s, ok := e.(FactoryJobBriefSpotter)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryJobBriefSpotParams
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
	// The spot is a path segment on the factory's route, so it is a short
	// word of letters and digits and nothing that could name another path.
	if p.Spot == "" || len(p.Spot) > briefSpotMost || strings.Trim(p.Spot, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return nil, Errorf(CodeBadParams, "spot must be a spot's id from the brief")
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryJobBriefSpot(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
