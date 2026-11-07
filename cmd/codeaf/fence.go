package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
)

// ── THE FENCE AROUND A RUN NOBODY IS WATCHING ──────────────────────────────
//
// Four doors run an agent with nobody at its keyboard: a factory stage
// (factory_stage.go), `codeaf chat --once` (chatv3.go), a headless engine
// conversation, which is what --once becomes when a host already holds the
// folder (engine.go), and `codeaf exec` (exec.go). An AgentField node reaches
// codeaf through the last three. Each builds its fence here, so they agree on
// where the widening is read from and where the job's own folders live.
//
// AN INTERACTIVE CONVERSATION IS NOT FENCED. A person at the keyboard sees
// each command and has the approval gate in front of it; fencing their chat
// would change what codeaf does for every person who uses it, which is a
// separate decision from this one.

// fenceStateMaxAge is how long a job's own temp and cache folders are kept
// after they were last used; a job that has not run for this long has ended.
const fenceStateMaxAge = 14 * 24 * time.Hour

// unattendedFence is the fence for a run working in workspace under the
// profile at profileDir, or nil when the person turned it off.
func unattendedFence(workspace, profileDir string) (*executor.Fence, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("codeaf does not know which folder this run works in, so it cannot keep its commands inside it")
	}
	words, err := config.JailAllowAt(executor.RepositoryOf(workspace), profileDir)
	if err != nil {
		return nil, err
	}
	person, _ := os.UserHomeDir()
	state := home.Join("jail")
	pruneFenceState(state, fenceStateMaxAge)
	return executor.NewFence(executor.FenceOptions{
		Root:       workspace,
		Home:       person,
		CodeafHome: home.Dir(),
		StateDir:   state,
		Allow:      executor.ParseFenceAllow(person, words...),
	})
}

// fencedSeat is ctx carrying the fenced seat for a door that has no session
// (`codeaf exec`), with the one sentence a system that cannot fence owes the
// person, said once on stderr where a scripted caller reads it.
func fencedSeat(ctx context.Context, f *executor.Fence) context.Context {
	if f == nil {
		return ctx
	}
	if line := executor.FenceLimitLine(); line != "" {
		fmt.Fprintln(os.Stderr, "note: "+line)
	}
	return executor.With(ctx, executor.FencedSeat(f))
}

// pruneFenceState removes the job folders nobody has used for maxAge. A job
// folder in use is touched by every command (its temp folder), so only a job
// that ended is old. Failures are left for the next prune.
func pruneFenceState(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		info, err := os.Stat(filepath.Join(path, "tmp"))
		if err != nil {
			info, err = e.Info()
		}
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		// Module caches are written read-only unless -modcacherw was set, so
		// the tree is opened up before it is removed.
		_ = filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
			if err == nil && fi.IsDir() {
				_ = os.Chmod(p, 0o700)
			}
			return nil
		})
		_ = os.RemoveAll(path)
	}
}
