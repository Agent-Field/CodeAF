package workspace

import "context"

// Placements are explicit governing bindings. Reference memberships remain a
// separate graph: filing or following a link never grants governing reach.
const placementSchema = `
CREATE TABLE placements (
 collection_id TEXT NOT NULL REFERENCES collections(id),
 kind TEXT NOT NULL CHECK(kind IN ('collection','conversation','task','standing','artifact')),
 ref_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 target_collection TEXT REFERENCES collections(id),
 CHECK ((kind='collection' AND target_collection IS NOT NULL AND target_collection=ref_id)
     OR (kind!='collection' AND target_collection IS NULL)),
 PRIMARY KEY(collection_id,kind,ref_id,session_id)
);
CREATE INDEX placements_reference ON placements(kind,ref_id,session_id);
`

// GoverningCollection reports the nearest governing path to a collection.
// Depth zero is a direct placement; larger depths traverse only placements.
// The caller decides whether a particular direction includes descendants.
type GoverningCollection struct {
	Collection
	Depth int `json:"depth"`
}

// AddPlacement explicitly binds a reference to a governing collection. It is
// idempotent and does not alter ordinary membership or start the referenced work.
// Multiple bindings are permitted; each collection's ancestry remains acyclic.
func (s *Store) AddPlacement(ctx context.Context, collectionID string, ref Ref) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, collectionID); err != nil {
		return err
	}
	var target any
	if ref.Kind == CollectionKind {
		if err := requireCollection(ctx, tx, ref.ID); err != nil {
			return err
		}
		// THE CHECK AND INSERT SHARE THE IMMEDIATE WRITER. Opposite edges
		// proposed by independent processes cannot both pass an old snapshot.
		var cycle bool
		err := tx.QueryRowContext(ctx, `WITH RECURSIVE ancestors(id) AS (
 VALUES (?) UNION
 SELECT p.collection_id FROM placements p JOIN ancestors a ON p.ref_id=a.id
 WHERE p.kind='collection'
) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=?)`, collectionID, ref.ID).Scan(&cycle)
		if err != nil {
			return err
		}
		if cycle {
			return ErrCycle
		}
		target = ref.ID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO placements(collection_id,kind,ref_id,session_id,target_collection)
 VALUES (?,?,?,?,?) ON CONFLICT(collection_id,kind,ref_id,session_id) DO NOTHING`, collectionID, ref.Kind, ref.ID, ref.SessionID, target)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RemovePlacement removes only this governing binding, preserving references
// and alternate governing paths. Removing an absent binding is harmless.
func (s *Store) RemovePlacement(ctx context.Context, collectionID string, ref Ref) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireCollection(ctx, tx, collectionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM placements WHERE collection_id=? AND kind=? AND ref_id=? AND session_id=?", collectionID, ref.Kind, ref.ID, ref.SessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// GoverningCollections reads one consistent graph snapshot, nearest first.
// UNION deduplicates equal-depth paths, avoiding exponential enumeration of
// diamond paths. MIN chooses the shortest depth when several paths reach one
// collection; creation order makes ties deterministic without implying priority.
func (s *Store) GoverningCollections(ctx context.Context, ref Ref) ([]GoverningCollection, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH RECURSIVE governing(id,depth) AS (
 SELECT collection_id,0 FROM placements WHERE kind=? AND ref_id=? AND session_id=?
 UNION
 SELECT p.collection_id,g.depth+1 FROM placements p JOIN governing g ON p.ref_id=g.id
 WHERE p.kind='collection'
)
SELECT c.id,c.name,MIN(g.depth) FROM governing g JOIN collections c ON c.id=g.id
GROUP BY c.id,c.name,c.seq ORDER BY MIN(g.depth),c.seq`, ref.Kind, ref.ID, ref.SessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]GoverningCollection, 0)
	for rows.Next() {
		var collection GoverningCollection
		if err := rows.Scan(&collection.ID, &collection.Name, &collection.Depth); err != nil {
			return nil, err
		}
		result = append(result, collection)
	}
	return result, rows.Err()
}

// Placed lists what is placed directly in a collection — the work its rules
// reach without passing through another folder — in key order. It reads the
// placements key's own prefix, so it costs what it returns.
func (s *Store) Placed(ctx context.Context, collectionID string) ([]Ref, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM collections WHERE id=?)", collectionID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, placedQuery, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Ref, 0)
	for rows.Next() {
		var ref Ref
		if err := rows.Scan(&ref.Kind, &ref.ID, &ref.SessionID); err != nil {
			return nil, err
		}
		result = append(result, ref)
	}
	return result, rows.Err()
}

// placedQuery is [Store.Placed]'s one read, named so its plan can be pinned.
const placedQuery = "SELECT kind,ref_id,session_id FROM placements WHERE collection_id=? ORDER BY kind,ref_id,session_id"
