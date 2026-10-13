package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/agent"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// wire uses the app server's constructors for every service on the agent's
// belt. Only automatic demo installation is suppressed in the private store.
func wire(ctx context.Context, root string, model svc.Model) *svc.Services {
	cfg := svc.Config{Root: root, Ctx: ctx}
	all := &svc.Services{Model: model}
	all.Hub = realtime.New(cfg, all)
	all.Store = store.NewWithOptions(cfg, all, store.Options{WithoutDemo: true})
	all.Caps = caps.New(cfg, all)
	all.Records = records.New(cfg, all)
	all.Approvals = runs.NewApprovals(cfg, all)
	all.Connectors = runs.NewConnectors(cfg, all)
	all.Runs = runs.New(cfg, all)
	all.Agent = agent.New(cfg, all)
	return all
}

func runJob(ctx context.Context, j job, timeout time.Duration) result {
	return runWithModel(ctx, j, timeout, builder.NewModel(svc.Config{}, nil))
}

func runWithModel(ctx context.Context, j job, timeout time.Duration, model svc.Model) result {
	r := result{Model: j.model, App: j.preset, Task: j.task.ID, Text: j.task.Text, Check: j.task.Operation}
	files, bundle, err := modelFiles(j.dir, j.model)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	seed, err := makeSeed(ctx, bundle, j, time.Now().UTC())
	if err != nil {
		r.Error = err.Error()
		return r
	}
	typ, expected, err := expectedFields(bundle, j.task, seed)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Expected = expected
	root, err := os.MkdirTemp("", "apps-agentbench-")
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer os.RemoveAll(root)
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	measured := &observedModel{Model: model, errors: map[string]bool{}}
	all := wire(turnCtx, filepath.Join(root, "apps"), measured)
	defer all.Runs.(*runs.Service).Close()
	scope, err := install(ctx, all, bundle, files, seed)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Before, err = readSnapshot(ctx, all, scope, bundle.App)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	started := time.Now()
	err = ask(turnCtx, all, scope, j.task.Text, timeout, cancel, &r)
	r.Seconds = time.Since(started).Seconds()
	cost, toolErrors, final := measured.measurements()
	r.Cost = cost
	if r.Error != "" {
		toolErrors = append(toolErrors, r.Error)
	}
	if !final {
		toolErrors = append(toolErrors, "no final reply text")
	}
	if err != nil {
		toolErrors = append(toolErrors, err.Error())
	}
	slices.Sort(toolErrors)
	r.Error = strings.Join(slices.Compact(toolErrors), "; ")
	r.OK = r.Error == ""
	// The turn's context may have expired. Check its persisted result with a
	// separate bounded context so failures still receive a data measurement.
	checkCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	r.After, err = readSnapshot(checkCtx, all, scope, bundle.App)
	if err != nil {
		r.OK = false
		r.Error = strings.Trim(r.Error+"; "+err.Error(), "; ")
		return r
	}
	r.DataChanged, r.DataOK = checkData(j.task, typ, expected, r.Before, r.After, r.Events)
	return r
}

func install(ctx context.Context, all *svc.Services, b *spec.Bundle, files spec.Files, seed map[string][]bridge.Record) (svc.Scope, error) {
	caller := svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}
	app, err := all.Store.Create(ctx, caller, b.App.Name, b.App.Description)
	if err != nil {
		return svc.Scope{}, err
	}
	scope := svc.Scope{App: app.ID, Caller: caller, Data: svc.DataSample}
	if err := all.Store.WriteFiles(ctx, app.ID, files); err != nil {
		return scope, err
	}
	return scope, all.Records.Seed(ctx, app.ID, seed)
}

// ask subscribes before Send: short replies cannot finish before the listener
// exists. Cancellation waits briefly for the real turn to publish its terminal
// event before the private store is removed.
func ask(ctx context.Context, all *svc.Services, scope svc.Scope, text string, timeout time.Duration, cancel context.CancelFunc, r *result) error {
	events, unsubscribe := all.Hub.Subscribe("app/" + scope.App + "/agent/*")
	defer unsubscribe()
	sent, err := all.Agent.Send(ctx, scope, bridge.AgentSendParams{Text: text})
	if err != nil {
		return err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var stopped error
	ctxDone := ctx.Done()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return fmt.Errorf("agent event stream closed without a final event")
			}
			e, ok := ev.Data.(bridge.AgentEvent)
			if !ok {
				if err, ok := ev.Data.(error); ok {
					return err
				}
				continue
			}
			if e.Turn != sent.Turn {
				continue
			}
			if err := recordEvent(r, e); err != nil {
				return errors.Join(stopped, err)
			}
			if e.Kind == bridge.AgentDone {
				return stopped
			}
		case <-ctxDone:
			stopped = ctx.Err()
			ctxDone = nil
			cancel()
			timer.Reset(5 * time.Second)
		case <-timer.C:
			if stopped != nil {
				return stopped
			}
			stopped = context.DeadlineExceeded
			ctxDone = nil
			cancel()
			timer.Reset(5 * time.Second)
		}
	}
}

func recordEvent(r *result, e bridge.AgentEvent) error {
	r.Events = append(r.Events, e)
	switch e.Kind {
	case bridge.AgentDelta, bridge.AgentShapeThis:
		if r.Final != "" {
			r.Final += "\n"
		}
		r.Final += e.Text
		if e.Text == "I stopped after too many steps." {
			r.Error = "agent step limit reached"
		}
	case bridge.AgentTool:
		r.Tools++
	case bridge.AgentError:
		return fmt.Errorf("agent: %s", e.Text)
	}
	return nil
}
