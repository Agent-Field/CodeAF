package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MemoryScopeCount is an active-row count, not the number that rode a model turn.
type MemoryScopeCount struct {
	Owner string `json:"owner"`
	Count int64  `json:"count"`
}
type MemoryScopeCounts struct {
	Scopes   []MemoryScopeCount `json:"scopes"`
	Revision string             `json:"revision"`
}

// MemoryCountsFor reads one consistent snapshot without loading any memory text.
// Only the conversation's explicit person/machine/project owner set belongs here.
// Failure returns no snapshot; callers must not convert it to zero counts.
func (s *Store) MemoryCountsFor(ctx context.Context, owners []string) (MemoryScopeCounts, error) {
	if s == nil || s.db == nil {
		return MemoryScopeCounts{}, fmt.Errorf("memory counts: %w: no database", ErrInvalid)
	}
	if ctx == nil || len(owners) == 0 || len(owners) > 3 {
		return MemoryScopeCounts{}, fmt.Errorf("memory counts: %w: expected 1–3 owners and a context", ErrInvalid)
	}
	if err := validMemoryCountOwners(owners); err != nil {
		return MemoryScopeCounts{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return MemoryScopeCounts{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return MemoryScopeCounts{}, err
	}
	defer tx.Rollback()
	out, watermark, err := s.countActiveMemories(ctx, tx, owners)
	if err != nil {
		return MemoryScopeCounts{}, err
	}
	if err := ctx.Err(); err != nil {
		return MemoryScopeCounts{}, err
	}
	return memoryCountsRevision(out, watermark)
}

// validMemoryCountOwners refuses an owner set that is not distinct, well-formed
// person/machine/project owners.
func validMemoryCountOwners(owners []string) error {
	seen := map[string]bool{}
	for _, owner := range owners {
		project := strings.HasPrefix(owner, "project:") && len(owner) > len("project:")
		if len(owner) > 256 || !utf8.ValidString(owner) || strings.TrimSpace(owner) != owner || strings.IndexFunc(owner, unicode.IsControl) >= 0 || (owner != OwnerUser && owner != OwnerMachine && !project) || owner == OwnerLegacyProject || seen[owner] {
			return fmt.Errorf("memory counts: %w: invalid owner set", ErrInvalid)
		}
		seen[owner] = true
	}
	return nil
}

// countActiveMemories counts each owner's active rows inside tx and answers the
// event watermark the snapshot was read at.
func (s *Store) countActiveMemories(ctx context.Context, tx *sql.Tx, owners []string) (MemoryScopeCounts, int64, error) {
	var watermark int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&watermark); err != nil {
		return MemoryScopeCounts{}, 0, err
	}
	out := MemoryScopeCounts{Scopes: make([]MemoryScopeCount, 0, len(owners))}
	for _, owner := range owners {
		var count int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE owner = ? AND status = ?`, owner, MemoryActive).Scan(&count); err != nil {
			return MemoryScopeCounts{}, 0, err
		}
		out.Scopes = append(out.Scopes, MemoryScopeCount{Owner: owner, Count: count})
	}
	return out, watermark, nil
}

// memoryCountsRevision stamps the counts with a hash of the watermark and scopes.
func memoryCountsRevision(out MemoryScopeCounts, watermark int64) (MemoryScopeCounts, error) {
	identity, err := json.Marshal(struct {
		Watermark int64
		Scopes    []MemoryScopeCount
	}{watermark, out.Scopes})
	if err != nil {
		return MemoryScopeCounts{}, err
	}
	sum := sha256.Sum256(identity)
	out.Revision = hex.EncodeToString(sum[:])
	return out, nil
}
