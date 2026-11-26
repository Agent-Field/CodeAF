package connections

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

const maxSyncRows = 10_000

type CutoverRequest struct {
	App            string             `json:"app"`
	Type           string             `json:"type"`
	To             data.ResidencyMode `json:"to"`
	SourceQuiesced bool               `json:"source_quiesced,omitempty"`
}

type ResidencyStatus struct {
	Type          string             `json:"type"`
	Connection    string             `json:"connection,omitempty"`
	Read          string             `json:"read,omitempty"`
	TypeResidency data.TypeResidency `json:"state"`
	Binding       string             `json:"-"`
}

type residencyRoute struct {
	config  registration
	name    string
	read    Read
	binding data.ResidencyBinding
	state   data.TypeResidency
	typ     *data.Type
	store   *data.Store
}

// route resolves the type grant before looking up a profile-owned binding.
func (s Service) route(app, alias string) (residencyRoute, error) {
	var route residencyRoute
	if app == "" || s.Store == nil {
		return route, ErrRejected
	}
	store, err := s.Store()
	if err != nil {
		return route, err
	}
	scope := data.Scope{App: app}
	typ, err := store.Type(scope, alias)
	if err != nil {
		return route, err
	}
	state, err := store.Residency(scope, alias)
	if err != nil {
		return route, err
	}
	route.store, route.typ, route.state = store, typ, state
	if typ.Common == "" {
		return route, nil
	}
	if state.Binding.App != "" && state.Binding.App != app {
		return route, errors.New("the source binding is outside this app's scope")
	}
	definitions, err := s.vault().Definitions()
	if err != nil {
		return route, err
	}
	for _, id := range slices.Sorted(maps.Keys(definitions)) {
		if state.Binding.App != "" && state.Binding.Connection != id {
			continue
		}
		var cfg registration
		if decodeJSON(definitions[id], &cfg) != nil || cfg.App != app || cfg.ID != id {
			continue
		}
		if err := cfg.validate(); err != nil {
			return route, err
		}
		for name, read := range cfg.Reads {
			if (read.Residency == nil && cfg.Kind != Google) || state.Binding.App != "" && state.Binding.Read != name {
				continue
			}
			bound, err := store.Type(scope, read.Type)
			if err != nil || bound.ID != typ.ID {
				continue
			}
			if route.name != "" {
				return route, errors.New("this type has more than one declared residency binding")
			}
			route.config, route.read, route.name = cfg, read, name
			route.binding = residencyBinding(cfg, read, name)
		}
	}
	if state.Binding.App != "" && (route.read.Residency == nil ||
		state.Binding.Key != route.binding.Key || !slices.Equal(state.Binding.Writable, route.binding.Writable)) {
		return route, ErrUnavailable
	}
	if route.name != "" && state.Binding.App == "" {
		route.state.Mode = data.ResidencyLive
	}
	if route.name != "" {
		if typ.HasRowPolicy() && route.state.Mode != data.ResidencyNative {
			return route, ErrSourcePermission
		}
		if _, _, err := store.LiveBinding(scope, route.read.Type, false); err != nil {
			return route, err
		}
	}
	return route, nil
}

func residencyBinding(cfg registration, read Read, name string) data.ResidencyBinding {
	fields := map[string]bool{}
	for _, write := range cfg.Writes {
		if write.Read == name {
			for _, field := range write.Fields {
				fields[field] = true
			}
		}
	}
	return data.ResidencyBinding{App: cfg.App, Connection: cfg.ID, Read: name, Key: externalKey(cfg, read, ""),
		Writable: slices.Sorted(maps.Keys(fields))}
}

func (s Service) Residency(app, alias string) (ResidencyStatus, error) {
	r, err := s.route(app, alias)
	return ResidencyStatus{Type: alias, Connection: r.config.ID, Read: r.name, TypeResidency: r.state, Binding: r.binding.Key.Binding}, err
}

func (s Service) PreviewCutover(ctx context.Context, req CutoverRequest) (data.ResidencyPlan, error) {
	return s.cutover(ctx, req, "")
}

func (s Service) ApplyCutover(ctx context.Context, req CutoverRequest, confirmation string) (data.ResidencyPlan, error) {
	if confirmation == "" {
		return data.ResidencyPlan{}, errors.New("apply needs the confirmation from a cut-over preview")
	}
	return s.cutover(ctx, req, confirmation)
}

func (s Service) cutover(ctx context.Context, req CutoverRequest, confirmation string) (data.ResidencyPlan, error) {
	var empty data.ResidencyPlan
	r, err := s.route(req.App, req.Type)
	if err != nil {
		return empty, err
	}
	if r.read.Residency == nil {
		return empty, errors.New("this type has no declared residency binding")
	}
	if _, _, _, err := s.bind(r.config.Config, r.read, true); err != nil {
		return empty, err
	}
	for _, field := range r.binding.Writable {
		if _, err := r.typ.LiveField(field, true); err != nil {
			return empty, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	tx, closeSource, err := s.openSnapshot(ctx, r, confirmation != "")
	if err != nil {
		return empty, err
	}
	defer closeSource()
	snapshot, err := readResidencySnapshot(ctx, tx, r, "")
	if err != nil {
		return empty, err
	}
	return r.store.Cutover(ctx, r.binding, req.Type, req.To, req.SourceQuiesced, snapshot, confirmation, func(records []data.ResidencyReplay) (data.ResidencySnapshot, error) {
		if len(records) == 0 {
			return snapshot, nil
		}
		for _, record := range records {
			if err := replaySQLRow(ctx, tx, r.read, record); err != nil {
				return snapshot, err
			}
		}
		refreshed, err := readResidencySnapshot(ctx, tx, r, "")
		if err != nil {
			return snapshot, err
		}
		if err := tx.Commit(); err != nil {
			return snapshot, ErrUncertain
		}
		return refreshed, nil
	})
}

func replaySQLRow(ctx context.Context, tx *sql.Tx, read Read, record data.ResidencyReplay) error {
	raw, err := base64.RawURLEncoding.DecodeString(record.Key.Key)
	var key string
	if err != nil || decodeJSON(raw, &key) != nil {
		return ErrInvalidRows
	}
	version, err := strconv.ParseInt(record.Revision, 10, 64)
	if err != nil || version < 0 || version == (1<<63)-1 {
		return ErrConflict
	}
	values := make(map[string]any, len(record.Set))
	for field, value := range record.Set {
		values[read.Fields[field]] = value
	}
	return updateSQLRow(ctx, tx, Postgres, read, key, version, values)
}

func (s Service) openSnapshot(ctx context.Context, r residencyRoute, fence bool) (*sql.Tx, func(), error) {
	credential, err := s.credential(r.config, s.person(ctx))
	if err != nil {
		return nil, nil, err
	}
	db, err := database(Postgres, credential)
	if err != nil {
		return nil, nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, nil, ErrUnavailable
	}
	txCtx, cancel := ctx, func() {}
	if fence {
		// Keep the table fence beyond the query deadline while SQLite
		// checks cancellation and commits or rolls back its transaction.
		txCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 2*deadline)
	}
	tx, err := conn.BeginTx(txCtx, &sql.TxOptions{ReadOnly: !fence, Isolation: sql.LevelRepeatableRead})
	closeSource := func() {
		if tx != nil {
			_ = tx.Rollback()
		}
		cancel()
		_ = conn.Close()
		_ = db.Close()
	}
	if err != nil {
		closeSource()
		return nil, nil, ErrUnavailable
	}
	if fence {
		if _, err := tx.ExecContext(ctx, "LOCK TABLE "+Postgres.table(r.read.Table)+" IN SHARE ROW EXCLUSIVE MODE"); err != nil {
			closeSource()
			return nil, nil, ErrUnavailable
		}
	}
	return tx, closeSource, nil
}

func readResidencySnapshot(ctx context.Context, tx *sql.Tx, r residencyRoute, checkpoint string) (data.ResidencySnapshot, error) {
	snapshot := data.ResidencySnapshot{ObservedAt: time.Now().UTC()}
	var sourceTime time.Time
	if err := tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&sourceTime); err != nil {
		return snapshot, ErrUnavailable
	}
	snapshot.Checkpoint = sourceTime.UTC().Format(time.RFC3339Nano)
	upper := snapshot.Checkpoint
	if checkpoint == "" {
		upper = ""
	}
	raw, err := scanSQLRows(ctx, tx, Postgres, r.read, nil, maxSyncRows+1, checkpoint, true, upper, "", "")
	if err != nil {
		return snapshot, err
	}
	if len(raw) > maxSyncRows {
		return snapshot, fmt.Errorf("a residency snapshot or poll may cover at most %d rows", maxSyncRows)
	}
	seen := map[string]bool{}
	for _, item := range raw {
		key, err := encodeSourceKey(item[r.read.Key])
		if err != nil || seen[key] {
			return snapshot, ErrInvalidRows
		}
		seen[key] = true
		revision, _ := sourceScalar(item[r.read.Version])
		if n, err := strconv.ParseInt(revision, 10, 64); err != nil || n < 0 {
			return snapshot, ErrInvalidRows
		}
		deleted, ok := item[r.read.Residency.Tombstone].(string)
		if !ok || (deleted != "true" && deleted != "false") {
			return snapshot, ErrInvalidRows
		}
		row := data.ResidencyRow{Key: externalKey(r.config, r.read, key), Revision: revision, Deleted: deleted == "true",
			Claims: make(map[string]data.SourceFieldClaim, len(r.read.Fields))}
		for name, column := range r.read.Fields {
			field, err := sourceField(r.read, r.typ, name)
			if err != nil {
				return snapshot, err
			}
			// Keep the actual value in the claim. The normal record projection
			// masks it under the same field grants used by native queries.
			plain := *field
			plain.Masked = false
			value, err := mappedSourceValue(r.store, data.Scope{App: r.config.App}, r.read, &plain, item[column])
			if err != nil {
				return snapshot, err
			}
			row.Claims[name] = data.SourceFieldClaim{Path: column, Value: value}
		}
		snapshot.Rows = append(snapshot.Rows, row)
	}
	return snapshot, nil
}

// SyncResidency polls an active mirror independently of the outbox cursor.
func (s Service) SyncResidency(ctx context.Context, app, alias string) error {
	r, err := s.route(app, alias)
	if err != nil || r.state.Mode != data.ResidencyMirror {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	tx, closeSource, err := s.openSnapshot(ctx, r, false)
	if err != nil {
		return err
	}
	defer closeSource()
	snapshot, err := readResidencySnapshot(ctx, tx, r, r.state.Checkpoint)
	if err != nil {
		return err
	}
	return r.store.SyncResidency(data.Scope{App: app}, alias, r.state, snapshot)
}

func (r residencyRoute) source() Source {
	freshness := string(r.state.Mode)
	freshFor := 0
	if r.read.Residency != nil {
		freshFor = r.read.Residency.FreshForSeconds
	}
	if r.state.Mode == data.ResidencyMirror &&
		time.Since(r.state.CheckedAt) > time.Duration(freshFor)*time.Second {
		freshness = "stale"
	}
	return Source{Connection: r.config.ID, Read: r.name, Residency: string(r.state.Mode), Freshness: freshness,
		ObservedAt: r.state.CheckedAt, MappingEpoch: r.state.Epochs.Mapping, ResidencyEpoch: r.state.Epochs.Residency,
		FreshForSeconds: freshFor}
}

func (s Service) readResident(r residencyRoute, sc data.Scope, q Query) (Page, error) {
	ir := &data.QueryIR{Type: r.read.Type}
	for name, value := range q.Args {
		if _, allowed := r.read.Match[name]; !allowed || value == nil || !boundedScalar(value) {
			return Page{}, ErrRejected
		}
		ir.Where = append(ir.Where, data.Cond{Field: name, Op: ":", Value: fmt.Sprint(value), Literal: true})
	}
	for name, column := range r.read.Fields {
		ir.Select = append(ir.Select, name)
		if column == r.read.OrderBy {
			ir.Sort = name
		}
	}
	page, err := r.store.QueryRows(sc, ir, "", q.Limit)
	if err != nil {
		return Page{}, err
	}
	if err := r.checkEpoch(); err != nil {
		return Page{}, err
	}
	if r.state.Mode == data.ResidencyMirror {
		if err := r.store.AttachSourceVersions(r.binding.Key, r.state, page.Rows); err != nil {
			return Page{}, err
		}
	}
	source := r.source()
	source.ObserveNative(page.Rows)
	return Page{Rows: page.Rows, Source: source, HasMore: page.Next != ""}, nil
}

func (r residencyRoute) checkEpoch() error {
	current, err := r.store.Residency(data.Scope{App: r.config.App}, r.read.Type)
	if err != nil {
		return err
	}
	if current.Epochs != r.state.Epochs {
		return ErrConflict
	}
	return nil
}

// Refresh polls configured mirrors under the engine lifecycle. It also
// invalidates live watches periodically because N1 source reads are uncached.
func (s Service) Refresh(ctx context.Context) {
	s.refresh(ctx, make(map[string]time.Time))
}

// WatchResidency owns the runtime's polling cadence for both routes.
func (s Service) WatchResidency(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	lastLive := make(map[string]time.Time)
	nextPrune := time.Time{}
	for {
		if s.Store != nil && time.Now().After(nextPrune) {
			if store, err := s.Store(); err == nil {
				_ = store.PruneSourceIndexes()
			}
			nextPrune = time.Now().Add(time.Minute)
		}
		s.refresh(ctx, lastLive)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s Service) refresh(ctx context.Context, lastLive map[string]time.Time) {
	defs, err := s.vault().Definitions()
	if err != nil {
		return
	}
	for _, raw := range defs {
		var cfg registration
		if decodeJSON(raw, &cfg) != nil {
			continue
		}
		for _, read := range cfg.Reads {
			if ctx.Err() != nil {
				return
			}
			if read.Residency == nil && cfg.Kind != Google {
				continue
			}
			r, err := s.route(cfg.App, read.Type)
			if err != nil || r.config.ID != cfg.ID {
				continue
			}
			if r.state.Mode == data.ResidencyMirror {
				if time.Since(r.state.CheckedAt) >= time.Second*time.Duration(max(1, read.Residency.FreshForSeconds/2)) {
					_ = s.SyncResidency(ctx, cfg.App, read.Type)
				}
			} else if r.state.Mode == data.ResidencyLive && r.store.HasSourceWatch(r.typ.ID, r.binding.Key.Binding) {
				key := r.typ.ID
				interval := googlePollInterval
				if read.Residency != nil {
					interval = time.Duration(read.Residency.FreshForSeconds) * time.Second
				}
				if time.Since(lastLive[key]) >= interval {
					if r.store.InvalidateSource(r.typ.ID, r.binding.Key.Binding) == nil {
						lastLive[key] = time.Now()
					}
				}
			}
		}
	}
}

// PlanQuery resolves an app's data query to the canonical local store or an
// in-memory bounded source scan, with metadata and source revisions.
func (s Service) PlanQuery(ctx context.Context, app string, ir data.QueryIR) (data.QueryIR, Source, map[string]string, error) {
	r, err := s.route(app, ir.Type)
	if err != nil {
		return ir, Source{}, nil, err
	}
	scop := data.Scope{App: app, Person: s.person(ctx)}
	dependencies, err := r.store.QueryDependencies(scop, &ir)
	if err != nil {
		return ir, Source{}, nil, err
	}
	source := NativeSource()
	var scans []data.SourceScan
	var versions map[string]string
	if r.read.Residency != nil {
		source = r.source()
	}
	if r.state.Mode == data.ResidencyLive {
		scan, err := s.liveScan(ctx, r, ir.Type, textFilters(r, ir))
		if err != nil {
			return ir, Source{}, nil, err
		}
		scans = append(scans, scan)
		source, versions = scan.Source, sourceVersions(scan.Rows)
	}
	for _, id := range dependencies {
		relation, err := s.route(app, id)
		if err != nil {
			return ir, Source{}, nil, err
		}
		if relation.name == "" {
			continue
		}
		relatedSource := relation.source()
		if relation.state.Mode == data.ResidencyLive {
			scan, err := s.liveScan(ctx, relation, id, nil)
			if err != nil {
				return ir, Source{}, nil, err
			}
			scans = append(scans, scan)
			relatedSource = scan.Source
		}
		source.Contributors = append(source.Contributors, relatedSource)
	}
	planned, err := r.store.WithSourceScans(scop, ir, scans)
	return planned, source, versions, err
}

func (s Service) liveScan(ctx context.Context, r residencyRoute, typ string, filters map[string]any) (data.SourceScan, error) {
	page, err := s.Read(ctx, Query{App: r.config.App, Connection: r.config.ID, Read: r.name, Limit: maxRows, filters: filters})
	if err != nil {
		return data.SourceScan{}, err
	}
	if page.HasMore {
		return data.SourceScan{}, ErrTooLarge
	}
	return data.SourceScan{Type: typ, Rows: page.Rows, Source: page.Source}, nil
}

// Effective text equality matches SQLite's ASCII-only case folding. Any
// condition with additional syntax stays in the shared evaluator.
func textFilters(r residencyRoute, ir data.QueryIR) map[string]any {
	// Gmail search and the record evaluator have different semantics.
	if r.config.Kind == Google {
		return nil
	}
	out := make(map[string]any)
	for _, cond := range ir.Where {
		col := r.read.Fields[cond.Field]
		field := r.typ.Fields[cond.Field]
		value := cond.Value
		if !cond.Literal {
			value = strings.TrimSpace(value)
		}
		if col == "" || field == nil || field.Masked || r.typ.FieldPolicy(field) != "" || (field.Type != data.TypeText && field.Type != data.TypeLongtext) ||
			(cond.Op != ":" && cond.Op != "=") || cond.Neg || value == "" || strings.TrimSpace(value) == "" ||
			strings.ContainsAny(value, "*%_\\,\"\x00") || (!cond.Literal && strings.HasPrefix(value, "-")) {
			continue
		}
		out[col] = sqlTextMatch(value)
	}
	return out
}

func sourceVersions(rows []data.Row) map[string]string {
	versions := make(map[string]string, len(rows))
	for _, row := range rows {
		versions[row.ID()], _ = row["_source_version"].(string)
	}
	return versions
}

func NativeSource() Source {
	return Source{Residency: string(data.ResidencyNative), Freshness: string(data.ResidencyNative)}
}

// GetRecord uses a source key lookup for a live row and the normal point
// reader for every local mode.
func (s Service) GetRecord(ctx context.Context, app, alias, id string, reveal bool) (data.Row, Source, error) {
	r, err := s.route(app, alias)
	if err != nil {
		return nil, Source{}, err
	}
	if r.name == "" {
		row, err := r.store.Get(data.Scope{App: app, Person: s.person(ctx)}, alias, id, reveal)
		return row, NativeSource(), err
	}
	if r.state.Mode == data.ResidencyLive {
		if reveal {
			return nil, Source{}, &data.Error{Code: data.CodeNotAllowed, Message: "live source fields stay masked"}
		}
		p, err := s.Read(ctx, Query{App: app, Connection: r.config.ID, Read: r.name, Limit: 1, id: id})
		if err != nil {
			return nil, Source{}, err
		}
		if len(p.Rows) == 0 || p.Rows[0].ID() != id || p.HasMore {
			return nil, Source{}, ErrConflict
		}
		return p.Rows[0], p.Source, nil
	}
	row, err := r.store.Get(data.Scope{App: app, Person: s.person(ctx)}, alias, id, reveal)
	if err != nil {
		return nil, Source{}, err
	}
	if err := s.AttachVersions(app, alias, r.source(), []data.Row{row}); err != nil {
		return nil, Source{}, err
	}
	return row, r.source(), r.checkEpoch()
}

// AttachVersions adds the broker's expected source token separately from the
// SQLite record revision. A native update has no source write token.
func (s Service) AttachVersions(app, alias string, source Source, rows []data.Row) error {
	if source.Connection == "" {
		return nil
	}
	r, err := s.route(app, alias)
	if err != nil {
		return err
	}
	if r.state.Epochs.Residency != source.ResidencyEpoch || r.state.Epochs.Mapping != source.MappingEpoch {
		return ErrConflict
	}
	if r.state.Mode != data.ResidencyMirror {
		return nil
	}
	return r.store.AttachSourceVersions(r.binding.Key, r.state, rows)
}

// BindCursor prevents using a local cursor after a residency transition.
func cursorPrefix(source Source) string {
	return fmt.Sprintf("%d:%d:", source.MappingEpoch, source.ResidencyEpoch)
}

func BindCursor(source Source, cursor string) string {
	if cursor == "" {
		return ""
	}
	return cursorPrefix(source) + cursor
}

func UnbindCursor(source Source, cursor string) (string, bool) {
	return strings.CutPrefix(cursor, cursorPrefix(source))
}

func sourceField(read Read, typ *data.Type, name string) (*data.Field, error) {
	if read.Residency != nil && read.Residency.References[name] != nil {
		return typ.SourceReferenceField(name)
	}
	return typ.LiveField(name, false)
}

func mappedSourceValue(store *data.Store, sc data.Scope, read Read, field *data.Field, value any) (any, error) {
	if field.Type != data.TypeRef || value == nil {
		return field.LiveValue(value, false)
	}
	key, err := sourceScalar(value)
	if err != nil {
		return nil, err
	}
	id := read.Residency.References[field.Name][key]
	row, err := store.Get(sc, field.To, id, false)
	if err != nil {
		return nil, errors.New("a source reference has no permitted canonical record")
	}
	return row.ID(), nil
}

// WatchBindings names the bindings whose rows must be refetched rather than
// delivered. Native queries without distributed inputs use native snapshots.
func (s Service) WatchBindings(app string, ir data.QueryIR) ([]string, error) {
	root, err := s.route(app, ir.Type)
	if err != nil {
		return nil, err
	}
	depends, err := root.store.QueryDependencies(data.Scope{App: app}, &ir)
	if err != nil {
		return nil, err
	}
	var bindings []string
	if root.state.Mode != data.ResidencyNative {
		bindings = append(bindings, root.binding.Key.Binding)
	}
	for _, id := range depends {
		r, err := s.route(app, id)
		if err != nil {
			return nil, err
		}
		if r.state.Mode != data.ResidencyNative {
			bindings = append(bindings, r.binding.Key.Binding)
		}
	}
	return bindings, nil
}
