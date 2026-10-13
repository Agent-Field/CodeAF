// Package imports maps one-time source tables into app records without changing definitions.
package imports

import (
	"context"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/query"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/google/uuid"
)

type Service struct {
	cfg    svc.Config
	all    *svc.Services
	mu     sync.Mutex
	saved  map[string]*receipt
	spaces Spaces
}

var _ svc.Imports = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	return &Service{cfg: cfg, all: all, saved: map[string]*receipt{}, spaces: defaultSpaces(all)}
}

// An unconfigured service has no app store to read or records to mutate.
func (s *Service) ready(method string) error {
	if s.all == nil || s.all.Store == nil || s.all.Records == nil {
		return bridge.NotBuilt(method)
	}
	return nil
}

func invalid(message string) error { return bridge.Errorf(bridge.CodeInvalid, message) }
func reason(code bridge.ErrorCode, why, message string) error {
	return &bridge.Error{Code: code, Message: message, Details: map[string]any{"reason": why}}
}

func (s *Service) definition(ctx context.Context, scope svc.Scope, typ string) (*spec.App, error) {
	if !query.ValidName(scope.App) {
		return nil, invalid("invalid app id")
	}
	def, err := s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return nil, err
	}
	if def == nil || def.App == nil || def.App.Records[typ] == nil {
		return nil, invalid("unknown record type " + typ)
	}
	return def.App, nil
}

func (s *Service) Sources(ctx context.Context, scope svc.Scope, p bridge.ImportSourcesParams) (bridge.ImportSourcesResult, error) {
	if err := s.ready(bridge.MethodImportsSources); err != nil {
		return bridge.ImportSourcesResult{}, err
	}
	if _, err := s.definition(ctx, scope, p.Type); err != nil {
		return bridge.ImportSourcesResult{}, err
	}
	sources := []bridge.ImportSource{{Kind: "csv", Label: "CSV file", Connected: true, Detail: "drop a .csv or choose…"}}
	if s.all.Connect.Grants == nil {
		return bridge.ImportSourcesResult{Sources: sources}, nil
	}
	accounts, err := s.all.Connect.Grants.Accounts(ctx, scope.Caller)
	if err != nil {
		return bridge.ImportSourcesResult{}, err
	}
	for _, candidate := range []bridge.ImportSource{
		{Kind: "airtable", Label: "Airtable", Detail: "a base and table · your Airtable connection"},
		{Kind: "hubspot", Label: "HubSpot", Detail: "deals · your HubSpot connection"},
	} {
		for _, connection := range accounts.Connections {
			if connection.Service == candidate.Kind && connection.Status == "connected" {
				candidate.Connected = true
				sources = append(sources, candidate)
				break
			}
		}
	}
	return bridge.ImportSourcesResult{Sources: sources}, nil
}

func (s *Service) Preview(ctx context.Context, scope svc.Scope, p bridge.ImportPreviewParams) (bridge.ImportPreview, error) {
	if err := s.ready(bridge.MethodImportsPreview); err != nil {
		return bridge.ImportPreview{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	app, err := s.definition(ctx, scope, p.Type)
	if err != nil {
		return bridge.ImportPreview{}, err
	}
	reader, err := s.reader(scope, p.Source)
	if err != nil {
		return bridge.ImportPreview{}, err
	}
	columns, rows, err := reader.Rows(ctx)
	if err != nil {
		return bridge.ImportPreview{}, err
	}
	if err := validTable(columns, rows); err != nil {
		return bridge.ImportPreview{}, err
	}
	r := &receipt{ID: uuid.NewString(), App: scope.App, Member: scope.Caller.Member, Type: p.Type, Source: p.Source.Kind, Columns: columns, Rows: rows, Mapping: p.Mapping, Status: "preview"}
	plan, err := s.evaluate(ctx, scope, app, r)
	if err != nil {
		return bridge.ImportPreview{}, err
	}
	r.Mapping = plan.mapping
	if err := s.save(r); err != nil {
		return bridge.ImportPreview{}, err
	}
	return plan.preview, nil
}

func validTable(columns []string, rows [][]string) error {
	if len(columns) == 0 {
		return invalid("source needs a header row")
	}
	seen := map[string]bool{}
	for _, column := range columns {
		if column == "" || seen[column] {
			return invalid("source columns must be named and distinct")
		}
		seen[column] = true
	}
	for _, row := range rows {
		if len(row) != len(columns) {
			return invalid("source row width differs from header")
		}
	}
	return nil
}
