package appwire

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
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
		Screen      json.RawMessage               `json:"screen"`
		CustomParts map[string]schema.PartProgram `json:"custom_parts,omitempty"`
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
			raw, parts, err := b.ScreenWithParts(p.Draft, p.Screen)
			return &appScreenResult{Screen: raw, CustomParts: parts}, buildError(err)
		}
		defs, ok := e.(AppDefinitions)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "This engine does not serve app definitions.")
		}
		d, err := defs.AppDefinitions().Get(p.App)
		if err != nil {
			return nil, buildError(err)
		}
		raw, ok := d.Screen(p.Screen)
		if !ok {
			return nil, buildError(store.ErrNotFound)
		}
		parts, err := d.ScreenParts(raw)
		return &appScreenResult{Screen: raw, CustomParts: parts}, buildError(err)
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
	"apps.promote_part": appCall(func(s *store.Store, p struct {
		App string `json:"app"`
		ID  string `json:"id"`
	}) (*schema.Part, error) {
		part, err := s.PromotePart(p.App, p.ID)
		return &part, err
	}),
	"apps.kit_parts": appCall(func(s *store.Store, _ Empty) (*[]schema.Part, error) {
		parts, err := s.KitParts()
		if parts == nil {
			parts = []schema.Part{}
		}
		return &parts, err
	}),
	"apps.promote_step": appCall(func(s *store.Store, p struct {
		App string `json:"app"`
		ID  string `json:"id"`
	}) (*store.KitStep, error) {
		step, err := s.PromoteStep(p.App, p.ID)
		return &step, err
	}),
	"apps.kit_steps": appCall(func(s *store.Store, _ Empty) (*[]store.KitStep, error) {
		steps, err := s.KitSteps()
		if steps == nil {
			steps = []store.KitStep{}
		}
		return &steps, err
	}),
	"apps.kit": typed(func(e Engine, p appKitParams) (*json.RawMessage, error) {
		defs, ok := e.(AppDefinitions)
		if !ok {
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
		if version == 2 {
			// The base catalog stays available even if a local Kit receipt
			// is damaged. The explicit listing returns that receipt error.
			store := defs.AppDefinitions()
			parts, _ := store.KitParts()
			steps, _ := store.KitSteps()
			archives, err := apppackage.Builtins()
			if err != nil {
				return nil, err
			}
			templates := make([]apppackage.Manifest, 0, len(archives))
			for _, archive := range archives {
				templates = append(templates, archive.Manifest)
			}
			var catalog map[string]json.RawMessage
			if err := json.Unmarshal(raw, &catalog); err != nil {
				return nil, err
			}
			catalog["app_templates"], err = json.Marshal(templates)
			if err != nil {
				return nil, err
			}
			if len(parts) > 0 {
				catalog["custom_parts"], err = json.Marshal(parts)
				if err != nil {
					return nil, err
				}
			}
			if len(steps) > 0 {
				catalog["custom_steps"], err = json.Marshal(steps)
				if err != nil {
					return nil, err
				}
			}
			raw, err = json.Marshal(catalog)
			if err != nil {
				return nil, err
			}
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
