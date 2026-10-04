package blobstore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// trickle answers a get with a long body that arrives slowly: its headers come
// at once and the body in pieces, taking longer than an empty request's whole
// allowance. stall says how long the server waits before each piece.
func trickle(t *testing.T, size, pieces int, stall time.Duration) *HTTP {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusOK)
		piece := strings.Repeat("x", size/pieces)
		for i := 0; i < pieces; i++ {
			select {
			case <-time.After(stall):
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write([]byte(piece))
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	c := NewHTTP(srv.URL, wireauth.Sign(func(*http.Request, []byte) {}), nil)
	// An empty request gets 100 ms and every KiB of answer another 20 ms.
	c.deadline = func(n int) time.Duration { return 100*time.Millisecond + time.Duration(n>>10)*20*time.Millisecond }
	return c
}

// A get sends no body, so its time limit must grow with the answer's length:
// a large answer that keeps arriving is not cut off by an empty request's allowance.
func TestGetDeadlineGrowsWithTheAnswer(t *testing.T) {
	c := trickle(t, 32<<10, 4, 60*time.Millisecond) // 240 ms of body against a 100 ms base
	got, err := c.Get(context.Background(), strings.Repeat("a", 64))
	if err != nil || len(got) != 32<<10 {
		t.Fatalf("Get = %d bytes, %v; want the whole answer", len(got), err)
	}
}

// An answer that stops arriving must still fail, and as an unreachable store,
// so callers degrade rather than wait for a relay that has gone quiet.
func TestGetOfAStalledAnswerFailsUnreachable(t *testing.T) {
	c := trickle(t, 1<<10, 1, time.Minute) // 1 KiB is allowed 120 ms; the body never comes
	start := time.Now()
	_, err := c.Get(context.Background(), strings.Repeat("a", 64))
	if !errors.Is(err, ErrUnreachable) || time.Since(start) > 5*time.Second {
		t.Fatalf("Get = %v after %v; want ErrUnreachable promptly", err, time.Since(start))
	}
}
