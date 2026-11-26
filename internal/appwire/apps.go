package appwire

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

// AppDefinitions is the optional engine door for personal app definitions.
// The store owns their validation and publication even when a caller is local.
type AppDefinitions interface {
	AppDefinitions() *store.Store
}

// AppRecordSchemas reads presentation schemas from the same scope as the rows.
type AppRecordSchemas interface {
	ResolvedAppRecords(DataScope, map[string]schema.RecordType) (map[string]schema.RecordType, error)
}

func resolveAppRecords(e Engine, raw json.RawMessage, draft string) (map[string]schema.RecordType, error) {
	reader, ok := e.(AppRecordSchemas)
	if !ok {
		return nil, nil
	}
	app, err := schema.ParseApp(raw)
	if err != nil {
		return nil, Errorf(CodeEngine, "This app definition could not be opened.")
	}
	for _, record := range app.Records {
		if record.Uses {
			return reader.ResolvedAppRecords(DataScope{App: app.ID, Draft: draft}, app.Records)
		}
	}
	return nil, nil
}

type (
	appKitParams struct {
		Kit *int `json:"kit,omitempty"`
	}
	appParams struct {
		App string `json:"app"`
	}
	appScreenParams struct {
		App    string `json:"app"`
		Screen string `json:"screen"`
		Draft  string `json:"draft,omitempty"`
	}
	appRevertParams struct {
		App string `json:"app"`
		N   int    `json:"n"`
	}
	appListResult struct {
		Apps   []store.Summary   `json:"apps"`
		Builds []builder.Summary `json:"builds"`
	}
	appScreenResult struct {
		Screen json.RawMessage `json:"screen"`
	}
	appVersionsResult struct {
		Versions []store.Version `json:"versions"`
	}
	appMetaResult struct {
		Meta store.Meta `json:"meta"`
	}
)

func init() {
	for name, m := range appMethods {
		methods[name] = m
	}
}

var appMethods = map[string]method{
	"apps.list": typed(func(e Engine, _ Empty) (*appListResult, error) {
		defs, ok := e.(AppDefinitions)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not serve app definitions.")
		}
		apps, err := defs.AppDefinitions().List()
		if err != nil {
			return nil, buildError(err)
		}
		result := &appListResult{Apps: apps, Builds: []builder.Summary{}}
		if _, ok := e.(AppBuilds); ok {
			b, err := openBuilds(e)
			if err != nil {
				return nil, err
			}
			result.Builds, err = b.List()
			if err != nil {
				return nil, buildError(err)
			}
		}
		return result, nil
	}),
	"apps.get": typed(func(e Engine, p appParams) (*store.Definition, error) {
		apps, ok := e.(AppDefinitions)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve app definitions")
		}
		d, err := apps.AppDefinitions().Get(p.App)
		if err != nil {
			return nil, appDefinitionError(err)
		}
		d.ResolvedRecords, err = resolveAppRecords(e, d.App, "")
		return &d, err
	}),
	"apps.screen": typed(func(e Engine, p appScreenParams) (*appScreenResult, error) {
		if p.Draft != "" {
			b, err := openBuilds(e)
			if err != nil {
				return nil, err
			}
			raw, err := b.Screen(p.Draft, p.Screen)
			return &appScreenResult{Screen: raw}, buildError(err)
		}
		defs, ok := e.(AppDefinitions)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not serve app definitions.")
		}
		raw, err := defs.AppDefinitions().Screen(p.App, p.Screen)
		return &appScreenResult{Screen: raw}, buildError(err)
	}),
	"apps.versions": appCall(func(s *store.Store, p appParams) (*appVersionsResult, error) {
		versions, err := s.Versions(p.App)
		return &appVersionsResult{Versions: versions}, err
	}),
	"apps.revert": appCall(func(s *store.Store, p appRevertParams) (*appMetaResult, error) {
		m, err := s.Revert(p.App, p.N)
		return &appMetaResult{Meta: m}, buildError(err)
	}),
	"apps.archive": appCall(func(s *store.Store, p appParams) (*appMetaResult, error) {
		m, err := s.Archive(p.App)
		return &appMetaResult{Meta: m}, err
	}),
	"apps.delete": appCall(func(s *store.Store, p appParams) (*Empty, error) {
		_, err := s.Delete(p.App)
		return done(err)
	}),
	"apps.kit": typed(func(e Engine, p appKitParams) (*json.RawMessage, error) {
		if _, ok := e.(AppDefinitions); !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve app definitions")
		}
		version := 2
		if p.Kit != nil {
			version = *p.Kit
		}
		raw := json.RawMessage(schema.KitJSONFor(version))
		if raw == nil {
			return nil, Errorf(CodeBadParams, "unknown app kit version %d", version)
		}
		return &raw, nil
	}),
}

func appCall[P, R any](call func(*store.Store, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		apps, ok := e.(AppDefinitions)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve app definitions")
		}
		r, err := call(apps.AppDefinitions(), p)
		return r, appDefinitionError(err)
	})
}

func appDefinitionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrBadInput):
		return Errorf(CodeBadParams, "The app, screen or version is invalid.")
	case errors.Is(err, store.ErrNotFound):
		return Errorf(CodeNotFound, "The app, screen or version could not be found.")
	case errors.Is(err, store.ErrConflict):
		return Errorf(CodeConflict, "This app changed since the draft was opened.")
	case errors.Is(err, store.ErrInvalid):
		return Errorf(CodeEngine, "This app definition could not be opened. Fix it or revert to a working version.")
	case errors.Is(err, store.ErrCorrupt):
		return Errorf(CodeEngine, "This app's metadata could not be read.")
	default:
		return Errorf(CodeEngine, "App definitions could not be accessed.")
	}
}
