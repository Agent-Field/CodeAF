package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// TerritoryGroup marks the self-authored organizational nodes that pack
// settled top-level jobs one level above their ordinary folds.
const TerritoryGroup = "territory"

// CharterGroup deliberately shares the existing organizational group value.
// The TUI's territory-family filter therefore keeps charter furniture out of
// ordinary job cards without acquiring charter-specific surface code.
const CharterGroup = TerritoryGroup

// IsOrganizationalGroup identifies durable spine furniture rather than work
// a runner may claim. New organizational node kinds join this one family.
func IsOrganizationalGroup(group string) bool {
	return group == TerritoryGroup
}

const territoryOwner = "territory-retrospective"

// TerritoryJob is one folded job plus the local signals the retrospective
// uses to decide whether several jobs belong to the same territory.
type TerritoryJob struct {
	Node          Node
	Workspace     string
	DominantScope string
	Continuity    []string
}

// TerritoryJobs returns ordinary job fold roots, including roots already
// nested directly under a territory. Scope and continuity are derived from
// journal-built views; workspace is derived from the fold's durable pointers.
func (s *Store) TerritoryJobs() ([]TerritoryJob, error) {
	nodes, err := s.Nodes()
	if err != nil {
		return nil, err
	}
	edges, err := s.Edges()
	if err != nil {
		return nil, err
	}

	territories := make(map[string]bool)
	for _, node := range nodes {
		if node.Group == TerritoryGroup {
			territories[node.ID] = true
		}
	}
	jobs := make([]TerritoryJob, 0)
	jobIndex := make(map[string]int)
	for _, node := range nodes {
		if !node.FoldRoot || node.Group == TerritoryGroup {
			continue
		}
		if node.Parent != RootID && !territories[node.Parent] {
			continue
		}
		jobIndex[node.ID] = len(jobs)
		jobs = append(jobs, TerritoryJob{
			Node:      node,
			Workspace: territoryWorkspace(node.FoldPointers),
		})
	}
	if len(jobs) == 0 {
		return jobs, nil
	}

	children := make(map[string][]string)
	for _, node := range nodes {
		children[node.Parent] = append(children[node.Parent], node.ID)
	}
	owner := make(map[string]string)
	for _, job := range jobs {
		stack := []string{job.Node.ID}
		for len(stack) > 0 {
			last := len(stack) - 1
			id := stack[last]
			stack = stack[:last]
			if _, assigned := owner[id]; assigned {
				continue
			}
			owner[id] = job.Node.ID
			stack = append(stack, children[id]...)
		}
	}

	scopeCounts := make(map[string]map[string]int)
	rows, err := s.db.Query(`SELECT node_id, scope FROM facts WHERE status = ?`, FactActive)
	if err != nil {
		return nil, fmt.Errorf("territory fact scopes: %w", err)
	}
	for rows.Next() {
		var nodeID, scope string
		if err := rows.Scan(&nodeID, &scope); err != nil {
			rows.Close()
			return nil, fmt.Errorf("territory fact scopes: %w", err)
		}
		jobID := owner[nodeID]
		if jobID == "" {
			continue
		}
		if scopeCounts[jobID] == nil {
			scopeCounts[jobID] = make(map[string]int)
		}
		scopeCounts[jobID][scope]++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("territory fact scopes: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("territory fact scopes: %w", err)
	}
	for jobID, counts := range scopeCounts {
		index := jobIndex[jobID]
		jobs[index].DominantScope = dominantSignal(counts)
	}

	continuity := make(map[string]map[string]bool)
	for _, edge := range edges {
		if edge.Kind != FeedsInto {
			continue
		}
		from, to := owner[edge.From], owner[edge.To]
		if from == "" || to == "" || from == to {
			continue
		}
		if continuity[from] == nil {
			continuity[from] = make(map[string]bool)
		}
		if continuity[to] == nil {
			continuity[to] = make(map[string]bool)
		}
		continuity[from][to] = true
		continuity[to][from] = true
	}
	for jobID, linked := range continuity {
		index := jobIndex[jobID]
		for other := range linked {
			jobs[index].Continuity = append(jobs[index].Continuity, other)
		}
		sort.Strings(jobs[index].Continuity)
	}
	return jobs, nil
}

func dominantSignal(counts map[string]int) string {
	best, bestCount := "", 0
	for signal, count := range counts {
		if count > bestCount || count == bestCount && (best == "" || signal < best) {
			best, bestCount = signal, count
		}
	}
	return best
}

func territoryWorkspace(pointers []string) string {
	counts := make(map[string]int)
	for _, pointer := range pointers {
		if !filepath.IsAbs(pointer) {
			continue
		}
		counts[filepath.Dir(filepath.Clean(pointer))]++
	}
	return dominantSignal(counts)
}

// FormTerritory atomically journals an organizational splice, its lifecycle,
// every member re-parent, and the enclosing fold. The digest is prepared
// before the transaction so a CAS spill cannot leave a partial event batch.
func (s *Store) FormTerritory(id, title, digest string, pointers, members []string) error {
	id, title = strings.TrimSpace(id), strings.TrimSpace(title)
	members = uniqueStrings(members)
	if id == "" || title == "" || len(members) == 0 {
		return fmt.Errorf("form territory: %w: id, title, and members are required", ErrInvalid)
	}
	payload, err := normalizeSubtree(RootID, Subtree{Nodes: []NodeSpec{{
		ID: id, Brief: "Territory: " + title, Title: title, Group: TerritoryGroup,
	}}}, Provenance{Origin: OriginSelf, Intent: "Territory: " + title})
	if err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	fold, encodedPointers, err := s.prepareFold(digest, pointers)
	if err != nil {
		return fmt.Errorf("form territory: %w", err)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	defer tx.Rollback()

	var rootStatus Status
	var rootFolded bool
	if err := tx.QueryRow(`SELECT status, folded FROM nodes WHERE id = ?`, RootID).
		Scan(&rootStatus, &rootFolded); err != nil {
		return fmt.Errorf("form territory: read spine: %w", err)
	}
	if terminal(rootStatus) || rootFolded {
		return fmt.Errorf("form territory: %w: spine is closed", ErrInvalid)
	}
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ?`, id).Scan(&exists); err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	if exists != 0 {
		return fmt.Errorf("form territory: %w: node %q already exists", ErrInvalid, id)
	}
	for _, member := range members {
		if err := requireTerritoryMember(tx, member); err != nil {
			return fmt.Errorf("form territory: %w", err)
		}
	}

	spliceSeq, _, err := appendEvent(tx, id, EventSubtreeSpliced, payload)
	if err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	if err := applySpliceView(tx, payload, spliceSeq); err != nil {
		return fmt.Errorf("form territory: materialize splice: %w", err)
	}
	claim := claimPayload{Owner: territoryOwner, Token: 1}
	claimSeq, _, err := appendEvent(tx, id, EventNodeClaimed, claim)
	if err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	if err := replayUpdate(tx, id, `
		UPDATE nodes
		SET status = ?, owner = ?, claim_token = ?, attempt = attempt + 1, updated_seq = ?
		WHERE id = ?`, Claimed, claim.Owner, claim.Token, claimSeq, id); err != nil {
		return fmt.Errorf("form territory: materialize claim: %w", err)
	}
	for _, member := range members {
		reparent := nodeReparentedPayload{Parent: id}
		seq, _, err := appendEvent(tx, member, EventNodeReparented, reparent)
		if err != nil {
			return fmt.Errorf("form territory: %w", err)
		}
		if err := applyNodeReparentedView(tx, member, id, seq); err != nil {
			return fmt.Errorf("form territory: re-parent %q: %w", member, err)
		}
	}
	completion := completePayload{Owner: territoryOwner, Token: 1, Summary: fold.Digest}
	completionSeq, completedAt, err := appendEvent(tx, id, EventNodeCompleted, completion)
	if err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	if err := replayUpdate(tx, id, `
		UPDATE nodes
		SET status = ?, owner = ?, claim_token = ?, summary = ?, finished_at = ?, updated_seq = ?
		WHERE id = ?`, Done, completion.Owner, completion.Token, completion.Summary,
		formatTime(completedAt), completionSeq, id); err != nil {
		return fmt.Errorf("form territory: materialize completion: %w", err)
	}
	if err := refreshGraphFTS(tx, id); err != nil {
		return fmt.Errorf("form territory: index completion: %w", err)
	}
	foldSeq, _, err := appendEvent(tx, id, EventSubtreeFolded, fold)
	if err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	if err := applyFoldView(tx, id, fold.Digest, encodedPointers, foldSeq); err != nil {
		return fmt.Errorf("form territory: materialize fold: %w", err)
	}
	if err := recordSelfReceipt(tx, id); err != nil {
		return fmt.Errorf("form territory: receipt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("form territory: %w", err)
	}
	return nil
}

// GrowTerritory adds one later job fold and refreshes the enclosing digest in
// the same event transaction. Existing member fold roots remain searchable.
func (s *Store) GrowTerritory(territoryID, memberID, digest string, pointers []string) error {
	territoryID, memberID = strings.TrimSpace(territoryID), strings.TrimSpace(memberID)
	if territoryID == "" || memberID == "" || territoryID == memberID {
		return fmt.Errorf("grow territory: %w: distinct territory and member are required", ErrInvalid)
	}
	fold, encodedPointers, err := s.prepareFold(digest, pointers)
	if err != nil {
		return fmt.Errorf("grow territory: %w", err)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("grow territory: %w", err)
	}
	defer tx.Rollback()

	var parent sql.NullString
	var group string
	var status Status
	var foldRoot bool
	if err := tx.QueryRow(`SELECT parent_id, grp, status, fold_root FROM nodes WHERE id = ?`, territoryID).
		Scan(&parent, &group, &status, &foldRoot); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("grow territory: %w: unknown territory %q", ErrNotFound, territoryID)
		}
		return fmt.Errorf("grow territory: %w", err)
	}
	if !parent.Valid || parent.String != RootID || group != TerritoryGroup || !foldRoot || !terminal(status) {
		return fmt.Errorf("grow territory: %w: %q is not a settled top-level territory fold", ErrInvalid, territoryID)
	}
	if err := requireTerritoryMember(tx, memberID); err != nil {
		return fmt.Errorf("grow territory: %w", err)
	}
	reparent := nodeReparentedPayload{Parent: territoryID}
	reparentSeq, _, err := appendEvent(tx, memberID, EventNodeReparented, reparent)
	if err != nil {
		return fmt.Errorf("grow territory: %w", err)
	}
	if err := applyNodeReparentedView(tx, memberID, territoryID, reparentSeq); err != nil {
		return fmt.Errorf("grow territory: re-parent %q: %w", memberID, err)
	}
	foldSeq, _, err := appendEvent(tx, territoryID, EventSubtreeFolded, fold)
	if err != nil {
		return fmt.Errorf("grow territory: %w", err)
	}
	if err := applyFoldView(tx, territoryID, fold.Digest, encodedPointers, foldSeq); err != nil {
		return fmt.Errorf("grow territory: materialize fold: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("grow territory: %w", err)
	}
	return nil
}

func requireTerritoryMember(tx *sql.Tx, memberID string) error {
	var parent sql.NullString
	var status Status
	var foldRoot bool
	err := tx.QueryRow(`SELECT parent_id, status, fold_root FROM nodes WHERE id = ?`, memberID).
		Scan(&parent, &status, &foldRoot)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: unknown member %q", ErrNotFound, memberID)
	}
	if err != nil {
		return err
	}
	if !parent.Valid || parent.String != RootID || !foldRoot || !terminal(status) {
		return fmt.Errorf("%w: member %q is not a settled top-level fold root", ErrInvalid, memberID)
	}
	return nil
}
