package connections

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/connect"
)

const (
	maxRows            = 500
	defaultRows        = 100
	maxBytes           = 1 << 20
	maxCell            = 16 << 10
	MaxCredentialBytes = 16 << 10
	deadline           = 5 * time.Second
)

var (
	ErrUnavailable = errors.New("the source is unavailable")
	ErrTooLarge    = errors.New("the source result exceeds the byte budget")
	ErrConflict    = errors.New("the source record has changed or is unavailable")
	ErrUncertain   = errors.New("the source could not confirm the write result")
	ErrRejected    = errors.New("the source refused the write")
	ErrInvalidRows = errors.New("the source rows do not match the declared mapping")
)

// Service resolves only Personal profile connections. TODO(Pol1): bind
// delegated credentials and inherited source policies before enabling org access.
type Service struct {
	Profile string
	Store   func() (*data.Store, error)
}

type Summary struct {
	ID     string   `json:"id"`
	Kind   Kind     `json:"kind"`
	Reads  []string `json:"reads"`
	Writes []string `json:"writes"`
}

type Query struct {
	App        string    `json:"app"`
	Connection string    `json:"connection"`
	Read       string    `json:"read"`
	Args       Arguments `json:"args,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	id         string
	filters    map[string]any
}

// Arguments preserves JSON numbers until the source interprets them.
type Arguments map[string]any

func (a *Arguments) UnmarshalJSON(raw []byte) error {
	return decodeJSON(raw, (*map[string]any)(a))
}

type Source = data.QuerySource

type Page struct {
	Rows    []data.Row `json:"rows"`
	Source  Source     `json:"source"`
	HasMore bool       `json:"has_more,omitempty"`
}

// The profile alias is selected by callers. The immutable registration ID
// names the identity namespace even when a second profile uses the same alias.
type registration struct {
	Config
	Binding string `json:"binding"`
}

func (s Service) vault() connect.AppSourceStore {
	return connect.AppSourceStore{Profile: s.Profile}
}

// Register is the local operator door. App tools and app wire calls can use
// scoped operation names but have no configuration or credential endpoint.
func (s Service) Register(c Config, credential string) error {
	if err := c.validate(); err != nil {
		return err
	}
	credential = strings.TrimSpace(credential)
	if credential == "" || len(credential) > MaxCredentialBytes || strings.ContainsAny(credential, "\r\n") {
		return errors.New("the connection needs one credential line")
	}
	if c.Kind != HTTP {
		db, err := database(c.Kind, credential)
		if err != nil {
			return err
		}
		_ = db.Close()
	}
	binding, err := cell.NewID(time.Now())
	if err != nil {
		return err
	}
	registered := registration{Config: c, Binding: binding}
	definition, err := json.Marshal(registered)
	if err != nil {
		return err
	}
	register := func() error { return s.vault().Register(c.ID, definition, credential) }
	bindings := map[string]data.ResidencyBinding{}
	var store *data.Store
	for name, read := range c.Reads {
		if read.Residency == nil {
			continue
		}
		if _, exists := bindings[read.Type]; exists {
			return errors.New("only one residency read is allowed per type")
		}
		current, _, typ, err := s.bind(c, read, true)
		if err != nil {
			return err
		}
		for field := range read.Fields {
			if _, err := sourceField(read, typ, field); err != nil {
				return err
			}
		}
		store = current
		bindings[read.Type] = residencyBinding(registered, read, name)
	}
	if store == nil {
		return register()
	}
	return store.DeclareResidencies(bindings, register)
}

func (s Service) List(app string) ([]Summary, error) {
	definitions, err := s.vault().Definitions()
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, id := range slices.Sorted(maps.Keys(definitions)) {
		var c registration
		if json.Unmarshal(definitions[id], &c) != nil || c.App != app || app == "" || c.ID != id || !cell.ValidID(c.Binding) {
			continue
		}
		out = append(out, Summary{ID: id, Kind: c.Kind, Reads: slices.Sorted(maps.Keys(c.Reads)), Writes: slices.Sorted(maps.Keys(c.Writes))})
	}
	return out, nil
}

func (s Service) scoped(app, id string) (registration, error) {
	var c registration
	definitions, err := s.vault().Definitions()
	if err != nil {
		return c, err
	}
	if app == "" || json.Unmarshal(definitions[id], &c) != nil || c.ID != id || c.App != app || !cell.ValidID(c.Binding) {
		return registration{}, errors.New("there is no such connection for this app")
	}
	return c, c.validate()
}

func (s Service) Test(ctx context.Context, app, id string) error {
	c, err := s.scoped(app, id)
	if err != nil {
		return err
	}
	secret, err := s.vault().Credential(c.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if c.Kind == HTTP {
		_, err := request(ctx, c.Config, secret, "GET", c.TestPath, nil, nil, "")
		return err
	}
	db, err := database(c.Kind, secret)
	if err != nil {
		return err
	}
	defer db.Close()
	if db.PingContext(ctx) != nil {
		return ErrUnavailable
	}
	return nil
}

func (s Service) Read(ctx context.Context, q Query) (Page, error) {
	c, err := s.scoped(q.App, q.Connection)
	if err != nil {
		return Page{}, err
	}
	read, ok := c.Reads[q.Read]
	if !ok {
		return Page{}, errors.New("this connection has no such declared read")
	}
	if q.Limit < 0 || q.Limit > maxRows {
		return Page{}, fmt.Errorf("a live read limit must be from 1 to %d", maxRows)
	}
	if q.Limit == 0 {
		q.Limit = defaultRows
	}
	store, binding, typ, err := s.bind(c.Config, read, false)
	if err != nil {
		return Page{}, err
	}
	var route residencyRoute
	state, err := store.Residency(data.Scope{App: q.App}, read.Type)
	if err != nil {
		return Page{}, err
	}
	if read.Residency == nil && state.Binding.App != "" {
		return Page{}, ErrUnavailable
	}
	if read.Residency != nil {
		route, err = s.route(q.App, read.Type)
		if err != nil {
			return Page{}, err
		}
		if route.config.ID != q.Connection || route.name != q.Read {
			return Page{}, ErrUnavailable
		}
		if route.state.Mode != data.ResidencyLive {
			return s.readResident(route, q)
		}
	}
	fields := map[string]*data.Field{}
	for name := range read.Fields {
		f, err := sourceField(read, typ, name)
		if err != nil {
			return Page{}, err
		}
		if f.Masked && read.OrderBy != "" && read.OrderBy == read.Fields[name] {
			return Page{}, errors.New("a read cannot sort on a masked field")
		}
		fields[name] = f
	}
	args, err := matchArgs(q.Args, read, fields)
	if err != nil {
		return Page{}, err
	}
	maps.Copy(args, q.filters)
	if q.id != "" {
		key, err := effectKey(store, c, read, binding, q.id)
		if err != nil {
			return Page{}, err
		}
		args[read.Key] = key
	}
	secret, err := s.vault().Credential(c.ID)
	if err != nil {
		return Page{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	observed := time.Now().UTC()
	var rows []map[string]any
	switch c.Kind {
	case HTTP:
		rows, err = httpRows(ctx, c.Config, secret, read, args)
	default:
		rows, err = sqlRows(ctx, c.Kind, secret, read, args, q.Limit+1)
	}
	if err != nil {
		return Page{}, err
	}
	// Recheck the binding before projecting any result from a remote call.
	current, currentType, err := store.LiveBinding(data.Scope{App: q.App}, read.Type, false)
	if err != nil || current != binding || currentType != typ {
		return Page{}, errors.New("the type binding changed during the read")
	}
	page := Page{
		Rows:    make([]data.Row, 0, min(len(rows), q.Limit)),
		Source:  Source{Connection: c.ID, Read: q.Read, Residency: "live", Freshness: "live", ObservedAt: observed},
		HasMore: len(rows) > q.Limit,
	}
	seen := map[string]bool{}
	for _, raw := range rows[:min(len(rows), q.Limit)] {
		if err := ctx.Err(); err != nil {
			return Page{}, ErrUnavailable
		}
		key, err := encodeSourceKey(raw[read.Key])
		if err != nil || seen[key] || c.Kind == HTTP && containsCredential(raw[read.Key], secret) {
			return Page{}, ErrInvalidRows
		}
		seen[key] = true
		row := data.Row{"type": binding.Alias}
		if read.Version != "" {
			version, err := sourceScalar(raw[read.Version])
			if err != nil || strings.Contains(version, secret) && c.Kind == HTTP {
				return Page{}, ErrInvalidRows
			}
			row["_source_version"] = version
		}
		var meta data.RowMeta
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			field := fields[name]
			value, exists := raw[read.Fields[name]]
			if !exists || !boundedScalar(value) || c.Kind == HTTP && containsCredential(value, secret) {
				return Page{}, ErrInvalidRows
			}
			mapped, err := mappedSourceValue(store, c.App, read, field, value)
			if err != nil {
				return Page{}, err
			}
			if field.Masked && mapped != nil {
				meta.Masked = append(meta.Masked, name)
			}
			row[name] = mapped
		}
		if len(meta.Masked) > 0 {
			row["_meta"] = meta
		}
		// TODO(O1): resolve reviewed cross-source matches. A stable source key
		// alone reserves a distinct identity and never guesses from a name.
		id, found, err := store.LookupBoundIdentity(binding, externalKey(c, read, key))
		if err == nil && !found {
			id, err = store.LinkIdentity(binding.Common, externalKey(c, read, key), "")
		}
		if err != nil {
			return Page{}, errors.New("the source identity could not be linked")
		}
		row["id"] = id
		page.Rows = append(page.Rows, row)
	}
	// TODO(R1): use the same binding when the type gains a mirror or native
	// route. A live read persists only the crosswalk, never source values.
	if read.Residency != nil {
		if err := route.checkEpoch(); err != nil {
			return Page{}, err
		}
		page.Source = route.source()
		page.Source.ObservedAt = observed
	}
	return page, nil
}

func (s Service) bind(c Config, read Read, write bool) (*data.Store, data.TypeBinding, *data.Type, error) {
	if s.Store == nil {
		return nil, data.TypeBinding{}, nil, errors.New("the app data store is unavailable")
	}
	store, err := s.Store()
	if err != nil {
		return nil, data.TypeBinding{}, nil, err
	}
	binding, typ, err := store.LiveBinding(data.Scope{App: c.App}, read.Type, write)
	return store, binding, typ, err
}

func sourceScalar(value any) (string, error) {
	var key string
	switch v := value.(type) {
	case string:
		key = v
	case json.Number:
		if _, err := v.Int64(); err != nil {
			return "", ErrInvalidRows
		}
		key = string(v)
	}
	if key == "" || len(key) > 1024 || !utf8.ValidString(key) {
		return "", ErrInvalidRows
	}
	return key, nil
}

func encodeSourceKey(value any) (string, error) {
	if _, err := sourceScalar(value); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 3000 {
		return "", ErrInvalidRows
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func externalKey(c registration, read Read, key string) data.ExternalKey {
	object := read.Table
	if c.Kind == HTTP {
		object = read.Path + "#" + read.Rows
	}
	return data.ExternalKey{
		Binding: c.Binding, Object: fmt.Sprintf("%x", sha256.Sum256([]byte(object+"#"+read.Key))),
		Generation: 1, Key: key,
	}
}

func matchArgs(input map[string]any, read Read, fields map[string]*data.Field) (map[string]any, error) {
	out := make(map[string]any, len(input))
	for name, value := range input {
		target, ok := read.Match[name]
		if !ok {
			return nil, errors.New("this read does not declare that argument")
		}
		if field := fields[name]; field != nil && (field.Masked || field.Type == data.TypeRef) {
			return nil, errors.New("a read cannot match on a masked or crosswalk field")
		}
		if !boundedScalar(value) || value == nil {
			return nil, errors.New("a read argument needs a bounded scalar")
		}
		out[target] = value
	}
	return out, nil
}

func boundedScalar(value any) bool {
	switch v := value.(type) {
	case nil, bool, int, int64, float64:
		return true
	case string:
		return len(v) <= maxCell
	case json.Number:
		return len(v) <= 64
	default:
		return false
	}
}
