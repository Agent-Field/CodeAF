package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DeliveryGate is the final judge's evidence about one job. Pass is the first
// delivery's result; Gap names what it missed; PolishClosed says whether the
// single permitted repair was subsequently judged complete.
type DeliveryGate struct {
	Pass         bool   `json:"pass"`
	Gap          string `json:"gap,omitempty"`
	PolishClosed bool   `json:"polish_closed"`
}

// RecordDeliveryGate appends one gate result. It has no materialized view: the
// event is sparse, read by node id, and remains the source of truth on rebuild.
func (s *Store) RecordDeliveryGate(nodeID string, gate DeliveryGate) error {
	nodeID = strings.TrimSpace(nodeID)
	gate.Gap = strings.TrimSpace(gate.Gap)
	if nodeID == "" {
		return fmt.Errorf("record delivery gate: %w: empty node id", ErrInvalid)
	}
	if !gate.Pass && gate.Gap == "" {
		return fmt.Errorf("record delivery gate: %w: a failed gate must name the gap", ErrInvalid)
	}
	gate.Gap = bounded(gate.Gap, MaxDigestBytes)

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventDeliveryGate, gate); err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record delivery gate: %w", err)
	}
	return nil
}

// DeliveryGateFor returns the latest gate event for a node. More than one is
// legal because corrections in an append-only journal are later events.
func (s *Store) DeliveryGateFor(nodeID string) (DeliveryGate, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, nodeID, EventDeliveryGate).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryGate{}, false, nil
	}
	if err != nil {
		return DeliveryGate{}, false, fmt.Errorf("read delivery gate: %w", err)
	}
	var gate DeliveryGate
	if err := json.Unmarshal([]byte(payload), &gate); err != nil {
		return DeliveryGate{}, false, fmt.Errorf("read delivery gate: %w", err)
	}
	return gate, true, nil
}
