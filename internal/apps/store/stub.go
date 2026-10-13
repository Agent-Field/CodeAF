package store

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Service is the walking-skeleton store: it serves the golden CRM as a kept
// v1 app and nothing else. B1 replaces this file with the folder + git store.
type Service struct {
	crm *spec.Bundle
	at  string
}

var _ svc.Store = (*Service)(nil)

// New returns the fixture-backed store.
func New(svc.Config, *svc.Services) *Service {
	files, _ := spec.GoldenCRM()
	bundle, err := spec.Decode(files)
	if err != nil {
		panic("golden CRM does not decode: " + err.Error())
	}
	return &Service{crm: bundle, at: time.Now().UTC().Format(time.RFC3339)}
}

func (s *Service) summary() bridge.AppSummary {
	app := s.crm.App
	return bridge.AppSummary{ID: app.ID, Name: app.Name, Description: app.Description, State: bridge.StateKept, Version: 1, Space: "personal", UpdatedAt: s.at}
}

// List returns the golden CRM.
func (s *Service) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	return []bridge.AppSummary{s.summary()}, nil
}

// Get returns the golden CRM with its files.
func (s *Service) Get(_ context.Context, app string) (bridge.AppDetail, error) {
	if app != s.crm.App.ID {
		return bridge.AppDetail{}, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	files := make(map[string]string, len(s.crm.Files))
	for name, data := range s.crm.Files {
		files[name] = string(data)
	}
	return bridge.AppDetail{AppSummary: s.summary(), Files: files, Needs: []bridge.NeedLine{}}, nil
}

// Definition returns the golden CRM bundle.
func (s *Service) Definition(_ context.Context, app string) (*spec.Bundle, error) {
	if app != s.crm.App.ID {
		return nil, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return s.crm, nil
}

func (s *Service) Create(context.Context, svc.Caller, string, string) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("store")
}

func (s *Service) WriteFiles(context.Context, string, spec.Files) error {
	return bridge.NotBuilt("store")
}

func (s *Service) SetState(context.Context, string, bridge.AppState) error {
	return bridge.NotBuilt("store")
}

func (s *Service) Keep(context.Context, string) (bridge.AppSummary, error) {
	return bridge.AppSummary{}, bridge.NotBuilt("store")
}

func (s *Service) Discard(context.Context, string) error { return bridge.NotBuilt("store") }
