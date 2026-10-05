package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	EventContextualDependency      EventKind = "contextual_dependency"
	EventContextualImpactDismissed EventKind = "contextual_impact_dismissed"
	ContextualImpactLimit                    = 8
	contextualDependencyWindow               = 128
)

// ContextualDependencyObservation is an exact observed link, never a guess
// based on matching project or file names. The caller supplies successful read
// receipts for both canonical paths and proves that the consumer actually
// references the producer. This record grants no access to consumer memories.
type ContextualDependencyObservation struct {
	ID            string
	ProducerOwner string
	ConsumerOwner string
	EntityID      string
	ProducerPath  string
	ConsumerPath  string
	ProducerHash  string
	ConsumerHash  string
	ReceiptIDs    []string
	Assumption    string
}

// ContextualImpactNotice offers a bounded consideration, not scheduled work.
// ConsumerHash must still match the observed assumption. EvidenceHash is the
// exact dismissal identity and changes when relevant content changes.
type ContextualImpactNotice struct {
	Dependency   ContextualDependencyObservation
	ProducerHash string
	ConsumerHash string
	EvidenceHash string
}

func dependencyProject(owner string) bool {
	return ValidOwner(owner) && OwnerKindOf(owner) == "project" && owner != OwnerLegacyProject
}

func dependencyAuthorized(owners []string, producer, consumer string) bool {
	producerOK, consumerOK := false, false
	for _, owner := range owners {
		owner = normalizeOwner(owner)
		producerOK = producerOK || owner == producer
		consumerOK = consumerOK || owner == consumer
	}
	return producerOK && consumerOK
}

func dependencyHashValid(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == sha256.Size && hash == strings.ToLower(hash)
}

func dependencyIdentity(d ContextualDependencyObservation) string {
	encoded, _ := json.Marshal([]string{d.ProducerOwner, d.ConsumerOwner, d.EntityID, d.ProducerPath, d.ConsumerPath})
	return fmt.Sprintf("dependency_%x", sha256.Sum256(encoded))
}

func validateContextualDependency(d ContextualDependencyObservation) (ContextualDependencyObservation, error) {
	d.ProducerOwner = normalizeOwner(d.ProducerOwner)
	d.ConsumerOwner = normalizeOwner(d.ConsumerOwner)
	if !dependencyProject(d.ProducerOwner) || !dependencyProject(d.ConsumerOwner) || d.ProducerOwner == d.ConsumerOwner {
		return d, fmt.Errorf("%w: dependency requires two distinct proven project owners", ErrInvalid)
	}
	for _, path := range []string{d.ProducerPath, d.ConsumerPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > 4096 {
			return d, fmt.Errorf("%w: dependency paths must be canonical absolute paths", ErrInvalid)
		}
	}
	if strings.TrimSpace(d.EntityID) == "" || len(d.EntityID) > 1024 || strings.TrimSpace(d.Assumption) == "" || len(d.Assumption) > 4096 {
		return d, fmt.Errorf("%w: dependency requires a bounded exact entity and observed assumption", ErrInvalid)
	}
	if !dependencyHashValid(d.ProducerHash) || !dependencyHashValid(d.ConsumerHash) {
		return d, fmt.Errorf("%w: dependency requires producer and consumer content hashes", ErrInvalid)
	}
	if len(d.ReceiptIDs) < 2 || len(d.ReceiptIDs) > 16 {
		return d, fmt.Errorf("%w: dependency requires receipts for both reads", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, receipt := range d.ReceiptIDs {
		if strings.TrimSpace(receipt) == "" || len(receipt) > 1024 || seen[receipt] {
			return d, fmt.Errorf("%w: dependency receipts must be distinct and bounded", ErrInvalid)
		}
		seen[receipt] = true
	}
	identity := dependencyIdentity(d)
	if d.ID != "" && d.ID != identity {
		return d, fmt.Errorf("%w: dependency id does not match exact endpoints", ErrInvalid)
	}
	d.ID = identity
	return d, nil
}

// ObserveContextualDependency accepts only a trusted successful-read adapter.
// Both project owners must have been explicitly authorized by those reads;
// merely finding a similar project name cannot supply that authorization.
// Canonical events are the only storage, so Rebuild cannot lose an edge.
func (s *Store) ObserveContextualDependency(authorizedOwners []string, d ContextualDependencyObservation) (ContextualDependencyObservation, error) {
	d, err := validateContextualDependency(d)
	if err != nil {
		return d, err
	}
	if !dependencyAuthorized(authorizedOwners, d.ProducerOwner, d.ConsumerOwner) {
		return d, fmt.Errorf("%w: dependency endpoints were not both authorized", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	// Re-reading unchanged content is evidence refresh, not a new dependency.
	// Avoid letting repeated reads of one edge bury the bounded neighborhood.
	var previous string
	err = tx.QueryRow("SELECT payload FROM events WHERE node_id = ? AND kind = ? AND json_extract(payload,'$.ID') = ? ORDER BY seq DESC LIMIT 1", contextualNode(d.ProducerOwner), EventContextualDependency, d.ID).Scan(&previous)
	if err == nil {
		var canonical ContextualDependencyObservation
		if err := json.Unmarshal([]byte(previous), &canonical); err != nil {
			return d, err
		}
		if canonical.ProducerHash == d.ProducerHash && canonical.ConsumerHash == d.ConsumerHash && canonical.Assumption == d.Assumption {
			return canonical, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return d, err
	}
	_, _, err = appendEvent(tx, contextualNode(d.ProducerOwner), EventContextualDependency, d)
	if err != nil {
		return d, err
	}
	return d, tx.Commit()
}

func dependencyLimit(limit int) int {
	if limit <= 0 || limit > ContextualImpactLimit {
		return ContextualImpactLimit
	}
	return limit
}

// DependenciesForProducer returns at most eight observed proof links owned by
// this producer. It exposes only the hashed consumer read already authorized
// when the edge was learned; it neither reads nor authorizes foreign memories.
func (s *Store) DependenciesForProducer(producerOwner string, limit int) ([]ContextualDependencyObservation, error) {
	producerOwner = normalizeOwner(producerOwner)
	if !dependencyProject(producerOwner) {
		return nil, fmt.Errorf("%w: a proven producer project is required", ErrInvalid)
	}
	return contextualDependenciesOn(s.db, producerOwner, dependencyLimit(limit))
}

func contextualDependenciesOn(q memoryQuerier, producerOwner string, limit int) ([]ContextualDependencyObservation, error) {
	rows, err := q.Query("SELECT payload FROM events WHERE node_id = ? AND kind = ? ORDER BY seq DESC LIMIT ?", contextualNode(producerOwner), EventContextualDependency, contextualDependencyWindow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextualDependencyObservation{}
	seen := map[string]bool{}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		var d ContextualDependencyObservation
		if err := json.Unmarshal([]byte(encoded), &d); err != nil {
			return nil, err
		}
		d, err = validateContextualDependency(d)
		if err != nil {
			return nil, err
		}
		if d.ProducerOwner != producerOwner {
			return nil, fmt.Errorf("%w: dependency owner differs from journal", ErrInvalid)
		}
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		out = append(out, d)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

// ContextualDependencies exposes records only when both owners are named.
func (s *Store) ContextualDependencies(owners []string, producerOwner string, limit int) ([]ContextualDependencyObservation, error) {
	producerOwner = normalizeOwner(producerOwner)
	if !dependencyProject(producerOwner) {
		return nil, fmt.Errorf("%w: a proven producer project is required", ErrInvalid)
	}
	records, err := contextualDependenciesOn(s.db, producerOwner, ContextualImpactLimit)
	if err != nil {
		return nil, err
	}
	out := []ContextualDependencyObservation{}
	for _, d := range records {
		if dependencyAuthorized(owners, d.ProducerOwner, d.ConsumerOwner) {
			out = append(out, d)
		}
		if len(out) >= dependencyLimit(limit) {
			break
		}
	}
	return out, nil
}

func contextualImpactHash(d ContextualDependencyObservation, producerHash, consumerHash string) string {
	encoded, _ := json.Marshal([]string{d.ID, d.ProducerHash, producerHash, consumerHash, d.Assumption})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

// ContextualImpacts requires an actual current consumer read hash and an exact
// producer entity. Missing reads, changed consumer assumptions and unchanged
// producer content produce no notice. It starts no work and keeps no scheduler.
func (s *Store) ContextualImpacts(owners []string, producerOwner, entityID, newProducerHash string, currentConsumerHashes map[string]string) ([]ContextualImpactNotice, error) {
	producerOwner = normalizeOwner(producerOwner)
	if !dependencyProject(producerOwner) || strings.TrimSpace(entityID) == "" || !dependencyHashValid(newProducerHash) {
		return nil, fmt.Errorf("%w: impact requires a proven producer, exact entity and current hash", ErrInvalid)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	dependencies, err := contextualDependenciesOn(tx, producerOwner, ContextualImpactLimit)
	if err != nil {
		return nil, err
	}
	out := []ContextualImpactNotice{}
	for _, d := range dependencies {
		if d.EntityID != entityID || !dependencyAuthorized(owners, d.ProducerOwner, d.ConsumerOwner) || d.ProducerHash == newProducerHash {
			continue
		}
		current, ok := currentConsumerHashes[d.ConsumerPath]
		if !ok || current != d.ConsumerHash {
			continue
		}
		notice := ContextualImpactNotice{Dependency: d, ProducerHash: newProducerHash, ConsumerHash: current, EvidenceHash: contextualImpactHash(d, newProducerHash, current)}
		var dismissed int
		if err := tx.QueryRow("SELECT COUNT(*) FROM events WHERE node_id = ? AND kind = ? AND json_extract(payload,'$.EvidenceHash') = ?", contextualNode(producerOwner), EventContextualImpactDismissed, notice.EvidenceHash).Scan(&dismissed); err != nil {
			return nil, err
		}
		if dismissed == 0 {
			out = append(out, notice)
		}
	}
	return out, nil
}

// DismissContextualImpact suppresses exactly this content change. It reloads
// the canonical edge instead of trusting altered endpoints on a caller's notice.
func (s *Store) DismissContextualImpact(owners []string, notice ContextualImpactNotice) error {
	d, err := validateContextualDependency(notice.Dependency)
	if err != nil {
		return err
	}
	if !dependencyAuthorized(owners, d.ProducerOwner, d.ConsumerOwner) || !dependencyHashValid(notice.ProducerHash) || notice.ConsumerHash != d.ConsumerHash || notice.ProducerHash == d.ProducerHash {
		return fmt.Errorf("%w: dismissal requires authorized current impact evidence", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var encoded string
	err = tx.QueryRow("SELECT payload FROM events WHERE node_id = ? AND kind = ? AND json_extract(payload,'$.ID') = ? ORDER BY seq DESC LIMIT 1", contextualNode(d.ProducerOwner), EventContextualDependency, d.ID).Scan(&encoded)
	if err != nil {
		return err
	}
	var canonical ContextualDependencyObservation
	if err := json.Unmarshal([]byte(encoded), &canonical); err != nil {
		return err
	}
	if canonical.ProducerHash != d.ProducerHash || canonical.ConsumerHash != d.ConsumerHash || canonical.Assumption != d.Assumption || dependencyIdentity(canonical) != d.ID {
		return fmt.Errorf("%w: dismissal evidence is not the current observed dependency", ErrInvalid)
	}
	expected := contextualImpactHash(canonical, notice.ProducerHash, notice.ConsumerHash)
	if notice.EvidenceHash != expected {
		return fmt.Errorf("%w: dismissal hash does not match evidence", ErrInvalid)
	}
	_, _, err = appendEvent(tx, contextualNode(d.ProducerOwner), EventContextualImpactDismissed, struct{ ID, EvidenceHash string }{d.ID, expected})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// The edge and dismissal projections read canonical events directly. They need
// no materialized tables, rebuild hooks, transport or independent lifecycle.
