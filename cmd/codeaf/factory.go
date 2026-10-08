package main

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
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

// recipeDoor is the chat's door onto a repository's recipe file, over the same
// store and workspace the floor reads, or nil.
//
// A NIL STORE IS A NIL DOOR, for [factoryDoor]'s reason, and for one more: the
// door finds a repository's checkout through the store's record of it
// ([factoryRepoDirs]), which is the same answer the local seam reads the recipe
// from, so the card can never bank a line into a file the floor does not read.
func recipeDoor(st *store.Store, workspace string) session.RecipeDoor {
	if st == nil {
		return nil
	}
	return fileRecipeDoor{dirs: factoryRepoDirs(st, workspace)}
}

// fileRecipeDoor writes one line into a repository's `.codeaf/factory.md`.
type fileRecipeDoor struct {
	dirs func(repo string) string
}

// dir is where repo is checked out, or the person's sentence for why the line
// cannot be written: an unknown checkout is a refusal, never a guess.
func (d fileRecipeDoor) dir(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if dir := d.dirs(repo); dir != "" {
		return dir, nil
	}
	return "", errors.New("codeaf does not know where " + repo + " is checked out; open codeaf there once")
}

// BankStage adds one stage to kind's section: the line is read with the file's
// own reader, and a stage of the same name already there is replaced where it
// stands, so the section gains exactly one line or changes exactly one.
//
// THE SECTION IS WRITTEN WHOLE, through [factory.BankRecipeStages], which is
// the floor's own `b`: a kind the file has no section for starts from the
// stages that kind runs today, so adding a stage never quietly drops the rest.
func (d fileRecipeDoor) BankStage(_ context.Context, repo string, kind factory.Kind, line string) error {
	dir, err := d.dir(repo)
	if err != nil {
		return err
	}
	parsed, problems := factory.Parse("## " + string(kind) + "\n1. " + line)
	if len(problems) > 0 {
		return errors.New(problems[0].Why)
	}
	add := parsed.For(kind)
	if len(add) != 1 {
		return errors.New("a stage is one line: N. name · kind · ask · knobs")
	}
	recipe, _, err := factory.Load(dir)
	if err != nil {
		return err
	}
	stages := factory.CopyStages(recipe.For(kind))
	if i := factory.StageIndex(stages, add[0].Name); i >= 0 {
		stages[i] = add[0]
	} else {
		stages = append(stages, add[0])
	}
	return factory.BankRecipeStages(dir, kind, stages)
}

// BankPolicy adds one sentence under `## policy`.
func (d fileRecipeDoor) BankPolicy(_ context.Context, repo, sentence string) error {
	dir, err := d.dir(repo)
	if err != nil {
		return err
	}
	return factory.BankRecipePolicy(dir, sentence)
}

// BankHabit adds one sentence under `## habits`.
func (d fileRecipeDoor) BankHabit(_ context.Context, repo, sentence string) error {
	dir, err := d.dir(repo)
	if err != nil {
		return err
	}
	return factory.BankRecipeHabit(dir, sentence)
}

// factorySeam is the factory page's seam for this launch.
//
// THE PERSON'S OWN FLOOR IS THE ORDINARY ANSWER: [factory.LocalSeam] over the
// store this process opened, which holds the items made from chat and from `n`
// on the floor, and whose every engine door is nil, so the page offers no
// launch it cannot do. A nil store is the zero seam, whose page is the one dim
// line saying nothing is connected yet.
//
// One switch stands in front of it and wins over the store when it is on:
// CODEAF_FACTORY_FIXTURE=1 hands the page a still fixture
// ([factory.FixtureSeam]) whose every verb is nil.
func factorySeam(st *store.Store, workspace, profileDir string) factory.Seam {
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
	//
	// THE PICKER AND THE CONNECT DOORS ARE HUNG HERE and only here: the local
	// seam is handed the forge's list over the profile's token
	// (factorygithub.Lister), and the result is wrapped with Connect so the
	// `github`, `ghlogin` and `connectgithub` doors exist. The fixture seam
	// above returns before this line and is never wrapped.
	profileDir = factoryProfile(profileDir)
	// AND THE ITEM'S OWN CONVERSATION (`T`, factory_talk.go) is made on this
	// machine, so its door is hung only here, where the floor is this
	// machine's: --once and --host never build this seam.
	local := factory.LocalSeam(st, time.Now(),
		// THE RUNNER'S DOORS ARE HUNG IN EVERY WINDOW (factory_run.go): in the
		// process that runs the floor they are the runner's own, and in every
		// other window the same eight are asks posted to that process's
		// mailbox and answered within about a second.
		factoryRunnerDoors(st),
		factory.WithRepoDirs(factoryRepoDirs(st, workspace)),
		factory.WithRepoLister(factorygithub.Lister(profileDir)),
		factory.WithTalk(talkMaker(st, workspace, profileDir)),
		// THE REFRESH DOORS (`u`, `U`, `g`) read through a GitHub source built
		// at the moment of asking over the profile's token and the watched
		// repositories, because the poll may be running in another process
		// and a token connected a minute ago is the one to use. With no token
		// or nothing watched, a GitHub item's refresh says github is not
		// connected; a terminal or chat item is only read again.
		factory.WithRefetch(factorygithub.Refetcher(st, func(ctx context.Context) *factorygithub.Source {
			return factoryGitHub(ctx, st, func(ctx context.Context) string {
				t, _ := factorygithub.TokenAt(ctx, profileDir)
				return t
			})
		})))
	local = talkPutAway(local, st, profileDir)
	// AND THE FOREMAN (`m`, factory_foreman.go), on the Talk door's law.
	local = withForeman(local, st, workspace, profileDir)
	return nudgeOnWatch(factorygithub.Connect(factorygithub.Facts(local, st, nil), profileDir), st)
}

// nudgeOnWatch wraps the picker's save so that, once repos.json is written,
// the poll on this machine reads at once ([factorygithub.Nudge]) instead of on
// its next tick: THE FIRST READ STARTS ON SAVE. On the ordinary launch the
// window saves and the engine polls, and the nudge reaches it as the store's
// `poll-now` mark, which a waiting poll looks at every second. A failed save
// nudges nothing.
func nudgeOnWatch(seam factory.Seam, st *store.Store) factory.Seam {
	if seam.SetRepos == nil || st == nil {
		return seam
	}
	set := seam.SetRepos
	seam.SetRepos = func(repos []string) error {
		if err := set(repos); err != nil {
			return err
		}
		factorygithub.Nudge(st)
		return nil
	}
	return seam
}

// factoryProfile is the profile directory the connection doors are hung over.
// AN EMPTY PROFILE IS THE ORDINARY ONE: [config.Config.ProfileDir] is empty
// unless CODEAF_PROFILE_DIR moves it, and every profile reader reads empty as
// the codeaf home. [factorygithub.Connect] declines an empty directory, which
// would leave the ordinary launch with no connect prompt, so the home is named
// here.
func factoryProfile(profileDir string) string {
	if p := strings.TrimSpace(profileDir); p != "" {
		return p
	}
	return home.Join()
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
	guard.Go("factory/record-checkout", func() {
		_ = recordWorkspaceCheckoutNow(st, workspace)
	})
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

// factoryWatchEvery is how often a process with no poll running looks again
// for the two things a poll needs: a watched repository and a token. It is a
// variable so a test can shorten it.
var factoryWatchEvery = 30 * time.Second

// factoryPollEvery is how often the GitHub source is read when nothing is
// failing. The manual says the same figure (factory.md, `## connecting
// github`).
const factoryPollEvery = 60 * time.Second

// factoryGitHub is the GitHub source for this floor, or nil.
//
// NIL IS GITHUB OFF, and it is the answer unless BOTH halves are there: a
// store that watches at least one repository, and a token ([factorygithub.TokenAt]). Without either, nothing is built and
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

// factoryBusyClear is this process's one clearing of stale in-flight marks.
var factoryBusyClear sync.Once

// waitForFactoryGitHub looks for a source now and then every `every` until one
// can be built or ctx ends, and answers nil only when ctx ended first.
//
// THE ORDER IS THE CONSENT: [factoryGitHub] asks the store for repositories
// before it asks for a token, and the token is [factorygithub.TokenAt], which
// reads GH_TOKEN, GITHUB_TOKEN and a token kept in the profile, and runs `gh`
// only after the person has said yes to it on the floor. So a person who has
// not connected, or has connected but watches nothing, costs no `gh` call on
// any tick.
//
// A NUDGE ENDS THE WAIT EARLY ([factorygithub.Nudge]): the picker's save
// nudges, so a person who watches their first repository is read at once and
// not up to [factoryWatchEvery] later.
func waitForFactoryGitHub(ctx context.Context, st *store.Store, token func(context.Context) string, every time.Duration) *factorygithub.Source {
	waker := factorygithub.NewWaker(st)
	for {
		if src := factoryGitHub(ctx, st, token); src != nil {
			return src
		}
		if !waker.Wait(ctx, every) {
			return nil
		}
	}
}

// startFactoryPoll starts this process's GitHub poll over st and returns at
// once: the token lookup can ask `gh`, which may take seconds, and a launch
// never waits on it. ONE POLL PER PROCESS, STOPPED WITH THE PROCESS.
//
// The poll is not started only when the launch finds repositories and a token.
// A small loop ([waitForFactoryGitHub]) looks again every [factoryWatchEvery]
// and starts the poll the first time both exist, so the picker's save (which
// writes repos.json after a yes to gh or a token) brings rows in without a
// relaunch; the save nudges ([nudgeOnWatch]), so neither loop waits its tick. Once the poll is alive it reads repos.json itself on every tick
// (factorygithub.PollOnce). Two processes on one machine share the floor's
// poller lock, so only one of them reads at a time (store.TryPoller). It is
// called only where a person's window on this machine opened the store
// (chatv3.go's interactive launch and engine.go's local hello), so --once, a
// task node, --host and --at never start one.
func startFactoryPoll(st *store.Store, profileDir string) {
	if st == nil {
		return
	}
	// THE CHEAP READ OF NEW ITEMS STARTS BESIDE THE POLL, in the same process
	// and under the same rule, and only once a key resolves
	// (factory_triage.go).
	// A PROCESS STARTING CLEARS THE FLOOR'S IN-FLIGHT MARKS a crashed one left
	// behind (store.ClearBusy), before it starts any work of its own, so no
	// row spins for work nobody is doing.
	factoryBusyClear.Do(func() { _ = st.ClearBusy() })
	startFactoryTriage(st)
	factoryPoll.Do(func() {
		guard.Go("factory/github-poll", func() {
			ctx := context.Background()
			token := func(ctx context.Context) string {
				t, _ := factorygithub.TokenAt(ctx, profileDir)
				return t
			}
			src := waitForFactoryGitHub(ctx, st, token, factoryWatchEvery)
			if src == nil {
				return
			}
			// NOTHING IS LOGGED TO THE SCREEN: in the window the surface owns it,
			// and in the engine stdout is the protocol. A failure is the facts
			// line's `github · not reachable`, read off the store.
			factorygithub.Poll(ctx, src, st, factoryPollEvery, nil)
		})
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
