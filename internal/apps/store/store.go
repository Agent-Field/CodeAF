package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// These lifecycle states stay local until the wave-2 bridge exposes them.
const (
	StateArchived  bridge.AppState = "archived"
	StateDiscarded bridge.AppState = "discarded"
	StateDeleted   bridge.AppState = "deleted"
)

const metadataFile = "state.json"

// Service owns the definition repository and registry metadata, never records.
type Service struct {
	mu      sync.Mutex
	startMu sync.Mutex
	started bool
	demo    bool
	root    string
	all     *svc.Services
	now     func() time.Time
}

type metadata struct {
	bridge.AppSummary
	Maker     string    `json:"maker"`
	Request   string    `json:"request"`
	DeletedAt time.Time `json:"deleted_at,omitempty"`
}

var _ svc.Store = (*Service)(nil)

func New(cfg svc.Config, all *svc.Services) *Service {
	return NewWithOptions(cfg, all, Options{})
}

func (s *Service) List(ctx context.Context, caller svc.Caller) ([]bridge.AppSummary, error) {
	if err := s.start(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return []bridge.AppSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	apps := []bridge.AppSummary{}
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		m, err := s.load(entry.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if m.Maker == caller.Member && m.State != StateDeleted {
			apps = append(apps, m.AppSummary)
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	return apps, nil
}

func (s *Service) Get(ctx context.Context, app string) (bridge.AppDetail, error) {
	if err := s.start(ctx); err != nil {
		return bridge.AppDetail{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil {
		return bridge.AppDetail{}, err
	}
	files, err := s.files(m)
	if err != nil {
		return bridge.AppDetail{}, err
	}
	return detail(m.AppSummary, files), nil
}

func (s *Service) Definition(ctx context.Context, app string) (*spec.Bundle, error) {
	if err := s.start(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil {
		return nil, err
	}
	files, err := s.files(m)
	if err != nil {
		return nil, err
	}
	return spec.Decode(files)
}

// Create reserves a slug-shaped id; a collision gets a numeric suffix.
func (s *Service) Create(ctx context.Context, caller svc.Caller, name, description string) (bridge.AppSummary, error) {
	if err := s.start(ctx); err != nil {
		return bridge.AppSummary{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return bridge.AppSummary{}, err
	}
	if caller.Member != svc.Owner || caller.Agent {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeDenied, "only the owner may create a personal app")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeInvalid, "app name is required")
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return bridge.AppSummary{}, err
	}
	id, err := s.allocate(slug(name))
	if err != nil {
		return bridge.AppSummary{}, err
	}
	dir := s.appDir(id)
	m := metadata{AppSummary: bridge.AppSummary{ID: id, Name: name, Description: description, State: bridge.StatePlanning, Space: "personal"}, Maker: caller.Member, Request: description}
	err = s.initialize(dir, &m)
	if err != nil {
		return bridge.AppSummary{}, errors.Join(err, os.RemoveAll(dir))
	}
	s.publish(m.AppSummary)
	return m.AppSummary, nil
}

func (s *Service) initialize(dir string, m *metadata) error {
	repo, err := git.PlainInit(filepath.Join(dir, "definition"), false)
	if err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, draftBranch)); err != nil {
		return err
	}
	return s.save(m)
}

// WriteFiles replaces the complete draft definition, including removed files.
// Check owns validation so an unfinished plan and a failing build can be saved.
func (s *Service) WriteFiles(ctx context.Context, app string, files spec.Files) error {
	if err := s.start(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil {
		return err
	}
	if !draftState(m.State) {
		return bridge.Errorf(bridge.CodeConflict, "a kept app's definition cannot be written")
	}
	if err := validatePaths(files); err != nil {
		return err
	}
	// The registry owns identity even when the Builder reuses an example app.
	var appDefinition map[string]json.RawMessage
	if data, ok := files[spec.AppFile]; ok && json.Unmarshal(data, &appDefinition) == nil && appDefinition != nil {
		files, err = versionIdentity(files, m.ID, 0)
		if err != nil {
			return err
		}
		var labels struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if json.Unmarshal(files[spec.AppFile], &labels) == nil && labels.Name != "" {
			m.Name, m.Description = labels.Name, labels.Description
		}
	}
	return s.changeDefinition(ctx, &m, func(dir string) error {
		return replaceFiles(dir, files)
	})
}

func (s *Service) SetState(ctx context.Context, app string, state bridge.AppState) error {
	if err := s.start(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil || m.State == state {
		return err
	}
	if !transition(m.State, state) {
		return bridge.Errorf(bridge.CodeConflict, fmt.Sprintf("cannot change app from %s to %s", m.State, state))
	}
	m.State = state
	if err := s.save(&m); err != nil {
		return err
	}
	s.publish(m.AppSummary)
	return nil
}

func transition(from, to bridge.AppState) bool {
	switch from {
	case bridge.StatePlanning:
		return to == bridge.StateBuilding
	case bridge.StateBuilding:
		return to == bridge.StatePreview || to == bridge.StateFailed || to == bridge.StatePlanning
	case bridge.StateFailed, bridge.StatePreview:
		return to == bridge.StateBuilding || to == bridge.StatePlanning
	case bridge.StateKept:
		return to == StateArchived
	case StateArchived:
		return to == bridge.StateKept
	}
	return false
}

func draftState(state bridge.AppState) bool {
	return state == bridge.StatePlanning || state == bridge.StateBuilding || state == bridge.StateFailed || state == bridge.StatePreview
}

func (s *Service) Discard(ctx context.Context, app string) error {
	if err := s.start(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil {
		return err
	}
	if !draftState(m.State) {
		return bridge.Errorf(bridge.CodeConflict, "only an unkept app can be discarded")
	}
	if err := os.RemoveAll(s.appDir(app)); err != nil {
		return err
	}
	m.State = StateDiscarded
	m.UpdatedAt = s.timestamp()
	s.publish(m.AppSummary)
	return nil
}

func (s *Service) Archive(ctx context.Context, app string) error {
	return s.SetState(ctx, app, StateArchived)
}

func (s *Service) Unarchive(ctx context.Context, app string) error {
	return s.SetState(ctx, app, bridge.StateKept)
}

func (s *Service) files(m metadata) (spec.Files, error) {
	dir := s.definitionDir(m.ID)
	if m.Version > 0 {
		return versionFiles(dir, m.Version)
	}
	return readFiles(dir)
}

func detail(summary bridge.AppSummary, files spec.Files) bridge.AppDetail {
	texts := make(map[string]string, len(files))
	for name, data := range files {
		texts[name] = string(data)
	}
	return bridge.AppDetail{AppSummary: summary, Files: texts, Needs: []bridge.NeedLine{}}
}

func (s *Service) publish(summary bridge.AppSummary) {
	if s.all != nil && s.all.Hub != nil {
		s.all.Hub.Publish(bridge.TopicState(summary.ID), summary)
	}
}

func (s *Service) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.root == "" {
		return bridge.Errorf(bridge.CodeInvalid, "app store root is required")
	}
	return nil
}

func (s *Service) lookup(ctx context.Context, app string) (metadata, error) {
	if err := s.ready(ctx); err != nil {
		return metadata{}, err
	}
	if !validID(app) {
		return metadata{}, bridge.Errorf(bridge.CodeInvalid, "invalid app id")
	}
	m, err := s.load(app)
	if errors.Is(err, os.ErrNotExist) || (err == nil && m.State == StateDeleted) {
		return metadata{}, bridge.Errorf(bridge.CodeNotFound, "no app "+app)
	}
	return m, err
}

func (s *Service) timestamp() string               { return s.now().UTC().Format(time.RFC3339Nano) }
func (s *Service) appDir(app string) string        { return filepath.Join(s.root, app) }
func (s *Service) definitionDir(app string) string { return filepath.Join(s.appDir(app), "definition") }
