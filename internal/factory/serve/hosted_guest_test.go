package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/coder/websocket"
)

const (
	guestTestHello     = "{\"kind\":\"hello\",\"payload\":{\"version\":1}}\n"
	guestOwnerWords    = "This chat is shared with you. Only its owner can do that."
	guestRunWords      = "You can watch this chat, but you were not given permission to send messages in it. Ask its owner."
	guestSteerWords    = "You can watch this chat, but you were not given permission to steer it. Ask its owner."
	guestAnswerWords   = "You can watch this chat, but you were not given permission to answer its questions. Ask its owner."
	guestRemovedWords  = "Your access to this chat was removed."
	guestTestHeartbeat = 100 * time.Millisecond
)

type guestHostWire struct {
	conn     *websocket.Conn
	incoming chan string
}

func recordGuestHostWires(rg *hostedRig) <-chan guestHostWire {
	opened := make(chan guestHostWire, 8)
	rg.h.config.Hosted.Heartbeat = guestTestHeartbeat
	rg.host.mu.Lock()
	defer rg.host.mu.Unlock()
	rg.host.wire = func(conn *websocket.Conn, r *http.Request) {
		wire := guestHostWire{conn, make(chan string, 512)}
		defer close(wire.incoming)
		opened <- wire
		for {
			_, raw, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			wire.incoming <- string(raw)
		}
	}
	return opened
}

func guestReceive[T any](t *testing.T, ch <-chan T) (T, bool) {
	t.Helper()
	select {
	case value, ok := <-ch:
		return value, ok
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the host wire")
		var zero T
		return zero, false
	}
}

func guestHostLine(t *testing.T, wire guestHostWire) string {
	t.Helper()
	raw, ok := guestReceive(t, wire.incoming)
	if !ok {
		t.Fatal("the host wire closed before the expected line")
	}
	return raw
}

func guestDial(t *testing.T, srv *httptest.Server, secret, chat string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wireURL(srv, chat), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + secret}, "Origin": {srv.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func guestConnect(t *testing.T, srv *httptest.Server, opened <-chan guestHostWire, secret, chat string) (*websocket.Conn, guestHostWire) {
	t.Helper()
	client := guestDial(t, srv, secret, chat)
	host, _ := guestReceive(t, opened)
	guestWrite(t, client, guestTestHello)
	var hello remote.Frame
	if err := json.Unmarshal([]byte(guestHostLine(t, host)), &hello); err != nil || hello.Kind != "hello" {
		t.Fatalf("the host did not receive a hello: %+v, %v", hello, err)
	}
	return client, host
}

func guestWrite(t *testing.T, conn *websocket.Conn, raw string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func guestRead(t *testing.T, conn *websocket.Conn, limit time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), limit)
	defer cancel()
	kind, raw, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary {
		t.Fatalf("the wire read = %v %q, %v", kind, raw, err)
	}
	return string(raw)
}

func guestFrame(t *testing.T, raw string) remote.Frame {
	t.Helper()
	var frame remote.Frame
	if err := json.Unmarshal([]byte(raw), &frame); err != nil || !strings.HasSuffix(raw, "\n") {
		t.Fatalf("the wire did not carry one frame: %q, %v", raw, err)
	}
	return frame
}

func guestCall(id int, method string) string {
	return fmt.Sprintf("{\"kind\":\"call\",\"id\":%d,\"method\":%q}\n", id, method)
}

func guestRefusal(t *testing.T, client *websocket.Conn, id int, words string) {
	t.Helper()
	raw := guestRead(t, client, 5*time.Second)
	want := fmt.Sprintf("{\"kind\":\"result\",\"id\":%d,\"error\":%q}\n", id, words)
	if raw != want {
		t.Fatalf("refusal = %q, want %q", raw, want)
	}
}

func guestClosed(t *testing.T, client *websocket.Conn, host guestHostWire, limit time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), limit)
	defer cancel()
	_, raw, err := client.Read(ctx)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("the client wire did not close: %q, %v", raw, err)
	}
	if raw, ok := guestReceive(t, host.incoming); ok {
		t.Fatalf("an unexpected frame reached the host: %q", raw)
	}
}

// P2-5
// A guest opens the existing conversation and both directions keep line boundaries across WebSocket messages.
func TestP2_5_GuestHelloAndWholeLines(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client := guestDial(t, srv, rg.tokens["abir"], chat)
	host, _ := guestReceive(t, opened)
	guestWrite(t, client, `{"kind":"hello","payload":{"launch":{},"model":"other","level":"high","workspace":"/elsewhere","Session":"elsewhere","ſession":"also elsewhere","new":true,"join":true,"holdInterruptedWork":true,"window":{},"BACK":false,"CLIENTID":"desk","version":1,"resume":[{"stream":8,"seq":99}],"encodings":["gzip"],"surface":"desk","REMOTE":{"device":"someone-else"},"watch":false,"extra":{"keep":true}}}`+"\n")
	frame := guestFrame(t, guestHostLine(t, host))
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(frame.Payload, &payload); err != nil || frame.Kind != "hello" {
		t.Fatalf("forwarded hello = %+v, %v", frame, err)
	}
	want := map[string]string{
		"back": "true", "clientID": `"guest:desk"`, "version": "1", "watch": "false",
		"resume": `[{"stream":8,"seq":99}]`, "encodings": `["gzip"]`, "surface": `"abir"`, "extra": `{"keep":true}`,
	}
	if len(payload) != len(want) {
		t.Errorf("hello kept unexpected fields: %s", frame.Payload)
	}
	for key, val := range want {
		if string(payload[key]) != val {
			t.Errorf("hello %s = %s, want %s", key, payload[key], val)
		}
	}

	const first = `{"kind":"event", "payload": `
	const last = `{"keep": "original spaces"}}` + "\n"
	guestWrite(t, host.conn, first)
	guestWrite(t, client, `{"kind":"call","id":9,"method":"Sub`)
	guestWrite(t, client, `mit"}`+"\n")
	guestRefusal(t, client, 9, guestRunWords)
	guestWrite(t, host.conn, last+"an opaque engine line\n")
	if got := guestRead(t, client, 5*time.Second); got != first+last {
		t.Fatalf("engine line changed: %q", got)
	}
	if got := guestRead(t, client, 5*time.Second); got != "an opaque engine line\n" {
		t.Fatalf("opaque engine line changed: %q", got)
	}
	guestWrite(t, client, " { \"kind\":\"call\", \"id\":10, \"method\":\"Ping\", \"unknown\":true }\n")
	if got := guestHostLine(t, host); got != guestCall(10, "Ping") {
		t.Fatalf("next engine input was not the checked frame: %q", got)
	}

	client.SetReadLimit(4 << 20)
	longLine := strings.Repeat("x", 4<<20) + "\n"
	guestWrite(t, host.conn, longLine)
	var received strings.Builder
	for received.Len() < len(longLine) {
		received.WriteString(guestRead(t, client, 5*time.Second))
	}
	if received.String() != longLine {
		t.Fatal("the long engine line changed on the guest wire")
	}
}

// P2-5
// Only the methods named by the guest's current verbs can reach the engine.
func TestP2_5_GuestMethodsUseVerbs(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	hostedGrant(t, rg, "santosh", chat, "mia", "edit")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	for _, person := range []string{"abir", "mia"} {
		t.Run(person, func(t *testing.T) {
			client, host := guestConnect(t, srv, opened, rg.tokens[person], chat)
			classes := maps.Clone(hostedGuestMethods)
			classes["Future.Method"] = "owner"
			id := 0
			for _, method := range slices.Sorted(maps.Keys(classes)) {
				class := classes[method]
				id++
				guestWrite(t, client, guestCall(id, method))
				words := guestOwnerWords
				switch class {
				case "view":
					words = ""
				case "run", "steer", "answer":
					words = ""
					if person == "abir" {
						words = map[string]string{"run": guestRunWords, "steer": guestSteerWords, "answer": guestAnswerWords}[class]
					}
				}
				if words != "" {
					guestRefusal(t, client, id, words)
				} else if got := guestHostLine(t, host); got != guestCall(id, method) {
					t.Fatalf("host received %q for %s", got, method)
				}
			}

			guestWrite(t, client, `{"method":"Close","kind":"call","id":900,"method":"Transcript"}`+"\n")
			if got := guestHostLine(t, host); got != guestCall(900, "Transcript") {
				t.Fatalf("the host received unchecked bytes: %q", got)
			}
			guestWrite(t, client, `{"method":"Transcript","kind":"call","id":901,"method":"Close"}`+"\n")
			guestRefusal(t, client, 901, guestOwnerWords)
			_ = client.CloseNow()
			if raw, ok := guestReceive(t, host.incoming); ok {
				t.Fatalf("a refused call reached the engine: %q", raw)
			}
		})
	}
}

// P2-5
// Invalid input closes the pump with one plain fatal line before any invalid frame reaches the host.
func TestP2_5_GuestInvalidInputClosesTheWire(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	for _, tc := range []struct {
		name, input string
		afterHello  bool
	}{
		{"call before hello", guestCall(1, "Ping"), false},
		{"hello without an object", "{\"kind\":\"hello\",\"payload\":null}\n", false},
		{"second hello", guestTestHello, true},
		{"bad JSON", "{\n", true},
		{"blank method", guestCall(1, " "), true},
		{"line over 256 KiB", strings.Repeat(" ", 256<<10) + "\n", true},
		{"oversized line without a newline", strings.Repeat(" ", (256<<10)+1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var client *websocket.Conn
			var host guestHostWire
			if tc.afterHello {
				client, host = guestConnect(t, srv, opened, rg.tokens["abir"], chat)
			} else {
				client = guestDial(t, srv, rg.tokens["abir"], chat)
				host, _ = guestReceive(t, opened)
			}
			guestWrite(t, client, tc.input)
			frame := guestFrame(t, guestRead(t, client, 5*time.Second))
			if frame.Kind != "fatal" || frame.Error == "" {
				t.Fatalf("expected a fatal line, got %+v", frame)
			}
			guestClosed(t, client, host, 5*time.Second)
		})
	}
}

// P2-5
// A policy change removes verbs from the current connection, and a grant revocation closes it.
func TestP2_5_GuestHeartbeatRefreshesVerbsAndRevokesAccess(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	grant := hostedGrant(t, rg, "santosh", chat, "abir", "edit")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client, host := guestConnect(t, srv, opened, rg.tokens["abir"], chat)
	for id, method := range []string{"Submit", "Steer", "ResolveQuestion"} {
		line := guestCall(id+1, method)
		guestWrite(t, client, line)
		if got := guestHostLine(t, host); got != line {
			t.Fatalf("host received %q, want %q", got, line)
		}
	}
	rg.setAccessPolicy(t, func(p *acl.Policy) {
		for i := range p.Roles {
			if p.Roles[i].Name == "edit" {
				p.Roles[i].Verbs = slices.DeleteFunc(p.Roles[i].Verbs, func(v string) bool {
					return v == "run" || v == "steer"
				})
			}
		}
	})
	// Two ticks allow a check already running when the policy was saved to finish.
	time.Sleep(2 * guestTestHeartbeat)
	guestWrite(t, client, guestCall(5, "Submit"))
	guestRefusal(t, client, 5, guestRunWords)
	guestWrite(t, client, guestCall(6, "Steer"))
	guestRefusal(t, client, 6, guestSteerWords)
	guestWrite(t, client, guestCall(7, "ResolveQuestion"))
	if got := guestHostLine(t, host); got != guestCall(7, "ResolveQuestion") {
		t.Fatalf("host received %q for a verb the guest kept", got)
	}

	rg.do(t, "santosh", http.MethodDelete, "/v1/resources/session/"+chat+"/grants/"+grant.ID, -1, nil, http.StatusNoContent, nil)
	guestAccessRemoved(t, client, host)
}

func guestAccessRemoved(t *testing.T, client *websocket.Conn, host guestHostWire) {
	t.Helper()
	deadline := time.Now().Add(3 * guestTestHeartbeat)
	if got := guestRead(t, client, 3*guestTestHeartbeat); got != `{"kind":"fatal","error":"`+guestRemovedWords+`"}`+"\n" {
		t.Fatalf("access removal = %q", got)
	}
	guestClosed(t, client, host, time.Until(deadline))
}

// P2-5
// Closing the organization handler closes every guest wire on both sides.
func TestP2_5_HostedShutdownClosesGuestWires(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	opened := recordGuestHostWires(rg)
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	first, firstHost := guestConnect(t, srv, opened, rg.tokens["abir"], chat)
	second, secondHost := guestConnect(t, srv, opened, rg.tokens["abir"], chat)
	rg.h.Close()
	guestClosed(t, first, firstHost, 5*time.Second)
	guestClosed(t, second, secondHost, 5*time.Second)
}

// P2-5
// A guest opening a chat that is not running, when its owner or the machine is at the limit of running chats, is told in the
// guest's own words and never reads the owner's sentence.
func TestP2_5_GuestIsToldPlainlyWhenThereIsNoRoomToStartTheChat(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", chat, "abir", "view")
	recordGuestHostWires(rg)
	rg.host.mu.Lock()
	rg.host.wireRefusal = http.StatusTooManyRequests
	rg.host.mu.Unlock()
	srv := httptest.NewServer(rg.h)
	defer srv.Close()
	client := guestDial(t, srv, rg.tokens["abir"], chat)
	frame := guestFrame(t, guestRead(t, client, 5*time.Second))
	if frame.Kind != "fatal" || frame.Error != hostedGuestNoRoom {
		t.Fatalf("the guest was told %+v; want the guest's own sentence", frame)
	}
	if strings.Contains(frame.Error, "You already have") || strings.ContainsAny(frame.Error, "0123456789") {
		t.Fatalf("the guest read the owner's limit: %q", frame.Error)
	}
}
