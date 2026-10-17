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
