package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// GH_TOKEN wins over GITHUB_TOKEN, which wins over gh; with none of the three
// the answer is empty. gh here is a script in a temp folder, never the
// machine's own.
func TestTokenOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in gh is a shell script")
	}
	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte("#!/bin/sh\necho from-gh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	look := func(string) (string, error) { return gh, nil }
	absent := func(string) (string, error) { return "", errors.New("absent") }
	ctx := context.Background()

	t.Setenv("GH_TOKEN", "from-gh-token")
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	if got := TokenWith(ctx, look); got != "from-gh-token" {
		t.Fatalf("GH_TOKEN set: %q", got)
	}
	t.Setenv("GH_TOKEN", "")
	if got := TokenWith(ctx, look); got != "from-github-token" {
		t.Fatalf("GITHUB_TOKEN set: %q", got)
	}
	t.Setenv("GITHUB_TOKEN", "")
	if got := TokenWith(ctx, look); got != "from-gh" {
		t.Fatalf("gh only: %q", got)
	}
	if got := TokenWith(ctx, absent); got != "" {
		t.Fatalf("nothing at all: %q", got)
	}
}

// fakeGH writes an executable stand-in gh (or, with mode 0o600, a file that is
// not executable) into dir and returns its path.
func fakeGH(t *testing.T, dir string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// seamsFor points the resolver at a fake inherited-PATH lookup and a fake
// Homebrew folder, restoring both afterwards. No real PATH is read or changed.
func seamsFor(t *testing.T, system func(string) (string, error), brew ...string) {
	t.Helper()
	keepSystem, keepDirs := systemLookPath, ghFallbackDirs
	systemLookPath, ghFallbackDirs = system, brew
	t.Cleanup(func() { systemLookPath, ghFallbackDirs = keepSystem, keepDirs })
}

func TestLookGHPrefersTheInheritedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in gh is a shell script")
	}
	brew := t.TempDir()
	fakeGH(t, brew, 0o700)
	seamsFor(t, func(string) (string, error) { return "/inherited/gh", nil }, brew)
	if got, err := lookGH("gh"); err != nil || got != "/inherited/gh" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLookGHFallsBackToTheHomebrewFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in gh is a shell script")
	}
	brew := t.TempDir()
	want := fakeGH(t, brew, 0o700)
	seamsFor(t, func(string) (string, error) { return "", errors.New("not on PATH") }, brew)
	if got, err := lookGH("gh"); err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestLookGHMissingAndNonExecutableAreNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in gh is a shell script")
	}
	empty, plain := t.TempDir(), t.TempDir()
	fakeGH(t, plain, 0o600)
	notFound := errors.New("not on PATH")
	seamsFor(t, func(string) (string, error) { return "", notFound }, empty, plain)
	if got, err := lookGH("gh"); err == nil || got != "" || !errors.Is(err, notFound) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLookGHLeavesOtherBinariesToThePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in gh is a shell script")
	}
	brew := t.TempDir()
	fakeGH(t, brew, 0o700)
	if err := os.WriteFile(filepath.Join(brew, "git-not-gh"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	seamsFor(t, func(string) (string, error) { return "", errors.New("not on PATH") }, brew)
	if got, err := lookGH("git-not-gh"); err == nil || got != "" {
		t.Fatalf("a different binary outside PATH resolved through gh's fallback: %q", got)
	}
}
