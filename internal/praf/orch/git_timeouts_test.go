package orch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// shorten sets one git timeout for the test.
func shorten(t *testing.T, timeout *time.Duration, to time.Duration) {
	t.Helper()
	was := *timeout
	*timeout = to
	t.Cleanup(func() { *timeout = was })
}

func installSleepingGit(t *testing.T, tmp, operation string) {
	t.Helper()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		fmt.Sprintf("  [ \"$arg\" = %q ] && exec sleep 10\n", operation) +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func makeFilterUpstream(t *testing.T, path string) {
	t.Helper()
	makeUpstream(t, path)
	if err := os.WriteFile(filepath.Join(path, ".gitattributes"), []byte("*.bin filter=controlled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "asset.bin"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, path, "add", "-A")
	git(t, path, "commit", "-qm", "add filtered asset")

	git(t, path, "checkout", "-q", "-b", "_filter-pr", "main")
	if err := os.WriteFile(filepath.Join(path, "asset.bin"), []byte("pr\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, path, "commit", "-qam", "update filtered asset")
	sha := git(t, path, "rev-parse", "HEAD")
	git(t, path, "update-ref", "refs/pull/1/head", sha)
	git(t, path, "checkout", "-q", "main")
	git(t, path, "branch", "-qD", "_filter-pr")
}

func TestCheckoutTimeoutKillsFilterProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("process-group assertion requires a Unix-like system")
	}
	shorten(t, &checkoutTimeout, 2*time.Second)
	tmp := t.TempDir()
	upstream := filepath.Join(tmp, "upstream")
	makeFilterUpstream(t, upstream)
	target := filepath.Join(tmp, "workspace")
	cloneWorkspace(t, upstream, target)
	git(t, target, "checkout", "-q", "main")
	pidFile := filepath.Join(tmp, "slow-filter.pid")
	filterCommand := fmt.Sprintf("sh -c 'echo $$ > \"$1\"; sleep 30; cat' sh %q", pidFile)
	git(t, target, "config", "filter.controlled.smudge", filterCommand)

	started := time.Now()
	err := checkoutPRBranch(context.Background(), "", target, 1)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected checkout timeout")
	}
	if elapsed < 1500*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("checkout timeout returned after %s, want about 2s", elapsed)
	}
	for _, lockName := range []string{"index.lock", "shallow.lock"} {
		if _, statErr := os.Stat(filepath.Join(target, ".git", lockName)); !os.IsNotExist(statErr) {
			t.Fatalf("%s remains after checkout timeout: %v", lockName, statErr)
		}
	}

	git(t, target, "config", "filter.controlled.smudge", "cat")
	checkoutTimeout = 10 * time.Second
	if err := checkoutPRBranch(context.Background(), "", target, 1); err != nil {
		t.Fatalf("checkout after timeout cleanup: %v", err)
	}
	if got := readFile(t, filepath.Join(target, "asset.bin")); got != "pr\n" {
		t.Fatalf("checkout after timeout = %q, want %q", got, "pr\n")
	}

	pidBytes, readErr := os.ReadFile(pidFile)
	if os.IsNotExist(readErr) {
		t.Skip("slow smudge filter did not record its PID")
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	filterPID, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if parseErr != nil {
		t.Fatalf("parse slow smudge filter PID: %v", parseErr)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("kill", "-0", strconv.Itoa(filterPID)).Run() != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("slow smudge filter process %d survived git timeout", filterPID)
}

// TestCheckoutSkipsLFSSmudging: the review reads code, so git is told to skip
// LFS smudging whatever the environment says.
func TestCheckoutSkipsLFSSmudging(t *testing.T) {
	t.Setenv("GIT_LFS_SKIP_SMUDGE", "0")
	tmp := t.TempDir()
	upstream := filepath.Join(tmp, "upstream")
	makeFilterUpstream(t, upstream)
	target := filepath.Join(tmp, "workspace")
	cloneWorkspace(t, upstream, target)
	git(t, target, "config", "filter.controlled.smudge",
		"printf 'SKIP=%s\\n' \"$GIT_LFS_SKIP_SMUDGE\"")
	if err := checkoutPRBranch(context.Background(), "", target, 1); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if got := readFile(t, filepath.Join(target, "asset.bin")); got != "SKIP=1\n" {
		t.Fatalf("smudge output = %q, want SKIP=1", got)
	}
}

// TestTheTokenRidesTheEnvironmentNotTheRemote: a token becomes an
// Authorization header in one git process's config, never part of a URL that
// git would write into the clone's .git/config.
func TestTheTokenRidesTheEnvironmentNotTheRemote(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.autocrlf")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	env := strings.Join(gitEnv("secret-token"), "\n")
	for _, want := range []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=core.autocrlf",
		"GIT_CONFIG_KEY_1=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_1=AUTHORIZATION: basic ",
		"GIT_TERMINAL_PROMPT=0",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("git env lacks %q", want)
		}
	}
	if strings.Contains(env, "secret-token") {
		t.Error("the token appears in the clear")
	}
	if plain := strings.Join(gitEnv(""), "\n"); strings.Contains(plain, "extraheader") {
		t.Error("an empty token still set a header")
	}
}

func TestResolveRepoCloneUsesResolvedTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git shim is a POSIX shell script")
	}
	tmp := t.TempDir()
	installSleepingGit(t, tmp, "clone")
	shorten(t, &cloneTimeout, time.Second)

	started := time.Now()
	_, err := ResolveRepo(context.Background(), Access{Workdir: filepath.Join(tmp, "workspaces")}, "", "https://github.com/owner/repo/pull/1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ResolveRepo clone error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("clone timeout returned after %s, want about 1s", elapsed)
	}
}

func TestResolveRepoFetchUsesResolvedTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git shim is a POSIX shell script")
	}
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workspaces")
	if err := os.MkdirAll(filepath.Join(workdir, "repo-pr1", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	installSleepingGit(t, tmp, "fetch")
	shorten(t, &fetchAllTimeout, time.Second)

	started := time.Now()
	_, err := ResolveRepo(context.Background(), Access{Workdir: workdir}, "", "https://github.com/owner/repo/pull/1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ResolveRepo fetch error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("fetch timeout returned after %s, want about 1s", elapsed)
	}
}
