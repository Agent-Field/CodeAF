package directory_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// TestAChangeReachesAFollowerQuickly runs the real feed over a real socket to
// an in-process relay, and times how long a change takes from the server
// sending its frame to the follower being told. It is the socket's share of
// the change-to-screen delay; the read the screen makes next is one request.
func TestAChangeReachesAFollowerQuickly(t *testing.T) {
	changes := make(chan uint64)
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		conn := accept(t, w, r)
		defer conn.CloseNow()
		ctx := r.Context()
		conn.Write(ctx, websocket.MessageText, []byte(`{"v":0}`))
		for {
			select {
			case v := <-changes:
				conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf(`{"v":%d}`, v)))
			case <-ctx.Done():
				return
			}
		}
	})
	f := dirwatch.Follow(t.Name(), c.Watch)
	defer f.Close()
	waitChange(t, f) // the first frame, on accept

	var took []time.Duration
	for v := uint64(1); v <= 20; v++ {
		start := time.Now()
		changes <- v
		waitChange(t, f)
		took = append(took, time.Since(start))
		if got := f.State().Version; got != v {
			t.Fatalf("the follower holds version %d after a frame for %d", got, v)
		}
	}
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	t.Logf("change-to-follower latency over a real socket: median %v, worst %v", took[len(took)/2], took[len(took)-1])
	if took[len(took)-1] > time.Second {
		t.Fatalf("a change took %v to reach the follower", took[len(took)-1])
	}
}

func waitChange(t *testing.T, f dirwatch.Follower) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-f.Changes():
	case <-ctx.Done():
		t.Fatal("the follower was never told")
	}
}
