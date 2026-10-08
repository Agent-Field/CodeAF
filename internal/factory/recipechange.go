package factory

// A TAUGHT LINE IS A CHANGE FOR THE TEAM, NOT A SILENT WRITE.
//
// `.codeaf/factory.md` in a repository is the team's law. When a line is banked
// from a conversation (a stage, a policy sentence, a habit), the file is written
// on a branch `factory/recipe-<word>` cut from the default branch, committed as
// `recipe: <the sentence>` with no trailers, pushed when a remote exists, and
// opened as a pull request when `gh` exists, so a teammate reviews it like code.
//
// THE CHECKOUT IS NEVER TOUCHED. The branch is made in a temporary worktree
// (`git worktree add`) that is removed before this returns, so a dirty working
// tree stays as it was and the checkout stays on the branch it was on.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The four answers, and the one for a line the file already had.
const (
	RecipeNotePR       = "written · pull request #%d opened for the team"
	RecipeNotePushed   = "written · branch %s pushed · open the pull request when you want"
	RecipeNoteLocal    = "written · committed on %s · no remote to push to"
	RecipeNotePlain    = "written · .codeaf/factory.md (not a git repository)"
	RecipeNoteHad      = "already in .codeaf/factory.md · nothing to change"
	recipeNotePushFail = "written · committed on %s · could not push: %s"
)

// RecipeChangeTimeout bounds the whole road.
const RecipeChangeTimeout = 90 * time.Second

var (
	reNonWord = regexp.MustCompile(`[^a-z0-9]+`)
	rePullNum = regexp.MustCompile(`/pull/(\d+)\s*$`)
)

func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), errors.New(oneLine(msg))
	}
	return strings.TrimSpace(out.String()), nil
}

// recipeBranchWord is the first word of the sentence, as a branch-safe word.
func recipeBranchWord(sentence string) string {
	for _, w := range strings.Fields(sentence) {
		if w = strings.Trim(reNonWord.ReplaceAllString(strings.ToLower(w), "-"), "-"); w != "" {
			return w
		}
	}
	return "line"
}

// BankRecipeChange runs write against a checkout of the default branch and
// turns the result into a change for the team. sentence is the line as typed
// (the commit message and the pull request title), why the one-line reason and
// line the text the file gains. write receives the directory to write in.
// The answer is one of the Recipe* note sentences.
func BankRecipeChange(ctx context.Context, dir, sentence, why, line string, write func(dir string) error) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, RecipeChangeTimeout)
	defer cancel()
	sentence = oneLine(sentence)
	top, err := gitRun(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if err := write(dir); err != nil {
			return "", err
		}
		return RecipeNotePlain, nil
	}
	if _, err := gitRun(ctx, top, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		// A repository with no commit has no default branch to cut from.
		if err := write(dir); err != nil {
			return "", err
		}
		return RecipeNotePlain, nil
	}
	// The recipe's directory inside the repository, if dir is below the top.
	sub := "."
	if a, e1 := filepath.EvalSymlinks(dir); e1 == nil {
		if b, e2 := filepath.EvalSymlinks(top); e2 == nil {
			if r, e3 := filepath.Rel(b, a); e3 == nil && !strings.HasPrefix(r, "..") {
				sub = r
			}
		}
	}
	base, defName := recipeBase(ctx, top)
	remote := recipeRemote(ctx, top)

	branch := recipeBranchName(ctx, top, recipeBranchWord(sentence), remote)
	tmp, err := os.MkdirTemp("", "codeaf-recipe-")
	if err != nil {
		return "", err
	}
	wt := tmp + "/wt"
	defer os.RemoveAll(tmp)
	if _, err := gitRun(ctx, top, "worktree", "add", "-q", "-b", branch, wt, base); err != nil {
		return "", errors.New("could not open a worktree for the recipe change: " + err.Error())
	}
	cleaned := false
	cleanup := func(dropBranch bool) {
		if cleaned {
			return
		}
		cleaned = true
		bg := context.Background()
		_, _ = gitRun(bg, top, "worktree", "remove", "--force", wt)
		_, _ = gitRun(bg, top, "worktree", "prune")
		if dropBranch {
			_, _ = gitRun(bg, top, "branch", "-D", branch)
		}
	}
	defer cleanup(true)

	if err := write(filepath.Join(wt, sub)); err != nil {
		return "", err
	}
	if _, err := gitRun(ctx, wt, "add", "--", filepath.Join(sub, RecipeFile)); err != nil {
		return "", err
	}
	if _, err := gitRun(ctx, wt, "diff", "--cached", "--quiet"); err == nil {
		return RecipeNoteHad, nil
	}
	if _, err := gitRun(ctx, wt, "commit", "-q", "-m", "recipe: "+sentence); err != nil {
		return "", errors.New("could not commit the recipe change: " + err.Error())
	}
	// From here the branch holds the work; keep it.
	cleanup(false)

	if remote == "" {
		return fmt.Sprintf(RecipeNoteLocal, branch), nil
	}
	if _, err := gitRun(ctx, top, "push", "-q", "-u", remote, branch); err != nil {
		return fmt.Sprintf(recipeNotePushFail, branch, err.Error()), nil
	}
	if gh, err := exec.LookPath("gh"); err == nil {
		body := strings.TrimSpace(oneLine(why))
		if body == "" {
			body = "Taught in conversation."
		}
		body += "\n\nAdds to `.codeaf/factory.md`:\n\n    " + oneLine(line)
		cmd := exec.CommandContext(ctx, gh, "pr", "create", "--title", sentence, "--body", body, "--head", branch, "--base", defName)
		cmd.Dir = top
		var out bytes.Buffer
		cmd.Stdout = &out
		if cmd.Run() == nil {
			if m := rePullNum.FindStringSubmatch(strings.TrimSpace(out.String())); m != nil {
				return fmt.Sprintf(RecipeNotePR, mustNum(m[1])), nil
			}
			// gh printed something else; take the last line's number.
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			if m := rePullNum.FindStringSubmatch(strings.TrimSpace(lines[len(lines)-1])); m != nil {
				return fmt.Sprintf(RecipeNotePR, mustNum(m[1])), nil
			}
		}
	}
	return fmt.Sprintf(RecipeNotePushed, branch), nil
}

// recipeBase is the ref to cut from and the default branch's name: the
// remote's default when known, else main or master, else the branch the
// checkout is on.
func recipeBase(ctx context.Context, top string) (ref, name string) {
	if out, err := gitRun(ctx, top, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return out, strings.TrimPrefix(out, "origin/")
	}
	for _, n := range []string{"main", "master"} {
		if _, err := gitRun(ctx, top, "rev-parse", "--verify", "-q", "refs/heads/"+n); err == nil {
			return n, n
		}
	}
	cur, err := gitRun(ctx, top, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || cur == "" || cur == "HEAD" {
		return "HEAD", "HEAD"
	}
	return cur, cur
}

func recipeRemote(ctx context.Context, top string) string {
	out, err := gitRun(ctx, top, "remote")
	if err != nil || out == "" {
		return ""
	}
	names := strings.Fields(out)
	for _, n := range names {
		if n == "origin" {
			return n
		}
	}
	return names[0]
}

// recipeBranchName is factory/recipe-<word>, with -2, -3 ... when taken here
// or on the remote.
func recipeBranchName(ctx context.Context, top, word, remote string) string {
	for i := 1; ; i++ {
		name := "factory/recipe-" + word
		if i > 1 {
			name += "-" + strconv.Itoa(i)
		}
		if _, err := gitRun(ctx, top, "rev-parse", "--verify", "-q", "refs/heads/"+name); err == nil {
			continue
		}
		if remote != "" {
			if out, err := gitRun(ctx, top, "ls-remote", "--heads", remote, name); err == nil && out != "" {
				continue
			}
		}
		return name
	}
}

func mustNum(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
