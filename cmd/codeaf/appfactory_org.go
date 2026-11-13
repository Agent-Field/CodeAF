package main

// ── AN ORGANIZATION'S TEAMS AND WORKSTREAMS, CARRIED ───────────────────────
//
// appwire builds each organization request (factory_org.go): the method, the
// escaped route, the body and the rev a guarded write is sent with. This half
// carries it to the connection with the session held for it and reads the
// answer back. It keeps nothing: the organization's server is the only copy.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/redact"
)

var _ appwire.FactoryOrg = (*appBackend)(nil)

// FactoryOrgCall sends req once. A read that cannot reach the server is
// unreachable; a write that may have left is uncertain, never retried. A 2xx
// answer is decoded into out (a 204 leaves it as it is).
func (b *appBackend) FactoryOrgCall(ctx context.Context, id string, req appwire.OrgRequest, out any) error {
	h := b.factoryHub()
	row, token, err := h.credentialed(ctx, id)
	if err != nil {
		return err
	}
	write := req.Method != http.MethodGet
	var body io.Reader
	if req.Body != nil {
		raw, err := json.Marshal(req.Body)
		if err != nil {
			return appwire.Errorf(appwire.CodeBadParams, "that is not a request an organization can take")
		}
		body = bytes.NewReader(raw)
	}
	r, err := http.NewRequestWithContext(ctx, req.Method, row.URL+req.Path, body)
	if err != nil {
		return appwire.Errorf(appwire.CodeBadParams, "that is not a request an organization can take")
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "codeaf-app")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if req.IfMatch != "" {
		r.Header.Set("If-Match", req.IfMatch)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.client.Do(r)
	if err != nil {
		if write {
			return orgUncertain(appFactoryWriteError(err))
		}
		return appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
	}
	defer resp.Body.Close()
	// A workstream carries its pages whole, so the answer may be larger than
	// a job write's; it is still bounded, and refused rather than cut.
	data, err := io.ReadAll(io.LimitReader(resp.Body, appFactoryBody+1))
	if err != nil || int64(len(data)) > appFactoryBody {
		if write {
			return appwire.Errorf(appwire.CodeUncertain, "the organization's answer could not be read; the change may have happened, so read it again before trying again")
		}
		if err != nil {
			return appwire.Errorf(appwire.CodeUnreachable, "the factory stopped answering")
		}
		return appwire.Errorf(appwire.CodeTooLarge, "the factory's answer is too large to read")
	}
	if err := h.staleCredential(id, token); err != nil && !write {
		return err
	}
	reply := appFactoryReply{status: resp.StatusCode, header: resp.Header, body: data}
	if reply.status < 200 || reply.status >= 300 {
		err := orgFailure(reply, write)
		if w, ok := err.(*appwire.Error); ok && w.Code == appwire.CodeSignedOut {
			h.refused(id, token)
		}
		return err
	}
	h.touch(id)
	if reply.status == http.StatusNoContent || len(bytes.TrimSpace(data)) == 0 || out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		if write {
			return appwire.Errorf(appwire.CodeUncertain, "the organization did not confirm the change; read it again before trying again")
		}
		return appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	return nil
}

// orgFailure is the wire error for an organization's refusal, with the
// contract's statuses kept: 403 forbidden, 409 conflict, 412 conflict whose
// reason is "stale" (the workstream moved past the rev it was read at). A 5xx
// on a write is uncertain, since the server may have acted before it failed.
// Everything else is the factory's usual mapping (signed out, not found, busy).
func orgFailure(r appFactoryReply, write bool) error {
	words := orgWords(r)
	or := func(fallback string) string {
		if words == "" {
			return fallback
		}
		return words
	}
	switch {
	case r.status == http.StatusPreconditionFailed:
		e := appwire.Errorf(appwire.CodeConflict, "%s", or("this workstream changed since you read it; read it again"))
		e.Detail = &appwire.FactoryRefusal{Reason: "stale"}
		return e
	case r.status == http.StatusForbidden:
		return appwire.Errorf(appwire.CodeForbidden, "%s", or("the organization does not let you do that"))
	case r.status == http.StatusConflict:
		return appwire.Errorf(appwire.CodeConflict, "%s", or("the organization refused that as things stand"))
	case write && r.status >= 500:
		const maybe = "the change may have happened, so read it again before trying again"
		if words != "" {
			return appwire.Errorf(appwire.CodeUncertain, "%s · %s", words, maybe)
		}
		return appwire.Errorf(appwire.CodeUncertain, "the organization failed while changing it; %s", maybe)
	}
	return appFactoryFailure(r)
}

// orgWords is the server's own sentence, with secrets taken out and cut to
// 300 bytes, or "".
func orgWords(r appFactoryReply) string {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(bytes.TrimSpace(r.body), &body)
	words := strings.TrimSpace(redact.Secrets(body.Error))
	if len(words) > 300 {
		words = words[:300]
	}
	return words
}

// orgUncertain says an uncertain write in the organization's terms: the
// job-write wording names a job, and there is none here.
func orgUncertain(err error) error {
	if e, ok := err.(*appwire.Error); ok && e.Code == appwire.CodeUncertain {
		e.Message = "the organization did not answer; the change may have happened, so read it again before trying again"
	}
	return err
}
