package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type action struct {
	Call
	Render            *schema.RenderAction `json:"render,omitempty"`
	RecordType        string               `json:"record_type,omitempty"`
	IDs               []string             `json:"ids,omitempty"`
	Prompt            string               `json:"prompt,omitempty"`
	Outbound          bool                 `json:"outbound,omitempty"`
	LocalWriteReceipt bool                 `json:"local_write_receipt,omitempty"`
}

const maxTransitions = 10_000

func (e *Engine) progress(ctx context.Context, c *checkpoint) error {
	// The bound also covers cycles formed by on_error goto.
	for ctx.Err() == nil {
		if c.Visit > maxTransitions {
			return e.stop(c, YourCall, "step limit reached")
		}
		if c.Declined != "" {
			return e.stop(c, Incomplete, c.Declined)
		}
		if c.Approval != nil || (!c.WakeAt.IsZero() && e.cfg.Now().Before(c.WakeAt)) {
			return nil
		}
		steps, vars := c.scope(e.cfg.Now())
		if e.cfg.OwnerName != "" {
			vars["owner"] = map[string]any{"name": e.cfg.OwnerName}
		}
		e.addRecordReader(c.App, vars)
		current := &c.Frames[len(c.Frames)-1]
		if current.Index == len(steps) {
			if len(c.Frames) == 1 {
				return e.stop(c, Done, "")
			}
			current.Index = 0
			current.Round++
			if err := e.enterRound(c); err != nil {
				return err
			}
			if c.State != Running && c.State != Finishing {
				return nil
			}
			continue
		}
		step := steps[current.Index]
		if c.Action == nil {
			run, err := condition(step.If, vars, nil)
			if err != nil {
				return e.stop(c, Incomplete, err.Error())
			}
			if !run {
				if err := e.advance(c, "", nil); err != nil {
					return err
				}
				continue
			}
			act, err := prepare(c, step, vars)
			if err != nil {
				if err := e.stepFailed(c, step, err); err != nil || c.State != Running {
					return err
				}
				continue
			}
			if act.Prompt != "" && len(c.Steering) > 0 {
				act.Prompt += "\n\nPerson’s follow-up:\n" + strings.Join(c.Steering, "\n")
				c.Steering = nil
			}
			c.Action, c.CanSteer = &act, false
		}
		outbound := c.Action.Outbound
		if c.AgentOrigin != nil {
			if e.cfg.CheckAgentAction == nil {
				return e.stop(c, Incomplete, "the agent action grant is unavailable")
			}
			if err := e.cfg.CheckAgentAction(c.App, *c.AgentOrigin); err != nil {
				return e.stop(c, Incomplete, err.Error())
			}
		}
		recordWrite := step.RecordsCreate != nil || step.RecordsUpdate != nil
		if c.Prepared && (outbound || step.Agent != "" || step.AI != nil || recordWrite && !c.Action.LocalWriteReceipt) {
			// Only a local write prepared with durable receipts can distinguish
			// a commit from a crash before dispatch.
			if connection, _, ok := schema.ParseSourceTool(step.Tool); ok {
				c.Effects = append(c.Effects, EffectReceipt{Step: step.ID, Tool: step.Tool,
					Key: c.Action.Key, Connection: connection, Outcome: "unknown"})
			}
			return e.stop(c, Incomplete, "step stopped before its result was saved")
		}
		// Source approvals need the local authority stamp even when the
		// declared gate is shown before the connector is checked.
		if connector, ok := e.cfg.Connector.(interface{ SourceFence(Call) (string, error) }); ok && outbound {
			fence, err := connector.SourceFence(c.Action.Call)
			switch {
			case err != nil:
				return e.stop(c, Incomplete, err.Error())
			case c.Approved != "" && c.Action.SourceFence != fence:
				return e.stop(c, Incomplete, "the source authority changed after approval")
			}
			c.Action.SourceFence = fence
		}
		// A declared gate can hold its exact payload while a connection is
		// offline. The connector is still checked before any delivery.
		if step.Gate != nil && c.Approved == "" {
			who := step.Gate.Who
			if who != "" {
				resolved, err := resolve(who, vars)
				if err != nil {
					return e.stop(c, Incomplete, err.Error())
				}
				who, _ = resolved.(string)
			}
			return e.ask(c, step.Gate.Approval, who)
		}
		if step.Tool != "" && e.cfg.CheckTool != nil {
			needsGate, err := e.cfg.CheckTool(ctx, c.Action.Call)
			if err != nil {
				if err := e.stepFailed(c, step, err); err != nil || c.State != Running {
					return err
				}
				continue
			}
			outbound = outbound || needsGate
			if c.Prepared && outbound {
				return e.stop(c, Incomplete, "step stopped before its result was saved")
			}
		}
		c.Action.Outbound = outbound

		if outbound && c.Approved != outboundGate(step.Tool) {
			return e.ask(c, outboundGate(step.Tool), c.Action.As)
		}
		switch {
		case step.Wait != nil:
			if c.WakeAt.IsZero() {
				d, _ := duration(step.Wait.For)
				c.WakeAt = e.cfg.Now().Add(d)
				return e.save(c, "", nil)
			}
			if err := e.advance(c, "", nil); err != nil {
				return err
			}
		case step.Loop != nil:
			f := frame{}
			if step.Loop.Over != "" {
				items, err := resolve(step.Loop.Over, vars)
				if err != nil {
					return e.stop(c, Incomplete, err.Error())
				}
				list, ok := items.([]any)
				if !ok {
					return e.stop(c, Incomplete, "loop over must resolve to a list")
				}
				// Only the rounds allowed by max need to be kept, plus one
				// entry to distinguish a full loop from an exhausted limit.
				f.Items = list
				if step.Loop.Max < len(list) {
					f.Items = list[:step.Loop.Max+1]
				}
			}
			c.Frames = append(c.Frames, f)
			if err := e.enterRound(c); err != nil {
				return err
			}
			if c.State != Running && c.State != Finishing {
				return nil
			}
		case step.RecordsCreate != nil || step.RecordsUpdate != nil || step.Tool != "" || step.Agent != "" || step.AI != nil || step.Render != nil:
			if err := e.execute(ctx, c, step); err != nil {
				return err
			}
			if c.State != Running && c.State != Finishing {
				return nil
			}
		default:
			if err := e.advance(c, "", nil); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func (c *checkpoint) scope(now time.Time) ([]Step, map[string]any) {
	vars := map[string]any{
		"me": "me", "now": now.UTC().Format(time.RFC3339Nano), "params": c.Params,
		"input": c.Input, "record": c.Input["record"], "steps": c.Outputs,
	}
	// A declared parameter may be used by its short name in templates.
	// System words and enclosing loop bindings keep their usual meaning.
	for name := range c.Definition.Params {
		if value, ok := c.Params[name]; ok && celVariable(name) && !reservedVariable(name) {
			vars[name] = value
		}
	}
	steps := c.Definition.Steps
	for i := 1; i < len(c.Frames); i++ {
		parent := steps[c.Frames[i-1].Index]
		f := c.Frames[i]
		if parent.Loop.As != "" && f.Round < len(f.Items) {
			vars[parent.Loop.As] = f.Items[f.Round]
		}
		steps = parent.Steps
	}
	return steps, vars
}

// A prompt can name the fields of its one clear subject directly. Existing
// run variables keep precedence and projected records omit masked fields.
func (c *checkpoint) promptVariables(vars map[string]any) map[string]any {
	record, _ := vars["record"].(map[string]any)
	if record == nil {
		var ref string
		for name, param := range c.Definition.Params {
			if param.Type != "ref" {
				continue
			}
			if ref != "" {
				return vars
			}
			ref = name
		}
		record, _ = c.Params[ref].(map[string]any)
	}
	if record == nil {
		return vars
	}
	result := maps.Clone(vars)
	for key, value := range record {
		if _, declared := result[key]; !declared {
			result[key] = value
		}
	}
	return result
}

func prepare(c *checkpoint, step Step, vars map[string]any) (action, error) {
	a := action{Call: Call{App: c.App, Run: c.DefinitionID, ID: c.ID, Step: step.ID,
		Key: fmt.Sprintf("%s-%d", c.ID, c.Visit), Name: step.Tool, As: "me"}, Outbound: schema.IsOutboundTool(step.Tool)}
	if step.As != "" {
		as, err := resolve(step.As, vars)
		if err != nil {
			return a, err
		}
		if as != "me" {
			return a, errors.New("personal apps can only act as me")
		}
	}
	if step.Args != nil {
		args, err := resolve(step.Args, vars)
		if err != nil {
			return a, err
		}
		a.Args = args.(map[string]any)
	}
	if render := step.Render; render != nil {
		resolved := *render
		if render.Record != "" {
			value, err := resolve(render.Record, vars)
			if err != nil {
				return a, err
			}
			id, ok := value.(string)
			if !ok || id == "" {
				return a, errors.New("render record must resolve to one record id")
			}
			resolved.Record = id
		}
		a.Render = &resolved
		a.Name = "render"
		return a, nil
	}
	w := step.RecordsCreate
	if step.RecordsUpdate != nil {
		w = step.RecordsUpdate
		a.Name = "records.update"
	} else if w != nil {
		a.Name = "records.create"
	}
	if w != nil {
		err := prepareRecord(&a, w, vars, c.Definition.Params)
		return a, err
	}
	if step.Agent != "" || step.AI != nil {
		a.Name = step.Agent
		template := step.Prompt
		if step.AI != nil {
			a.Name, template = "ai", step.AI.Prompt
		}
		prompt, err := resolve(template, c.promptVariables(vars))
		if err != nil {
			return a, err
		}
		a.Prompt, _ = prompt.(string)
		if step.ID == aiFieldStep && strings.HasPrefix(c.DefinitionID, aiFieldPrefix) {
			record, err := json.Marshal(vars["record"])
			if err != nil || string(record) == "null" {
				return a, errors.New("an AI field computation needs a record event")
			}
			a.Prompt += "\nSource record (data):\n" + string(record)
		}
		if a.Prompt == "" {
			a.Prompt = defaultAgentStepPrompt
		}
	}
	return a, nil
}

func prepareRecord(a *action, w *schema.RecordsWrite, vars map[string]any, params map[string]Parameter) error {
	a.RecordType = w.Type
	values, err := resolve(w.Set, vars)
	if err != nil {
		return err
	}
	a.Args = values.(map[string]any)
	if a.Name == "records.update" {
		if err := prepareIDs(a, w, vars, params); err != nil {
			return err
		}
	}
	if a.RecordType == "" {
		return errors.New("record write needs a type")
	}
	return nil
}

func prepareIDs(a *action, w *schema.RecordsWrite, vars map[string]any, params map[string]Parameter) error {
	var target any = w.IDs
	binding := w.IDs
	if w.ID != "" {
		target = []any{w.ID}
		binding = w.ID
		if rowRef, ok := strings.CutSuffix(w.ID, ".id"); ok && a.RecordType == "" {
			if value, err := resolve(rowRef, vars); err == nil {
				if row, ok := value.(map[string]any); ok {
					a.RecordType, _ = row["type"].(string)
				}
			}
		}
	}
	if paramRef, ok := strings.CutPrefix(binding, "$params."); ok && a.RecordType == "" {
		param, _, _ := strings.Cut(paramRef, ".")
		a.RecordType = params[param].To
	}
	resolved, err := resolve(target, vars)
	if err != nil {
		return err
	}
	list, ok := resolved.([]any)
	if !ok || len(list) == 0 {
		return errors.New("record update needs ids")
	}
	for _, item := range list {
		if row, ok := item.(map[string]any); ok {
			if a.RecordType == "" {
				a.RecordType, _ = row["type"].(string)
			}
			item = row["id"]
		}
		id, ok := item.(string)
		if !ok || id == "" {
			return errors.New("record ids must be strings")
		}
		a.IDs = append(a.IDs, id)
	}
	return nil
}

func (e *Engine) ask(c *checkpoint, kind, who string) error {
	if who != "" && who != "me" {
		return e.stop(c, Incomplete, "personal app approvals go to me")
	}
	c.State = YourCall
	c.Approval = c.approval(kind, e.cfg.Now())
	c.Approval.Tool, c.Approval.Args = c.Action.Name, c.Action.Args
	if c.Approval.Tool == "" {
		c.Approval.Tool = kind
	}
	return e.publishApproval(c)
}

func (c *checkpoint) approval(kind string, now time.Time) *Approval {
	c.ApprovalSeq++
	return &Approval{ID: fmt.Sprintf("%s-%d-%s", c.Action.Key, c.ApprovalSeq, kind), Kind: kind, Who: "me", AskedAt: now}
}

func (e *Engine) publishApproval(c *checkpoint) error {

	if err := e.save(c, "", nil); err != nil {
		return err
	}
	if e.cfg.Ask != nil {
		e.cfg.Ask(c.Run)
	}
	return nil
}

func (e *Engine) enterRound(c *checkpoint) error {
	i := len(c.Frames) - 1
	steps := c.Definition.Steps
	for _, f := range c.Frames[:i-1] {
		steps = steps[f.Index].Steps
	}
	parent := steps[c.Frames[i-1].Index]
	frame := c.Frames[i]
	if parent.Loop.Over != "" && frame.Round == len(frame.Items) {
		c.Frames = c.Frames[:i]
		return e.advance(c, "", nil)
	}
	if parent.Loop.Until != "" && frame.Round > 0 {
		_, vars := c.scope(e.cfg.Now())
		e.addRecordReader(c.App, vars)
		finished, err := condition(parent.Loop.Until, vars, nil)
		if err != nil {
			return e.stop(c, Incomplete, err.Error())
		}
		if finished {
			c.Frames = c.Frames[:i]
			return e.advance(c, "", nil)
		}
	}
	if frame.Round >= parent.Loop.Max {
		c.Step = parent.ID
		return e.stop(c, YourCall, "out of rounds")
	}
	c.Step = parent.Steps[0].ID
	c.reset()
	return e.save(c, "", nil)
}

func (c *checkpoint) budget() float64 {
	if c.Definition.Budget == nil {
		return c.RaisedBudget
	}
	return max(c.RaisedBudget, c.Definition.Budget.PerRun)
}

func (e *Engine) askBudget(c *checkpoint) error {
	if c.Queue != "" && c.Queue != c.ID {
		return e.stop(c, Incomplete, ErrBudget.Error())
	}
	c.Prepared = false
	c.State = YourCall
	c.Error = ErrBudget.Error()
	c.Approval = c.approval("budget", e.cfg.Now())
	c.Approval.MinBudget = max(c.Cost, c.budget())
	return e.publishApproval(c)
}

func (e *Engine) execute(ctx context.Context, c *checkpoint, step Step) error {
	budget := c.budget()
	if (step.Tool != "" || step.Agent != "" || step.AI != nil) && budget > 0 && c.Cost >= budget {
		return e.askBudget(c)
	}
	a := c.Action
	if !c.Prepared || !a.LocalWriteReceipt {
		c.Prepared, c.State, c.Error = true, Running, ""
		c.WakeAt = time.Time{}
		a.LocalWriteReceipt = step.RecordsCreate != nil || step.RecordsUpdate != nil
		if err := e.save(c, "", nil); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.Outbound && c.AppVersion != 0 && e.cfg.AppVersion != nil {
		current, err := e.cfg.AppVersion(c.App)
		if err != nil || current != c.AppVersion {
			return e.stop(c, Incomplete, "the app changed after the effect was prepared")
		}
	}
	var reply Reply
	var err error
	switch {
	case a.Render != nil:
		if e.cfg.Render == nil {
			err = errors.New("rendering outputs is unavailable in this run")
		} else {
			reply, err = e.cfg.Render(ctx, a.Call, *a.Render)
		}
	case step.RecordsCreate != nil || step.RecordsUpdate != nil:
		if e.cfg.Records == nil {
			err = errors.New("record writes are unavailable")
		} else {
			call := RecordCall{Call: a.Call, Type: a.RecordType, IDs: a.IDs, Set: a.Args, agentOrigin: c.AgentOrigin, ai: c.IntakeAI, localWriteReceipt: a.LocalWriteReceipt}
			if c.TriggerReceipt != nil {
				causes := c.TriggerReceipt.CauseChain
				start := max(0, len(causes)-(maxRunCauseChain-1))
				call.causeChain = append(slices.Clone(causes[start:]), "run:"+c.ID)
			}
			reply, err = e.cfg.Records(ctx, call)
		}
	case step.Agent != "" || step.AI != nil:
		if e.cfg.Agent == nil {
			err = errors.New("the app agent is unavailable")
		} else {
			reply, err = e.completeStep(ctx, c, step, a, budget)
		}
	default:
		if e.cfg.Connector == nil {
			err = errors.New("connectors are unavailable")
		} else {
			call := a.Call
			call.approved = a.Outbound && c.Approved == outboundGate(a.Name)
			reply, err = e.cfg.Connector.Call(ctx, call)
		}
	}
	if reply.Effect != nil {
		c.Effects = append(c.Effects, *reply.Effect)
		if err != nil {
			return e.stop(c, Incomplete, err.Error())
		}
	}
	if validCost(reply.Cost) {
		c.Cost += reply.Cost
	} else {
		return e.stop(c, Incomplete, "step returned an invalid cost")
	}
	if (step.AI != nil || step.Agent != "") && budget > 0 && c.Cost > budget {
		return e.stop(c, Incomplete, ErrBudget.Error())
	}
	if err == nil && step.AI != nil && step.ID == aiFieldStep && strings.HasPrefix(c.DefinitionID, aiFieldPrefix) {
		reply.Output, err = e.fillAIFields(ctx, c, reply.Output)
	}
	if err == nil && step.AI != nil && step.AI.Propose != "" {
		values, _ := reply.Output.(map[string]any)
		_, vars := c.scope(e.cfg.Now())
		c.Proposal, err = makeProposal(step.AI.Propose, vars, values)
		if err == nil && c.Queue == "" {
			c.Queue = c.ID
		}
	}
	if err == nil {
		if step.Agent != "" && c.TriggerReceipt != nil && c.TriggerReceipt.IntakeType != "" {
			c.IntakeAI = true
		}
		return e.advance(c, "", reply.Output)
	}
	return e.stepFailed(c, step, err)
}

func (e *Engine) stepFailed(c *checkpoint, step Step, err error) error {
	if errors.Is(err, ErrBudget) {
		return e.askBudget(c)
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		return e.stop(c, Incomplete, err.Error())
	}
	if !errors.Is(err, ErrAgentOutput) && step.Retry != nil && c.Attempt < step.Retry.Max {
		const ceiling = 10 * time.Minute
		backoff := time.Second
		if step.Retry.Backoff != "" {
			backoff, _ = duration(step.Retry.Backoff)
		}
		backoff = min(backoff, ceiling)
		for range c.Attempt {
			backoff = min(backoff*2, ceiling)
		}
		c.Attempt++
		c.Prepared, c.State, c.WakeAt = false, Running, e.cfg.Now().Add(backoff)
		return e.save(c, "", nil)
	}
	if step.OnError == "continue" {
		return e.advance(c, "", nil)
	}
	if target, ok := strings.CutPrefix(step.OnError, "goto "); ok {
		return e.advance(c, target, nil)
	}
	return e.stop(c, Incomplete, err.Error())
}

func (c *checkpoint) reset() {
	c.Action, c.Approval = nil, nil
	c.Approved, c.Declined, c.Prepared = "", "", false
	c.Attempt, c.State, c.WakeAt = 0, Running, time.Time{}
	c.Visit++
}

func (e *Engine) advance(c *checkpoint, target string, output any) error {
	steps, _ := c.scope(e.cfg.Now())
	f := &c.Frames[len(c.Frames)-1]
	stepID := steps[f.Index].ID
	delete(c.Outputs, stepID)
	if output != nil {
		values := map[string]any{"output": output}
		if object, ok := output.(map[string]any); ok {
			maps.Copy(values, object)
			values["output"] = output
		}
		c.Outputs[stepID] = values
	}
	f.Index++
	c.reset()
	if target != "" {
		return e.jump(c, target)
	}
	if f.Index < len(steps) {
		c.Step = steps[f.Index].ID
	} else if len(c.Frames) == 1 && c.Harness != "" {
		c.State = Finishing
	}
	return e.save(c, "", nil)
}

func (e *Engine) jump(c *checkpoint, target string) error {
	steps := c.Definition.Steps
	for depth, frame := range c.Frames {
		for index, step := range steps {
			if step.ID == target {
				c.Frames = c.Frames[:depth+1]
				c.Frames[depth].Index = index
				c.Step = target
				return e.save(c, "", nil)
			}
		}
		if frame.Index < len(steps) {
			steps = steps[frame.Index].Steps
		}
	}
	return e.stop(c, Incomplete, "error handling step is outside this run path")
}

func (e *Engine) stop(c *checkpoint, state, reason string) error {
	c.State, c.Error, c.WakeAt, c.Approval = state, reason, time.Time{}, nil
	if state == Done || state == Incomplete {
		c.EndedAt = e.cfg.Now()
	}
	if state == Done {
		c.Step = ""
	}
	return e.save(c, "", nil)
}

func outboundGate(tool string) string {
	if kind := schema.ToolApprovalKind(tool); kind != "" {
		return kind
	}
	return "send"
}
