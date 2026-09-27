//go:build !windows

// Package project resolves the directory and project a tool call runs
// against. The instance travels on context.Context so concurrent scheduler
// leaves cannot bleed working directories into one another.
package project

import (
	"context"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/seniordev/core"
)

type ID string

const GlobalID ID = "global"

type Icon struct {
	URL      *string `json:"url,omitempty"`
	Override *string `json:"override,omitempty"`
	Color    *string `json:"color,omitempty"`
}

type Commands struct {
	Start *string `json:"start,omitempty"`
}

type Time struct {
	Created     int64  `json:"created"`
	Updated     int64  `json:"updated"`
	Initialized *int64 `json:"initialized,omitempty"`
}

// Info is the public project record.
type Info struct {
	ID        ID        `json:"id"`
	Worktree  string    `json:"worktree"`
	VCS       *string   `json:"vcs,omitempty"`
	Name      *string   `json:"name,omitempty"`
	Icon      *Icon     `json:"icon,omitempty"`
	Commands  *Commands `json:"commands,omitempty"`
	Time      Time      `json:"time"`
	Sandboxes []string  `json:"sandboxes"`
}

// InstanceContext is the execution boundary a tool call resolves paths
// against: which directory it runs in, and which project that is.
type InstanceContext struct {
	Directory string `json:"directory"`
	Worktree  string `json:"worktree"`
	Project   Info   `json:"project"`
}

type instanceContextKey struct{}

// WithContext binds an instance to ctx. The value is immutable by convention.
func WithContext(ctx context.Context, instance InstanceContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, instanceContextKey{}, instance)
}

// FromContext returns the bound instance.
func FromContext(ctx context.Context) (InstanceContext, bool) {
	if ctx == nil {
		return InstanceContext{}, false
	}
	instance, ok := ctx.Value(instanceContextKey{}).(InstanceContext)
	return instance, ok
}

// Directory returns the active tool cwd, falling back when no instance is
// bound.
func Directory(ctx context.Context, fallback string) string {
	if instance, ok := FromContext(ctx); ok && instance.Directory != "" {
		return instance.Directory
	}
	return fallback
}

// ContainsPath reports whether path is inside the instance directory or its
// worktree; a non-git worktree of "/" does not count as containing anything.
func ContainsPath(path string, instance InstanceContext) bool {
	if core.Contains(instance.Directory, path) {
		return true
	}
	if instance.Worktree == "/" {
		return false
	}
	return core.Contains(instance.Worktree, path)
}

// RedirectIntoDirectory remaps an absolute original-worktree path into the
// active isolated directory.
func RedirectIntoDirectory(path string, instance InstanceContext) string {
	if instance.Directory == instance.Worktree || instance.Worktree == "/" {
		return path
	}
	if core.Contains(instance.Directory, path) || !core.Contains(instance.Worktree, path) {
		return path
	}
	relative, err := filepath.Rel(instance.Worktree, path)
	if err != nil {
		return path
	}
	return filepath.Join(instance.Directory, relative)
}

// Provide invokes fn with an instance-bound context.
func Provide[T any](ctx context.Context, instance InstanceContext, fn func(context.Context) (T, error)) (T, error) {
	return fn(WithContext(ctx, instance))
}
