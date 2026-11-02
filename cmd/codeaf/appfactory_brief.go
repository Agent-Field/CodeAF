package main

// ── codeaf app: A JOB'S BRIEF ON AN ATTACHED FACTORY ────────────────────────
//
// The engine's half of appwire.FactoryJobBriefer: one fixed read route,
// GET /v1/jobs/{id}/brief, spelled here and never taken from the app. The
// factory computes the brief's facts and keeps its versions; this side only
// carries the answer, with the factory named by the app's id for it.

import (
	"context"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryJobBriefer = (*appBackend)(nil)

func (b *appBackend) FactoryJobBrief(ctx context.Context, p appwire.FactoryJobBriefParams) (appwire.FactoryJobBrief, error) {
	path := jobPath(p.Job, "brief")
	if p.Since > 0 {
		path += "?since=" + strconv.Itoa(p.Since)
	}
	var got api.JobBrief
	if err := b.factoryHub().read(ctx, p.Factory, path, appFactoryBody, &got); err != nil {
		if routeMissing(err) {
			return appwire.FactoryJobBrief{}, factoryTooOld()
		}
		return appwire.FactoryJobBrief{}, err
	}
	changed := got.ChangedSinceSeen
	if changed == nil {
		changed = []string{}
	}
	return appwire.FactoryJobBrief{
		Factory: p.Factory, Job: p.Job, Status: got.Status, Failure: got.Failure, FailedAfter: got.FailedAfter,
		Since: got.Since, Brief: got.Brief, ChangedSinceSeen: changed,
	}, nil
}

var _ appwire.FactoryJobBriefSpotter = (*appBackend)(nil)

// FactoryJobBriefSpot reads one spot of a job's brief, GET
// /v1/jobs/{id}/brief/spots/{spot}; the spot was checked to be a word.
func (b *appBackend) FactoryJobBriefSpot(ctx context.Context, p appwire.FactoryJobBriefSpotParams) (appwire.FactoryJobBriefSpot, error) {
	var got api.JobBriefSpot
	if err := b.factoryHub().read(ctx, p.Factory, jobPath(p.Job, "brief/spots/"+p.Spot), appFactoryBody, &got); err != nil {
		if routeMissing(err) {
			return appwire.FactoryJobBriefSpot{}, factoryTooOld()
		}
		return appwire.FactoryJobBriefSpot{}, err
	}
	out := appwire.FactoryJobBriefSpot{
		Factory: p.Factory, Job: p.Job, Rev: got.Rev, Spot: got.Spot, File: got.File, Symbol: got.Symbol, Why: got.Why,
		Lines: got.Lines, Hunks: got.Hunks, Annotations: got.Annotations, Truncated: got.Truncated,
	}
	if out.Hunks == nil {
		out.Hunks = []factory.SpotHunk{}
	}
	if out.Annotations == nil {
		out.Annotations = []factory.SpotNote{}
	}
	return out, nil
}
