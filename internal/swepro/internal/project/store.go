// This file ports swe-pro/src/project/instance-store.ts:1-190 and project.ts:137-286.
package project

// This file ports the explicit lifecycle of instance-store.ts:1-190 and the
// discovery core of project.ts:137-286. The bootstrap service is injected.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type LoadInput struct {
	Directory string
	Worktree  string
	Project   *Info
	PlanDB    *PlanDB
}

type Bootstrap interface {
	Run(ctx context.Context, instance InstanceContext) error
}

type BootstrapFunc func(context.Context, InstanceContext) error

func (f BootstrapFunc) Run(ctx context.Context, instance InstanceContext) error {
	return f(ctx, instance)
}

type Discoverer interface {
	FromDirectory(ctx context.Context, directory string) (Info, string, error)
}

type DiscovererFunc func(context.Context, string) (Info, string, error)

func (f DiscovererFunc) FromDirectory(ctx context.Context, directory string) (Info, string, error) {
	return f(ctx, directory)
}

type storeEntry struct {
	ready chan struct{}
	value InstanceContext
	err   error
}

// Store caches one booted instance per resolved active directory.
type Store struct {
	discover  Discoverer
	bootstrap Bootstrap

	mu    sync.Mutex
	cache map[string]*storeEntry
}

func NewStore(discover Discoverer, bootstrap Bootstrap) *Store {
	if discover == nil {
		discover = DiscovererFunc(Discover)
	}
	return &Store{discover: discover, bootstrap: bootstrap, cache: map[string]*storeEntry{}}
}

func resolvedDirectory(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

func (s *Store) boot(ctx context.Context, input LoadInput) (InstanceContext, error) {
	instance := InstanceContext{Directory: input.Directory, PlanDB: input.PlanDB}
	if input.Project != nil && input.Worktree != "" {
		instance.Worktree = input.Worktree
		instance.Project = *input.Project
	} else {
		project, sandbox, err := s.discover.FromDirectory(ctx, input.Directory)
		if err != nil {
			return InstanceContext{}, err
		}
		instance.Project, instance.Worktree = project, sandbox
	}
	if s.bootstrap != nil {
		if err := s.bootstrap.Run(WithContext(ctx, instance), instance); err != nil {
			return InstanceContext{}, err
		}
	}
	return instance, nil
}

func (s *Store) Load(ctx context.Context, input LoadInput) (InstanceContext, error) {
	input.Directory = resolvedDirectory(input.Directory)
	s.mu.Lock()
	if entry := s.cache[input.Directory]; entry != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return InstanceContext{}, ctx.Err()
		case <-entry.ready:
			return entry.value, entry.err
		}
	}
	entry := &storeEntry{ready: make(chan struct{})}
	s.cache[input.Directory] = entry
	s.mu.Unlock()

	entry.value, entry.err = s.boot(ctx, input)
	if entry.err != nil {
		s.mu.Lock()
		if s.cache[input.Directory] == entry {
			delete(s.cache, input.Directory)
		}
		s.mu.Unlock()
	}
	close(entry.ready)
	return entry.value, entry.err
}

func (s *Store) Reload(ctx context.Context, input LoadInput) (InstanceContext, error) {
	input.Directory = resolvedDirectory(input.Directory)
	entry := &storeEntry{ready: make(chan struct{})}
	s.mu.Lock()
	s.cache[input.Directory] = entry
	s.mu.Unlock()
	entry.value, entry.err = s.boot(ctx, input)
	if entry.err != nil {
		s.mu.Lock()
		if s.cache[input.Directory] == entry {
			delete(s.cache, input.Directory)
		}
		s.mu.Unlock()
	}
	close(entry.ready)
	return entry.value, entry.err
}

func (s *Store) Dispose(instance InstanceContext) {
	s.mu.Lock()
	delete(s.cache, filepath.Clean(instance.Directory))
	s.mu.Unlock()
}

func (s *Store) DisposeAll() {
	s.mu.Lock()
	s.cache = map[string]*storeEntry{}
	s.mu.Unlock()
}

func (s *Store) Provide(ctx context.Context, input LoadInput, fn func(context.Context) error) error {
	instance, err := s.Load(ctx, input)
	if err != nil {
		return err
	}
	return fn(WithContext(ctx, instance))
}

func runGit(ctx context.Context, cwd string, args ...string) (string, bool) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = cwd
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &bytes.Buffer{}
	if err := command.Run(); err != nil {
		return "", false
	}
	return strings.TrimSpace(stdout.String()), true
}

func findDotGit(directory string) string {
	current := directory
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return filepath.Join(current, ".git")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func readCachedProjectID(dotGit string) ID {
	data, err := os.ReadFile(filepath.Join(dotGit, "codeaf"))
	if err != nil {
		return ""
	}
	return ID(strings.TrimSpace(string(data)))
}

// Discover implements Project.fromDirectory's git/non-git split.
func Discover(ctx context.Context, directory string) (Info, string, error) {
	directory = resolvedDirectory(directory)
	now := time.Now().UnixMilli()
	dotGit := findDotGit(directory)
	if dotGit == "" {
		return Info{
			ID: GlobalID, Worktree: "/", Time: Time{Created: now, Updated: now},
			Sandboxes: []string{"/"},
		}, "/", nil
	}
	sandbox := filepath.Dir(dotGit)
	projectID := readCachedProjectID(dotGit)
	commonRaw, gitOK := runGit(ctx, sandbox, "rev-parse", "--git-common-dir")
	if !gitOK {
		if projectID == "" {
			projectID = GlobalID
		}
		vcs := "git"
		return Info{
			ID: projectID, Worktree: sandbox, VCS: &vcs,
			Time: Time{Created: now, Updated: now}, Sandboxes: []string{sandbox},
		}, sandbox, nil
	}
	common := commonRaw
	if !filepath.IsAbs(common) {
		common = filepath.Join(sandbox, common)
	}
	common = filepath.Clean(common)
	bareRaw, bareOK := runGit(ctx, sandbox, "config", "--bool", "core.bare")
	isBare := bareOK && bareRaw == "true"
	worktree := filepath.Dir(common)
	if common == sandbox {
		worktree = sandbox
	} else if isBare {
		worktree = common
	}
	if projectID == "" {
		projectID = readCachedProjectID(common)
	}
	if projectID == "" {
		rootsRaw, _ := runGit(ctx, sandbox, "rev-list", "--max-parents=0", "HEAD")
		roots := []string{}
		for _, root := range strings.Split(rootsRaw, "\n") {
			if root = strings.TrimSpace(root); root != "" {
				roots = append(roots, root)
			}
		}
		sort.Strings(roots)
		if len(roots) > 0 {
			projectID = ID(roots[0])
			_ = os.WriteFile(filepath.Join(common, "codeaf"), []byte(projectID), 0o644)
		}
	}
	if projectID == "" {
		projectID = GlobalID
	}
	vcs := "git"
	return Info{
		ID: projectID, Worktree: worktree, VCS: &vcs,
		Time: Time{Created: now, Updated: now}, Sandboxes: []string{sandbox},
	}, sandbox, nil
}

var ErrNoInstance = errors.New("project: no instance in context")

func Require(ctx context.Context) (InstanceContext, error) {
	instance, ok := FromContext(ctx)
	if !ok {
		return InstanceContext{}, ErrNoInstance
	}
	return instance, nil
}
