package data

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// QuerySource describes the scan even when it returns zero rows.
type QuerySource struct {
	Connection      string    `json:"connection,omitempty"`
	Read            string    `json:"read,omitempty"`
	Residency       string    `json:"residency"`
	Freshness       string    `json:"freshness"`
	ObservedAt      time.Time `json:"observed_at"`
	MappingEpoch    int64     `json:"mapping_epoch,omitempty"`
	ResidencyEpoch  int64     `json:"residency_epoch,omitempty"`
	FreshForSeconds int       `json:"fresh_for_seconds,omitempty"`
}

// WithSourceRows compiles the same QueryField semantics over a bounded source
// snapshot. The rows remain in memory and are never inserted into the store.
func (s *Store) WithSourceRows(sc Scope, query QueryIR, rows []Row, source QuerySource) (QueryIR, error) {
	t, err := s.Type(sc, query.Type)
	if err != nil {
		return query, err
	}
	if len(rows) > PageMax {
		return query, errorf(CodeInvalid, "the source exceeds the query row budget")
	}
	type sourceRow struct {
		ID       string         `json:"id"`
		Revision any            `json:"revision"`
		Data     map[string]any `json:"data"`
	}
	input := make([]sourceRow, 0, len(rows))
	for _, row := range rows {
		values := map[string]any{}
		for name, value := range row {
			if field := t.field(name); field != nil && !field.Hidden && value != nil {
				values[field.ID] = value
			}
		}
		input = append(input, sourceRow{ID: row.ID(), Revision: row["_source_version"], Data: values})
	}
	raw := canonicalJSON(input)
	query.external = &sourceQuery{typeID: t.ID, raw: raw, cursor: fmt.Sprintf("%x",
		sha256.Sum256([]byte(canonicalJSON([]any{sc.App, query, source.MappingEpoch, source.ResidencyEpoch, raw}))))}
	return query, nil
}

type sourceQuery struct {
	typeID, raw, cursor string
}

func (c *compiled) statement(query string, args []any) (string, []any) {
	if c.external == nil {
		return query, args
	}
	// Keep native relation targets available while replacing the scanned type.
	return `WITH records AS (
		SELECT json_extract(value, '$.id') AS id, ? AS type_id, 0 AS rev,
			json_extract(value, '$.data') AS data_json, '{}' AS meta_json,
			'' AS created_at, '' AS updated_at, NULL AS deleted_at FROM json_each(?)
		UNION ALL SELECT id, type_id, rev, data_json, meta_json, created_at, updated_at, deleted_at
			FROM main.records WHERE type_id != ?
			AND NOT EXISTS(SELECT 1 FROM type_residency WHERE type_id = main.records.type_id AND mode = 'live')
	) ` + query, append([]any{c.external.typeID, c.external.raw, c.external.typeID}, args...)
}

func (s *Store) checkLocalRead(t *Type, c *compiled) error {
	if t.Common != "" && (c == nil || c.external == nil) {
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
			if target := c.sch.byID[id]; target == nil || target.Common == "" {
				continue
			}
			state, err := residencyState(s.rdb, id)
			if err != nil {
				return err
			}
			if state.Mode == ResidencyLive && (c.external == nil || id != c.external.typeID) {
				return errorf(CodeNotAllowed, "the query needs a separate live relation")
			}
		}
	}
	return nil
}

// AttachSourceVersions joins the precise source revision to a projected row.
// Matching the local revision refuses a response that raced a newer sync.
func (s *Store) AttachSourceVersions(key ExternalKey, expected TypeResidency, rows []Row) error {
	for _, row := range rows {
		var source string
		var rev int64
		var checkpoint string
		err := s.rdb.QueryRow(`SELECT o.source_revision, r.rev, tr.checkpoint FROM source_observations o
			JOIN external_keys k USING(binding, object, key, generation)
			JOIN records r ON r.id = k.entity_id
			JOIN type_residency tr ON tr.type_id = r.type_id
			WHERE r.id = ? AND o.binding = ? AND o.object = ? AND o.generation = ?`,
			row.ID(), key.Binding, key.Object, key.Generation).Scan(&source, &rev, &checkpoint)
		if err != nil {
			return err
		}
		if rev != row["_rev"] || checkpoint != expected.Checkpoint {
			return errorf(CodeStale, "the mirror advanced during the query")
		}
		row["_source_version"] = source
	}
	return nil
}
