package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type splicedPayload struct {
	Parent     string     `json:"parent"`
	Root       string     `json:"root"`
	Nodes      []NodeSpec `json:"nodes"`
	Provenance Provenance `json:"provenance"`
}

// Splice atomically admits a complete subtree below parent. Validation and the
// event/view writes share one immediate transaction, so another process sees
// either the complete admission or none of it.
func (s *Store) Splice(parent string, subtree Subtree, provenance Provenance) error {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return fmt.Errorf("splice: %w: empty parent", ErrInvalid)
	}
	payload, err := normalizeSubtree(parent, subtree, provenance)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("splice: %w", err)
	}
	defer tx.Rollback()

	var parentStatus Status
	var parentFolded bool
	if err := tx.QueryRow(`SELECT status, folded FROM nodes WHERE id = ?`, parent).Scan(&parentStatus, &parentFolded); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("splice parent %q: %w", parent, ErrNotFound)
		}
		return fmt.Errorf("read splice parent %q: %w", parent, err)
	}
	if terminal(parentStatus) || parentFolded {
		return fmt.Errorf("splice parent %q is closed (%s): %w", parent, parentStatus, ErrInvalid)
	}
	if payload.Provenance.TrialOf > 0 {
		var kind FactKind
		var status string
		if err := tx.QueryRow(`SELECT kind, status FROM facts WHERE seq = ?`, payload.Provenance.TrialOf).Scan(&kind, &status); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("splice trial fact #%d: %w", payload.Provenance.TrialOf, ErrNotFound)
			}
			return fmt.Errorf("read trial fact #%d: %w", payload.Provenance.TrialOf, err)
		}
		if kind != FactUnsettled || status != FactActive {
			return fmt.Errorf("splice trial fact #%d is %s/%s: %w", payload.Provenance.TrialOf, kind, status, ErrInvalid)
		}
	}

	batch := make(map[string]struct{}, len(payload.Nodes))
	admitted := make([]string, 0, len(payload.Nodes))
	for _, node := range payload.Nodes {
		batch[node.ID] = struct{}{}
		admitted = append(admitted, node.ID)
	}
	present, err := existingNodeIDs(tx, admitted)
	if err != nil {
		return fmt.Errorf("check nodes: %w", err)
	}
	for _, node := range payload.Nodes {
		if present[node.ID] {
			return fmt.Errorf("splice node %q already exists: %w", node.ID, ErrInvalid)
		}
	}
	var needed []string
	for _, node := range payload.Nodes {
		for _, need := range node.Needs {
			if _, inside := batch[need.NodeID]; inside {
				continue
			}
			needed = append(needed, need.NodeID)
		}
	}
	dependencies, err := existingNodeIDs(tx, needed)
	if err != nil {
		return fmt.Errorf("check dependencies: %w", err)
	}
	for _, node := range payload.Nodes {
		for _, need := range node.Needs {
			if _, inside := batch[need.NodeID]; inside {
				continue
			}
			if !dependencies[need.NodeID] {
				return fmt.Errorf("node %q needs unknown node %q: %w", node.ID, need.NodeID, ErrInvalid)
			}
		}
	}

	seq, _, err := appendEvent(tx, payload.Root, EventSubtreeSpliced, payload)
	if err != nil {
		return fmt.Errorf("splice: %w", err)
	}
	if err := applySpliceView(tx, payload, seq); err != nil {
		return fmt.Errorf("materialize splice: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("splice: %w", err)
	}
	return nil
}

// existingNodeIDs answers admission's membership question once per batch. The
// per-node COUNT(*) it replaces ran inside the write transaction, so every
// extra round trip was held against every other writer.
func existingNodeIDs(tx *sql.Tx, ids []string) (map[string]bool, error) {
	present := make(map[string]bool, len(ids))
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		end := min(start+chunk, len(ids))
		placeholders := make([]string, 0, end-start)
		args := make([]any, 0, end-start)
		for _, id := range ids[start:end] {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		rows, err := tx.Query(`SELECT id FROM nodes WHERE id IN (`+
			strings.Join(placeholders, ", ")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			present[id] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return present, nil
}

func normalizeSubtree(parent string, subtree Subtree, provenance Provenance) (splicedPayload, error) {
	if !validOrigin(provenance.Origin) {
		return splicedPayload{}, fmt.Errorf("splice: %w: unknown origin %q", ErrInvalid, provenance.Origin)
	}
	if strings.TrimSpace(provenance.Intent) == "" {
		return splicedPayload{}, fmt.Errorf("splice: %w: empty verbatim intent", ErrInvalid)
	}
	if provenance.TrialOf < 0 {
		return splicedPayload{}, fmt.Errorf("splice: %w: negative trial fact sequence", ErrInvalid)
	}
	if provenance.RetryOf == RootID {
		return splicedPayload{}, fmt.Errorf("splice: %w: the permanent spine cannot be a retry predecessor", ErrInvalid)
	}
	if provenance.Origin == OriginUser && strings.TrimSpace(provenance.CharterID) != "" {
		return splicedPayload{}, fmt.Errorf("splice: %w: charter pointer requires trigger or self origin", ErrInvalid)
	}
	if len(subtree.Nodes) == 0 {
		return splicedPayload{}, fmt.Errorf("splice: %w: empty subtree", ErrInvalid)
	}

	nodes := make([]NodeSpec, 0, len(subtree.Nodes))
	byID := make(map[string]NodeSpec, len(subtree.Nodes))
	root := ""
	for _, raw := range subtree.Nodes {
		node := raw
		node.ID = strings.TrimSpace(node.ID)
		node.Parent = strings.TrimSpace(node.Parent)
		if node.ID == "" || node.ID == RootID || strings.TrimSpace(node.Brief) == "" {
			return splicedPayload{}, fmt.Errorf("splice: %w: node requires a non-root id and brief", ErrInvalid)
		}
		if node.Stage < 0 {
			return splicedPayload{}, fmt.Errorf("splice node %q: %w: negative stage", node.ID, ErrInvalid)
		}
		if _, duplicate := byID[node.ID]; duplicate {
			return splicedPayload{}, fmt.Errorf("splice node %q is duplicated: %w", node.ID, ErrInvalid)
		}
		if node.Parent == "" {
			if root != "" {
				return splicedPayload{}, fmt.Errorf("splice: %w: roots %q and %q", ErrInvalid, root, node.ID)
			}
			root = node.ID
		}
		node.Brief = bounded(node.Brief, MaxDigestBytes)
		node.Needs = normalizeNeeds(node.ID, node.Needs)
		for _, need := range node.Needs {
			if !validEdgeKind(need.Kind) {
				return splicedPayload{}, fmt.Errorf("node %q has unknown edge kind %q: %w", node.ID, need.Kind, ErrInvalid)
			}
			if need.NodeID == node.ID {
				return splicedPayload{}, fmt.Errorf("node %q depends on itself: %w", node.ID, ErrInvalid)
			}
			// The permanent spine is Running by construction — it is the trunk
			// every job splices under, not work that anybody finishes. A hard
			// dependency on it is therefore a wait with no end: Ready excludes
			// any node whose dependency is not done, failed or cancelled, and
			// the root is none of those and never will be.
			//
			// This is not hypothetical. A compiler answered builds_on:["root"],
			// continuity wired the edge because a node by that name existed, and
			// the job was deadlocked from the instant it was created: compiled
			// cleanly, spliced cleanly, pending with an empty started_at for the
			// full 900-second ceiling while the resident ticked beside it once a
			// second with nothing it was allowed to claim. The refusal belongs
			// here, beside the identical one RetryOf already gets, because the
			// store is the only layer that knows why the root is Running.
			if need.NodeID == RootID {
				return splicedPayload{}, fmt.Errorf(
					"node %q depends on the permanent spine, which never settles: %w", node.ID, ErrInvalid)
			}
		}
		byID[node.ID] = node
		nodes = append(nodes, node)
	}
	if root == "" {
		return splicedPayload{}, fmt.Errorf("splice: %w: subtree has no root", ErrInvalid)
	}

	for _, node := range nodes {
		if node.Parent == "" {
			continue
		}
		if _, ok := byID[node.Parent]; !ok {
			return splicedPayload{}, fmt.Errorf("node %q has parent %q outside the subtree: %w", node.ID, node.Parent, ErrInvalid)
		}
		seen := map[string]bool{node.ID: true}
		ancestor := node.Parent
		for ancestor != "" {
			if seen[ancestor] {
				return splicedPayload{}, fmt.Errorf("parent cycle at node %q: %w", node.ID, ErrInvalid)
			}
			seen[ancestor] = true
			ancestor = byID[ancestor].Parent
		}
	}
	if dependencyCycle(nodes) {
		return splicedPayload{}, fmt.Errorf("splice dependency cycle: %w", ErrInvalid)
	}

	return splicedPayload{
		Parent:     parent,
		Root:       root,
		Nodes:      nodes,
		Provenance: provenance,
	}, nil
}

func normalizeNeeds(nodeID string, needs []Need) []Need {
	seen := make(map[Need]struct{}, len(needs))
	result := make([]Need, 0, len(needs))
	for _, raw := range needs {
		need := Need{NodeID: strings.TrimSpace(raw.NodeID), Kind: raw.Kind}
		if need.NodeID == "" {
			continue
		}
		if _, duplicate := seen[need]; duplicate {
			continue
		}
		seen[need] = struct{}{}
		result = append(result, need)
	}
	return result
}

// dependencyCycle checks only scheduling edges. Suggests is allowed to point
// freely because it neither blocks nor establishes execution order.
func dependencyCycle(nodes []NodeSpec) bool {
	inside := make(map[string]bool, len(nodes))
	dependencies := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		inside[node.ID] = true
	}
	for _, node := range nodes {
		for _, need := range node.Needs {
			if need.Kind != Suggests && inside[need.NodeID] {
				dependencies[node.ID] = append(dependencies[node.ID], need.NodeID)
			}
		}
	}
	state := make(map[string]uint8, len(nodes))
	var visit func(string) bool
	visit = func(id string) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, dependency := range dependencies[id] {
			if visit(dependency) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range inside {
		if visit(id) {
			return true
		}
	}
	return false
}

func applySpliceView(tx *sql.Tx, payload splicedPayload, seq int64) error {
	ordered, err := parentFirst(payload.Nodes)
	if err != nil {
		return err
	}
	orderByID := make(map[string]int, len(payload.Nodes))
	attachments, err := json.Marshal(payload.Provenance.Attachments)
	if err != nil {
		return err
	}
	for index, node := range payload.Nodes {
		orderByID[node.ID] = index + 1
	}
	for _, node := range ordered {
		parent := node.Parent
		if parent == "" {
			parent = payload.Parent
		}
		// The node's own choice where the plan made one, the splice's otherwise.
		// Resolving it here is what makes inheritance a fact in the row rather
		// than a rule every reader has to remember.
		//
		// "Made one" means made any choice, not made an interesting one. A node
		// the sizing pass judged and sent to the generalist arrives here saying
		// "linear", and that is a choice — it must not be overwritten by the
		// worker the job as a whole was spliced for. Only the empty string is
		// silence, and only silence inherits. This distinction is the whole
		// reason the generalist has a name: while it was spelled as the empty
		// string, every node a coding job's planner deliberately left to the
		// generalist was admitted as a coding-pipeline node instead.
		subharness := strings.TrimSpace(node.Subharness)
		if subharness == "" {
			subharness = strings.TrimSpace(payload.Provenance.Subharness)
		}
		if _, err := tx.Exec(`
			INSERT INTO nodes (
			    id, parent_id, brief, title, grp, stage, status, origin, session_id,
			    intent, charter_id, trial_of, retry_of, service_intent, work_model, plan_model, run_model, craft, subharness, splice_subharness, spec, attachments, created_seq, created_order, updated_seq
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			node.ID, parent, node.Brief, node.Title, node.Group, node.Stage, Pending,
			payload.Provenance.Origin, nullIfEmpty(payload.Provenance.SessionID),
			payload.Provenance.Intent, payload.Provenance.CharterID, payload.Provenance.TrialOf, payload.Provenance.RetryOf, payload.Provenance.ServiceIntent,
			payload.Provenance.WorkModel, payload.Provenance.PlanModel, payload.Provenance.RunModel, payload.Provenance.Craft, subharness, payload.Provenance.Subharness,
			string(node.Spec), string(attachments), seq, orderByID[node.ID], seq); err != nil {
			return fmt.Errorf("insert node %q: %w", node.ID, err)
		}
		if err := refreshGraphFTS(tx, node.ID); err != nil {
			return fmt.Errorf("index node %q: %w", node.ID, err)
		}
	}
	edgeOrder := 0
	for _, node := range payload.Nodes {
		for _, need := range node.Needs {
			edgeOrder++
			if _, err := tx.Exec(`INSERT INTO edges (from_id, to_id, kind, created_seq, created_order) VALUES (?, ?, ?, ?, ?)`,
				need.NodeID, node.ID, need.Kind, seq, edgeOrder); err != nil {
				return fmt.Errorf("insert edge %q -> %q: %w", need.NodeID, node.ID, err)
			}
		}
	}
	return nil
}

func parentFirst(nodes []NodeSpec) ([]NodeSpec, error) {
	remaining := make(map[string]NodeSpec, len(nodes))
	for _, node := range nodes {
		remaining[node.ID] = node
	}
	ordered := make([]NodeSpec, 0, len(nodes))
	inserted := make(map[string]bool, len(nodes))
	for len(remaining) > 0 {
		ids := make([]string, 0, len(remaining))
		for id := range remaining {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		progress := false
		for _, id := range ids {
			node := remaining[id]
			if node.Parent != "" && !inserted[node.Parent] {
				continue
			}
			ordered = append(ordered, node)
			inserted[id] = true
			delete(remaining, id)
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("parent cycle: %w", ErrInvalid)
		}
	}
	return ordered, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
