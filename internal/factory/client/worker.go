package client

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
type Refusal struct {
	error
	Status int
}

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
//
// The live-capacity attestation goes in [api.LiveCapacityHeader], never in the
// JSON: a factory that predates the field decodes strictly and would answer 422
// to the body, but ignores a header. It is one POST either way, never retried.
func (w *WorkerClient) Attach(ctx context.Context, body api.WorkerAttach) (api.WorkerAttached, error) {
	var out api.WorkerAttached
	var extra http.Header
	if body.LiveCapacity {
		extra = http.Header{api.LiveCapacityHeader: {api.LiveCapacityHeaderValue}}
		body.LiveCapacity = false
	}
	_, err := w.sendWith(ctx, http.MethodPost, "/v1/workers/attach", w.token, extra, body, &out)
	if err == nil {
		w.credential = out.Credential
	}
	return out, err
}

// ErrSelfUnsupported is a factory that predates the capacity read. The worker
// keeps the capacity it has rather than guessing a new one.
var ErrSelfUnsupported = errors.New("factory worker: this factory does not report runner capacity")

// Self reads the capacity this runner's owner saved, with the worker's own
// credential. An older factory answers 404 or 405, which is
// [ErrSelfUnsupported]; an unknown credential is [ErrNotAttached].
func (w *WorkerClient) Self(ctx context.Context) (api.RunnerSelf, error) {
	var out api.RunnerSelf
	status, err := w.send(ctx, http.MethodGet, "/v1/runner/self", w.credential, nil, &out)
	if errors.Is(err, ErrLeaseGone) || status == http.StatusMethodNotAllowed {
		return api.RunnerSelf{}, ErrSelfUnsupported
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
	request, err := PrepareWorkerReport(report, "")
	if err != nil {
		return err
	}
	return w.ReportPrepared(ctx, lease, request)
}

// PreparedWorkerReport freezes the complete request, including compatibility
// headers, so a lost reply cannot turn a retry into a different cost delta.
type PreparedWorkerReport struct {
	Body    json.RawMessage `json:"body"`
	Headers http.Header     `json:"headers"`
}

func PrepareWorkerReport(report api.WorkerReport, key string) (PreparedWorkerReport, error) {
	// Keep new model evidence out of the JSON older factories decode strictly.
	// Copy the steps: callers may reuse their report after this send.
	// Check names are read-side detail; older strict factories accept the original PR shape.
	if report.PR != nil {
		cp := *report.PR
		cp.CheckRuns = nil
		report.PR = &cp
	}
	if report.Merge != nil && report.Merge.PR != nil {
		merge := *report.Merge
		pr := *merge.PR
		pr.CheckRuns = nil
		merge.PR = &pr
		report.Merge = &merge
	}
	models := map[string]string{}
	chats := map[string]string{}
	runtime := map[string]api.StepRuntime{}
	// A reshaped pipeline also uses Step's read-side runtime fields. Runtime
	// travels only with named step reports; leave pipeline JSON legacy-safe.
	report.Pipeline = append([]api.Step(nil), report.Pipeline...)
	for i := range report.Pipeline {
		report.Pipeline[i].RoundLimit, report.Pipeline[i].Verification = 0, nil
		// A step's model pick is the person's, which the factory keeps itself
		// (run.applyPipeline); a factory older than picks refuses the field.
		report.Pipeline[i].UseModel = ""
	}
	report.Steps = append([]api.StepReport(nil), report.Steps...)
	for i := range report.Steps {
		if chat := report.Steps[i].Chat; chat != "" {
			chats[report.Steps[i].Name] = chat
			report.Steps[i].Chat = ""
		}
		if model := report.Steps[i].Model; model != "" {
			models[report.Steps[i].Name] = model
			report.Steps[i].Model = ""
		}
		if report.Steps[i].RoundLimit > 0 || report.Steps[i].Verification != nil {
			runtime[report.Steps[i].Name] = api.StepRuntime{RoundLimit: report.Steps[i].RoundLimit, Verification: report.Steps[i].Verification}
		}
		report.Steps[i].RoundLimit, report.Steps[i].Verification = 0, nil
	}
	extra := http.Header{}
	if len(runtime) > 0 {
		raw, err := json.Marshal(runtime)
		if err != nil {
			return PreparedWorkerReport{}, err
		}
		if len(raw) > 64<<10 {
			return PreparedWorkerReport{}, errors.New("step runtime metadata exceeds 64 KiB")
		}
		extra.Set(api.StepRuntimeHeader, string(raw))
	}
	if len(chats) > 0 {
		raw, err := json.Marshal(chats)
		if err != nil {
			return PreparedWorkerReport{}, err
		}
		extra.Set(api.StepChatsHeader, string(raw))
	}
	if len(models) > 0 {
		raw, err := json.Marshal(models)
		if err != nil {
			return PreparedWorkerReport{}, err
		}
		extra.Set(api.StepModelsHeader, string(raw))
	}
	if key != "" {
		extra.Set("Idempotency-Key", key)
	}
	body, err := json.Marshal(report)
	return PreparedWorkerReport{Body: body, Headers: extra}, err
}

func (w *WorkerClient) ReportPrepared(ctx context.Context, lease string, request PreparedWorkerReport) error {
	_, err := w.sendWith(ctx, http.MethodPost, leasePath(lease, "report"), w.credential, request.Headers, request.Body, nil)
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
	return w.sendWith(ctx, method, path, bearer, nil, in, out)
}

// sendWith is send plus fixed headers chosen by this package; no caller outside
// it can name one.
func (w *WorkerClient) sendWith(ctx context.Context, method, path, bearer string, extra http.Header, in, out any) (int, error) {
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
	for name, values := range extra {
		req.Header[name] = values
	}
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
		return resp.StatusCode, Refusal{error: sentence(raw), Status: resp.StatusCode}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.Unmarshal(raw, out)
}

// Recover reconciles one retained report under the server's durable receipt.
// It is independent of a stale lease's report URL; the server checks ownership,
// current round and head before accepting any saved work or spending.
func (w *WorkerClient) Recover(ctx context.Context, id int, originalLease string, request PreparedWorkerReport, additionalCost float64) (api.WorkerRecovered, error) {
	var out api.WorkerRecovered
	_, err := w.send(ctx, http.MethodPost, fmt.Sprintf("/v1/workers/recover/%d", id), w.credential, api.WorkerRecovery{OriginalLease: originalLease, Body: request.Body, Headers: request.Headers, AdditionalCost: additionalCost}, &out)
	return out, err
}

// RecoveryRetry reads a typed user retry request without applying retained work.
func (w *WorkerClient) RecoveryRetry(ctx context.Context, id int) (time.Time, error) {
	var out api.WorkerRecovered
	_, err := w.send(ctx, http.MethodGet, fmt.Sprintf("/v1/workers/recover/%d", id), w.credential, nil, &out)
	return out.RetryRequestedAt, err
}
