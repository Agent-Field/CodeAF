package exec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The test docs/SUBHARNESSES.md ends its checklist with, executed.
//
// "The test of the law: grep the reconciler, narrator, TUI, head, and store for
// the string 'swe' — none of them may contain it." It is not a style rule. The
// engine of record is singular, and it stays singular exactly as long as no
// part of it can tell which worker ran a leaf: the moment the reconciler has an
// `if subharness == "swe"` in it, the seam has stopped being a seam and the
// next specialist arrives as a rewrite of the dispatch paths instead of a
// registration.
//
// The word-boundary match is what makes this runnable rather than aspirational:
// "answer" contains those three letters and is not a mention of anything.
//
// Test files are exempt and only test files. A test may name a worker because
// naming one is how you check that nothing else does — internal/store's own
// subharness test splices a node whose worker is swe, which is the durability
// of the field being proven, not the store branching on it.
func TestNoEngineOfRecordPackageNamesTheSWEWorker(t *testing.T) {
	// The named packages, plus the reason each one is on the list.
	forbidden := map[string]string{
		"../resident": "the reconciler dispatches by registration, never by name",
		"../tui":      "a job card renders a node, and every node renders the same way",
		"../head":     "the head speaks about work, not about which worker did it",
		"../store":    "the store carries the choice as data and never reads it",
	}
	mention := regexp.MustCompile(`(?i)\bswe\b`)
	for directory, why := range forbidden {
		err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for index, line := range strings.Split(string(raw), "\n") {
				if mention.MatchString(line) {
					t.Errorf("%s:%d names the swe worker — %s\n  %s",
						path, index+1, why, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The other side of the same law, stated so it cannot rot into "swe is
// mentioned nowhere": there are exactly four places it may be named, and the
// executor is one of them. A test that only forbids would pass just as well
// against a build where the worker had been deleted.
func TestTheSWEWorkerIsNamedWhereItIsAllowedToBe(t *testing.T) {
	if SWESubharness != "swe" {
		t.Fatalf("the worker's name is %q; docs/SUBHARNESSES.md and every profile file say swe", SWESubharness)
	}
	worker := &SWE{}
	if worker.Subharness() != SWESubharness {
		t.Fatalf("the executor answers to %q", worker.Subharness())
	}
}
