package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec"
	homepkg "github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/substore"
)

// `codeaf run` has the same shape as `codeaf exec` — one headless door working
// in a directory a person named — so it must leave that directory as clean as
// exec does: the harness's own files (the turn recorder, the .obs spills) go
// to the state root keyed by the workspace's name, and the person's directory
// holds only work product. The door itself has to be driven — [drive] builds
// its own registry and bypasses the workspace this test is about.

// runHomeForTest gives the whole in-process run an isolated state root, a
// profile and a key the provider layer accepts but never dials: the program
// under test makes no model calls, so nothing behind the base URL is reached.
func runHomeForTest(t *testing.T) string {
	t.Helper()
	_, _ = execPendingHome(t)
	return os.Getenv(homepkg.EnvVar)
}

// TestARunLeavesTheWorkspaceGitClean is slice 2's check: one headless run of a
// real bundle, driven through the run door's own command function, in a fresh
// git repository — then `git status --porcelain`, which must be empty, with
// the run's machinery under the state root instead.
func TestARunLeavesTheWorkspaceGitClean(t *testing.T) {
	state := runHomeForTest(t)
	repo := gitRepoForExec(t)

	// The program is one real bundle in the home store this run resolves. Its
	// body makes no model call, and the guard names a file the workspace does
	// not have, so the run is handled the long way: the generalist leaf is a
	// real Linear run answered by the lane stub, and that leaf is what leaves a
	// turn recorder — a program that finishes without a leaf writes no recorder
	// at all, so the clean-workspace fact is this test's and the recorder's
	// address is only observable on the path that writes one.
	store := substore.Home()
	manifest := exec.Manifest{
		SubharnessInfo: exec.SubharnessInfo{
			Name:    "stamp-the-repo",
			Purpose: "leave the check's workspace clean",
		},
		Guards: []exec.Guard{{
			Kind: exec.GuardFile, File: "the-weeks-notes.md",
			Because: "the check needs the run to be handled the long way",
		}},
		// The long way must actually be TAKEN, not held: a ceiling narrower
		// than the generalist's reach stops the run before any leaf opens, and
		// then no recorder is written and the check has nothing to look at.
		Whitelist: []string{"bash"},
	}
	if _, err := store.Mint(substore.Files{
		Manifest: manifest,
		Program:  []byte("function run(input) { return { ok: true }; }\n"),
	}, "", ""); err != nil {
		t.Fatalf("minting the bundle: %v", err)
	}
	// The long way hands the ORIGINAL input to the generalist leaf, whose
	// TaskInput requires a brief — an input without one is refused before any
	// leaf opens.
	input := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(input, []byte(`{"brief":"answer with one sentence"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runSubharnessCommand([]string{
		manifest.Name, "--input", input, "--dir", repo,
	}); err != nil {
		t.Fatalf("the run did not settle cleanly: %v", err)
	}

	if dirty := workspaceStatus(t, repo); strings.TrimSpace(dirty) != "" {
		t.Fatalf("the run left the person's workspace dirty:\n%s", dirty)
	}
	for _, machinery := range []string{".codeaf", ".obs"} {
		if _, err := os.Stat(filepath.Join(repo, machinery)); err == nil {
			t.Fatalf("the run left its own %s directory in the person's workspace", machinery)
		}
	}
	// The recorder went somewhere, and that somewhere is the state root keyed
	// by this workspace's name — not the person's directory.
	scratch := homepkg.Join("scratch", filepath.Base(repo))
	var sawTrace bool
	_ = filepath.Walk(scratch, func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(path, ".trace.log") {
			sawTrace = true
		}
		return nil
	})
	if !sawTrace {
		t.Fatalf("the run left no recorder under the state root's scratch home %s (state %s)", scratch, state)
	}
}
