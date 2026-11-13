// Package clusterapi carries signed scoped requests to the one coordination
// path. It owns no retry loop, authority cache, member store or tool execution.
package clusterapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

type Client struct {
	Now     func() time.Time
	Base    string
	HTTP    *http.Client
	Request func() cs.ClusterRequest
	Sign    func([]byte) []byte
	Allowed func() bool
}

func (c Client) call(ctx context.Context, action string, command *cs.Command, cell string, ref *cs.AssignmentRef) (cs.ClusterResponse, error) {
	var result cs.ClusterResponse
	if c.Allowed == nil || !c.Allowed() {
		return result, errors.New("sync is waiting until this computer is allowed to connect")
	}
	if c.Request == nil || c.Sign == nil {
		return result, errors.New("cluster credentials are unavailable")
	}
	request := c.Request()
	request.Action, request.Command, request.Cell, request.Assignment = action, command, cell, ref
	now := c.Now
	if now == nil {
		now = time.Now
	}
	request.At = now().UnixMilli()
	raw, err := cs.ClusterRequestBytes(request)
	if err != nil {
		return result, err
	}
	request.Signature = c.Sign(raw)
	raw, err = cs.Encode(request)
	if err != nil || len(raw) > cs.MaxCommandBytes {
		return result, cs.ErrCodec
	}
	if _, err = cs.VerifyClusterRequest(request, request.At); err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Base, "/")+cs.ClusterAPIPath, bytes.NewReader(raw))
	if err != nil {
		return result, err
	}
	// The root header is a routing hint only; the signed body binds the same key.
	req.Header.Set("codeaf-identity", base64.RawURLEncoding.EncodeToString(request.IdentityKey))
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 30 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	if client.Timeout <= 0 || client.Timeout > 30*time.Second {
		client.Timeout = 30 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// Recheck at the actual send as retained handles may outlive an off decision.
	client.Transport = guard{next: client.Transport, allowed: c.Allowed}
	res, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer res.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(res.Body, cs.MaxEvaluationBytes+1))
	if err != nil {
		return result, err
	}
	if res.StatusCode != http.StatusOK || len(raw) > cs.MaxEvaluationBytes {
		return result, errors.New("the selected work is unavailable; refresh before trying again")
	}
	if err = cs.Decode(raw, &result); err != nil {
		return result, err
	}
	return result, nil
}
func (c Client) Submit(ctx context.Context, command cs.Command) (cs.Result, error) {
	out, err := c.call(ctx, "submit", &command, "", nil)
	if err == nil && out.Result == nil {
		err = cs.ErrCodec
	}
	if err != nil {
		return cs.Result{}, err
	}
	return *out.Result, nil
}
func (c Client) Cell(ctx context.Context, cell string) (cs.CellView, []byte, error) {
	out, err := c.call(ctx, "cell", nil, cell, nil)
	if err == nil && out.View == nil {
		err = cs.ErrCodec
	}
	if err != nil {
		return cs.CellView{}, nil, err
	}
	return *out.View, out.Grant, nil
}
func (c Client) Current(ctx context.Context, ref cs.AssignmentRef) (cs.Assignment, cs.CellView, error) {
	out, err := c.call(ctx, "assignment", nil, "", &ref)
	if err == nil && (out.Assignment == nil || out.View == nil) {
		err = cs.ErrCodec
	}
	if err != nil {
		return cs.Assignment{}, cs.CellView{}, err
	}
	return *out.Assignment, *out.View, nil
}

type guard struct {
	next    http.RoundTripper
	allowed func() bool
}

func (g guard) RoundTrip(r *http.Request) (*http.Response, error) {
	if !g.allowed() {
		return nil, errors.New("sync is off on this computer")
	}
	next := g.next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(r)
}
