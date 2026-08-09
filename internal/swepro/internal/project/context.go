// Package project ports swe-pro/src/project at commit 3b25a1a. Effect
// fiber-locals become ordinary context.Context values so concurrent scheduler
// leaves cannot bleed working directories into one another.
package project

// This file ports instance-context.ts:1-57, instance.ts:1-36, and
// with-instance.ts:1-12.

import (
	"context"
	"path/filepath"

	"github.com/Agent-Field/swe-pro-go/internal/core"
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

// Info is the project.ts public project record.
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

type PlanDB struct {
	DBPath     string `json:"dbPath"`
	ProjectID  string `json:"projectID"`
	RootTaskID string `json:"rootTaskID"`
	TaskID     string `json:"taskID,omitempty"`
}

// InstanceContext is the per-leaf execution boundary.
type InstanceContext struct {
	Directory string  `json:"directory"`
	Worktree  string  `json:"worktree"`
	Project   Info    `json:"project"`
	PlanDB    *PlanDB `json:"planDB,omitempty"`
}

type instanceContextKey struct{}

// WithContext binds an instance to ctx. The value is immutable by convention;
// a defensive copy keeps callers from changing PlanDB coordinates after bind.
func WithContext(ctx context.Context, instance InstanceContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	copy := instance
	if instance.PlanDB != nil {
		plan := *instance.PlanDB
		copy.PlanDB = &plan
	}
	return context.WithValue(ctx, instanceContextKey{}, copy)
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

// ContainsPath matches AppFileSystem.contains and preserves the non-git "/"
// special case.
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
