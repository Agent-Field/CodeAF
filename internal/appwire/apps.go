package appwire

import (
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/apps/store"
)

// AppDefinitions is the optional engine door for personal app definitions.
// The store owns their validation and publication even when a caller is local.
type AppDefinitions interface {
	AppDefinitions() *store.Store
	AppKit() json.RawMessage
}

type (
	appParams struct {
		App string `json:"app"`
	}
	appScreenParams struct {
		App    string `json:"app"`
		Screen string `json:"screen"`
	}
	appRevertParams struct {
		App string `json:"app"`
		N   int    `json:"n"`
	}
	appListResult struct {
		Apps []store.Summary `json:"apps"`
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
	"apps.list": appCall(func(s *store.Store, _ Empty) (*appListResult, error) {
		apps, err := s.List()
		return &appListResult{Apps: apps}, err
	}),
	"apps.get": appCall(func(s *store.Store, p appParams) (*store.Definition, error) {
		d, err := s.Get(p.App)
		return &d, err
	}),
	"apps.screen": appCall(func(s *store.Store, p appScreenParams) (*appScreenResult, error) {
		raw, err := s.Screen(p.App, p.Screen)
		return &appScreenResult{Screen: raw}, err
	}),
	"apps.versions": appCall(func(s *store.Store, p appParams) (*appVersionsResult, error) {
		versions, err := s.Versions(p.App)
		return &appVersionsResult{Versions: versions}, err
	}),
	"apps.revert": appCall(func(s *store.Store, p appRevertParams) (*appMetaResult, error) {
		m, err := s.Revert(p.App, p.N)
		return &appMetaResult{Meta: m}, err
	}),
	"apps.archive": appCall(func(s *store.Store, p appParams) (*appMetaResult, error) {
		m, err := s.Archive(p.App)
		return &appMetaResult{Meta: m}, err
	}),
	"apps.delete": appCall(func(s *store.Store, p appParams) (*Empty, error) {
		_, err := s.Delete(p.App)
		return done(err)
	}),
	"apps.kit": typed(func(e Engine, _ Empty) (*json.RawMessage, error) {
		apps, ok := e.(AppDefinitions)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve app definitions")
		}
		raw := apps.AppKit()
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
		switch {
		case err == nil:
			return r, nil
		case errors.Is(err, store.ErrBadInput):
			return nil, Errorf(CodeBadParams, "The app, screen or version is invalid.")
		case errors.Is(err, store.ErrNotFound):
			return nil, Errorf(CodeNotFound, "The app, screen or version could not be found.")
		case errors.Is(err, store.ErrInvalid):
			return nil, Errorf(CodeEngine, "This app definition could not be opened. Fix it or revert to a working version.")
		case errors.Is(err, store.ErrCorrupt):
			return nil, Errorf(CodeEngine, "This app's metadata could not be read.")
		default:
			return nil, Errorf(CodeEngine, "App definitions could not be accessed.")
		}
	})
}
