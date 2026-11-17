package cellsync

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// fetcherFor is a second device's fetcher over store, with its own engine.
func fetcherFor(t *testing.T, store blobstore.Store) (*Fetcher, cell.Cell) {
	t.Helper()
	c := cell.Cell{ID: cellID, Root: t.TempDir()}
	inbox := t.TempDir()
	return &Fetcher{
		Engine: NewFakeEngine(t.TempDir()),
		Store:  store,
		Inbox:  func(c cell.Cell) string { return filepath.Join(inbox, c.ID) },
	}, c
}

func TestFetchMaterializesHead(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "one"})
	head := r.seal(map[string]string{"a": "one", "sub/b": "two"})
	if err := r.pub.Publish(context.Background(), r.drv, head, r.info()); err != nil {
		t.Fatal(err)
	}
	f, c := fetcherFor(t, r.mem)
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "one", "sub/b": "two"}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized %v, want %v", got, want)
	}
}

func TestFetchNamesMissingRid(t *testing.T) {
	r := newRig(t)
	head := r.publishFirst(map[string]string{"a": "one"})
	f, c := fetcherFor(t, blobstore.NewMemory()) // a store that never got the objects
	err := f.Fetch(context.Background(), c, head)
	if !errors.Is(err, blobstore.ErrNotFound) || !strings.Contains(err.Error(), head) {
		t.Fatalf("fetch = %v, want ErrNotFound naming %s", err, head)
	}
}

func TestFetchReportsProgress(t *testing.T) {
	r := newRig(t)
	r.publishFirst(map[string]string{"a": "one"})
	head := r.seal(map[string]string{"a": "one", "b": "two"})
	if err := r.pub.Publish(context.Background(), r.drv, head, r.info()); err != nil {
		t.Fatal(err)
	}
	f, c := fetcherFor(t, r.mem)
	var have, want []int
	f.OnProgress = func(h, w int) { have, want = append(have, h), append(want, w) }
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	if len(have) < 2 {
		t.Fatalf("expected one report per round, got %v of %v", have, want)
	}
	for i := 1; i < len(have); i++ {
		if have[i] <= have[i-1] || want[i] < want[i-1] {
			t.Fatalf("progress went backwards: have %v want %v", have, want)
		}
	}
	if last := len(have) - 1; have[last] != want[last] {
		t.Fatalf("finished with %d of %d", have[last], want[last])
	}
}

// A takeover asks the store once per frame's worth of objects, not once per
// object: three hundred small files are a handful of requests.
func TestFetchAsksOncePerBatchNotOncePerObject(t *testing.T) {
	r := newRig(t)
	files := map[string]string{}
	for i := range 300 {
		files[fmt.Sprintf("f%03d", i)] = fmt.Sprint("body ", i)
	}
	head := r.publishFirst(files)
	f, c := fetcherFor(t, r.mem)
	before := gets(r.mem)
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	objects := 301 // a blob for each file, and the snapshot
	if got, most := gets(r.mem)-before, 2*(objects/blobstore.MaxGetMany+2); got > most {
		t.Fatalf("%d store requests for %d objects, want at most %d", got, objects, most)
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, files) {
		t.Fatalf("materialized %d files, want %d", len(got), len(files))
	}
}

// N21: a fetch from the hosted relay asks for at most ManyMax rids per
// request, still in want order, so a big want list is several smaller requests.
func TestFetchCapsEachRequestAtManyMax(t *testing.T) {
	r := newRig(t)
	files := map[string]string{}
	for i := range 300 {
		files[fmt.Sprintf("f%03d", i)] = fmt.Sprint("body ", i)
	}
	head := r.publishFirst(files)
	f, c := fetcherFor(t, r.mem)
	f.ManyMax = HostedGetMany
	before, logged := gets(r.mem), len(r.mem.Log())
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	objects := 301
	if got, least := gets(r.mem)-before, objects/HostedGetMany; got < least {
		t.Fatalf("%d store requests for %d objects at %d per request, want at least %d", got, objects, HostedGetMany, least)
	}
	for _, op := range r.mem.Log()[logged:] {
		if op.Kind == "get" && len(op.RIDs) > HostedGetMany {
			t.Fatalf("a request asked for %d rids, over the %d cap", len(op.RIDs), HostedGetMany)
		}
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, files) {
		t.Fatalf("materialized %d files, want %d", len(got), len(files))
	}
}

func TestManyMaxForHostedRelayOnly(t *testing.T) {
	was, hosts := syncwords.HostedURL, []string{"fabric.example.test"}
	syncwords.UseHosted("https://fabric.example.test/fabric", hosts)
	t.Cleanup(func() { syncwords.UseHosted(was, nil) })
	for relay, want := range map[string]int{
		"https://fabric.example.test/fabric": HostedGetMany,
		"https://fabric.example.test":        HostedGetMany,
		"https://relay.home.example.test":    blobstore.MaxGetMany,
		"http://127.0.0.1:8443":              blobstore.MaxGetMany,
	} {
		if got := ManyMaxFor(relay); got != want {
			t.Errorf("ManyMaxFor(%q) = %d, want %d", relay, got, want)
		}
	}
	if got := (&Fetcher{ManyMax: 0}).manyMax(); got != blobstore.MaxGetMany {
		t.Errorf("an unset ManyMax asks for %d, want the full %d", got, blobstore.MaxGetMany)
	}
}

// gets counts the get requests a Memory store has answered.
func gets(m *blobstore.Memory) int {
	n := 0
	for _, op := range m.Log() {
		if op.Kind == "get" {
			n++
		}
	}
	return n
}

// roundCountingStore wraps a Store, counts peak concurrent GetMany requests
// and serial waves ("rounds"), and advances a fake clock by perRound on each
// round with no wall-clock sleeps.
type roundCountingStore struct {
	blobstore.Store
	perRound time.Duration

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	rounds      int
	requests    int
	virtualTime time.Duration
}

func (s *roundCountingStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	s.mu.Lock()
	if s.inFlight == 0 {
		s.rounds++
		s.virtualTime += s.perRound
	}
	s.inFlight++
	s.requests++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.mu.Unlock()

	for range 4 {
		runtime.Gosched()
	}
	got, err := s.Store.GetMany(ctx, rids)

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return got, err
}

// C5: Hosted fetches keep at least 4 GetMany batches in flight concurrently
// (bounded by GatherWindow) and take at most a quarter of the serial round
// trips for 2,000 small objects on a fake relay with 300 ms per request.
func TestC5ParallelFetchReducesRoundsAndBoundsInFlight(t *testing.T) {
	r := newRig(t)
	files := make(map[string]string, 2000)
	for i := range 2000 {
		files[fmt.Sprintf("f%04d.txt", i)] = fmt.Sprintf("small object %d", i)
	}
	head := r.publishFirst(files)

	// Baseline: serial fetch (Concurrency=1, today's order).
	serialStore := &roundCountingStore{Store: r.mem, perRound: 300 * time.Millisecond}
	serialFetcher, serialCell := fetcherFor(t, serialStore)
	serialFetcher.ManyMax = HostedGetMany
	serialFetcher.Concurrency = 1
	if err := serialFetcher.Fetch(context.Background(), serialCell, head); err != nil {
		t.Fatal(err)
	}

	// Parallel fetch (default GatherWindow).
	parallelStore := &roundCountingStore{Store: r.mem, perRound: 300 * time.Millisecond}
	f, c := fetcherFor(t, parallelStore)
	f.ManyMax = HostedGetMany
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatal(err)
	}
	if parallelStore.maxInFlight < 4 || parallelStore.maxInFlight > GatherWindow {
		t.Fatalf("max in-flight = %d, want between 4 and %d", parallelStore.maxInFlight, GatherWindow)
	}
	if parallelStore.rounds > serialStore.rounds/4 {
		t.Fatalf("parallel rounds = %d (%s virtual), want <= 1/4 of serial rounds %d (%s virtual)",
			parallelStore.rounds, parallelStore.virtualTime, serialStore.rounds, serialStore.virtualTime)
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, files) {
		t.Fatalf("materialized %d files, want %d", len(got), len(files))
	}
}

type scriptStore struct {
	blobstore.Store
	mu        sync.Mutex
	calls     int
	ridsAsked []string
	decide    func(call int, rids []string) error
}

func (s *scriptStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.ridsAsked = append(s.ridsAsked, rids...)
	var err error
	if s.decide != nil {
		err = s.decide(call, rids)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.Store.GetMany(ctx, rids)
}

// C4: transient failures (5xx, timeouts, connection resets) are retried at
// least 3 times with backoff; 4xx and auth refusals are never retried; a
// retried fetch keeps the objects it already fetched.
func TestC4FetchRetriesTransientAndKeepsFetchedObjects(t *testing.T) {
	r := newRig(t)
	files := make(map[string]string, 40)
	for i := range 40 {
		files[fmt.Sprintf("f%02d", i)] = fmt.Sprintf("payload %d", i)
	}
	head := r.publishFirst(files)

	// 1. A 500 / timeout / connection reset then success is retried and succeeds.
	for _, transientErr := range []error{
		fmt.Errorf("%w: server answered 500", blobstore.ErrUnreachable),
		context.DeadlineExceeded,
		syscall.ECONNRESET,
	} {
		store := &scriptStore{
			Store: r.mem,
			decide: func(call int, _ []string) error {
				if call <= 2 {
					return transientErr
				}
				return nil
			},
		}
		f, c := fetcherFor(t, store)
		f.ManyMax = HostedGetMany
		var sleeps int
		f.Sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
		if err := f.Fetch(context.Background(), c, head); err != nil {
			t.Fatalf("transient %v: Fetch = %v, want nil", transientErr, err)
		}
		if sleeps != 2 {
			t.Fatalf("transient %v: sleeps = %d, want 2", transientErr, sleeps)
		}
	}

	// 2. Persistent failures retry at least 3 times before failing, and a retry
	// of the fetch keeps the objects already fetched in the first attempt.
	var failBatch string
	store := &scriptStore{
		Store: r.mem,
		decide: func(call int, rids []string) error {
			// Let the snapshot (call 1) and the first blob batch (call 2) succeed,
			// then fail the next blob batch across all retries.
			if call >= 3 && (failBatch == "" || rids[0] == failBatch) {
				failBatch = rids[0]
				return fmt.Errorf("%w: server answered 500", blobstore.ErrUnreachable)
			}
			return nil
		},
	}
	f, c := fetcherFor(t, store)
	f.ManyMax = HostedGetMany
	f.Concurrency = 1
	var sleeps int
	f.Sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
	err := f.Fetch(context.Background(), c, head)
	if !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("Fetch with persistent 500 = %v, want ErrUnreachable", err)
	}
	if sleeps < blobstore.FetchRetries {
		t.Fatalf("sleeps = %d, want at least %d retries", sleeps, blobstore.FetchRetries)
	}

	// Now allow the store to succeed and retry Fetch on the same Fetcher/Cell.
	// Neither the snapshot nor the first 16 blobs may be requested again.
	firstAttemptAsked := append([]string(nil), store.ridsAsked...)
	store.mu.Lock()
	store.ridsAsked = nil
	store.decide = nil
	store.mu.Unlock()
	if err := f.Fetch(context.Background(), c, head); err != nil {
		t.Fatalf("retry Fetch = %v, want nil", err)
	}
	if len(store.ridsAsked) >= len(firstAttemptAsked) || len(store.ridsAsked) != 40-HostedGetMany {
		t.Fatalf("retry asked for %d rids, want only the remaining %d (not the %d already fetched)",
			len(store.ridsAsked), 40-HostedGetMany, HostedGetMany+1)
	}
	if got := readFiles(t, c.Root); !reflect.DeepEqual(got, files) {
		t.Fatalf("materialized %d files after retry, want %d", len(got), len(files))
	}

	// 3. A 403 or auth refusal is never retried.
	for _, refusal := range []error{
		wireauth.ErrStorageOff,
		wireauth.ErrUnauthorized,
		errors.New("blobstore: server refused with 403 \"forbidden\""),
	} {
		store := &scriptStore{
			Store:  r.mem,
			decide: func(int, []string) error { return refusal },
		}
		f, c := fetcherFor(t, store)
		var sleeps int
		f.Sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
		if err := f.Fetch(context.Background(), c, head); err == nil {
			t.Fatalf("refusal %v succeeded", refusal)
		}
		if store.calls != 1 || sleeps != 0 {
			t.Fatalf("refusal %v: calls = %d, sleeps = %d; want 1 call and 0 retries", refusal, store.calls, sleeps)
		}
	}
}
