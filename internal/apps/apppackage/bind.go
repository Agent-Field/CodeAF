package apppackage

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type Binding struct {
	Common string `json:"common,omitempty"`
	Type   string `json:"type"`
}

type Findings struct{ Report schema.Report }

func (e *Findings) Error() string { return e.Report.String() }

// Bind uses the same common-type catalog as the Builder. Keeping the
// original app alias means its screens, runs and record grants travel intact.
func (a Archive) Bind(existing schema.TypeSet, appID string, fork bool) (map[string]string, map[string]Binding, error) {
	var app map[string]json.RawMessage
	if err := json.Unmarshal(a.Files[schema.AppFile], &app); err != nil {
		return nil, nil, err
	}
	var records map[string]map[string]json.RawMessage
	if err := json.Unmarshal(app["records"], &records); err != nil {
		return nil, nil, err
	}
	bindings := map[string]Binding{}
	targets := map[string]schema.ExistingType{}
	var report schema.Report
	for _, alias := range slices.Sorted(maps.Keys(a.Manifest.Entities)) {
		entity := a.Manifest.Entities[alias]
		if entity.Common != "" {
			records[alias]["common"], _ = json.Marshal(entity.Common)
		}
		var name string
		if entity.Common != "" {
			if offer, ok := existing.OfferCommonType(alias, schema.RecordType{Common: entity.Common}); ok {
				name = offer.Existing
			}
		} else if target, ok := existing[alias]; ok {
			if entity.Uses || fork || samePrivateShape(records[alias], target) {
				name = alias
			}
		}
		if name != "" {
			targets[alias] = existing[name]
			if appID == "" || targets[alias].Owner != appID {
				bindings[alias] = Binding{Common: entity.Common, Type: name}
			}
		}
	}
	for _, alias := range slices.Sorted(maps.Keys(a.Manifest.Entities)) {
		entity := a.Manifest.Entities[alias]
		record := records[alias]
		binding, reuse := bindings[alias]
		if !reuse && !entity.Uses {
			continue
		}
		if reuse {
			record["uses"] = json.RawMessage("true")
		} else {
			delete(record, "uses")
			// A fallback is owned in this profile, but it remains a
			// shared dependency if the person exports this app again.
			record["portable_reuse"] = json.RawMessage("true")
		}
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(record["fields"], &fields); err != nil {
			return nil, nil, err
		}
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		for _, name := range slices.Sorted(maps.Keys(entity.Fields)) {
			field := entity.Fields[name]
			have, found := targets[alias].Fields[name]
			if reuse && found {
				if !compatible(field, have, targets, existing) {
					report.Problems = append(report.Problems, schema.Problem{File: schema.AppFile, Path: "/records/" + alias + "/fields/" + name,
						Code: "package_field_unmapped", Message: fmt.Sprintf("%s.%s does not match the package's %s field %s.%s.", binding.Type, name, field.Type, alias, name)})
				}
				delete(fields, name)
			} else if _, declared := fields[name]; !declared {
				// Older archives carry reused fields only in the manifest.
				fields[name], _ = json.Marshal(schema.Field{Type: field.Type, Required: field.Required, To: field.To, Currency: field.Currency, Values: field.Values})
			}
		}
		record["fields"], _ = json.Marshal(fields)
	}
	if !report.OK() {
		return nil, nil, &Findings{Report: report}
	}
	app["records"], _ = json.Marshal(records)
	// The receiving store stamps its own ID and version. Normalizing them
	// keeps publication numbers out of the three-way comparison.
	app["version"] = json.RawMessage("1")
	if appID != "" {
		app["id"], _ = json.Marshal(appID)
	}
	raw, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	files := make(map[string]string, len(a.Files))
	for path, content := range a.Files {
		files[path] = string(content)
	}
	files[schema.AppFile] = string(raw) + "\n"
	return files, bindings, nil
}

// A matching name alone does not make two private record types interchangeable.
func samePrivateShape(raw map[string]json.RawMessage, target schema.ExistingType) bool {
	if target.Common != "" || raw["policy"] != nil || raw["lifecycle"] != nil ||
		raw["ledger"] != nil || raw["freeze"] != nil || raw["constants"] != nil {
		return false
	}
	var fields map[string]schema.Field
	if json.Unmarshal(raw["fields"], &fields) != nil || len(fields) == 0 || len(fields) != len(target.Fields) {
		return false
	}
	for name, want := range fields {
		have, ok := target.Fields[name]
		want.ID, have.ID = "", ""
		if !ok || !reflect.DeepEqual(want, have) {
			return false
		}
	}
	return true
}

func compatible(need Field, have schema.Field, targets map[string]schema.ExistingType, existing schema.TypeSet) bool {
	if need.Type != have.Type || need.Currency != have.Currency ||
		!slices.Equal(need.Values, have.Values) {
		return false
	}
	return need.To == have.To || (targets[need.To].ID != "" && targets[need.To].ID == existing[have.To].ID)
}
