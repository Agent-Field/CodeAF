package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const nodeColumns = `
	id, parent_id, brief, title, grp, stage, status, owner, claim_token, attempt,
	summary, error, held, cancel_requested, priority, origin, session_id, intent, charter_id, trial_of, retry_of, service_intent, work_model, attachments, created_seq, created_order, updated_seq,
    started_at, finished_at, folded, fold_root, fold_digest, fold_pointers`

// migrateNodesSchema adds provenance and display columns introduced after the
// original node table. ALTER TABLE is idempotent-by-inspection: the column
// list is read first, so re-opening an already-migrated store does nothing.
func migrateNodesSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(nodes)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primary int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &primary); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	columns := map[string]string{
		"title":            `TEXT NOT NULL DEFAULT ''`,
		"grp":              `TEXT NOT NULL DEFAULT ''`,
		"charter_id":       `TEXT NOT NULL DEFAULT ''`,
		"trial_of":         `INTEGER NOT NULL DEFAULT 0 CHECK (trial_of >= 0)`,
		"attachments":      `JSON NOT NULL DEFAULT '[]' CHECK (json_valid(attachments))`,
		"retry_of":         `TEXT NOT NULL DEFAULT ''`,
		"held":             `INTEGER NOT NULL DEFAULT 0 CHECK (held IN (0, 1))`,
		"cancel_requested": `INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1))`,
		"priority":         `INTEGER NOT NULL DEFAULT 0`,
		"service_intent":   `INTEGER NOT NULL DEFAULT 0 CHECK (service_intent IN (0, 1))`,
		"work_model":       `TEXT NOT NULL DEFAULT ''`,
	}
	for _, column := range []string{"title", "grp", "charter_id", "trial_of", "attachments", "retry_of", "held", "cancel_requested", "priority", "service_intent", "work_model"} {
		if existing[column] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE nodes ADD COLUMN ` + column + ` ` + columns[column]); err != nil {
			return err
		}
	}
	return nil
}

// Node returns one node from the complete materialized view.
func (s *Store) Node(id string) (Node, bool, error) {
	row := s.db.QueryRow(`SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id)
	node, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, false, nil
	}
	if err != nil {
		return Node{}, false, fmt.Errorf("read node %q: %w", id, err)
	}
	return node, true, nil
}

// Nodes returns every node, including folded history, in stable admission
// order.
func (s *Store) Nodes() ([]Node, error) {
	return s.queryNodes(``, nil)
}

// ActiveNodes returns the live graph plus the outermost compact representative
// for each fold. Nested fold roots remain addressable history, but their folded
// parent already represents them in the active view.
func (s *Store) ActiveNodes() ([]Node, error) {
	return s.queryNodes(`
		WHERE folded = 0 OR (
			fold_root = 1 AND NOT EXISTS (
				SELECT 1 FROM nodes AS parent
				WHERE parent.id = nodes.parent_id AND parent.fold_root = 1
			)
		)`, nil)
}

func (s *Store) queryNodes(where string, args []any) ([]Node, error) {
	return s.queryNodesLimit(where, args, 0)
}

func (s *Store) queryNodesLimit(where string, args []any, limit int) ([]Node, error) {
	return s.queryNodesLimitOrdered(where, args, limit, `created_seq, created_order, id`)
}

func (s *Store) queryNodesLimitOrdered(where string, args []any, limit int, order string) ([]Node, error) {
	statement := `SELECT ` + nodeColumns + ` FROM nodes ` + where + ` ORDER BY ` + order
	if limit > 0 {
		statement += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(statement, args...)
	if err != nil {
		return nil, fmt.Errorf("read nodes: %w", err)
	}
	defer rows.Close()
	result := make([]Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("read nodes: %w", err)
		}
		result = append(result, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read nodes: %w", err)
	}
	return result, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNode(scanner rowScanner) (Node, error) {
	var node Node
	var parent, session, started, finished sql.NullString
	var pointers, attachments string
	if err := scanner.Scan(
		&node.ID, &parent, &node.Brief, &node.Title, &node.Group, &node.Stage, &node.Status,
		&node.Owner, &node.ClaimToken, &node.Attempt, &node.Summary, &node.Error,
		&node.Held, &node.CancelRequested, &node.Priority,
		&node.Provenance.Origin, &session, &node.Provenance.Intent, &node.Provenance.CharterID, &node.Provenance.TrialOf, &node.Provenance.RetryOf, &node.Provenance.ServiceIntent,
		&node.Provenance.WorkModel, &attachments,
		&node.CreatedSeq, &node.CreatedOrder, &node.UpdatedSeq, &started, &finished,
		&node.Folded, &node.FoldRoot, &node.FoldDigest, &pointers,
	); err != nil {
		return Node{}, err
	}
	if parent.Valid {
		node.Parent = parent.String
	}
	if session.Valid {
		node.Provenance.SessionID = session.String
	}
	var err error
	if started.Valid {
		node.StartedAt, err = parseTime(started.String)
		if err != nil {
			return Node{}, fmt.Errorf("parse node %q start time: %w", node.ID, err)
		}
	}
	if finished.Valid {
		node.FinishedAt, err = parseTime(finished.String)
		if err != nil {
			return Node{}, fmt.Errorf("parse node %q finish time: %w", node.ID, err)
		}
	}
	if err := json.Unmarshal([]byte(pointers), &node.FoldPointers); err != nil {
		return Node{}, fmt.Errorf("decode node %q fold pointers: %w", node.ID, err)
	}
	if err := json.Unmarshal([]byte(attachments), &node.Provenance.Attachments); err != nil {
		return Node{}, fmt.Errorf("decode node %q attachments: %w", node.ID, err)
	}
	return node, nil
}

// Edges returns every materialized edge, including folded history.
func (s *Store) Edges() ([]Edge, error) {
	return s.queryEdges(``)
}

// ActiveEdges returns only edges whose endpoints remain in ActiveNodes.
func (s *Store) ActiveEdges() ([]Edge, error) {
	return s.queryEdges(`
		JOIN nodes AS source ON source.id = edge.from_id
		JOIN nodes AS target ON target.id = edge.to_id
		WHERE (source.folded = 0 OR (source.fold_root = 1 AND NOT EXISTS (
		          SELECT 1 FROM nodes AS parent
		          WHERE parent.id = source.parent_id AND parent.fold_root = 1
		      )))
		  AND (target.folded = 0 OR (target.fold_root = 1 AND NOT EXISTS (
		          SELECT 1 FROM nodes AS parent
		          WHERE parent.id = target.parent_id AND parent.fold_root = 1
		      )))`)
}

func (s *Store) queryEdges(joinWhere string) ([]Edge, error) {
	rows, err := s.db.Query(`
		SELECT edge.from_id, edge.to_id, edge.kind, edge.created_seq, edge.created_order
		FROM edges AS edge ` + joinWhere + `
		ORDER BY edge.created_seq, edge.created_order, edge.from_id, edge.to_id, edge.kind`)
	if err != nil {
		return nil, fmt.Errorf("read edges: %w", err)
	}
	defer rows.Close()
	result := make([]Edge, 0)
	for rows.Next() {
		var edge Edge
		if err := rows.Scan(&edge.From, &edge.To, &edge.Kind, &edge.CreatedSeq, &edge.CreatedOrder); err != nil {
			return nil, fmt.Errorf("read edges: %w", err)
		}
		result = append(result, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read edges: %w", err)
	}
	return result, nil
}

// Snapshot copies both complete materialized views.
func (s *Store) Snapshot() (Snapshot, error) {
	nodes, err := s.Nodes()
	if err != nil {
		return Snapshot{}, err
	}
	edges, err := s.Edges()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Nodes: nodes, Edges: edges}, nil
}

// ActiveSnapshot copies the compact, schedulable view.
func (s *Store) ActiveSnapshot() (Snapshot, error) {
	nodes, err := s.ActiveNodes()
	if err != nil {
		return Snapshot{}, err
	}
	edges, err := s.ActiveEdges()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Nodes: nodes, Edges: edges}, nil
}

// Events returns journal entries after afterSeq. A non-positive limit means no
// limit.
func (s *Store) Events(afterSeq int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, kind, payload
		FROM events WHERE seq > ? ORDER BY seq LIMIT ?`, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer rows.Close()
	result := make([]Event, 0)
	for rows.Next() {
		var event Event
		var timestamp, payload string
		if err := rows.Scan(&event.Seq, &timestamp, &event.NodeID, &event.Kind, &payload); err != nil {
			return nil, fmt.Errorf("read events: %w", err)
		}
		parsed, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("parse event %d time: %w", event.Seq, err)
		}
		event.Time = parsed
		event.Payload = json.RawMessage(payload)
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return result, nil
}

// LatestEventSeq returns the current journal watermark without walking the
// journal. It is useful for projections that report only transitions written
// by one bounded operation.
func (s *Store) LatestEventSeq() (int64, error) {
	var seq int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read latest event sequence: %w", err)
	}
	return seq, nil
}

// Ready returns pending active nodes whose hard dependencies have all settled.
// Failed and cancelled dependencies are terminal by design; their digest is
// available through DependencyDigests.
func (s *Store) Ready(limit int) ([]Node, error) {
	// The spine root and organizational furniture are never ready work, no
	// matter what status a repair or migration leaves them in.
	where := `
		WHERE status = ? AND folded = 0 AND held = 0 AND cancel_requested = 0
		  AND id != ? AND grp NOT IN (?)
		  AND NOT EXISTS (
		      SELECT 1
		      FROM edges AS edge
		      JOIN nodes AS dependency ON dependency.id = edge.from_id
		      WHERE edge.to_id = nodes.id
		        AND edge.kind IN (?, ?)
		        AND dependency.status NOT IN (?, ?, ?)
		  )`
	return s.queryNodesLimitOrdered(where, []any{Pending, RootID, TerritoryGroup, FeedsInto, Blocks, Done, Failed, Cancelled}, limit,
		`priority DESC, created_seq, created_order, id`)
}

// DependencyDigests is the bounded context handed from settled hard
// dependencies to a downstream node. Failures are named instead of omitted.
func (s *Store) DependencyDigests(id string, maxBytes int) ([]string, error) {
	if maxBytes <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT dependency.id, dependency.status, dependency.summary, dependency.error
		FROM edges AS edge
		JOIN nodes AS dependency ON dependency.id = edge.from_id
		WHERE edge.to_id = ? AND edge.kind IN (?, ?)
		ORDER BY edge.created_seq, edge.created_order, dependency.created_seq, dependency.created_order, dependency.id`, id, FeedsInto, Blocks)
	if err != nil {
		return nil, fmt.Errorf("read dependencies for %q: %w", id, err)
	}
	defer rows.Close()
	remaining := maxBytes
	digests := make([]string, 0)
	for rows.Next() {
		var dependencyID, summary, failure string
		var status Status
		if err := rows.Scan(&dependencyID, &status, &summary, &failure); err != nil {
			return nil, fmt.Errorf("read dependencies for %q: %w", id, err)
		}
		line := dependencyDigest(dependencyID, status, summary, failure)
		if line == "" || remaining <= 0 {
			continue
		}
		line = bounded(line, remaining)
		digests = append(digests, line)
		remaining -= len(line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read dependencies for %q: %w", id, err)
	}
	return digests, nil
}

func dependencyDigest(id string, status Status, summary, failure string) string {
	switch status {
	case Done:
		if strings.TrimSpace(summary) == "" {
			return id + ": completed"
		}
		return id + ": " + summary
	case Failed:
		if strings.TrimSpace(failure) == "" {
			failure = "no reason recorded"
		}
		return id + " (failed): " + failure
	case Cancelled:
		if strings.TrimSpace(failure) == "" {
			failure = "no reason recorded"
		}
		return id + " (not run): " + failure
	default:
		return ""
	}
}
