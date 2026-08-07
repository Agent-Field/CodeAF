package store

import "fmt"

// Narrow reads of the node table. Each of these answers a question a caller
// used to answer by materializing the whole graph and filtering it in Go: node
// ids are a namespace the primary key already indexes, and the queue beside a
// node is a count, not a view.

// NodeIDExistsWithPrefix reports whether the graph holds prefix itself or any
// node inside its dash-delimited namespace.
func (s *Store) NodeIDExistsWithPrefix(prefix string) (bool, error) {
	if prefix == "" {
		return false, nil
	}
	namespace := prefix + "-"
	ceiling, ok := idPrefixCeiling(namespace)
	if !ok {
		return false, fmt.Errorf("check node id prefix %q: %w: prefix has no ordered ceiling", prefix, ErrInvalid)
	}
	var exists int
	if err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM nodes WHERE id = ? OR (id >= ? AND id < ?))`,
		prefix, namespace, ceiling).Scan(&exists); err != nil {
		return false, fmt.Errorf("check node id prefix %q: %w", prefix, err)
	}
	return exists != 0, nil
}

// NodeIDsWithPrefix returns every node id beginning with prefix, in id order.
func (s *Store) NodeIDsWithPrefix(prefix string) ([]string, error) {
	if prefix == "" {
		return nil, nil
	}
	ceiling, ok := idPrefixCeiling(prefix)
	if !ok {
		return nil, fmt.Errorf("read node ids with prefix %q: %w: prefix has no ordered ceiling", prefix, ErrInvalid)
	}
	rows, err := s.db.Query(`SELECT id FROM nodes WHERE id >= ? AND id < ? ORDER BY id`, prefix, ceiling)
	if err != nil {
		return nil, fmt.Errorf("read node ids with prefix %q: %w", prefix, err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read node ids with prefix %q: %w", prefix, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read node ids with prefix %q: %w", prefix, err)
	}
	return ids, nil
}

// PendingSiblingCount counts the work queued beside one node under the same
// parent, excluding the node itself. An empty parent is the spine root's
// missing parent, exactly as the node view reports it.
func (s *Store) PendingSiblingCount(id, parent string) (int, error) {
	var parentID any
	if parent != "" {
		parentID = parent
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes
		WHERE parent_id IS ? AND status = ? AND id <> ?`, parentID, Pending, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending siblings of %q: %w", id, err)
	}
	return count, nil
}

// idPrefixCeiling is the exclusive upper bound of a prefix under the id
// column's byte ordering, which is what turns "starts with" into an index
// range. It is undefined only for a prefix of nothing but 0xFF bytes, which no
// id can produce.
func idPrefixCeiling(prefix string) (string, bool) {
	raw := []byte(prefix)
	for index := len(raw) - 1; index >= 0; index-- {
		if raw[index] == 0xFF {
			continue
		}
		raw[index]++
		return string(raw[:index+1]), true
	}
	return "", false
}
