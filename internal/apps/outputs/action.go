package outputs

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type Invocation struct {
	Scope          data.Scope
	Render         schema.RenderAction
	Run, Step, Key string
}

// Invoke runs the same declared Action for the person, agent and run paths.
func Invoke(ctx context.Context, store *data.Store, app schema.App, call Invocation) (data.Output, error) {
	scope := call.Scope
	if scope.App == "" || scope.App != app.ID {
		return data.Output{}, fmt.Errorf("the output must run in its declared app")
	}
	action := call.Render
	t, ok := app.Outputs[action.Output]
	if !ok || app.Kit != 2 {
		return data.Output{}, &data.Error{Code: data.CodeNotFound, Message: "this app does not declare that output"}
	}
	if (action.Record != "") != (t.Record != "") || action.Inbox && t.Query == nil {
		return data.Output{}, &data.Error{Code: data.CodeInvalid, Message: "the input does not match the declared output source"}
	}
	if scope.Principal.ID != "" {
		scope.Action = "render/" + action.Output
		allowed, err := store.AgentAllowed(scope.App, scope.Principal, "actions", scope.Action)
		if err != nil {
			return data.Output{}, err
		}
		if !allowed {
			return data.Output{}, &data.Error{Code: data.CodeNotAllowed, Need: scope.Action, Message: "the agent needs a grant for this render Action"}
		}
	}
	if previous, found, err := store.RunOutput(scope, call.Key); err != nil || found {
		return previous, err
	}
	source, err := store.Type(scope, t.SourceType())
	if err != nil {
		return data.Output{}, err
	}
	rows, matched, err := readRows(store, scope, t, action.Record)
	if err != nil {
		return data.Output{}, err
	}
	if err := ctx.Err(); err != nil {
		return data.Output{}, err
	}
	content, err := Render(t, rows)
	if err != nil {
		return data.Output{}, &data.Error{Code: data.CodeInvalid, Message: err.Error()}
	}
	if err := ctx.Err(); err != nil {
		return data.Output{}, err
	}
	file := data.Output{
		App: app.ID, Action: action.Output, SourceType: source.ID, Version: app.Version,
		Name: action.Output + "." + t.Format, MIME: mediaTypes[t.Format], Inbox: action.Inbox,
		Run: call.Run, Step: call.Step, Matched: matched,
	}
	for _, row := range rows {
		id, _ := row["id"].(string)
		if rev, ok := row["_rev"].(int64); ok && id != "" {
			file.Sources = append(file.Sources, data.OutputSource{ID: id, Rev: rev})
		}
	}
	return store.SaveOutput(scope, file, call.Key, content)
}

var mediaTypes = map[string]string{
	"csv":  "text/csv; charset=utf-8",
	"pdf":  "application/pdf",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

func readRows(store *data.Store, scope data.Scope, t schema.OutputTemplate, record string) ([]map[string]any, int, error) {
	q := t.Query
	if q == nil {
		row, err := store.Get(scope, t.Record, record, false)
		return []map[string]any{row}, 1, err
	}
	ir := &data.QueryIR{Type: q.Type, Select: q.Select, Group: q.Group}
	for _, p := range q.Filter {
		op, ok := filterOps[p.Op]
		if !ok {
			return nil, 0, fmt.Errorf("unsupported output query operator %q", p.Op)
		}
		cond := data.Cond{Field: p.Field, Op: op, Neg: p.Op == "ne"}
		if p.Op != "has" {
			if p.Value == nil {
				return nil, 0, fmt.Errorf("output query filter on %s needs a value", p.Field)
			}
			cond.Value = fmt.Sprint(p.Value)
		}
		ir.Where = append(ir.Where, cond)
	}
	for _, a := range q.Aggregate {
		ir.Aggregate = append(ir.Aggregate, data.Agg{Fn: a.Op, Field: a.Field, As: a.As})
	}
	if len(ir.Aggregate) > 0 {
		result, err := store.Aggregate(scope, ir)
		if err != nil {
			return nil, 0, err
		}
		rows := make([]map[string]any, 0, len(result.Groups))
		for _, group := range result.Groups {
			for i, field := range result.Group {
				group.Values[field] = group.Key[i]
			}
			rows = append(rows, group.Values)
		}
		return rows, result.Total, nil
	}
	limit := schema.OutputRowsMax
	if q.Window != nil {
		if len(q.Window.OrderBy) == 1 {
			ir.Sort = strings.TrimSpace(q.Window.OrderBy[0])
		}
		if q.Window.Limit > 0 {
			limit = q.Window.Limit
		}
	}
	page, err := store.QueryRows(scope, ir, "", limit)
	if err != nil {
		return nil, 0, err
	}
	if page.Next != "" && (q.Window == nil || q.Window.Limit == 0) {
		return nil, 0, &data.Error{Code: data.CodeInvalid, Message: fmt.Sprintf("the output query matched more than %d records; declare a limit or summarize it", limit)}
	}
	rows := make([]map[string]any, 0, len(page.Rows))
	for _, row := range page.Rows {
		rows = append(rows, row)
	}
	return rows, page.Total, nil
}

var filterOps = map[string]string{"eq": "=", "ne": "=", "lt": "<", "lte": "<=", "gt": ">", "gte": ">=", "has": "has"}
