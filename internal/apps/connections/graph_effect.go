package connections

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

type graphEffect struct {
	operation string
	body      []byte
}

func (s Service) prepareGraphEffect(c registration, read Read, write Write, operation string, args map[string]any,
	store *data.Store, binding data.TypeBinding, typ *data.Type) (*Effect, error) {
	if binding.Common != graphCommon(read.Table) {
		return nil, ErrRejected
	}
	ctx := context.Background()
	grant, _, err := s.graphGrant(ctx, c)
	if err != nil {
		return nil, err
	}
	c.graphGrant = grant
	set, _ := args["set"].(map[string]any)
	id, _ := args["id"].(string)
	version, _ := args["version"].(string)
	for name := range args {
		if name != "set" && name != "id" && name != "version" {
			return nil, ErrRejected
		}
	}
	if len(set) == 0 {
		return nil, ErrRejected
	}
	var key any
	if operation == graphEventUpdateAction {
		if !graphUserVersion(version) || len(version) > 1024 || len(id) != 26 {
			return nil, ErrConflict
		}
		key, err = effectKey(store, c, read, binding, id)
		if err != nil {
			return nil, err
		}
		if sourceID, ok := key.(string); !ok || !graphIDValid(sourceID) {
			return nil, ErrConflict
		}
	} else if _, hasKey := args["id"]; hasKey || args["version"] != nil {
		return nil, ErrRejected
	}
	values := make(map[string]any, len(set))
	for name, value := range set {
		if !slices.Contains(write.Fields, name) || !boundedScalar(value) {
			return nil, ErrRejected
		}
		field, err := typ.LiveField(name, true)
		if err != nil {
			return nil, err
		}
		if !typ.AllowsLiveSourceContent(field) {
			_ = store.AuditPolicy(c.App, typ.ID, "source_write_refused")
			return nil, ErrSourcePermission
		}
		normalized, err := field.LiveValue(value, true)
		if err != nil {
			return nil, err
		}
		if _, ok := normalized.(string); !ok {
			return nil, ErrRejected
		}
		values[read.Fields[name]] = normalized
	}
	body, err := graphPayload(operation, values)
	if err != nil {
		return nil, err
	}
	state, err := store.Residency(data.Scope{App: c.App}, read.Type)
	if err != nil || state.Binding.App != "" {
		return nil, ErrRejected
	}
	pub, _, err := store.Publication(c.App)
	if err != nil {
		return nil, err
	}
	return &Effect{service: s, config: c, read: read, write: write, key: key, version: version, values: values,
		fence: effectFence(c, state.Epochs, pub), graph: &graphEffect{operation: operation, body: body}}, nil
}

func graphPayload(operation string, values map[string]any) ([]byte, error) {
	text := func(name string) string {
		v, _ := values[name].(string)
		return v
	}
	body := map[string]any{}
	switch operation {
	case graphDraftAction:
		for _, field := range []struct{ arg, property string }{{"to", "toRecipients"}, {"cc", "ccRecipients"}} {
			if text(field.arg) == "" && field.arg == "cc" {
				continue
			}
			recipients, err := graphAddresses(text(field.arg), false)
			if err != nil {
				return nil, err
			}
			body[field.property] = recipients
		}
		if strings.TrimSpace(text("body")) == "" || strings.ContainsAny(text("subject"), "\r\n") {
			return nil, ErrRejected
		}
		body["subject"] = text("subject")
		body["body"] = graphPlainBody(text("body"))
	case graphEventCreateAction, graphEventUpdateAction:
		for _, label := range []struct{ arg, property string }{{"title", "subject"}, {"description", "body"}, {"location", "location"}} {
			value, ok := values[label.arg]
			if !ok {
				continue
			}
			switch label.arg {
			case "location":
				value = map[string]any{"displayName": value}
			case "description":
				value = graphPlainBody(text(label.arg))
			}
			body[label.property] = value
		}
		if _, set := values["attendees"]; set {
			if text("attendees") == "" {
				body["attendees"] = []any{}
			} else {
				recipients, err := graphAddresses(text("attendees"), true)
				if err != nil {
					return nil, err
				}
				body["attendees"] = recipients
			}
		}
		if operation == graphEventCreateAction || values["start"] != nil || values["end"] != nil {
			start, errStart := time.Parse(time.RFC3339Nano, text("start"))
			end, errEnd := time.Parse(time.RFC3339Nano, text("end"))
			if errStart != nil || errEnd != nil || !end.After(start) {
				return nil, ErrRejected
			}
			body["start"] = graphWriteDate(start)
			body["end"] = graphWriteDate(end)
		}
		if len(body) == 0 || operation == graphEventCreateAction && strings.TrimSpace(text("title")) == "" {
			return nil, ErrRejected
		}
	default:
		return nil, ErrRejected
	}
	return json.Marshal(body)
}

func graphAddresses(text string, attendee bool) ([]map[string]any, error) {
	addresses, err := mail.ParseAddressList(text)
	if err != nil || len(addresses) > 100 || strings.ContainsAny(text, "\r\n") {
		return nil, ErrRejected
	}
	result := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		entry := map[string]any{"emailAddress": map[string]string{"address": address.Address}}
		if attendee {
			entry["type"] = "required"
		}
		result = append(result, entry)
	}
	return result, nil
}

func graphPlainBody(s string) map[string]string {
	return map[string]string{"contentType": "text", "content": s}
}

func graphWriteDate(t time.Time) map[string]string {
	return map[string]string{"dateTime": t.UTC().Format("2006-01-02T15:04:05.9999999"), "timeZone": "UTC"}
}

func (g graphSource) apply(ctx context.Context, operation, key, version string, body []byte, idempotency string) (graphEntry, error) {
	method, path := http.MethodPost, ""
	switch operation {
	case graphDraftAction:
		path = "/me/messages"
	case graphEventCreateAction:
		path = eventPath
		// Graph's event transactionId suppresses replays of the same create.
		// Outlook draft creation lacks that facility; uncertain writes always
		// stop at the broker and must be reconciled in Outlook.
		var payload map[string]any
		if decodeJSON(body, &payload) != nil {
			return graphEntry{}, ErrRejected
		}
		payload["transactionId"] = fmt.Sprintf("%x", sha256.Sum256([]byte(idempotency)))
		body, _ = json.Marshal(payload)
	case graphEventUpdateAction:
		if !graphIDValid(key) || !graphUserVersion(version) {
			return graphEntry{}, ErrConflict
		}
		method, path = http.MethodPatch, eventPath+"/"+url.PathEscape(key)
	default:
		return graphEntry{}, ErrRejected
	}
	raw, _, err := g.request(ctx, method, g.address(path, nil), "", body, version, maxBytes)
	if err != nil {
		return graphEntry{}, err
	}
	var response struct {
		graphEntry
		ChangeKey string
		IsDraft   bool
	}
	if decodeJSON(raw, &response) != nil || !graphIDValid(response.ID) ||
		operation == graphDraftAction && !response.IsDraft ||
		operation == graphEventUpdateAction && response.ID != key {
		return graphEntry{}, ErrUncertain
	}
	response.ETag = graphVersion(response.ETag, response.ChangeKey)
	return response.graphEntry, nil
}

func (e *Effect) applyGraph(ctx context.Context, idempotencyKey string) (Receipt, error) {
	c := e.config
	r := Receipt{Connection: c.ID}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	g, grant, err := e.service.graph(ctx, c)
	if err != nil {
		return r, err
	}
	if grant != c.graphGrant || e.checkRoute() != nil {
		return r, ErrConflict
	}
	key, _ := e.key.(string)
	entry, err := g.apply(ctx, e.graph.operation, key, e.version, e.graph.body, idempotencyKey)
	if errors.Is(err, ErrSourcePermission) || errors.Is(err, ErrGraphThrottle) {
		return r, errors.Join(ErrRejected, err)
	}
	if err != nil {
		return r, err
	}
	r.Version = entry.ETag
	// A reconnect during the call must not expose the previous account's
	// response. The confirmed outcome still prevents the broker from retrying.
	if err := e.service.checkGraphGrant(ctx, c); err != nil {
		r.Version = ""
		return r, nil
	}
	store, binding, _, err := e.service.bind(c.Config, e.read, false)
	if err == nil {
		encoded, err := encodeSourceKey(entry.ID)
		if err == nil {
			id, err := store.LinkIdentity(binding.Common, externalKey(c, e.read, encoded), "")
			if err == nil {
				r.Response = map[string]any{"id": id}
			}
		}
	}
	return r, nil
}
