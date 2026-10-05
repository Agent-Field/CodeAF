package audit

// Tests for repository resolution.
//
// Validation contract: only an existing local directory is audited. It comes
// back absolute and with symlinks resolved; a URL, an empty value, a missing
// path and a regular file are each refused with an error that says which, and
// nothing is cloned, created or fallen back to.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRepoReturnsTheResolvedDirectory(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveRepo(dir)
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	if got != want {
		t.Errorf("ResolveRepo = %q, want %q", got, want)
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if got, err := ResolveRepo(link); err != nil || got != want {
		t.Errorf("ResolveRepo(symlink) = (%q, %v), want %q", got, err, want)
	}
}

func TestResolveRepoMakesARelativePathAbsolute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir("sub", 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRepo("sub")
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	if !filepath.IsAbs(got) || filepath.Base(got) != "sub" {
		t.Errorf("ResolveRepo(sub) = %q, want an absolute path ending in sub", got)
	}
}

func TestResolveRepoRefuses(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, in, want string
		remote         bool
	}{
		{"https", "https://github.com/o/r", "is a URL", true},
		{"http", "http://example.test/r.git", "is a URL", true},
		{"scp-style", "git@github.com:o/r.git", "is a URL", true},
		{"ssh", "ssh://git@example.test/r", "is a URL", true},
		{"empty", "  ", "repo_url is empty", false},
		{"missing", filepath.Join(t.TempDir(), "absent"), "does not exist", false},
		{"file", file, "is not a directory", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRepo(tc.in)
			if err == nil {
				t.Fatalf("ResolveRepo(%q) = %q, want a refusal", tc.in, got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to say %q", err.Error(), tc.want)
			}
			if errors.Is(err, ErrRemoteRepository) != tc.remote {
				t.Errorf("errors.Is(ErrRemoteRepository) = %v, want %v", !tc.remote, tc.remote)
			}
		})
	}
}
