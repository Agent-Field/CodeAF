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
	catalog := builder.Catalog{Types: schema.TypeSet{}, Counts: map[string]int{}}
	for _, typ := range types {
		if typ.Hidden || typ.DefinedBy == exclude {
			continue
		}
		shape := schema.ExistingType{Owner: typ.DefinedBy, Fields: map[string]schema.Field{}}
		for _, field := range typ.Fields {
			if !field.Hidden && field.App != exclude {
				shape.Fields[field.Name] = field.FieldDef.Field
			}
		}
		catalog.Types[typ.Name] = shape
		catalog.Counts[typ.Name] = typ.Count
	}
	return catalog, nil
}

func (b *appBackend) appBuildTypes(id string) (schema.TypeSet, error) {
	catalog, err := b.appBuildCatalog(id)
	return catalog.Types, err
}

func (b *appBackend) seedBuildSample(draft string, app []byte, rows map[string][]map[string]any) error {
	place, err := b.appData()
	if err != nil {
		return err
	}
	return place.SeedSample(draft, app, rows)
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

func (b *appBackend) registerBuiltApp(raw json.RawMessage) error {
	main, err := b.dataStore("")
	if err != nil {
		return err
	}
	return main.RegisterApp(raw)
}

// RegisterAppDefinition synchronizes the record registry when a working
// definition is copied forward by Revert.
func (b *appBackend) RegisterAppDefinition(id string) error {
	app, err := b.AppDefinitions().Get(id)
	if err != nil {
		return err
	}
	return b.registerBuiltApp(app.App)
}

// The data lane checks its own record declarations before a draft includes
// them. Live registration still happens only during Apply.
func validateAppBuilderTypes(raw []byte) error {
	_, err := data.ParseApp(raw)
	return err
}
