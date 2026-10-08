package factory_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const fixedSentence = "security is fixed by the recipe · change .codeaf/factory.md to change it"

// A STAGE LINE MAY END `· fixed`, and the file writes it back.
func TestRecipeFileFixedRoundTrips(t *testing.T) {
	r, probs := factory.Parse("## issue\n1. plan · chat · say how\n2. security · chat · secrets and authz · effort strong · fixed\n3. fix · chat · fixed bugs get a test\n")
	if len(probs) != 0 {
		t.Fatalf("problems = %+v", probs)
	}
	sec := r.Stages[factory.StageIndex(r.Stages, "security")]
	if !sec.Fixed || !sec.On || sec.Effort != "strong" || sec.Ask != "secrets and authz" {
		t.Fatalf("security = %+v", sec)
	}
	if r.Stages[0].Fixed {
		t.Fatal("plan was not written fixed")
	}
	if fx := r.Stages[2]; fx.Fixed || fx.Ask != "fixed bugs get a test" {
		t.Fatalf("an ask that starts with fixed = %+v", fx)
	}
	out := factory.Format(r)
	if !strings.Contains(out, "2. security · chat · secrets and authz · effort strong · fixed\n") {
		t.Fatalf("Format lost fixed:\n%s", out)
	}
	again, probs := factory.Parse(out)
	if len(probs) != 0 || !again.Stages[1].Fixed || again.Stages[0].Fixed {
		t.Fatalf("fixed drifted across a round trip: %+v\n%s", probs, out)
	}
	// The bank writes the section back with the word on.
	dir := t.TempDir()
	if err := factory.BankRecipeStages(dir, factory.KindIssue, r.Stages); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, factory.RecipeFile))
	if !strings.Contains(string(data), "· fixed\n") {
		t.Fatalf("the bank dropped fixed:\n%s", data)
	}
}

// `## issue · fixed` MARKS EVERY STAGE of its section, and keeps plan's
// word.
func TestRecipeFileFixedSectionMarksEveryStage(t *testing.T) {
	r, probs := factory.Parse("## issue · fixed\n1. plan · chat · say how\n2. write · chat · do it\n\n## pr\n1. read · chat · the diff\n")
	if len(probs) != 0 {
		t.Fatalf("problems = %+v", probs)
	}
	if r.AdaptFor(factory.KindIssue) != factory.AdaptFixed {
		t.Fatal("the section's word was lost")
	}
	for _, s := range r.Stages {
		if !s.Fixed {
			t.Fatalf("%s is not fixed", s.Name)
		}
	}
	if r.For(factory.KindPR)[0].Fixed {
		t.Fatal("another section's stage was fixed")
	}
	out := factory.Format(r)
	if strings.Contains(out, "do it · fixed") || !strings.Contains(out, "## issue · fixed\n") {
		t.Fatalf("a fixed section repeats the word per line, or lost it:\n%s", out)
	}
	again, _ := factory.Parse(out)
	for _, s := range again.Stages {
		if !s.Fixed {
			t.Fatalf("%s lost fixed across a round trip", s.Name)
		}
	}
	// A fixed heading with no lines binds the stages it falls back to.
	bare, _ := factory.Parse("## issue · fixed\n")
	if len(bare.Stages) == 0 || !bare.Stages[0].Fixed || factory.DefaultRecipe().Stages[0].Fixed {
		t.Fatalf("an empty fixed section = %+v", bare.Stages)
	}
}

func fixedRecipe() factory.Recipe {
	r, _ := factory.Parse("## issue\n1. plan · chat · say how\n2. write · chat · do it\n3. security · chat · secrets · off · fixed\n4. proof · chat · show it\n")
	return r
}

func fixedItem() factory.Item {
	it := adaptItem()
	it.Stages = factory.CopyStages(fixedRecipe().Stages)
	it.Stream = nil
	return it
}

// A FIXED STAGE BINDS EVERYONE, in both modes, with the one sentence; it may
// be switched on.
func TestEditRefusesAFixedStageForEveryone(t *testing.T) {
	r := fixedRecipe()
	for _, by := range []string{factory.ByManager, factory.ByPlan, factory.ByYou} {
		for _, mode := range []factory.EditMode{factory.EditBeforeRun, factory.EditInRun} {
			for _, e := range []factory.RunEdit{
				{By: by, Ask: map[string]string{"security": "only secrets"}},
				{By: by, Thinking: map[string]string{"security": "cheap"}},
				{By: by, Skip: []string{"security"}},
			} {
				it := fixedItem()
				it.Stages[2].On = true
				got, lines, err := factory.Edit(it, e, r, mode)
				if err == nil || err.Error() != fixedSentence {
					t.Fatalf("%s %s %+v: err = %v", by, mode, e, err)
				}
				if lines != nil || !got.Stages[2].On || got.Stages[2].Ask != "secrets" {
					t.Fatalf("%s %s: the item changed: %+v", by, mode, got.Stages[2])
				}
			}
			got, lines, err := factory.Edit(fixedItem(), factory.RunEdit{By: by, On: []string{"security"}}, r, mode)
			if err != nil || len(lines) == 0 || !got.Stages[2].On {
				t.Fatalf("%s %s: switching a fixed stage on = %v %q", by, mode, err, lines)
			}
		}
	}
	// An item that took its copy before the file said fixed is bound all the
	// same, by the recipe's stage of that name.
	old := fixedItem()
	old.Stages[2].Fixed = false
	if _, _, err := factory.Edit(old, factory.RunEdit{By: factory.ByYou, Ask: map[string]string{"security": "x"}}, r, factory.EditBeforeRun); err == nil || err.Error() != fixedSentence {
		t.Fatalf("an older copy = %v", err)
	}
	// Stages beside it still change.
	if _, _, err := factory.Edit(fixedItem(), factory.RunEdit{By: factory.ByYou, Ask: map[string]string{"write": "do it well"}}, r, factory.EditBeforeRun); err != nil {
		t.Fatal(err)
	}
}

// THE PERSON'S TOGGLE AND THINKING go through the same bound.
func TestLocalToggleRefusesAFixedStage(t *testing.T) {
	_, st := openLocal(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, factory.RecipeFile), []byte(factory.Format(fixedRecipe())), 0o644); err != nil {
		t.Fatal(err)
	}
	const repo = "agentfield/codeaf"
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(func(r string) string {
		if r == repo {
			return dir
		}
		return ""
	}))
	id, err := seam.New(repo, "the fixed stage")
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := seam.Load()
	var it factory.Item
	for _, x := range snap.Items {
		if x.ID == id {
			it = x
		}
	}
	i := factory.StageIndex(it.Stages, "security")
	if i < 0 || !it.Stages[i].Fixed {
		t.Fatalf("the item's copy lost fixed: %+v", it.Stages)
	}
	if err := seam.SetStage(id, i, true); err != nil {
		t.Fatalf("switching on = %v", err)
	}
	if err := seam.SetStage(id, i, false); err == nil || err.Error() != fixedSentence {
		t.Fatalf("switching off = %v", err)
	}
	if err := seam.SetEffort(id, i, "strong"); err == nil || err.Error() != fixedSentence {
		t.Fatalf("thinking = %v", err)
	}
	if err := seam.SetStage(id, factory.StageIndex(it.Stages, "write"), false); err != nil {
		t.Fatalf("a stage beside it = %v", err)
	}
	if err := seam.AddStage(id, "after write, check the docs"); err != nil {
		t.Fatalf("adding a stage = %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeRecipe(t *testing.T, dir, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, factory.RecipeFile), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE RECIPE IS READ FROM THE MAIN BRANCH: a branch that edits the file,
// checked out or not, does not change what is read.
func TestRecipeSourceReadsTheMainBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	// No git at all: the working file.
	writeRecipe(t, dir, "## issue\n1. plan · chat · from the tree\n")
	if text, from, err := factory.RecipeSource(dir); err != nil || from != factory.FromWorkingTree || !strings.Contains(text, "from the tree") {
		t.Fatalf("no git = %q %q %v", text, from, err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "commit.gpgsign", "false")
	// A repository with no commit has no main branch to read yet.
	if _, from, _ := factory.RecipeSource(dir); from != factory.FromWorkingTree {
		t.Fatalf("no commit = %q", from)
	}
	law := "## issue\n1. plan · chat · say how\n2. security · chat · secrets · fixed\n"
	writeRecipe(t, dir, law)
	git(t, dir, "add", factory.RecipeFile)
	git(t, dir, "commit", "-q", "-m", "law")
	git(t, dir, "checkout", "-q", "-b", "pr")
	writeRecipe(t, dir, "## issue\n1. plan · chat · say how\n2. security · chat · secrets · off\n")
	git(t, dir, "commit", "-q", "-am", "a pull request lifts the law")
	text, from, err := factory.RecipeSource(dir)
	if err != nil || from != factory.FromMainBranch || text != law {
		t.Fatalf("on the pull request's branch = %q %q %v", text, from, err)
	}
	r, _, err := factory.Load(dir)
	if err != nil || !r.Stages[factory.StageIndex(r.Stages, "security")].Fixed {
		t.Fatalf("Load read the branch: %+v %v", r.Stages, err)
	}
	// An uncommitted edit on main is not the law either.
	git(t, dir, "checkout", "-q", "main")
	writeRecipe(t, dir, "## issue\n1. plan · chat · edited in place\n")
	if text, _, _ := factory.RecipeSource(dir); text != law {
		t.Fatalf("an uncommitted edit = %q", text)
	}
	// origin/HEAD wins over a local main.
	clone := filepath.Join(t.TempDir(), "clone")
	git(t, dir, "checkout", "-q", "--", ".")
	git(t, filepath.Dir(clone), "clone", "-q", dir, clone)
	git(t, clone, "checkout", "-q", "-b", "pr2", "origin/pr")
	if text, from, _ := factory.RecipeSource(clone); from != factory.FromMainBranch || text != law {
		t.Fatalf("a clone on a branch = %q %q", text, from)
	}
	// A main branch without the file is the default recipe, whatever the
	// working tree holds.
	bare := t.TempDir()
	git(t, bare, "init", "-q", "-b", "main")
	git(t, bare, "config", "commit.gpgsign", "false")
	git(t, bare, "commit", "-q", "--allow-empty", "-m", "start")
	writeRecipe(t, bare, "## issue\n1. plan · chat · not yet the law\n")
	if text, from, err := factory.RecipeSource(bare); err != nil || from != factory.FromDefault || text != "" {
		t.Fatalf("main without the file = %q %q %v", text, from, err)
	}
}
