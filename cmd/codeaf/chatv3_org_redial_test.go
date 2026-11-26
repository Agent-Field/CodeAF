package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// These tests are written from what a person sees when the link between their terminal and their organization's server
// breaks while a hosted chat is open: a laptop that slept, a network that changed, a server that restarted. The chat goes
// on running on the server whether anyone is looking, so the window must come back to it by itself.
//
//	RT8   the window comes back by itself, as the same window coming back (not a new one arriving), and says nothing
//	      that is not true.
//	RT9   a window whose access was taken away while it was gone ends at once with the server's own words, and does not
//	      spend half an hour saying it is reconnecting.
//	RT10  one message sent from a script (--once) is never retried: the first drop ends it.

// droppingOrg is the far end of one hosted chat that lets go of the first dropFirst links right after their welcome, and
// from refuseFrom on (when set) answers the upgrade with the server's own no.
type droppingOrg struct {
	dropFirst  int
	refuseFrom int

	mu          sync.Mutex
	connections int
	hellos      []remote.Hello
}

func (o *droppingOrg) wire(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	o.connections++
	n := o.connections
	o.mu.Unlock()
	if o.refuseFrom > 0 && n >= o.refuseFrom {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"There is no such chat."}`)
		return
	}
	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer socket.CloseNow()
	conn := websocket.NetConn(r.Context(), socket, websocket.MessageBinary)
	send := func(frame remote.Frame) {
		line, _ := json.Marshal(frame)
		_, _ = conn.Write(append(line, '\n'))
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var frame remote.Frame
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			return
		}
		switch frame.Kind {
		case "hello":
			var hello remote.Hello
			_ = json.Unmarshal(frame.Payload, &hello)
			o.mu.Lock()
			o.hellos = append(o.hellos, hello)
			o.mu.Unlock()
			welcome, _ := json.Marshal(remote.Welcome{Version: remote.Version, Workspace: "/srv/work", SessionFile: "/srv/j.jsonl", Model: "a/b", Persistent: true})
			send(remote.Frame{Kind: "welcome", Payload: welcome})
			if n <= o.dropFirst {
				return
			}
		case "call":
			send(remote.Frame{Kind: "result", ID: frame.ID})
		}
	}
}

func (o *droppingOrg) seen() (connections int, hellos []remote.Hello) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.connections, append([]remote.Hello(nil), o.hellos...)
}

func orgWaitFor(t *testing.T, what string, within time.Duration, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestRT8_AnOrgWindowComesBackWhenItsLinkDrops(t *testing.T) {
	orgSignedIn(t)
	org := newFakeOrg(t)
	far := &droppingOrg{dropFirst: 1}
	org.wire = far.wire
	door, _ := newDoorFor(t, org.srv.URL)
	client, shut, err := door.attach(context.Background(), orgID('a'), remote.Hello{Window: true}, true)
	if err != nil {
		t.Fatalf("the first attach failed: %v", err)
	}
	defer shut()
	// A redial waits a second and up to five more, so that a restarted server is not asked by every window at once.
	orgWaitFor(t, "the window to come back", 20*time.Second, func() bool {
		n, _ := far.seen()
		return n >= 2
	})
	_, hellos := far.seen()
	if len(hellos) < 2 || hellos[0].Back || !hellos[1].Back {
		t.Fatalf("the window said %+v; it must come back as a returning window (back) and not as a new arrival that takes the keyboard", hellos)
	}
	orgWaitFor(t, "the window to be whole again", 5*time.Second, func() bool { return client.LinkNote() == "" })
	if err := client.Err(); err != nil {
		t.Fatalf("a window that came back reads %v", err)
	}
}

func TestRT9_AnOrgWindowWhoseAccessWentWhileItWasGoneEndsWithTheServersWords(t *testing.T) {
	orgSignedIn(t)
	org := newFakeOrg(t)
	far := &droppingOrg{dropFirst: 1, refuseFrom: 2}
	org.wire = far.wire
	door, _ := newDoorFor(t, org.srv.URL)
	client, shut, err := door.attach(context.Background(), orgID('a'), remote.Hello{Window: true}, true)
	if err != nil {
		t.Fatalf("the first attach failed: %v", err)
	}
	defer shut()
	orgWaitFor(t, "the window to end", 20*time.Second, func() bool { return client.Err() != nil })
	if got := client.Err().Error(); !strings.Contains(got, "There is no such chat.") {
		t.Fatalf("a window whose chat went away reads %q, want the server's own words", got)
	}
	if n, _ := far.seen(); n != 2 {
		t.Fatalf("the server was asked %d times; a no that will not change is asked once", n)
	}
}

func TestRT10_AHeadlessOrgRunIsNeverRetried(t *testing.T) {
	orgSignedIn(t)
	org := newFakeOrg(t)
	far := &droppingOrg{dropFirst: 1}
	org.wire = far.wire
	door, _ := newDoorFor(t, org.srv.URL)
	client, shut, err := door.attach(context.Background(), orgID('a'), remote.Hello{Headless: true}, false)
	if err != nil {
		t.Fatalf("the first attach failed: %v", err)
	}
	defer shut()
	orgWaitFor(t, "the one-shot to end", 5*time.Second, func() bool { return client.Err() != nil })
	if note := client.LinkNote(); note != "" {
		t.Fatalf("a one-shot says %q; it does not reconnect", note)
	}
}
