package github

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func drainNudges() {
	for {
		select {
		case <-nudged:
		default:
			return
		}
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A poll waiting out an hour reads again when another process on this
// machine nudges the floor, long before its timer: the window that saved the
// picker is not the process that polls.
func TestANudgeWakesAWaitingPollBeforeItsTimer(t *testing.T) {
	defer func(d time.Duration) { NudgeEvery = d }(NudgeEvery)
	NudgeEvery = 10 * time.Millisecond
	drainNudges()
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/api"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Poll(ctx, New(api, nil, nil), st, time.Hour, nil); close(done) }()
	defer func() { cancel(); <-done }()

	const key = "GET /repos/acme/api/issues"
	waitFor(t, "the first read did not start at once", func() bool { return f.hit(key) == 1 })
	waitFor(t, "the first read did not finish", func() bool {
		m, _ := st.SourceMeta(Name)
		return !m.Tried.IsZero() && !m.Polling
	})
	time.Sleep(30 * time.Millisecond)
	if f.hit(key) != 1 {
		t.Fatal("the poll read again before its timer with nobody nudging")
	}
	other, err := store.Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := other.NudgePoll(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a nudge from another process did not wake the poll", func() bool { return f.hit(key) == 2 })

	// And in this process the nudge is at once, and nudges coalesce.
	waitFor(t, "the second read did not finish", func() bool {
		m, _ := st.SourceMeta(Name)
		return !m.Polling
	})
	time.Sleep(5 * time.Millisecond)
	Nudge(st)
	Nudge(st)
	waitFor(t, "an in-process nudge did not wake the poll", func() bool { return f.hit(key) >= 3 })
}

// A waker answers false when its context ends, and true when its time is up.
func TestWakerWaitsOutItsTimeOrItsContext(t *testing.T) {
	drainNudges()
	st := openStore(t)
	w := NewWaker(st)
	if !w.Wait(context.Background(), time.Millisecond) {
		t.Fatal("a wait that ran its time said its context ended")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if w.Wait(ctx, time.Hour) {
		t.Fatal("a cancelled wait said it woke")
	}
}

// progressWatcher records the source's record each time the read moves on.
type progressWatcher struct {
	*Source
	st   *store.Store
	seen []store.SourceMeta
}

func (p *progressWatcher) SetProgressItems(fn func(full string, done, of, items int)) {
	if fn == nil {
		p.Source.SetProgressItems(nil)
		return
	}
	p.Source.SetProgressItems(func(full string, done, of, items int) {
		fn(full, done, of, items)
		m, _ := p.st.SourceMeta(Name)
		p.seen = append(p.seen, m)
	})
}

// While a read is in flight the source's record says which repository it is
// on and how many of how many are done (at its start and again as each of
// its lists is in), the floor's facts carry it, and all of it is taken off
// with Polling at the end.
func TestProgressIsWrittenPerRepositoryAndClearedAtTheEnd(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/api", "acme/gone"})
	_ = st.SetSourceMeta(Name, store.SourceMeta{Tried: time.Now()})
	w := &progressWatcher{Source: New(api, nil, nil), st: st}
	_, _ = PollOnce(context.Background(), w, st, "", time.Now)
	// acme/api at its start, its issues in, its pull requests in; acme/gone
	// at its start, where its read fails.
	if len(w.seen) != 4 {
		t.Fatalf("progress written %d times, want 4: %+v", len(w.seen), w.seen)
	}
	if m := w.seen[0]; !m.Polling || m.Reading != "acme/api" || m.Read != 0 || m.Of != 2 {
		t.Fatalf("first repository: %+v", m)
	}
	if m := w.seen[2]; m.Reading != "acme/api" || m.Items != 2 {
		t.Fatalf("first repository's lists in: %+v", m)
	}
	if m := w.seen[3]; !m.Polling || m.Reading != "acme/gone" || m.Read != 1 || m.Of != 2 {
		t.Fatalf("second repository: %+v", m)
	}
	m, _ := st.SourceMeta(Name)
	if m.Polling || m.Reading != "" || m.Read != 0 || m.Of != 0 {
		t.Fatalf("the end of the read left its place: %+v", m)
	}
	_ = st.SetPolling(Name, true)
	_ = st.SetReading(Name, "acme/api", 0, 2)
	info, ok := SourceFacts(st, time.Now())
	if !ok || info.Reading != "acme/api" || info.Read != 0 || info.Of != 2 {
		t.Fatalf("the floor's facts did not carry where the read is: %+v", info)
	}
	w.mu.Lock()
	left := w.progress != nil
	w.mu.Unlock()
	if left {
		t.Fatal("the poll left its progress function on the source")
	}
}
