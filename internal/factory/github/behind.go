package github

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// FirstWait is how long a read that only shows something waits for GitHub
// before it answers with what it has. A READ NEVER WAITS ON GITHUB LONGER: a
// gh or forge that did not answer once held every read of the factory on this
// Mac until the app called it "not answering". GitHub's answer lands for the
// next read. Every such read in the factory uses this one bound.
const FirstWait = 150 * time.Millisecond

// askWait bounds one ask of GitHub made behind the reads.
const askWait = 30 * time.Second

// behind runs at most one ask per key in the background, so the reads that
// need the same answer share one ask instead of each waiting on GitHub.
type behind struct {
	mu  sync.Mutex
	out map[string]*ask
}

// ask is one ask that is out, and when it began.
type ask struct {
	done  chan struct{}
	began time.Time
}

// start runs fn under its own bounded context unless an ask for key is out,
// and answers the one that is out. fn's context is cancelled after [askWait].
func (b *behind) start(key, name string, fn func(context.Context)) *ask {
	b.mu.Lock()
	defer b.mu.Unlock()
	if a := b.out[key]; a != nil {
		return a
	}
	if b.out == nil {
		b.out = map[string]*ask{}
	}
	a := &ask{done: make(chan struct{}), began: time.Now()}
	b.out[key] = a
	guard.Go(name, func() {
		ctx, cancel := context.WithTimeout(context.Background(), askWait)
		defer cancel()
		fn(ctx)
		b.mu.Lock()
		delete(b.out, key)
		b.mu.Unlock()
		close(a.done)
	})
	return a
}

// wait waits for the ask to finish, at most bound from when it began (zero
// waits for it to finish), and answers whether it finished.
func (a *ask) wait(ctx context.Context, bound time.Duration) bool {
	var late <-chan time.Time
	if bound > 0 {
		late = time.After(time.Until(a.began.Add(bound)))
	}
	select {
	case <-a.done:
		return true
	case <-late:
	case <-ctx.Done():
	}
	return false
}

type showOnlyKey struct{}

// ShowOnly marks ctx as a read that only shows what GitHub said (the queue, a
// job's card), never one that decides by it: such a read waits on GitHub at
// most [FirstWait].
func ShowOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, showOnlyKey{}, true)
}

func showOnly(ctx context.Context) bool {
	only, _ := ctx.Value(showOnlyKey{}).(bool)
	return only
}
