package raftdirectory

import (
	"bytes"
	"context"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// ClusterAPI serializes current-time commit, fresh ReadIndex and every selected
// read. Caller cancellation cannot interrupt an admitted durability transaction.
// The restricted effects witness advances time; it cannot authorize the caller.
func (g *Native) ClusterAPI(ctx context.Context, request cs.ClusterRequest, clock cs.Command) (cs.ClusterResponse, error) {
	ctx = context.WithoutCancel(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	var out cs.ClusterResponse
	actor, err := cs.VerifyClusterRequest(request, clock.Now)
	if err != nil || request.Cluster != g.manifest.Config.Cluster || !bytes.Equal(request.IdentityKey, g.manifest.Config.SigningKey) || clock.Cluster != request.Cluster || clock.Change.DeviceSeen == nil || clock.Change.DeviceSeen.Device != actor {
		return out, directory.ErrBadRequest
	}
	result, err := g.submit(ctx, clock)
	if err != nil {
		return out, err
	}
	if err = directory.ResultError(result.Code); err != nil {
		return out, err
	}
	i, app, err := g.barrier(ctx)
	if err != nil {
		return out, err
	}
	if _, err = cs.VerifyClusterRequest(request, app.LogicalTime); err != nil {
		return out, directory.ErrBadRequest
	}
	if request.Action == "submit" {
		result, err = g.submit(ctx, *request.Command)
		if err != nil {
			return out, err
		}
		out.Result = &result
		return out, nil
	}
	if request.Action == "cell" {
		view, grant, err := AuthorizedWorkerRead(ctx, g.stores[i], request.Cluster, actor, request.Cell, request.Incarnation)
		if err != nil {
			return out, err
		}
		out.View, out.Grant = &view, grant
		return out, nil
	}
	assignment, view, err := AuthorizedWorkerAssignment(ctx, g.stores[i], request.Cluster, actor, request.Incarnation, *request.Assignment)
	if err != nil {
		return out, err
	}
	out.Assignment, out.View = &assignment, &view
	return out, nil
}
