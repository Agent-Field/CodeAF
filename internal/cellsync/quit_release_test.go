package cellsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

type quitReleaseDir struct {
	directory.Client
	failures []error
	calls    int
}

func (d *quitReleaseDir) Release(ctx context.Context, id string, fence uint64) error {
	d.calls++
	if d.calls <= len(d.failures) {
		return d.failures[d.calls-1]
	}
	return d.Client.Release(ctx, id, fence)
}

// A clean quit requires a temporary coordination refusal or lost reply during close
// to retry the same release, within the caller's existing close deadline.
func TestClosingReleaseRetriesTransientRefusal(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	r.publishFirst(map[string]string{"a": "saved"})
	d := &quitReleaseDir{Client: b.Publisher.Dir, failures: []error{directory.ErrUnreachable}}
	b.Publisher.Dir = d
	b.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatal("transient refusal stranded a release", err)
	}
	if d.calls != 2 || r.head(cellID).Lease.ReleasedAt == 0 {
		t.Fatal("retry did not commit the release", d.calls)
	}
}

// A temporary refusal includes a relay's plain HTTP unavailable response, even when the
// coordinator could not produce the usual signed directory error body.
func TestClosingReleaseRetriesPlainHTTPUnavailable(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	r.publishFirst(map[string]string{"a": "saved"})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := r.dirA.Release(q.Context(), cellID, r.drv.Fence); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	b.Publisher.Dir = directory.NewHTTP(server.URL, func(*http.Request, []byte) {}, server.Client())
	b.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatal("plain HTTP refusal stranded a release", err)
	}
	if calls != 2 || r.head(cellID).Lease.ReleasedAt == 0 {
		t.Fatal("plain HTTP retry did not commit", calls)
	}
}

// A clean quit requires definitive refusals to end immediately, while repeated
// unavailability remains bounded by the closing context.
func TestClosingReleaseNeverRetriesDefinitiveRefusal(t *testing.T) {
	for _, refusal := range []error{directory.ErrFenceStale, directory.ErrRevoked} {
		t.Run(refusal.Error(), func(t *testing.T) {
			r := newRig(t)
			b := r.batcher()
			r.publishFirst(map[string]string{"a": "saved"})
			d := &quitReleaseDir{Client: b.Publisher.Dir, failures: []error{refusal}}
			b.Publisher.Dir = d
			err := b.Close(t.Context())
			if d.calls != 1 || refusal == directory.ErrRevoked && !errors.Is(err, refusal) {
				t.Fatal("definitive refusal retried or changed", d.calls, err)
			}
		})
	}
}

// A clean quit requires a persistent temporary refusal to finish at the caller's
// deadline, without issuing another request after that deadline.
func TestClosingReleaseRespectsDeadline(t *testing.T) {
	r := newRig(t)
	b := r.batcher()
	r.publishFirst(map[string]string{"a": "saved"})
	d := &quitReleaseDir{Client: b.Publisher.Dir, failures: []error{directory.ErrUnreachable}}
	b.Publisher.Dir = d
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, time.Second)
	defer deadline()
	b.Sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	if err := b.Close(ctx); !errors.Is(err, context.Canceled) || d.calls != 1 {
		t.Fatal("release ignored its closing context", err, d.calls)
	}
}
