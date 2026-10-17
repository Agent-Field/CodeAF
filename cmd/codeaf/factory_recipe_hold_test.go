package main

// THE RECIPE DOOR WRITES NOTHING IN A FOLDER A PROGRAM'S RUN HOLDS, and a yes
// from the person is not an exception. The guard stands at the folder the write
// will ACTUALLY land in: the worktree cut from the default branch is nobody's
// to hold, while a checkout with no git, no commit or a git that fails is
// written in place, and a git error is no way around the hold.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// holdRecipeFolder readies a real program run on dir, which holds it until the
// test ends, and answers a function that ends the run early.
func holdRecipeFolder(t *testing.T, dir string) (end func()) {
	t.Helper()
	program, carried := builtin.Find("senior-dev")
	if !carried {
		t.Skip("this build carries no senior-dev")
	}
	folder, err := session.PrepareProgramFolder(session.ProgramFolderOrder{
		Program: program, Dir: dir, Title: "Fix the parser", Holder: "task 4 (Fix the parser)",
		Keep: filepath.Join(t.TempDir(), "run"),
	})
	if err != nil {
		t.Fatalf("hold %s: %v", dir, err)
	}
	ended := false
	end = func() {
		if !ended {
			ended = true
			folder.Finish("")
		}
	}
	t.Cleanup(end)
	return end
}

func recipeDoorAt(t *testing.T, dir string) fileRecipeDoor {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return recipeDoor(st, dir).(fileRecipeDoor)
}

func recipeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// bankEveryWay tries all four roads a chat yes can take and answers each error.
func bankEveryWay(door fileRecipeDoor) []error {
	ctx := context.Background()
	_, noteErr := door.BankNote(ctx, session.RecipeNotice{Repo: "web", Policy: "never post without green tests"})
	_, stageNoteErr := door.BankNote(ctx, session.RecipeNotice{Repo: "web", Kind: "issue", Line: "docs · chat · update the readme"})
	return []error{
		noteErr, stageNoteErr,
		door.BankStage(ctx, "web", factory.KindIssue, "docs · chat · update the readme"),
		door.BankPolicy(ctx, "web", "never post without green tests"),
		door.BankHabit(ctx, "web", "ship on green"),
	}
}

func requireRefusedAndUntouched(t *testing.T, label, dir string, errs []error) {
	t.Helper()
	for i, err := range errs {
		if err == nil || !strings.Contains(err.Error(), "task 4 (Fix the parser), is working, so nothing was written") {
			t.Fatalf("%s: road %d = %v, want the hold's refusal", label, i, err)
		}
	}
	for _, name := range []string{pipeline.RepoFile, filepath.Join(".codeaf", "factory.yaml")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("%s: the refused write left %s", label, name)
		}
	}
}

func TestRecipeDoorRefusesAHeldFolderWhereGitCannotCutAWorktree(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"not a git repository": func(*testing.T, string) {},
		"unborn git":           func(t *testing.T, dir string) { recipeGit(t, dir, "init", "-q", "-b", "main") },
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			web := filepath.Join(t.TempDir(), "web")
			if err := os.MkdirAll(web, 0o755); err != nil {
				t.Fatal(err)
			}
			prepare(t, web)
			door := recipeDoorAt(t, web)
			// THE HOLD APPEARS AFTER THE DOOR WAS BUILT — the card was awaiting its
			// yes — so the check is made at the write, not at the door's creation.
			end := holdRecipeFolder(t, web)
			requireRefusedAndUntouched(t, name, web, bankEveryWay(door))
			end()
			if _, err := door.BankNote(context.Background(), session.RecipeNotice{Repo: "web", Policy: "never post without green tests"}); err != nil {
				t.Fatalf("the same yes after the run ended = %v", err)
			}
			if _, err := os.Stat(filepath.Join(web, pipeline.RepoFile)); err != nil {
				t.Fatalf("the write did not land once the hold ended: %v", err)
			}
		})
	}
}

func TestRecipeDoorGitErrorIsNoWayAroundTheHold(t *testing.T) {
	// A program in a repository holds only its own copy, so the checkout is held
	// by a run on the plain folder around it.
	projects := t.TempDir()
	web := filepath.Join(projects, "web")
	recipeGit(t, projects, "init", "-q", "-b", "main", web)
	if err := os.WriteFile(filepath.Join(web, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recipeGit(t, web, "add", "README")
	recipeGit(t, web, "commit", "-q", "-m", "init")
	door := recipeDoorAt(t, web)
	end := holdRecipeFolder(t, projects)
	defer end()
	// A git that cannot be run is a git error on every call BankRecipeChange makes.
	t.Setenv("PATH", t.TempDir())
	requireRefusedAndUntouched(t, "git cannot run", web, bankEveryWay(door))
}

func TestRecipeDoorWritesTheWorktreeWhenOnlyTheCheckoutIsHeld(t *testing.T) {
	projects := t.TempDir()
	web := filepath.Join(projects, "web")
	recipeGit(t, projects, "init", "-q", "-b", "main", web)
	if err := os.WriteFile(filepath.Join(web, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recipeGit(t, web, "add", "README")
	recipeGit(t, web, "commit", "-q", "-m", "init")
	// Keep the answer to the branch: no gh on PATH, no remote.
	t.Setenv("PATH", filepath.Dir(mustLookPath(t, "git")))
	door := recipeDoorAt(t, web)
	// A run on the plain folder around the repository holds the checkout too.
	holdRecipeFolder(t, projects)
	note, err := door.BankNote(context.Background(), session.RecipeNotice{Repo: "web", Policy: "never post without green tests"})
	if err != nil {
		t.Fatalf("the worktree is not the held folder, yet the yes was refused: %v", err)
	}
	if want := "written · committed on factory/recipe-never · no remote to push to"; note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	if _, err := os.Stat(filepath.Join(web, pipeline.RepoFile)); err == nil {
		t.Fatal("the held checkout's file was written")
	}
	// The plain roads write in the checkout itself, which IS held.
	if err := door.BankPolicy(context.Background(), "web", "x"); err == nil {
		t.Fatal("BankPolicy wrote into the held checkout")
	}
}

func TestRecipeWriteOutsideAHeldChildRemainsAvailable(t *testing.T) {
	web := filepath.Join(t.TempDir(), "web")
	child := filepath.Join(web, "src")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	holdRecipeFolder(t, child)
	door := recipeDoorAt(t, web)
	if err := door.BankPolicy(context.Background(), "web", "Keep reviews small"); err != nil {
		t.Fatalf("the recipe file is outside the held child: %v", err)
	}
	if _, err := os.Stat(filepath.Join(web, pipeline.RepoFile)); err != nil {
		t.Fatal(err)
	}
}
