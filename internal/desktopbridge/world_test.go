package desktopbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

const worldToken = "world-test-token"

// fakeWorld is a mutable persisted world and a count of how often it was read.
type fakeWorld struct {
	mu    sync.Mutex
	rows  []session.SessionRow
	reads atomic.Int64
}

func (f *fakeWorld) set(rows ...session.SessionRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = rows
}
func (f *fakeWorld) read() session.World {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return session.World{Projects: []session.Project{{Name: "repo", Path: "/work/repo", Sessions: append([]session.SessionRow(nil), f.rows...)}}}
}

func worldWorking(id string) session.SessionRow {
	return session.SessionRow{ID: id, Title: "t-" + id, Live: true, Open: true, Presence: session.SessionPresence{State: session.PresenceWorking}}
}
func asking(id, text string) session.SessionRow {
	return session.SessionRow{ID: id, Live: true, Open: true, Presence: session.SessionPresence{State: session.PresenceWaiting, Reason: text, Question: session.PresenceQuestion{Kind: "consent", ID: 7, Text: text}}}
}

// manualClock gives the feed tickers the test fires by hand.
type manualClock struct {
	mu    sync.Mutex
	chans map[time.Duration]chan time.Time
}

func (c *manualClock) ticker(d time.Duration) (<-chan time.Time, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chans == nil {
		c.chans = map[time.Duration]chan time.Time{}
	}
	ch := make(chan time.Time, 1)
	c.chans[d] = ch
	return ch, func() {}
}
func (c *manualClock) fire(d time.Duration) {
	c.mu.Lock()
	ch := c.chans[d]
	c.mu.Unlock()
	ch <- time.Now()
}
func (c *manualClock) has(d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chans[d] != nil
}

func testFeed(fw *fakeWorld) (*WorldFeed, *manualClock) {
	f := NewWorldFeed(fw.read)
	clock := &manualClock{}
	f.newTicker = clock.ticker
	f.interval = 2 * time.Second
	f.heartbeat = 15 * time.Second
	f.maxAge = 0
	return f, clock
}

// bridgeFor closes the server in a cleanup, which runs after the stream cleanups
// registered later: an open stream would otherwise block Close after a failure.
func bridgeFor(t *testing.T, f *WorldFeed) *httptest.Server {
	b := New(worldToken, nil)
	b.UseWorld(f)
	srv := httptest.NewServer(b.Handler())
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	return srv
}

type sse struct {
	lines  chan string
	cancel context.CancelFunc
	done   chan struct{}
}

func openStream(t *testing.T, srv *httptest.Server, after string) *sse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/engine/events?after="+after, nil)
	req.Header.Set("Authorization", "Bearer "+worldToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		cancel()
		t.Fatalf("status %d", resp.StatusCode)
	}
	s := &sse{lines: make(chan string, 256), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	t.Cleanup(cancel)
	return s
}

// next returns the next record's data line, or a comment line.
func (s *sse) next(t *testing.T) (WorldRecord, string) {
	t.Helper()
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatal("stream closed")
			}
			if line == "" || strings.HasPrefix(line, "id: ") {
				continue
			}
			if strings.HasPrefix(line, ":") {
				return WorldRecord{}, line
			}
			if strings.HasPrefix(line, "data: ") {
				var rec WorldRecord
				if err := json.Unmarshal([]byte(line[6:]), &rec); err != nil {
					t.Fatal(err)
				}
				return rec, ""
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no record within 3s")
		}
	}
}
func (s *sse) quiet(t *testing.T) {
	t.Helper()
	deadline := time.After(150 * time.Millisecond)
	for {
		select {
		case line := <-s.lines:
			if line != "" {
				t.Fatalf("unexpected line %q", line)
			}
		case <-deadline:
			return
		}
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func get(t *testing.T, srv *httptest.Server, path, auth string, header ...string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestWorldRoutesNeedTheToken(t *testing.T) {
	fw := &fakeWorld{}
	f, _ := testFeed(fw)
	srv := bridgeFor(t, f)
	for _, path := range []string{"/api/engine/world", "/api/engine/events?after=0"} {
		for _, auth := range []string{"", "Bearer nope"} {
			if code := get(t, srv, path, auth).StatusCode; code != 401 {
				t.Fatalf("%s with %q: %d", path, auth, code)
			}
		}
	}
	if fw.reads.Load() != 0 || f.Observers() != 0 {
		t.Fatal("an unauthenticated request reached the disk or attached")
	}
}

// A foreign origin gets no CORS grant and no data; a native origin still does.
func TestWorldKeepsNativeOriginsAndGivesForeignOnesNothing(t *testing.T) {
	fw := &fakeWorld{}
	f, _ := testFeed(fw)
	srv := bridgeFor(t, f)
	for _, origin := range []string{"tauri://localhost", "http://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420"} {
		resp := get(t, srv, "/api/engine/world", "Bearer "+worldToken, "Origin", origin)
		if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != origin {
			t.Fatalf("native origin %s: %d %q", origin, resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
		}
	}
	// The origin guard answers before the token check, so a foreign page is 403
	// and learns nothing, including whether a token would have worked.
	resp := get(t, srv, "/api/engine/world", "", "Origin", "https://evil.example")
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign origin: %d %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestWorldProjectsOnlyCanonicalFacts(t *testing.T) {
	fw := &fakeWorld{}
	closed := session.SessionRow{ID: "c1", Title: "old", At: time.Unix(100, 0)}
	// A window died on a question: the file still says waiting but nobody refreshes it.
	stale := asking("s1", "stale?")
	stale.Live = false
	fw.set(worldWorking("w1"), asking("q1", "Run rm?"), closed, stale)
	f, _ := testFeed(fw)
	srv := bridgeFor(t, f)
	var got struct {
		Seq   uint64
		Rows  []WorldRow
		Items []AttentionItem
	}
	if err := json.NewDecoder(get(t, srv, "/api/engine/world", "Bearer "+worldToken).Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	byID := map[string]WorldRow{}
	for _, r := range got.Rows {
		byID[r.Session] = r
	}
	if len(got.Rows) != 4 {
		t.Fatalf("background and closed sessions must be visible: %+v", got.Rows)
	}
	if !byID["w1"].Running || byID["w1"].NeedsYou || byID["w1"].State != "working" {
		t.Fatalf("working: %+v", byID["w1"])
	}
	if !byID["q1"].NeedsYou || byID["q1"].State != "waiting on you" {
		t.Fatalf("asking: %+v", byID["q1"])
	}
	if c := byID["c1"]; c.State != "closed" || c.Live || c.Running || c.NeedsYou || c.At == "" {
		t.Fatalf("closed: %+v", c)
	}
	if s := byID["s1"]; s.NeedsYou || s.State != "open" {
		t.Fatalf("a stale claim must not ask: %+v", s)
	}
	if len(got.Items) != 1 || got.Items[0].Session != "q1" || got.Items[0].Text != "Run rm?" || got.Items[0].Kind != "consent" || got.Items[0].Key != "q1:consent:7" {
		t.Fatalf("attention: %+v", got.Items)
	}
}

func TestResetFirstThenOnlyDeltas(t *testing.T) {
	fw := &fakeWorld{}
	fw.set(worldWorking("a"))
	f, _ := testFeed(fw)
	srv := bridgeFor(t, f)
	s := openStream(t, srv, "0")
	rec, _ := s.next(t)
	if rec.Type != "reset" {
		t.Fatalf("first record %q", rec.Type)
	}
	var full worldFull
	_ = json.Unmarshal(rec.Payload, &full)
	if len(full.Rows) != 1 || full.Rows[0].Session != "a" {
		t.Fatalf("reset body %+v", full)
	}
	s.quiet(t)
	// An unchanged world, even re-read, says nothing. A presence heartbeat is not news.
	f.refresh()
	s.quiet(t)

	fw.set(worldWorking("a"), worldWorking("b"))
	f.refresh()
	rec, _ = s.next(t)
	var d worldDelta
	_ = json.Unmarshal(rec.Payload, &d)
	if rec.Type != "world" || len(d.Rows) != 1 || d.Rows[0].Session != "b" || len(d.Removed) != 0 {
		t.Fatalf("delta %s %+v", rec.Type, d)
	}
	fw.set(asking("b", "ok?"))
	f.refresh()
	rec1, _ := s.next(t)
	rec2, _ := s.next(t)
	if rec1.Type != "world" || rec2.Type != "attention" || rec2.Seq != rec1.Seq+1 {
		t.Fatalf("got %s/%s seq %d,%d", rec1.Type, rec2.Type, rec1.Seq, rec2.Seq)
	}
	_ = json.Unmarshal(rec1.Payload, &d)
	if len(d.Removed) != 1 || d.Removed[0] != "a" {
		t.Fatalf("a vanished row must be reported removed: %+v", d)
	}
	fw.set()
	f.refresh()
	s.next(t)
	rec, _ = s.next(t)
	var a attentionDelta
	_ = json.Unmarshal(rec.Payload, &a)
	if rec.Type != "attention" || len(a.Items) != 0 {
		t.Fatalf("an answered question must clear: %s %+v", rec.Type, a)
	}
}

func TestStreamResumesAfterSeq(t *testing.T) {
	fw := &fakeWorld{}
	f, _ := testFeed(fw)
	srv := bridgeFor(t, f)
	for _, ids := range [][]string{{"a"}, {"a", "b"}, {"a", "b", "c"}} {
		var rows []session.SessionRow
		for _, id := range ids {
			rows = append(rows, worldWorking(id))
		}
		fw.set(rows...)
		f.refresh()
	}
	f.mu.Lock()
	seq := f.seq
	f.mu.Unlock()
	if seq != 3 {
		t.Fatalf("seq %d", seq)
	}
	s := openStream(t, srv, "1")
	r2, _ := s.next(t)
	r3, _ := s.next(t)
	if r2.Seq != 2 || r3.Seq != 3 || r2.Type != "world" {
		t.Fatalf("resume gave %d,%d (%s)", r2.Seq, r3.Seq, r2.Type)
	}
	s.quiet(t)
	// Caught up exactly: nothing replays.
	again := openStream(t, srv, "3")
	again.quiet(t)
	fw.set(worldWorking("a"))
	f.refresh()
	rec, _ := again.next(t)
	if rec.Seq != 4 {
		t.Fatalf("live record seq %d", rec.Seq)
	}
}

func TestAGapSendsOneResetWithEveryRow(t *testing.T) {
	fw := &fakeWorld{}
	f, _ := testFeed(fw)
	f.ringSize = 3
	srv := bridgeFor(t, f)
	for i := 1; i <= 8; i++ {
		var rows []session.SessionRow
		for j := 0; j < i; j++ {
			rows = append(rows, worldWorking(string(rune('a'+j))))
		}
		fw.set(rows...)
		f.refresh()
	}
	s := openStream(t, srv, "2")
	rec, _ := s.next(t)
	var full worldFull
	_ = json.Unmarshal(rec.Payload, &full)
	if rec.Type != "reset" || len(full.Rows) != 8 || rec.Seq != 8 {
		t.Fatalf("%s seq %d rows %d", rec.Type, rec.Seq, len(full.Rows))
	}
	s.quiet(t)

	// A cursor from a server that lived longer (restart) is a gap too.
	future := openStream(t, srv, "99999")
	if rec, _ := future.next(t); rec.Type != "reset" || rec.Seq != 8 {
		t.Fatalf("future cursor: %s %d", rec.Type, rec.Seq)
	}
}

func TestHeartbeatKeepsTheStreamAlive(t *testing.T) {
	fw := &fakeWorld{}
	f, clock := testFeed(fw)
	srv := bridgeFor(t, f)
	s := openStream(t, srv, "0")
	s.next(t)
	waitFor(t, "heartbeat ticker", func() bool { return clock.has(15 * time.Second) })
	clock.fire(15 * time.Second)
	if _, line := s.next(t); line != ": alive" {
		t.Fatalf("heartbeat line %q", line)
	}
}

func TestPollRunsOnlyWhileSomebodyListens(t *testing.T) {
	fw := &fakeWorld{}
	fw.set(worldWorking("a"))
	f, clock := testFeed(fw)
	srv := bridgeFor(t, f)
	if f.Observers() != 0 || clock.has(2*time.Second) {
		t.Fatal("polling before anyone listens")
	}
	s := openStream(t, srv, "0")
	s.next(t)
	waitFor(t, "poller", func() bool { return clock.has(2 * time.Second) })
	if f.Observers() != 1 {
		t.Fatalf("observers %d", f.Observers())
	}
	fw.set(worldWorking("a"), worldWorking("b"))
	clock.fire(2 * time.Second)
	if rec, _ := s.next(t); rec.Type != "world" {
		t.Fatalf("poll produced %s", rec.Type)
	}
	// Cancelling the request detaches it and stops the poll with the last reader.
	s.cancel()
	waitFor(t, "detach", func() bool { return f.Observers() == 0 })
	f.mu.Lock()
	stopped := f.stopPoll == nil
	f.mu.Unlock()
	if !stopped {
		t.Fatal("the poller outlived its last reader")
	}
	before := fw.reads.Load()
	time.Sleep(50 * time.Millisecond)
	if fw.reads.Load() != before {
		t.Fatal("disk read with nobody listening")
	}
}

// Changes made while nobody listened arrive as deltas on the next attach, so a
// reconnecting cursor resumes instead of resetting.
func TestReconnectCatchesUpOnWhatMovedWhileAway(t *testing.T) {
	fw := &fakeWorld{}
	fw.set(worldWorking("a"))
	f, _ := testFeed(fw)
	srv := bridgeFor(t, f)
	s := openStream(t, srv, "0")
	rec, _ := s.next(t)
	cursor := rec.Seq
	s.cancel()
	waitFor(t, "detach", func() bool { return f.Observers() == 0 })
	fw.set(worldWorking("a"), asking("b", "go?"))
	again := openStream(t, srv, "0")
	again.cancel()
	resumed := openStream(t, srv, itoa(cursor))
	r1, _ := resumed.next(t)
	r2, _ := resumed.next(t)
	if r1.Type != "world" || r2.Type != "attention" {
		t.Fatalf("resume gave %s,%s", r1.Type, r2.Type)
	}
}

func itoa(n uint64) string { return uitoa(n) }

type stuckWriter struct {
	header  http.Header
	release chan struct{}
}

func (w *stuckWriter) Header() http.Header         { return w.header }
func (w *stuckWriter) WriteHeader(int)             {}
func (w *stuckWriter) Flush()                      {}
func (w *stuckWriter) Write(p []byte) (int, error) { <-w.release; return len(p), nil }

func TestASlowReaderNeverBlocksPublish(t *testing.T) {
	fw := &fakeWorld{}
	f, _ := testFeed(fw)
	f.ringSize = 8
	w := &stuckWriter{header: http.Header{}, release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events?after=0", nil).WithContext(ctx)
	ended := make(chan struct{})
	go func() { defer close(ended); f.serve(w, req) }()
	waitFor(t, "reader attached", func() bool { return f.Observers() == 1 })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			fw.set(worldWorking(itoa(uint64(i))))
			f.refresh()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing waited on a stuck reader")
	}
	f.mu.Lock()
	held := len(f.ring)
	f.mu.Unlock()
	if held > 8 {
		t.Fatalf("ring grew to %d", held)
	}
	cancel()
	close(w.release)
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not end on cancellation")
	}
	if f.Observers() != 0 {
		t.Fatal("observer leaked")
	}
}

// Run with -race: readers, pollers, GET /world and publishers all at once.
func TestConcurrentReadersAndWritersAreRaceFree(t *testing.T) {
	fw := &fakeWorld{}
	f, clock := testFeed(fw)
	f.ringSize = 16
	srv := bridgeFor(t, f)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		s := openStream(t, srv, "0")
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-s.lines:
				case <-stop:
					return
				}
			}
		}()
	}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					f.Current()
				}
			}
		}()
	}
	waitFor(t, "poller", func() bool { return clock.has(2 * time.Second) })
	for i := 0; i < 200; i++ {
		fw.set(worldWorking(itoa(uint64(i%7))), asking("q", "x"))
		f.refresh()
		if i%20 == 0 {
			clock.mu.Lock()
			ch := clock.chans[2*time.Second]
			clock.mu.Unlock()
			select {
			case ch <- time.Now():
			default:
			}
		}
	}
	close(stop)
	wg.Wait()
}

func TestBridgeCloseStopsThePoller(t *testing.T) {
	fw := &fakeWorld{}
	f, _ := testFeed(fw)
	b := New(worldToken, nil)
	b.UseWorld(f)
	detach := f.attach()
	defer detach()
	b.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopPoll != nil || !f.closed {
		t.Fatal("Close left the poller running")
	}
}

func TestWorldEpochForcesResetEvenWhenOldSequenceFitsNewFeed(t *testing.T) {
	fw := &fakeWorld{}
	fw.set(worldWorking("a"))
	f, _ := testFeed(fw)
	other := NewWorldFeed(nil)
	defer other.Close()
	if f.epoch == "" || f.epoch == other.epoch {
		t.Fatal("feed identities must be distinct")
	}
	srv := bridgeFor(t, f)
	s := openStream(t, srv, "1&epoch="+other.epoch)
	rec, _ := s.next(t)
	if rec.Type != "reset" || rec.Epoch != f.epoch {
		t.Fatalf("wrong epoch reset %+v", rec)
	}
}
