package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// stream holds /v1/events open and writes o.frames to it until an empty frame
// drops it or the follower leaves.
func (o *orgServer) stream(w http.ResponseWriter, r *http.Request) {
	o.opened <- r.Header.Clone()
	if r.Header.Get("Authorization") == "" {
		orgRefuse(w, http.StatusUnauthorized, "sign in")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-o.frames:
			if frame == "" {
				return
			}
			_, _ = fmt.Fprint(w, frame)
			w.(http.Flusher).Flush()
		}
	}
}

// orgRelay attaches the stub organization with a hub that relays to a channel.
func orgRelay(t *testing.T) (*orgServer, *factoryWire, string, chan appwire.FactoryOrgChanged) {
	t.Helper()
	o, wire, id := attachOrg(t)
	got := make(chan appwire.FactoryOrgChanged, 16)
	h := wire.backend.factories
	h.mu.Lock()
	h.orgChanged = func(e appwire.FactoryOrgChanged) { got <- e }
	h.orgRetry = 5 * time.Millisecond
	h.mu.Unlock()
	return o, wire, id, got
}

func nextOrgEvent(t *testing.T, got chan appwire.FactoryOrgChanged) appwire.FactoryOrgChanged {
	t.Helper()
	select {
	case e := <-got:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no factory.org_changed arrived")
	}
	return appwire.FactoryOrgChanged{}
}

func nextOpening(t *testing.T, o *orgServer) http.Header {
	t.Helper()
	select {
	case h := <-o.opened:
		return h
	case <-time.After(5 * time.Second):
		t.Fatal("the engine did not open the organization's event stream")
	}
	return nil
}

// AN ORGANIZATION'S CHANGES REACH THE DESK: once the connection answers an
// organization method, the server's three kinds arrive as factory.org_changed
// with the connection's id, and nothing else on the stream does. A dropped
// stream is reopened where it left off, and a gap the server cannot replay is
// relayed as "read both lists again".
func TestOrgEventsAreRelayedWithTheConnection(t *testing.T) {
	o, wire, id, got := orgRelay(t)
	var teams appwire.OrgTeams
	wire.must("factory.teams", map[string]any{"connection": id}, &teams)
	if auth := nextOpening(t, o).Get("Authorization"); auth != "Bearer "+testHostToken {
		t.Fatalf("the stream was opened with %q", auth)
	}
	o.frames <- ": keepalive\n\n"
	o.frames <- "event: changed\ndata: 4\n\n"
	o.frames <- "id: 7\nevent: activity\ndata: {\"kind\":\"job\"}\n\n"
	o.frames <- "id: 8\nevent: team.changed\ndata: {\"kind\":\"team.changed\",\"id\":\"tm_1\"}\n\n"
	o.frames <- "id: 9\nevent: workstream.changed\ndata: {\"kind\":\"workstream.changed\",\"id\":\"ws_2\"}\n\n"
	o.frames <- "id: 10\nevent: workstream.removed\ndata: {\"kind\":\"workstream.removed\",\"id\":\"ws_3\"}\n\n"
	want := []appwire.FactoryOrgChanged{
		{Connection: id, Kind: "team.changed", ID: "tm_1"},
		{Connection: id, Kind: "workstream.changed", ID: "ws_2"},
		{Connection: id, Kind: "workstream.removed", ID: "ws_3"},
	}
	for _, w := range want {
		if e := nextOrgEvent(t, got); e != w {
			t.Fatalf("relayed %+v, want %+v", e, w)
		}
	}

	// A second organization call does not open a second stream.
	wire.must("factory.teams", map[string]any{"connection": id}, &teams)
	o.frames <- ""
	if last := nextOpening(t, o).Get("Last-Event-ID"); last != "10" {
		t.Fatalf("the stream was reopened from %q, want 10", last)
	}
	select {
	case h := <-o.opened:
		t.Fatalf("a second stream was opened: %v", h)
	default:
	}
	o.frames <- "event: changed\ndata: *\n\n"
	for _, kind := range []string{"team.changed", "workstream.changed"} {
		if e := nextOrgEvent(t, got); e != (appwire.FactoryOrgChanged{Connection: id, Kind: kind}) {
			t.Fatalf("a gap relayed %+v, want %s with no id", e, kind)
		}
	}
}

// FOLLOWING ENDS WITH THE CONNECTION: a detached connection's stream is
// closed and nothing more of it is relayed.
func TestOrgEventsStopWhenTheConnectionIsDetached(t *testing.T) {
	o, wire, id, got := orgRelay(t)
	var teams appwire.OrgTeams
	wire.must("factory.teams", map[string]any{"connection": id}, &teams)
	nextOpening(t, o)
	wire.must("factory.detach", map[string]any{"factory": id}, nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		h := wire.backend.factories
		h.mu.Lock()
		_, following := h.orgFollow[id]
		h.mu.Unlock()
		if !following {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the detached connection is still followed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	o.frames <- "id: 1\nevent: team.changed\ndata: {\"kind\":\"team.changed\",\"id\":\"tm_1\"}\n\n"
	select {
	case e := <-got:
		t.Fatalf("a detached connection relayed %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}
