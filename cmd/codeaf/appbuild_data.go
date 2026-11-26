package main

import (
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
	catalog := builder.Catalog{Types: schema.TypeSet{}, Counts: map[string]int{}}
	shapes := main.TypeSet()
	for _, typ := range types {
		shape, ok := shapes[typ.Name]
		if !ok || typ.Hidden || (exclude != "" && shape.Owner == exclude) {
			continue
		}
		for _, field := range typ.Fields {
			if exclude != "" && field.App == exclude {
				delete(shape.Fields, field.Name)
			}
		}
		catalog.Types[typ.Name] = shape
		catalog.Counts[typ.Name] = typ.Count
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
