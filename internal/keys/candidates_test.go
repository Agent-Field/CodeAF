package keys

import (
	"os"
	"path/filepath"
	"testing"
)

// TestC9CandidatesScreenTheWholeListWithoutExpandingIt proves C9/U2: even a
// long full plan is screened, while a directory entry cannot silently widen
// the scope beyond the engine's candidates.
func TestC9CandidatesScreenTheWholeListWithoutExpandingIt(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "unlisted"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unlisted", ".env"), []byte("PRIVATE=stay-here"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PRIVATE=listed"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 1024)
	for i := range paths {
		paths[i] = "missing"
	}
	paths = append(paths, "unlisted", ".env")
	found := (Scanner{}).Candidates(root, paths)
	if len(found) != 1 || found[0].Path != ".env" || found[0].Rule != RuleDotenv {
		t.Fatalf("candidate screen widened or truncated its scope: %v", found)
	}
}
