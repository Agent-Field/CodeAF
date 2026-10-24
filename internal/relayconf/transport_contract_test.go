//go:build relayurl

package relayconf

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

type contractTransport func(*http.Request) (*http.Response, error)

func (f contractTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// C19 and C22 bound in-flight local proxy requests without changing the
// canonical concurrency assertions; a cancelled waiter never enters the proxy.
func TestC19ProxyConcurrencyIsBoundedAndCancellable(t *testing.T) {
	entered, release, done := make(chan struct{}, 2), make(chan struct{}), make(chan struct{}, 2)
	var calls atomic.Int32
	transport := &workerTransport{slots: make(chan struct{}, 2), base: contractTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return &http.Response{StatusCode: http.StatusOK}, nil
	})}
	for range 2 {
		go func() {
			req, _ := http.NewRequest(http.MethodGet, "http://worker.test/", nil)
			_, _ = transport.RoundTrip(req)
			done <- struct{}{}
		}()
	}
	for range 2 {
		<-entered
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://worker.test/", nil)
	_, err := transport.RoundTrip(req)
	if !errors.Is(err, context.Canceled) || calls.Load() != 2 {
		t.Errorf("cancelled waiter reached proxy: calls=%d err=%v", calls.Load(), err)
	}
	close(release)
	for range 2 {
		<-done
	}
}
