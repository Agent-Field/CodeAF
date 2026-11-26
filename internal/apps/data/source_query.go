package data

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// QuerySource describes the scan even when it returns zero rows.
type QuerySource struct {
	Connection      string        `json:"connection,omitempty"`
	Read            string        `json:"read,omitempty"`
	Residency       string        `json:"residency"`
	Freshness       string        `json:"freshness"`
	ObservedAt      time.Time     `json:"observed_at,omitempty,omitzero"`
	MappingEpoch    int64         `json:"mapping_epoch,omitempty"`
	ResidencyEpoch  int64         `json:"residency_epoch,omitempty"`
	Partial         bool          `json:"partial,omitempty"`
	Window          int           `json:"window,omitempty"`
	FreshForSeconds int           `json:"fresh_for_seconds,omitempty"`
	Contributors    []QuerySource `json:"contributors,omitempty"`
}

// ObserveNative uses the newest visible record commit as the observation.
// Empty results keep their residency and freshness without inventing a time.
func (source *QuerySource) ObserveNative(rows []Row) {
	if source.Residency != string(ResidencyNative) {
		return
	}
	for _, row := range rows {
		updated, ok := row["updated_at"].(string)
		at, err := time.Parse(stamp, updated)
		if ok && err == nil && at.After(source.ObservedAt) {
			source.ObservedAt = at
		}
	}
}

// SourceScan carries an authorized fragment to the shared query compiler.
type SourceScan struct {
	Type   string
	Rows   []Row
	Source QuerySource
}

// QueryDependencies validates grants before any remote fragment is fetched.
func (s *Store) QueryDependencies(sc Scope, ir *QueryIR) ([]string, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, ir.Type, false)
	if err != nil {
		return nil, err
	}
	filters := *ir
	filters.Group = nil
	c, err := compileIR(sch, t, sc, &filters, s.now())
	if err != nil {
		return nil, err
	}
	if _, err := s.selection(sch, t, sc, ir.Select, c); err != nil {
		return nil, err
	}
	if len(ir.Aggregate) > 0 {
		for _, name := range ir.Group {
			if _, _, err := c.groupColumn(sc, name); err != nil {
				return nil, err
			}
		}
	}
	delete(c.related, t.ID)
	return sortedKeys(c.related), nil
}

// WithSourceScans compiles the same QueryField semantics over bounded source
// observations. The rows are never inserted into the store.
func (s *Store) WithSourceScans(sc Scope, query QueryIR, scans []SourceScan) (QueryIR, error) {
	if len(scans) == 0 {
		return query, nil
	}
	type sourceRow struct {
		ID       string         `json:"id"`
		Type     string         `json:"type"`
		Revision any            `json:"revision"`
		Data     map[string]any `json:"data"`
	}
	input := []sourceRow{}
	types := map[string]bool{}
	var epochs []any
	for _, scan := range scans {
		t, err := s.Type(sc, scan.Type)
		if err != nil {
			return query, err
		}
		if len(input)+len(scan.Rows) > PageMax {
			return query, errorf(CodeInvalid, "the source fragments exceed the query row budget")
		}
		types[t.ID] = true
		for _, row := range scan.Rows {
			values := map[string]any{}
			for name, value := range row {
				if field := t.field(name); field != nil && !field.Hidden && value != nil {
					values[field.ID] = value
				}
			}
			input = append(input, sourceRow{ID: row.ID(), Type: t.ID, Revision: row["_source_version"], Data: values})
		}
		epochs = append(epochs, []any{t.ID, scan.Source.MappingEpoch, scan.Source.ResidencyEpoch})
	}
	raw := canonicalJSON(input)
	query.external = &sourceQuery{types: types, raw: raw, cursor: fmt.Sprintf("%x",
		sha256.Sum256([]byte(canonicalJSON([]any{sc.App, sc.Person.ID, sc.Principal.ID, query, epochs, raw}))))}
	return query, nil
}

type sourceQuery struct {
	types       map[string]bool
	raw, cursor string
}

func (c *compiled) statement(query string, args []any) (string, []any) {
	if c.external == nil {
		return query, args
	}
	// Native inputs share the same evaluator as the authorized source rows.
	return `WITH records AS (
        SELECT json_extract(value,'$.id') AS id, json_extract(value,'$.type') AS type_id,
            0 AS rev, json_extract(value,'$.data') AS data_json, '{}' AS meta_json,
            '' AS created_at, '' AS updated_at, NULL AS deleted_at FROM json_each(?)
        UNION ALL SELECT id, type_id, rev, data_json, meta_json, created_at, updated_at, deleted_at
            FROM main.records WHERE type_id NOT IN (SELECT value FROM json_each(?))
            AND NOT EXISTS(SELECT 1 FROM type_residency WHERE type_id = main.records.type_id AND mode = 'live')
    ) ` + query, append([]any{c.external.raw, canonicalJSON(sortedKeys(c.external.types))}, args...)
}

func (s *Store) checkLocalRead(t *Type, c *compiled) error {
	hasScan := func(id string) bool { return c != nil && c.external != nil && c.external.types[id] }
	if t.Common != "" && !hasScan(t.ID) {
		state, err := residencyState(s.rdb, t.ID)
		if err != nil {
			return err
		}
		if state.Mode == ResidencyLive {
			return errorf(CodeNotAllowed, "this type needs its live source query")
		}
	}
	if c != nil {
		for id := range c.related {
			if target := c.sch.byID[id]; target == nil || target.Common == "" || hasScan(id) {
				continue
			}
			state, err := residencyState(s.rdb, id)
			if err != nil {
				return err
			}
			if state.Mode == ResidencyLive {
				return errorf(CodeNotAllowed, "the query needs a separate live relation")
			}
		}
	}
	return nil
}

// AttachSourceVersions joins the precise source revision to a projected row.
// Matching the local revision refuses a response that raced a newer sync.
func (s *Store) AttachSourceVersions(key ExternalKey, expected TypeResidency, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	projected := make(map[string]Row, len(rows))
	var ids []string
	for _, row := range rows {
		projected[row.ID()] = row
		ids = append(ids, row.ID())
	}
	seen := make(map[string]bool, len(ids))
	err := eachRow(s.rdb, `SELECT r.id, o.source_revision, r.rev, tr.checkpoint FROM source_observations o
        JOIN external_keys k USING(binding, object, key, generation)
        JOIN records r ON r.id = `+identityRefSQL(nil, "k.entity_id")+`
        JOIN type_residency tr ON tr.type_id = r.type_id
        WHERE o.binding = ? AND o.object = ? AND o.generation = ?
        AND r.id IN (SELECT value FROM json_each(?))`,
		[]any{key.Binding, key.Object, key.Generation, canonicalJSON(ids)}, func(scan func(...any) error) error {
			var id, source, checkpoint string
			var revision int64
			if err := scan(&id, &source, &revision, &checkpoint); err != nil {
				return err
			}
			if seen[id] || revision != projected[id]["_rev"] || checkpoint != expected.Checkpoint {
				return errorf(CodeStale, "the mirror advanced during the query")
			}
			projected[id]["_source_version"] = source
			seen[id] = true
			return nil
		})
	if err == nil && len(seen) != len(ids) {
		return errorf(CodeStale, "a mirrored row changed during the query")
	}
	return err
}
