package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// registerOwnedWorkspace names only copies beneath the conversation's tree root.
// A project reference never becomes an owned run just because its path is known.
func registerOwnedWorkspace(tree taskTree, place Place) (taskTree, error) {
	if !WorkspaceOwnershipEnabled(place) {
		return tree, nil
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return tree, err
	}

	projectURI := ""
	project := tree.ground
	if project == "" {
		project = tree.root
	}
	owned := tree.mode != TaskModeInPlace && tree.mode != TaskModeFolder && ownedTreePath(tree.dir, place)
	var chat workspaceidentity.Record
	if strings.TrimSpace(place.Dir) != "" {
		chat, err = registry.Register(workspaceidentity.Chat, canonicalPath(place.Dir))
		if err != nil {
			return tree, fmt.Errorf("record task conversation: %w", err)
		}
	}
	if project != "" {
		if owned && ownedTreePath(project, place) {
			parent, err := registry.Lookup(workspaceidentity.Run, canonicalPath(project))
			if err != nil {
				return tree, fmt.Errorf("find task's admitted parent copy: %w", err)
			}
			if parent.OwnerURI != chat.URI {
				return tree, errors.New("task ground belongs to another conversation")
			}
			projectURI = parent.ProjectURI
			tree.groundURI = parent.URI
		} else {
			record, err := registry.Register(workspaceidentity.Project, canonicalPath(project))
			if err != nil {
				return tree, fmt.Errorf("record task project: %w", err)
			}
			projectURI, tree.groundURI = record.URI, record.URI
		}
	}
	tree.projectURI, tree.chatURI = projectURI, chat.URI
	if !owned {
		return tree, nil
	}
	if tree.root != "" {
		if canonicalPath(tree.root) == canonicalPath(project) {
			tree.rootURI = tree.groundURI
		} else {
			tree.rootURI = projectURI
		}
	}
	run, err := registry.Register(workspaceidentity.Run, canonicalPath(tree.dir), workspaceidentity.Links{OwnerURI: chat.URI, ProjectURI: projectURI})
	if err != nil {
		return tree, fmt.Errorf("record task working copy: %w", err)
	}
	tree.uri, tree.chatURI, tree.projectURI = run.URI, chat.URI, run.ProjectURI

	// Temporary files are run-owned; conventional caches are shared by project.
	if _, err := ownedRuntimePaths(registry, run); err != nil {
		return tree, err
	}

	return tree, nil
}

func ownedTreePath(path string, place Place) bool {
	trees := place.Trees()
	if trees == "" || path == "" {
		return false
	}
	relative, err := filepath.Rel(trees, canonicalPath(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// resolveOwnedWorkspace never falls back from a supplied URI to a stale path.
// Foreign relay identities require explicit receiver adoption.
func resolveOwnedWorkspace(record *TaskCopyRecord, place Place) (string, error) {
	if !WorkspaceOwnershipEnabled(place) {
		return "", errors.New("private task placement must be enabled to reopen this identity")
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return "", err
	}
	run, err := registry.Resolve(record.URI)
	if err != nil {
		return "", err
	}
	if run.Role != workspaceidentity.Run || record.ChatURI == "" || run.OwnerURI != record.ChatURI || run.ProjectURI != record.ProjectURI {
		return "", errors.New("working copy identity does not match its recorded ownership")
	}
	chat, err := registry.Resolve(record.ChatURI)
	if err != nil {
		return "", err
	}
	if chat.Role != workspaceidentity.Chat || chat.Path != canonicalPath(place.Dir) {
		return "", errors.New("working copy belongs to another conversation")
	}
	if !ownedTreePath(run.Path, place) {
		return "", errors.New("working copy binding is outside its conversation trees")
	}
	return run.Path, nil
}

// configureOwnedWorker routes scratch for admitted copies or explicit in-place
// projects at the runner seam. Project configuration grants no copy ownership.
// It preserves HOME and the existing seat's policy/recording capabilities.
func configureOwnedWorker(config *Config, taskDir string, logOwnerURI ...string) error {
	if config.WorkspaceURI == "" {
		return nil
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		return err
	}
	run, err := registry.Resolve(config.WorkspaceURI)
	if err != nil {
		return err
	}
	if (run.Role != workspaceidentity.Run && run.Role != workspaceidentity.Project) || run.Path != canonicalPath(config.Workspace) {
		return errors.New("worker directory does not match its admitted workspace identity")
	}
	if len(logOwnerURI) > 1 {
		return errors.New("worker log directory has more than one owner")
	}
	logOwner := run.URI
	if len(logOwnerURI) == 1 && logOwnerURI[0] != "" {
		chat, err := registry.Resolve(logOwnerURI[0])
		if err != nil {
			return err
		}
		if chat.Role != workspaceidentity.Chat || run.Role == workspaceidentity.Run && run.OwnerURI != chat.URI {
			return errors.New("worker log directory belongs to another conversation")
		}
		logOwner = chat.URI
	}
	if _, err := registry.Register(workspaceidentity.Log, canonicalPath(taskDir), workspaceidentity.Links{OwnerURI: logOwner}); err != nil {
		return err
	}

	paths, err := ownedRuntimePaths(registry, run)
	if err != nil {
		return err
	}
	scratch, cache := paths[0], paths[1]

	config.Seat = executor.WithEnvironment(config.seat(), []string{"TMPDIR=" + scratch, "TMP=" + scratch, "TEMP=" + scratch, "XDG_CACHE_HOME=" + cache, "CODEAF_WORKSPACE_URI=" + run.URI})
	return nil
}

// workspaceRuntimeRoot uses the full URI: imported namespaces can reuse the
// same resource suffix without ever sharing an unrelated run's scratch files.
func workspaceRuntimeRoot(uri string) string {
	digest := sha256.Sum256([]byte(uri))
	return home.Join("workspace-runtime", hex.EncodeToString(digest[:]))
}

func ownedRuntimePaths(registry *workspaceidentity.Registry, run workspaceidentity.Record) ([2]string, error) {
	scratch := filepath.Join(workspaceRuntimeRoot(run.URI), "temp")
	if err := os.MkdirAll(scratch, 0700); err != nil {
		return [2]string{}, err
	}
	if _, err := registry.Register(workspaceidentity.Temp, scratch, workspaceidentity.Links{OwnerURI: run.URI}); err != nil {
		return [2]string{}, err
	}
	var cache string
	var err error
	if run.ProjectURI != "" {
		cache, err = ownedProjectCache(registry, run.ProjectURI)
	} else {
		cache, err = ownedCache(registry, run.URI, filepath.Join(workspaceRuntimeRoot(run.URI), "cache"))
	}
	return [2]string{scratch, cache}, err
}

// ownedProjectCache is project-owned shared storage, never a run's cleanup target.
func ownedProjectCache(registry *workspaceidentity.Registry, projectURI string) (string, error) {
	project, err := registry.Resolve(projectURI)
	if err != nil {
		return "", err
	}
	if project.Role != workspaceidentity.Project {
		return "", errors.New("cache project identity does not name a project")
	}
	// A transfer may admit a second name for a project already used here.
	// Cache ownership follows its canonical local identity, not that alias.
	project, err = registry.Lookup(workspaceidentity.Project, project.Path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(project.URI))
	legacy := home.Join("workspace-caches", hex.EncodeToString(digest[:]), "cache")
	return ownedCache(registry, project.URI, legacy)
}

// ownedCache retains an admitted historical binding, including after its files
// were explicitly cleaned. New bindings use the existing shared cache owner.
func ownedCache(registry *workspaceidentity.Registry, ownerURI, legacy string) (string, error) {
	digest := sha256.Sum256([]byte(ownerURI))
	cache := filepath.Join(cachedir.Root(), "workspaces", hex.EncodeToString(digest[:]), "cache")
	prior, err := registry.LookupRecorded(workspaceidentity.Cache, legacy)
	if err == nil {
		if prior.OwnerURI != ownerURI {
			return "", errors.New("cache belongs to another workspace identity")
		}
		cache = prior.Path
	} else if !errors.Is(err, workspaceidentity.ErrUnknown) {
		return "", err
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return "", err
	}
	_, err = registry.Register(workspaceidentity.Cache, cache, workspaceidentity.Links{OwnerURI: ownerURI})
	return cache, err
}
