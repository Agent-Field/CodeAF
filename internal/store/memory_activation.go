package store

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
	"sort"
	"strings"
	"time"
	"unicode"
)

// MemoryContext contains local cues only. Assembly never calls a model.
type MemoryContext struct {
	Owners                  []string
	Project, Brief, Session string
	Keywords, Tasks         []string
	Now                     time.Time
}
type MemoryAdmission struct {
	Activation float64
	Riding     bool
	Reason     string
}
type MemoryBlock struct {
	Memories  []Memory
	Omitted   int
	Decisions map[string]MemoryAdmission
}

const EventMemoryInteraction EventKind = "memory_interaction"

type memoryInteraction struct {
	ID, Kind, Session string
	At                int64
	Pin               bool
	Cues              []string
}

const activationSchema = `
CREATE TABLE IF NOT EXISTS memory_activation(memory_id TEXT PRIMARY KEY, owner TEXT NOT NULL, status TEXT NOT NULL, pinned INTEGER NOT NULL, mass REAL NOT NULL DEFAULT 1, rehearsals INTEGER NOT NULL DEFAULT 0, at INTEGER NOT NULL, recent INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS memory_activation_rank ON memory_activation(owner,status,mass DESC,recent DESC);
CREATE INDEX IF NOT EXISTS memory_activation_recent ON memory_activation(owner,status,recent DESC);
CREATE INDEX IF NOT EXISTS memory_activation_pins ON memory_activation(owner,status,pinned);
CREATE TABLE IF NOT EXISTS memory_events(memory_id TEXT NOT NULL,seq INTEGER NOT NULL,kind TEXT NOT NULL,session TEXT NOT NULL,at INTEGER NOT NULL,PRIMARY KEY(memory_id,seq));
CREATE INDEX IF NOT EXISTS memory_events_chunk ON memory_events(memory_id,at);
CREATE TABLE IF NOT EXISTS memory_legacy_practice(memory_id TEXT PRIMARY KEY,mass REAL NOT NULL,at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS memory_associations(topic TEXT NOT NULL,memory_id TEXT NOT NULL,n INTEGER NOT NULL,PRIMARY KEY(topic,memory_id));
CREATE TABLE IF NOT EXISTS memory_maintenance(id INTEGER PRIMARY KEY,at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS memory_topics(topic TEXT NOT NULL,memory_id TEXT NOT NULL,PRIMARY KEY(topic,memory_id));
CREATE INDEX IF NOT EXISTS memory_topics_chunk ON memory_topics(memory_id,topic);
CREATE INDEX IF NOT EXISTS memory_associations_chunk ON memory_associations(memory_id,topic);
CREATE TABLE IF NOT EXISTS memory_topic_owners(topic TEXT NOT NULL,owner TEXT NOT NULL,memory_id TEXT NOT NULL,PRIMARY KEY(topic,owner,memory_id));
CREATE INDEX IF NOT EXISTS memory_topic_owners_chunk ON memory_topic_owners(memory_id);
CREATE TABLE IF NOT EXISTS memory_topic_fans(topic TEXT PRIMARY KEY,n INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS memory_topic_add AFTER INSERT ON memory_topics BEGIN INSERT OR IGNORE INTO memory_topic_owners SELECT new.topic,owner,new.memory_id FROM memory_activation WHERE memory_id=new.memory_id; INSERT INTO memory_topic_fans VALUES(new.topic,1) ON CONFLICT(topic) DO UPDATE SET n=n+1; END;
CREATE TRIGGER IF NOT EXISTS memory_topic_remove AFTER DELETE ON memory_topics BEGIN DELETE FROM memory_topic_owners WHERE topic=old.topic AND memory_id=old.memory_id; UPDATE memory_topic_fans SET n=n-1 WHERE topic=old.topic; END;
CREATE TABLE IF NOT EXISTS memory_fingerprints(memory_id TEXT PRIMARY KEY,b0 INTEGER,b1 INTEGER,b2 INTEGER,b3 INTEGER,minhash INTEGER);
CREATE INDEX IF NOT EXISTS memory_fingerprint_b0 ON memory_fingerprints(b0);
CREATE INDEX IF NOT EXISTS memory_fingerprint_b1 ON memory_fingerprints(b1);
CREATE INDEX IF NOT EXISTS memory_fingerprint_b2 ON memory_fingerprints(b2);
CREATE INDEX IF NOT EXISTS memory_fingerprint_b3 ON memory_fingerprints(b3);
CREATE TABLE IF NOT EXISTS memory_owner_counts(owner TEXT PRIMARY KEY,n INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS memory_activation_add AFTER INSERT ON memories BEGIN
 INSERT OR REPLACE INTO memory_activation VALUES(new.id,new.owner,new.status,(new.always_on != 0 OR new.origin='person'),1,0,COALESCE((SELECT unixepoch(ts) FROM events WHERE seq=new.created_seq),unixepoch()),new.created_seq);
 INSERT INTO memory_owner_counts VALUES(new.owner,CASE WHEN new.status='active' THEN 1 ELSE 0 END) ON CONFLICT(owner) DO UPDATE SET n=n+excluded.n;
 INSERT OR IGNORE INTO memory_events SELECT new.id,new.created_seq,'created',new.source_session,COALESCE(unixepoch(ts),unixepoch()) FROM events WHERE seq=new.created_seq;
END;
CREATE TRIGGER IF NOT EXISTS memory_activation_update AFTER UPDATE ON memories BEGIN
 INSERT OR IGNORE INTO memory_events SELECT new.id,new.updated_seq,CASE WHEN new.status='active' THEN 'confirmed' ELSE new.status END,new.source_session,COALESCE(unixepoch(ts),unixepoch()) FROM events WHERE seq=new.updated_seq AND (new.status != old.status OR new.text != old.text);
 UPDATE memory_topic_owners SET owner=new.owner WHERE memory_id=new.id;
 UPDATE memory_activation SET owner=new.owner,status=new.status,pinned=(new.always_on != 0 OR new.origin='person'),recent=new.updated_seq WHERE memory_id=new.id;
 UPDATE memory_owner_counts SET n=n-CASE WHEN old.status='active' THEN 1 ELSE 0 END WHERE owner=old.owner;
 INSERT INTO memory_owner_counts VALUES(new.owner,CASE WHEN new.status='active' THEN 1 ELSE 0 END) ON CONFLICT(owner) DO UPDATE SET n=n+excluded.n;
END;
CREATE TRIGGER IF NOT EXISTS memory_activation_delete AFTER DELETE ON memories BEGIN
 DELETE FROM memory_activation WHERE memory_id=old.id;
 DELETE FROM memory_topics WHERE memory_id=old.id;
 DELETE FROM memory_fingerprints WHERE memory_id=old.id;
 UPDATE memory_owner_counts SET n=n-CASE WHEN old.status='active' THEN 1 ELSE 0 END WHERE owner=old.owner;
END;`

func initMemoryActivation(db *sql.DB) error {
	if _, err := db.Exec(activationSchema); err != nil {
		return err
	}
	// Backfill only rows not indexed yet, so a normal reopen does no projection work.
	rows, err := db.Query(`SELECT m.id,m.text,m.tags,m.source_session FROM memories m LEFT JOIN memory_activation a ON a.memory_id=m.id WHERE a.memory_id IS NULL`)
	if err != nil {
		return err
	}
	type old struct{ id, text, tags, session string }
	var pending []old
	for rows.Next() {
		var x old
		if err := rows.Scan(&x.id, &x.text, &x.tags, &x.session); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, x)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hasUse, err := tableHasColumnOn(tx, "memories", "use_count")
	if err != nil {
		return err
	}
	for _, x := range pending {
		if err := backfillMemoryActivation(tx, x.id, x.text, x.tags, x.session, hasUse); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT OR REPLACE INTO memory_owner_counts SELECT owner,COUNT(*) FROM memories WHERE status='active' GROUP BY owner`); err != nil {
		return err
	}
	return tx.Commit()
}
func memoryWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
func indexMemoryOn(tx *sql.Tx, id, text string, tags []string, session string) error {
	if _, err := tx.Exec(`DELETE FROM memory_topics WHERE memory_id=?`, id); err != nil {
		return err
	}
	sig := memorySignature(memoryWords(text))
	minhash := uint64(math.MaxUint64)
	for _, w := range memoryWords(text) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(w))
		minhash = min(minhash, h.Sum64())
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO memory_fingerprints VALUES(?,?,?,?,?,?)`, id, int(sig&65535), int((sig>>16)&65535), int((sig>>32)&65535), int(sig>>48), int64(minhash)); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, word := range append(memoryWords(text), tags...) {
		if len(word) < 3 || seen[word] {
			continue
		}
		seen[word] = true
		if _, err := tx.Exec(`INSERT OR IGNORE INTO memory_topics VALUES(?,?)`, word, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO memory_topics SELECT topic,memory_id FROM memory_associations WHERE memory_id=?`, id); err != nil {
		return err
	}
	if session != "" {
		_, err := tx.Exec(`INSERT OR IGNORE INTO memory_topics VALUES(?,?)`, "session:"+session, id)
		return err
	}
	return nil
}
func (s *Store) RecordMemoryEvent(id, kind, session string) error {
	switch kind {
	case "surfaced", "retrieved", "used", "confirmed", "ignored", "forgotten", "merged":
	default:
		return fmt.Errorf("memory event: %w: unknown kind", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = memoryInteractionOn(tx, id, kind, session, false); err != nil {
		return err
	}
	return tx.Commit()
}
func memoryInteractionOn(tx *sql.Tx, id, kind, session string, pin bool) error {
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM memories WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("memory event: %w: missing memory", ErrInvalid)
	}
	p := memoryInteraction{ID: id, Kind: kind, Session: session, At: time.Now().Unix(), Pin: pin}
	seq, _, err := appendEvent(tx, id, EventMemoryInteraction, p)
	if err != nil {
		return err
	}
	return applyMemoryInteraction(tx, p, seq)
}
func applyMemoryInteraction(tx *sql.Tx, p memoryInteraction, seq int64) error {
	if _, err := tx.Exec(`INSERT OR IGNORE INTO memory_events VALUES(?,?,?,?,?)`, p.ID, seq, p.Kind, p.Session, p.At); err != nil {
		return err
	}
	if err := applyMemoryAssociations(tx, p); err != nil {
		return err
	}
	if p.Pin {
		if _, err := tx.Exec(`UPDATE memories SET origin='person' WHERE id=?`, p.ID); err != nil {
			return err
		}
	}
	// Surfacing is recorded but is not evidence of use: rehearsing every prompt
	// would make ignored status lines immortal. Only engaged content reinforces.
	if p.Kind != "used" && p.Kind != "confirmed" && p.Kind != "retrieved" && p.Kind != "merged" {
		return nil
	}
	var mass float64
	var at int64
	if err := tx.QueryRow(`SELECT mass,at FROM memory_activation WHERE memory_id=?`, p.ID).Scan(&mass, &at); err != nil {
		return err
	}
	age := math.Max(1, float64(p.At-at)/3600)
	mass = mass/math.Sqrt(age) + 1
	_, err := tx.Exec(`UPDATE memory_activation SET mass=?,at=?,rehearsals=rehearsals+1 WHERE memory_id=?`, mass, p.At, p.ID)
	return err
}

// SelectMemoryBlock is shared by the prompt and inspection UI. A cached trace
// uses the ACT-R power-law exponent 0.5; event history remains available for
// exact offline calibration. The trace is a bounded-work approximation.
func SelectMemoryBlock(memories []Memory, ctx MemoryContext) MemoryBlock {
	if ctx.Now.IsZero() {
		ctx.Now = time.Now()
	}
	out := MemoryBlock{Decisions: map[string]MemoryAdmission{}}
	owners := map[string]bool{}
	for _, o := range ctx.Owners {
		owners[o] = true
	}
	cues := map[string]bool{}
	for _, w := range memoryWords(ctx.Brief + " " + strings.Join(ctx.Keywords, " ") + " " + strings.Join(ctx.Tasks, " ")) {
		if len(w) > 2 {
			cues[w] = true
		}
	}
	var ranking []memoryScored
	for _, m := range memories {
		d := MemoryAdmission{Reason: "outside this conversation's owners"}
		if len(owners) > 0 && !owners[m.Owner] {
			out.Decisions[m.ID] = d
			continue
		}
		if m.Status != "" && m.Status != MemoryActive {
			d.Reason = "archived by explicit change"
			out.Decisions[m.ID] = d
			continue
		}
		a := memoryScore(m, ctx, cues)
		pin := m.Rule || m.Origin == OriginPerson
		ranking = append(ranking, memoryScored{m, a, pin})
		out.Decisions[m.ID] = MemoryAdmission{Activation: a, Reason: "below activation threshold"}
	}
	sort.SliceStable(ranking, func(i, j int) bool { return memoryRankLess(ranking[i], ranking[j]) })
	used := 0
	for _, r := range ranking {
		d := out.Decisions[r.m.ID]
		cost := MemoryLineCost(r.m.Text)
		d = memoryAdmission(r, used)
		if d.Riding {
			out.Memories = append(out.Memories, r.m)
			used += cost
		} else {
			out.Omitted++
		}
		out.Decisions[r.m.ID] = d
	}
	return out
}

// AssembleMemoryBlock performs indexed, bounded admission rather than scanning
// all stored chunks. Pins are deliberately unbounded and always ride.
func (s *Store) AssembleMemoryBlock(ctx MemoryContext) (MemoryBlock, error) {
	if len(ctx.Owners) == 0 {
		return MemoryBlock{}, fmt.Errorf("assemble memory: %w: no owners", ErrInvalid)
	}
	ids := map[string]bool{}
	total := 0
	collect := func(q string, args ...any) error {
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids[id] = true
		}
		return closeRows(rows)
	}
	var err error
	total, err = s.memoryOwnerCandidates(ctx.Owners, collect)
	if err != nil {
		return MemoryBlock{}, err
	}
	cues := memoryWords(ctx.Brief + " " + strings.Join(ctx.Keywords, " ") + " " + strings.Join(ctx.Tasks, " "))
	if ctx.Session != "" {
		cues = append(cues, "session:"+ctx.Session)
	}
	if len(cues) > 32 {
		cues = cues[:32]
	}
	for _, cue := range cues {
		for _, owner := range ctx.Owners {
			if err := collect(`SELECT t.memory_id FROM (SELECT memory_id FROM memory_topic_owners WHERE topic=? AND owner=? LIMIT 256) t JOIN memory_activation a ON a.memory_id=t.memory_id WHERE a.status='active' ORDER BY a.mass DESC LIMIT 32`, cue, owner); err != nil {
				return MemoryBlock{}, err
			}
		}
	}
	all, err := s.memoryCandidates(ids)
	if err != nil {
		return MemoryBlock{}, err
	}
	out := SelectMemoryBlock(all, ctx)
	out.Omitted = max(0, total-len(out.Memories))
	return out, nil
}
func (s *Store) MemorySelectionFor(ctx MemoryContext) (MemoryBlock, error) {
	return s.AssembleMemoryBlock(ctx)
}
func memorySignature(words []string) uint64 {
	var v [64]int
	for _, w := range words {
		h := fnv.New64a()
		_, _ = h.Write([]byte(w))
		x := h.Sum64()
		for i := range v {
			if x&(1<<i) != 0 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}
	var sig uint64
	for i, n := range v {
		if n >= 0 {
			sig |= 1 << i
		}
	}
	return sig
}

// Similarity never overrides numbers or negation: these frequently distinguish
// contradictory instructions. The token-set check is the conservative MinHash
// verification step (exact Jaccard, avoiding probabilistic false positives).
func memoryNearDuplicate(a, b string) bool {
	aw, bw := memoryWords(a), memoryWords(b)
	if len(aw) < 8 || len(bw) < 8 {
		return false
	}
	if strings.Join(aw, " ") != strings.Join(bw, " ") {
		return false
	}
	return bits.OnesCount64(memorySignature(aw)^memorySignature(bw)) <= 3
}

// Stable session jitter permits exploration without per-turn prompt churn.
func memoryNoise(id, session string) float64 {
	h := fnv.New128a()
	_, _ = h.Write([]byte(id + "\x00" + session))
	v := h.Sum(nil)
	u1 := (float64(binary.BigEndian.Uint64(v[:8])>>11) + 1) / (1<<53 + 1)
	u2 := (float64(binary.BigEndian.Uint64(v[8:])>>11) + 1) / (1<<53 + 1)
	return 0.025 * math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

type memoryScored struct {
	m   Memory
	a   float64
	pin bool
}

func memoryScore(m Memory, ctx MemoryContext, cues map[string]bool) float64 {
	at := m.ActivationAt
	if at.IsZero() || at.Unix() == 0 {
		at = m.UpdatedAt
	}
	if at.IsZero() {
		at = ctx.Now
	}
	mass := m.Activation
	if mass <= 0 {
		mass = 1
	}
	age := math.Max(1, ctx.Now.Sub(at).Hours())
	a := math.Log(mass / math.Sqrt(age))
	a += memorySpread(m, cues)
	if ctx.Project != "" && (m.Owner == ctx.Project || m.Owner == OwnerProject(ctx.Project)) {
		a += 2 / math.Sqrt(float64(max(1, m.OwnerFan)))
	}
	if ctx.Session != "" && m.SourceSession == ctx.Session {
		a += math.Log(2)
	}
	if ctx.Session != "" {
		a += memoryNoise(m.ID, ctx.Session)
	}

	return a
}

func memoryAdmission(r memoryScored, used int) MemoryAdmission {
	d := MemoryAdmission{Activation: r.a, Reason: "below activation threshold"}
	cost := MemoryLineCost(r.m.Text)
	switch {
	case r.pin:
		d.Riding = true
		d.Reason = "explicit pin"
	case r.a < -3:
	case used+cost > MemoryBlockRunes:
		d.Reason = "prompt budget"
	default:
		d.Riding = true
		d.Reason = "activation and current context"
	}

	return d
}

func (s *Store) memoryCandidates(ids map[string]bool) ([]Memory, error) {
	var all []Memory
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for start := 0; start < len(keys); start += 200 {
		end := min(start+200, len(keys))
		args := make([]any, 0, end-start)
		for _, id := range keys[start:end] {
			args = append(args, id)
		}
		rows, err := s.queryMemories(`WHERE memories.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args, "", 0)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}

	return all, nil
}

func (s *Store) memoryOwnerCandidates(owners []string, collect func(string, ...any) error) (int, error) {
	total := 0
	for _, owner := range owners {
		var n int
		err := s.db.QueryRow(`SELECT n FROM memory_owner_counts WHERE owner=?`, owner).Scan(&n)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		total += n
		if err = collect(`SELECT memory_id FROM memory_activation WHERE owner=? AND status='active' AND pinned=1`, owner); err != nil {
			return 0, err
		}
		if err = collect(`SELECT memory_id FROM memory_activation WHERE owner=? AND status='active' ORDER BY mass DESC,recent DESC LIMIT 64`, owner); err != nil {
			return 0, err
		}
		if err = collect(`SELECT memory_id FROM memory_activation WHERE owner=? AND status='active' ORDER BY recent DESC LIMIT 32`, owner); err != nil {
			return 0, err
		}
	}

	return total, nil
}

func (s *Store) SearchMemoriesFor(owners []string, query string, limit int) ([]Memory, error) {
	if len(owners) == 0 {
		return nil, fmt.Errorf("search memories: %w: no owners", ErrInvalid)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	words := memoryWords(query)
	if len(words) > 16 {
		words = words[:16]
	}
	if len(words) == 0 {
		return []Memory{}, nil
	}
	args := []any{}
	for _, w := range words {
		args = append(args, w)
	}
	for _, o := range owners {
		args = append(args, o)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT t.memory_id FROM memory_topics t JOIN memory_activation a ON a.memory_id=t.memory_id WHERE t.topic IN (`+strings.TrimSuffix(strings.Repeat("?,", len(words)), ",")+`) AND a.owner IN (`+strings.TrimSuffix(strings.Repeat("?,", len(owners)), ",")+`) AND a.status='active' GROUP BY t.memory_id ORDER BY COUNT(*) DESC,a.mass DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	ids, err := readMemorySearchIDs(rows)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []Memory{}, nil
	}
	return s.queryMemories(`WHERE memories.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", ids, "memories.updated_seq DESC", limit)
}
func readMemorySearchIDs(rows *sql.Rows) ([]any, error) {
	var ids []any
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, closeRows(rows)
}

func (s *Store) RecordMemoryContextEvent(id, kind string, ctx MemoryContext) error {
	if kind != "used" {
		return s.RecordMemoryEvent(id, kind, ctx.Session)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p := memoryInteraction{ID: id, Kind: kind, Session: ctx.Session, At: time.Now().Unix(), Cues: memoryWords(ctx.Brief + " " + strings.Join(ctx.Keywords, " ") + " " + strings.Join(ctx.Tasks, " "))}
	if len(p.Cues) > 32 {
		p.Cues = p.Cues[:32]
	}
	seq, _, err := appendEvent(tx, id, EventMemoryInteraction, p)
	if err != nil {
		return err
	}
	if err = applyMemoryInteraction(tx, p, seq); err != nil {
		return err
	}
	return tx.Commit()
}
func applyMemoryAssociations(tx *sql.Tx, p memoryInteraction) error {
	if p.Kind != "used" {
		return nil
	}
	seen := map[string]bool{}
	for _, cue := range p.Cues {
		if len(cue) < 3 || seen[cue] {
			continue
		}
		seen[cue] = true
		if _, err := tx.Exec(`INSERT INTO memory_associations VALUES(?,?,1) ON CONFLICT(topic,memory_id) DO UPDATE SET n=n+1`, cue, p.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO memory_topics VALUES(?,?)`, cue, p.ID); err != nil {
			return err
		}
	}
	return nil
}

// RefreshMemoryActivationDue materializes exact historical traces once daily.
// It runs off the turn path. Legacy use counts supply undated practice at the
// original timestamp; migration never invents historical session labels.
func (s *Store) RefreshMemoryActivationDue(now time.Time) error {
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var last int64
	err = tx.QueryRow(`SELECT at FROM memory_maintenance WHERE id=1`).Scan(&last)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if now.Unix()-last < 86400 {
		return nil
	}
	if err = refreshMemoryTraces(tx, now); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR REPLACE INTO memory_maintenance VALUES(1,?)`, now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func refreshMemoryTraces(tx *sql.Tx, now time.Time) error {
	rows, err := tx.Query(`SELECT memory_id,at,1.0 FROM memory_events WHERE kind IN ('created','used','confirmed','retrieved','merged') UNION ALL SELECT memory_id,at,mass FROM memory_legacy_practice`)
	if err != nil {
		return err
	}
	sums := map[string]float64{}
	for rows.Next() {
		var id string
		var at int64
		var weight float64
		if err = rows.Scan(&id, &at, &weight); err != nil {
			rows.Close()
			return err
		}
		sums[id] += weight / math.Sqrt(math.Max(1, float64(now.Unix()-at)/3600))
	}
	if err = closeRows(rows); err != nil {
		return err
	}
	for id, mass := range sums {
		if _, err = tx.Exec(`UPDATE memory_activation SET mass=?,at=? WHERE memory_id=?`, mass, now.Unix(), id); err != nil {
			return err
		}
	}
	return nil
}

func memoryRankLess(a, b memoryScored) bool {
	if a.pin != b.pin {
		return a.pin
	}
	if a.a != b.a {
		return a.a > b.a
	}
	if a.m.CreatedSeq != b.m.CreatedSeq {
		return a.m.CreatedSeq > b.m.CreatedSeq
	}
	return a.m.ID < b.m.ID
}

func memorySpread(m Memory, cues map[string]bool) float64 {
	matches := 0
	spread := 0.0
	matched := map[string]bool{}
	words := memoryWords(m.Text + " " + strings.Join(m.Tags, " "))
	for w := range m.TopicStrength {
		words = append(words, w)
	}
	for _, w := range words {
		if cues[w] && !matched[w] {
			matched[w] = true
			matches++
			fan := max(1, m.TopicFans[w])
			spread += math.Log1p(float64(max(1, m.TopicStrength[w]))) / math.Log(2) / math.Log2(2+float64(fan))
		}
	}
	if matches > 0 {
		return math.Log1p(spread) * 2
	}

	return 0
}

func backfillMemoryActivation(tx *sql.Tx, id, text, encodedTags, session string, hasUse bool) error {
	var err error
	if _, err = tx.Exec(`INSERT OR IGNORE INTO memory_activation SELECT id,owner,status,(always_on != 0 OR origin='person'),1,0,COALESCE((SELECT unixepoch(ts) FROM events WHERE seq=created_seq),unixepoch()),created_seq FROM memories WHERE id=?`, id); err != nil {
		return err
	}
	if hasUse {
		if _, err = tx.Exec(`UPDATE memory_activation SET mass=1+COALESCE((SELECT use_count FROM memories WHERE id=?),0),rehearsals=COALESCE((SELECT use_count FROM memories WHERE id=?),0) WHERE memory_id=?`, id, id, id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO memory_events SELECT id,created_seq,'created',source_session,COALESCE((SELECT unixepoch(ts) FROM events WHERE seq=created_seq),unixepoch()) FROM memories WHERE id=?`, id); err != nil {
		return err
	}
	if hasUse {
		if _, err = tx.Exec(`INSERT OR REPLACE INTO memory_legacy_practice SELECT memory_id,mass-1,at FROM memory_activation WHERE memory_id=?`, id); err != nil {
			return err
		}
	}
	var tags []string
	_ = json.Unmarshal([]byte(encodedTags), &tags)
	if err = indexMemoryOn(tx, id, text, tags, session); err != nil {
		return err
	}
	return nil
}
