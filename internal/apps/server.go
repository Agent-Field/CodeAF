// Package apps wires the Apps services and mounts the bridge on the app
// server. This file is the composition root; lanes replace the stub file in
// their own package and never edit it.
package apps

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/agent"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/grants"
	"github.com/Agent-Field/codeaf/internal/apps/imports"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Mux is the part of the app server Mount needs (appapi.Server has it).
type Mux interface {
	Handle(pattern string, handler http.Handler)
}

// Wire builds every service. Each constructor gets the shared *Services,
// filled here before any call arrives.
func Wire(ctx context.Context, home string) *svc.Services {
	cfg := svc.Config{Root: filepath.Join(home, "apps"), Ctx: ctx}
	all := &svc.Services{}
	all.Connect.Grants = grants.New(cfg, all)
	all.Connect.Imports = imports.New(cfg, all)
	all.Hub = realtime.New(cfg, all)
	all.Caps = caps.New(cfg, all)
	all.Store = store.New(cfg, all)
	all.Records = records.New(cfg, all)
	all.Sample = sample.New(cfg, all)
	all.Approvals = runs.NewApprovals(cfg, all)
	all.Connectors = runs.NewConnectors(cfg, all)
	all.Runs = runs.New(cfg, all)
	all.Model = builder.NewModel(cfg, all)
	all.Check = check.New(cfg, all)
	all.Builder = builder.New(cfg, all)
	all.Agent = agent.New(cfg, all)
	return all
}

// Mount serves the bridge at bridge.Path on mux.
func Mount(ctx context.Context, mux Mux, all *svc.Services) {
	mux.Handle(bridge.Path, gateway.Handler(ctx, all))
}
