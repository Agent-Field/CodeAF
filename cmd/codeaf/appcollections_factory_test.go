package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
)

func collectionFactoryRef(factory string, job int) appwire.Member {
	return appwire.Member{Ref: appwire.Ref{Kind: appwire.RefFactoryJob, ID: fmt.Sprint(job), Factory: factory}}
}

func collectionFactoryCreate(t *testing.T, b *appBackend, name string, refs ...appwire.Member) string {
	t.Helper()
	c, err := b.CreateCollection(name)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) > 0 {
		if _, err := b.AddToCollection(c.ID, refs); err != nil {
			t.Fatal(err)
		}
	}
	return c.ID
}

// Contract (b), G1: the moved batch reader resolves nested refs once per job,
// reads distinct factories concurrently, and shares cache entries with expand.
func TestCollectionFactoryBatchCacheAndNestedTargets(t *testing.T) {
	b, _, _, _ := collectionsBackend(t)
	nested := collectionFactoryCreate(t, b, "Nested", collectionFactoryRef("f", 7), collectionFactoryRef("g", 9))
	root := collectionFactoryCreate(t, b, "Root", collectionFactoryRef("f", 7), appwire.Member{Ref: appwire.Ref{Kind: appwire.RefCollection, ID: nested}})
	entered := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var mu sync.Mutex
	calls := map[string]int{}
	b.collectionFactory.queue = func(ctx context.Context, factory string) (api.Queue, error) {
		func() { mu.Lock(); defer mu.Unlock(); calls[factory]++ }()
		entered <- factory
		select {
		case <-release:
		case <-ctx.Done():
			return api.Queue{}, ctx.Err()
		}
		id := 7
		if factory == "g" {
			id = 9
		}
		return api.Queue{Jobs: []api.Job{{ID: id, Num: 1107, Title: "Recorded title", State: "backlog", Repo: "acme/api"}}}, nil
	}
	done := make(chan error, 1)
	guard.Go("app/collection-factory-open-test", func() {
		open, err := b.OpenCollection(root, 3, 200, "")
		if err == nil {
			var visit func([]appwire.Node)
			found := 0
			visit = func(nodes []appwire.Node) {
				for _, node := range nodes {
					if node.Kind == appwire.RefFactoryJob {
						found++
						if node.Target.State != appwire.TargetLive || node.Target.Title != "Recorded title" || node.Target.Status != "backlog" || node.Target.Repo != "acme/api" {
							t.Errorf("target: %+v", node.Target)
						}
					}
					if node.Children != nil {
						visit(node.Children.Members)
					}
				}
			}
			visit(open.Members)
			if found != 3 {
				t.Errorf("resolved %d job refs, want 3", found)
			}
		}
		done <- err
	})
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("factories were not read concurrently")
		}
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("open did not finish")
	}
	if _, err := b.ExpandCollection(root, 3, 200); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["f"] != 1 || calls["g"] != 1 {
		t.Fatalf("queue calls: %v", calls)
	}
}

// Contract (b), G1: missing-job reads have a shared deadline and global bound;
// duplicate refs are asked once and unavailable queues never trigger fallbacks.
func TestCollectionFactoryMissingJobReadsAreBounded(t *testing.T) {
	b, _, _, _ := collectionsBackend(t)
	refs := []appwire.Member{}
	for i := 1; i <= 25; i++ {
		refs = append(refs, collectionFactoryRef("f", i))
	}
	id := collectionFactoryCreate(t, b, "Missing", refs...)
	queueReads, jobReads := 0, 0
	b.collectionFactory.queue = func(ctx context.Context, _ string) (api.Queue, error) {
		queueReads++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > factoryReadTimeout {
			t.Error("queue read has no bounded deadline")
		}
		return api.Queue{}, nil
	}
	b.collectionFactory.job = func(ctx context.Context, _ string, job int) (api.Job, error) {
		jobReads++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("job read has no deadline")
		}
		return api.Job{ID: job, Num: 999, State: "backlog"}, nil
	}
	open, err := b.OpenCollection(id, 1, 200, "")
	live, unknown := 0, 0
	for _, node := range open.Members {
		switch node.Target.State {
		case appwire.TargetLive:
			live++
		case appwire.TargetUnknown:
			unknown++
		}
	}
	if err != nil || queueReads != 1 || jobReads != 20 || live != 20 || unknown != 5 {
		t.Fatalf("bounded fallback: %+v, %v; queue %d jobs %d", open, err, queueReads, jobReads)
	}
	b.collectionFactory.cache = nil
	jobReads = 0
	b.collectionFactory.queue = func(context.Context, string) (api.Queue, error) { return api.Queue{}, errors.New("unreachable") }
	open, err = b.OpenCollection(id, 1, 200, "")
	if err != nil || jobReads != 0 {
		t.Fatalf("unavailable queue triggered fallbacks: %v, %d", err, jobReads)
	}
	for _, node := range open.Members {
		if node.Target.State != appwire.TargetUnknown || node.Target.Status != "unknown" {
			t.Fatalf("unavailable target: %+v", node.Target)
		}
	}
	b.collectionFactory.cache = nil
	b.collectionFactory.queue = func(context.Context, string) (api.Queue, error) { return api.Queue{}, nil }
	jobReads = 0
	reads := b.factoryJobReads([]factoryJob{{Factory: "f", Number: 7}, {Factory: "f", Number: 7}})
	if len(reads) != 1 || jobReads != 1 {
		t.Fatalf("duplicate job read %d times: %v", jobReads, reads)
	}
}

// Contract (b), G1: a missing job is deleted, a queue error or a wrong returned
// job is unknown, and a short cache shares both successes and failures.
func TestCollectionFactoryDeletedUnknownAndCacheExpiry(t *testing.T) {
	b, _, _, _ := collectionsBackend(t)
	id := collectionFactoryCreate(t, b, "Targets", collectionFactoryRef("f", 7))
	clock := time.Now()
	b.now = func() time.Time { return clock }
	calls := 0
	b.collectionFactory.queue = func(context.Context, string) (api.Queue, error) { calls++; return api.Queue{}, nil }
	b.collectionFactory.job = func(context.Context, string, int) (api.Job, error) {
		return api.Job{}, appwire.Errorf(appwire.CodeNotFound, "gone")
	}
	assertTarget := func(state string) {
		t.Helper()
		open, err := b.OpenCollection(id, 1, 200, "")
		if err != nil || len(open.Members) != 1 || open.Members[0].Target.State != state {
			t.Fatalf("target: %+v, %v", open, err)
		}
	}
	assertTarget(appwire.TargetDeleted)
	b.collectionFactory.job = func(context.Context, string, int) (api.Job, error) { return api.Job{ID: 8}, nil }
	assertTarget(appwire.TargetUnknown)
	if calls != 1 {
		t.Fatalf("queue was not shared: %d", calls)
	}
	clock = clock.Add(factoryQueueFresh)
	b.collectionFactory.queue = func(context.Context, string) (api.Queue, error) {
		calls++
		return api.Queue{}, errors.New("unreachable")
	}
	assertTarget(appwire.TargetUnknown)
	assertTarget(appwire.TargetUnknown)
	if calls != 2 {
		t.Fatalf("failed queue cache: %d", calls)
	}
	clock = clock.Add(factoryQueueFresh)
	b.collectionFactory.queue = func(context.Context, string) (api.Queue, error) {
		calls++
		return api.Queue{Jobs: []api.Job{{ID: 7, Title: "Back", State: "running"}}}, nil
	}
	assertTarget(appwire.TargetLive)
	if calls != 3 {
		t.Fatalf("queue did not retry after expiry: %d", calls)
	}
}

// factoryQueueWaitContext signals when a coalesced reader selects on Done,
// so the panic cannot race ahead of the waiter entering the flight branch.
type factoryQueueWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *factoryQueueWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

// Contract (b), G1: the moved guarded reader releases its flight on panic,
// caches the unavailable result and permits a successful retry after expiry.
func TestCollectionFactoryPanicFinishesFlight(t *testing.T) {
	b, _, _, _ := collectionsBackend(t)
	id := collectionFactoryCreate(t, b, "Panic", collectionFactoryRef("f", 7))
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	b.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int32
	b.collectionFactory.queue = func(ctx context.Context, _ string) (api.Queue, error) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
				panic("queue reader fault")
			case <-ctx.Done():
				return api.Queue{}, ctx.Err()
			}
		}
		return api.Queue{Jobs: []api.Job{{ID: 7, State: "backlog"}}}, nil
	}
	b.collectionFactory.job = func(context.Context, string, int) (api.Job, error) {
		t.Error("failed queue triggered fallback")
		return api.Job{}, errors.New("unexpected fallback")
	}
	owner := make(chan appwire.CollectionOpenResult, 1)
	guard.Go("app/collection-factory-panic-owner-test", func() {
		open, err := b.OpenCollection(id, 1, 200, "")
		if err != nil {
			t.Error(err)
		}
		owner <- open
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reader did not enter")
	}
	_, flight, owns := b.collectionFactory.factoryQueueStart("f", b.now())
	if flight == nil || owns {
		t.Fatal("reader did not own a shared flight")
	}
	ctx, cancel := context.WithTimeout(context.Background(), factoryReadTimeout)
	defer cancel()
	waitCtx := &factoryQueueWaitContext{Context: ctx, waiting: make(chan struct{})}
	waiter := make(chan factoryQueueRead, 1)
	guard.Go("app/collection-factory-panic-waiter-test", func() { waiter <- b.factoryQueue(waitCtx, "f") })
	select {
	case <-waitCtx.waiting:
	case <-time.After(time.Second):
		t.Fatal("waiter did not enter flight")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case open := <-owner:
		if len(open.Members) != 1 || open.Members[0].Target.State != appwire.TargetUnknown {
			t.Fatalf("panic target: %+v", open)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not finish")
	}
	select {
	case <-flight:
	default:
		t.Fatal("panic left flight open")
	}
	select {
	case read := <-waiter:
		if read.err == nil || errors.Is(read.err, context.DeadlineExceeded) || !read.at.Equal(b.now()) {
			t.Fatalf("cached failure: %+v", read)
		}
	case <-time.After(time.Second):
		t.Fatal("panic stranded waiter")
	}
	entry, flight, owns := b.collectionFactory.factoryQueueStart("f", b.now())
	if entry.err == nil || flight != nil || owns || calls.Load() != 1 {
		t.Fatal("panic did not cache failure and release flight")
	}
	clock.Add((factoryQueueFresh - time.Millisecond).Milliseconds())
	if got := b.factoryQueue(ctx, "f"); got.err == nil || calls.Load() != 1 {
		t.Fatalf("failure not cached: %+v", got)
	}
	clock.Add(1)
	if got := b.factoryQueue(ctx, "f"); got.err != nil || got.jobs[7].ID != 7 || calls.Load() != 2 {
		t.Fatalf("retry after expiry: %+v", got)
	}
}
