package runs

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/connect"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func withPermit(ctx context.Context) context.Context { return connect.WithPermit(ctx) }

// ConnectorHook is the durable wait seam implemented by the runs engine.
type ConnectorHook interface {
	ConnectorChanged(context.Context, bridge.ConnectorWait, bool) error
}

var _ ConnectorHook = (*Service)(nil)

func (e *engine) connectorCall(ctx context.Context, scope svc.Scope, tool string, args map[string]any) (any, error) {
	if e.all.Connectors != nil {
		if _, legacy := e.all.Connectors.(*Connectors); !legacy {
			return e.all.Connectors.Call(ctx, scope, tool, args)
		}
	}
	// The wave-1 constructor remains a fake for existing harnesses; all canned replies live in the door.
	scope.Data = svc.DataSample
	fake := connect.New(svc.Config{Root: e.root}, e.all)
	defer fake.Close()
	out, err := fake.Call(ctx, scope, tool, args)
	if err == nil {
		e.mu.Lock()
		e.calls = append(e.calls, recorded{App: scope.App, Tool: tool, Args: resolveMap(args, map[string]any{})})
		if reply, ok := e.replies[tool]; ok {
			out = reply
		}
		e.mu.Unlock()
	}
	return out, err
}

func (e *engine) connectorStep(ctx context.Context, job *job, bundle *spec.Bundle, found piece) (bool, error) {
	step := found.step.step
	if step.Tool == "" || step.Shell != "" || step.Workspace == "repo" {
		return e.execStep(ctx, job, bundle, found)
	}
	if e.all.Connectors != nil {
		job.State.Fake = job.Data != svc.DataReal
	}
	if step.If != "" {
		value, err := eval(step.If, job.Env)
		if err != nil {
			return e.onStepError(ctx, job, found, err)
		}
		if !truthy(value) {
			job.Done[found.path] = stepRec{Status: bridge.RunDone, Title: step.Title}
			return false, nil
		}
	}
	if e.all.Connectors != nil && e.all.Connectors.Outbound(step.Tool) {
		// An unrelated explicit gate cannot stand in for the send approval.
		view := *found.step
		found.step = &view
		if view.gate == nil || view.gate.Approval != "send" {
			view.gate = &gateBody{Approval: "send", Who: "$me", Title: approvalTitle(step.Tool)}
		}
	}
	if blocked, err := e.connectorGate(ctx, job, bundle, found); blocked || err != nil {
		return blocked, err
	}
	callCtx := connect.WithInvocation(ctx, job.State.ID, strings.TrimPrefix(found.path, "/"))
	if found.step.gate != nil && found.step.gate.Approval == "send" && job.Answer == "yes" {
		callCtx = withPermit(callCtx)
	}
	out, err := e.execTool(callCtx, job, bundle, found)
	if err != nil {
		var wait *connect.WaitError
		if errors.As(err, &wait) {
			e.connectorWaiting(job, found)
			return true, nil
		}
		return e.onStepError(ctx, job, found, err)
	}
	job.Done[found.path] = stepRec{Status: bridge.RunDone, Title: step.Title, Output: out.output, Approval: job.ApprovalID}
	recordOutput(job, step.ID, out.output)
	e.clearWait(job)
	job.State.Status = bridge.RunRunning
	return false, nil
}
func (e *engine) connectorGate(ctx context.Context, job *job, bundle *spec.Bundle, found piece) (bool, error) {
	if found.step.gate == nil {
		return false, nil
	}
	switch e.gatePhase(job, found.path) {
	case "need":
		if err := e.openGate(ctx, job, bundle, found); err != nil {
			return e.onStepError(ctx, job, found, err)
		}
		return true, nil
	case "wait":
		job.State.Status = bridge.RunWaiting
		return true, nil
	case "no":
		job.Done[found.path] = stepRec{Status: bridge.RunStopped, Title: found.step.step.Title, Approval: job.ApprovalID, Error: "approval declined"}
		e.stop(job, "approval declined")
		return true, nil
	}
	return false, nil
}
func (e *engine) connectorWaiting(job *job, found piece) {
	job.State.Status = bridge.RunNeedsYou
	job.State.Error = ""
	job.State.EndedAt = ""
	job.WaitPath = found.path
	job.WaitKind = "connector"
	job.Done[found.path] = stepRec{Status: bridge.RunNeedsYou, Title: found.step.step.Title, Approval: job.ApprovalID}
}

// ConnectorChanged only touches the run named by the persisted connection wait.
func (s *Service) ConnectorChanged(ctx context.Context, wait bridge.ConnectorWait, stop bool) error {
	e := s.eng
	if !e.begin(wait.RunID) {
		return bridge.Errorf(bridge.CodeConflict, "run is finishing its step")
	}
	err := e.changeConnector(ctx, wait, stop)
	e.end(wait.RunID)
	if err == nil && !stop {
		e.drive(ctx, wait.App, wait.RunID)
		job, loadErr := e.loadJob(wait.App, wait.RunID)
		if loadErr != nil {
			return loadErr
		}
		if job != nil && job.WaitKind == "connector" && strings.TrimPrefix(job.WaitPath, "/") == wait.Step {
			return bridge.Errorf(bridge.CodeConflict, "connection is still waiting")
		}
	}
	return err
}
func (e *engine) changeConnector(ctx context.Context, wait bridge.ConnectorWait, stop bool) error {
	job, err := e.loadJob(wait.App, wait.RunID)
	if err != nil {
		return err
	}
	if job == nil || terminal(job.State.Status) && job.State.Status != bridge.RunNeedsYou {
		return nil
	}
	if job.WaitKind != "connector" || strings.TrimPrefix(job.WaitPath, "/") != wait.Step {
		return bridge.Errorf(bridge.CodeConflict, "run has not saved its connection wait")
	}
	bundle, err := e.bundle(ctx, wait.App)
	if err != nil {
		return err
	}
	if stop {
		message := "stopped · " + serviceName(wait.Service) + " expired"
		rec := job.Done[job.WaitPath]
		rec.Status, rec.Error = bridge.RunStopped, message
		job.Done[job.WaitPath] = rec
		e.stop(job, message)
	} else {
		delete(job.Done, job.WaitPath)
		if job.ApprovalID != "" && job.Answer == "yes" {
			job.WaitKind = "gate"
		} else {
			e.clearWait(job)
		}
		job.State.Status = bridge.RunRunning
		job.State.Error = ""
		job.State.EndedAt = ""
	}
	return e.persist(ctx, job, bundle)
}
func serviceName(service string) string {
	names := map[string]string{"gmail": "Gmail", "calendar": "Calendar", "slack": "Slack", "github": "GitHub", "stripe": "Stripe", "airtable": "Airtable", "hubspot": "HubSpot"}
	if name := names[service]; name != "" {
		return name
	}
	return strings.TrimPrefix(service, "mcp:")
}
