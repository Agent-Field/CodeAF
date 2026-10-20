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
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── THE CLONE DOOR ──────────────────────────────────────────────────────────
//
// A RUN NEVER STARTS WITHOUT A CHECKOUT, AND THE FACTORY GETS ONE ITSELF
// (owner ruling, 2026-10-08: `r` on item #1786 ran with no checkout, skipped
// plan and write, and stopped at test saying codeaf did not know where the
// repository was). The runner's Workdir ([factoryRunWorkdir]) is the caller:
// a repository with no known folder is cloned, on the run's first step, into
// the factory's own folder,
//
//	<v3FactoryRoot>/repos/<owner>/<name>     (~/.codeaf/v3/factory/repos/…)
//
// and the folder is recorded as its checkout (store.SetCheckout), which is
// what [factoryRepoDirs] answers next, for the floor, the recipe page and
// every run alike. THE WORKSPACE RULE STAYS FIRST: a window opened inside a
// watched repository records that folder ([recordWorkspaceCheckout]), and a
// repository with any known folder is never cloned again.
//
// Until 2026-10-20 nothing called this door: the Start of a job on a
// repository the app had never opened was refused for having no checkout,
// every time, from every surface. Now every start asks [factoryCheckoutDoor]
// first, which refuses only what cannot be cloned at all, before anything is
// written, and the runner clones the rest.
//
// The clone is `git clone https://github.com/<owner>/<name>.git` with the
// token the poll and the push use ([factorygithub.TokenAt]) as an http header
// in GIT_CONFIG_*, NEVER `gh repo clone`, which follows the person's own
// git_protocol and may go over ssh with a key the factory does not hold. The
// token reaches the child only through its environment: NEVER ON A COMMAND
// LINE, AND NEVER IN THE WORDS A FAILURE SAYS.

// factoryCloneWait bounds one clone the runner makes: a large repository takes
// minutes, and a clone that never ends must still fail its round.
const factoryCloneWait = 15 * time.Minute

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
		// ONE CLONE OF A REPOSITORY AT A TIME, across every slot and every
		// worker process on this machine: two jobs of one repository started
		// together would otherwise both clone into the one folder, and the
		// loser's failure would take the winner's half-made clone away. The
		// lock is a file's, so it holds between processes too; whoever waited
		// finds the clone recorded and answers it.
		// factoryFileLock always hands out a closer (one that does nothing
		// when it failed), so it is deferred before the error is read.
		release, err := factoryFileLock(dir+".lock", false)
		defer release()
		if err != nil {
			return "", fmt.Errorf("could not clone %s · %v", full, err)
		}
		if dir := st.CheckoutDir(full); dir != "" {
			return dir, nil
		}
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

// factoryCloneCommand is git's clone of full into dir over https, with the
// token as an http header in the child's environment, or the sentence that
// git is not on this machine.
func factoryCloneCommand(ctx context.Context, full, dir, tok string) (*exec.Cmd, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("could not clone %s · git is not on this machine", full)
	}
	cmd := exec.CommandContext(ctx, git, "clone", "https://github.com/"+full+".git", dir)
	e := factoryEnvironWithoutGitConfig()
	if tok != "" {
		e = append(e,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+factoryBasicAuth(tok))
	}
	cmd.Env = append(e, "GIT_TERMINAL_PROMPT=0")
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

// factoryTokenOf is the GitHub token the factory reads, clones and pushes
// with on profileDir, "" for none.
func factoryTokenOf(profileDir string) func(context.Context) string {
	return func(ctx context.Context) string {
		t, _ := factorygithub.TokenAt(ctx, profileDir)
		return t
	}
}

// factoryCheckoutDoor is the seam's CheckoutOf on this machine
// ([factory.Seam.CheckoutOf]): here when dirs knows a folder for repo,
// will_clone when the runner's clone door can get one (a repository the
// factory can name on GitHub, and a token to clone it with), else missing,
// with the sentence saying which of the two it lacks. It reads no network and
// clones nothing: the job's read asks it every time it is read.
func factoryCheckoutDoor(st *store.Store, dirs func(repo string) string, token func(context.Context) string) func(context.Context, string) (factory.CheckoutState, error) {
	return func(ctx context.Context, repo string) (factory.CheckoutState, error) {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return "", nil
		}
		if dirs(repo) != "" {
			return factory.CheckoutHere, nil
		}
		if _, err := factoryCloneName(st, repo); err != nil {
			return factory.CheckoutMissing, factory.NeedsCheckout{Repo: repo, Why: factory.CheckoutNotOnGitHub}
		}
		if token == nil || strings.TrimSpace(token(ctx)) == "" {
			return factory.CheckoutMissing, factory.NeedsCheckout{Repo: repo, Why: factory.CheckoutNeedsGitHub}
		}
		return factory.CheckoutWillClone, nil
	}
}

// factoryCheckouts hangs the checkout door on seam and wraps its Launch so a
// job whose repository is missing is refused rather than started: A RUN NEVER
// STARTS WITHOUT A CHECKOUT, whatever asked for it. A repository the runner
// will clone passes, so the floor's launch, the header's Start (whose service
// asks the same door before it writes) and the manager's `factory_start`
// start the same jobs and refuse the same ones in the same words. EVERY SEAM
// THAT LAUNCHES ON THIS MACHINE'S FLOOR IS MADE THROUGH IT.
func factoryCheckouts(seam factory.Seam, st *store.Store, dirs func(repo string) string, token func(context.Context) string) factory.Seam {
	if st == nil || dirs == nil {
		return seam
	}
	checkout := factoryCheckoutDoor(st, dirs, token)
	seam.CheckoutOf = checkout
	if seam.Launch == nil {
		return seam
	}
	launch := seam.Launch
	seam.Launch = func(id int) error {
		if it, err := st.Get(id); err == nil {
			if _, err := checkout(context.Background(), it.Repo); err != nil {
				return err
			}
		}
		return launch(id)
	}
	return seam
}

// factoryRunWorkdir is the runner's Workdir: the job's own worktree, made from
// its repository's checkout, AND THE CHECKOUT CLONED FIRST WHEN THIS MACHINE
// HAS NONE ([factoryCloneDoor]). The clone says `cloning owner/name…` on the
// job's log while it runs, and a clone that fails fails that round with the
// door's own sentence (could not clone acme/api · repository not found), so
// the job says what went wrong and can be run again.
func factoryRunWorkdir(floor *store.Store, dirs func(repo string) string, workdirs *factoryrun.Workdirs, token func(context.Context) string) func(factory.Item) (string, error) {
	clone := factoryCloneDoor(floor, token)
	return func(it factory.Item) (string, error) {
		checkout := dirs(it.Repo)
		if checkout == "" && strings.TrimSpace(it.Repo) != "" {
			if full, err := factoryCloneName(floor, it.Repo); err == nil && workdirs.Log != nil {
				workdirs.Log(it, "cloning "+full+"…")
			}
			ctx, cancel := context.WithTimeout(context.Background(), factoryCloneWait)
			dir, err := clone(ctx, it.Repo)
			cancel()
			if err != nil {
				return "", err
			}
			checkout = dir
		}
		return workdirs.For(it, checkout)
	}
}
