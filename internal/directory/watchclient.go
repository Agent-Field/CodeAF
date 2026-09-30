package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// Versioned is a Client that can say which directory version a listing is.
type Versioned interface {
	ListAt(ctx context.Context) (Listing, uint64, error)
}

// Watcher is a Client that can open the relay's watch socket.
type Watcher interface {
	Watch(ctx context.Context) (dirwatch.Stream, error)
}

var (
	_ Versioned = (*HTTP)(nil)
	_ Watcher   = (*HTTP)(nil)
)

// versionHeader carries the directory's version on a list answer.
const versionHeader = "Codeaf-Dir-Version"

// watchLimit is the most a frame may weigh. The frames are a few bytes, so a
// larger one is a fault and is refused before it is buffered.
const watchLimit = 1 << 10

// ListAt is List plus the version the relay read it at; 0 means the relay did
// not say, which a feed treats as "unknown, compare nothing".
func (h *HTTP) ListAt(ctx context.Context) (l Listing, version uint64, err error) {
	hdr, err := h.doHeader(ctx, http.MethodGet, dirBase+"/list", nil, &l)
	if err != nil {
		return l, 0, err
	}
	version, _ = strconv.ParseUint(hdr.Get(versionHeader), 10, 64)
	return l, version, nil
}

// ListVersioned lists through ListAt when the client has it, and otherwise
// lists with version 0, so a client without versions still feeds a watcher.
func ListVersioned(ctx context.Context, c Client) (Listing, uint64, error) {
	if v, ok := c.(Versioned); ok {
		return v.ListAt(ctx)
	}
	l, err := c.List(ctx)
	return l, 0, err
}

// Watch opens the signed watch socket. The upgrade request is signed like any
// other directory request, with an empty body.
func (h *HTTP) Watch(ctx context.Context) (dirwatch.Stream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+dirBase+"/watch", nil)
	if err != nil {
		return nil, err
	}
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
	return &socket{c: c}, nil
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
type socket struct{ c *websocket.Conn }

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

// decodeFrame reads `pong` or `{"v":N}`; fields it does not know are ignored
// so the server may add some.
func decodeFrame(data []byte) (dirwatch.Frame, error) {
	if string(data) == "pong" {
		return dirwatch.Frame{Pong: true}, nil
	}
	var f struct {
		V *uint64 `json:"v"`
	}
	if err := json.Unmarshal(data, &f); err != nil || f.V == nil {
		return dirwatch.Frame{}, fmt.Errorf("directory: undecodable watch frame %q", data)
	}
	return dirwatch.Frame{Version: *f.V}, nil
}
