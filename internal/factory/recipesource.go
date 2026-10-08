package factory

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Where a recipe was read from, the words [RecipeSource] answers.
const (
	FromMainBranch  = "main branch"
	FromWorkingTree = "working tree"
	FromDefault     = "default"
)

// recipeGitWait is how long one git read of the recipe may take.
const recipeGitWait = 5 * time.Second

// RecipeSource is the text of the recipe file for the repository checked out
// at dir, and where it came from.
//
// THE RECIPE IS THE TEAM'S LAW, AND IT IS READ FROM THE MAIN BRANCH: the copy
// at the checkout's default branch (`origin/HEAD`'s target, else `main`, else
// `master`), read with `git show`, never the working tree and never a pull
// request's branch, so a pull request cannot change the law for its own
// review. from is [FromMainBranch] then.
//
// Only when there is no main branch to read (no git, not a repository, no such
// branch) is the working file read, and from is [FromWorkingTree]. When the
// place read has no recipe file the text is "" and from is [FromDefault]: the
// default recipe runs. A main branch with no recipe file is the default
// recipe even when the working tree has one, because the law is what main
// says.
//
// Which it was is logged the first time, and every time it changes, for a
// checkout: `recipe from main branch`.
func RecipeSource(dir string) (text string, from string, err error) {
	text, from, err = recipeSource(dir)
	if err == nil {
		noteRecipeSource(dir, from)
	}
	return text, from, err
}

func recipeSource(dir string) (string, string, error) {
	if ref := defaultBranch(dir); ref != "" {
		spec := ref + ":" + RecipeFile
		if _, err := recipeGit(dir, "cat-file", "-e", spec); err != nil {
			return "", FromDefault, nil
		}
		out, err := recipeGit(dir, "show", spec)
		if err != nil {
			return "", "", err
		}
		return out, FromMainBranch, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, RecipeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", FromDefault, nil
	}
	if err != nil {
		return "", "", err
	}
	return string(data), FromWorkingTree, nil
}

// defaultBranch is the checkout's main branch as a ref git can read, or ""
// when there is none: origin/HEAD's target, else main, else master.
func defaultBranch(dir string) string {
	var tries []string
	if out, err := recipeGit(dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			tries = append(tries, ref)
		}
	}
	tries = append(tries, "main", "master")
	for _, ref := range tries {
		if _, err := recipeGit(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			return ref
		}
	}
	return ""
}

// recipeGit runs one read-only git command in dir. The environment's own git
// directory, when a caller set one, is dropped, so dir is the repository read.
func recipeGit(dir string, args ...string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("no checkout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), recipeGitWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") || strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	out, err := cmd.Output()
	return string(out), err
}

// recipeFrom is where each checkout's recipe was last read from, so the log
// says it once and again only when it changes.
var recipeFrom sync.Map

func noteRecipeSource(dir, from string) {
	if prev, ok := recipeFrom.Swap(dir, from); ok && prev == from {
		return
	}
	log.Printf("recipe from %s · %s", from, dir)
}
