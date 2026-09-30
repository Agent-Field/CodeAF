package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// HTTP is a Client that speaks the directory wire. Which device it acts as is
// decided by the Sign func it was built with, never by anything it sends.
type HTTP struct {
	base string
	sign wireauth.Sign
	hc   *http.Client
}

// NewHTTP returns the Client for the device that sign stamps requests as.
func NewHTTP(base string, sign wireauth.Sign, hc *http.Client) *HTTP {
	return &HTTP{base: base, sign: sign, hc: hc}
}

var _ Client = (*HTTP)(nil)

func cellPath(id, verb string) string {
	p := dirBase + "/cells/" + url.PathEscape(id)
	if verb != "" {
		p += "/" + verb
	}
	return p
}

func (h *HTTP) List(ctx context.Context) (l Listing, err error) {
	return l, h.do(ctx, http.MethodGet, dirBase+"/list", nil, &l)
}

func (h *HTTP) Cell(ctx context.Context, id string) (v CellView, err error) {
	return v, h.do(ctx, http.MethodGet, cellPath(id, ""), nil, &v)
}

func (h *HTTP) PutDevice(ctx context.Context, id string, d Device) error {
	return h.do(ctx, http.MethodPut, dirBase+"/devices/"+url.PathEscape(id), d, nil)
}

func (h *HTTP) SetVault(ctx context.Context, old, next string) error {
	return h.do(ctx, http.MethodPost, dirBase+"/vault", vaultSwap{Old: old, New: next}, nil)
}

func (h *HTTP) Create(ctx context.Context, id string, in CellInit) (v CellView, err error) {
	return v, h.do(ctx, http.MethodPost, cellPath(id, ""), in, &v)
}

func (h *HTTP) Acquire(ctx context.Context, id string, o AcquireOpts) (v CellView, err error) {
	return v, h.do(ctx, http.MethodPost, cellPath(id, "acquire"), o, &v)
}

func (h *HTTP) Heartbeat(ctx context.Context, id string, b Beat) (v CellView, err error) {
	return v, h.do(ctx, http.MethodPost, cellPath(id, "heartbeat"), b, &v)
}

func (h *HTTP) Publish(ctx context.Context, id string, p Publish) (v CellView, err error) {
	return v, h.do(ctx, http.MethodPost, cellPath(id, "publish"), p, &v)
}

func (h *HTTP) Release(ctx context.Context, id string, fence uint64) error {
	return h.do(ctx, http.MethodPost, cellPath(id, "release"), fenceBody{Fence: fence}, nil)
}

func (h *HTTP) Archive(ctx context.Context, id string) error {
	return h.do(ctx, http.MethodPost, cellPath(id, "archive"), nil, nil)
}

// do sends one signed request and decodes the answer into out (when non-nil).
func (h *HTTP) do(ctx context.Context, method, path string, in, out any) error {
	body, err := encode(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	h.sign(req, body)
	resp, err := h.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return refusal(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// encode marshals a body; a request without one sends no bytes, so the signed
// body is exactly what the server reads.
func encode(in any) ([]byte, error) {
	if in == nil {
		return nil, nil
	}
	return json.Marshal(in)
}

// refusal turns an error response back into the sentinel its code names.
func refusal(resp *http.Response) error {
	var b errBody
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if json.Unmarshal(raw, &b) == nil {
		if err := errorOf(b.Err); err != nil {
			return err
		}
	}
	return fmt.Errorf("directory: %s", resp.Status)
}
