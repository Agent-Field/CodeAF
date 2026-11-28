package relayserve_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// heldShutdownWithin is how long a relay may take to stop once it is told to,
// with a phone's wait parked and a directory watch socket open. The old relay
// took up to a wait's 25 seconds and an operator had to kill it.
const heldShutdownWithin = 2 * time.Second

// TestShutdownEndsWhatTheRelayIsHoldingOpen is the sentence "a relay told to stop
// stops within a couple of seconds, whoever is connected to it": a phone with a
// slot wait parked (up to 25 seconds), a pairing poll parked (also up to 25) and a
// computer with a directory watch socket open must not keep SIGTERM waiting. The
// slot wait is answered as unavailable at once, the poll returns, the socket is
// closed, and the server's shutdown completes.
func TestShutdownEndsWhatTheRelayIsHoldingOpen(t *testing.T) {
	r := newRig(t)
	computer, client := pairTestClient(t, r)
	// The directory is created on first use; do that before the clock starts.
	if _, err := r.dir(computer).List(bg); err != nil {
		t.Fatal(err)
	}

	// The relay as the program runs it: Serve over the same service, stopped by
	// cancelling the context, which is what SIGTERM does.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	// stop is SIGTERM. It must not also cancel what the clients below wait on, or a
	// wait that nobody ended would look ended: they run on the test's own context.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	served := make(chan error, 1)
	go func() { served <- relayserve.Serve(ctx, ln, r.svc.Handler) }()

	// A phone parks a slot wait of the longest length.
	type answer struct {
		status int
		body   []byte
		err    error
	}
	waited := make(chan answer, 1)
	go func() {
		req, err := http.NewRequest("GET", "http://"+addr+"/v1/clients/"+client.dev.ID()+"/slots?since=0&wait=25", nil)
		if err != nil {
			waited <- answer{err: err}
			return
		}
		signAs(req, nil, client, r)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			waited <- answer{err: err}
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		waited <- answer{status: resp.StatusCode, body: raw, err: err}
	}()

	// A computer being paired parks a poll of the pairing mailbox for the longest wait.
	box := pairbox.NewHTTP("http://"+addr, &http.Client{Timeout: time.Minute})
	key, err := pairbox.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	plate, err := box.Create(t.Context(), key)
	if err != nil {
		t.Fatalf("open a pairing mailbox: %v", err)
	}
	polled := make(chan error, 1)
	go func() {
		_, err := box.Poll(t.Context(), plate.Nameplate, pairbox.SideB, 0, pairbox.MaxWait)
		polled <- err
	}()

	// A computer opens its directory watch socket and hears the first version.
	watch, err := http.NewRequest("GET", "http://"+addr+"/v1/dir/watch", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(watch, nil, computer, r.clock.Now())
	conn, _, err := websocket.Dial(t.Context(), "ws://"+addr+"/v1/dir/watch", &websocket.DialOptions{HTTPHeader: watch.Header})
	if err != nil {
		t.Fatalf("open the watch socket: %v", err)
	}
	defer conn.CloseNow()
	readCtx, cancelRead := context.WithTimeout(t.Context(), 5*time.Second)
	if _, _, err := conn.Read(readCtx); err != nil {
		t.Fatalf("the watch socket never sent its first version: %v", err)
	}
	cancelRead()
	closed := make(chan error, 1)
	go func() {
		_, _, err := conn.Read(t.Context())
		closed <- err
	}()

	// Both are parked now; give the wait time to reach the relay.
	select {
	case a := <-waited:
		t.Fatalf("the slot wait ended before the shutdown: %+v", a)
	case err := <-closed:
		t.Fatalf("the watch socket ended before the shutdown: %v", err)
	case err := <-polled:
		t.Fatalf("the pairing poll ended before the shutdown: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	began := time.Now()
	stop()
	deadline := time.After(heldShutdownWithin)
	var gotWait, gotSocket, gotPoll, gotServe bool
	for !(gotWait && gotSocket && gotPoll && gotServe) {
		select {
		case a := <-waited:
			if a.err != nil || a.status != http.StatusServiceUnavailable || !bytes.Contains(a.body, []byte(`"unavailable"`)) {
				t.Fatalf("the parked slot wait ended with %+v %s, want 503 unavailable", a, a.body)
			}
			gotWait = true
		case <-closed:
			gotSocket = true
		case <-polled:
			gotPoll = true
		case err := <-served:
			if err != nil {
				t.Fatalf("Serve returned %v, want a clean stop", err)
			}
			gotServe = true
		case <-deadline:
			t.Fatalf("after %s the relay was still holding on: slot wait ended=%v, watch socket closed=%v, pairing poll returned=%v, Serve returned=%v",
				heldShutdownWithin, gotWait, gotSocket, gotPoll, gotServe)
		}
	}
	t.Logf("a relay with a parked slot wait, a parked pairing poll and an open watch socket stopped in %s", time.Since(began).Round(time.Millisecond))
}
