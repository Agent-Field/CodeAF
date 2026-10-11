package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ErrNotAttached is a worker credential the factory does not know (it was
// restarted, or never issued it): attach again.
var ErrNotAttached = errors.New("factory worker: credential not known; attach again")

// ErrLeaseGone is a lease the factory no longer holds for this worker: the
// job was stopped, or the lease expired. The worker lets the job go.
var ErrLeaseGone = errors.New("factory worker: the lease is gone")

// Refusal is a request the factory read and refused, in its own sentence, as
// opposed to one that never reached it.
type Refusal struct{ error }

func (r Refusal) Unwrap() error { return r.error }

// WorkerClient is one machine's worker door into a factory: attach once, then
// pull, heartbeat, report and done with the credential attach answered.
type WorkerClient struct {
	*Client
	credential string
}

// Worker answers a worker door into the same factory as c. credential may be
// a cached one; an unknown one answers [ErrNotAttached] on first use.
func (c *Client) Worker(credential string) *WorkerClient {
	return &WorkerClient{Client: c, credential: credential}
}

// Base is the factory's origin this worker talks to.
func (w *WorkerClient) Base() string { return w.base }

// Credential is the bearer secret this worker holds now.
func (w *WorkerClient) Credential() string { return w.credential }

// Attach registers the machine and keeps the credential the factory answers.
// The access token, when the client has one, is what lets it in off loopback.
func (w *WorkerClient) Attach(ctx context.Context, body api.WorkerAttach) (api.WorkerAttached, error) {
	var out api.WorkerAttached
	_, err := w.send(ctx, http.MethodPost, "/v1/workers/attach", w.token, body, &out)
	if err == nil {
		w.credential = out.Credential
	}
	return out, err
}

// Pull waits up to the factory's long-poll bound for a job. ok is false when
// none was offered.
func (w *WorkerClient) Pull(ctx context.Context) (api.WorkerPull, bool, error) {
	var out api.WorkerPull
	status, err := w.send(ctx, http.MethodGet, "/v1/workers/pull", w.credential, nil, &out)
	return out, err == nil && status == http.StatusOK, err
}

// Heartbeat renews the lease and collects the verbs waiting for it.
func (w *WorkerClient) Heartbeat(ctx context.Context, lease string) (api.WorkerHeartbeat, error) {
	var out api.WorkerHeartbeat
	_, err := w.send(ctx, http.MethodPost, leasePath(lease, "heartbeat"), w.credential, struct{}{}, &out)
	return out, err
}

// Report sends what the runner wrote since the last report.
func (w *WorkerClient) Report(ctx context.Context, lease string, report api.WorkerReport) error {
	_, err := w.send(ctx, http.MethodPost, leasePath(lease, "report"), w.credential, report, nil)
	return err
}

// Done names the job's final state and gives the lease back.
func (w *WorkerClient) Done(ctx context.Context, lease string, done api.WorkerDone) error {
	_, err := w.send(ctx, http.MethodPost, leasePath(lease, "done"), w.credential, done, nil)
	return err
}

func leasePath(lease, verb string) string {
	return "/v1/workers/leases/" + url.PathEscape(lease) + "/" + verb
}

// send is one worker request with bearer as its credential. The statuses
// a worker acts on are errors of their own; any other refusal is the
// server's sentence.
func (w *WorkerClient) send(ctx context.Context, method, path, bearer string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.base+path, body)
	if err != nil {
		return 0, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	switch {
	case resp.StatusCode == http.StatusNoContent:
		return resp.StatusCode, nil
	case resp.StatusCode == http.StatusUnauthorized && path != "/v1/workers/attach":
		return resp.StatusCode, ErrNotAttached
	case resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return resp.StatusCode, ErrLeaseGone
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return resp.StatusCode, Refusal{sentence(raw)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.Unmarshal(raw, out)
}
