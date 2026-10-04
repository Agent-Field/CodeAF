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
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// HTTP is a Box over the wire, and cannot tell a self-hosted relay from a
// hosted one, which is the point.
type HTTP struct {
	base string
	hc   *http.Client

	// gens is the opening of each nameplate this client has been told, which it
	// sends back so a later mailbox under the same plate cannot answer for it.
	mu   sync.Mutex
	gens map[string]string
}

// NewHTTP reaches the relay at base. The client's timeout must outlast MaxWait,
// or a long poll is cut off by the caller instead of ended by the relay.
func NewHTTP(base string, hc *http.Client) *HTTP {
	return &HTTP{base: base, hc: hc, gens: map[string]string{}}
}

var _ Box = (*HTTP)(nil)

// ErrTooOld is a relay that does not serve the pairing wire at all.
var ErrTooOld = errors.New("pairbox: that relay is too old for pairing")

// Limits asks the relay for its numbers. A relay from before pairing answers
// 404, which is reported as ErrTooOld so a device can say so.
func (h *HTTP) Limits(ctx context.Context) (Limits, error) {
	var out limitsBody
	_, err := h.do(ctx, http.MethodGet, "", "/limits", nil, nil, http.StatusOK, &out)
	if errors.Is(err, ErrGone) {
		return Limits{}, ErrTooOld
	}
	out.Limits.TTL = time.Duration(out.TTLMS) * time.Millisecond
	return out.Limits, err
}

// Create opens a mailbox.
func (h *HTTP) Create(ctx context.Context, key Key) (Created, error) {
	var out createdBody
	reply, err := h.do(ctx, http.MethodPost, "", "", key.header(), nil, http.StatusCreated, &out)
	h.keep(out.Nameplate, reply)
	return Created{out.Nameplate, time.Duration(out.ExpiresMS) * time.Millisecond}, err
}

// Post appends a message to a side.
func (h *HTTP) Post(ctx context.Context, plate string, side Side, key Key, msg []byte) (int, error) {
	var out postedBody
	_, err := h.do(ctx, http.MethodPost, plate, sidePath(plate, side), key.header(), msg, http.StatusCreated, &out)
	return out.N, err
}

// Poll reads what a side has written after a position.
func (h *HTTP) Poll(ctx context.Context, plate string, side Side, after int, wait time.Duration) (Batch, error) {
	query := url.Values{"after": {fmt.Sprint(after)}, "wait": {fmt.Sprint(wait.Milliseconds())}}
	var out batchBody
	_, err := h.do(ctx, http.MethodGet, plate, sidePath(plate, side)+"?"+query.Encode(), nil, nil, http.StatusOK, &out)
	if errors.Is(err, errNoContent) {
		return Batch{Next: after}, nil
	}
	return Batch{Msgs: out.Msgs, Next: out.Next}, err
}

// Delete removes a mailbox.
func (h *HTTP) Delete(ctx context.Context, plate string, key Key) error {
	_, err := h.do(ctx, http.MethodDelete, plate, "/"+plate, key.header(), nil, http.StatusNoContent, nil)
	return err
}

func (k Key) header() http.Header { return http.Header{KeyHeader: {k.String()}} }

func sidePath(plate string, side Side) string { return "/" + plate + "/" + string(side) }

// errNoContent is a 204: a wait that ended with nothing to say.
var errNoContent = errors.New("pairbox: no content")

// keep remembers the opening a relay told for plate, the first time it does. A
// plate the relay says nothing about is never fenced.
func (h *HTTP) keep(plate string, reply http.Header) {
	gen := reply.Get(GenHeader)
	if plate == "" || gen == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, known := h.gens[plate]; !known {
		h.gens[plate] = gen
	}
}

// forget drops what is known of plate, once its mailbox is gone.
func (h *HTTP) forget(plate string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.gens, plate)
}

// opening is the header that names the opening of plate this client was told, if any.
func (h *HTTP) opening(plate string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gens[plate]
}

// do sends one request about plate ("" for none) and turns the answer into a
// decoded body or one of the package's errors, and gives back the answer's
// headers. want is the status that means success.
func (h *HTTP) do(ctx context.Context, method, plate, tail string, header http.Header, body []byte, want int, into any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, wireauth.Endpoint(h.base, Path+tail), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	if gen := h.opening(plate); gen != "" {
		req.Header.Set(GenHeader, gen)
	}
	if peer, ok := ctx.Value(peerKey{}).(string); ok {
		req.Header.Set(PeerHeader, peer)
	}
	resp, err := h.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	h.keep(plate, resp.Header)
	err = answer(resp, want, into)
	if plate != "" && (errors.Is(err, ErrGone) || method == http.MethodDelete && err == nil) {
		h.forget(plate)
	}
	return resp.Header, err
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
