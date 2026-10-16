package credits

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeHTTP answers by path with no socket. A path not in the table is a test
// failure's 599.
type fakeHTTP struct {
	mu    sync.Mutex
	paths []string
	reply map[string]func(*http.Request) (*http.Response, error)
}

func (f *fakeHTTP) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.Path)
	h := f.reply[r.URL.Path]
	f.mu.Unlock()
	if h == nil {
		return text(599, "no such path"), nil
	}
	return h(r)
}

func text(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func answer(status int, body string) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) { return text(status, body), nil }
}

func detailOf(t *testing.T, f *fakeHTTP) (Detail, error) {
	t.Helper()
	return ReadDetail(context.Background(), &http.Client{Transport: f}, "https://fake.invalid/api/v1/", "k")
}

func TestReadDetailKeepsAccountAndKeyLimitApart(t *testing.T) {
	f := &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
		"/api/v1/key":     answer(200, `{"data":{"limit_remaining":3.5}}`),
		"/api/v1/credits": answer(200, `{"data":{"total_credits":10,"total_usage":2.25}}`),
	}}
	d, err := detailOf(t, f)
	if err != nil || d.AccountRemaining == nil || *d.AccountRemaining != 7.75 || d.KeyLimitRemaining == nil || *d.KeyLimitRemaining != 3.5 {
		t.Fatalf("%+v %v", d, err)
	}
	if got, ok := d.Remaining(); !ok || got != 3.5 {
		t.Fatalf("effective %v %v, want the lesser, 3.5", got, ok)
	}
	if len(f.paths) != 2 {
		t.Fatalf("one implementation, two GETs: %v", f.paths)
	}
}

func TestReadDetailNeverFabricatesAMissingNumber(t *testing.T) {
	for name, credits := range map[string]string{
		"no usage":   `{"data":{"total_credits":10}}`,
		"no total":   `{"data":{"total_usage":1}}`,
		"null total": `{"data":{"total_credits":null,"total_usage":1}}`,
		"forbidden":  "",
	} {
		status := 200
		if credits == "" {
			status = 403
		}
		f := &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
			"/api/v1/key":     answer(200, `{"data":{"limit_remaining":null}}`),
			"/api/v1/credits": answer(status, credits),
		}}
		d, err := detailOf(t, f)
		if err != nil || d.Known() {
			t.Fatalf("%s: %+v %v, want nothing known and no error", name, d, err)
		}
	}
}

func TestReadDetailKeepsANegativeBalanceAsReported(t *testing.T) {
	f := &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
		"/api/v1/key":     answer(200, `{"data":{"limit_remaining":-0.5}}`),
		"/api/v1/credits": answer(200, `{"data":{"total_credits":1,"total_usage":3}}`),
	}}
	d, _ := detailOf(t, f)
	if *d.AccountRemaining != -2 || *d.KeyLimitRemaining != -0.5 {
		t.Fatalf("%+v", d)
	}
	if got, _ := d.Remaining(); got != -2 {
		t.Fatalf("effective %v", got)
	}
}

func TestReadDetailRefusesNonFiniteAndMalformedFigures(t *testing.T) {
	for name, body := range map[string]string{
		"overflow subtraction": `{"data":{"total_credits":1.7e308,"total_usage":-1.7e308}}`,
		"not a number":         `{"data":{"total_credits":"NaN","total_usage":1}}`,
		"not json":             `<html>`,
	} {
		f := &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
			"/api/v1/key":     answer(200, `{"data":{}}`),
			"/api/v1/credits": answer(200, body),
		}}
		if d, err := detailOf(t, f); err == nil || d.Known() || Classify(err) != KindBadResponse {
			t.Fatalf("%s: %+v %v (%s)", name, d, err, Classify(err))
		}
	}
}

func TestReadDetailBodyCapIsABadResponse(t *testing.T) {
	f := &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
		"/api/v1/key": answer(200, strings.Repeat("x", 64<<10+1)),
	}}
	if _, err := detailOf(t, f); !errors.Is(err, ErrTooLarge) || Classify(err) != KindBadResponse {
		t.Fatalf("%v", err)
	}
	exact := `{"data":{"limit_remaining":1}}`
	f.reply["/api/v1/key"] = answer(200, exact+strings.Repeat(" ", 64<<10-len(exact)))
	f.reply["/api/v1/credits"] = answer(403, "")
	if d, err := detailOf(t, f); err != nil || d.KeyLimitRemaining == nil {
		t.Fatalf("a body of exactly the cap is read: %+v %v", d, err)
	}
}

func TestReadDetailExpiredKeyAndRejectedKey(t *testing.T) {
	f := &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
		"/api/v1/key": answer(401, `{"error":{"message":"API key expired."}}`),
	}}
	if d, err := detailOf(t, f); err != nil || !d.Expired || !d.Known() {
		t.Fatalf("%+v %v", d, err)
	}
	f.reply["/api/v1/key"] = answer(401, `{"error":{"message":"No auth credentials found"}}`)
	_, err := detailOf(t, f)
	if Classify(err) != KindKeyRejected || err.Error() != "key credits answered 401" {
		t.Fatalf("%v %s", err, Classify(err))
	}
	// The old verdict API says the same things it always did.
	got, err := Read(context.Background(), &http.Client{Transport: &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
		"/api/v1/key": answer(401, `API key expired`),
	}}}, "https://fake.invalid/api/v1", "k")
	if err != nil || got != (Reading{Known: true, Expired: true}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestReadDetailTimeoutAndCancelAreNamed(t *testing.T) {
	hang := &fakeHTTP{reply: map[string]func(*http.Request) (*http.Response, error){
		"/api/v1/key": func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() },
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := ReadDetail(ctx, &http.Client{Transport: hang}, "https://fake.invalid/api/v1", "k")
	if Classify(err) != KindCanceled {
		t.Fatalf("%v", err)
	}
	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(20*time.Millisecond))
	defer dcancel()
	_, err = ReadDetail(dctx, &http.Client{Transport: hang}, "https://fake.invalid/api/v1", "k")
	if Classify(err) != KindTimeout {
		t.Fatalf("%v", err)
	}
	if Classify(newStatusError(503, "x")) != KindProvider || Classify(newStatusError(400, "x")) != KindUnexpected || Classify(errors.New("dial")) != KindUnreachable {
		t.Fatal("classification")
	}
}

// ── the book ────────────────────────────────────────────────────────────────

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func usd(v float64) *float64 { return &v }

func newTestBook() (*Book, *clock) {
	c := &clock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	return NewBook(context.Background(), c.now), c
}

func TestBookSingleflightsConcurrentCallers(t *testing.T) {
	b, _ := newTestBook()
	id := NewIdentity("openrouter", "a", "k1")
	var calls atomic.Int32
	release := make(chan struct{})
	fetch := func(context.Context) (Detail, error) {
		calls.Add(1)
		<-release
		return Detail{AccountRemaining: usd(5)}, nil
	}
	var wg sync.WaitGroup
	snaps := make([]Snapshot, 6)
	for i := range snaps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snaps[i], _ = b.Get(context.Background(), id, fetch)
		}()
	}
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("%d requests for one account", calls.Load())
	}
	for _, s := range snaps {
		if !s.HasGood || *s.Good.AccountRemaining != 5 {
			t.Fatalf("%+v", s)
		}
	}
}

func TestBookWaitersAreBoundedAndLeaveOnTheirOwnContext(t *testing.T) {
	b, _ := newTestBook()
	id := NewIdentity("openrouter", "a", "k1")
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	fetch := func(context.Context) (Detail, error) {
		once.Do(func() { close(started) })
		<-release
		return Detail{AccountRemaining: usd(1)}, nil
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = b.Get(context.Background(), id, fetch) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	var waiters sync.WaitGroup
	for i := 0; i < WaitersMax; i++ {
		waiters.Add(1)
		go func() { defer waiters.Done(); _, _ = b.Get(ctx, id, fetch) }()
	}
	for {
		b.mu.Lock()
		n := b.entries[id].waiting
		b.mu.Unlock()
		if n == WaitersMax {
			break
		}
		time.Sleep(time.Millisecond)
	}
	s, err := b.Get(context.Background(), id, fetch)
	if err != nil || !s.Busy {
		t.Fatalf("the ninth caller waited: %+v %v", s, err)
	}
	cancel()
	waiters.Wait()
	close(release)
	wg.Wait()
}

func TestBookRateLimitsPerAccountEvenAfterAFailure(t *testing.T) {
	b, c := newTestBook()
	id := NewIdentity("openrouter", "a", "k1")
	var calls int
	outcome := func(context.Context) (Detail, error) {
		calls++
		return Detail{}, errors.New("dial")
	}
	s, _ := b.Get(context.Background(), id, outcome)
	if calls != 1 || s.Err == nil || s.LastError.IsZero() {
		t.Fatalf("%+v", s)
	}
	c.add(59 * time.Second)
	s, _ = b.Get(context.Background(), id, outcome)
	if calls != 1 || s.RetryAfter != time.Second {
		t.Fatalf("calls %d, retry %v", calls, s.RetryAfter)
	}
	c.add(time.Second)
	b.Get(context.Background(), id, outcome)
	if calls != 2 {
		t.Fatalf("calls %d after the minute", calls)
	}
}

func TestBookKeepsLastGoodThroughATransientFailureOnly(t *testing.T) {
	b, c := newTestBook()
	id := NewIdentity("openrouter", "a", "k1")
	ok := func(context.Context) (Detail, error) { return Detail{AccountRemaining: usd(4)}, nil }
	b.Get(context.Background(), id, ok)
	c.add(MinGap)
	s, _ := b.Get(context.Background(), id, func(context.Context) (Detail, error) { return Detail{}, newStatusError(503, "x") })
	if !s.HasGood || *s.Good.AccountRemaining != 4 || s.Err == nil || s.LastSuccess.IsZero() || s.LastError.IsZero() {
		t.Fatalf("transient failure lost the last good: %+v", s)
	}
	c.add(MinGap)
	s, _ = b.Get(context.Background(), id, func(context.Context) (Detail, error) { return Detail{}, newStatusError(401, "x") })
	if s.HasGood || Classify(s.Err) != KindKeyRejected {
		t.Fatalf("a rejected key kept its old figures: %+v", s)
	}
	c.add(MinGap)
	b.Get(context.Background(), id, ok)
	c.add(MinGap)
	s, _ = b.Get(context.Background(), id, func(context.Context) (Detail, error) { return Detail{}, nil })
	if s.HasGood {
		t.Fatalf("a reply with no figures kept old ones: %+v", s)
	}
}

func TestBookCredentialChangeDropsTheOldAccountAtOnce(t *testing.T) {
	b, _ := newTestBook()
	old := NewIdentity("openrouter", "a", "k1")
	b.Get(context.Background(), old, func(context.Context) (Detail, error) { return Detail{AccountRemaining: usd(9)}, nil })
	// No minute has passed; a new key is asked immediately and sees nothing of the old.
	var saw bool
	s, _ := b.Get(context.Background(), NewIdentity("openrouter", "a", "k2"), func(context.Context) (Detail, error) {
		saw = true
		return Detail{}, errors.New("dial")
	})
	if !saw || s.HasGood || b.Len() != 1 {
		t.Fatalf("saw=%v len=%d %+v", saw, b.Len(), s)
	}
	// A request in flight for the old key is cancelled and its answer discarded.
	b2, _ := newTestBook()
	started := make(chan struct{})
	res := make(chan Snapshot, 1)
	go func() {
		s, _ := b2.Get(context.Background(), old, func(ctx context.Context) (Detail, error) {
			close(started)
			<-ctx.Done()
			return Detail{AccountRemaining: usd(9)}, nil
		})
		res <- s
	}()
	<-started
	b2.Get(context.Background(), NewIdentity("openrouter", "a", "k2"), func(context.Context) (Detail, error) { return Detail{}, nil })
	if s := <-res; s.HasGood || !s.Busy {
		t.Fatalf("the old account's answer escaped: %+v", s)
	}
}

func TestBookIsBoundedAndEvictsOnlyIdleEntries(t *testing.T) {
	b, _ := newTestBook()
	ok := func(context.Context) (Detail, error) { return Detail{AccountRemaining: usd(1)}, nil }
	for i := 0; i < BookMax+5; i++ {
		b.Get(context.Background(), NewIdentity(string(rune('a'+i)), "a", "k"), ok)
		if b.Len() > BookMax {
			t.Fatalf("book holds %d", b.Len())
		}
	}
	// Every slot busy: a new account is refused, not queued.
	full, _ := newTestBook()
	release := make(chan struct{})
	started := make(chan struct{}, BookMax)
	var wg sync.WaitGroup
	for i := 0; i < BookMax; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			full.Get(context.Background(), NewIdentity(string(rune('a'+i)), "a", "k"), func(context.Context) (Detail, error) {
				started <- struct{}{}
				<-release
				return Detail{}, nil
			})
		}()
	}
	for i := 0; i < BookMax; i++ {
		<-started
	}
	s, err := full.Get(context.Background(), NewIdentity("zz", "a", "k"), ok)
	if err != nil || !s.Busy || full.Len() != BookMax {
		t.Fatalf("%+v %v len %d", s, err, full.Len())
	}
	close(release)
	wg.Wait()
}

func TestBookCancelledCallerTakesItsRequestAndDoesNotSpendTheMinute(t *testing.T) {
	b, _ := newTestBook()
	id := NewIdentity("openrouter", "a", "k1")
	ctx, cancel := context.WithCancel(context.Background())
	_, err := b.Get(ctx, id, func(c context.Context) (Detail, error) {
		cancel()
		<-c.Done()
		return Detail{}, c.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	var calls int
	s, _ := b.Get(context.Background(), id, func(context.Context) (Detail, error) { calls++; return Detail{AccountRemaining: usd(2)}, nil })
	if calls != 1 || !s.HasGood {
		t.Fatalf("an abandoned attempt used up the minute: %d %+v", calls, s)
	}
}

func TestBookBaseCancelEndsRequestsInFlight(t *testing.T) {
	base, stop := context.WithCancel(context.Background())
	b := NewBook(base, nil)
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Get(context.Background(), NewIdentity("openrouter", "a", "k"), func(c context.Context) (Detail, error) {
			close(started)
			<-c.Done()
			return Detail{}, c.Err()
		})
	}()
	<-started
	stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a closed process left a request running")
	}
}

func TestBookDropForgetsAProvider(t *testing.T) {
	b, _ := newTestBook()
	b.Get(context.Background(), NewIdentity("openrouter", "a", "k"), func(context.Context) (Detail, error) { return Detail{}, nil })
	b.Drop("openrouter")
	if b.Len() != 0 {
		t.Fatal("kept")
	}
}
