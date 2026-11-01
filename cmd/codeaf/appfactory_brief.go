package main

// ── codeaf app: A JOB'S BRIEF ON AN ATTACHED FACTORY ────────────────────────
//
// The engine's half of appwire.FactoryJobBriefer: one fixed read route,
// GET /v1/jobs/{id}/brief, spelled here and never taken from the app. The
// factory computes the brief's facts and keeps its versions; this side only
// carries the answer, with the factory named by the app's id for it.

import (
	"context"
	"errors"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryJobBriefer = (*appBackend)(nil)

// endpointNotFound is the sentence a factory answers for a route it does not
// serve, which is how a factory older than the brief answers its read.
const endpointNotFound = "factory endpoint not found"

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

// routeMissing says whether err is a factory's not found for the route
// itself rather than for the job.
func routeMissing(err error) bool {
	var e *appwire.Error
	return errors.As(err, &e) && e.Code == appwire.CodeNotFound && e.Message == endpointNotFound
}
