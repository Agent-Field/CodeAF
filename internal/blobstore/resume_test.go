package blobstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// slowRelay is a relay whose puts can be held at the moment they are received,
// with no wall-clock guess about how slow a disk is. Every put blocks in
// authentication, which is after its body has been read and before the store
// sees it, until the test lets it go; every Has announces itself on hasSeen.
type slowRelay struct {
	srv     *httptest.Server
	counts  *Counters
	putSeen chan struct{}
	hasSeen chan struct{}
	release chan struct{}
}

func newSlowRelay(t *testing.T) *slowRelay {
	t.Helper()
	disk, err := NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := &slowRelay{
		counts:  &Counters{},
		putSeen: make(chan struct{}, 64),
		hasSeen: make(chan struct{}, 64),
		release: make(chan struct{}, 64),
	}
	store := Counting{Inner: disk, C: g.counts}
	g.srv = httptest.NewServer(Handler(g.auth, func(string) (Store, error) { return store, nil }))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *slowRelay) auth(r *http.Request, _ []byte) (string, string, error) {
	switch r.URL.Path {
	case pathFrames:
		g.putSeen <- struct{}{}
		<-g.release
	case pathHas:
		g.hasSeen <- struct{}{}
	}
	return "id", "dev", nil
}

// client dials the relay with a put deadline short enough to fire while the put
// is held, and a Has deadline of hasWithin.
func (g *slowRelay) client(hasWithin time.Duration) *HTTP {
	c := NewHTTP(g.srv.URL, wireauth.Sign(func(*http.Request, []byte) {}), nil)
	c.deadline = func(body int) time.Duration {
		if body > 1000 {
			return 30 * time.Millisecond
		}
		return hasWithin
	}
	return c
}

// A large publish must send every frame's bytes exactly once. Each put is held
// until the client has given up on it and asked whether it landed; Has must
// wait for the held put and answer yes, so nothing is sent a second time, and a
// flush that restarts from the first frame skips what was delivered.
func TestSlowDiskPublishSendsEveryFrameOnce(t *testing.T) {
	g := newSlowRelay(t)
	client := NewResuming(g.client(5 * time.Second))
	frames := manyFrames(t, 4, 8)
	var total int64
	for _, f := range frames {
		total += int64(len(f))
		put := make(chan error, 1)
		go func() { _, err := client.PutFrame(context.Background(), f); put <- err }()
		<-g.putSeen
		<-g.hasSeen // the client timed out and now asks whether the put landed
		time.Sleep(20 * time.Millisecond)
		g.release <- struct{}{}
		if err := <-put; err != nil {
			t.Fatalf("put of a held frame failed: %v", err)
		}
	}
	for _, f := range frames { // the next flush starts again from the first frame
		if _, err := client.PutFrame(context.Background(), f); err != nil {
			t.Fatal(err)
		}
	}
	if got := g.counts.BytesUp.Load(); got != total {
		t.Fatalf("relay received %d bytes for frames totalling %d", got, total)
	}
}

// A put that never ends must not hang Has, and Has must not answer no for it:
// it fails, and the client learns nothing rather than something false.
func TestHasFailsRatherThanAnswerNoWhilePutIsStuck(t *testing.T) {
	g := newSlowRelay(t)
	c := g.client(50 * time.Millisecond)
	frame := manyFrames(t, 1, 2)[0]
	go func() { _, _ = c.PutFrame(context.Background(), frame) }()
	<-g.putSeen
	defer func() { g.release <- struct{}{} }()

	start := time.Now()
	have, err := c.Has(context.Background(), []string{fmt.Sprintf("%064x", 1)})
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Has = %v, %v; want ErrUnreachable while a put is stuck", have, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("Has hung on a stuck put")
	}
}

// manyFrames builds n frames of k sealed objects each, all distinct.
func manyFrames(t *testing.T, n, k int) [][]byte {
	t.Helper()
	var out [][]byte
	for i := 0; i < n; i++ {
		var objs []Object
		for j := 0; j < k; j++ {
			rid := fmt.Sprintf("%064x", i*1000+j+1)
			objs = append(objs, sealedObject(rid, strings.Repeat("x", 512)+rid))
		}
		f, err := Encode(strings.Repeat("0", 32), objs)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	return out
}

// A frame that was delivered is not sent again, and the deadline grows with the
// body so that a large frame is not cut off by the allowance a small one gets.
func TestResumingSkipsDeliveredFrame(t *testing.T) {
	c := Counting{Inner: NewMemory(), C: &Counters{}}
	r := NewResuming(c)
	frame := manyFrames(t, 1, 3)[0]
	for i := 0; i < 3; i++ {
		if _, err := r.PutFrame(context.Background(), frame); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.C.Puts.Load(); got != 1 {
		t.Fatalf("store saw %d puts of one frame, want 1", got)
	}
}

func TestDeadlineGrowsWithBody(t *testing.T) {
	if small, large := Deadline(1<<10), Deadline(MaxFrame); large <= small+time.Minute {
		t.Fatalf("deadline for a max frame %v is not well above a small one's %v", large, small)
	}
}
