package apppackage

import (
	"encoding/json"
	"fmt"
	"maps"
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
		if offer, ok := existing.OfferCommonType(alias, schema.RecordType{Common: entity.Common}); entity.Common != "" && ok && offer.Existing != "" {
			targets[alias] = existing[offer.Existing]
			if appID == "" || targets[alias].Owner != appID || entity.Uses {
				bindings[alias] = Binding{Common: offer.Key, Type: offer.Existing}
			}
		} else if entity.Common == "" && (entity.Uses || fork) {
			if target, ok := existing[alias]; ok {
				targets[alias] = target
				bindings[alias] = Binding{Type: alias}
			}
		}
		if entity.Uses && bindings[alias].Type == "" {
			report.Problems = append(report.Problems, schema.Problem{File: schema.AppFile, Path: "/records/" + alias,
				Code: "package_type_unmapped", Message: fmt.Sprintf("Map the package's %s records to an existing type (%s).", alias, entity.Common)})
		}
	}
	for _, alias := range slices.Sorted(maps.Keys(bindings)) {
		entity := a.Manifest.Entities[alias]
		target := targets[alias]
		record := records[alias]
		record["uses"] = json.RawMessage("true")
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(record["fields"], &fields); err != nil {
			return nil, nil, err
		}
		for _, name := range slices.Sorted(maps.Keys(entity.Fields)) {
			field := entity.Fields[name]
			have, found := target.Fields[name]
			var message string
			switch {
			case !found && field.Required:
				message = fmt.Sprintf("%s records need a %s field named %s to use the package's %s.%s.", bindings[alias].Type, field.Type, name, alias, name)
			case found && !compatible(field, have, targets, existing):
				message = fmt.Sprintf("%s.%s does not match the package's %s field %s.%s.", bindings[alias].Type, name, field.Type, alias, name)
			}
			if message != "" {
				report.Problems = append(report.Problems, schema.Problem{File: schema.AppFile, Path: "/records/" + alias + "/fields/" + name,
					Code: "package_field_unmapped", Message: message})
			}
			if found {
				delete(fields, name)
			}
		}
		if fields == nil {
			fields = map[string]json.RawMessage{}
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

func compatible(need Field, have schema.Field, targets map[string]schema.ExistingType, existing schema.TypeSet) bool {
	if need.Type != have.Type || need.Currency != have.Currency ||
		!slices.Equal(need.Values, have.Values) {
		return false
	}
	return need.To == have.To || (targets[need.To].ID != "" && targets[need.To].ID == existing[have.To].ID)
}
