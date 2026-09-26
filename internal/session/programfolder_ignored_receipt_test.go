package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An ignored subfolder uses the plain-folder road, and its first receipt
// names the enclosing repository's ignore rule as the reason for no branch.
func TestIgnoredFolderStartReceiptNamesWhyItHasNoBranch(t *testing.T) {
	repo := newTestRepo(t)
	gitOut(t, repo, "config", "user.name", "Fixture")
	gitOut(t, repo, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("scratch/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, repo, "add", ".gitignore")
	gitOut(t, repo, "commit", "-m", "ignore scratch")
	ignored := filepath.Join(repo, "scratch")
	if err := os.Mkdir(ignored, 0o755); err != nil {
		t.Fatal(err)
	}
	program := testPrograms("senior-dev")[0]
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: ignored, Title: "Task", Holder: "test", Keep: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer folder.Finish("")
	receipt := delegateFolderReceipt(ignored, program, nil)
	if strings.Contains(receipt, "holds your home folder") || !strings.Contains(receipt, "git ignores this folder inside "+repo) {
		t.Fatalf("ignored folder start receipt = %q", receipt)
	}
}

// A dotfiles repository at the home folder has a different reason for
// working in place, and its start receipt keeps that reason distinct.
func TestHomeRepositoryStartReceiptNamesWhyItHasNoBranch(t *testing.T) {
	repo := newTestRepo(t)
	t.Setenv("HOME", repo)
	t.Setenv("CODEAF_HOME", t.TempDir())
	folderDir := filepath.Join(repo, "project")
	if err := os.Mkdir(folderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	program := testPrograms("senior-dev")[0]
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: folderDir, Title: "Task", Holder: "test", Keep: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer folder.Finish("")
	receipt := delegateFolderReceipt(folderDir, program, nil)
	if !strings.Contains(receipt, "which holds your home folder") || strings.Contains(receipt, "git ignores") {
		t.Fatalf("home repository start receipt = %q", receipt)
	}
}
