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
	"strings"
	"sync"

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
		headTakeServices.Store(h.serviceKey(), hdr.Get(HeadTakeHeader) == "1")
	}
	return l, err
}

func (h *HTTP) Cell(ctx context.Context, id string) (v CellView, err error) {
	hdr, err := h.doHeader(ctx, http.MethodGet, cellPath(id, ""), nil, &v)
	if err == nil {
		conditionalServices.Store(h.serviceKey(), hdr.Get(ConditionalTakeHeader) == "1")
		headTakeServices.Store(h.serviceKey(), hdr.Get(HeadTakeHeader) == "1")
	}
	return v, err
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
	if in.SavedAt != 0 && !h.savedTurnTimes(ctx, id) {
		in.SavedAt = 0
	}
	return v, h.do(ctx, http.MethodPost, cellPath(id, ""), in, &v)
}

func (h *HTTP) Acquire(ctx context.Context, id string, o AcquireOpts) (v CellView, err error) {
	// Only displacing a live holder needs the promise. A take of a lapsed lease
	// is safe on every service: an older one still refuses a live holder
	// atomically, raises the fence, and answers the head the caller compares
	// with the one it saw, so a person is never kept from their own chat (#2072).
	if o.Force {
		if err := h.requireConditionalTake(ctx, id); err != nil {
			return v, err
		}
	}
	if o.Expect != nil && o.Expect.Head != "" {
		if supported, _ := headTakeServices.Load(h.serviceKey()); supported != true {
			if _, err := h.Cell(ctx, id); err != nil {
				return v, err
			}
			if supported, _ = headTakeServices.Load(h.serviceKey()); supported != true {
				return v, ErrConditionalTake
			}
		}
	}
	return v, h.do(ctx, http.MethodPost, cellPath(id, "acquire"), o, &v)
}

// HeadTakeHeader advertises atomic saved-head checks on conditional takes.
const HeadTakeHeader = "Codeaf-Head-Takes"

var headTakeServices sync.Map

// HeadTakes reports the service's last advertised atomic head-check support.
// Callers omit head observations on older relays, preserving their take wire.
func (h *HTTP) HeadTakes() bool {
	known, _ := headTakeServices.Load(h.serviceKey())
	return known == true
}

// ConditionalTakeHeader is the service's promise that a take compares the
// expected holder and fence atomically. Older services omit it.
const ConditionalTakeHeader = "codeaf-conditional-takes"

// conditionalServices holds each service's latest promise for this process,
// across device clients. Every successful read refreshes it and failed reads
// never count as an answer. A promise is trusted until a read says otherwise;
// its absence is checked again before each take, so a service updated while
// codeaf runs moves chats at the next attempt instead of after a restart.
var conditionalServices sync.Map

// SavedTurnTimeHeader advertises optional sealed-head timestamps. Older
// relays strictly decode signed bodies, so clients omit the field without it.
const SavedTurnTimeHeader = "Codeaf-Saved-Turn-Times"

var savedTimeServices sync.Map

func (h *HTTP) savedTurnTimes(ctx context.Context, id string) bool {
	if known, ok := savedTimeServices.Load(h.serviceKey()); ok {
		return known == true
	}
	// Ordinary startup and drive reads already teach the capability. A direct
	// first publish probes once; inability to learn it keeps the older wire.
	_, _ = h.Cell(ctx, id)
	known, _ := savedTimeServices.Load(h.serviceKey())
	return known == true
}

func (h *HTTP) serviceKey() string { return strings.TrimRight(h.base, "/") }

func (h *HTTP) requireConditionalTake(ctx context.Context, id string) error {
	if answer, _ := conditionalServices.Load(h.serviceKey()); answer == true {
		return nil
	}
	if _, err := h.Cell(ctx, id); err != nil {
		return err
	}
	if answer, _ := conditionalServices.Load(h.serviceKey()); answer != true {
		return ErrConditionalTake
	}
	return nil
}

func (h *HTTP) Heartbeat(ctx context.Context, id string, b Beat) (v CellView, err error) {
	return v, h.do(ctx, http.MethodPost, cellPath(id, "heartbeat"), b, &v)
}

func (h *HTTP) Publish(ctx context.Context, id string, p Publish) (v CellView, err error) {
	if p.SavedAt != 0 && !h.savedTurnTimes(ctx, id) {
		p.SavedAt = 0
	}
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
	req, err := http.NewRequestWithContext(ctx, method, wireauth.Endpoint(h.base, path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	h.sign(req, body)
	resp, err := h.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusInternalServerError {
		savedTimeServices.Store(h.serviceKey(), resp.Header.Get(SavedTurnTimeHeader) == "1")
	}
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
	if resp.StatusCode >= http.StatusInternalServerError {
		// Coordination can refuse before a directory handler has an error
		// body. It is the same retryable refusal the coded answer names, not
		// an unreachable network, so closing releases retry it and nothing
		// tells a person to check a connection that works.
		return fmt.Errorf("%w: %s", wireauth.ErrUnavailable, resp.Status)
	}
	return fmt.Errorf("directory: %s", resp.Status)
}

// ReleaseQuiet lets background preparation go without announcing a quit.
func (h *HTTP) ReleaseQuiet(ctx context.Context, id string, fence uint64) error {
	return h.do(ctx, http.MethodPost, cellPath(id, "release"), fenceBody{Fence: fence, Quiet: true}, nil)
}
