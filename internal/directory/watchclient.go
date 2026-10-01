package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Watcher is a Client that can open the relay's watch socket.
type Watcher interface {
	Watch(ctx context.Context) (dirwatch.Stream, error)
}

// HoldWatcher is a Watcher whose socket can name the leases its holder holds,
// so the relay can count the socket as proof that the holder is alive.
type HoldWatcher interface {
	Watcher
	WatchHolding(ctx context.Context, holds []Hold) (dirwatch.Stream, error)
}

var (
	_ Watcher     = (*HTTP)(nil)
	_ HoldWatcher = (*HTTP)(nil)
)

// vouchHeader is how a relay says, in the upgrade answer, that it counts a
// socket as proof of life for the leases the socket names. A relay that does
// not send it is an old one that ignores the names.
const vouchHeader = "Codeaf-Vouch"

// watchLimit is the most a frame may weigh. The frames are a few bytes, so a
// larger one is a fault and is refused before it is buffered.
const watchLimit = 1 << 10

// Watch opens the signed watch socket of a screen: it names no holds, so it
// vouches for nothing, and it asks for events (EventsQuery), so its Stream
// also carries who is online and who joined. The upgrade request is signed like any other directory
// request, with an empty body.
func (h *HTTP) Watch(ctx context.Context) (dirwatch.Stream, error) {
	return h.dial(ctx, nil, true)
}

// WatchHolding opens the watch socket of a process that holds leases, naming
// them in the query. The query is part of the signed uri, so only this device
// can name a hold.
func (h *HTTP) WatchHolding(ctx context.Context, holds []Hold) (dirwatch.Stream, error) {
	return h.dial(ctx, holds, false)
}

// dial opens the socket naming holds, and asking for events when events is set.
func (h *HTTP) dial(ctx context.Context, holds []Hold, events bool) (dirwatch.Stream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wireauth.Endpoint(h.base, dirBase+"/watch"), nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = watchQuery(holds, events)
	h.sign(req, nil)
	// The library applies the client's Timeout to the dial alone and clears it
	// for the socket, so the shared client is safe to hand over.
	c, resp, err := websocket.Dial(ctx, req.URL.String(), &websocket.DialOptions{
		HTTPClient: h.hc,
		HTTPHeader: req.Header,
	})
	if err != nil {
		return nil, dialFailure(resp, err)
	}
	c.SetReadLimit(watchLimit)
	return &socket{c: c, vouching: resp.Header.Get(vouchHeader) == "1"}, nil
}

// watchRefusals pairs each refusal the feed must stop on with the dirwatch
// sentinel that says it, so one error matches both vocabularies.
var watchRefusals = []struct{ dir, watch error }{
	{ErrRevoked, dirwatch.ErrRevoked},
	{ErrRotated, dirwatch.ErrRotated},
}

// dialFailure names why the upgrade did not happen.
func dialFailure(resp *http.Response, err error) error {
	if resp == nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return dirwatch.ErrNoRoute
	}
	refused := refusal(resp)
	for _, r := range watchRefusals {
		if errors.Is(refused, r.dir) {
			return fmt.Errorf("%w: %w", r.watch, refused)
		}
	}
	return refused
}

// socket is the watch socket as the feed reads it.
type socket struct {
	c        *websocket.Conn
	vouching bool
}

// Next reads one text frame: a version, or the answer to a ping.
func (s *socket) Next(ctx context.Context) (dirwatch.Frame, error) {
	_, data, err := s.c.Read(ctx)
	if err != nil {
		return dirwatch.Frame{}, closeSentinel(err)
	}
	return decodeFrame(data)
}

// Ping writes the text the server answers with a pong.
func (s *socket) Ping(ctx context.Context) error {
	return s.c.Write(ctx, websocket.MessageText, []byte("ping"))
}

// Vouching is whether the upgrade answer carried the vouch header.
func (s *socket) Vouching() bool { return s.vouching }

// Close says goodbye without waiting for the server to answer.
func (s *socket) Close() { _ = s.c.Close(websocket.StatusNormalClosure, "") }

// closeSentinel turns the server's refusal close codes into their sentinels.
func closeSentinel(err error) error {
	switch websocket.CloseStatus(err) {
	case dirwatch.CloseRevoked:
		return dirwatch.ErrRevoked
	case dirwatch.CloseRotated:
		return dirwatch.ErrRotated
	}
	return err
}

// decodeFrame reads `pong`, `{"v":N}` or an event frame `{"t":...}`; fields it
// does not know are ignored so the server may add some, and an event of a kind
// it does not know is passed on for the feed to ignore.
func decodeFrame(data []byte) (dirwatch.Frame, error) {
	if string(data) == "pong" {
		return dirwatch.Frame{Pong: true}, nil
	}
	var f struct {
		V *uint64 `json:"v"`
		dirwatch.Event
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return dirwatch.Frame{}, fmt.Errorf("directory: undecodable watch frame %q", data)
	}
	switch {
	case f.V != nil:
		return dirwatch.Frame{Version: *f.V}, nil
	case f.T != "":
		return dirwatch.Frame{Event: &f.Event}, nil
	}
	return dirwatch.Frame{}, fmt.Errorf("directory: undecodable watch frame %q", data)
}
