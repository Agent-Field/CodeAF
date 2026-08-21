package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type foldPayload struct {
	Digest   string   `json:"digest"`
	Pointers []string `json:"pointers"`
}

// Fold replaces a completed subtree in the active view with one bounded digest
// at subtreeRoot. The original nodes and edges remain materialized as folded
// history, and the journal retains every transition that produced them.
func (s *Store) Fold(subtreeRoot, digest string, pointers []string) error {
	subtreeRoot = strings.TrimSpace(subtreeRoot)
	if subtreeRoot == "" || subtreeRoot == RootID {
		return fmt.Errorf("fold %q: %w: the permanent spine cannot be folded", subtreeRoot, ErrInvalid)
	}
	digest = strings.TrimSpace(digest)
	pointers = uniqueStrings(pointers)

	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("fold %q: %w", subtreeRoot, err)
	}
	defer tx.Rollback()

	var alreadyFolded bool
	if err := tx.QueryRow(`SELECT fold_root FROM nodes WHERE id = ?`, subtreeRoot).Scan(&alreadyFolded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("fold %q: %w", subtreeRoot, ErrNotFound)
		}
		return fmt.Errorf("fold %q: %w", subtreeRoot, err)
	}
	if alreadyFolded {
		return fmt.Errorf("fold %q: %w: already folded", subtreeRoot, ErrInvalid)
	}

	rows, err := tx.Query(`
		WITH RECURSIVE subtree(id, status) AS (
		    SELECT id, status FROM nodes WHERE id = ?
		    UNION ALL
		    SELECT child.id, child.status
		    FROM nodes AS child
		    JOIN subtree AS parent ON child.parent_id = parent.id
		)
		SELECT id, status FROM subtree ORDER BY id`, subtreeRoot)
	if err != nil {
		return fmt.Errorf("fold %q: inspect subtree: %w", subtreeRoot, err)
	}
	var openID string
	var openStatus Status
	for rows.Next() {
		var id string
		var status Status
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return fmt.Errorf("fold %q: inspect subtree: %w", subtreeRoot, err)
		}
		if openID == "" && !terminal(status) {
			openID, openStatus = id, status
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("fold %q: inspect subtree: %w", subtreeRoot, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("fold %q: inspect subtree: %w", subtreeRoot, err)
	}
	if openID != "" {
		return fmt.Errorf("fold %q: %w: node %q is %s", subtreeRoot, ErrOpenSubtree, openID, openStatus)
	}

	payload, encodedPointers, err := s.prepareFold(digest, pointers)
	if err != nil {
		return fmt.Errorf("fold %q: %w", subtreeRoot, err)
	}

	seq, _, err := appendEvent(tx, subtreeRoot, EventSubtreeFolded, payload)
	if err != nil {
		return fmt.Errorf("fold %q: %w", subtreeRoot, err)
	}
	if err := applyFoldView(tx, subtreeRoot, payload.Digest, encodedPointers, seq); err != nil {
		return fmt.Errorf("materialize fold %q: %w", subtreeRoot, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fold %q: %w", subtreeRoot, err)
	}
	return nil
}

func (s *Store) prepareFold(digest string, pointers []string) (foldPayload, string, error) {
	digest = strings.TrimSpace(digest)
	pointers = uniqueStrings(pointers)

	// The graph carries only a bounded map. When a fold is larger, preserve its
	// full territory in the immutable CAS and put the ordinary filesystem path
	// on the fold so the executor can read it with the same tool as any artifact.
	if len(digest) > MaxDigestBytes {
		ref, err := s.blobs.PutBytes([]byte(digest))
		if err != nil {
			return foldPayload{}, "", fmt.Errorf("spill digest: %w", err)
		}
		path, err := s.blobs.Path(ref)
		if err != nil {
			return foldPayload{}, "", fmt.Errorf("locate spilled digest: %w", err)
		}
		pointers = uniqueStrings(append(pointers, path))
	}
	payload := foldPayload{Digest: bounded(digest, MaxDigestBytes), Pointers: pointers}
	encodedPointers, err := json.Marshal(payload.Pointers)
	if err != nil {
		return foldPayload{}, "", fmt.Errorf("encode pointers: %w", err)
	}
	return payload, string(encodedPointers), nil
}

func applyFoldView(tx *sql.Tx, root, digest, pointers string, seq int64) error {
	result, err := tx.Exec(`
		WITH RECURSIVE subtree(id) AS (
		    SELECT id FROM nodes WHERE id = ?
		    UNION ALL
		    SELECT child.id
		    FROM nodes AS child
		    JOIN subtree AS parent ON child.parent_id = parent.id
		)
		UPDATE nodes
		SET folded = 1,
		    fold_root = CASE WHEN id = ? THEN 1 ELSE fold_root END,
		    fold_digest = CASE WHEN id = ? THEN ? ELSE fold_digest END,
		    fold_pointers = CASE WHEN id = ? THEN ? ELSE fold_pointers END,
		    updated_seq = ?
		WHERE id IN (SELECT id FROM subtree)`,
		root, root, root, digest, root, pointers, seq)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrNotFound
	}
	return refreshGraphFTS(tx, root)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
