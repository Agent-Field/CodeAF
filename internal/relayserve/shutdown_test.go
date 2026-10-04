package relayserve_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relayserve"
)

// SIGTERM cancels the context Serve runs under; a request already in flight
// must still be answered in full, and Serve must then return cleanly.
func TestServeLetsInFlightRequestsFinish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, "finished")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- relayserve.Serve(ctx, ln, h) }()

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			body <- err.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body <- string(b)
	}()

	<-started
	stop() // the signal
	select {
	case err := <-served:
		t.Fatalf("Serve returned with a request in flight: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if got := <-body; got != "finished" {
		t.Fatalf("in-flight request got %q", got)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}
