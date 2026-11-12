package store

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestWatchedReposRoundTripAndStayOffTheFloor(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.Repos(); err != nil || len(got) != 0 {
		t.Fatalf("a fresh floor watches %v, %v", got, err)
	}
	if err := st.SetRepos([]string{"acme/web", " acme/api ", "ACME/api"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Repos()
	if err != nil || len(got) != 2 || got[0] != "acme/api" || got[1] != "acme/web" {
		t.Fatalf("watched %v, %v", got, err)
	}
	if err := st.SetRepos([]string{"not a repo"}); err == nil {
		t.Fatal("a repository that is not owner/name was kept")
	}
	if err := st.SetSourceMeta("github", SourceMeta{Polled: time.Unix(10, 0).UTC(), Trouble: "not reachable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(factory.Item{Title: "one", Repo: "api"}); err != nil {
		t.Fatal(err)
	}
	items, err := st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("the floor read %d items with repos.json and sources.json beside them: %+v", len(items), items)
	}
	// A hand-written bare array is how a person says which repositories to
	// watch until the picker exists.
	if err := os.WriteFile(st.ReposPath(), []byte(`["acme/cli", "bad"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Repos(); err != nil || len(got) != 1 || got[0] != "acme/cli" {
		t.Fatalf("a bare array read as %v, %v", got, err)
	}
}

func TestSourceMetaKeepsEachSource(t *testing.T) {
	st, _ := Open(t.TempDir())
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	_ = st.SetSourceMeta("github", SourceMeta{Polled: at, Tried: at})
	_ = st.SetSourceMeta("gitlab", SourceMeta{Trouble: "token refused", Tried: at})
	gh, err := st.SourceMeta("github")
	if err != nil || !gh.Polled.Equal(at) || gh.Trouble != "" {
		t.Fatalf("github %+v %v", gh, err)
	}
	gl, _ := st.SourceMeta("gitlab")
	if gl.Trouble != "token refused" {
		t.Fatalf("gitlab %+v", gl)
	}
	if none, err := st.SourceMeta("linear"); err != nil || !none.Tried.IsZero() {
		t.Fatalf("a source that never polled %+v %v", none, err)
	}
}

func TestOnlyOnePollerAtATime(t *testing.T) {
	st, _ := Open(t.TempDir())
	release, ok := st.TryPoller()
	if !ok {
		t.Fatal("the first poller was refused")
	}
	if _, again := st.TryPoller(); again {
		t.Fatal("a second poller took the lock while the first held it")
	}
	release()
	release2, ok := st.TryPoller()
	if !ok {
		t.Fatal("the lock was not given back")
	}
	release2()
}
