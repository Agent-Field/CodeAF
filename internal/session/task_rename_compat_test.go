package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// H5: task commits are authored with the current product name and address.
func TestH5TaskCommitIdentityUsesTheCurrentName(t *testing.T) {
	got := strings.Join(codeafGitIdentity(), " ")
	for _, want := range []string{"user.name=codeaf", "user.email=codeaf@localhost"} {
		if !strings.Contains(got, want) {
			t.Fatalf("identity %q does not contain %q", got, want)
		}
	}
}

// H5: a repository advanced only by a commit bearing the former task identity
// is still recognised as machine work, while a person's next commit is not.
func TestH5LegacyTaskCommitIsStillOurs(t *testing.T) {
	repo := newTestRepo(t)
	branch := currentBranch(repo)
	recorded := branchCommit(repo, branch)
	if err := os.WriteFile(filepath.Join(repo, "machine.txt"), []byte("old task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "machine.txt")
	mustGit(t, repo, "-c", "user.name="+legacyCodeafGitName, "-c", "user.email="+legacyCodeafGitEmail,
		"commit", "-m", "task work")
	if branchMovedByPerson(repo, branch, recorded) {
		t.Fatal("legacy-authored task commit was treated as a person's movement")
	}
	mustGit(t, repo, "-c", "user.name=person", "-c", "user.email=person@example.invalid",
		"commit", "--allow-empty", "-m", "person moved it")
	if !branchMovedByPerson(repo, branch, recorded) {
		t.Fatal("person-authored commit was treated as task machinery")
	}
}

// H5: every recogniser and staging filter accepts both repository-dropping
// spellings while the live write path stays unchanged.
func TestH5TaskDroppingRecognisersAcceptBothSpellings(t *testing.T) {
	if codeafDroppings != ".codeaf-v3" {
		t.Fatalf("live dropping path moved to %q", codeafDroppings)
	}
	for _, name := range taskDroppingNames() {
		if !isTaskDropping(name) || !isTaskDropping(filepath.ToSlash(filepath.Join(name, "tasks", "1"))) {
			t.Fatalf("%q is not recognised as task machinery", name)
		}
		if got := stageableWork(t.TempDir(), []string{filepath.ToSlash(filepath.Join(name, "private.log"))}); len(got) != 0 {
			t.Fatalf("%q was offered to git: %v", name, got)
		}
	}
	root := filepath.Join(t.TempDir(), legacyCodeafDroppings, "tasks", "family")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if (taskTree{root: root, ground: root}).landsInThePersonsRepository() {
		t.Fatal("legacy task tree was treated as the person's repository")
	}

	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "person.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range taskDroppingNames() {
		private := filepath.Join(repo, name, "private.log")
		if err := os.MkdirAll(filepath.Dir(private), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(private, []byte("private\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := groundManifest(repo)
	if !strings.Contains(manifest, "person.txt") {
		t.Fatalf("manifest lost person work: %q", manifest)
	}
	for _, name := range taskDroppingNames() {
		if strings.Contains(manifest, name) {
			t.Fatalf("manifest exposed %s: %q", name, manifest)
		}
	}
	digests := map[string]string{}
	if !gatherDigests(repo, "", digests, &digestWalk{budget: auditRestoreEntries}) {
		t.Fatal("digest walk unexpectedly exceeded its budget")
	}
	for path := range digests {
		if isTaskDropping(path) {
			t.Fatalf("digest walk recorded private path %q", path)
		}
	}

	mirror := t.TempDir()
	if err := os.WriteFile(filepath.Join(mirror, "person.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range taskDroppingNames() {
		private := filepath.Join(mirror, name, "private.log")
		if err := os.MkdirAll(filepath.Dir(private), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(private, []byte("private\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if problem := openFamilyRepository(mirror); problem != "" {
		t.Fatalf("open family repository: %s", problem)
	}
	tracked, err := git(mirror, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tracked, "person.txt") {
		t.Fatalf("family baseline lost person work: %q", tracked)
	}
	for _, name := range taskDroppingNames() {
		if strings.Contains(tracked, name) {
			t.Fatalf("family baseline staged %s: %q", name, tracked)
		}
	}
}
