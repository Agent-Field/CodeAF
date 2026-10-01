package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

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

// List reads the directory. The listing carries the version the relay read it
// at (0 when the relay does not say), which a feed compares with the versions
// it is told of.
func (h *HTTP) List(ctx context.Context) (l Listing, err error) {
	hdr, err := h.doHeader(ctx, http.MethodGet, dirBase+"/list", nil, &l)
	if err == nil {
		l.Version, _ = strconv.ParseUint(hdr.Get(VersionHeader), 10, 64)
	}
	return l, err
}

func (h *HTTP) Cell(ctx context.Context, id string) (v CellView, err error) {
	return v, h.do(ctx, http.MethodGet, cellPath(id, ""), nil, &v)
}

func (h *HTTP) PutDevice(ctx context.Context, id string, d Device) error {
	return h.do(ctx, http.MethodPut, dirBase+"/devices/"+url.PathEscape(id), d, nil)
}

func (h *HTTP) Revoke(ctx context.Context, id string) error {
	return h.do(ctx, http.MethodPost, dirBase+"/devices/"+url.PathEscape(id)+"/revoke", nil, nil)
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

func (h *HTTP) ApproveRequest(ctx context.Context, code string, a Approval) error {
	return h.do(ctx, http.MethodPost, requestPath(code, "approve"), a, nil)
}

func (h *HTTP) DenyRequest(ctx context.Context, code string) error {
	return h.do(ctx, http.MethodPost, requestPath(code, "deny"), nil, nil)
}

func requestPath(code, verb string) string {
	return dirBase + "/requests/" + url.PathEscape(code) + "/" + verb
}

func (h *HTTP) Rotate(ctx context.Context, req RotationReq) (v RotationView, err error) {
	return v, h.do(ctx, http.MethodPost, rotationPath, req, &v)
}

func (h *HTTP) Rotation(ctx context.Context) (v RotationView, err error) {
	return v, h.do(ctx, http.MethodGet, rotationPath, nil, &v)
}

// do sends one signed request and decodes the answer into out (when non-nil).
func (h *HTTP) do(ctx context.Context, method, path string, in, out any) error {
	_, err := h.doHeader(ctx, method, path, in, out)
	return err
}

// doHeader is do that also hands back the response headers, for the reads
// that carry a fact in a header as well as in the body.
func (h *HTTP) doHeader(ctx context.Context, method, path string, in, out any) (http.Header, error) {
	body, err := encode(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	h.sign(req, body)
	resp, err := h.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return resp.Header, refusal(resp)
	}
	if out == nil {
		return resp.Header, nil
	}
	if resp.StatusCode == http.StatusNoContent {
		return resp.Header, ErrStillPending // a long poll that ended with nothing new
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(out)
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
		if err := errorOf(b.Err, resp.StatusCode); err != nil {
			return wireauth.Wait(err, resp.Header)
		}
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrTooOld
	}
	return fmt.Errorf("directory: %s", resp.Status)
}
