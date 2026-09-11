package workspace

import (
	"context"
	"strings"
)

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
	return s.governing(ctx, "SELECT collection_id,0 FROM placements WHERE kind=? AND ref_id=? AND session_id=?", ref.Kind, ref.ID, ref.SessionID)
}

// GoverningIfPlaced answers what [Store.GoverningCollections] would answer for
// a reference placed directly in the given collections and nowhere else.
//
// IT EXISTS FOR WORK THAT DOES NOT EXIST YET. A proposal for ongoing work names
// the folder it will be placed in, and the person is owed the rules that folder
// brings BEFORE saying yes — but nothing may be placed until the yes, so there
// is no reference to ask about. It is the same walk seeded with the folders
// themselves, which is exactly what a placement in them would seed it with, so
// the two readings cannot disagree about ancestry. A collection that does not
// exist is refused rather than silently dropped from the seed.
func (s *Store) GoverningIfPlaced(ctx context.Context, collectionIDs []string) ([]GoverningCollection, error) {
	if len(collectionIDs) == 0 {
		return []GoverningCollection{}, nil
	}
	seen := make(map[string]bool, len(collectionIDs))
	args := make([]any, 0, len(collectionIDs))
	marks := make([]string, 0, len(collectionIDs))
	for _, id := range collectionIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		args = append(args, id)
		marks = append(marks, "?")
	}
	result, err := s.governing(ctx, "SELECT id,0 FROM collections WHERE id IN ("+strings.Join(marks, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	direct := 0
	for _, collection := range result {
		if collection.Depth == 0 {
			direct++
		}
	}
	if direct != len(args) {
		return nil, ErrNotFound
	}
	return result, nil
}

// governing is the ONE walk up the placement graph, from whichever seed the
// caller names: the rows `seed` selects are depth zero, and every collection a
// placement chain reaches from them follows at its shortest depth.
func (s *Store) governing(ctx context.Context, seed string, args ...any) ([]GoverningCollection, error) {
	rows, err := s.db.QueryContext(ctx, `WITH RECURSIVE governing(id,depth) AS (
 `+seed+`
 UNION
 SELECT p.collection_id,g.depth+1 FROM placements p JOIN governing g ON p.ref_id=g.id
 WHERE p.kind='collection'
)
SELECT c.id,c.name,MIN(g.depth) FROM governing g JOIN collections c ON c.id=g.id
GROUP BY c.id,c.name,c.seq ORDER BY MIN(g.depth),c.seq`, args...)
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
