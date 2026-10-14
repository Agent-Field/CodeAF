package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// carrySample gives a first Keep the preview's sample records as its starting
// real records, so the owner opens an app they can judge instead of an empty
// one. Real records are never replaced: an app that already has records.db
// keeps it untouched, and a preview that was never seeded carries nothing. Rows keep their sample ids (`sample_<type>_NNNN` from
// the generator), which is how a later "clear sample data" tells them apart.
func (s *Service) carrySample(ctx context.Context, app string) error {
	if s.all == nil || s.all.Records == nil {
		return nil
	}
	dir := s.appDir(app)
	if _, err := os.Stat(filepath.Join(dir, "records.db")); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "sample.db")); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	def, err := s.Definition(ctx, app)
	if err != nil {
		return err
	}
	rows, err := s.sampleRows(ctx, app, def)
	if err != nil || len(rows) == 0 {
		return err
	}
	return s.adoptSample(ctx, app, rows)
}

// sampleRows reads every sample record through the records service, page by
// page, so what the owner saw in Preview, edits included, is what is kept.
func (s *Service) sampleRows(ctx context.Context, app string, def *spec.Bundle) (map[string][]bridge.Record, error) {
	if def == nil || def.App == nil {
		return nil, nil
	}
	scope := svc.Scope{App: app, Data: svc.DataSample, Caller: svc.Caller{Member: svc.Owner}}
	rows := map[string][]bridge.Record{}
	for typ := range def.App.Records {
		after := ""
		for {
			page, err := s.all.Records.Query(ctx, scope, bridge.QueryParams{Type: typ, After: after})
			if err != nil {
				return nil, err
			}
			if len(page.Rows) > 0 {
				rows[typ] = append(rows[typ], page.Rows...)
			}
			if after = page.Next; after == "" {
				break
			}
		}
	}
	return rows, nil
}

// adoptSample seeds rows through the records service, which owns the schema
// and its rules, then moves that closed database to records.db and reseeds a
// separate sample.db so later preview writes stay away from real content.
func (s *Service) adoptSample(ctx context.Context, app string, rows map[string][]bridge.Record) error {
	if err := s.all.Records.Seed(ctx, app, rows); err != nil {
		return err
	}
	dir := s.appDir(app)
	if err := os.Rename(filepath.Join(dir, "sample.db"), filepath.Join(dir, "records.db")); err != nil {
		return err
	}
	return s.all.Records.Seed(ctx, app, rows)
}
