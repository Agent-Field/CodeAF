package main

import (
	"context"
	"sync"
	"testing"
	"time"

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
	for _, door := range []string{"repos", "github", "connectgithub"} {
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
	for _, door := range []string{"repos", "github", "connectgithub"} {
		if bare.Has(door) {
			t.Fatalf("a floor over no store has a %q door", door)
		}
	}
	t.Setenv(factoryFixtureEnv, "1")
	if factorySeam(st, "", t.TempDir()).Has("github") {
		t.Fatal("the fixture was wrapped with the connection doors")
	}
}
