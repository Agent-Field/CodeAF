package workspacepath

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

func TestAliasesRequireLiveEquivalentCitations(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	dir := home.Join("v3", "hosts", "old-key")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace"), []byte(alias), 0600); err != nil {
		t.Fatal(err)
	}
	unrelated := t.TempDir()
	if got := Aliases(target, unrelated); len(got) != 2 || !slices.Contains(got, alias) || slices.Contains(got, unrelated) {
		t.Fatalf("aliases: %v", got)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if got := Aliases(target); len(got) != 1 || got[0] != target {
		t.Fatalf("broken citation: %v", got)
	}
	if got := Canonical(alias); got != alias {
		t.Fatalf("missing path fallback: %s", got)
	}
}

func TestAliasLookupDoesNotFollowStateDirectoryLinks(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "workspace"), []byte(alias), 0600); err != nil {
		t.Fatal(err)
	}
	hosts := home.Join("v3", "hosts")
	if err := os.MkdirAll(hosts, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(hosts, "linked-host")); err != nil {
		t.Fatal(err)
	}
	if got := Aliases(target); len(got) != 1 {
		t.Fatalf("followed a linked state entry: %v", got)
	}
}
