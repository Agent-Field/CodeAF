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
	summary, error, held, cancel_requested, priority, origin, session_id, intent, charter_id, trial_of, retry_of, service_intent, work_model, craft, attachments, created_seq, created_order, updated_seq,
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
		"craft":            `TEXT NOT NULL DEFAULT ''`,
	}
	for _, column := range []string{"title", "grp", "charter_id", "trial_of", "attachments", "retry_of", "held", "cancel_requested", "priority", "service_intent", "work_model", "craft"} {
		if existing[column] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE nodes ADD COLUMN ` + column + ` ` + columns[column]); err != nil {
			return err
		}
	}
	// charter_id is a migration column, so its index cannot live in the base
	// schema: a store created before the column existed would fail to open.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS nodes_charter ON nodes (origin, charter_id)`); err != nil {
		return err
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

// CharterFiredNodes returns only the nodes a charter firing admitted, in the
// same stable admission order as Nodes. The reconciler asks this question twice
// a second and the answer is almost always empty, so the filter belongs in SQL
// rather than in a full-table decode the caller throws away.
func (s *Store) CharterFiredNodes() ([]Node, error) {
	return s.queryNodes(`WHERE origin = ? AND charter_id != ''`, []any{OriginTrigger})
}

// SubtreeNodes returns root and every descendant in the same stable admission
// order as Nodes, without loading the rest of the graph.
func (s *Store) SubtreeNodes(root string) ([]Node, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE descendants(id) AS (
		    SELECT id FROM nodes WHERE id = ?
		    UNION ALL
		    SELECT child.id FROM nodes AS child
		    JOIN descendants ON child.parent_id = descendants.id
		)
		SELECT `+nodeColumns+` FROM nodes
		WHERE id IN (SELECT id FROM descendants)
		ORDER BY created_seq, created_order, id`, root)
	if err != nil {
		return nil, fmt.Errorf("read subtree %q: %w", root, err)
	}
	defer rows.Close()
	result := make([]Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("read subtree %q: %w", root, err)
		}
		result = append(result, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read subtree %q: %w", root, err)
	}
	return result, nil
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
		&node.Provenance.WorkModel, &node.Provenance.Craft, &attachments,
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
	// Most nodes carry neither pointers nor attachments, and the empty array is
	// the stored default. Recognizing it costs a comparison and saves a JSON
	// decode on every row of every whole-graph read.
	if node.FoldPointers, err = decodeStringArray(pointers); err != nil {
		return Node{}, fmt.Errorf("decode node %q fold pointers: %w", node.ID, err)
	}
	if node.Provenance.Attachments, err = decodeStringArray(attachments); err != nil {
		return Node{}, fmt.Errorf("decode node %q attachments: %w", node.ID, err)
	}
	return node, nil
}

func decodeStringArray(encoded string) ([]string, error) {
	if encoded == "" || encoded == "[]" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, err
	}
	return values, nil
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

// EventsThrough returns journal entries in the window (afterSeq, throughSeq],
// oldest first. It is the bounded form of Events, for a reader that already
// knows the watermark its work ends at: carrying the rest of the journal into
// memory only to discard it is a cost that grows with tenure forever.
//
// A throughSeq at or below afterSeq is an empty window, not an error.
func (s *Store) EventsThrough(afterSeq, throughSeq int64) ([]Event, error) {
	result := make([]Event, 0)
	if throughSeq <= afterSeq {
		return result, nil
	}
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, kind, payload
		FROM events WHERE seq > ? AND seq <= ? ORDER BY seq`, afterSeq, throughSeq)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer rows.Close()
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

// DependencyInput is one settled hard dependency as its consumer receives it:
// who produced it, the bounded digest of what it said, and the files it left
// behind.
//
// Artifacts are a separate field rather than the tail of the digest because
// they are the one part that must survive the byte bound. A producer writes its
// file list at the end of its summary, which is exactly where the bound bites
// first, and a consumer that loses the paths loses its only route to the full
// detail — it is then holding a 200-word pointer to work it cannot open.
type DependencyInput struct {
	NodeID    string
	Digest    string
	Artifacts []string
}

// DependencyDigests is the bounded context handed from settled hard
// dependencies to a downstream node. Failures are named instead of omitted.
func (s *Store) DependencyDigests(id string, maxBytes int) ([]string, error) {
	inputs, err := s.DependencyInputs(id, maxBytes)
	if err != nil {
		return nil, err
	}
	digests := make([]string, 0, len(inputs))
	for _, input := range inputs {
		digests = append(digests, input.Digest)
	}
	return digests, nil
}

// minDependencyBytes is the floor under one dependency's share. Below roughly
// this much a digest is a stub rather than a summary, so a fan-in wide enough to
// push every share under it takes fewer, fuller inputs instead of a hundred
// unreadable fragments — and says so.
const minDependencyBytes = 512

// dependencyClipNote is appended to a digest the budget cut short. It exists
// because the alternative is the failure this whole function used to have: a
// synthesis leaf writing a confident report over six of fifty findings, with
// nothing anywhere telling it the other forty-four were ever produced.
const dependencyClipNote = "\n[clipped to fit — the full text is in this step's own record and its files]"

// DependencyInputs is DependencyDigests with the producer and its files kept
// separate instead of flattened into one line.
//
// The budget is shared out per dependency rather than first-come. One shared
// pot in edge order meant the first verbose finding could take all 4 KB and
// every sibling after it was dropped by a bare `continue` — no marker, no
// warning, nothing the consumer could notice. A fan-out of fifty leaves
// therefore synthesised whatever happened to be first. Each dependency now gets
// an equal share of the pot, unused share is handed back to the ones that need
// it, and a clipped digest says out loud that it was clipped.
func (s *Store) DependencyInputs(id string, maxBytes int) ([]DependencyInput, error) {
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
	type produced struct {
		id      string
		line    string
		summary string
		failure string
	}
	settled := make([]produced, 0)
	for rows.Next() {
		var dependencyID, summary, failure string
		var status Status
		if err := rows.Scan(&dependencyID, &status, &summary, &failure); err != nil {
			return nil, fmt.Errorf("read dependencies for %q: %w", id, err)
		}
		line := dependencyDigest(dependencyID, status, summary, failure)
		if line == "" {
			continue
		}
		settled = append(settled, produced{id: dependencyID, line: line, summary: summary, failure: failure})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read dependencies for %q: %w", id, err)
	}
	if len(settled) == 0 {
		return []DependencyInput{}, nil
	}

	// How many can be carried at all. A share below the floor is not a small
	// digest, it is a fragment, so the pot buys as many whole inputs as it can
	// and the ones that did not fit are named rather than vanishing.
	pot := maxBytes
	carried := len(settled)
	if share := pot / carried; share < minDependencyBytes {
		carried = pot / minDependencyBytes
		if carried < 1 {
			carried = 1
		}
	}
	dropped := settled[carried:]
	settled = settled[:carried]
	overflow := ""
	if len(dropped) > 0 {
		names := make([]string, 0, len(dropped))
		for _, dependency := range dropped {
			names = append(names, dependency.id)
		}
		overflow = fmt.Sprintf("%d more finished step(s) fed into this one and did not fit here: %s. "+
			"Their results exist — say so rather than writing as though they did not.",
			len(dropped), strings.Join(names, ", "))
		// The notice is part of the bill, not an exemption from it.
		overflow = bounded(overflow, pot/2)
		pot -= len(overflow)
	}

	// Two passes so a short digest's unspent share reaches a long one: the first
	// gives each only what it needs up to an equal share, the second hands the
	// unspent remainder to whoever is still clipped.
	share := pot / len(settled)
	allotted := make([]int, len(settled))
	remaining := pot
	for index, dependency := range settled {
		allotted[index] = share
		if len(dependency.line) < share {
			allotted[index] = len(dependency.line)
		}
		remaining -= allotted[index]
	}
	for index, dependency := range settled {
		if remaining <= 0 {
			break
		}
		growth := len(dependency.line) - allotted[index]
		if growth <= 0 {
			continue
		}
		if growth > remaining {
			growth = remaining
		}
		allotted[index] += growth
		remaining -= growth
	}

	inputs := make([]DependencyInput, 0, len(settled)+1)
	for index, dependency := range settled {
		line := bounded(dependency.line, allotted[index])
		if len(dependency.line) > allotted[index] {
			// The marker is budgeted inside the allotment, not added on top of
			// it: a truncation that overflows the bound it is announcing is the
			// same lie in a different direction.
			line = bounded(dependency.line, allotted[index]-len(dependencyClipNote)) + dependencyClipNote
			if len(line) > allotted[index] {
				line = bounded(dependency.line, allotted[index])
			}
		}
		inputs = append(inputs, DependencyInput{
			NodeID: dependency.id, Digest: line,
			// A failed step's files are as real as a finished one's, and the
			// only place a failure records them is its error text.
			Artifacts: summaryPaths(dependency.summary + "\n" + dependency.failure),
		})
	}
	if overflow != "" {
		inputs = append(inputs, DependencyInput{Digest: overflow})
	}
	return inputs, nil
}

// summaryPathCap bounds how many paths one dependency may contribute. The list
// rides in a downstream prompt, so it is a context budget rather than a
// correctness limit.
const summaryPathCap = 8

// summaryPaths recovers the files a settled node wrote from the only durable
// record there is of them: its own summary. Artifacts are not a column — the
// executor's path→node map lives in the worker's memory and dies with it — so
// the producer writes them into the text it hands on, and this reads them back
// out. Absolute paths only: a bare word can be anything, and a wrong path
// offered as a file to open is worse than no file at all.
func summaryPaths(summary string) []string {
	var paths []string
	seen := make(map[string]bool)
	for _, field := range strings.Fields(summary) {
		path := strings.Trim(field, `"'(),;:.`)
		if !strings.HasPrefix(path, "/") || len(path) < 2 || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
		if len(paths) == summaryPathCap {
			break
		}
	}
	return paths
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
