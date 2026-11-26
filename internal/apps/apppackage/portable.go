package apppackage

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// withPortableRecords gives reused records enough shape to be created on
// an empty profile. Uses still advertises the existing binding when there is one.
func withPortableRecords(raw []byte, app schema.App, existing schema.TypeSet) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	var records map[string]map[string]json.RawMessage
	if err := json.Unmarshal(obj["records"], &records); err != nil {
		return nil, err
	}

	if records == nil {
		return nil, fmt.Errorf("app.json /records: the package needs a record declarations object")
	}

	queue := slices.Sorted(maps.Keys(records))
	// Start from the app's own alias choices. Writer field shapes may point
	// to a common type by its old name in the exporting space.
	aliases := map[string]string{}
	for _, alias := range queue {
		decl := app.Records[alias]
		target, ok := existing.ForRecord(alias, decl)
		if !ok {
			continue
		}
		if target.ID != "" && (decl.Uses || target.Owner == app.ID) {
			aliases[target.ID] = alias
		}
		if decl.Common == "" && target.Common != "" && (decl.Uses || target.Owner == app.ID) {
			records[alias]["common"], _ = json.Marshal(target.Common)
		}
	}
	addDependency := func(name string) (string, bool) {
		if _, declared := records[name]; declared {
			return name, true
		}
		target, ok := existing[name]
		if !ok {
			return "", false
		}
		if alias := aliases[target.ID]; alias != "" {
			return alias, true
		}
		records[name] = map[string]json.RawMessage{"uses": json.RawMessage("true"), "fields": json.RawMessage("{}")}
		if target.Common != "" {
			records[name]["common"], _ = json.Marshal(target.Common)
		}
		if target.ID != "" {
			aliases[target.ID] = name
		}
		app.Records[name] = schema.RecordType{Uses: true, Common: target.Common}
		queue = append(queue, name)
		return name, true
	}
	for _, name := range slices.Sorted(maps.Keys(app.Capabilities.Records)) {
		if _, ok := addDependency(name); !ok {
			return nil, fmt.Errorf("app.json /capabilities/records/%s: the type the package uses is unavailable", name)
		}
	}
	for i := 0; i < len(queue); i++ {
		alias := queue[i]
		record := records[alias]
		decl := app.Records[alias]
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(record["fields"], &fields); err != nil {
			return nil, err
		}
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		if decl.Uses {
			target, ok := existing.ForRecord(alias, decl)
			if !ok {
				return nil, fmt.Errorf("app.json /records/%s: the type the package uses is unavailable", alias)
			}
			for name, field := range target.Fields {
				if _, local := fields[name]; local {
					continue
				}
				// Only schema needed to recreate the field travels from
				// another app. Defaults and instructions stay in the source.
				shape := schema.Field{Type: field.Type, Label: field.Label, Class: field.Class,
					Masked: field.Masked, Required: field.Required, To: field.To, Currency: field.Currency, Values: field.Values}
				fields[name], _ = json.Marshal(shape)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			var field map[string]json.RawMessage
			if err := json.Unmarshal(fields[name], &field); err != nil {
				return nil, err
			}
			var to string
			if json.Unmarshal(field["to"], &to) != nil || to == "" {
				continue
			}
			bound, ok := addDependency(to)
			if !ok {
				return nil, fmt.Errorf("app.json /records/%s/fields/%s/to: the %s type is unavailable", alias, name, to)
			}
			if bound != to {
				field["to"], _ = json.Marshal(bound)
				fields[name], _ = json.Marshal(field)
			}
		}
		record["fields"], _ = json.Marshal(fields)
	}
	obj["records"], _ = json.Marshal(records)
	return json.MarshalIndent(obj, "", "  ")
}
