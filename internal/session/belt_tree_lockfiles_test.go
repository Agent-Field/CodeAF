package session

import (
	"path/filepath"
	"strings"
	"testing"
)

// A PROJECT'S OWN LOCKFILES ARE THE PROJECT'S WORK, AND A BELT LANDING CARRIES
// THEM. Every package manager keeps one beside its manifest, and a run that
// changed a dependency changed that file; a landing that dropped every path
// ending in `.lock` left the run's dependency change behind while the manifest
// beside it landed. Only the paths the harness itself writes stay out — its own
// folder, its plan store and the files beside it, and the shim it arms — and a
// project folder that merely has a familiar name is the project's.
func TestABeltLandingCarriesTheProjectsOwnLockfiles(t *testing.T) {
	repo := newTestRepo(t)
	for _, name := range []string{"yarn.lock", "Cargo.lock"} {
		writeFile(t, filepath.Join(repo, name), "pinned 1.0\n")
	}
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "lockfiles")

	// The run's own changes: two lockfiles moved, two new ones, and a project
	// folder whose name a benchmark rig also uses.
	project := []string{"yarn.lock", "Cargo.lock", "poetry.lock", "flake.lock", "bench-results/table.md"}
	for _, name := range project {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(name)), "pinned 2.0\n")
	}
	// The harness's own writes inside the copy.
	machinery := []string{".codeaf/plandb.db", planStoreFilename, planStoreFilename + "-wal", "bin/" + planShimFilename}
	for _, name := range machinery {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(name)), "harness\n")
	}

	staged := map[string]bool{}
	for _, spec := range beltTreeWork(repo) {
		staged[strings.TrimPrefix(spec, literalPathspec)] = true
	}
	for _, name := range project {
		if !staged[name] {
			t.Errorf("the landing left the project's own %s behind; it staged %v", name, staged)
		}
	}
	for _, name := range machinery {
		if staged[name] {
			t.Errorf("the landing staged the harness's own %s as the run's work", name)
		}
	}
}
