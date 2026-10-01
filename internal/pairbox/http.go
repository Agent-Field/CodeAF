package pairbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// HTTP is a Box over the wire, and cannot tell a self-hosted relay from a
// hosted one, which is the point.
type HTTP struct {
	base string
	hc   *http.Client
}

// NewHTTP reaches the relay at base. The client's timeout must outlast MaxWait,
// or a long poll is cut off by the caller instead of ended by the relay.
func NewHTTP(base string, hc *http.Client) *HTTP {
	return &HTTP{base: base, hc: hc}
}

var _ Box = (*HTTP)(nil)

// ErrTooOld is a relay that does not serve the pairing wire at all.
var ErrTooOld = errors.New("pairbox: that relay is too old for pairing")

// Limits asks the relay for its numbers. A relay from before pairing answers
// 404, which is reported as ErrTooOld so a device can say so.
func (h *HTTP) Limits(ctx context.Context) (Limits, error) {
	var out limitsBody
	err := h.do(ctx, http.MethodGet, "/limits", nil, nil, http.StatusOK, &out)
	if errors.Is(err, ErrGone) {
		return Limits{}, ErrTooOld
	}
	out.Limits.TTL = time.Duration(out.TTLMS) * time.Millisecond
	return out.Limits, err
}

// Create opens a mailbox.
func (h *HTTP) Create(ctx context.Context, key Key) (Created, error) {
	var out createdBody
	err := h.do(ctx, http.MethodPost, "", key.header(), nil, http.StatusCreated, &out)
	return Created{out.Nameplate, time.Duration(out.ExpiresMS) * time.Millisecond}, err
}

// Post appends a message to a side.
func (h *HTTP) Post(ctx context.Context, plate string, side Side, key Key, msg []byte) (int, error) {
	var out postedBody
	err := h.do(ctx, http.MethodPost, sidePath(plate, side), key.header(), msg, http.StatusCreated, &out)
	return out.N, err
}

// Poll reads what a side has written after a position.
func (h *HTTP) Poll(ctx context.Context, plate string, side Side, after int, wait time.Duration) (Batch, error) {
	query := url.Values{"after": {fmt.Sprint(after)}, "wait": {fmt.Sprint(wait.Milliseconds())}}
	var out batchBody
	err := h.do(ctx, http.MethodGet, sidePath(plate, side)+"?"+query.Encode(), nil, nil, http.StatusOK, &out)
	if errors.Is(err, errNoContent) {
		return Batch{Next: after}, nil
	}
	return Batch{Msgs: out.Msgs, Next: out.Next}, err
}

// Delete removes a mailbox.
func (h *HTTP) Delete(ctx context.Context, plate string, key Key) error {
	return h.do(ctx, http.MethodDelete, "/"+plate, key.header(), nil, http.StatusNoContent, nil)
}

func (k Key) header() http.Header { return http.Header{KeyHeader: {k.String()}} }

func sidePath(plate string, side Side) string { return "/" + plate + "/" + string(side) }

// errNoContent is a 204: a wait that ended with nothing to say.
var errNoContent = errors.New("pairbox: no content")

// do sends one request and turns the answer into a decoded body or one of the
// package's errors. want is the status that means success.
func (h *HTTP) do(ctx context.Context, method, tail string, header http.Header, body []byte, want int, into any) error {
	req, err := http.NewRequestWithContext(ctx, method, wireauth.Endpoint(h.base, Path+tail), bytes.NewReader(body))
	if err != nil {
		return err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	if peer, ok := ctx.Value(peerKey{}).(string); ok {
		req.Header.Set(PeerHeader, peer)
	}
	resp, err := h.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return answer(resp, want, into)
}

func answer(resp *http.Response, want int, into any) error {
	if resp.StatusCode == want {
		return decode(resp.Body, into)
	}
	if resp.StatusCode == http.StatusNoContent {
		return errNoContent
	}
	err, ok := errorFor(resp.StatusCode)
	switch {
	case !ok:
		return fmt.Errorf("pairbox: the relay answered %d", resp.StatusCode)
	case errors.Is(err, ErrRateLimited):
		return RateLimited{RetryAfter: retryAfter(resp.Header)}
	}
	return err
}

func decode(r io.Reader, into any) error {
	if into == nil {
		return nil
	}
	return json.NewDecoder(r).Decode(into)
}
