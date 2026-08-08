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
//
// The last four fields are the gap ledger, and they are fields on this event
// rather than a second event kind because every reader of a job's judgement
// already reads this one. Quote is the span of the user's verbatim request the
// gap was said to be a failure of; Round is which round of repair it was
// weighed for; Extended says the job actually grew work to close it; Refused
// names, in the words the user would be told, why it did not. A quote that was
// extended on is spent — the same words may not buy a second round — so the
// ledger that bounds the loop is exactly what replays out of the journal.
type DeliveryGate struct {
	Pass         bool   `json:"pass"`
	Gap          string `json:"gap,omitempty"`
	PolishClosed bool   `json:"polish_closed"`
	Quote        string `json:"quote,omitempty"`
	Round        int    `json:"round,omitempty"`
	Extended     bool   `json:"extended,omitempty"`
	Refused      string `json:"refused,omitempty"`
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
	gate.Quote = bounded(strings.TrimSpace(gate.Quote), MaxDigestBytes)
	gate.Refused = bounded(strings.TrimSpace(gate.Refused), MaxDigestBytes)

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

// DeliveryGateLineage returns every gate recorded for a node and everything
// spliced beneath its id, oldest first — one job's whole run of judgements,
// including the repair rounds that continue it under "<id>-x<n>".
//
// It is an id-range read rather than a graph walk for the same reason the round
// counter is: the lineage IS an id namespace, and a reader that rebuilt it from
// parents and edges would own a second copy of the "-x" law.
func (s *Store) DeliveryGateLineage(baseID string) ([]DeliveryGate, error) {
	baseID = strings.TrimSpace(baseID)
	if baseID == "" {
		return nil, nil
	}
	// The lineage is the node itself plus its own split namespace, and nothing
	// else: a bare prefix range would also swallow "jobless" for "job", which
	// would let one job's ledger bound another's.
	namespace := baseID + SplitNamespace
	ceiling, ok := idPrefixCeiling(namespace)
	if !ok {
		return nil, fmt.Errorf("read delivery gate lineage %q: %w: prefix has no ordered ceiling", baseID, ErrInvalid)
	}
	rows, err := s.db.Query(`
		SELECT payload FROM events
		WHERE kind = ? AND (node_id = ? OR (node_id >= ? AND node_id < ?))
		ORDER BY seq`, EventDeliveryGate, baseID, namespace, ceiling)
	if err != nil {
		return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
	}
	defer rows.Close()
	gates := make([]DeliveryGate, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
		}
		var gate DeliveryGate
		if err := json.Unmarshal([]byte(payload), &gate); err != nil {
			return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
		}
		gates = append(gates, gate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read delivery gate lineage %q: %w", baseID, err)
	}
	return gates, nil
}
