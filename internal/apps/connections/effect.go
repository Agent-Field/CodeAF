package connections

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

// Effect is a validated source mutation handed only to the runs adapter. Its
// transport, key and credential are unavailable to screens and wire callers.
type Effect struct {
	service Service
	config  registration
	read    Read
	write   Write
	key     any
	version string
	values  map[string]any
	fence   string
	google  *googleEffect
}

type Receipt struct {
	Connection string         `json:"connection"`
	Version    string         `json:"version,omitempty"`
	Response   map[string]any `json:"response,omitempty"`
}

// PrepareEffect resolves the write declaration, current grants and canonical
// row identity before the broker asks for approval. Callers pass no source key.
func (s Service) PrepareEffect(app, connection, operation string, args map[string]any) (*Effect, error) {
	c, err := s.scoped(app, connection)
	if err != nil {
		return nil, err
	}
	write, ok := c.Writes[operation]
	if !ok {
		return nil, errors.New("the connection has no such declared write")
	}
	read := c.Reads[write.Read]
	store, binding, typ, err := s.bind(c.Config, read, true)
	if err != nil {
		return nil, err
	}
	if typ.HasRowPolicy() {
		_ = store.AuditPolicy(app, typ.ID, "source_write_refused")
		return nil, errors.New("this source write cannot enforce the record row policy")
	}
	if c.Kind == Google {
		return s.prepareGoogleEffect(c, read, write, operation, args, store, binding, typ)
	}
	id, _ := args["id"].(string)
	set, _ := args["set"].(map[string]any)
	version, _ := args["version"].(string)
	for name := range args {
		if name != "id" && name != "version" && name != "set" {
			return nil, ErrRejected
		}
	}
	if id == "" || len(id) != 26 || len(set) == 0 ||
		(read.Version != "" && (version == "" || len(version) > 1024)) || (read.Version == "" && version != "") {
		return nil, errors.New("a source write needs a canonical id, any declared version, and fields to set")
	}
	key, err := effectKey(store, c, read, binding, id)
	if err != nil {
		return nil, err
	}
	values := make(map[string]any, len(set))
	for name, value := range set {
		if !slices.Contains(write.Fields, name) || !boundedScalar(value) {
			return nil, errors.New("the declared write cannot set that field")
		}
		field, err := typ.LiveField(name, true)
		if err != nil {
			return nil, err
		}
		if typ.FieldPolicy(field) != "" {
			_ = store.AuditPolicy(app, typ.ID, "source_write_refused")
			return nil, errors.New("this source write cannot set a protected field")
		}
		normalized, err := field.LiveValue(value, true)
		if err != nil {
			return nil, err
		}
		values[read.Fields[name]] = normalized
	}
	state, err := store.Residency(data.Scope{App: app}, read.Type)
	if err != nil {
		return nil, err
	}
	if read.Residency == nil && state.Binding.App != "" {
		return nil, ErrRejected
	}
	var route residencyRoute
	if read.Residency != nil {
		route, err = s.route(app, read.Type)
		if err != nil {
			return nil, err
		}
		if route.state.Mode == data.ResidencyNative {
			return nil, ErrRejected
		}
	}
	publication, _, err := store.Publication(app)
	if err != nil {
		return nil, err
	}
	fence := effectFence(c, state.Epochs, publication)
	return &Effect{fence: fence, service: s, config: c, read: read, write: write, key: key, version: version, values: values}, nil
}

func effectKey(store *data.Store, c registration, read Read, binding data.TypeBinding, id string) (any, error) {
	namespace := externalKey(c, read, "")
	keys, err := store.ExternalKeys(id)
	if err != nil {
		return nil, err
	}
	var key any
	for _, candidate := range keys {
		if candidate.Binding != namespace.Binding || candidate.Object != namespace.Object || candidate.Generation != namespace.Generation {
			continue
		}
		linked, found, err := store.LookupBoundIdentity(binding, candidate)
		if err != nil || !found || linked != id {
			return nil, ErrConflict
		}
		raw, err := base64.RawURLEncoding.DecodeString(candidate.Key)
		if err != nil || len(raw) == 0 || key != nil || decodeJSON(raw, &key) != nil {
			return nil, ErrConflict
		}
	}
	if key == nil {
		return nil, ErrConflict
	}
	return key, nil
}

// Apply dispatches a prepared write with the broker's stable step key.
func (e *Effect) Apply(ctx context.Context, idempotencyKey string) (Receipt, error) {
	if idempotencyKey == "" {
		return Receipt{}, ErrRejected
	}
	c := e.config
	store, _, typ, err := e.service.bind(c.Config, e.read, true)
	if err != nil {
		return Receipt{}, err
	}
	if typ.HasRowPolicy() {
		_ = store.AuditPolicy(c.App, typ.ID, "source_write_refused")
		return Receipt{}, errors.New("this source write cannot enforce the record row policy")
	}
	for name, column := range e.read.Fields {
		if _, writing := e.values[column]; writing {
			field, err := typ.LiveField(name, true)
			if err != nil || typ.FieldPolicy(field) != "" {
				_ = store.AuditPolicy(c.App, typ.ID, "source_write_refused")
				return Receipt{}, errors.New("this source write cannot set a protected field")
			}
		}
	}
	if e.google != nil {
		return e.applyGoogle(ctx)
	}
	secret, err := e.service.credential(c, e.service.person(ctx))
	if err != nil {
		return Receipt{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	receipt := Receipt{Connection: c.ID}
	if c.Kind != HTTP {
		// The version is advanced in the transaction. A repeated effect
		// carrying the same expected version cannot apply the update twice.
		sqlKey, ok := e.key.(string)
		if !ok {
			return receipt, ErrConflict
		}
		receipt.Version, err = sqlWrite(ctx, c.Kind, secret, e.read, sqlKey, e.version, e.values, e.checkRoute)
		return receipt, err
	}
	if err := e.checkRoute(); err != nil {
		return receipt, err
	}
	body := make(map[string]any, len(e.values)+2)
	for name, value := range e.values {
		body[name] = value
	}
	body[e.read.Key] = e.key
	if e.read.Version != "" {
		body[e.read.Version] = e.version
	}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > maxBytes {
		return Receipt{}, ErrRejected
	}
	raw, err := request(ctx, c.Config, secret, "POST", e.write.Path, nil, encoded, idempotencyKey)
	if errors.Is(err, ErrTooLarge) {
		return receipt, ErrUncertain
	}
	if err != nil {
		return receipt, err
	}
	if len(e.write.Response) != 0 {
		var response map[string]any
		if decodeJSON(raw, &response) != nil {
			return receipt, ErrUncertain
		}
		receipt.Response = make(map[string]any, len(e.write.Response))
		for name, target := range e.write.Response {
			value, ok := response[target]
			if !ok || !boundedScalar(value) || containsCredential(value, secret) {
				return receipt, ErrUncertain
			}
			receipt.Response[name] = value
		}
	}
	return receipt, nil
}

// Fence binds a broker approval to the registration, app version and epochs.
func (e *Effect) Fence() string { return e.fence }

func effectFence(c registration, epochs data.TypeEpochs, pub data.AppPublication) string {
	return fmt.Sprintf("%s/%d/%d/%d/%x", c.Binding, epochs.Mapping, epochs.Residency, pub.Version, sha256.Sum256(pub.Metadata))
}

func (e *Effect) checkRoute() error {
	store, _, _, err := e.service.bind(e.config.Config, e.read, true)
	if err != nil {
		return err
	}
	state, err := store.Residency(data.Scope{App: e.config.App}, e.read.Type)
	if err != nil {
		return err
	}
	publication, _, err := store.Publication(e.config.App)
	if err != nil {
		return err
	}
	if e.Fence() != effectFence(e.config, state.Epochs, publication) {
		return ErrConflict
	}
	return nil
}
