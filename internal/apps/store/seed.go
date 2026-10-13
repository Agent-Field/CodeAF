package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Options lets callers create an empty registry for isolated builds and tests.
type Options struct {
	WithoutDemo bool
}

func NewWithOptions(cfg svc.Config, all *svc.Services, opts Options) *Service {
	return &Service{root: cfg.Root, all: all, now: time.Now, demo: !opts.WithoutDemo}
}

type seedContextKey struct{}

// Startup waits until peers are wired; records seeding may read the definition
// through the same context without starting the registry a second time.
func (s *Service) start(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if ctx.Value(seedContextKey{}) == s {
		return nil
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.started {
		return nil
	}
	if s.demo {
		if err := s.seedGoldenCRM(ctx); err != nil {
			return err
		}
	}
	s.started = true
	return nil
}

// SeedGoldenCRM installs the golden definition as kept v1 and seeds its sample
// records through the records service. A nonempty registry is left alone.
func (s *Service) SeedGoldenCRM(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if err := s.seedGoldenCRM(ctx); err != nil {
		return err
	}
	s.started = true
	return nil
}

func (s *Service) seedGoldenCRM(ctx context.Context) error {
	ctx = context.WithValue(ctx, seedContextKey{}, s)
	s.mu.Lock()
	m, err := s.installGoldenCRM(ctx)
	s.mu.Unlock()
	if err != nil || m == nil {
		return err
	}
	if s.all != nil && s.all.Records != nil {
		_, sample := spec.GoldenCRM()
		var rows map[string][]bridge.Record
		err = json.Unmarshal(sample, &rows)
		if err == nil {
			err = s.seedDemoRecords(ctx, m.ID, rows)
		}
		if err != nil {
			s.mu.Lock()
			err = errors.Join(err, os.RemoveAll(s.appDir(m.ID)))
			s.mu.Unlock()
			return err
		}
	}
	s.publish(m.AppSummary)
	return nil
}

func (s *Service) seedDemoRecords(ctx context.Context, app string, rows map[string][]bridge.Record) error {
	if err := s.all.Records.Seed(ctx, app, rows); err != nil {
		return err
	}
	// Seed owns the schema and closes its database before returning. Only the
	// first-run demo adopts that database as real content; ordinary Keep does not.
	dir := s.appDir(app)
	if err := os.Rename(filepath.Join(dir, "sample.db"), filepath.Join(dir, "records.db")); err != nil {
		return err
	}
	// A separate sample database keeps preview writes away from demo content.
	return s.all.Records.Seed(ctx, app, rows)
}

func (s *Service) installGoldenCRM(ctx context.Context) (*metadata, error) {
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil || len(entries) != 0 {
		return nil, err
	}
	files, _ := spec.GoldenCRM()
	var plan spec.Plan
	if err := json.Unmarshal(files[spec.PlanFile], &plan); err != nil {
		return nil, err
	}
	m := &metadata{AppSummary: bridge.AppSummary{ID: "crm", State: bridge.StateKept, Space: "personal", Version: 1}, Maker: svc.Owner, Request: plan.Ask}
	dir := s.appDir(m.ID)
	if err := s.initialize(dir, m); err != nil {
		return nil, errors.Join(err, os.RemoveAll(dir))
	}
	if err := s.commitVersion(ctx, m, files, "Keep", m.Request); err != nil {
		return nil, errors.Join(err, os.RemoveAll(dir))
	}
	return m, nil
}
