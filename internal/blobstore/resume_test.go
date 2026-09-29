package blobstore

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// A large publish over a slow disk must send every frame's bytes exactly once.
// Each fsync sleeps, so a frame outlasts the client's deadline and the client
// gives up on a put the relay then finishes. Without Resuming that put, and
// every frame before it on the next flush, would be sent again.
func TestSlowDiskPublishSendsEveryFrameOnce(t *testing.T) {
	disk, err := NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	disk.sync = func(f []*os.File) error { time.Sleep(60 * time.Millisecond); return flushFiles(f) }
	relay := Counting{Inner: disk, C: &Counters{}}
	srv := httptest.NewServer(Handler(
		func(*http.Request, []byte) (string, string, error) { return "id", "dev", nil },
		func(string) (Store, error) { return relay, nil }))
	t.Cleanup(srv.Close)

	wire := NewHTTP(srv.URL, wireauth.Sign(func(*http.Request, []byte) {}), nil)
	wire.deadline = func(int) time.Duration { return 100 * time.Millisecond }
	client := NewResuming(wire)

	frames := manyFrames(t, 6, 8)
	var total int64
	for _, f := range frames {
		total += int64(len(f))
	}
	publish := func() error {
		for _, f := range frames {
			if _, err := client.PutFrame(context.Background(), f); err != nil {
				return err
			}
		}
		return nil
	}
	// A failed flush is retried from the first frame, as the Batcher does.
	for attempt := 0; publish() != nil; attempt++ {
		if attempt > len(frames) {
			t.Fatal("publish never completed")
		}
	}
	if got := relay.C.BytesUp.Load(); got != total {
		t.Fatalf("relay received %d bytes for frames totalling %d", got, total)
	}
	if got := relay.C.Puts.Load(); got != int64(len(frames)) {
		t.Fatalf("relay received %d puts for %d frames", got, len(frames))
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
