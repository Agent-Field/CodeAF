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
	summary, error, held, cancel_requested, priority, origin, session_id, intent, charter_id, trial_of, retry_of, service_intent, work_model, plan_model, run_model, craft, subharness, splice_subharness, ran, spec, attachments, created_seq, created_order, updated_seq,
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
		// plan_model is empty for every node written before it existed, and empty
		// is exactly what "the plan slot followed the work slot" has always meant,
		// so an old store reads back as the truth it was recorded under.
		"plan_model": `TEXT NOT NULL DEFAULT ''`,
		// run_model rides beside plan_model and is empty wherever plan_model is,
		// including on every node written before either existed. A split job from
		// an older build therefore reads back as it always did: it says who
		// planned it and stays silent about who ran it, which is the truth about
		// what that build recorded.
		"run_model": `TEXT NOT NULL DEFAULT ''`,
		"craft":     `TEXT NOT NULL DEFAULT ''`,
		// subharness is the node's settled worker; splice_subharness is the
		// choice the whole subtree was admitted under. Both default to empty,
		// which is the generalist, so every node written before either column
		// existed reads back exactly as it always did.
		"subharness":        `TEXT NOT NULL DEFAULT ''`,
		"splice_subharness": `TEXT NOT NULL DEFAULT ''`,
		// ran is the worker that actually executed the node, and it is the one
		// column here that is a fact about the RUN rather than about the plan.
		// Empty on every node written before it existed, and empty means what
		// it has always meant for those stores: nobody wrote down who ran this.
		// A reader that wants the fact for an old store has only the assignment
		// column to fall back on, and it must say so rather than guess.
		"ran": `TEXT NOT NULL DEFAULT ''`,
		// spec holds the planner's task object as it was admitted. Empty is the
		// default and is exactly what "this node was admitted before specs
		// existed" means, so an old store reads back as it always did and every
		// reader falls through to brief, which is still the read.
		"spec": `TEXT NOT NULL DEFAULT ''`,
	}
	for _, column := range []string{"title", "grp", "charter_id", "trial_of", "attachments", "retry_of", "held", "cancel_requested", "priority", "service_intent", "work_model", "plan_model", "run_model", "craft", "subharness", "splice_subharness", "ran", "spec"} {
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

// CharterFiredNodes returns the nodes a charter firing admitted and whose
// outcome may still be undecided, in the same stable admission order as Nodes.
// The reconciler asks this question twice a second and the answer is almost
// always empty, so the filter belongs in SQL rather than in a full-table decode
// the caller throws away.
//
// Two clauses do that narrowing, and both are statements about what cannot
// still be pending. A folded node's job settled at least a fold grace ago, and
// the resident reviews charter outcomes before it folds anything on every one
// of the thousands of ticks in between — so a folded firing has been reviewed,
// and asking again costs a node read, a parent walk and two unindexed
// json_extract queries to be told so. afterSeq is the caller's own watermark
// over the same fact: everything at or below it is settled business, and the
// verdict it reached is journaled, so nothing is lost by not deriving it twice.
func (s *Store) CharterFiredNodes(afterSeq int64) ([]Node, error) {
	return s.queryNodes(`WHERE origin = ? AND charter_id != '' AND folded = 0 AND created_seq > ?`,
		[]any{OriginTrigger, afterSeq})
}

// SessionMemberNodes returns every node one session admitted — steps and all,
// not the job roots [Store.SessionNodes] answers with — in the same stable
// admission order as Nodes. The splice stamps its session on every node it
// admits and an extension inherits it, so equality on the stored id is the
// whole membership test, and the spine root, which belongs to no session, is
// excluded the way every session-scoped read excludes it.
//
// A headless run asks this several times a second for as long as it lasts. It
// used to ask by decoding every node in the graph and throwing away the ones
// that were somebody else's, which on a store with any history at all is a
// thirty-nine-column decode of a month's work to be told about four nodes.
func (s *Store) SessionMemberNodes(sessionID string) ([]Node, error) {
	return s.queryNodes(`WHERE session_id = ? AND id != ?`, []any{sessionID, RootID})
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

// OpenNodes is every node the graph has not finished with, FOLDED OR NOT.
//
// Folding is a presentation decision about SETTLED work: a job is filed away
// once it is over, and the fold root then speaks for its members so the active
// view stays the size of what is happening. Nothing about that reasoning
// applies to a node that is still running, claimed or pending, and the day the
// two states met the reasoning failed outright — a running continuation whose
// lineage had been filed away was invisible to every read the head owns, so a
// person watching it tick on the rail asked to cancel it and was told, three
// board reads and five searches later, that no such work existed. The rail
// could see it; nothing the head could ask could.
//
// So this is the corpus that answers "what is still going on", and its one law
// is that FOLDING HIDES NOTHING THAT IS STILL ALIVE. It is deliberately not a
// replacement for [Store.ActiveNodes] — the compact view is right for the
// question it answers — but any reader whose sentence turns on liveness must
// union this in, because a live node missing from that reader's world is not a
// tidier answer, it is a false one.
//
// The permanent spine is excluded: it is Running forever by construction and is
// nobody's live work.
func (s *Store) OpenNodes() ([]Node, error) {
	return s.queryNodes(`WHERE id != ? AND status NOT IN (?, ?, ?)`,
		[]any{RootID, Done, Failed, Cancelled})
}

// withOpenNodes unions the open corpus into a view that may have folded some of
// it away, keeping the view's own order and appending what it was missing.
//
// A failed read leaves the view as it was rather than failing the caller: this
// is a widening, and a widening that cannot be performed must not narrow the
// answer to nothing.
func withOpenNodes(s *Store, view []Node) []Node {
	open, err := s.OpenNodes()
	if err != nil || len(open) == 0 {
		return view
	}
	present := make(map[string]bool, len(view))
	for _, node := range view {
		present[node.ID] = true
	}
	for _, node := range open {
		if present[node.ID] {
			continue
		}
		present[node.ID] = true
		view = append(view, node)
	}
	return view
}

// AddressableNodes is ActiveNodes plus the jobs a territory has packed away.
//
// The packer is what made a month-old job invisible to every snapshot-derived
// read at once. FormTerritory re-parents a settled fold under a territory node
// that is itself a fold root, so ActiveNodes' "outermost representative"
// clause — correct for a fold's own members, which the root speaks for — starts
// excluding the job root too. The territory then speaks for it, and a territory
// says "eleven jobs about pricing", which is not an answer to a question about
// one of them.
//
// So reads that are asking "what do I have about this?" use this corpus and
// verbs keep the compact one. Membership in a territory is a filing decision
// made hours after a job landed; it was never meant to be the thing that
// decides whether the job can be spoken about.
func (s *Store) AddressableNodes() ([]Node, error) {
	return s.queryNodes(`
		WHERE folded = 0 OR (
			fold_root = 1 AND NOT EXISTS (
				SELECT 1 FROM nodes AS parent
				WHERE parent.id = nodes.parent_id AND parent.fold_root = 1
			)
		) OR (
			fold_root = 1 AND EXISTS (
				SELECT 1 FROM nodes AS packer
				WHERE packer.id = nodes.parent_id AND packer.grp = ?
			)
		)`, []any{TerritoryGroup})
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
	rows, err := s.queryPrepared(statement, args...)
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
	var pointers, attachments, spec string
	if err := scanner.Scan(
		&node.ID, &parent, &node.Brief, &node.Title, &node.Group, &node.Stage, &node.Status,
		&node.Owner, &node.ClaimToken, &node.Attempt, &node.Summary, &node.Error,
		&node.Held, &node.CancelRequested, &node.Priority,
		&node.Provenance.Origin, &session, &node.Provenance.Intent, &node.Provenance.CharterID, &node.Provenance.TrialOf, &node.Provenance.RetryOf, &node.Provenance.ServiceIntent,
		&node.Provenance.WorkModel, &node.Provenance.PlanModel, &node.Provenance.RunModel, &node.Provenance.Craft, &node.Subharness, &node.Provenance.Subharness, &node.Ran, &spec, &attachments,
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
	// The spec goes back out as the bytes that came in. Nothing here knows what
	// they mean, and a node with none — every node from before the column, and
	// every node a splice admitted without one — reads back as nil.
	if spec != "" {
		node.Spec = json.RawMessage(spec)
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
//
// IT DOES NOT RETURN LEAF TRANSCRIPTS, and that exclusion is deliberate. Every
// caller of this function derives a DECISION from the journal — trial verdicts,
// reversal rates, measured capacity, what the resident learned, what happened
// while the machine slept — and four of them read the whole journal from
// sequence one to do it. A leaf's transcript is bulk evidence rather than a
// decision: it is journaled here so a rebuild can put it back (see
// [Store.Rebuild], which reads the events table directly and therefore still
// sees every one of them), it is bounded per execution, and it is still far
// larger than every decision event in the database put together. Handing it to
// a full-journal scan would make each of those readers page a megabyte of tool
// output per leaf to answer a question about none of it. Its own reader is
// [Store.TranscriptFor].
func (s *Store) Events(afterSeq int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, kind, payload
		FROM events WHERE seq > ? AND kind <> ? ORDER BY seq LIMIT ?`,
		afterSeq, string(EventTranscriptRecorded), limit)
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
	if err := s.queryRowPrepared(`SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&seq); err != nil {
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
//
// It leaves out leaf transcripts for the same reason [Store.Events] does, and it
// matters more here rather than less: both of this function's callers are
// summarising what happened to a person, and a worker's tool output is not what
// happened, it is how.
func (s *Store) EventsThrough(afterSeq, throughSeq int64) ([]Event, error) {
	result := make([]Event, 0)
	if throughSeq <= afterSeq {
		return result, nil
	}
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, kind, payload
		FROM events WHERE seq > ? AND seq <= ? AND kind <> ? ORDER BY seq`,
		afterSeq, throughSeq, string(EventTranscriptRecorded))
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
// who produced it, the bounded digest of what it said, the files it left
// behind, and — when the digest is not the whole of it — a handle that opens
// the whole of it.
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
	// Handle is the absolute path of a file holding this dependency's complete
	// text, and it is set exactly when the digest above is short of it.
	//
	// It was written on the belief that pushing was what cost 37× — that a
	// window-sized pot had been handing every join every upstream result in
	// full. That belief was wrong, and the measurement says so: the join that
	// billed 163k tokens was handed 2.3 KB, and this field was never once set in
	// either benchmark run, because a fan-in of one-sentence summaries is never
	// large enough to overrun anything. What actually cost was the opposite, the
	// pulling: a consumer handed paths instead of results went and read them.
	//
	// So the digest carries the product now, and this is what remains true of
	// the handle: a fan-in genuinely too large for its reader is clipped, and
	// every withheld byte is one ordinary path away instead of gone. It is the
	// exception it was always meant to be rather than the rule it silently was.
	//
	// The path is content-addressed, so the same settled result yields the same
	// handle on every read: a prompt prefix built from these does not move
	// underneath a cache that is counting on it not moving.
	Handle string
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

// minDependencyBytes is the absolute floor under one dependency's share. Below
// roughly this much a digest is a stub rather than a summary, so a fan-in wide
// enough to push every share under it takes fewer, fuller inputs instead of a
// hundred unreadable fragments — and says so.
const minDependencyBytes = 512

// dependencyFloor is the least one dependency may be given out of a pot of this
// size. It scales with the pot because "a stub rather than a summary" is a
// judgment relative to the window doing the reading: a leaf whose model holds
// 200k tokens, handed sixty 512-byte fragments, has been starved by a number
// that was chosen for a 4 KiB pot and never revisited.
//
// A sixty-fourth is the ratio the old pair already had — 4096/64 is 512 — so a
// caller that could not size its pot from a real window keeps exactly the floor
// it always had, and a caller that could buys fuller inputs rather than only
// more of them.
func dependencyFloor(pot int) int {
	if floor := pot / 64; floor > minDependencyBytes {
		return floor
	}
	return minDependencyBytes
}

// There was a dependencyCeiling here: a flat MaxDigestBytes past which no one
// dependency could take more of the prompt, however large the pot was. It was
// written to close the opposite hole to the one this file has now — a pot sized
// from a 1M-token window handed every consumer every upstream summary whole —
// and it is gone, deliberately, because its premise was measured and found
// false and because it is exactly what defeats a product-carrying edge.
//
// False premise first. The join that was supposed to have been handed "every
// upstream result in full" was handed 2.3 KB; its 163k tokens were eleven turns
// of accumulated transcript, not one fat prompt. The machinery the ceiling
// installed — clip, spill, handle — never engaged in either benchmark run: the
// content-addressed store was empty in both, because a fan-in of press releases
// is never large enough to bite a window-sized pot.
//
// And a flat ceiling is the wrong shape now. With the product on the edge, four
// producers of a 7 KB report each are 28 KB of material that fits in the pot of
// any modern window with room to spare — and a 4 KiB per-dependency cap would
// clip all four and hand back four file handles, which is the archaeology this
// whole change exists to end, reintroduced by a constant.
//
// What bounds one dependency now is the pot and the two-pass share below: every
// dependency is given an equal share first, and only genuinely unspent share is
// handed on to whoever is still clipped. A verbose producer therefore cannot
// take a terse sibling's room — which is the property the ceiling was reached
// for — and when the material fits, it arrives.

// potForMaterial bounds a window-derived pot by the material actually on the
// edges. The window says what the consumer can hold; this says what there is to
// hold, and the smaller of the two is the budget.
//
// It changes no byte of what is pushed — the allotment below already gives each
// dependency min(its share, what it wrote) — and that is precisely why it is
// worth stating. A pot of 1.1 MB over 28 KB of material is not a budget, it is a
// number nobody has looked at since the window was consulted, and every judgment
// made from it is made at a scale the run does not have: how many inputs are
// worth carrying, what counts as a fragment rather than a summary, whether
// anything needed spilling. Sized from the material, those judgments are made
// against the run that is actually happening.
//
// It is applied here, over the assembled lines, rather than by the caller over a
// measurement, because here the number is exact. An estimate that lands one byte
// under the material would clip and spill the largest producer for no reason and
// announce it to the consumer as though something had genuinely not fit.
func potForMaterial(window int, lines []int) int {
	material := 0
	for _, length := range lines {
		material += length
	}
	if material <= 0 || material >= window {
		return window
	}
	return material
}

// dependencyClipNote is appended to a digest the budget cut short. It exists
// because the alternative is the failure this whole function used to have: a
// synthesis leaf writing a confident report over six of fifty findings, with
// nothing anywhere telling it the other forty-four were ever produced.
//
// With a handle it says more than that it was clipped: it says where the rest
// is. A model told only that text is missing has to guess whether it may act;
// told the file that holds it, the missing text is one tool call away, which is
// the whole difference between a bound and a blindfold.
func dependencyClipNote(handle string) string {
	if handle == "" {
		return "\n[clipped to fit — the full text is in this step's own record and its files]"
	}
	return "\n[clipped to fit — the complete result is the file " + handle +
		"; read it if your work needs every byte of it]"
}

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
//
// The pot itself is the caller's to name, and it must be sized from the window
// of the model that will read these inputs — see ctxbudget. MaxDigestBytes is
// what a caller passes when nothing can say how big that window is; it is a
// fallback, not a ceiling, and a caller that knows better must not use it.
//
// What every input carries is as much of the producer's product as its share
// buys, and a handle whenever that was not all of it — a path to a file holding
// the whole. The consumer decides what to open. That is the whole policy: the
// pot is the push, the handle is the pull, and a fan-in costs what its consumer
// reads rather than what its producers wrote.
//
// This is the shape for the node that has to WORK from what fed it, and the
// files a producer left are read back into its digest. For the reader that is
// deciding a shape rather than doing the work, see DependencyAccounts.
func (s *Store) DependencyInputs(id string, maxBytes int) ([]DependencyInput, error) {
	return s.dependencyInputs(id, maxBytes, true)
}

// DependencyAccounts is DependencyInputs for a reader that needs to know what
// exists rather than to hold it: a sub-planner deciding how to divide a claimed
// node, an audit, anything whose next act is a judgment about shape.
//
// It carries the producers' own accounts of their work — which is what this edge
// carried for everyone until the consumers that have to work from it were told
// apart from the consumers that do not. The files still travel, so a reader that
// turns out to need a byte of the material has the path; it simply is not handed
// eight reports to decide that three parts are really four.
func (s *Store) DependencyAccounts(id string, maxBytes int) ([]DependencyInput, error) {
	return s.dependencyInputs(id, maxBytes, false)
}

func (s *Store) dependencyInputs(id string, maxBytes int, product bool) ([]DependencyInput, error) {
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
		files   []string
	}
	settled := make([]produced, 0)
	// One file is inlined once for one consumer, however many producers name it.
	// Two panelists that both cite the shared spec used to hand the join two
	// copies of it and pay twice.
	inlined := make(map[string]bool)
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
		// A producer's files are its files whether it finished or failed, and
		// this is the only durable record of either: the executor's path→node
		// map lives in the worker's memory and dies with it.
		files := summaryPaths(summary + "\n" + failure)
		// And here the edge stops carrying the announcement and starts carrying
		// the work. A node that recorded files answered by writing them, so the
		// files are the answer; a node that recorded none answered in its final
		// message, so the message is the answer and the line above is already
		// all of it. That is the whole test, and it is a fact about the record
		// rather than a reading of the prose.
		if product {
			line += readProduct(files, maxBytes, inlined)
		}
		settled = append(settled, produced{
			id: dependencyID, line: line, summary: summary, failure: failure, files: files})
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
	//
	// This question is asked of the window's pot and not of the material's,
	// because it is a question about the consumer: "would this dependency's
	// share be too small to read?" is answerable only against the room the
	// reader has. Asked of the material instead, six short results totalling 90
	// bytes would divide into 45-byte shares, fall under the floor, and be
	// declared a fan-in too wide to carry — five of them dropped for not fitting
	// inside themselves.
	pot := maxBytes
	floor := dependencyFloor(pot)
	carried := len(settled)
	if share := pot / carried; share < floor {
		carried = pot / floor
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

	// And now the second bound, over the inputs that survived the first: the pot
	// is the smaller of what the consumer can hold and what its producers
	// actually wrote. See potForMaterial.
	lengths := make([]int, 0, len(settled))
	for _, dependency := range settled {
		lengths = append(lengths, len(dependency.line))
	}
	pot = potForMaterial(pot, lengths)

	// Two passes so a short digest's unspent share reaches a long one: the first
	// gives each only what it needs up to an equal share, the second hands the
	// unspent remainder to whoever is still clipped. There is no third bound on
	// one input — see the note where the flat ceiling used to be — because these
	// two already say the thing the ceiling was reached for: nothing a verbose
	// producer takes was ever a terse sibling's.
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
		handle := ""
		if len(dependency.line) > allotted[index] {
			// Withheld bytes are moved, never dropped. The handle is written
			// before the note that names it, because the note has to carry the
			// path and the path has to be inside the allotment with it.
			handle = s.spillDependency(dependency.line)
			note := dependencyClipNote(handle)
			// The marker is budgeted inside the allotment, not added on top of
			// it: a truncation that overflows the bound it is announcing is the
			// same lie in a different direction.
			line = bounded(dependency.line, allotted[index]-len(note)) + note
			if len(line) > allotted[index] {
				line = bounded(dependency.line, allotted[index])
			}
		}
		inputs = append(inputs, DependencyInput{
			NodeID: dependency.id, Digest: line, Handle: handle,
			// A failed step's files are as real as a finished one's, and the
			// only place a failure records them is its error text.
			Artifacts: dependency.files,
		})
	}
	if overflow != "" {
		inputs = append(inputs, DependencyInput{Digest: overflow})
	}
	return inputs, nil
}

// spillDependency writes one dependency's complete text to the content-addressed
// store and returns the ordinary filesystem path that holds it.
//
// It is the mechanism a large fold already uses, deliberately in the same shape
// — see prepareFold, whose comment says why: a plain path, not a scheme, so the
// consumer opens it with the tool it already has for artifacts and nothing new
// has to be taught to anyone.
//
// Content addressing is what makes it safe to call on a read path. The same
// settled result spills to the same path however many consumers ask for it, so
// the writes de-duplicate to one and the prompt built from them does not move
// between two reads of the same fan-in.
//
// A spill that fails returns the empty string, and the digest falls back to the
// note it always carried. Losing the route to the full text is a smaller failure
// than refusing to hand a consumer the inputs it can still be given.
func (s *Store) spillDependency(text string) string {
	if s.blobs == nil {
		return ""
	}
	ref, err := s.blobs.PutBytes([]byte(text))
	if err != nil {
		return ""
	}
	path, err := s.blobs.Path(ref)
	if err != nil {
		return ""
	}
	return path
}

// DependencyFanIn is what actually landed into one node: how many settled hard
// dependencies it has, and how many bytes of result text they wrote between
// them. All of it — not the share DependencyInputs carries into the prompt.
//
// It is a measurement and not an estimate, and it exists because a gathering
// node's budget has to be one. A join sized from the flat leaf defaults is
// sized for a leaf that gathers nothing, which is how an assembler ran out of
// room mid-assembly and had to be bought a continuation to finish typing what
// it had already read.
type DependencyFanIn struct {
	Count int
	Bytes int
}

// DependencyFanIn measures the settled hard dependencies of one node. It is
// read once, at claim time, by a caller that then holds the two numbers fixed
// for the life of the worker: a budget that moved between turns would move the
// prompt prefix with it.
//
// The measurement counts the product and not the announcement of it. A summary
// reading "The evaluation is complete. The file is at /w/job/07-vendors.md" is
// 61 bytes and stands for 7 KB, so a fan-in of four such nodes measured 244
// bytes and sized its consumer's completion reserve, turn grant and token grant
// for 244 bytes of assembly. Every one of those numbers is arithmetic over this
// one, which is why this one has to be about the work.
func (s *Store) DependencyFanIn(id string) (DependencyFanIn, error) {
	rows, err := s.db.Query(`
		SELECT dependency.summary, dependency.error
		FROM edges AS edge
		JOIN nodes AS dependency ON dependency.id = edge.from_id
		WHERE edge.to_id = ? AND edge.kind IN (?, ?)
		  AND dependency.status IN (?, ?, ?)`,
		id, FeedsInto, Blocks, Done, Failed, Cancelled)
	if err != nil {
		return DependencyFanIn{}, fmt.Errorf("measure the fan-in of %q: %w", id, err)
	}
	defer rows.Close()
	var fanIn DependencyFanIn
	// One file counted once, matching what the fan-in will actually carry: two
	// producers naming the same file hand their consumer one copy of it.
	counted := make(map[string]bool)
	for rows.Next() {
		var summary, failure string
		if err := rows.Scan(&summary, &failure); err != nil {
			return DependencyFanIn{}, fmt.Errorf("measure the fan-in of %q: %w", id, err)
		}
		fanIn.Count++
		fanIn.Bytes += len(summary) + len(failure) +
			productBytes(summaryPaths(summary+"\n"+failure), counted)
	}
	if err := rows.Err(); err != nil {
		return DependencyFanIn{}, fmt.Errorf("measure the fan-in of %q: %w", id, err)
	}
	return fanIn, nil
}

// SurpriseFor returns the prediction residual recorded against one node, if one
// was ever recorded. It is the read that did not exist: surprise was written to
// the journal and to an analytics table, then only ever summed, averaged and
// rendered as a percentage on a receipt somebody might read afterwards.
//
// A number nothing consults at a decision point is decoration. This is the
// route by which the machinery that decides whether to retry, escalate or
// continue a node can be told that the node already cost 2.85× what its own
// profile predicted — which is a fact about the work, and belongs in front of
// the judge that is about to buy more of it.
func (s *Store) SurpriseFor(nodeID string) (NodeSurprise, bool, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return NodeSurprise{}, false, nil
	}
	surprise := NodeSurprise{NodeID: nodeID}
	err := s.db.QueryRow(`
		SELECT actual_tokens, expected_tokens, surprise FROM surprises WHERE node_id = ?`,
		nodeID).Scan(&surprise.ActualTokens, &surprise.ExpectedTokens, &surprise.Surprise)
	if errors.Is(err, sql.ErrNoRows) {
		return NodeSurprise{}, false, nil
	}
	if err != nil {
		return NodeSurprise{}, false, fmt.Errorf("read the surprise recorded for %q: %w", nodeID, err)
	}
	return surprise, true, nil
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
		// A cancelled node that got somewhere before it was stopped is not the
		// same input as one that never ran, and saying "(not run)" over a
		// half-written report is how a restart retyped work that was already on
		// disk. The label says where the work stopped; the partial itself is the
		// digest, budgeted and clipped exactly as a finished node's is.
		if partial := strings.TrimSpace(summary); partial != "" {
			return id + " (cancelled midway): " + partial
		}
		if strings.TrimSpace(failure) == "" {
			failure = "no reason recorded"
		}
		return id + " (not run): " + failure
	default:
		return ""
	}
}
