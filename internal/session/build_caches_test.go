package session

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

// THE BUILD-CACHE RULE MATCHES WHOLE NAMES AND NOTHING THAT RESEMBLES THEM
// (contract 5a and 5c). The harness-files rule beside it once dropped every
// lockfile for matching a suffix, so the near-misses here are the half that
// matters: each one is a project's own file and must stay work.
func TestBuildCacheMatchesOnlyTheNamedCaches(t *testing.T) {
	for path, want := range map[string]bool{
		"__pycache__/calc.cpython-310.pyc":  true,
		"pkg/__pycache__/m.cpython-310.pyc": true,
		"lib/old.pyc":                       true,
		".pytest_cache/v/cache/nodeids":     true,
		"a/.mypy_cache/3.12/x.json":         true,
		".ruff_cache/0.5/abc":               true,
		".DS_Store":                         true,
		"docs/.DS_Store":                    true,
		"calc.py":                           false,
		"poetry.lock":                       false,
		"package-lock.json":                 false,
		"bench-results/x":                   false,
		"pycache_notes.md":                  false,
		"src/__pycache__helper.py":          false,
		"my.pyc.txt":                        false,
		"DS_Store.md":                       false,
		"__pycache__":                       false,
		"notes/.pytest_cache.md":            false,
	} {
		if got := buildCache(path); got != want {
			t.Errorf("buildCache(%q) = %v, want %v", path, got, want)
		}
	}
}

// THE WORK TAB DOES NOT LIST A CACHE THE LANDING WILL LEAVE OUT (contract 5d),
// because the tab is the person's preview of what comes home.
func TestReadPlanWorkLeavesOutUntrackedBuildCaches(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	writeFile(t, filepath.Join(repo, "__pycache__", "calc.cpython-310.pyc"), "cache\n")
	writeFile(t, filepath.Join(repo, ".DS_Store"), "cache\n")
	writeFile(t, filepath.Join(repo, "pycache_notes.md"), "work\n")
	work := readPlanWork(&TaskCopyRecord{Dir: repo, HomeSha: base})
	if !slices.Equal(work.Added, []string{"pycache_notes.md"}) {
		t.Fatalf("added = %q, want only pycache_notes.md", work.Added)
	}
}

// THE MANUAL NAMES THE LIST AS THE CODE SPELLS IT (contract 5f), so a person
// asking why a file did not land reads the same names the landing leaves out.
func TestTheManualNamesEveryBuildCache(t *testing.T) {
	for _, name := range buildCacheNames {
		if !manual.Chat().Mentions("`" + name + "`") {
			t.Errorf("no chat manual page names the build cache `%s`", name)
		}
	}
}
