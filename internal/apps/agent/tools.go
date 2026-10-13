package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// toolbox is what the agent can touch: the app's records, its runs and its
// connector verbs. Every call passes svc.Caps first, the same checker the
// bridge uses, so there is no back door (rule 05).
type toolbox struct {
	all     *svc.Services
	scope   svc.Scope
	bundle  *spec.Bundle
	allowed []string
}

// outcome is a tool's result for the model, plus the approval it opened.
type outcome struct {
	result   any
	approval *bridge.Approval
}

// agentTools are the tools agent.md may list besides connector verbs.
var agentTools = []string{
	bridge.MethodRecordsQuery, bridge.MethodRecordsGet,
	bridge.MethodRecordsCreate, bridge.MethodRecordsUpdate, bridge.MethodRunsStart,
}

func newToolbox(all *svc.Services, scope svc.Scope, b *spec.Bundle) *toolbox {
	scope.Caller.Agent = true
	return &toolbox{all: all, scope: scope, bundle: b, allowed: usableTools(b)}
}

// usableTools is agent.md's list without definition-writing methods: the
// agent never gets those, whatever the file says.
func usableTools(b *spec.Bundle) []string {
	var out []string
	for _, t := range b.Agent.Tools {
		if isConnectorVerb(t) || slices.Contains(agentTools, t) {
			out = append(out, t)
		}
	}
	return out
}

func isConnectorVerb(tool string) bool {
	name, verb, ok := strings.Cut(tool, ".")
	return ok && name != "" && verb != "" && name != "records" && name != "runs" &&
		name != "ai" && name != "apps" && name != "builder" && name != "approvals" && name != "agent"
}

func (tb *toolbox) call(ctx context.Context, name string, args map[string]any) (outcome, error) {
	if !slices.Contains(tb.allowed, name) {
		return outcome{}, bridge.Errorf(bridge.CodeDenied, "no tool "+name)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return outcome{}, bridge.Errorf(bridge.CodeInvalid, "args must be an object")
	}
	if err := tb.all.Caps.Allow(ctx, tb.scope, name, raw); err != nil {
		return outcome{}, err
	}
	switch name {
	case bridge.MethodRecordsQuery:
		return decode(raw, tb.query, ctx)
	case bridge.MethodRecordsGet:
		return decode(raw, tb.get, ctx)
	case bridge.MethodRecordsCreate:
		return decode(raw, tb.create, ctx)
	case bridge.MethodRecordsUpdate:
		return decode(raw, tb.update, ctx)
	case bridge.MethodRunsStart:
		return decode(raw, tb.startRun, ctx)
	}
	return tb.connector(ctx, name, args)
}

func decode[P any](raw json.RawMessage, fn func(context.Context, P) (outcome, error), ctx context.Context) (outcome, error) {
	var p P
	if err := json.Unmarshal(raw, &p); err != nil {
		return outcome{}, bridge.Errorf(bridge.CodeInvalid, "bad args: "+err.Error())
	}
	return fn(ctx, p)
}

func (tb *toolbox) query(ctx context.Context, p bridge.QueryParams) (outcome, error) {
	res, err := tb.all.Records.Query(ctx, tb.scope, p)
	res.Rows = tb.withTitles(ctx, tb.hideMasked(res.Rows))
	return outcome{result: res}, err
}

func (tb *toolbox) get(ctx context.Context, p bridge.GetRecordParams) (outcome, error) {
	row, err := tb.all.Records.Get(ctx, tb.scope, p)
	return outcome{result: tb.withTitles(ctx, tb.hideMasked([]bridge.Record{row}))[0]}, err
}

func (tb *toolbox) create(ctx context.Context, p bridge.CreateRecordParams) (outcome, error) {
	row, err := tb.all.Records.Create(ctx, tb.scope, p)
	return outcome{result: tb.hideMasked([]bridge.Record{row})[0]}, err
}

func (tb *toolbox) update(ctx context.Context, p bridge.UpdateRecordParams) (outcome, error) {
	res, err := tb.all.Records.Update(ctx, tb.scope, p)
	res.Rows = tb.hideMasked(res.Rows)
	return outcome{result: res}, err
}

func (tb *toolbox) startRun(ctx context.Context, p bridge.StartRunParams) (outcome, error) {
	state, err := tb.all.Runs.Start(ctx, tb.scope, p)
	return outcome{result: state}, err
}

// connector sends outbound verbs to an approval and never to the connector
// (rule 03). Verbs that only read or draft run straight away.
func (tb *toolbox) connector(ctx context.Context, tool string, args map[string]any) (outcome, error) {
	if !tb.all.Connectors.Outbound(tool) {
		res, err := tb.all.Connectors.Call(ctx, tb.scope, tool, args)
		return outcome{result: res}, err
	}
	opened, err := tb.all.Approvals.Open(ctx, tb.scope, bridge.Approval{
		App:     tb.scope.App,
		Kind:    "send",
		Title:   "Approval needed: " + strings.Replace(tool, ".", " ", 1),
		Summary: describeArgs(args),
		Tool:    tool,
		Payload: args,
		Who:     tb.scope.Caller.Member,
		State:   bridge.ApprovalOpen,
	})
	if err != nil {
		return outcome{}, err
	}
	note := map[string]any{"status": "waiting for approval", "approval": opened.ID}
	return outcome{result: note, approval: &opened}, nil
}

// hideMasked removes masked fields so their values never reach the model.
func (tb *toolbox) hideMasked(rows []bridge.Record) []bridge.Record {
	out := make([]bridge.Record, len(rows))
	for i, row := range rows {
		out[i] = row
		rt := tb.bundle.App.Records[row.Type]
		if rt == nil {
			continue
		}
		out[i].Fields = map[string]any{}
		for name, v := range row.Fields {
			if f := rt.Fields[name]; f == nil || (!f.Masked && f.Blind == nil) {
				out[i].Fields[name] = v
			}
		}
	}
	return out
}

// toolFailure turns a tool error into text the model can react to. Only a
// cancelled context aborts the turn.
func toolFailure(err error) (string, error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	var typed *bridge.Error
	if errors.As(err, &typed) {
		return fmt.Sprintf(`{"error":%q,"code":%q}`, typed.Message, typed.Code), nil
	}
	return fmt.Sprintf(`{"error":%q}`, err.Error()), nil
}

// withTitles adds "<field>_title" beside every ref field, so the model can
// name a related record by its title and never has to show an id.
func (tb *toolbox) withTitles(ctx context.Context, rows []bridge.Record) []bridge.Record {
	titles := map[string]string{}
	for _, row := range rows {
		rt := tb.bundle.App.Records[row.Type]
		if rt == nil {
			continue
		}
		for name, f := range rt.Fields {
			if title := tb.refTitle(ctx, f, row.Fields[name], titles); title != nil && title != "" {
				row.Fields[name+"_title"] = title
			}
		}
	}
	return rows
}

// refTitle is the title of what a ref or refs value points at, or nil when the
// field is not a relation or the agent may not read the target.
func (tb *toolbox) refTitle(ctx context.Context, f *spec.Field, value any, cache map[string]string) any {
	switch f.Type {
	case spec.FieldRef:
		id, _ := value.(string)
		return tb.titleOfRef(ctx, f.To, id, cache)
	case spec.FieldRefs:
		ids, _ := value.([]any)
		var out []string
		for _, id := range ids {
			s, _ := id.(string)
			out = append(out, tb.titleOfRef(ctx, f.To, s, cache))
		}
		return out
	}
	return nil
}

func (tb *toolbox) titleOfRef(ctx context.Context, recordType, id string, cache map[string]string) string {
	if id == "" {
		return ""
	}
	key := recordType + "/" + id
	if title, ok := cache[key]; ok {
		return title
	}
	params := bridge.GetRecordParams{Type: recordType, ID: id}
	raw, _ := json.Marshal(params)
	title := ""
	if tb.all.Caps.Allow(ctx, tb.scope, bridge.MethodRecordsGet, raw) == nil {
		if row, err := tb.all.Records.Get(ctx, tb.scope, params); err == nil {
			title = titleOf(tb.bundle, tb.hideMasked([]bridge.Record{row})[0])
		}
	}
	cache[key] = title
	return title
}
