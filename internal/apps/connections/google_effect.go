package connections

import (
	"context"
	"errors"
	"slices"

	"encoding/base64"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

func googleOperation(object, operation string) bool {
	switch object {
	case "gmail":
		return operation == "create-draft"
	case "calendar":
		return operation == "create-event" || operation == "update-event"
	case "drive", "transcripts":
		return operation == "comment"
	}
	return false
}

func googleText(values map[string]any, field string) string {
	text, _ := values[field].(string)
	return text
}

func googleFields(object string, writing bool) []string {
	switch object {
	case "gmail":
		if writing {
			return []string{"to", "cc", "subject", "body"}
		}
		return []string{"thread", "date", "subject", "snippet", "from", "to", "cc", "labels", "attachments", "sender_domain"}
	case "calendar":
		if writing {
			return []string{"title", "start", "end", "description", "location"}
		}
		return []string{"title", "start", "end", "start_date", "end_date", "attendees", "conference", "description", "location"}
	default:
		if writing {
			return []string{"comment"}
		}
		return []string{"title", "mime_type", "modified", "link", "description", "shared", "sharing", "transcript"}
	}
}

func googlePayload(operation string, values map[string]any) ([]byte, error) {
	var body any
	switch operation {
	case "create-draft":
		to := googleText(values, "to")
		cc := googleText(values, "cc")
		if _, err := mail.ParseAddressList(to); err != nil || strings.ContainsAny(to+cc, "\r\n") {
			return nil, ErrRejected
		}
		if cc != "" {
			if _, err := mail.ParseAddressList(cc); err != nil {
				return nil, ErrRejected
			}
		}
		subject := googleText(values, "subject")
		text := googleText(values, "body")
		if strings.ContainsAny(subject, "\r\n") || strings.TrimSpace(text) == "" {
			return nil, ErrRejected
		}
		var raw strings.Builder
		raw.WriteString("To: " + to + "\r\n")
		if cc != "" {
			raw.WriteString("Cc: " + cc + "\r\n")
		}
		raw.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
		raw.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n")
		raw.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		encoded := base64.StdEncoding.EncodeToString([]byte(text))
		for len(encoded) > 76 {
			raw.WriteString(encoded[:76] + "\r\n")
			encoded = encoded[76:]
		}
		raw.WriteString(encoded + "\r\n")
		body = map[string]any{"message": map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte(raw.String()))}}
	case "create-event", "update-event":
		event := map[string]any{}
		for _, field := range []string{"title", "description", "location"} {
			if value, ok := values[field].(string); ok {
				key := field
				if field == "title" {
					key = "summary"
				}
				event[key] = value
			}
		}
		// Moving a meeting needs both endpoints, so a truncated patch cannot
		// accidentally shorten it or create a reversed time span.
		start, end := googleText(values, "start"), googleText(values, "end")
		if operation == "create-event" || start != "" || end != "" {
			from, err1 := time.Parse(time.RFC3339, start)
			to, err2 := time.Parse(time.RFC3339, end)
			if err1 != nil || err2 != nil || !to.After(from) {
				return nil, ErrRejected
			}
			event["start"] = map[string]string{"dateTime": start}
			event["end"] = map[string]string{"dateTime": end}
		}
		if len(event) == 0 || operation == "create-event" && strings.TrimSpace(googleText(values, "title")) == "" {
			return nil, ErrRejected
		}
		body = event
	case "comment":
		comment := googleText(values, "comment")
		if strings.TrimSpace(comment) == "" {
			return nil, ErrRejected
		}
		body = map[string]string{"content": comment}
	default:
		return nil, ErrRejected
	}
	return json.Marshal(body)
}

func (g googleSource) apply(ctx context.Context, connection, operation, id, version string, body []byte) (Receipt, error) {
	receipt := Receipt{Connection: connection}
	method, path := http.MethodPost, ""
	query := url.Values{}
	switch operation {
	case "create-draft":
		path = gmailPath + "/drafts"
	case "create-event":
		path = calendarPath
		query.Set("sendUpdates", "none")
	case "update-event":
		method, path = http.MethodPatch, calendarPath+"/"+id
		query.Set("sendUpdates", "none")
	case "comment":
		path = drivePath + "/" + id + "/comments"
		query.Set("fields", "id")
	default:
		return receipt, ErrRejected
	}
	raw, _, err := g.request(ctx, method, path, query, body, version)
	if err != nil {
		return receipt, err
	}
	var result struct{ ID, Etag string }
	if decodeJSON(raw, &result) != nil || !googleID.MatchString(result.ID) {
		return receipt, ErrUncertain
	}
	receipt.Version = result.Etag
	receipt.Response = map[string]any{"id": result.ID}
	return receipt, nil
}

type googleEffect struct {
	operation, writeGrant, authority string
	body                             []byte
}

func (s Service) prepareGoogleEffect(c registration, read Read, write Write, operation string, args map[string]any,
	store *data.Store, binding data.TypeBinding, typ *data.Type) (*Effect, error) {
	ctx := context.Background()
	_, grant, err := s.workspace(ctx, c, read.Table, false)
	if err != nil {
		return nil, err
	}
	c.googleGrant = grant
	_, writeGrant, err := s.workspace(ctx, c, read.Table, true)
	if err != nil {
		return nil, err
	}
	set, _ := args["set"].(map[string]any)
	id, _ := args["id"].(string)
	version, _ := args["version"].(string)
	for name, value := range args {
		if name != "set" && (name != "id" && name != "version" || value == nil) {
			return nil, ErrRejected
		}
	}
	create := operation == "create-draft" || operation == "create-event"
	if create && (args["id"] != nil || args["version"] != nil) ||
		!create && len(id) != 26 || operation == "update-event" && (read.Version == "" || version == "" || len(version) > maxCell ||
		strings.ContainsAny(version, "\r\n")) || operation == "comment" && args["version"] != nil {
		return nil, ErrRejected
	}
	var key string
	if !create {
		value, err := effectKey(store, c, read, binding, id)
		if err != nil {
			return nil, err
		}
		key, _ = value.(string)
		if !googleID.MatchString(key) || key == "." || key == ".." {
			return nil, ErrRejected
		}
	}
	values := make(map[string]any, len(set))
	for name, value := range set {
		if !slices.Contains(write.Fields, name) || !boundedScalar(value) || value == nil {
			return nil, ErrRejected
		}
		field, err := typ.LiveField(name, true)
		if err != nil || typ.FieldPolicy(field) != "" {
			_ = store.AuditPolicy(c.App, typ.ID, "source_write_refused")
			return nil, ErrSourcePermission
		}
		normalized, err := field.LiveValue(value, true)
		if err != nil {
			return nil, err
		}
		values[read.Fields[name]] = normalized
	}
	body, err := googlePayload(operation, values)
	if err != nil || len(body) > maxBytes {
		return nil, ErrRejected
	}
	state, err := store.Residency(data.Scope{App: c.App}, read.Type)
	if err != nil || state.Binding.App != "" {
		return nil, ErrConflict
	}
	publication, _, err := store.Publication(c.App)
	if err != nil {
		return nil, err
	}
	authority := effectFence(c, state.Epochs, publication)
	return &Effect{service: s, config: c, read: read, write: write, key: key, version: version, values: values,
		fence:  authority + "/" + grant + "/" + writeGrant,
		google: &googleEffect{operation: operation, writeGrant: writeGrant, authority: authority, body: body}}, nil
}

func (e *Effect) applyGoogle(ctx context.Context) (Receipt, error) {
	receipt := Receipt{Connection: e.config.ID}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	reader, grant, err := e.service.workspace(ctx, e.config, e.read.Table, false)
	if err != nil || grant != e.config.googleGrant {
		return receipt, ErrSourcePermission
	}
	id, _ := e.key.(string)
	if id != "" {
		rows, _, err := reader.point(ctx, e.read.Table, id)
		if err != nil {
			return receipt, err
		}
		if len(rows) != 1 || e.version != "" && rows[0]["version"] != e.version {
			return receipt, ErrConflict
		}
	}
	writer, grant, err := e.service.workspace(ctx, e.config, e.read.Table, true)
	if err != nil || grant != e.google.writeGrant {
		return receipt, ErrSourcePermission
	}
	if _, grant, err := e.service.workspace(ctx, e.config, e.read.Table, false); err != nil || grant != e.config.googleGrant {
		return receipt, ErrSourcePermission
	}
	// A changed binding, publication or source epoch invalidates the approved effect.
	current, err := e.service.scoped(e.config.App, e.config.ID)
	if err != nil || current.Binding != e.config.Binding || current.GoogleFixture != e.config.GoogleFixture {
		return receipt, ErrConflict
	}
	store, _, _, err := e.service.bind(current.Config, e.read, true)
	if err != nil {
		return receipt, err
	}
	state, err := store.Residency(data.Scope{App: current.App}, e.read.Type)
	if err != nil {
		return receipt, err
	}
	pub, _, err := store.Publication(current.App)
	if err != nil || effectFence(current, state.Epochs, pub) != e.google.authority {
		return receipt, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return receipt, errors.Join(ErrUnavailable, err)
	}
	return writer.apply(ctx, e.config.ID, e.google.operation, id, e.version, e.google.body)
}
