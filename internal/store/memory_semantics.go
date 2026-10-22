package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const memorySemanticVersion = 2
const memorySemanticSchema = `CREATE TABLE IF NOT EXISTS memory_semantics(memory_id TEXT PRIMARY KEY,semantic_type TEXT NOT NULL,reason TEXT NOT NULL,version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS memory_schema_versions(version INTEGER PRIMARY KEY,at INTEGER NOT NULL);`

var memoryStatusID = regexp.MustCompile(`(?i)(\b(task|pr|issue|commit|head|origin/dev|build|tests?|suite|branch|ci)\b|\bv?\d+\.\d+|\b[0-9a-f]{7,40}\b)`)
var memoryStatusVerbs = regexp.MustCompile(`(?i)\b(landed|passed|verified|shipped|merged|implemented|completed|finished|fixed|deployed|wired|resolved|rebuilt|recorded)\b`)
var memoryNormative = regexp.MustCompile(`(?i)\b(always|never|prefer|must|should|require|default|convention|rule|do not|don't)\b`)

// DerivedMemoryType leaves source declarations intact. Completion language plus
// structural evidence identifies progress paragraphs; isolated past-tense words
// inside a durable rule do not change its meaning.
func DerivedMemoryType(declared, text string) (string, string) {
	verbs := len(memoryStatusVerbs.FindAllString(text, -1))
	structural := len(memoryStatusID.FindAllString(text, -1))
	normative := memoryNormative.MatchString(text)
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "current status") || strings.HasPrefix(lower, "current progress") {
		return MemoryProjectState, "explicit current-status paragraph"
	}
	if memoryStandingInstruction(lower) {
		return declared, "standing instruction retained"
	}
	progress := verbs >= 2 && structural > 0
	if verbs > 0 && structural > 1 && !normative {
		progress = true
	}
	if progress {
		return MemoryProjectState, "completed-work verbs and progress identifiers"
	}
	if declared == MemoryProjectState && normative && verbs == 0 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "always ") {
		return MemoryPreference, "standing instruction without completion evidence"
	}
	return declared, "declared type retained"
}
func memorySemanticType(m Memory) string {
	if m.SemanticType != "" {
		return m.SemanticType
	}
	return m.Type
}
func memoryDurable(m Memory) bool {
	switch memorySemanticType(m) {
	case MemoryCorrection, MemoryPreference, MemoryDecision:
		return true
	}
	return false
}
func deriveMemoryTypeOn(tx *sql.Tx, id, text string) error {
	var declared string
	if err := tx.QueryRow(`SELECT type FROM memories WHERE id=?`, id).Scan(&declared); err != nil {
		return err
	}
	kind, reason := DerivedMemoryType(declared, text)
	if _, err := tx.Exec(`INSERT OR REPLACE INTO memory_semantics VALUES(?,?,?,?)`, id, kind, reason, memorySemanticVersion); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM memory_durable_candidates WHERE memory_id=?`, id); err != nil {
		return err
	}
	if !memoryDurable(Memory{SemanticType: kind}) {
		return nil
	}
	_, err := tx.Exec(`INSERT OR REPLACE INTO memory_durable_candidates SELECT memory_id,owner,status,mass FROM memory_activation WHERE memory_id=?`, id)
	return err
}
func initMemoryV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM memory_schema_versions WHERE version=?`, memorySemanticVersion).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	if err = refreshMemoryV2TriggersOn(tx); err != nil {
		return err
	}
	rows, err := queryMemoriesOn(tx, "", nil, "", 0)
	if err != nil {
		return err
	}
	for _, m := range rows {
		if err = indexMemoryOn(tx, m.ID, m.Text, m.Tags, m.SourceSession); err != nil {
			return err
		}
	}
	// Cached v1 mass may include neutral retrievals. Rebuild traces from proof
	// events immediately, rather than keeping their influence until tomorrow.
	if err = refreshMemoryTraces(tx, time.Now()); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO memory_schema_versions VALUES(?,?)`, memorySemanticVersion, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

type MemoryTypeChange struct{ ID, Owner, Text, DeclaredType, SemanticType, Reason string }
type MemoryTypeCounts struct {
	Before, After map[string]int
	Relabeled     int
}
type MemoryTypeAudit struct {
	Total, Relabeled int
	ByOwner          map[string]MemoryTypeCounts
	Changes          []MemoryTypeChange
}

// MemorySemanticAudit is an explicit inspection API; callers choose where any
// personal row text is displayed. Migration itself never logs memory contents.
func (s *Store) MemorySemanticAudit() (MemoryTypeAudit, error) {
	out := MemoryTypeAudit{ByOwner: map[string]MemoryTypeCounts{}}
	rows, err := s.db.Query(`SELECT m.id,m.owner,m.text,m.type,COALESCE(s.semantic_type,m.type),COALESCE(s.reason,'declared type fallback') FROM memories m LEFT JOIN memory_semantics s ON s.memory_id=m.id WHERE m.status='active'`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r MemoryTypeChange
		if err = rows.Scan(&r.ID, &r.Owner, &r.Text, &r.DeclaredType, &r.SemanticType, &r.Reason); err != nil {
			return out, err
		}
		c := out.ByOwner[r.Owner]
		if c.Before == nil {
			c.Before = map[string]int{}
			c.After = map[string]int{}
		}
		c.Before[r.DeclaredType]++
		c.After[r.SemanticType]++
		out.Total++
		if r.DeclaredType != r.SemanticType {
			out.Relabeled++
			c.Relabeled++
			out.Changes = append(out.Changes, r)
		}
		out.ByOwner[r.Owner] = c
	}
	return out, rows.Err()
}

// MemoryTypeAuditSummary intentionally contains aggregate data only.
func (a MemoryTypeAudit) Summary() string {
	return fmt.Sprintf("semantic type audit: %d/%d active rows relabeled", a.Relabeled, a.Total)
}
func memoryLintType(declared, text string) string {
	kind, _ := DerivedMemoryType(declared, strings.TrimSpace(text))
	return kind
}

func memoryStandingInstruction(lower string) bool {
	if memoryNormative.MatchString(lower) {
		return true
	}
	return strings.HasPrefix(lower, "no ") && (strings.Contains(lower, " until ") || strings.Contains(lower, " unless "))
}

func refreshMemoryV2TriggersOn(tx *sql.Tx) error {
	pattern := regexp.MustCompile(`(?s)CREATE TRIGGER IF NOT EXISTS ([a-z_]+).*?END;`)
	for _, match := range pattern.FindAllStringSubmatch(activationSchema, -1) {
		if _, err := tx.Exec("DROP TRIGGER IF EXISTS " + match[1]); err != nil {
			return err
		}
		if _, err := tx.Exec(match[0]); err != nil {
			return err
		}
	}
	return nil
}
