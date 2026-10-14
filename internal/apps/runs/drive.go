package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (e *engine) start(ctx context.Context, scope svc.Scope, p bridge.StartRunParams, trigger string) (bridge.RunState, error) {
	ctx, done, err := e.work(ctx)
	if err != nil {
		return bridge.RunState{}, err
	}
	defer done()
	if strings.TrimSpace(p.Run) == "" {
		return bridge.RunState{}, bridge.Errorf(bridge.CodeInvalid, "run is required")
	}
	bundle, err := e.bundle(ctx, scope.App)
	if err != nil {
		return bridge.RunState{}, err
	}
	def := bundle.Runs[p.Run]
	if def == nil {
		return bridge.RunState{}, bridge.Errorf(bridge.CodeNotFound, "no run "+p.Run)
	}
	capUSD, err := e.precheck(scope.App, bundle.App, def)
	if err != nil {
		return bridge.RunState{}, err
	}
	if trigger == "" {
		trigger = "manual"
	}
	now := e.clock().UTC()
	params := applyDefaults(def, p.Params)
	job := &job{
		State: bridge.RunState{
			ID: e.newID("run_"), App: scope.App, Run: def.Run, Trigger: trigger,
			Params: params, Status: bridge.RunRunning, Fake: true,
			Cost:      bridge.Cost{Spent: 0, Cap: capUSD},
			StartedAt: now.Format(time.RFC3339),
		},
		Member: scope.Caller.Member, Role: scope.Caller.Role, Agent: scope.Caller.Agent,
		Data: scope.Data, Month: bundle.App.Budget.Month, AtCap: bundle.App.Budget.AtCap,
		Env:      baseEnv(scope, bundle.App.Space, params, now),
		Done:     map[string]stepRec{},
		Loops:    map[string]loopRec{},
		Attempts: map[string]int{},
	}
	if err := e.persist(ctx, job, bundle); err != nil {
		return bridge.RunState{}, bridge.Errorf(bridge.CodeInternal, "could not save the run")
	}
	e.noteStarted(job.State.ID)
	e.drive(ctx, scope.App, job.State.ID)
	loaded, err := e.loadJob(scope.App, job.State.ID)
	if err != nil || loaded == nil {
		return job.State, err
	}
	return loaded.State, nil
}

func applyDefaults(def *spec.Run, in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	for name, param := range def.Params {
		if _, ok := out[name]; !ok && param != nil && param.Default != nil {
			out[name] = param.Default
		}
	}
	return out
}

func baseEnv(scope svc.Scope, space string, params map[string]any, now time.Time) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	if space == "" {
		space = "personal"
	}
	member := scope.Caller.Member
	if member == "" {
		member = svc.Owner
	}
	return map[string]any{
		"me": member, "space": space, "now": now.UTC().Format(time.RFC3339),
		"params": params, "steps": map[string]any{},
	}
}

func (e *engine) bundle(ctx context.Context, app string) (*spec.Bundle, error) {
	if e.all == nil || e.all.Store == nil {
		return nil, bridge.Errorf(bridge.CodeInternal, "no store")
	}
	bundle, err := e.all.Store.Definition(ctx, app)
	if err != nil {
		return nil, err
	}
	if bundle == nil || bundle.App == nil {
		return nil, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return bundle, nil
}

func (e *engine) drive(ctx context.Context, app, id string) {
	ctx, done, err := e.work(ctx)
	if err != nil {
		return
	}
	defer done()
	if !e.begin(id) {
		return
	}
	defer e.end(id)
	for n := 0; n < maxDriveSteps; n++ {
		if ctx.Err() != nil {
			return
		}
		job, err := e.loadJob(app, id)
		if err != nil || job == nil || terminal(job.State.Status) {
			return
		}
		bundle, err := e.bundle(ctx, app)
		if err != nil || bundle == nil || bundle.Runs[job.State.Run] == nil {
			e.fail(job, err)
			_ = e.persist(ctx, job, bundle)
			return
		}
		job.Env["now"] = e.clock().UTC().Format(time.RFC3339)
		found := nextPiece(job, bundle.Runs[job.State.Run].Steps, "", bundle.App)
		switch found.kind {
		case "done":
			e.complete(job)
		case "needs":
			e.needsYou(job, "out of rounds")
		case "broken-jump":
			e.fail(job, bridge.Errorf(bridge.CodeInvalid, "goto target is missing"))
		case "begin":
			if err := e.beginLoop(ctx, job, bundle, found); err != nil {
				e.fail(job, err)
				_ = e.persist(ctx, job, bundle)
				return
			}
			if err := e.persist(ctx, job, bundle); err != nil {
				return
			}
			continue
		case "step":
			blocked, stepErr := e.execStep(ctx, job, bundle, found)
			if errors.Is(stepErr, context.Canceled) {
				return
			}
			if err := e.persist(ctx, job, bundle); err != nil {
				return
			}
			if blocked || terminal(job.State.Status) {
				return
			}
			continue
		default:
			e.fail(job, bridge.Errorf(bridge.CodeInternal, "could not advance"))
		}
		_ = e.persist(ctx, job, bundle)
		return
	}
	job, _ := e.loadJob(app, id)
	if job != nil {
		e.fail(job, bridge.Errorf(bridge.CodeInternal, "too many steps"))
		bundle, _ := e.bundle(ctx, app)
		_ = e.persist(ctx, job, bundle)
	}
}

func (e *engine) beginLoop(ctx context.Context, job *job, bundle *spec.Bundle, found piece) error {
	step := found.step.step
	lp := loopRec{Max: step.Loop.Max, As: step.Loop.As}
	if step.Loop.Over != "" {
		items, err := e.resolveList(ctx, job, bundle, step.Loop.Over)
		if err != nil {
			return err
		}
		lp.Items = items
		if len(lp.Items) == 0 {
			job.Done[found.path] = stepRec{Status: bridge.RunDone, Title: step.Title}
			return nil
		}
	}
	job.Loops[found.path] = lp
	return nil
}

func (e *engine) resolveList(ctx context.Context, job *job, bundle *spec.Bundle, expr string) ([]any, error) {
	value, err := eval(expr, job.Env)
	if err != nil {
		value = lookup(expr, job.Env)
	}
	switch typed := value.(type) {
	case []any:
		return typed, nil
	case []string:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = item
		}
		return out, nil
	case string:
		return e.queryIDs(ctx, job, bundle, expr, typed)
	case nil:
		return nil, nil
	default:
		return []any{typed}, nil
	}
}

func (e *engine) queryIDs(ctx context.Context, job *job, bundle *spec.Bundle, expr, query string) ([]any, error) {
	name := paramName(expr)
	def := bundle.Runs[job.State.Run]
	if def == nil || def.Params[name] == nil || def.Params[name].To == "" {
		return []any{query}, nil
	}
	rows, err := e.queryRows(ctx, job, def.Params[name].To, query, 0)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(rows))
	for i, row := range rows {
		out[i] = row.ID
	}
	return out, nil
}

func paramName(expr string) string {
	expr = strings.TrimPrefix(strings.TrimSpace(expr), "$")
	rest, ok := strings.CutPrefix(expr, "params.")
	if !ok {
		return ""
	}
	if i := strings.IndexByte(rest, '.'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func (e *engine) execStep(ctx context.Context, job *job, bundle *spec.Bundle, found piece) (bool, error) {
	step := found.step.step
	if step.Shell != "" || step.Workspace == "repo" {
		e.fail(job, bridge.Errorf(bridge.CodeInvalid, "shell steps are not available"))
		job.Done[found.path] = stepRec{Status: bridge.RunFailed, Title: step.Title, Error: job.State.Error}
		return true, nil
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
	if found.step.gate != nil {
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
			job.Done[found.path] = stepRec{Status: bridge.RunStopped, Title: step.Title, Approval: job.ApprovalID, Error: "approval declined"}
			e.stop(job, "approval declined")
			return true, nil
		}
	}
	if step.Wait != nil {
		return e.execWait(job, found)
	}
	out, err := e.execAction(ctx, job, bundle, found)
	if err != nil {
		return e.onStepError(ctx, job, found, err)
	}
	job.Done[found.path] = stepRec{Status: bridge.RunDone, Title: step.Title, Output: out.output, AI: out.ai, Approval: job.ApprovalID}
	recordOutput(job, step.ID, out.output)
	e.clearWait(job)
	if job.State.Status == bridge.RunWaiting {
		job.State.Status = bridge.RunRunning
	}
	return false, nil
}

func (e *engine) gatePhase(job *job, path string) string {
	if job.WaitPath != path || job.WaitKind != "gate" {
		return "need"
	}
	switch job.Answer {
	case "yes":
		return "yes"
	case "no":
		return "no"
	default:
		return "wait"
	}
}

func (e *engine) openGate(ctx context.Context, job *job, bundle *spec.Bundle, found piece) error {
	step := found.step.step
	args := map[string]any{}
	if step.Args != nil {
		args = resolveMap(step.Args, job.Env)
	}
	title := found.step.gate.Title
	if title == "" {
		title = approvalTitle(step.Tool)
	}
	approval := bridge.Approval{
		ID: e.newID("appr_"), App: job.State.App, RunID: job.State.ID, Step: step.ID,
		Kind: found.step.gate.Approval, Title: title, Summary: summaryOf(args),
		Tool: step.Tool, Payload: args, Who: e.who(found.step.gate, job.Env),
		State: bridge.ApprovalOpen, CreatedAt: e.clock().UTC().Format(time.RFC3339),
	}
	if err := e.saveApproval(approval); err != nil {
		return err
	}
	e.publishApproval(approval)
	job.WaitPath = found.path
	job.WaitKind = "gate"
	job.ApprovalID = approval.ID
	job.Answer = ""
	job.State.Status = bridge.RunWaiting
	_ = ctx
	_ = bundle
	return nil
}

func (e *engine) who(gate *gateBody, env map[string]any) string {
	if gate == nil || gate.Who == "" {
		return svc.Owner
	}
	text := strings.TrimSpace(asText(resolveValue(gate.Who, env)))
	if text == "" {
		return svc.Owner
	}
	return text
}

func summaryOf(args map[string]any) string {
	if text, ok := args["subject"].(string); ok && text != "" {
		return text
	}
	if text, ok := args["text"].(string); ok {
		return text
	}
	return ""
}

func (e *engine) execWait(job *job, found piece) (bool, error) {
	step := found.step.step
	if job.WaitPath != found.path {
		until, kind, err := e.armWait(step.Wait, job.Env)
		if err != nil {
			return false, err
		}
		job.WaitPath = found.path
		job.WaitKind = kind
		job.WaitUntil = until
		job.State.Status = bridge.RunWaiting
		return true, nil
	}
	if e.clock().Before(job.WaitUntil) {
		job.State.Status = bridge.RunWaiting
		return true, nil
	}
	if job.WaitKind == "event" && step.OnTimeout != "continue" {
		return false, bridge.Errorf(bridge.CodeInvalid, "wait timed out")
	}
	job.Done[found.path] = stepRec{Status: bridge.RunDone, Title: step.Title}
	e.clearWait(job)
	job.State.Status = bridge.RunRunning
	return false, nil
}

func (e *engine) armWait(wait *waitBody, env map[string]any) (time.Time, string, error) {
	if wait.Until != "" {
		text := asText(resolveValue(wait.Until, env))
		when, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return time.Time{}, "", bridge.Errorf(bridge.CodeInvalid, "wait until must be a time")
		}
		return when, "until", nil
	}
	if d, ok := parseDur(wait.For); ok {
		return e.clock().Add(d), "until", nil
	}
	timeout := 24 * time.Hour
	if d, ok := parseDur(wait.Timeout); ok {
		timeout = d
	}
	return e.clock().Add(timeout), "event", nil
}

type outcome struct {
	output any
	ai     bool
}

func (e *engine) execAction(ctx context.Context, job *job, bundle *spec.Bundle, found piece) (outcome, error) {
	step := found.step.step
	switch {
	case step.Agent != "":
		return e.execAgent(ctx, job, bundle, step)
	case step.Tool != "":
		return e.execTool(ctx, job, bundle, found)
	case step.RecordsQuery != nil:
		return e.execQuery(ctx, job, step.RecordsQuery)
	case step.RecordsCreate != nil:
		return e.execWrite(ctx, job, "records.create", step.RecordsCreate, false)
	case step.RecordsUpdate != nil:
		return e.execWrite(ctx, job, "records.update", step.RecordsUpdate, true)
	case step.RecordsDelete != nil:
		return e.execDelete(ctx, job, step.RecordsDelete)
	case found.step.gate != nil:
		return outcome{}, nil
	default:
		return outcome{}, bridge.Errorf(bridge.CodeInvalid, "step "+step.ID+" has no action")
	}
}

func (e *engine) execAgent(ctx context.Context, job *job, bundle *spec.Bundle, step *stepBody) (outcome, error) {
	if !e.room(job, 0) {
		return outcome{}, errCap
	}
	if e.all.Model == nil {
		return outcome{}, bridge.Errorf(bridge.CodeInternal, "model is not configured")
	}
	model := bundle.App.Agent.Model
	if model == "" {
		model = defaultModel
	}
	var schema json.RawMessage
	if step.Output != nil {
		schema, _ = json.Marshal(step.Output)
	}
	reply, err := e.all.Model.Complete(ctx, svc.ModelCall{
		Model: model, System: "Reply with JSON only. The result is labelled AI.",
		Messages: []svc.ModelMessage{{Role: "user", Text: render(step.Prompt, job.Env)}},
		Schema:   schema, App: job.State.App,
	})
	if err != nil {
		return outcome{}, err
	}
	if !e.room(job, reply.Cost) {
		job.State.Cost.Spent += reply.Cost
		return outcome{}, errCap
	}
	job.State.Cost.Spent += reply.Cost
	parsed, err := parseModel(reply.Text, step.Output)
	if err != nil {
		return outcome{}, err
	}
	return outcome{output: parsed, ai: true}, nil
}

var errCap = errors.New("budget")

func parseModel(text string, schema map[string]any) (any, error) {
	text = stripFence(text)
	if schema == nil {
		var value any
		if json.Unmarshal([]byte(text), &value) == nil {
			return value, nil
		}
		return text, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "model reply was not JSON")
	}
	if missing := missingRequired(schema, obj); missing != "" {
		return nil, bridge.Errorf(bridge.CodeInvalid, "model reply missing "+missing)
	}
	return obj, nil
}

func stripFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimPrefix(strings.TrimSpace(text), "json")
	text = strings.TrimPrefix(text, "JSON")
	if i := strings.LastIndex(text, "```"); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

func missingRequired(schema, obj map[string]any) string {
	required, _ := schema["required"].([]any)
	for _, item := range required {
		name, _ := item.(string)
		if _, ok := obj[name]; !ok {
			return name
		}
	}
	return ""
}

func (e *engine) execTool(ctx context.Context, job *job, bundle *spec.Bundle, found piece) (outcome, error) {
	step := found.step.step
	args := resolveMap(step.Args, job.Env)
	if job.Edit != nil {
		args = resolveMap(job.Edit, job.Env)
	}
	call := e.callChecked
	if outboundVerb(step.Tool) || mustGate(bundle.App, step.Tool) {
		call = e.callApproved
	}
	out, err := call(ctx, e.scope(job), step.Tool, args)
	if err != nil {
		return outcome{}, err
	}
	return outcome{output: out}, nil
}

// callChecked passes the capability check, then calls the connector.
func (e *engine) callChecked(ctx context.Context, scope svc.Scope, tool string, args map[string]any) (any, error) {
	if err := e.allow(ctx, scope, tool, args); err != nil {
		return nil, err
	}
	return e.callConnector(ctx, scope, tool, args)
}

// callApproved is the one door for an action a person approved, from a run
// gate or from the app agent: checked like any call, then let past the
// outbound refusal.
func (e *engine) callApproved(ctx context.Context, scope svc.Scope, tool string, args map[string]any) (any, error) {
	return e.callChecked(withPermit(ctx), scope, tool, args)
}

func (e *engine) execQuery(ctx context.Context, job *job, rec *recBody) (outcome, error) {
	resolved, err := resolveRunValue(rec.Query, job.Env)
	if err != nil {
		return outcome{}, fmt.Errorf("records.query query %q: %w", rec.Query, err)
	}
	query := asText(resolved)
	if err := e.allow(ctx, e.scope(job), "records.query", map[string]any{"type": rec.Type}); err != nil {
		return outcome{}, err
	}
	rows, err := e.queryRows(ctx, job, rec.Type, query, rec.Limit)
	if err != nil {
		return outcome{}, fmt.Errorf("records.query query %q: %w; %s", query, err, spec.RunQueryRule)
	}
	if len(rows) == 1 || rec.Limit == 1 && len(rows) > 0 {
		return outcome{output: rowOut(rows[0])}, nil
	}
	list := make([]any, len(rows))
	for i, row := range rows {
		list[i] = rowOut(row)
	}
	return outcome{output: map[string]any{"rows": list, "total": len(rows)}}, nil
}

func (e *engine) queryRows(ctx context.Context, job *job, recordType, query string, limit int) ([]bridge.Record, error) {
	if e.all.Records == nil {
		return nil, bridge.Errorf(bridge.CodeInternal, "no records")
	}
	page, err := e.all.Records.Query(ctx, e.scope(job), bridge.QueryParams{Type: recordType, Query: query})
	if err != nil {
		return nil, err
	}
	var matched []bridge.Record
	now := e.clock()
	for _, row := range page.Rows {
		if matchQuery(row, query, now) {
			matched = append(matched, row)
		}
	}
	if limit > 0 && limit < len(matched) {
		matched = matched[:limit]
	}
	return matched, nil
}

func (e *engine) execWrite(ctx context.Context, job *job, method string, rec *recBody, update bool) (outcome, error) {
	set, err := resolveRunMap(rec.Set, job.Env)
	if err != nil {
		return outcome{}, err
	}
	if err := e.allow(ctx, e.scope(job), method, map[string]any{"type": rec.Type}); err != nil {
		return outcome{}, err
	}
	if e.all.Records == nil {
		return outcome{}, bridge.Errorf(bridge.CodeInternal, "no records")
	}
	scope := e.scope(job)
	if !update {
		row, err := e.all.Records.Create(ctx, scope, bridge.CreateRecordParams{Type: rec.Type, Set: set})
		if err != nil {
			return outcome{}, err
		}
		return outcome{output: rowOut(row)}, nil
	}
	id, ids, err := resolveRecordIDs(rec, job.Env)
	if err != nil {
		return outcome{}, err
	}
	result, err := e.all.Records.Update(ctx, scope, bridge.UpdateRecordParams{Type: rec.Type, ID: id, IDs: ids, Set: set})
	if err != nil {
		return outcome{}, err
	}
	if len(result.Rows) == 1 {
		return outcome{output: rowOut(result.Rows[0])}, nil
	}
	list := make([]any, len(result.Rows))
	for i, row := range result.Rows {
		list[i] = rowOut(row)
	}
	return outcome{output: map[string]any{"rows": list}}, nil
}

func (e *engine) execDelete(ctx context.Context, job *job, rec *recBody) (outcome, error) {
	if err := e.allow(ctx, e.scope(job), "records.delete", map[string]any{"type": rec.Type}); err != nil {
		return outcome{}, err
	}
	id, ids, err := resolveRecordIDs(rec, job.Env)
	if err != nil {
		return outcome{}, err
	}
	result, err := e.all.Records.Delete(ctx, e.scope(job), bridge.DeleteRecordParams{Type: rec.Type, ID: id, IDs: ids})
	if err != nil {
		return outcome{}, err
	}
	return outcome{output: map[string]any{"ids": result.IDs}}, nil
}

func resolveRecordIDs(rec *recBody, env map[string]any) (string, []string, error) {
	id, err := resolveRunValue(rec.ID, env)
	if err != nil {
		return "", nil, err
	}
	ids, err := resolveRunValue(rec.IDs, env)
	return asText(id), stringList(ids), err
}

func (e *engine) onStepError(ctx context.Context, job *job, found piece, err error) (bool, error) {
	if errors.Is(err, context.Canceled) {
		return true, err
	}
	if errors.Is(err, errCap) {
		e.needsYou(job, e.budgetMessage(job))
		return true, nil
	}
	step := found.step.step
	n := job.Attempts[found.path] + 1
	job.Attempts[found.path] = n
	if step.RetryMax > 0 && n <= step.RetryMax {
		if napErr := e.nap(ctx, backoff(step.RetryBackoff, n)); napErr != nil {
			return true, napErr
		}
		return false, nil
	}
	job.Done[found.path] = stepRec{Status: bridge.RunFailed, Title: step.Title, Error: errText(err)}
	switch {
	case step.OnError == "continue":
		return false, nil
	case strings.HasPrefix(step.OnError, "goto "):
		job.Jump = strings.TrimSpace(strings.TrimPrefix(step.OnError, "goto "))
		job.JumpPrefix = dirOf(found.path)
		return false, nil
	default:
		e.fail(job, err)
		return true, nil
	}
}

func (e *engine) allow(ctx context.Context, scope svc.Scope, method string, params any) error {
	if e.all.Caps == nil {
		return nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return e.all.Caps.Allow(ctx, scope, method, raw)
}

func (e *engine) scope(job *job) svc.Scope {
	member := job.Member
	if member == "" {
		member = svc.Owner
	}
	role := job.Role
	if role == "" {
		role = svc.RoleEditor
	}
	return svc.Scope{App: job.State.App, Caller: svc.Caller{Member: member, Role: role, Agent: job.Agent}, Data: job.Data}
}

func (e *engine) clearWait(job *job) {
	job.WaitPath = ""
	job.WaitKind = ""
	job.WaitUntil = time.Time{}
	job.ApprovalID = ""
	job.Answer = ""
	job.Edit = nil
}

func recordOutput(job *job, id string, output any) {
	steps, _ := job.Env["steps"].(map[string]any)
	if steps == nil {
		steps = map[string]any{}
		job.Env["steps"] = steps
	}
	steps[id] = map[string]any{"output": output}
}

func (e *engine) complete(job *job) {
	job.State.Status = bridge.RunDone
	job.State.EndedAt = e.clock().UTC().Format(time.RFC3339)
	job.State.Error = ""
}

func (e *engine) stop(job *job, message string) {
	job.State.Status = bridge.RunStopped
	job.State.Error = message
	job.State.EndedAt = e.clock().UTC().Format(time.RFC3339)
}

func (e *engine) fail(job *job, err error) {
	job.State.Status = bridge.RunFailed
	job.State.Error = errText(err)
	job.State.EndedAt = e.clock().UTC().Format(time.RFC3339)
}

func (e *engine) needsYou(job *job, message string) {
	job.State.Status = bridge.RunNeedsYou
	if job.AtCap == "stop" {
		job.State.Status = bridge.RunStopped
	}
	job.State.Error = message
	job.State.EndedAt = e.clock().UTC().Format(time.RFC3339)
}

func (e *engine) budgetMessage(job *job) string {
	if job.State.Cost.Cap > 0 {
		return fmt.Sprintf("up to $%.2f", job.State.Cost.Cap)
	}
	return fmt.Sprintf("up to $%.2f", job.Month)
}

func (e *engine) persist(ctx context.Context, job *job, bundle *spec.Bundle) error {
	if bundle != nil && bundle.Runs[job.State.Run] != nil {
		job.State.Steps = materialize(bundle.Runs[job.State.Run].Steps, "", job)
	}
	if err := e.saveJob(job); err != nil {
		return err
	}
	e.publishRun(job.State)
	_ = ctx
	return nil
}

func (e *engine) publishRun(state bridge.RunState) {
	if e.all == nil || e.all.Hub == nil {
		return
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return
	}
	var copy bridge.RunState
	if json.Unmarshal(raw, &copy) != nil {
		return
	}
	e.all.Hub.Publish(bridge.TopicRuns(state.App), copy)
}

func (e *engine) publishApproval(a bridge.Approval) {
	if e.all == nil || e.all.Hub == nil {
		return
	}
	e.all.Hub.Publish(bridge.TopicApprovals(a.App), a)
}

func (e *engine) newID(prefix string) string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return prefix + hex.EncodeToString(buf[:])
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	var typed *bridge.Error
	if errors.As(err, &typed) {
		return typed.Message
	}
	return err.Error()
}

func dirOf(path string) string {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return ""
	}
	return path[:i]
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := asText(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		text := asText(typed)
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func rowOut(row bridge.Record) map[string]any {
	out := map[string]any{"id": row.ID, "type": row.Type}
	for key, value := range row.Fields {
		out[key] = value
	}
	return out
}
