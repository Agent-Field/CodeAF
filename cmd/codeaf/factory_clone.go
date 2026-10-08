package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// ── THE CLONE DOOR ──────────────────────────────────────────────────────────
//
// A RUN NEVER STARTS WITHOUT A CHECKOUT, AND THE FACTORY GETS ONE ITSELF
// (owner ruling, 2026-10-08: `r` on CodeAF #1786 ran with no checkout, skipped
// plan and write, and stopped at test saying codeaf did not know where CodeAF
// was). [factory.Seam.Clone] is that door on this machine: a repository with
// no known folder is cloned into the factory's own folder,
//
//	<v3FactoryRoot>/repos/<owner>/<name>     (~/.codeaf/v3/factory/repos/…)
//
// and the folder is recorded as its checkout (store.SetCheckout), which is
// what [factoryRepoDirs] answers next, for the floor, the recipe page and
// every run alike. THE WORKSPACE RULE STAYS FIRST: a window opened inside a
// watched repository records that folder ([recordWorkspaceCheckout]), and a
// repository with any known folder is never cloned again.
//
// The clone goes over `gh repo clone` when gh is on this machine, else over
// `git clone https://github.com/<owner>/<name>`. Either way the token is the
// one [factorygithub.TokenAt] resolves, the same road the forge client takes,
// and it reaches the child only through its environment (GH_TOKEN for gh, an
// http header in GIT_CONFIG_* for git): NEVER ON A COMMAND LINE, AND NEVER IN
// THE WORDS A FAILURE SAYS.

// factoryCloneFolder is where full is cloned under the store's root.
func factoryCloneFolder(root, full string) string {
	return filepath.Join(root, "repos", filepath.FromSlash(full))
}

// factoryCloneDoor is the seam's Clone door over st, cloning under st's root.
// token answers the token to clone with, "" for none (a public repository
// still clones).
func factoryCloneDoor(st *store.Store, token func(context.Context) string) func(ctx context.Context, repo string) (string, error) {
	return func(ctx context.Context, repo string) (string, error) {
		full, err := factoryCloneName(st, repo)
		if err != nil {
			return "", err
		}
		if dir := st.CheckoutDir(full); dir != "" {
			return dir, nil
		}
		dir := factoryCloneFolder(st.Root(), full)
		// A CLONE AN EARLIER ASK LEFT is used as it is: it is the same
		// repository in the same folder, and only its record was missing.
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, st.SetCheckout(full, dir)
		}
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			return "", fmt.Errorf("could not clone %s · %s is in the way and is not a checkout", full, dir)
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
			return "", fmt.Errorf("could not clone %s · %v", full, err)
		}
		tok := ""
		if token != nil {
			tok = strings.TrimSpace(token(ctx))
		}
		cmd, err := factoryCloneCommand(ctx, full, dir, tok)
		if err != nil {
			return "", err
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			// A HALF-MADE CLONE IS TAKEN AWAY, so the next ask starts clean
			// rather than finding a folder in the way. The folder was empty
			// or absent before the clone, so nothing of the person's goes.
			_ = os.RemoveAll(dir)
			why := factoryCloneWhy(string(out), tok)
			if why == "" {
				why = err.Error()
			}
			return "", fmt.Errorf("could not clone %s · %s", full, why)
		}
		if err := st.SetCheckout(full, dir); err != nil {
			return "", err
		}
		return dir, nil
	}
}

// factoryCloneName is `owner/name` for repo: as given when it already is one,
// else the one watched repository whose name it is.
func factoryCloneName(st *store.Store, repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if strings.Contains(repo, "/") {
		if !store.RepoName(repo) {
			return "", fmt.Errorf("%s is not a repository name", repo)
		}
		return repo, nil
	}
	if repo == "" {
		return "", errors.New("say which repository to clone")
	}
	watched, _ := st.Repos()
	var found []string
	for _, w := range watched {
		if _, short, ok := strings.Cut(w, "/"); ok && strings.EqualFold(short, repo) {
			found = append(found, w)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("codeaf does not know whose %s this is · watch it in the repos list first", repo)
	}
	return found[0], nil
}

// factoryCloneCommand is the clone of full into dir: gh's when gh is here,
// else git's, else the sentence that neither is.
func factoryCloneCommand(ctx context.Context, full, dir, tok string) (*exec.Cmd, error) {
	quiet := []string{"GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1"}
	if gh, err := forge.LookPath("gh"); err == nil {
		cmd := exec.CommandContext(ctx, gh, "repo", "clone", full, dir)
		e := env.EnvironWithout("GH_TOKEN", "GITHUB_TOKEN")
		if tok != "" {
			e = append(e, "GH_TOKEN="+tok)
		}
		cmd.Env = append(e, quiet...)
		return cmd, nil
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("could not clone %s · neither gh nor git is on this machine", full)
	}
	cmd := exec.CommandContext(ctx, git, "clone", "https://github.com/"+full+".git", dir)
	e := factoryEnvironWithoutGitConfig()
	if tok != "" {
		e = append(e,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+factoryBasicAuth(tok))
	}
	cmd.Env = append(e, quiet...)
	return cmd, nil
}

// factoryBasicAuth is the token as git's basic credential for github.com.
func factoryBasicAuth(tok string) string {
	return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok))
}

// factoryEnvironWithoutGitConfig is the process environment without any
// GIT_CONFIG_COUNT/KEY_n/VALUE_n of its own, which the clone's header would
// otherwise be counted among.
func factoryEnvironWithoutGitConfig() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "GIT_CONFIG_COUNT" || strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// factoryCloneWhy is the last line a failed clone printed, with the token
// taken out in both the forms it could be in.
func factoryCloneWhy(out, tok string) string {
	if tok != "" {
		out = strings.ReplaceAll(out, tok, "…")
		out = strings.ReplaceAll(out, factoryBasicAuth(tok), "…")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return strings.TrimPrefix(strings.TrimPrefix(l, "fatal: "), "error: ")
		}
	}
	return ""
}

// factoryNeedsCheckout wraps the seam's Launch so that an item whose
// repository has no checkout on this machine is refused rather than started:
// A RUN NEVER STARTS WITHOUT A CHECKOUT, whatever asked for it. The floor
// asks first and offers the clone ([factory.Snapshot.Checkouts]); this is the
// door's own floor under that, for an ask that did not.
func factoryNeedsCheckout(seam factory.Seam, st *store.Store, dirs func(repo string) string) factory.Seam {
	if seam.Launch == nil || st == nil || dirs == nil {
		return seam
	}
	launch := seam.Launch
	seam.Launch = func(id int) error {
		it, err := st.Get(id)
		if err == nil && strings.TrimSpace(it.Repo) != "" && dirs(it.Repo) == "" {
			return fmt.Errorf("not run · codeaf does not know where %s is checked out", strings.TrimSpace(it.Repo))
		}
		return launch(id)
	}
	return seam
}
