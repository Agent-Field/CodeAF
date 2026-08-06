package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// graph_fts is a materialized search view rather than an external-content
// table. Keeping the node id beside the indexed fields makes replacement
// explicit, which matters because every replacement must share the journal
// event's transaction and Rebuild must reproduce it by replay.
const graphFTSSchema = `
CREATE VIRTUAL TABLE graph_fts USING fts5(
    node_id UNINDEXED,
    intent,
    summary,
    digest
);
`

// migrateGraphFTS creates the index for stores from before recall existed.
// Existing nodes are already a journal-derived materialized view, so a
// one-time backfill is sufficient; subsequent Rebuilds recreate the index by
// replaying the journal through the ordinary view functions.
func migrateGraphFTS(db *sql.DB) error {
	var exists int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'graph_fts'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(graphFTSSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO graph_fts (node_id, intent, summary, digest)
		SELECT id, intent, summary, fold_digest FROM nodes`); err != nil {
		return err
	}
	return tx.Commit()
}

// refreshGraphFTS replaces one index row from the authoritative node view.
// Callers invoke it before committing the event transaction, so searchable
// memory can never get ahead of or lag behind the event that changed it.
func refreshGraphFTS(tx *sql.Tx, nodeID string) error {
	if _, err := tx.Exec(`DELETE FROM graph_fts WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO graph_fts (node_id, intent, summary, digest)
		SELECT id, intent, summary, fold_digest FROM nodes WHERE id = ?`, nodeID)
	return err
}

// RecallHit is one compact route back into prior work. Digest is the bounded
// map carried in the prompt; Pointers name the territory an executor can read
// when the digest says that memory matters.
type RecallHit struct {
	NodeID   string   `json:"node_id"`
	Intent   string   `json:"intent"`
	Digest   string   `json:"digest"`
	Pointers []string `json:"pointers"`
	Age      string   `json:"age"`
	Score    float64  `json:"score"`
}

// FormatRecall renders hits for a model-facing context. The wording states
// the memory contract once: the digest is enough to orient, while pointers are
// the route to source material when the work needs depth.
func FormatRecall(hits []RecallHit, maxBytes int) string {
	if len(hits) == 0 || maxBytes <= 0 {
		return ""
	}
	var block strings.Builder
	block.WriteString("You have worked here before; here is what was learned and where the details live:\n")
	for _, hit := range hits {
		if block.Len() >= maxBytes {
			break
		}
		fmt.Fprintf(&block, "- %s", strings.TrimSpace(hit.Intent))
		if hit.Age != "" {
			fmt.Fprintf(&block, " (%s)", hit.Age)
		}
		block.WriteByte('\n')
		if digest := strings.TrimSpace(hit.Digest); digest != "" {
			fmt.Fprintf(&block, "  learned: %s\n", digest)
		}
		if len(hit.Pointers) > 0 {
			fmt.Fprintf(&block, "  details: %s\n", strings.Join(hit.Pointers, ", "))
		}
	}
	return bounded(block.String(), maxBytes)
}

type recallCandidate struct {
	RecallHit
	territoryParent bool
	at              time.Time
	updatedSeq      int64
	ftsRank         int
	scopeHits       int
}

// HasFolds reports whether recall can change a headless run. Callers use it as
// the additive boundary: a database containing only the permanent spine must
// not add prompt bytes or tool definitions to the benchmarked path.
func (s *Store) HasFolds() (bool, error) {
	var exists int
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM nodes WHERE fold_root = 1)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check fold history: %w", err)
	}
	return exists != 0, nil
}

// Recall searches folded graph memory using lexical relevance, optional
// workspace/file cues, and recency. FTS is deliberately first: embeddings add
// cost and opacity before there is evidence that local lexical recall fails.
// Hostile FTS input is treated as a miss so memory can specialize a run but
// can never prevent it from starting.
func (s *Store) Recall(terms string, scopeCues []string, limit int) ([]RecallHit, error) {
	if limit <= 0 {
		limit = 6
	}
	if limit > 50 {
		limit = 50
	}
	candidateLimit := limit * 8
	if candidateLimit < 32 {
		candidateLimit = 32
	}
	if candidateLimit > 400 {
		candidateLimit = 400
	}

	byID := make(map[string]*recallCandidate)
	ftsTerms := ftsQueryFrom(terms)
	if ftsTerms != "" {
		rows, err := s.db.Query(`
			SELECT n.id, n.intent,
			       CASE WHEN n.fold_digest <> '' THEN n.fold_digest ELSE n.summary END,
			       n.fold_pointers, e.ts, n.updated_seq
			FROM graph_fts
			JOIN nodes AS n ON n.id = graph_fts.node_id
			JOIN events AS e ON e.seq = n.updated_seq
			WHERE graph_fts MATCH ? AND n.fold_root = 1
			ORDER BY bm25(graph_fts, 0.0, 10.0, 3.0, 6.0), n.updated_seq DESC
			LIMIT ?`, ftsTerms, candidateLimit)
		if err == nil {
			candidates, scanErr := scanRecallCandidates(rows)
			if scanErr != nil {
				return nil, scanErr
			}
			for rank := range candidates {
				candidate := candidates[rank]
				candidate.ftsRank = rank + 1
				byID[candidate.NodeID] = candidate
			}
		}
		// MATCH can reject adversarial syntax despite sanitization. Recall is an
		// additive hint, so an FTS error is intentionally indistinguishable from
		// no remembered work.
	}

	cues := normalizeRecallCues(scopeCues)
	if len(cues) > 0 {
		rows, err := s.db.Query(`
			SELECT n.id, n.intent,
			       CASE WHEN n.fold_digest <> '' THEN n.fold_digest ELSE n.summary END,
			       n.fold_pointers, e.ts, n.updated_seq
			FROM nodes AS n
			JOIN events AS e ON e.seq = n.updated_seq
			WHERE n.fold_root = 1
			ORDER BY n.updated_seq DESC
			LIMIT ?`, candidateLimit)
		if err != nil {
			return nil, fmt.Errorf("recall scope candidates: %w", err)
		}
		candidates, err := scanRecallCandidates(rows)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			hits := recallScopeHits(candidate, cues)
			if hits == 0 {
				continue
			}
			if existing := byID[candidate.NodeID]; existing != nil {
				existing.scopeHits = hits
				continue
			}
			candidate.scopeHits = hits
			byID[candidate.NodeID] = candidate
		}
	}
	if err := s.addTerritoryParents(byID); err != nil {
		return nil, err
	}
	if len(byID) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	candidates := make([]*recallCandidate, 0, len(byID))
	for _, candidate := range byID {
		score := 0.0
		if candidate.ftsRank > 0 {
			score += 4 / float64(candidate.ftsRank)
		}
		if strings.EqualFold(strings.TrimSpace(candidate.Intent), strings.TrimSpace(terms)) && strings.TrimSpace(terms) != "" {
			score += 8
		}
		score += float64(candidate.scopeHits) * 2
		age := now.Sub(candidate.at)
		if age < 0 {
			age = 0
		}
		score += .25 / (1 + age.Hours()/(24*30))
		if candidate.territoryParent {
			score += .5
		}
		candidate.Score = score
		candidate.Age = AgeLabel(candidate.at, now)
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].updatedSeq != candidates[j].updatedSeq {
			return candidates[i].updatedSeq > candidates[j].updatedSeq
		}
		return candidates[i].NodeID < candidates[j].NodeID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	hits := make([]RecallHit, len(candidates))
	for index, candidate := range candidates {
		hits[index] = candidate.RecallHit
	}
	return hits, nil
}

// addTerritoryParents expands a member hit by one parent join. Territories are
// exactly one level above jobs, so recall never needs a recursive traversal.
func (s *Store) addTerritoryParents(byID map[string]*recallCandidate) error {
	if len(byID) == 0 {
		return nil
	}
	memberIDs := make([]string, 0, len(byID))
	for id := range byID {
		memberIDs = append(memberIDs, id)
	}
	sort.Strings(memberIDs)
	placeholders := make([]string, len(memberIDs))
	args := make([]any, 0, len(memberIDs)+1)
	for index, id := range memberIDs {
		placeholders[index] = "?"
		args = append(args, id)
	}
	args = append(args, TerritoryGroup)
	rows, err := s.db.Query(`
		SELECT member.id, territory.id, territory.intent,
		       CASE WHEN territory.fold_digest <> '' THEN territory.fold_digest ELSE territory.summary END,
		       territory.fold_pointers, event.ts, territory.updated_seq
		FROM nodes AS member
		JOIN nodes AS territory ON territory.id = member.parent_id
		JOIN events AS event ON event.seq = territory.updated_seq
		WHERE member.id IN (`+strings.Join(placeholders, ",")+`)
		  AND member.fold_root = 1
		  AND territory.fold_root = 1
		  AND territory.grp = ?`, args...)
	if err != nil {
		return fmt.Errorf("recall territory parents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var memberID, pointers, timestamp string
		var parent recallCandidate
		if err := rows.Scan(&memberID, &parent.NodeID, &parent.Intent, &parent.Digest,
			&pointers, &timestamp, &parent.updatedSeq); err != nil {
			return fmt.Errorf("scan recall territory: %w", err)
		}
		if err := json.Unmarshal([]byte(pointers), &parent.Pointers); err != nil {
			return fmt.Errorf("decode recall pointers for %q: %w", parent.NodeID, err)
		}
		parent.at, err = parseTime(timestamp)
		if err != nil {
			return fmt.Errorf("parse recall time for %q: %w", parent.NodeID, err)
		}
		source := byID[memberID]
		if source == nil {
			continue
		}
		existing := byID[parent.NodeID]
		if existing == nil {
			parent.territoryParent = true
			parent.ftsRank = source.ftsRank
			parent.scopeHits = source.scopeHits
			byID[parent.NodeID] = &parent
			continue
		}
		if source.ftsRank > 0 && (existing.ftsRank == 0 || source.ftsRank < existing.ftsRank) {
			existing.ftsRank = source.ftsRank
		}
		if source.scopeHits > existing.scopeHits {
			existing.scopeHits = source.scopeHits
		}
		existing.territoryParent = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan recall territories: %w", err)
	}
	return nil
}

func scanRecallCandidates(rows *sql.Rows) ([]*recallCandidate, error) {
	defer rows.Close()
	candidates := make([]*recallCandidate, 0)
	for rows.Next() {
		var candidate recallCandidate
		var pointers, timestamp string
		if err := rows.Scan(&candidate.NodeID, &candidate.Intent, &candidate.Digest,
			&pointers, &timestamp, &candidate.updatedSeq); err != nil {
			return nil, fmt.Errorf("scan recall hit: %w", err)
		}
		if err := json.Unmarshal([]byte(pointers), &candidate.Pointers); err != nil {
			return nil, fmt.Errorf("decode recall pointers for %q: %w", candidate.NodeID, err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("parse recall time for %q: %w", candidate.NodeID, err)
		}
		candidate.at = at
		candidates = append(candidates, &candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan recall hits: %w", err)
	}
	return candidates, nil
}

func normalizeRecallCues(cues []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(cues))
	for _, cue := range cues {
		cue = strings.ToLower(strings.TrimSpace(cue))
		if strings.HasPrefix(cue, "tool:") || strings.HasPrefix(cue, "domain:") || cue == "user" || cue == "env" {
			continue
		}
		for _, prefix := range []string{"file:", "workspace:", "repo:"} {
			cue = strings.TrimPrefix(cue, prefix)
		}
		cue = strings.Trim(cue, "\"'` ")
		if cue == "" || seen[cue] {
			continue
		}
		seen[cue] = true
		result = append(result, cue)
	}
	return result
}

func recallScopeHits(candidate *recallCandidate, cues []string) int {
	hits := 0
	intent := strings.ToLower(candidate.Intent)
	for _, cue := range cues {
		matched := strings.Contains(intent, cue)
		for _, pointer := range candidate.Pointers {
			if strings.Contains(strings.ToLower(pointer), cue) {
				matched = true
				break
			}
		}
		if matched {
			hits++
		}
	}
	return hits
}
