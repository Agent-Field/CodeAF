//go:build !windows

package main

// A SHELL RUN WORKS IN ITS FOLDER THE WAY A CONVERSATION'S RUN DOES
// (internal/session's programfolder.go): a plain folder is worked in as it is
// with the program told so on its line — where it used to end at once with
// "workspace is not a git repository" — a repository gets a branch of its own
// that is left checked out with the work committed on it, and a checkout with
// changes that are not committed is refused before anything is spent.

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeFolder is the name of the fake that works in its folder.
const fakeFolder = "fake-folder"

// fakeFolderProgram is a program that edits files the way senior-dev does:
// it says whether it was told it works without git, writes one file of work
// and one of its own notes into the folder it was handed, and passes.
func fakeFolderProgram() delegate.Delegate {
	return delegate.Delegate{
		Name: fakeFolder, Summary: "a program the tests carry, which writes a file where it is told to work", Default: "run", Page: "delegates",
		Guide:       "For the tests' folder work, with a brief that names the file.",
		PlainFolder: []string{"--in-place"},
		Notes:       ".fake-folder",
		Commands: []delegate.Command{{
			Name: "run", Usage: "[flags] -- <brief>", Summary: "does the whole task",
			Bind: func(fs *flag.FlagSet) delegate.Body {
				inPlace := fs.Bool("in-place", false, "work without git")
				return func(ctx context.Context, host delegate.Host, args []string) error {
					host.Hello([]string{"implement"})
					mode := "git"
					if *inPlace {
						mode = "in place"
					}
					host.Step(delegate.StepRecord{Command: "folder", Observation: mode})
					if err := os.WriteFile(filepath.Join(host.Workspace(), "made.txt"), []byte("made\n"), 0o644); err != nil {
						return err
					}
					if err := os.MkdirAll(filepath.Join(host.Workspace(), ".fake-folder"), 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(host.Workspace(), ".fake-folder", "spec.md"), []byte(strings.Join(args, " ")+"\n"), 0o644); err != nil {
						return err
					}
					host.Terminal(delegate.Ending{Status: delegate.StatusPass, Message: "made it"})
					return nil
				}
			},
		}},
	}
}

// hostWithFolderChild is [hostWithRealChild] for the fake that works in its
// folder, and it answers what the run printed on stdout and on stderr.
func hostWithFolderChild(t *testing.T) (*carriedFunnel, *lockedBuffer, *lockedBuffer) {
	t.Helper()
	restore := builtin.Override([]delegate.Delegate{fakeFolderProgram()})
	t.Cleanup(restore)
	t.Setenv(carriedChildEnv, "folder")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	calling := &carriedFunnel{cost: 0.001}
	previousRoad, previousOut, previousErr, previousGrace := carriedModels, carriedStdout, carriedStderr, carriedGrace
	carriedModels = func() (carriedRoad, error) {
		return carriedRoad{completerFor: calling.completerFor, seat: "seat/model"}, nil
	}
	printed, said := &lockedBuffer{}, &lockedBuffer{}
	carriedStdout, carriedStderr = printed, said
	carriedGrace = 5 * time.Second
	t.Cleanup(func() {
		carriedModels, carriedStdout, carriedStderr, carriedGrace = previousRoad, previousOut, previousErr, previousGrace
	})
	return calling, printed, said
}

// shellRepo is a repository with one commit on `main`, the way a person's
// project stands.
func shellRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "first"},
	} {
		shellGit(t, repo, args...)
	}
	return repo
}

// shellGit runs one git command in dir and answers what it printed.
func shellGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// A SHELL RUN IN A PLAIN FOLDER NO LONGER FAILS: the program is told on its
// line that it works without git, its work is left in the folder, its notes
// are moved into the run's record folder, and the last lines say so.
func TestAShellRunInAPlainFolderIsToldSoAndDoesNotFail(t *testing.T) {
	_, printed, _ := hostWithFolderChild(t)
	folder := t.TempDir()
	err := runCarried(fakeFolderProgram(), []string{"--dir", folder, "make a file"})
	if code := exitCodeOf(err); code != 0 {
		t.Fatalf("the shell run left with %d (%v):\n%s", code, err, printed)
	}
	out := printed.String()
	if !strings.Contains(out, "  folder · in place") {
		t.Fatalf("the program was not told it works without git:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(folder, "made.txt")); err != nil {
		t.Fatalf("the work is not in the folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, ".git")); !os.IsNotExist(err) {
		t.Fatalf("a plain folder was made a repository: %v", err)
	}
	record := newestRecordOf(t, fakeFolder)
	if _, err := os.Stat(filepath.Join(record, fakeFolder, "spec.md")); err != nil {
		t.Fatalf("the program's notes are not in the run's record folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, ".fake-folder")); !os.IsNotExist(err) {
		t.Fatalf("the program's notes were left in the folder: %v", err)
	}
	want := "  its work is in " + folder + ", which has no git history, so nothing was committed; its notes (.fake-folder/) are kept in " + filepath.Join(record, fakeFolder)
	if !strings.Contains(out, want) {
		t.Fatalf("the last lines do not say where the work is:\n%s\nwant %q", out, want)
	}
}

// A SHELL RUN IN A REPOSITORY WORKS ON A BRANCH OF ITS OWN, and the person's
// branch never moves: the program's work is committed on its branch, which is
// left checked out, and the last lines say how to go back.
func TestAShellRunInARepositoryWorksOnABranchOfItsOwn(t *testing.T) {
	_, printed, _ := hostWithFolderChild(t)
	repo := shellRepo(t)
	base := shellGit(t, repo, "rev-parse", "main")
	err := runCarried(fakeFolderProgram(), []string{"--dir", repo, "make a file"})
	if code := exitCodeOf(err); code != 0 {
		t.Fatalf("the shell run left with %d (%v):\n%s", code, err, printed)
	}
	out := printed.String()
	branch := shellGit(t, repo, "branch", "--show-current")
	if !strings.HasPrefix(branch, "task/make-a-file-") {
		t.Fatalf("the checkout is on %q, want the run's own branch left checked out", branch)
	}
	if !strings.Contains(out, fakeFolder+" · working in "+repo+", on its own branch "+branch) || !strings.Contains(out, "  folder · git") {
		t.Fatalf("the run did not say it works on its own branch with git:\n%s", out)
	}
	if tip := shellGit(t, repo, "rev-parse", "main"); tip != base {
		t.Fatalf("the person's branch moved from %s to %s", base, tip)
	}
	if files := shellGit(t, repo, "ls-tree", "--name-only", branch); files != "made.txt" {
		t.Fatalf("the run's branch holds %q, want its work and none of its notes", files)
	}
	if subject := shellGit(t, repo, "log", "-1", "--format=%s", branch); subject != "make a file" {
		t.Fatalf("the commit of its work is %q, want the brief's words", subject)
	}
	if status := shellGit(t, repo, "status", "--porcelain"); status != "" {
		t.Fatalf("the run left changes that are not committed:\n%s", status)
	}
	if !strings.Contains(out, "  its work is on the branch "+branch+" in "+repo+", 1 file, and that branch is checked out there; your branch main is as it was") {
		t.Fatalf("the last lines do not say where the work is:\n%s", out)
	}
}

// A SHELL RUN ON A CHECKOUT WITH CHANGES THAT ARE NOT COMMITTED IS REFUSED
// before anything is started or spent, with the paths named.
func TestAShellRunIsRefusedACheckoutWithChangesThatAreNotCommitted(t *testing.T) {
	calling, printed, said := hostWithFolderChild(t)
	repo := shellRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "draft.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCarried(fakeFolderProgram(), []string{"--dir", repo, "make a file"})
	if code := exitCodeOf(err); code != int(exitCannotRun) {
		t.Fatalf("left with %d, want the rung for a run that could not start", code)
	}
	if want := "error: " + repo + " has changes that are not committed (draft.md); commit or stash them, then ask again"; !strings.Contains(said.String(), want) {
		t.Fatalf("the refusal = %q, want %q", said.String(), want)
	}
	if len(calling.seen()) != 0 || printed.String() != "" {
		t.Fatalf("a refused run did something: %d calls, printed %q", len(calling.seen()), printed.String())
	}
	if branch := shellGit(t, repo, "branch", "--show-current"); branch != "main" {
		t.Fatalf("a refused checkout was switched to %q", branch)
	}
}

// A SHELL RUN'S CHILD IS TOLD WHAT CODEAF DECIDED ABOUT ITS FOLDER: the
// program's own flags for a folder without git, and the repository's root in
// place of the person's --dir when that named a folder inside it.
func TestAShellRunsChildLineCarriesItsFolder(t *testing.T) {
	program := fakeFolderProgram()
	inv, err := delegate.Parse(program, []string{"--dir", "/r/repo/sub", "fix", "it"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	child := carriedInFolder(carriedChildLine(inv), inv, &session.ProgramFolder{Dir: "/r/repo", Branch: "task/fix-it-abc123"})
	if got, want := strings.Join(child, " "), fakeFolder+" --json --dir /r/repo fix it"; got != want {
		t.Fatalf("the child line = %q, want %q", got, want)
	}
	plain, err := delegate.Parse(program, []string{"--dir", "/r/plain", "fix", "it"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	child = carriedInFolder(carriedChildLine(plain), plain, &session.ProgramFolder{Dir: "/r/plain"})
	if got, want := strings.Join(child, " "), fakeFolder+" --json --in-place --dir /r/plain fix it"; got != want {
		t.Fatalf("the child line = %q, want %q", got, want)
	}
	again, err := delegate.Parse(program, child[1:], &bytes.Buffer{})
	if err != nil || again.Workspace != "/r/plain" || again.Brief() != "fix it" {
		t.Fatalf("the child reads %+v (%v)", again, err)
	}
}

// newestRecordOf is the most recent shell run's record folder for a program.
func newestRecordOf(t *testing.T, name string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(carriedRecordRoot(name), "*"))
	if len(matches) == 0 {
		t.Fatal("the shell run kept no record folder")
	}
	newest := matches[0]
	for _, match := range matches[1:] {
		if match > newest {
			newest = match
		}
	}
	return newest
}

// A SHELL RUN FINISHES ITS FOLDER BEFORE IT WAITS FOR ITS LAST PRICES. That
// wait is up to seventy seconds, a second ctrl-c during it leaves at once,
// and the folder used to be finished only after it: the repository was left
// on the program's branch with its work uncommitted and nothing said. By the
// time the model API starts closing, the work is committed and the run's
// record says when its program ended.
func TestAShellRunFinishesItsFolderBeforeWaitingForPrices(t *testing.T) {
	_, printed, _ := hostWithFolderChild(t)
	repo := shellRepo(t)
	var atClose struct{ status, files string }
	previous := carriedAPIClose
	carriedAPIClose = func(api *modelapi.Server) error {
		atClose.status = shellGit(t, repo, "status", "--porcelain")
		atClose.files = shellGit(t, repo, "ls-tree", "--name-only", "HEAD")
		return previous(api)
	}
	t.Cleanup(func() { carriedAPIClose = previous })
	err := runCarried(fakeFolderProgram(), []string{"--dir", repo, "make a file"})
	if code := exitCodeOf(err); code != 0 {
		t.Fatalf("the shell run left with %d (%v):\n%s", code, err, printed)
	}
	if atClose.status != "" || atClose.files != "made.txt" {
		t.Fatalf("when the API began to close the folder held %q uncommitted and %q committed, want its work committed", atClose.status, atClose.files)
	}
}
