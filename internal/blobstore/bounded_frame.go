package blobstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"io"
	"net/http"
)

// ErrBoundedFrameUnsupported means this adapter cannot enforce an application
// read bound. Callers must degrade rather than use unbounded GetFrame.
var ErrBoundedFrameUnsupported = errors.New("blobstore: bounded frame reads unavailable")

// BoundedFrameStore is optional; Store and its ordinary behavior stay unchanged.
// maxEncoded excludes one reserved EOF/oversize probe byte. It bounds consumed
// response bytes, not packets already buffered by a transport.
type BoundedFrameStore interface {
	GetFrameBounded(context.Context, string, int64) ([]byte, error)
}

func ReadFrameBounded(ctx context.Context, store Store, id string, maxEncoded int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ValidRID(id) {
		return nil, ErrBadRID
	}
	if maxEncoded <= 0 || maxEncoded >= MaxFrame {
		return nil, ErrDamaged
	}
	bounded, ok := store.(BoundedFrameStore)
	if !ok {
		return nil, ErrBoundedFrameUnsupported
	}
	return bounded.GetFrameBounded(ctx, id, maxEncoded)
}

// GetFrameBounded verifies complete exact-size immutable bytes with at most one
// probe byte beyond maxEncoded. A claimed Content-Length never expands the cap.
func (c *HTTP) GetFrameBounded(ctx context.Context, id string, maxEncoded int64) ([]byte, error) {
	if !ValidRID(id) {
		return nil, ErrBadRID
	}
	if maxEncoded <= 0 || maxEncoded >= MaxFrame {
		return nil, ErrDamaged
	}
	ctx, limit := startBudget(ctx, c.deadline, 0)
	defer limit.done()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wireauth.Endpoint(c.base, pathFrames+"/"+id), nil)
	if err != nil {
		return nil, err
	}
	c.sign(req, nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: bounded frame refused", ErrUnreachable)
	}
	if resp.ContentLength > maxEncoded {
		return nil, ErrDamaged
	}
	limit.answering(resp.ContentLength)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxEncoded+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != maxEncoded || IDOf(raw) != id {
		return nil, ErrDamaged
	}
	return raw, nil
}
