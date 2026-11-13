package github

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

type blockingPeopleForge struct {
	reads       atomic.Int32
	enteredOnce sync.Once
	entered     chan struct{}
	release     chan struct{}
	user        forge.UserProfile
	contents    string
}

func newBlockingPeopleForge() *blockingPeopleForge {
	return &blockingPeopleForge{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (f *blockingPeopleForge) User(context.Context, string, string) (forge.UserProfile, string, error) {
	f.reads.Add(1)
	f.enteredOnce.Do(func() { close(f.entered) })
	<-f.release
	return f.user, "etag-1", nil
}

func (f *blockingPeopleForge) Contents(context.Context, string, string, string, string, string) (string, string, string, error) {
	f.reads.Add(1)
	f.enteredOnce.Do(func() { close(f.entered) })
	<-f.release
	return f.contents, "sha-1", "etag-1", nil
}

func TestAReadThatIgnoresCancellationCannotHoldThePeopleLoadersClose(t *testing.T) {
	was := closeWait
	closeWait = 20 * time.Millisecond
	t.Cleanup(func() { closeWait = was })

	fake := newBlockingPeopleForge()
	fake.user = forge.UserProfile{Name: "Ada Lovelace", AvatarURL: "https://example.invalid/ada.png", HTMLURL: "https://github.com/ada"}
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(fake.release) }) }
	t.Cleanup(unblock)

	loader := NewPeopleLoader(func(context.Context) PeopleAPI { return fake }, nil)
	loader.Known("ada")
	loader.Known("grace")
	<-fake.entered

	loader.mu.Lock()
	done := loader.readDone
	loader.mu.Unlock()

	start := time.Now()
	loader.Close()
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("PeopleLoader.Close waited %s with closeWait=%s", elapsed, closeWait)
	}

	unblock()
	<-done

	if person, ok := loader.Known("ada"); ok || person.Name != "" {
		t.Fatalf("closed loader cached %+v (ok=%v)", person, ok)
	}
	if got := fake.reads.Load(); got != 1 {
		t.Fatalf("loader made %d profile reads, want 1", got)
	}
}

func TestAnOwnersAskThatIgnoresCancellationCannotHoldThePeopleLoadersClose(t *testing.T) {
	was := closeWait
	closeWait = 20 * time.Millisecond
	t.Cleanup(func() { closeWait = was })

	fake := newBlockingPeopleForge()
	fake.contents = "* @ada\n"
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(fake.release) }) }
	t.Cleanup(unblock)

	loader := NewPeopleLoader(func(context.Context) PeopleAPI { return fake }, nil)
	callerCtx, callerCancel := context.WithCancel(context.Background())
	callerCancel()
	if _, err := loader.Owners(callerCtx, "acme/api"); err != nil {
		t.Fatal(err)
	}
	<-fake.entered

	loader.ownersAsk.mu.Lock()
	done := loader.ownersAsk.out["acme/api"].done
	loader.ownersAsk.mu.Unlock()

	start := time.Now()
	loader.Close()
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("PeopleLoader.Close waited %s with closeWait=%s", elapsed, closeWait)
	}

	unblock()
	<-done

	rules, err := loader.Owners(context.Background(), "acme/api")
	if err != nil || len(rules) != 0 {
		t.Fatalf("closed loader cached owners %+v, err %v", rules, err)
	}
	if got := fake.reads.Load(); got != 1 {
		t.Fatalf("loader made %d CODEOWNERS reads, want 1", got)
	}
}

func TestAReadThatIgnoresCancellationCannotHoldThePipelineLoadersClose(t *testing.T) {
	was := closeWait
	closeWait = 20 * time.Millisecond
	t.Cleanup(func() { closeWait = was })

	fake := newBlockingPeopleForge()
	fake.contents = "pipeline:\n  issue: [test]\nsteps:\n  test: run it\n"
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(fake.release) }) }
	t.Cleanup(unblock)

	loader := NewPipelineLoader(func(context.Context) PipelineAPI { return fake })
	callerCtx, callerCancel := context.WithCancel(context.Background())
	callerCancel()
	if _, err := loader.Load(ShowOnly(callerCtx), "acme/api", "", ""); err != nil {
		t.Fatal(err)
	}
	<-fake.entered

	loader.asking.mu.Lock()
	done := loader.asking.out["acme/api"].done
	loader.asking.mu.Unlock()

	start := time.Now()
	loader.Close()
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("PipelineLoader.Close waited %s with closeWait=%s", elapsed, closeWait)
	}

	unblock()
	<-done

	f, err := loader.Load(ShowOnly(context.Background()), "acme/api", "", "")
	if err != nil || f.Source != pipeline.SourceDefaults {
		t.Fatalf("closed loader cached %+v, err %v", f.Source, err)
	}
	loader.mu.Lock()
	repos, blobs := len(loader.repos), len(loader.blobs)
	loader.mu.Unlock()
	if repos != 0 || blobs != 0 {
		t.Fatalf("closed loader kept %d repos and %d blobs", repos, blobs)
	}
	if got := fake.reads.Load(); got != 1 {
		t.Fatalf("loader made %d pipeline reads, want 1", got)
	}
}
