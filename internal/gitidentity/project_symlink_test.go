package gitidentity

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder reached through a symlink is one project: the alias and the target
// hash to the same key, or memory holds two owners for one folder (#1761). A
// workspace that is not on disk keeps the absolute spelling — the key is still
// minted, because a project with no key quarantines its writes.
func TestThePathFallbackKeysOneFolderAcrossItsSpellings(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	fromTarget, err := ProjectKey(target)
	if err != nil {
		t.Fatalf("ProjectKey(target): %v", err)
	}
	fromLink, err := ProjectKey(link)
	if err != nil {
		t.Fatalf("ProjectKey(link): %v", err)
	}
	if fromLink != fromTarget {
		t.Fatalf("two spellings of one folder keyed two projects: link=%q target=%q", fromLink, fromTarget)
	}

	imagined := filepath.Join(t.TempDir(), "nowhere-at-all")
	key, err := ProjectKey(imagined)
	if err != nil || key == "" {
		t.Fatalf("a workspace that is not on disk minted no key: %q, %v", key, err)
	}
}
