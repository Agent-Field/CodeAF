package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryFixtureEnv is the one switch that puts a still fixture on the factory
// page in place of the person's own floor.
const factoryFixtureEnv = "CODEAF_FACTORY_FIXTURE"

// v3FactoryRoot is where the factory's items are kept: the v3 factory folder
// under the codeaf home, one file per item. The manual names it in words
// (internal/manual/chat/factory.md), because "where is it saved" is asked.
func v3FactoryRoot() string { return home.Join("v3", "factory") }

// v3Factory opens the factory store once for this process, or answers nil.
//
// NIL IS THE FACTORY OFF, and every caller reads it that way: no page floor, no
// `factory_add` on the belt. A folder that cannot be made is exactly that case
// — a capability that cannot work is absent, not broken — so the error is
// swallowed here rather than failing somebody's launch over a folder, and
// nothing is written to the screen, which the surface has not taken yet.
func v3Factory() *store.Store {
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		return nil
	}
	return st
}

// factoryDoor is the chat's door onto the same store, as the session takes it.
//
// A NIL STORE IS A NIL DOOR, SPELLED OUT. A typed-nil *store.Store inside the
// interface would read as non-nil to [session.Config]'s belt predicate and put
// `factory_add` on the belt with nothing behind it, so the nil is returned as
// the interface's own zero value and never as a wrapped pointer.
func factoryDoor(st *store.Store) session.FactoryDoor {
	if st == nil {
		return nil
	}
	return st
}

// factorySeam is the factory page's seam for this launch.
//
// THE PERSON'S OWN FLOOR IS THE ORDINARY ANSWER: [factory.LocalSeam] over the
// store this process opened, which holds the items made from chat and from `n`
// on the floor, and whose every engine door is nil, so the page offers no
// launch it cannot do. A nil store is the zero seam, whose page is the one dim
// line saying nothing is connected yet.
//
// Two switches stand in front of it, and both win over the store when they are
// on. The moving mock exists only in a -tags factorymock build with
// CODEAF_FACTORY_MOCK=1 (factorymock.go); CODEAF_FACTORY_FIXTURE=1 hands the
// page a still fixture ([factory.FixtureSeam]) whose every verb is nil.
func factorySeam(st *store.Store, workspace string) factory.Seam {
	// The mock is asked first. The default build has a stub that always
	// answers false, so the shipped binary carries none of it
	// (internal/factory/mock/REMOVING.md).
	if seam, ok := factoryMockSeam(); ok {
		return seam
	}
	if strings.TrimSpace(env.Get(factoryFixtureEnv)) == "1" {
		return factory.FixtureSeam(time.Now())
	}
	if st == nil {
		return factory.Seam{}
	}
	// The floor's facts line names GitHub only while a poll somewhere on this
	// machine is keeping the store's record of it fresh (factorygithub.Facts),
	// which is how the window on the ordinary launch learns what the engine
	// behind it is doing without a word on the wire.
	return factorygithub.Facts(factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(factoryRepoDirs(st, workspace))), st, nil)
}

// factoryRepoDirs answers where a repository is checked out, by the name the
// floor shows. THREE RULES, IN ORDER: the workspace's own folder when repo is
// that folder's name (the person opened codeaf inside it), else the folder the
// store has recorded for it, else "" (unknown, and the default recipe runs).
func factoryRepoDirs(st *store.Store, workspace string) func(repo string) string {
	here := ""
	if w := strings.TrimSpace(workspace); w != "" {
		here = filepath.Clean(w)
	}
	return func(repo string) string {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return ""
		}
		if here != "" {
			base := filepath.Base(here)
			if short := repo[strings.LastIndex(repo, "/")+1:]; base != "." && base != string(filepath.Separator) && (repo == base || short == base) {
				return here
			}
		}
		return st.CheckoutDir(repo)
	}
}

// recordWorkspaceCheckout notes the workspace as a watched repository's
// checkout when its origin remote names a repository in repos.json, so a
// person who opens codeaf inside the repo they watch gets `b` for free. It
// returns at once and never fails the launch: the git call is bounded to two
// seconds and every error is dropped.
func recordWorkspaceCheckout(st *store.Store, workspace string) {
	if st == nil || strings.TrimSpace(workspace) == "" {
		return
	}
	go func() {
		_ = recordWorkspaceCheckoutNow(st, workspace)
	}()
}

func recordWorkspaceCheckoutNow(st *store.Store, workspace string) error {
	repos, err := st.Repos()
	if err != nil || len(repos) == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", workspace, "remote", "get-url", "origin").Output()
	if err != nil {
		return err
	}
	full := remoteRepoName(strings.TrimSpace(string(out)))
	for _, r := range repos {
		if full != "" && strings.EqualFold(r, full) {
			return st.SetCheckout(r, filepath.Clean(workspace))
		}
	}
	return nil
}

// remoteRepoName reads `owner/name` off a GitHub remote URL (https or ssh
// form), or "" when it is not one.
func remoteRepoName(url string) string {
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	i := strings.Index(url, "github.com")
	if i < 0 {
		return ""
	}
	rest := strings.TrimLeft(url[i+len("github.com"):], ":/")
	if !store.RepoName(rest) {
		return ""
	}
	return rest
}

// factoryPollEvery is how often the GitHub source is read when nothing is
// failing. The manual says the same figure (factory.md, `## connecting
// github`).
const factoryPollEvery = 60 * time.Second

// factoryGitHub is the GitHub source for this floor, or nil.
//
// NIL IS GITHUB OFF, and it is the answer unless BOTH halves are there: a
// store that watches at least one repository, and a token (GH_TOKEN, then
// GITHUB_TOKEN, then `gh auth token`). Without either, nothing is built and
// nothing polls, so the floor's facts line says only `terminal · chat`: A
// CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. The token goes into the
// client and nowhere else.
func factoryGitHub(ctx context.Context, st *store.Store, token func(context.Context) string) *factorygithub.Source {
	if st == nil || token == nil {
		return nil
	}
	repos, err := st.Repos()
	if err != nil || len(repos) == 0 {
		return nil
	}
	tok := token(ctx)
	if tok == "" {
		return nil
	}
	return factorygithub.New(forge.NewClient(tok), repos, nil)
}

// factoryPoll is the one GitHub poll this process runs, started at most once.
var factoryPoll sync.Once

// startFactoryPoll starts this process's GitHub poll over st, when there is a
// source to poll, and returns at once: the token lookup can ask `gh`, which
// may take seconds, and a launch never waits on it. ONE POLL PER PROCESS,
// STOPPED WITH THE PROCESS: it runs for the process's life and nothing else
// owns it. Two processes on one machine (two windows, or a window and an
// engine) share the floor's poller lock, so only one of them reads at a time
// (store.TryPoller). It is called only where a person's window on this machine
// opened the store (chatv3.go's interactive launch and engine.go's local
// hello), so --once, a task node, --host and --at never start one.
func startFactoryPoll(st *store.Store) {
	if st == nil {
		return
	}
	factoryPoll.Do(func() {
		go func() {
			ctx := context.Background()
			src := factoryGitHub(ctx, st, forge.Token)
			if src == nil {
				return
			}
			// NOTHING IS LOGGED TO THE SCREEN: in the window the surface owns it,
			// and in the engine stdout is the protocol. A failure is the facts
			// line's `github · not reachable`, read off the store.
			factorygithub.Poll(ctx, src, st, factoryPollEvery, nil)
		}()
	})
}

// factoryHere names the repository new work lands on when the floor itself
// cannot: a floor with no items has no repos yet, so the page's `n` asks for
// one with nothing to offer, and the store rightly refuses work on no
// repository. The answer is this window's workspace, by its folder's name,
// which is the same name `factory_add` is told to use when the person names
// none (internal/session's tools_factory.go). A floor that already has repos
// passes its own, and a seam with no `new` door is handed back untouched.
func factoryHere(seam factory.Seam, workspace string) factory.Seam {
	here := strings.TrimSpace(workspace)
	if seam.New == nil || here == "" {
		return seam
	}
	here = filepath.Base(filepath.Clean(here))
	if here == "." || here == string(filepath.Separator) {
		return seam
	}
	made := seam.New
	seam.New = func(repo, words string) (int, error) {
		if strings.TrimSpace(repo) == "" {
			repo = here
		}
		return made(repo, words)
	}
	return seam
}
