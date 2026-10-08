package main

import (
	"context"
	"testing"
	"time"

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
	snap, err := factorySeam(st, "").Load()
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
	snap, _ = factorySeam(st, "").Load()
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
	startFactoryPoll(nil)
	ran := false
	factoryPoll.Do(func() { ran = true })
	if !ran {
		t.Fatal("a nil store spent the process's one poll")
	}
}
