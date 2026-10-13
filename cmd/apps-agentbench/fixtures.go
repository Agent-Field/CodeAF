package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"gopkg.in/yaml.v3"
)

// modelFiles changes both model selectors in a private copy. The agent body,
// tool list, capabilities, budgets and every other definition stay as built.
func modelFiles(dir, model string) (spec.Files, *spec.Bundle, error) {
	b, err := spec.Load(dir)
	if err != nil {
		return nil, nil, err
	}
	if b.Agent == nil {
		return nil, nil, fmt.Errorf("%s has no agent.md", dir)
	}
	var app map[string]any
	if err := json.Unmarshal(b.Files[spec.AppFile], &app); err != nil {
		return nil, nil, err
	}
	ref, ok := app["agent"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s has no app agent", dir)
	}
	ref["model"] = model
	raw, err := json.Marshal(app)
	if err != nil {
		return nil, nil, err
	}
	b.Files[spec.AppFile] = raw
	b.Agent.Model = model
	front, err := yaml.Marshal(b.Agent)
	if err != nil {
		return nil, nil, err
	}
	b.Files[spec.AgentFile] = []byte("---\n" + string(front) + "---\n" + b.Agent.Body + "\n")
	b, err = spec.Decode(b.Files)
	return b.Files, b, err
}

// makeSeed reuses the product's schema-aware sample generator, then gives the
// first records names and values that everyday owner requests can refer to.
// Required fields and relationships therefore work with Builder variations.
func makeSeed(ctx context.Context, b *spec.Bundle, j job, now time.Time) (map[string][]bridge.Record, error) {
	rows, err := sample.Generate(ctx, b.App, 3, 1)
	if err != nil {
		return nil, err
	}
	for _, hint := range seedHints(j.preset, now) {
		typ, err := recordType(b.App, hint.typ)
		if err != nil {
			return nil, err
		}
		rt := b.App.Records[typ]
		for i, title := range hint.titles {
			rows[typ][i].Fields[rt.Title] = title
		}
		for key, values := range hint.fields {
			name, err := fieldName(rt, key)
			if err != nil {
				continue
			}
			f := rt.Fields[name]
			if f.Computed != "" {
				continue
			}
			for i, value := range values {
				if text, ok := value.(string); ok && f.Type == spec.FieldDate && len(text) > 10 {
					value = text[:10]
				}
				rows[typ][i].Fields[name] = enumValue(f, value)
			}
		}
	}
	if j.task.Operation == "delete" {
		typ, err := recordType(b.App, j.task.Type)
		if err != nil {
			return nil, err
		}
		rows[typ][0].Fields[b.App.Records[typ].Title] = j.task.Target
		if name, err := fieldName(b.App.Records[typ], "email"); err == nil {
			rows[typ][0].Fields[name] = "ana@acme.test"
		}
	}
	return rows, nil
}

type seedHint struct {
	typ    string
	titles []string
	fields map[string][]any
}

func seedHints(preset string, now time.Time) []seedHint {
	old := now.AddDate(0, 0, -21).Format(time.RFC3339)
	recent := now.AddDate(0, 0, -2).Format(time.RFC3339)
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1)
	switch preset {
	case "crm":
		return []seedHint{
			{"company", []string{"Acme", "Northwind", "Cedar"}, nil},
			{"contact", []string{"Jo Silva", "Sam Lee", "Pat Chen"}, map[string][]any{"email": {"jo@acme.test", "sam@northwind.test", "pat@cedar.test"}}},
			{"deal", []string{"Acme deal", "Northwind renewal", "Cedar pilot"}, map[string][]any{"stage|status": {"Lead", "Proposal", "Qualified"}, "value|amount": {float64(1000), float64(3000), float64(2000)}, "last_touch|last_contacted|last_contact|last_activity|last_contact_at|last_activity_at": {old, recent, old}}},
		}
	case "tracker":
		return []seedHint{
			{"project", []string{"Website", "Mobile", "Operations"}, nil},
			{"issue", []string{"Login bug", "Search polish", "Update docs"}, map[string][]any{"status": {"Open", "In progress", "Done"}, "priority": {"High", "Medium", "Low"}, "assignee": {"alex", "me", "me"}, "due_on|due_date|due": {yesterday, tomorrow.Format("2006-01-02"), yesterday}}},
		}
	case "inventory":
		return []seedHint{
			{"supplier", []string{"Bike Supply", "Parts Depot", "Cycle Goods"}, nil},
			{"product", []string{"Road Tyre", "Chain", "Helmet"}, map[string][]any{"stock|quantity|stock_quantity|in_stock": {float64(2), float64(20), float64(1)}, "reorder_level|reorder_point": {float64(5), float64(5), float64(3)}, "price": {float64(30), float64(15), float64(40)}, "sku": {"TYRE-01", "CHAIN-01", "HELMET-01"}}},
		}
	case "booking":
		return []seedHint{
			{"client", []string{"Ana Costa", "Jo Silva", "Pat Chen"}, nil},
			{"service", []string{"Consultation", "Follow-up", "Assessment"}, nil},
			{"booking", []string{"Ana consultation", "Jo follow-up", "Pat assessment"}, map[string][]any{"status": {"Booked", "Confirmed", "Done"}, "starts|starts_at|start|start_at|date_time|datetime": {tomorrow.Format(time.RFC3339), tomorrow.Add(2 * time.Hour).Format(time.RFC3339), old}}},
		}
	}
	return nil
}

func recordType(app *spec.App, requested string) (string, error) {
	for _, name := range []string{requested, requested + "s"} {
		if app.Records[name] != nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("bundle has no %s record type", requested)
}

func fieldName(rt *spec.RecordType, alternatives string) (string, error) {
	for _, name := range strings.Split(alternatives, "|") {
		if rt.Fields[name] != nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("record has no field matching %s", alternatives)
}

func enumValue(f *spec.Field, value any) any {
	text, ok := value.(string)
	if !ok || f.Type != spec.FieldEnum {
		return value
	}
	for _, candidate := range f.Values {
		if strings.EqualFold(text, candidate) {
			return candidate
		}
	}
	return value
}

// expectedFields resolves the task's semantic fields against this bundle.
// Missing fields are setup errors, not measurements of model quality.
func expectedFields(b *spec.Bundle, t task, rows map[string][]bridge.Record) (string, map[string]any, error) {
	typ, err := recordType(b.App, t.Type)
	if err != nil {
		return "", nil, err
	}
	rt := b.App.Records[typ]
	set := map[string]any{}
	if t.Target != "" {
		set[rt.Title] = t.Target
	}
	for key, value := range t.Set {
		name, err := fieldName(rt, key)
		if err != nil {
			return "", nil, fmt.Errorf("%s: %w", typ, err)
		}
		if ref, ok := value.(string); ok && strings.HasPrefix(ref, "@") {
			target, err := recordType(b.App, strings.TrimPrefix(ref, "@"))
			if err != nil {
				return "", nil, err
			}
			value = rows[target][0].ID
		}
		set[name] = enumValue(rt.Fields[name], value)
	}
	return typ, set, nil
}
