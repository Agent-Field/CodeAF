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
	"github.com/Agent-Field/codeaf/internal/factory/api"
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
	if req.RawBody != nil {
		body = bytes.NewReader(req.RawBody)
	} else if req.Body != nil {
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
	if req.RawBody != nil {
		r.Header.Set("Content-Type", req.ContentType)
	} else if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if req.IfMatch != "" {
		r.Header.Set("If-Match", req.IfMatch)
	}
	if req.IdempotencyKey != "" {
		r.Header.Set("Idempotency-Key", req.IdempotencyKey)
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
	most := int64(appFactoryBody)
	if req.MaxAnswer > most {
		// A canvas's snapshot, op log or asset is allowed more.
		most = req.MaxAnswer
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, most+1))
	if err != nil || int64(len(data)) > most {
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
	// The connection answers as an organization, so its changes are
	// followed from here on (appfactory_org_events.go).
	h.followOrg(id, token)
	if raw, ok := out.(*appwire.OrgBytes); ok {
		// An asset is its bytes, not JSON.
		raw.Data, raw.Type = data, resp.Header.Get("Content-Type")
		return nil
	}
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
		e := appwire.Errorf(appwire.CodeConflict, "%s", or("the organization refused that as things stand"))
		// A live document's refusals say what to do next by their reason
		// (seeded, reset, snapshot_needed), so the desk is told it. A canvas
		// write into a frame another person's hand is on carries who holds it
		// and the ops the writer has not seen; they ride in detail so the desk
		// catches up without another read.
		var hand appwire.OrgCanvasConflict
		if reason := orgDocReason(r); reason != "" {
			e.Detail = &appwire.FactoryRefusal{Reason: reason}
		} else if json.Unmarshal(bytes.TrimSpace(r.body), &hand) == nil && hand.Frame != "" && hand.Holder != "" {
			hand.Reason = "conflict"
			if hand.Ops == nil {
				hand.Ops = []json.RawMessage{}
			}
			e.Detail = &hand
		}
		// Someone else drives the workstream's agent: name them, so the desk
		// says "santosh is driving" instead of a refusal.
		var held struct {
			Driver *struct {
				Handle string `json:"handle"`
			} `json:"driver"`
		}
		if json.Unmarshal(bytes.TrimSpace(r.body), &held) == nil && held.Driver != nil && held.Driver.Handle != "" {
			e.Detail = &appwire.FactoryRefusal{Reason: "driving", By: "@" + strings.TrimPrefix(held.Driver.Handle, "@")}
		}
		// The server's agent is answering someone else's message: name them,
		// so the desk says whose answer to wait for.
		var busy struct {
			Reason string `json:"reason"`
			By     string `json:"by"`
		}
		if json.Unmarshal(bytes.TrimSpace(r.body), &busy) == nil && busy.Reason == "busy" {
			refusal := &appwire.FactoryRefusal{Reason: "busy"}
			if busy.By != "" {
				refusal.By = "@" + strings.TrimPrefix(busy.By, "@")
			}
			e.Detail = refusal
		}
		return e
	case write && r.status >= 500:
		const maybe = "the change may have happened, so read it again before trying again"
		if words != "" {
			return appwire.Errorf(appwire.CodeUncertain, "%s · %s", words, maybe)
		}
		return appwire.Errorf(appwire.CodeUncertain, "the organization failed while changing it; %s", maybe)
	}
	return appFactoryFailure(r)
}

// orgDocReason is a live document's refusal reason, "" for any other.
func orgDocReason(r appFactoryReply) string {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(bytes.TrimSpace(r.body), &body)
	switch body.Reason {
	case api.DocReasonSeeded, api.DocReasonReset, api.DocReasonSnapshotNeeded:
		return body.Reason
	}
	return ""
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
