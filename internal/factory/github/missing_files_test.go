package github_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// noFiles is a repository with none of the files the factory looks for. It
// counts every ask: a 404 carries no ETag, so each one costs a counted call.
type noFiles struct {
	mu    sync.Mutex
	asked map[string]int
}

func (f *noFiles) Contents(_ context.Context, _, _, path, _, _ string) (string, string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked[path]++
	return "", "", "", &forge.APIError{StatusCode: http.StatusNotFound}
}

func (f *noFiles) User(context.Context, string, string) (forge.UserProfile, string, error) {
	return forge.UserProfile{}, "", nil
}

func (f *noFiles) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.asked {
		n += c
	}
	return n
}

// idleMinute ticks once a second for a minute on clock, as an intake that
// refreshes every repository on every tick would.
func idleMinute(clock *time.Time, tick func()) {
	for range 60 {
		*clock = clock.Add(time.Second)
		tick()
	}
}

// A repository with no pipeline file is not asked again on every tick: the
// factory read factory.yaml and then factory.md four times a minute.
func TestAMissingPipelineFileIsBelievedForAWhile(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	files := &noFiles{asked: map[string]int{}}
	l := factorygithub.NewPipelineLoader(func(context.Context) factorygithub.PipelineAPI { return files }).WithClock(func() time.Time { return clock })
	idleMinute(&clock, func() { l.Refresh(context.Background(), "acme/api", "") })
	if n := files.total(); n != 2 {
		t.Fatalf("an idle minute asked GitHub %d times for two missing files: %v", n, files.asked)
	}
	clock = clock.Add(factorygithub.MissingFresh)
	l.Refresh(context.Background(), "acme/api", "")
	if n := files.total(); n != 4 {
		t.Fatalf("a stale absence was not asked again: %v", files.asked)
	}
}

// The same for the three places a CODEOWNERS file may be.
func TestAMissingCodeOwnersFileIsBelievedForAWhile(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	files := &noFiles{asked: map[string]int{}}
	l := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI { return files }, func() time.Time { return clock })
	idleMinute(&clock, func() { l.RefreshOwners(context.Background(), "acme/api") })
	if n := files.total(); n != 3 {
		t.Fatalf("an idle minute asked GitHub %d times for three missing files: %v", n, files.asked)
	}
	clock = clock.Add(factorygithub.MissingFresh)
	l.RefreshOwners(context.Background(), "acme/api")
	if n := files.total(); n != 6 {
		t.Fatalf("a stale absence was not asked again: %v", files.asked)
	}
}
