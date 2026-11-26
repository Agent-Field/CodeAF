package main

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func (b *appBackend) appBuildCatalog(exclude string) (builder.Catalog, error) {
	main, err := b.dataStore("")
	if err != nil {
		return builder.Catalog{}, err
	}
	types, err := main.Types(data.Scope{})
	if err != nil {
		return builder.Catalog{}, err
	}
	catalog := builder.Catalog{
		Types: main.TypeSetFor(exclude), Counts: map[string]int{},
		TypeLabels: map[string]string{}, AppNames: map[string]string{}, Origins: map[string]string{},
	}
	for _, typ := range types {
		shape, ok := catalog.Types[typ.Name]
		if !ok {
			continue
		}
		catalog.Counts[typ.Name] = typ.Count
		catalog.TypeLabels[typ.Name] = typ.Label
		catalog.Origins[typ.Name] = typ.CreatedByApp
		source := shape.Owner
		if source == "" {
			source = typ.CreatedByApp
		}
		if _, read := catalog.AppNames[source]; read || source == "" {
			continue
		}
		published, _, err := main.Publication(source)
		if err != nil {
			return builder.Catalog{}, err
		}
		var meta struct{ Name string }
		_ = json.Unmarshal(published.Metadata, &meta)
		catalog.AppNames[source] = meta.Name
	}
	if exclude != "" {
		if sources, err := b.appConnections().List(exclude); err == nil {
			catalog.SourceOperations = map[string][]string{}
			for _, source := range sources {
				if len(source.Writes) != 0 {
					catalog.SourceOperations["connections."+source.ID] = source.Writes
				}
			}
		}
	}
	return catalog, nil
}

func (b *appBackend) seedBuildSample(draft string, app []byte, rows map[string][]map[string]any) error {
	place, err := b.appData()
	if err != nil {
		return err
	}
	err = place.SeedSample(draft, app, rows)
	if refused, ok := data.AsError(err); ok {
		return &check.SampleProblem{Detail: refused.Message}
	}
	return err
}

func (b *appBackend) seedBuildPreview(scope string, def schema.Definition, real bool) error {
	app := def.Files[schema.AppFile]
	place, err := b.appData()
	if err != nil {
		return err
	}
	if real {
		// A record snapshot is no longer an authoring scope. The data
		// lane will provide the person's separate read-only projection.
		return appwire.Errorf(appwire.CodeForbidden, "This preview uses example records.")
	}
	rows, err := check.Sample(def)
	if err != nil {
		return err
	}
	return place.SeedSample(scope, app, rows)
}

func (b *appBackend) closeBuildPreview(scope string) {
	if place, err := b.appData(); err == nil {
		_ = place.CloseDraft(scope)
	}
}

// The data lane checks its own record declarations before a draft includes
// them. Live registration still happens only during Apply.
func validateAppBuilderTypes(raw []byte) error {
	_, err := data.ParseApp(raw)
	return err
}
