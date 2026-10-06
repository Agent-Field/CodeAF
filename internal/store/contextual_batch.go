package store

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// contextualRecordKey identifies one canonical evidence row inside a batched
// guard: the owner it belongs to and the sequence the journal gave it.
func contextualRecordKey(owner string, seq int64) string {
	return owner + "\x00" + strconv.FormatInt(seq, 10)
}

// dedupeStrings is the trimmed, order-preserving set one batched read asks for.
func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// ContextualEvidenceLatestForMemories answers the newest evidence record per
// memory id in ONE indexed read. It exists so a caller projecting many claims
// does not issue a latest-record query per candidate: the index
// events_contextual_memory (node_id, kind, memory id, seq) lets SQLite take the
// maximum sequence per id directly. The read is NOT windowed to a count of
// records \u2014 a memory whose latest evidence sits behind a burst of newer rows is
// still answered, so no hidden total-claim ceiling can drop it.
func (s *Store) ContextualEvidenceLatestForMemories(owner string, memoryIDs []string) (map[string]ContextualEvidence, error) {
	out := map[string]ContextualEvidence{}
	wanted := dedupeStrings(memoryIDs)
	if len(wanted) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(wanted)+4)
	for _, id := range wanted {
		args = append(args, id)
	}
	node := contextualNode(owner)
	args = append(args, node, EventContextualEvidence, node, EventContextualEvidence)
	rows, err := s.db.Query(contextualLatestForMemoriesSQL(len(wanted)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e ContextualEvidence
		var payload, ts string
		var seq int64
		if err = rows.Scan(&seq, &ts, &payload); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(payload), &e); err != nil {
			return nil, err
		}
		e.Seq = seq
		if e.At, err = parseTime(ts); err != nil {
			return nil, err
		}
		out[e.MemoryID] = e
	}
	return out, rows.Err()
}

// contextualLatestForMemoriesSQL is the one statement the per-memory latest read
// runs. It is a function so the query-plan test in this package can EXPLAIN the
// SAME text the door runs, rather than a copy that could drift.
func contextualLatestForMemoriesSQL(count int) string {
	values := strings.TrimSuffix(strings.Repeat("(?),", count), ",")
	return `WITH wanted(mid) AS (VALUES ` + values + `)
SELECT e.seq, e.ts, e.payload
FROM events AS e
JOIN (SELECT json_extract(payload,'$.MemoryID') AS mid, MAX(seq) AS seq
      FROM events JOIN wanted ON json_extract(payload,'$.MemoryID') = wanted.mid
      WHERE node_id = ? AND kind = ?
      GROUP BY mid) AS latest ON e.seq = latest.seq
WHERE e.node_id = ? AND e.kind = ?`
}

// ContextualEvidenceEligibleBatch is [Store.ContextualEvidenceEligible] for many
// records at once. It preloads the canonical rows, their derivation ancestry,
// the memory-suppression join and the source-retirement join in a handful of
// owner-scoped, indexed reads, then answers each record with the SAME recursion
// the single-record door uses. A record is eligible if and only if its canonical
// row is inside its validity window, is not suppressed (by memory id or by any
// historical source), has no later correction or direct source suppression, and
// its derivation ancestry is itself usable under the conditions.
func (s *Store) ContextualEvidenceEligibleBatch(records []ContextualEvidence, conditions map[string]string, at time.Time) ([]bool, error) {
	if at.IsZero() {
		at = time.Now()
	}
	cache, err := s.contextualCacheFor(records)
	if err != nil {
		return nil, err
	}
	out := make([]bool, len(records))
	for i, rec := range records {
		canonical, err := cache.record(rec.Owner, rec.Seq)
		if err != nil {
			return nil, err
		}
		budget := ContextualEvidenceLimit
		ok, err := contextualUsable(cache, canonical, at, map[int64]bool{}, &budget)
		if err != nil {
			return nil, err
		}
		if ok {
			if ok, err = contextualConditionsBounded(cache, canonical, conditions); err != nil {
				return nil, err
			}
		}
		out[i] = ok
	}
	return out, nil
}

// contextualCache holds everything one batched guard asks for, read before the
// recursion starts. It is bounded by the size of the ANCESTRY it walks, not by a
// record count, so a wide or deep derivation cannot be cut behind a ceiling.
type contextualCache struct {
	records     map[string]ContextualEvidence
	suppression map[string]bool
	retirement  map[string]bool
}

func (c *contextualCache) record(owner string, seq int64) (ContextualEvidence, error) {
	e, ok := c.records[contextualRecordKey(owner, seq)]
	if !ok {
		return ContextualEvidence{}, fmt.Errorf("contextual guard is missing evidence seq %d", seq)
	}
	return e, nil
}

func (c *contextualCache) suppressed(e ContextualEvidence) (bool, error) {
	value, ok := c.suppression[contextualRecordKey(e.Owner, e.Seq)]
	if !ok {
		return false, fmt.Errorf("contextual guard is missing suppression for seq %d", e.Seq)
	}
	return value, nil
}

func (c *contextualCache) sourceRetired(e ContextualEvidence) (bool, error) {
	if e.SourceKey == "" {
		return false, nil
	}
	value, ok := c.retirement[contextualRecordKey(e.Owner, e.Seq)]
	if !ok {
		return false, fmt.Errorf("contextual guard is missing source state for seq %d", e.Seq)
	}
	return value, nil
}

// contextualCacheFor loads the canonical seeds, the transitive derivation
// ancestry, the memory-suppression answer and the source-retirement answer for
// every row in that closure, each in owner-scoped indexed reads.
func (s *Store) contextualCacheFor(seeds []ContextualEvidence) (*contextualCache, error) {
	cache := &contextualCache{
		records:     map[string]ContextualEvidence{},
		suppression: map[string]bool{},
		retirement:  map[string]bool{},
	}
	frontier, err := s.loadContextualFrontier(cache, seeds)
	for err == nil && len(frontier) > 0 {
		frontier, err = s.loadContextualFrontier(cache, frontier)
	}
	if err != nil {
		return nil, err
	}
	if err = s.loadContextualSuppression(cache); err != nil {
		return nil, err
	}
	return cache, nil
}

// loadContextualFrontier admits every not-yet-loaded row named by rows \u2014 the
// rows themselves and their derivation parents \u2014 and answers what it loaded, so
// the walk above can follow the next level. A cycle terminates because a row is
// loaded at most once.
func (s *Store) loadContextualFrontier(cache *contextualCache, rows []ContextualEvidence) ([]ContextualEvidence, error) {
	byOwner := map[string][]int64{}
	queued := map[string]bool{}
	enqueue := func(owner string, seq int64) {
		if seq <= 0 {
			return
		}
		key := contextualRecordKey(owner, seq)
		if queued[key] {
			return
		}
		if _, loaded := cache.records[key]; loaded {
			return
		}
		queued[key] = true
		byOwner[owner] = append(byOwner[owner], seq)
	}
	for _, e := range rows {
		enqueue(e.Owner, e.Seq)
		for _, seq := range e.Derivations {
			enqueue(e.Owner, seq)
		}
	}
	var loaded []ContextualEvidence
	for owner, seqs := range byOwner {
		found, err := s.readContextualEvidenceBatch(owner, seqs)
		if err != nil {
			return nil, err
		}
		for _, e := range found {
			cache.records[contextualRecordKey(e.Owner, e.Seq)] = e
			loaded = append(loaded, e)
		}
	}
	return loaded, nil
}

// loadContextualSuppression fills the two owner-journal answers the recursion
// consults, one indexed read per owner for each.
func (s *Store) loadContextualSuppression(cache *contextualCache) error {
	byOwner := map[string][]ContextualEvidence{}
	for _, e := range cache.records {
		byOwner[e.Owner] = append(byOwner[e.Owner], e)
	}
	for owner, rows := range byOwner {
		suppressed, err := s.contextualSuppressionBatch(owner, rows)
		if err != nil {
			return err
		}
		for key, value := range suppressed {
			cache.suppression[key] = value
		}
		retired, err := s.contextualSourceRetiredBatch(owner, rows)
		if err != nil {
			return err
		}
		for key, value := range retired {
			cache.retirement[key] = value
		}
	}
	return nil
}

// readContextualEvidenceBatch reads the named sequences for one owner in one
// query.
func (s *Store) readContextualEvidenceBatch(owner string, seqs []int64) ([]ContextualEvidence, error) {
	if len(seqs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(seqs)), ",")
	args := make([]any, 0, len(seqs)+2)
	args = append(args, contextualNode(owner), EventContextualEvidence)
	for _, seq := range seqs {
		args = append(args, seq)
	}
	rows, err := s.db.Query(`SELECT seq,ts,payload FROM events WHERE node_id=? AND kind=? AND seq IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ContextualEvidence, 0, len(seqs))
	for rows.Next() {
		var e ContextualEvidence
		var payload, ts string
		var seq int64
		if err = rows.Scan(&seq, &ts, &payload); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(payload), &e); err != nil {
			return nil, err
		}
		e.Seq = seq
		if e.At, err = parseTime(ts); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// contextualSuppressionBatch is [contextualMemorySuppressed] for many rows at
// once. It keeps the exact rule: a memory is retired when a suppression names
// its id, or when a suppression names a memory any of whose historical evidence
// rows carries this row's non-empty source key and hash.
func (s *Store) contextualSuppressionBatch(owner string, rows []ContextualEvidence) (map[string]bool, error) {
	out := make(map[string]bool, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	probes := make([]string, 0, len(rows))
	args := []any{contextualNode(owner), EventContextualMemorySuppression, EventContextualEvidence}
	for _, e := range rows {
		probes = append(probes, "SELECT ? AS seq, ? AS mid, ? AS key, ? AS hash")
		args = append(args, e.Seq, e.MemoryID, e.SourceKey, e.SourceHash)
	}
	query := `SELECT probe.seq, EXISTS(
  SELECT 1 FROM events AS suppression
  WHERE suppression.node_id=? AND suppression.kind=? AND (
    json_extract(suppression.payload,'$.MemoryID')=probe.mid
    OR (probe.key<>'' AND EXISTS(
      SELECT 1 FROM events AS source
      WHERE source.node_id=suppression.node_id AND source.kind=?
        AND json_extract(source.payload,'$.MemoryID')=json_extract(suppression.payload,'$.MemoryID')
        AND json_extract(source.payload,'$.SourceKey')=probe.key
        AND json_extract(source.payload,'$.SourceHash')=probe.hash))))
FROM (` + strings.Join(probes, " UNION ALL ") + `) AS probe`
	return s.scanProbeFlags(owner, query, args)
}

// contextualSourceRetiredBatch is the source half of [contextualUsable] for many
// rows at once: a direct source suppression, or a later evidence row over the
// same key with a different hash or revision.
func (s *Store) contextualSourceRetiredBatch(owner string, rows []ContextualEvidence) (map[string]bool, error) {
	out := make(map[string]bool, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	probes := make([]string, 0, len(rows))
	args := []any{contextualNode(owner), EventContextualSuppression, EventContextualEvidence}
	for _, e := range rows {
		probes = append(probes, "SELECT ? AS seq, ? AS key, ? AS hash, ? AS revision")
		args = append(args, e.Seq, e.SourceKey, e.SourceHash, e.Revision)
	}
	query := `SELECT probe.seq, EXISTS(
  SELECT 1 FROM events
  WHERE node_id=? AND probe.key<>'' AND (
    (kind=? AND json_extract(payload,'$.SourceKey')=probe.key
       AND (COALESCE(json_extract(payload,'$.SourceHash'),'')='' OR json_extract(payload,'$.SourceHash')=probe.hash))
    OR (kind=? AND seq>probe.seq AND json_extract(payload,'$.SourceKey')=probe.key
       AND (COALESCE(json_extract(payload,'$.SourceHash'),'')<>probe.hash OR COALESCE(json_extract(payload,'$.Revision'),'')<>probe.revision))))
FROM (` + strings.Join(probes, " UNION ALL ") + `) AS probe`
	return s.scanProbeFlags(owner, query, args)
}

// scanProbeFlags runs one probe-shaped query, whose first column is a sequence
// and whose second is an EXISTS flag, and keys the flags by owner and sequence.
func (s *Store) scanProbeFlags(owner, query string, args []any) (map[string]bool, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var seq int64
		var flag bool
		if err = rows.Scan(&seq, &flag); err != nil {
			return nil, err
		}
		out[contextualRecordKey(owner, seq)] = flag
	}
	return out, rows.Err()
}
