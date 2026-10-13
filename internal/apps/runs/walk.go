package runs

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func viewSteps(steps []*spec.Step) []*stepBody {
	out := make([]*stepBody, len(steps))
	for i, step := range steps {
		out[i] = viewStep(step)
	}
	return out
}

func viewStep(step *spec.Step) *stepBody {
	if step == nil {
		return nil
	}
	body := &stepBody{
		ID: step.ID, Title: step.Title, If: step.If, OnError: step.OnError,
		Agent: step.Agent, Prompt: step.Prompt, Output: step.Output,
		Tool: step.Tool, As: step.As, Args: step.Args,
		OnTimeout: step.OnTimeout, Shell: step.Shell, Workspace: step.Workspace,
		RecordsQuery: viewRec(step.RecordsQuery), RecordsCreate: viewRec(step.RecordsCreate),
		RecordsUpdate: viewRec(step.RecordsUpdate), RecordsDelete: viewRec(step.RecordsDelete),
		Steps: viewSteps(step.Steps),
	}
	if step.Retry != nil {
		body.RetryMax = step.Retry.Max
		body.RetryBackoff = step.Retry.Backoff
	}
	if step.Wait != nil {
		body.Wait = &waitBody{For: step.Wait.For, Until: step.Wait.Until, Thread: step.Wait.Thread, Timeout: step.Wait.Timeout}
	}
	if step.Loop != nil {
		body.Loop = &loopBody{Over: step.Loop.Over, As: step.Loop.As, Until: step.Loop.Until, Max: step.Loop.Max}
	}
	return body
}

func viewRec(rec *spec.RecordsStep) *recBody {
	if rec == nil {
		return nil
	}
	return &recBody{Type: rec.Type, Query: rec.Query, ID: rec.ID, IDs: rec.IDs, Set: rec.Set, Limit: rec.Limit}
}

func applyGate(step *spec.Step, app *spec.App) stepView {
	body := viewStep(step)
	view := stepView{step: body}
	switch {
	case step.Gate != nil:
		view.gate = &gateBody{Approval: step.Gate.Approval, Who: step.Gate.Who, Title: step.Gate.Title}
	case mustGate(app, step.Tool):
		view.gate = &gateBody{Approval: "send", Who: "$me", Title: approvalTitle(step.Tool)}
	}
	return view
}

func mustGate(app *spec.App, tool string) bool {
	if tool == "" {
		return false
	}
	if outboundVerb(tool) {
		return true
	}
	name, verb, ok := strings.Cut(tool, ".")
	if !ok || app == nil {
		return false
	}
	for _, approved := range app.Capabilities.Connectors[name].Approve {
		if approved == verb {
			return true
		}
	}
	return false
}

func outboundVerb(tool string) bool {
	_, verb, ok := strings.Cut(tool, ".")
	if !ok {
		return false
	}
	switch {
	case verb == "send" || strings.HasSuffix(verb, ".send"):
		return true
	case verb == "post" || strings.HasSuffix(verb, ".post"):
		return true
	case verb == "pay" || strings.HasSuffix(verb, ".pay"):
		return true
	default:
		return false
	}
}

func approvalTitle(tool string) string {
	switch {
	case strings.Contains(tool, "send"):
		return "Approval needed: send email"
	case strings.Contains(tool, "post"):
		return "Approval needed: post"
	case strings.Contains(tool, "pay"):
		return "Approval needed: pay"
	default:
		return "Approval needed: " + tool
	}
}

// nextPiece returns the next thing drive should do. prefix is the path of the
// enclosing loop round; top-level steps use "".
func nextPiece(job *job, steps []*spec.Step, prefix string, app *spec.App) piece {
	for _, step := range steps {
		if job.Jump != "" && job.JumpPrefix == prefix && step.ID != job.Jump {
			continue
		}
		if job.Jump != "" && job.JumpPrefix == prefix && step.ID == job.Jump {
			job.Jump = ""
		}
		path := prefix + "/" + step.ID
		if _, ok := job.Done[path]; ok {
			continue
		}
		if step.Loop != nil {
			found := nextLoop(job, step, path, app)
			if found.kind == "continue" {
				continue
			}
			return found
		}
		if len(step.Steps) > 0 {
			inner := nextPiece(job, step.Steps, path, app)
			if inner.kind == "done" {
				job.Done[path] = stepRec{Status: bridge.RunDone, Title: step.Title}
				continue
			}
			return inner
		}
		return piece{kind: "step", path: path, step: ptrView(applyGate(step, app))}
	}
	if job.Jump != "" && job.JumpPrefix == prefix {
		return piece{kind: "broken-jump"}
	}
	return piece{kind: "done"}
}

func ptrView(view stepView) *stepView { return &view }

func nextLoop(job *job, step *spec.Step, path string, app *spec.App) piece {
	lp, ok := job.Loops[path]
	if !ok {
		return piece{kind: "begin", path: path, step: ptrView(applyGate(step, app))}
	}
	if step.Loop.Until != "" {
		if truthy(mustEval(step.Loop.Until, job.Env)) {
			job.Done[path] = stepRec{Status: bridge.RunDone, Title: step.Title}
			return piece{kind: "continue"}
		}
		if lp.Index >= step.Loop.Max {
			return piece{kind: "needs", path: path, step: ptrView(applyGate(step, app))}
		}
	} else {
		if lp.Index >= len(lp.Items) {
			job.Done[path] = stepRec{Status: bridge.RunDone, Title: step.Title}
			return piece{kind: "continue"}
		}
		if lp.Index >= step.Loop.Max {
			return piece{kind: "needs", path: path, step: ptrView(applyGate(step, app))}
		}
		if lp.As != "" {
			job.Env[lp.As] = lp.Items[lp.Index]
		}
	}
	round := path + "#" + strconv.Itoa(lp.Index)
	inner := nextPiece(job, step.Steps, round, app)
	if inner.kind == "done" {
		lp.Index++
		job.Loops[path] = lp
		return nextLoop(job, step, path, app)
	}
	return inner
}

func mustEval(expr string, env map[string]any) any {
	value, err := eval(expr, env)
	if err != nil {
		return false
	}
	return value
}
