package store

import (
	"context"
	"encoding/json"
	"fmt"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// CommitShape compares the live tag while holding the store lock, so two
// callers cannot both publish a successor to the same definition.
func (s *Service) CommitShape(ctx context.Context, app string, base int, files spec.Files, requests []string, member string) (bridge.AppSummary, error) {
	if err := s.start(ctx); err != nil {
		return bridge.AppSummary{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if m.State != bridge.StateKept || m.Version != base {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "the app version changed; replay your draft")
	}
	if member == "" || strings.ContainsAny(member, "\r\n") {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeInvalid, "a member is required")
	}
	if err := validatePaths(files); err != nil {
		return bridge.AppSummary{}, err
	}
	m.Version++
	files, err = versionIdentity(files, app, m.Version)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	validation, err := ShapeValidationFiles(files)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if issues := spec.Validate(validation); len(issues) > 0 {
		return bridge.AppSummary{}, &bridge.Error{Code: bridge.CodeCheckFailed, Message: "app definition is invalid", Details: map[string]any{"issues": issues}}
	}
	if err := definitionSummary(&m, files); err != nil {
		return bridge.AppSummary{}, err
	}
	request := strings.Join(requests, "\n")
	author := m
	author.Maker = member
	err = s.changeDefinition(ctx, &m, func(dir string) error {
		if err := replaceFiles(dir, files); err != nil {
			return err
		}
		return s.shapeTag(dir, &author, request, requests)
	})
	if err != nil {
		return bridge.AppSummary{}, err
	}
	s.publish(m.AppSummary)
	return m.AppSummary, nil
}

// ShapeValidationFiles validates the ordinary field contract separately from
// rename annotations until the shared schema gains its wave-2 alias property.
func ShapeValidationFiles(files spec.Files) (spec.Files, error) {
	var app map[string]any
	if err := json.Unmarshal(files[spec.AppFile], &app); err != nil {
		return nil, err
	}
	types, _ := app["records"].(map[string]any)
	for _, value := range types {
		typ, _ := value.(map[string]any)
		fields, _ := typ["fields"].(map[string]any)
		for _, value := range fields {
			field, _ := value.(map[string]any)
			if alias, exists := field["alias"]; exists {
				text, ok := alias.(string)
				if !ok || !validID(text) {
					return nil, bridge.Errorf(bridge.CodeInvalid, "field alias must be a field name")
				}
				if fields[text] != nil {
					return nil, bridge.Errorf(bridge.CodeInvalid, "alias names a live field")
				}
				old := make(map[string]any, len(field))
				for name, value := range field {
					if name != "alias" {
						old[name] = value
					}
				}
				delete(old, "required")
				delete(old, "unique")
				delete(old, "computed")
				fields[text] = old
				delete(field, "alias")
			}
		}
	}
	data, err := json.Marshal(app)
	copy := make(spec.Files, len(files))
	for name, data := range files {
		copy[name] = data
	}
	copy[spec.AppFile] = data
	return copy, err
}

// The candidate repository is still private here. Add structured requests to
// its new commit before changeDefinition publishes any of the candidate tags.
func (s *Service) shapeTag(dir string, m *metadata, request string, requests []string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return err
	}
	if err := commitTag(repo, m, "Shape", request, s.now()); err != nil {
		return err
	}
	commit, err := taggedCommit(repo, m.Version)
	if err != nil {
		return err
	}
	data, err := json.Marshal(requests)
	if err != nil {
		return err
	}
	commit.Message += "\nShape-Requests: " + string(data) + "\n"
	encoded := repo.Storer.NewEncodedObject()
	if err := commit.Encode(encoded); err != nil {
		return err
	}
	hash, err := repo.Storer.SetEncodedObject(encoded)
	if err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/"+versionTag(m.Version)), hash)); err != nil {
		return err
	}
	return repo.Storer.SetReference(plumbing.NewHashReference(liveBranch, hash))
}

// VersionRequests retains request boundaries, including paragraphs with
// newlines. Older Keep/Restore commits carry exactly one request.
func (s *Service) VersionRequests(ctx context.Context, app string, version int) ([]string, error) {
	if err := s.start(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.lookup(ctx, app); err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(s.definitionDir(app))
	if err != nil {
		return nil, err
	}
	commit, err := taggedCommit(repo, version)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(commit.Message, "\n") {
		if text, ok := strings.CutPrefix(line, "Shape-Requests: "); ok {
			var requests []string
			if err := json.Unmarshal([]byte(text), &requests); err != nil {
				return nil, fmt.Errorf("version request metadata: %w", err)
			}
			return requests, nil
		}
	}
	request := commitRequest(commit.Message)
	if request == "" {
		return []string{}, nil
	}
	return []string{request}, nil
}
