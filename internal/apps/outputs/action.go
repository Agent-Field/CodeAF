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
	if previous, found, err := store.RunOutput(scope, call.Key); err != nil || found {
		return previous, err
	}
	action := call.Render
	t, ok := app.Outputs[action.Output]
	if !ok || app.Kit != 2 {
		return data.Output{}, &data.Error{Code: data.CodeNotFound, Message: "this app does not declare that output"}
	}
	if (action.Record != "") != (t.Record != "") || action.Inbox && t.Query == nil {
		return data.Output{}, &data.Error{Code: data.CodeInvalid, Message: "the input does not match the declared output source"}
	}
	source, err := store.Type(scope, t.SourceType())
	if err != nil {
		return data.Output{}, err
	}
	rows, err := readRows(store, scope, t, action.Record)
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
		Run: call.Run, Step: call.Step,
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

func readRows(store *data.Store, scope data.Scope, t schema.OutputTemplate, record string) ([]map[string]any, error) {
	q := t.Query
	if q == nil {
		row, err := store.Get(scope, t.Record, record, false)
		return []map[string]any{row}, err
	}
	ir := &data.QueryIR{Type: q.Type, Select: q.Select, Group: q.Group}
	for _, p := range q.Filter {
		op, ok := filterOps[p.Op]
		if !ok {
			return nil, fmt.Errorf("unsupported output query operator %q", p.Op)
		}
		ir.Where = append(ir.Where, data.Cond{Field: p.Field, Op: op, Value: fmt.Sprint(p.Value), Neg: p.Op == "ne"})
	}
	for _, a := range q.Aggregate {
		ir.Aggregate = append(ir.Aggregate, data.Agg{Fn: a.Op, Field: a.Field, As: a.As})
	}
	if len(ir.Aggregate) > 0 {
		result, err := store.Aggregate(scope, ir)
		if err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(result.Groups))
		for _, group := range result.Groups {
			for i, field := range result.Group {
				group.Values[field] = group.Key[i]
			}
			rows = append(rows, group.Values)
		}
		return rows, nil
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
		return nil, err
	}
	if page.Next != "" && (q.Window == nil || q.Window.Limit == 0) {
		return nil, &data.Error{Code: data.CodeInvalid, Message: fmt.Sprintf("the output query matched more than %d records; declare a limit or summarize it", limit)}
	}
	rows := make([]map[string]any, 0, len(page.Rows))
	for _, row := range page.Rows {
		rows = append(rows, row)
	}
	return rows, nil
}

var filterOps = map[string]string{"eq": "=", "ne": "=", "lt": "<", "lte": "<=", "gt": ">", "gte": ">=", "has": "has"}
