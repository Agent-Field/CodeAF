package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	draftBranch plumbing.ReferenceName = "refs/heads/draft/me"
	liveBranch  plumbing.ReferenceName = "refs/heads/main"
)

// Version identifies an immutable definition and the request that made it.
type Version struct {
	Version int    `json:"version"`
	Tag     string `json:"tag"`
	Commit  string `json:"commit"`
	Request string `json:"request"`
	By      string `json:"by"`
	At      string `json:"at"`
}

func (s *Service) Keep(ctx context.Context, app string) (bridge.AppSummary, error) {
	if err := s.start(ctx); err != nil {
		return bridge.AppSummary{}, err
	}
	s.mu.Lock()
	_, err := s.keepable(ctx, app)
	s.mu.Unlock()
	if err != nil {
		return bridge.AppSummary{}, err
	}
	// The records service reads the definition through this store, so the
	// sample is carried before the registry lock is taken for the commit.
	if err := s.carrySample(ctx, app); err != nil {
		return bridge.AppSummary{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.keepable(ctx, app)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	files, err := readFiles(s.definitionDir(app))
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if data := files[spec.PlanFile]; data != nil {
		var plan spec.Plan
		if json.Unmarshal(data, &plan) == nil && plan.Ask != "" {
			m.Request = plan.Ask
		}
	}
	m.State, m.Version = bridge.StateKept, 1
	if err := s.commitVersion(ctx, &m, files, "Keep", m.Request); err != nil {
		return bridge.AppSummary{}, err
	}
	s.publish(m.AppSummary)
	return m.AppSummary, nil
}

// keepable returns the app when it is a preview that has never been kept.
// The caller holds s.mu.
func (s *Service) keepable(ctx context.Context, app string) (metadata, error) {
	m, err := s.lookup(ctx, app)
	if err != nil {
		return metadata{}, err
	}
	if m.State != bridge.StatePreview || m.Version != 0 {
		return metadata{}, bridge.Errorf(bridge.CodeConflict, "only a preview can be kept")
	}
	return m, nil
}

// GetVersion reads a tag, even if the current working tree was later changed.
func (s *Service) GetVersion(ctx context.Context, app string, version int) (bridge.AppDetail, error) {
	if err := s.start(ctx); err != nil {
		return bridge.AppDetail{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil {
		return bridge.AppDetail{}, err
	}
	files, err := versionFiles(s.definitionDir(app), version)
	if err != nil {
		return bridge.AppDetail{}, err
	}
	m.Version = version
	if err := definitionSummary(&m, files); err != nil {
		return bridge.AppDetail{}, err
	}
	return detail(m.AppSummary, files), nil
}

// Versions lists numbered tags newest first; the draft branch is never a version.
func (s *Service) Versions(ctx context.Context, app string) ([]Version, error) {
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
	tags, err := repo.Tags()
	if err != nil {
		return nil, err
	}
	versions := []Version{}
	err = tags.ForEach(func(ref *plumbing.Reference) error {
		n, err := strconv.Atoi(strings.TrimPrefix(ref.Name().Short(), "v"))
		if err != nil || n < 1 || ref.Name().Short() != versionTag(n) {
			return nil
		}
		commit, err := taggedCommit(repo, n)
		if err != nil {
			return err
		}
		versions = append(versions, Version{Version: n, Tag: versionTag(n), Commit: commit.Hash.String(), Request: commitRequest(commit.Message), By: commit.Author.Name, At: commit.Author.When.UTC().Format(time.RFC3339)})
		return nil
	})
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version > versions[j].Version })
	return versions, err
}

// Restore appends a version; it never moves or deletes an existing tag.
// Definition restores do not read, migrate or replace records.
func (s *Service) Restore(ctx context.Context, app string, version int) (bridge.AppSummary, error) {
	if err := s.start(ctx); err != nil {
		return bridge.AppSummary{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.lookup(ctx, app)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	if m.State != bridge.StateKept {
		return bridge.AppSummary{}, bridge.Errorf(bridge.CodeConflict, "only a kept app can restore a version")
	}
	files, err := versionFiles(s.definitionDir(app), version)
	if err != nil {
		return bridge.AppSummary{}, err
	}
	m.Version++
	request := "restore of " + versionTag(version)
	if err := s.commitVersion(ctx, &m, files, "Restore", request); err != nil {
		return bridge.AppSummary{}, err
	}
	s.publish(m.AppSummary)
	return m.AppSummary, nil
}

func (s *Service) commitVersion(ctx context.Context, m *metadata, files spec.Files, kind, request string) error {
	files, err := versionIdentity(files, m.ID, m.Version)
	if err != nil {
		return err
	}
	if issues := spec.Validate(files); len(issues) > 0 {
		return &bridge.Error{Code: bridge.CodeCheckFailed, Message: "app definition is invalid", Details: map[string]any{"issues": issues}}
	}
	if err := definitionSummary(m, files); err != nil {
		return err
	}
	return s.changeDefinition(ctx, m, func(dir string) error {
		if err := replaceFiles(dir, files); err != nil {
			return err
		}
		repo, err := git.PlainOpen(dir)
		if err != nil {
			return err
		}
		return commitTag(repo, m, kind, request, s.now())
	})
}

func versionIdentity(files spec.Files, app string, version int) (spec.Files, error) {
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(files[spec.AppFile], &definition); err != nil || definition == nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "app.json must be a JSON object")
	}
	definition["id"], _ = json.Marshal(app)
	definition["space"], _ = json.Marshal("personal")
	definition["version"], _ = json.Marshal(version)
	data, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return nil, err
	}
	copy := make(spec.Files, len(files))
	for name, data := range files {
		copy[name] = data
	}
	copy[spec.AppFile] = append(data, '\n')
	return copy, nil
}

func definitionSummary(m *metadata, files spec.Files) error {
	var app spec.App
	if err := json.Unmarshal(files[spec.AppFile], &app); err != nil {
		return err
	}
	m.Name, m.Description = app.Name, app.Description
	return nil
}

func commitTag(repo *git.Repository, m *metadata, kind, request string, at time.Time) error {
	work, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := work.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return err
	}
	first, _, _ := strings.Cut(request, "\n")
	message := fmt.Sprintf("%s: %s\n\nRequest: %s\nBy: @%s\nVersion: %d\nCheck: pass\n", kind, first, strings.ReplaceAll(request, "\n", "\nRequest: "), m.Maker, m.Version)
	hash, err := work.Commit(message, &git.CommitOptions{Author: &object.Signature{Name: m.Maker, Email: m.Maker + "@codeaf.invalid", When: at.UTC()}})
	if err != nil {
		return err
	}
	if _, err := repo.CreateTag(versionTag(m.Version), hash, nil); err != nil {
		return err
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(liveBranch, hash)); err != nil {
		return err
	}
	return repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, liveBranch))
}

func commitRequest(message string) string {
	var lines []string
	for _, line := range strings.Split(message, "\n") {
		if request, ok := strings.CutPrefix(line, "Request: "); ok {
			lines = append(lines, request)
		}
	}
	return strings.Join(lines, "\n")
}

func versionTag(version int) string { return "v" + strconv.Itoa(version) }

func taggedCommit(repo *git.Repository, version int) (*object.Commit, error) {
	if version < 1 {
		return nil, bridge.Errorf(bridge.CodeInvalid, "version must be positive")
	}
	ref, err := repo.Tag(versionTag(version))
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) || errors.Is(err, git.ErrTagNotFound) {
			return nil, bridge.Errorf(bridge.CodeNotFound, "no version "+versionTag(version))
		}
		return nil, err
	}
	return repo.CommitObject(ref.Hash())
}

func versionFiles(dir string, version int) (spec.Files, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return nil, err
	}
	commit, err := taggedCommit(repo, version)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	files := spec.Files{}
	err = tree.Files().ForEach(func(file *object.File) error {
		if !allowedPath(file.Name) || file.Mode != filemode.Regular {
			return bridge.Errorf(bridge.CodeInvalid, "invalid file in app version: "+file.Name)
		}
		contents, err := file.Contents()
		files[file.Name] = []byte(contents)
		return err
	})
	return files, err
}
