package check

import (
	"context"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type renderer struct {
	ctx    context.Context
	bundle *spec.Bundle
	kit    *spec.Kit
	info   screenInfo
	rows   map[string][]bridge.Record
	file   string
	index  int
	issues []Issue
	seen   map[string]bool
}

func render(ctx context.Context, b *spec.Bundle, kit *spec.Kit, screen *spec.Screen, rows map[string][]bridge.Record) []Issue {
	r := renderer{ctx: ctx, bundle: b, kit: kit, info: inspect(screen), rows: rows, file: "screens/" + screen.Screen + ".json", seen: map[string]bool{}}
	count := 1
	for _, records := range rows {
		if len(records) > count {
			count = len(records)
		}
	}
	// Single, empty and multiple selections exercise guarded panels and bulk
	// controls without a Cartesian product of every list in a screen.
	for index := 0; index < count; index++ {
		r.index = index
		for _, selection := range []int{1, 0, 2} {
			values := r.values(selection)
			r.node(screen.Nav, "/nav", values, r.info.recordType)
			r.node(screen.Body, "/body", values, r.info.recordType)
		}
	}
	return r.issues
}

func (r *renderer) issue(path, rule string, err error, hint string) {
	key := path + "|" + rule
	if r.seen[key] {
		return
	}
	r.seen[key] = true
	r.issues = append(r.issues, finding(r.file, path, rule, err.Error(), hint))
}

func (r *renderer) values(selected int) map[string]any {
	values := map[string]any{"me": "me", "space": r.bundle.App.Space, "now": "2026-10-13T12:00:00Z", "params": map[string]any{}, "selection": map[string]any{}, "input": map[string]any{}}
	params := values["params"].(map[string]any)
	for _, name := range keys(r.info.params) {
		typ := r.info.params[name]
		params[name] = "sample"
		if len(r.rows[typ]) > 0 {
			params[name] = r.rows[typ][r.index%len(r.rows[typ])].ID
		}
	}
	selection := values["selection"].(map[string]any)
	for _, id := range keys(r.info.lists) {
		records := r.rows[r.info.lists[id]]
		ids := []string{}
		if selected > 0 && len(records) > 0 {
			ids = append(ids, records[r.index%len(records)].ID)
			if selected > 1 && len(records) > 1 {
				ids = append(ids, records[(r.index+1)%len(records)].ID)
			}
		}
		entry := map[string]any{"ids": ids, "count": int64(len(ids)), "id": ""}
		if len(ids) == 1 {
			entry["id"] = ids[0]
		}
		selection[id] = entry
	}
	typ := r.info.recordType
	if typ == "" {
		for _, id := range keys(r.info.lists) {
			typ = r.info.lists[id]
			break
		}
	}
	if len(r.rows[typ]) > 0 {
		record := r.record(typ, r.rows[typ][r.index%len(r.rows[typ])])
		values["record"] = record
		values["row"] = record
		if def := r.bundle.App.Records[typ]; def != nil {
			for key := range def.Fields {
				if _, reserved := values[key]; !reserved {
					values[key] = record[key]
				}
			}
			for key, value := range def.Constants {
				if _, reserved := values[key]; !reserved {
					values[key] = value
				}
			}
		}
	}
	return values
}

func (r *renderer) record(typ string, row bridge.Record) map[string]any {
	values := map[string]any{"id": row.ID, "type": typ, "version": int64(row.Version), "fields": row.Fields}
	if record := r.bundle.App.Records[typ]; record != nil {
		for name := range record.Fields {
			values[name] = row.Fields[name]
		}
		for name, value := range record.Constants {
			values[name] = value
		}
	}
	return values
}

func (r *renderer) node(n *spec.Node, path string, values map[string]any, typ string) {
	if n == nil || r.ctx.Err() != nil {
		return
	}
	values = clone(values)
	typ = nodeType(n, typ, r.info)
	visible := true
	if n.When != "" {
		program, err := compileWhen(n.When, values)
		if err == nil {
			visible, err = evalWhen(r.ctx, program, values)
		}
		if err != nil {
			r.issue(path+"/when", "render/when", err, "write a boolean CEL expression that works on every sample row")
		}
	}
	if visible || (n.Bind != nil && n.Bind.Records != "") {
		r.bind(n.Bind, path+"/bind", values, typ)
	}
	if visible {
		r.props(n, path, values, typ)
		r.actions(n, path, values, typ)
	}
	nodeChildren(n, path, func(child *spec.Node, childPath string) {
		if visible {
			r.node(child, childPath, values, typ)
		} else {
			r.hidden(child, childPath, values)
		}
	})
}

func (r *renderer) hidden(n *spec.Node, path string, values map[string]any) {
	if n == nil {
		return
	}
	if n.Bind != nil && n.Bind.Records != "" {
		r.bind(n.Bind, path+"/bind", clone(values), n.Bind.Records)
	}
	if n.When != "" {
		program, err := compileWhen(n.When, values)
		if err == nil {
			_, err = evalWhen(r.ctx, program, values)
		}
		if err != nil {
			r.issue(path+"/when", "render/when", err, "write a boolean CEL expression using declared targets")
		}
	}
	nodeChildren(n, path, func(child *spec.Node, childPath string) { r.hidden(child, childPath, values) })
}

func (r *renderer) bind(bind *spec.Binding, path string, values map[string]any, typ string) {
	if bind == nil {
		return
	}
	if bind.Records != "" {
		records := r.rows[bind.Records]
		if len(records) == 0 {
			r.issue(path+"/records", "render/bind", fmt.Errorf("no sample records for %q", bind.Records), "generate sample records for the bound type")
			return
		}
		record := r.record(bind.Records, records[r.index%len(records)])
		values["row"] = record
		resolvedQuery, err := resolveValue(bind.Query, values)
		if err == nil {
			if _, text := resolvedQuery.(string); !text {
				err = fmt.Errorf("query must resolve to a string, got %T", resolvedQuery)
			}
		}
		recordType := r.bundle.App.Records[bind.Records]
		if err == nil && recordType != nil {
			_, err = query.Parse(recordType, resolvedQuery.(string), svc.Owner)
		}
		if err != nil {
			r.issue(path+"/query", "render/bind", fmt.Errorf("%s: %w", r.file, err), boundFieldHint(recordType, bind.Records))
		}
		return
	}
	if bind.Record != "" {
		value, err := resolveValue(bind.Record, values)
		if err != nil {
			r.issue(path+"/record", "render/bind", err, "use a declared parameter, selected id or sample record id")
			return
		}
		id, ok := value.(string)
		if !ok {
			r.issue(path+"/record", "render/bind", fmt.Errorf("record id must be a string, got %T", value), "bind to one record id")
			return
		}
		for _, row := range r.rows[typ] {
			if row.ID == id {
				values["record"] = r.record(typ, row)
				return
			}
		}
		r.issue(path+"/record", "render/bind", fmt.Errorf("record %q does not exist in sample type %q", id, typ), "use a matching sample id and guard empty selections with when")
	}
	if bind.For != "" {
		value, err := resolveValue(bind.For, values)
		if err == nil {
			if _, ok := value.(string); !ok {
				err = fmt.Errorf("run for must resolve to a record id, got %T", value)
			}
		}
		if err != nil {
			r.issue(path+"/for", "render/bind", err, "reference a single record id")
		}
	}
	if bind.Selection != "" {
		if _, err := resolve("$selection."+bind.Selection, values); err != nil {
			r.issue(path+"/selection", "render/bind", err, "name a list in this screen")
		}
	}
}

func (r *renderer) props(n *spec.Node, path string, values map[string]any, typ string) {
	part := r.kit.Part(n.Part)
	if part == nil {
		return
	}
	for _, key := range keys(n.Props) {
		prop := part.Props[key]
		if prop == nil {
			continue
		}
		value, err := resolveValue(n.Props[key], values)
		if err == nil && !prop.Accepts(value) {
			err = fmt.Errorf("resolved prop must have type %s, got %T", prop.Type, value)
		}
		if err == nil && prop.Type == "value" {
			err = r.valueType(n, key, value, typ)
		}
		if err == nil && (prop.Type == "field" || prop.Type == "fields") {
			err = r.boundFieldTypes(n, prop, value, values, typ)
		}
		if err != nil {
			r.issue(ptr(path+"/props", key), "render/prop", err, "use references and templates that resolve to the prop's declared type")
		}
	}
}

func (r *renderer) boundFieldTypes(n *spec.Node, prop *spec.Prop, value any, values map[string]any, typ string) error {
	def := r.bundle.App.Records[typ]
	if def == nil {
		return nil
	}
	fields := []string{}
	if prop.Type == "field" {
		fields = append(fields, value.(string))
	} else {
		fields = mustStrings(value)
	}
	key := "record"
	if n.Bind != nil && n.Bind.Records != "" {
		key = "row"
	}
	row, _ := values[key].(map[string]any)
	for _, name := range fields {
		field := def.Fields[name]
		if field == nil {
			continue
		}
		if err := fieldValue(field, row[name]); err != nil {
			return fmt.Errorf("%s.%s: %w", typ, name, err)
		}
	}
	return nil
}

func (r *renderer) valueType(n *spec.Node, key string, value any, typ string) error {
	if n.Part == "Toggle" && key == "on" {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("Toggle.on must resolve to bool, got %T", value)
		}
	}
	if key != "value" || value == nil {
		return nil
	}
	field, _ := n.Props["field"].(string)
	record := r.bundle.App.Records[typ]
	if record == nil || record.Fields[field] == nil {
		return nil
	}
	return fieldValue(record.Fields[field], value)
}

func fieldValue(field *spec.Field, value any) error {
	if value == nil {
		return nil
	}
	ok := true
	switch field.Type {
	case spec.FieldBool:
		_, ok = value.(bool)
	case spec.FieldNumber, spec.FieldMoney:
		switch value.(type) {
		case float64, int, int64:
		default:
			ok = false
		}
	case spec.FieldEnums, spec.FieldRefs, spec.FieldPeople, spec.FieldFiles:
		switch items := value.(type) {
		case []string:
		case []any:
			for _, item := range items {
				if _, text := item.(string); !text {
					ok = false
				}
			}
		default:
			ok = false
		}
	default:
		_, ok = value.(string)
	}
	if !ok {
		return fmt.Errorf("field value must have type %s, got %T", field.Type, value)
	}
	return nil
}

func clone(values map[string]any) map[string]any {
	copy := make(map[string]any, len(values))
	for name, value := range values {
		copy[name] = value
	}
	return copy
}

func (r *renderer) actions(n *spec.Node, path string, values map[string]any, typ string) {
	for _, event := range keys(n.On) {
		for i, action := range n.On[event] {
			at := fmt.Sprintf("%s/on/%s/%d", path, escape(event), i)
			data := actionValue(action)
			local := clone(values)
			input := map[string]any{}
			if ask, ok := data["ask"].(map[string]any); ok {
				for name, fieldType := range ask {
					input[name] = sampleInput(fmt.Sprint(fieldType))
				}
			}
			if n.Part == "Form" {
				if record := r.bundle.App.Records[typ]; record != nil {
					for name, field := range record.Fields {
						if _, ok := input[name]; !ok {
							input[name] = sampleInput(string(field.Type))
						}
					}
				}
			}
			local["input"] = input
			for _, key := range keys(data) {
				if key == "ask" {
					continue
				}
				// Confirmation templates may contain shell-owned count.
				local["count"] = int64(1)
				if _, err := resolveValue(data[key], local); err != nil {
					r.issue(ptr(at, key), "render/action", err, "reference a declared row, record, selection, parameter or input")
				}
			}
			r.actionTarget(action.Verb(), data, at, typ)
		}
	}
}

func sampleInput(typ string) any {
	switch typ {
	case "number", "money":
		return float64(1)
	case "bool":
		return true
	case "refs", "people", "enums", "files":
		return []string{"sample"}
	default:
		return "sample"
	}
}

func (r *renderer) actionTarget(verb string, data map[string]any, path, typ string) {
	switch verb {
	case spec.VerbOpen:
		id, _ := data[verb].(string)
		if r.bundle.Screens[id] == nil {
			r.issue(ptr(path, verb), "kit/bind", fmt.Errorf("unknown screen %q", id), "name a screens/<id>.json file")
		}
	case spec.VerbRunsStart:
		id, _ := data[verb].(string)
		if r.bundle.Runs[id] == nil {
			r.issue(ptr(path, verb), "kit/bind", fmt.Errorf("unknown run %q", id), "name a runs/<id>.yaml file")
		}
	case spec.VerbRecordsCreate, spec.VerbRecordsUpdate:
		args, _ := data[verb].(map[string]any)
		if target, ok := args["type"].(string); ok {
			typ = target
		}
		record := r.bundle.App.Records[typ]
		if record == nil {
			r.issue(ptr(path, verb), "kit/bind", fmt.Errorf("unknown record type %q", typ), "use a declared record type")
			return
		}
		set, _ := args["set"].(map[string]any)
		for _, field := range keys(set) {
			if record.Fields[field] == nil {
				r.issue(ptr(ptr(path, verb)+"/set", field), "kit/bind", fmt.Errorf("unknown field %q on %q", field, typ), "write only declared record fields")
			}
		}
	case spec.VerbAISort:
		list, _ := data[verb].(string)
		field, _ := data["field"].(string)
		record := r.bundle.App.Records[r.info.lists[list]]
		if record == nil || record.Fields[field] == nil {
			r.issue(ptr(path, verb), "kit/bind", fmt.Errorf("unknown list or AI field %q.%s", list, field), "name a list in this screen and a field on its record type")
		}
	case spec.VerbToolsCall:
		parts, _ := data[verb].([]any)
		if len(parts) < 2 {
			return
		}
		connector, _ := parts[0].(string)
		method, _ := parts[1].(string)
		if !contains(r.bundle.App.Capabilities.Connectors[connector].Can, method) {
			r.issue(ptr(path, verb), "kit/bind", fmt.Errorf("unknown connector method %s.%s", connector, method), "declare the connector and verb in app.json capabilities")
		}
	}
}
