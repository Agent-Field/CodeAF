package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type assistantRecordArgs struct {
	Type   string         `json:"type"`
	Query  string         `json:"query,omitempty"`
	Cursor string         `json:"cursor,omitempty"`
	Limit  int            `json:"limit,omitempty"`
	Group  string         `json:"group,omitempty"`
	ID     string         `json:"id,omitempty"`
	IDs    []string       `json:"ids,omitempty"`
	Set    map[string]any `json:"set,omitempty"`
	Field  string         `json:"field,omitempty"`
	Value  any            `json:"value,omitempty"`
}

// This is the data lane's boundary. The scope is supplied by the engine and
// never decoded from model arguments.
type appAssistantData interface {
	appAssistantRecordCall(operation, appID, actor string, args assistantRecordArgs) (any, error)
}

func (b *appAssistantBelt) records(app schema.App, data appAssistantData) {
	var read, write, sortTypes []string
	for name, mode := range app.Capabilities.Records {
		if mode == "r" || mode == "rw" {
			read = append(read, name)
		}
		if mode == "rw" {
			write = append(write, name)
			for _, field := range app.Records[name].Fields {
				if field.AI != "" && !field.Masked {
					sortTypes = append(sortTypes, name)
					break
				}
			}
		}
	}
	slices.Sort(read)
	slices.Sort(write)
	slices.Sort(sortTypes)
	const (
		query = `"query":{"type":"string"}`
		page  = query + `,"cursor":{"type":"string"},"limit":{"type":"integer"}`
		ids   = `"ids":{"type":"array","items":{"type":"string"}}`
		set   = `"set":{"type":"object"}`
	)
	for _, spec := range []struct {
		op, description, args string
		types, required       []string
		writes                bool
		ai                    string
	}{
		{"query", "Find records with query tokens, or search by name with plain words. Results include IDs and a cursor.", page, read, nil, false, ""},
		{"count", "Count matching records grouped by one field.", query + `,"group":{"type":"string"}`, read, []string{"group"}, false, ""},
		{"get", "Read one record by ID.", `"id":{"type":"string"}`, read, []string{"id"}, false, ""},
		{"create", "Create one record with the supplied fields.", set, write, []string{"set"}, true, ""},
		{"update", "Set fields on the named record IDs. Read the records first if you need their IDs.", ids + "," + set, write, []string{"ids", "set"}, true, ""},
		{"delete", "Delete the named record IDs when the person asks.", ids, write, []string{"ids"}, true, ""},
		{"summarise", "Read a page of matching records for an AI summary. Base the summary on the results.", page, read, nil, false, "summarise"},
		{"sort", "Assign a value to an AI field for matching record IDs, keeping pinned values.", ids + `,"field":{"type":"string"},"value":{}`, sortTypes, []string{"ids", "field", "value"}, true, "sort"},
	} {
		if len(spec.types) == 0 || spec.ai != "" && !slices.Contains(app.Capabilities.AI, spec.ai) {
			continue
		}
		name := "data_" + spec.op
		if spec.ai != "" {
			name = "ai_" + spec.ai
		}
		b.add(name, spec.description, false, `{"type":%s,`+spec.args+"}", append([]string{"type"}, spec.required...), appEnum(spec.types),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var args assistantRecordArgs
				if err := appAssistantDecode(raw, &args); err != nil {
					return nil, err
				}
				live, _, err := b.load()
				if err != nil {
					return nil, err
				}
				mode := live.Capabilities.Records[args.Type]
				if !slices.Contains(spec.types, args.Type) || (mode != "rw" && (spec.writes || mode != "r")) {
					return nil, fmt.Errorf("This app cannot %s records of type %q.", spec.op, args.Type)
				}
				if spec.ai != "" && !slices.Contains(live.Capabilities.AI, spec.ai) {
					return nil, fmt.Errorf("This app cannot use AI to %s records.", spec.ai)
				}
				if spec.ai == "sort" {
					field := live.Records[args.Type].Fields[args.Field]
					if field.AI == "" || field.Masked {
						return nil, errors.New("That field cannot be filled by AI.")
					}
				}
				return data.appAssistantRecordCall(spec.op, b.scope.ID, live.Name+" agent", args)
			})
	}
}
