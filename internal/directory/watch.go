package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// The close codes a watcher reads. Any other code means reconnect with backoff.
const (
	CloseRevoked websocket.StatusCode = 4401 // this device was stopped; never reconnect
	CloseRotated websocket.StatusCode = 4410 // the identity was replaced; poll only
	// CloseSilent says the relay stopped hearing this socket's pings: redial now.
	CloseSilent websocket.StatusCode = 4408
)

const (
	// watchWrite bounds one frame to a peer, so a stalled one cannot hold a
	// watcher forever.
	watchWrite = 10 * time.Second
	// watchRead is the largest client frame; the only one with a meaning is "ping".
	watchRead = 1 << 10
)

// watch answers GET /v1/dir/watch: it authenticates like every route, refuses
// what cannot be watched before the upgrade, and then tells the socket the
// directory's version now and each time it changes. The socket carries versions
// only: what changed is read by the list the device already knows how to read.
func (h *handler) watch(w http.ResponseWriter, r *http.Request) {
	c, err := h.admit(w, r)
	if err != nil {
		refuse(w, err)
		return
	}
	sub, err := subscribe(c)
	if err != nil {
		refuse(w, err)
		return
	}
	defer sub.Close()
	// Accept keeps the headers set on w, so this one rides the 101 answer.
	w.Header().Set(VouchHeader, "1")
	w.Header().Set(PresenceHeader, "1")
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	conn.SetReadLimit(watchRead)
	serveWatch(r.Context(), conn, sub, c.device, wantsEvents(r))
}

// wantsEvents is whether the socket opted in to event frames (EventsQuery).
func wantsEvents(r *http.Request) bool { return r.URL.Query().Get("events") == "1" }

// VouchHeader on the 101 answer says this relay counts a watch socket that
// names a hold as proof its holder is alive (contract 21.11.1). A client that
// does not see it keeps beating.
const VouchHeader = "Codeaf-Vouch"

// PresenceHeader on the 101 answer says this relay counts a socket online only
// while its pings keep coming (contract 21.12.3).
const PresenceHeader = "Codeaf-Presence"

// subscribe applies every refusal that precedes the upgrade and takes a place
// under the identity's cap. An identity that is replaced can only ever be
// thawed, and a thaw is found by the client's slow backstop poll.
func subscribe(c call) (*Sub, error) {
	if !isUpgrade(c.r) {
		return nil, errUpgradeRequired
	}
	feed, ok := c.dir.(Watchable)
	if !ok {
		return nil, ErrNotFound
	}
	st := feed.Feed().Status()
	switch {
	case st.Stopped[c.device]:
		return nil, ErrRevoked
	case st.Rotation != nil:
		return nil, ErrRotated
	}
	holds, err := ParseHolds(c.r.URL.Query()["hold"])
	if err != nil {
		return nil, err
	}
	beat, err := ParseBeat(c.r.URL.Query()["beat"])
	if err != nil {
		return nil, err
	}
	return feed.Feed().SubscribeBeat(c.device, holds, beat)
}

func isUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// serveWatch runs one socket until it or the directory ends. The reader answers
// "ping" with "pong" and ignores every other frame, so its error is what ends
// the socket when the peer goes.
func serveWatch(ctx context.Context, conn *websocket.Conn, sub *Sub, device string, events bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer cancel()
		answerPings(ctx, conn, sub)
	}()
	// The version is the first frame; an event socket then hears who is online.
	first := func() {}
	if events {
		first = func() {
			events := sub.Events()
			go sendEvents(ctx, cancel, conn, events)
		}
	}
	conn.Close(tell(ctx, conn, sub, device, first))
}

// sendEvents writes each event frame of an event socket until it ends. A write
// that fails ends the socket, like a failed version write.
func sendEvents(ctx context.Context, end context.CancelFunc, conn *websocket.Conn, events <-chan dirwatch.Event) {
	for {
		select {
		case e := <-events:
			if err := sendEvent(ctx, conn, e); err != nil {
				end()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func sendEvent(ctx context.Context, conn *websocket.Conn, e dirwatch.Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return sendText(ctx, conn, string(b))
}

// tell sends each version the watcher has not heard, then closes the socket
// with the code that explains a stop, so a device always hears the bump first.
// first runs once, after the first version is written.
func tell(ctx context.Context, conn *websocket.Conn, sub *Sub, device string, first func()) (websocket.StatusCode, string) {
	for {
		st, err := sub.Next(ctx)
		if errors.Is(err, errSilent) {
			return CloseSilent, "silent"
		}
		if err != nil {
			return closing(ctx)
		}
		if err := sendVersion(ctx, conn, st.Version); err != nil {
			return websocket.StatusInternalError, "write failed"
		}
		first()
		first = func() {}
		if code, why := stopAfter(st, device); code != 0 {
			return code, why
		}
	}
}

// closing is the close for a socket whose wait ended: going away when the
// relay closed its directory, and a plain end when the peer hung up.
func closing(ctx context.Context) (websocket.StatusCode, string) {
	if ctx.Err() != nil {
		return websocket.StatusNormalClosure, ""
	}
	return websocket.StatusGoingAway, "closing"
}

// stopAfter is the close a Status calls for, or zero when the socket may stay.
func stopAfter(st Status, device string) (websocket.StatusCode, string) {
	switch {
	case st.Stopped[device]:
		return CloseRevoked, "revoked"
	case st.Rotation != nil:
		return CloseRotated, "rotated"
	}
	return 0, ""
}

func sendVersion(ctx context.Context, conn *websocket.Conn, v uint64) error {
	return sendText(ctx, conn, fmt.Sprintf(`{"v":%d}`, v))
}

// answerPings answers each ping, stamping the socket's sign of life first so a
// peer that has heard the pong can rely on it.
func answerPings(ctx context.Context, conn *websocket.Conn, sub *Sub) {
	for {
		kind, msg, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind == websocket.MessageText && string(msg) == "ping" {
			sub.Ping()
			if sendText(ctx, conn, "pong") != nil {
				return
			}
		}
	}
}

func sendText(ctx context.Context, conn *websocket.Conn, s string) error {
	ctx, cancel := context.WithTimeout(ctx, watchWrite)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, []byte(s))
}
