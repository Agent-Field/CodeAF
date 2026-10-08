package main

// The recipe door behind `factory_recipe`: one line into the repository's own
// `.codeaf/factory.md`, found the way the floor finds it, and a refusal in a
// person's words when codeaf does not know where the repository is.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestFactoryRecipeDoorOfANilStoreIsNil(t *testing.T) {
	if door := recipeDoor(nil, t.TempDir()); door != nil {
		t.Fatalf("a nil store became a non-nil recipe door: %#v", door)
	}
}

func TestFactoryRecipeDoorBanksOneLineIntoTheWorkspacesRecipe(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(t.TempDir(), "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	door := recipeDoor(st, web)
	ctx := context.Background()
	defaults := len(factory.DefaultRecipe().For(factory.KindIssue))

	// A stage the kind already has by name is replaced where it stands.
	if err := door.BankStage(ctx, "web", factory.KindIssue, "security · chat · read it for auth holes · when touches auth"); err != nil {
		t.Fatal(err)
	}
	recipe, problems, err := factory.Load(web)
	if err != nil || len(problems) > 0 {
		t.Fatalf("the banked file does not read: %v %v", err, problems)
	}
	stages := recipe.For(factory.KindIssue)
	if len(stages) != defaults {
		t.Fatalf("replacing a stage changed the count: %d, want %d", len(stages), defaults)
	}
	if i := factory.StageIndex(stages, "security"); i < 0 || stages[i].Ask != "read it for auth holes" {
		t.Fatalf("security stage = %+v", stages)
	}

	// A new stage is one more line.
	if err := door.BankStage(ctx, "web", factory.KindIssue, "docs · chat · update the readme"); err != nil {
		t.Fatal(err)
	}
	recipe, _, _ = factory.Load(web)
	if got := recipe.For(factory.KindIssue); len(got) != defaults+1 || got[len(got)-1].Name != "docs" {
		t.Fatalf("after a new stage: %+v", got)
	}

	if err := door.BankPolicy(ctx, "web", "never post without green tests"); err != nil {
		t.Fatal(err)
	}
	if err := door.BankHabit(ctx, "web", "PRs from my own issues ship when proof is green"); err != nil {
		t.Fatal(err)
	}
	recipe, problems, _ = factory.Load(web)
	if len(problems) > 0 || len(recipe.Policy) != 1 || recipe.Policy[0] != "never post without green tests" ||
		len(recipe.Habits) != 1 || recipe.Habits[0] != "PRs from my own issues ship when proof is green" {
		t.Fatalf("policy %q habits %q problems %v", recipe.Policy, recipe.Habits, problems)
	}
	data, _ := os.ReadFile(filepath.Join(web, factory.RecipeFile))
	if strings.Index(string(data), "## policy") > strings.Index(string(data), "## habits") {
		t.Fatalf("policy is written after habits:\n%s", data)
	}
}

func TestFactoryRecipeDoorRefusesAnUnknownCheckout(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	door := recipeDoor(st, filepath.Join(t.TempDir(), "web"))
	want := "codeaf does not know where billing is checked out; open codeaf there once"
	for _, err := range []error{
		door.BankStage(context.Background(), "billing", factory.KindPR, "review · chat"),
		door.BankPolicy(context.Background(), "billing", "x"),
		door.BankHabit(context.Background(), "billing", "x"),
	} {
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	}
}

func TestFactoryRecipeDoorBanksANoteOnABranchInAGitCheckout(t *testing.T) {
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "t")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@example.com")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(t.TempDir(), "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = web
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(web, "README"), []byte("x\n"), 0o644)
	git("add", "README")
	git("commit", "-q", "-m", "init")
	// No gh on PATH, no remote.
	t.Setenv("PATH", filepath.Dir(mustLookPath(t, "git")))
	door := recipeDoor(st, web).(session.RecipeNoter)
	note, err := door.BankNote(context.Background(), session.RecipeNotice{Repo: "web", Kind: "issue", Line: "security · chat · read it for auth holes · when touches auth"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "written · committed on factory/recipe-security · no remote to push to"; note != want {
		t.Fatalf("note = %q", note)
	}
	if _, err := os.Stat(filepath.Join(web, factory.RecipeFile)); err == nil {
		t.Fatal("the checkout's file was written")
	}
	cmd := exec.Command("git", "show", "factory/recipe-security:"+factory.RecipeFile)
	cmd.Dir = web
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "security · chat") {
		t.Fatalf("branch file: %v\n%s", err, out)
	}
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
