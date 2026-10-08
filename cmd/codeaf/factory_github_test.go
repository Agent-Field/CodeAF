package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The GitHub source exists only with both halves: a watched repository and a
// token. Either one missing builds nothing, so nothing polls.
func TestFactoryGitHubNeedsReposAndAToken(t *testing.T) {
	ctx := context.Background()
	token := func(context.Context) string { return "tok" }
	none := func(context.Context) string { return "" }
	asked := false
	spy := func(context.Context) string { asked = true; return "tok" }

	if factoryGitHub(ctx, nil, token) != nil {
		t.Fatal("no store built a source")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if factoryGitHub(ctx, st, spy) != nil {
		t.Fatal("a floor watching nothing built a source")
	}
	if asked {
		t.Fatal("a floor watching nothing asked for a token; the lookup can run gh")
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	if factoryGitHub(ctx, st, none) != nil {
		t.Fatal("no token built a source")
	}
	src := factoryGitHub(ctx, st, token)
	if src == nil {
		t.Fatal("repositories and a token built no source")
	}
	if got := src.Watched(); len(got) != 1 || got[0] != "acme/api" {
		t.Fatalf("the source watches %v", got)
	}
}

// With no poll keeping its record fresh, the floor's sources are the chat and
// the terminal and nothing else; a live record adds github.
func TestFactorySeamNamesGitHubOnlyWhileItPolls(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SetRepos([]string{"acme/api"})
	snap, err := factorySeam(st, "", "").Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range snap.Sources {
		if src.Name == "github" {
			t.Fatal("github was named with no poll behind it")
		}
	}
	now := time.Now()
	_ = st.SetSourceMeta("github", store.SourceMeta{Polled: now, Tried: now})
	snap, _ = factorySeam(st, "", "").Load()
	found := false
	for _, src := range snap.Sources {
		found = found || src.Name == "github"
	}
	if !found {
		t.Fatalf("a live poll's record did not name github: %+v", snap.Sources)
	}
}

// startFactoryPoll over no store starts nothing and does not spend the once.
func TestFactoryPollOverNoStoreStartsNothing(t *testing.T) {
	startFactoryPoll(nil, "")
	ran := false
	factoryPoll.Do(func() { ran = true })
	if !ran {
		t.Fatal("a nil store spent the process's one poll")
	}
}

// A process with no poll yet looks again on its own clock: nothing is built
// while there are no repositories or no token, and the source appears on the
// first look after both exist, with no relaunch. The token is not asked for
// while nothing is watched, because that lookup can run gh.
func TestWaitForFactoryGitHubWaitsForReposAndAToken(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	have, asked := "", 0
	token := func(context.Context) string {
		mu.Lock()
		defer mu.Unlock()
		asked++
		return have
	}
	got := make(chan *factorygithub.Source, 1)
	go func() { got <- waitForFactoryGitHub(context.Background(), st, token, 5*time.Millisecond) }()

	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	if asked != 0 {
		mu.Unlock()
		t.Fatal("the token was asked for while nothing was watched")
	}
	mu.Unlock()
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	select {
	case <-got:
		t.Fatal("a source was built with repositories and no token")
	default:
	}
	mu.Lock()
	have = "tok"
	mu.Unlock()
	select {
	case src := <-got:
		if src == nil || len(src.Watched()) != 1 {
			t.Fatalf("source = %v", src)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no source after repositories and a token both existed")
	}
}

// A cancelled wait answers nil and does not leak.
func TestWaitForFactoryGitHubStopsWithItsContext(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *factorygithub.Source, 1)
	go func() { done <- waitForFactoryGitHub(ctx, st, nil, time.Hour) }()
	cancel()
	select {
	case src := <-done:
		if src != nil {
			t.Fatal("a cancelled wait built a source")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the wait outlived its context")
	}
}

// With a store, the seam carries the picker's list and the three connection
// doors; with none (and for the fixture) it carries none of them.
func TestFactorySeamHangsTheConnectionDoorsOnlyOverAStore(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factorySeam(st, "", t.TempDir())
	for _, door := range []string{"repos", "github", "connectgithub", "refresh", "refreshall", "open"} {
		if !seam.Has(door) {
			t.Fatalf("a floor over a store has no %q door", door)
		}
	}
	if seam.GHLogin == nil {
		t.Fatal("no ghlogin door")
	}
	// The ordinary launch's profile is the empty string, which means the codeaf
	// home; it must hang the doors too.
	if !factorySeam(st, "", "").Has("github") {
		t.Fatal("an empty (ordinary) profile hung no connection doors")
	}
	bare := factorySeam(nil, "", t.TempDir())
	for _, door := range []string{"repos", "github", "connectgithub", "refresh", "refreshall", "open"} {
		if bare.Has(door) {
			t.Fatalf("a floor over no store has a %q door", door)
		}
	}
	t.Setenv(factoryFixtureEnv, "1")
	if fixture := factorySeam(st, "", t.TempDir()); fixture.Has("github") || fixture.Has("refresh") || fixture.Has("open") {
		t.Fatal("the fixture was wrapped with the connection or source doors")
	}
}

// With nothing watched and no token, a GitHub item's refresh says github is
// not connected, and a terminal item's refresh only takes its read off.
func TestFactorySeamRefreshWithNoGitHub(t *testing.T) {
	t.Setenv(factoryFixtureEnv, "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factorySeam(st, "", t.TempDir())
	ctx := context.Background()
	forgeID, _ := st.Add(ctx, factory.Item{Title: "from github", Repo: "api", Product: "acme", Num: 3, Origin: factory.OriginForge})
	mineID, _ := st.Add(ctx, factory.Item{Title: "typed", Repo: "api", Origin: factory.OriginTerminal})
	_ = st.Annotate(mineID, func(it *factory.Item) error { it.Triage.Read = "a read"; return nil })
	if err := seam.Refresh(ctx, forgeID); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("a github item refreshed with no github: %v", err)
	}
	if err := seam.Refresh(ctx, mineID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(mineID); got.Triage.Read != "" {
		t.Fatal("a terminal item's refresh kept its read")
	}
}

// The picker's save wakes a process waiting for repositories at once, not a
// factoryWatchEvery later: THE FIRST READ STARTS ON SAVE.
func TestThePickersSaveWakesTheWaitAtOnce(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token := func(context.Context) string { return "tok" }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan *factorygithub.Source, 1)
	go func() { got <- waitForFactoryGitHub(ctx, st, token, time.Hour) }()
	time.Sleep(20 * time.Millisecond)
	seam := nudgeOnWatch(factory.Seam{SetRepos: st.SetRepos}, st)
	if err := seam.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	select {
	case src := <-got:
		if src == nil || len(src.Watched()) != 1 {
			t.Fatalf("source = %v", src)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the save did not wake the wait")
	}
	if st.PollNudged().IsZero() {
		t.Fatal("the save left no mark for a poll in another process")
	}
}
