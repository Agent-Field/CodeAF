package store

import (
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
// The last fields are the gap ledger, and they are fields on this event rather
// than a second event kind because every reader of a job's judgement already
// reads this one. Quotes are the spans of the user's verbatim request the gap
// was said to be a failure of, and Quote is those spans as the one line a
// person reads; Round is which round of repair it was weighed for; Extended
// says the job actually grew work to close it; Refused names, in the words the
// user would be told, why it did not. A citation that was extended on is spent
// — the same words may not buy a second round — so the ledger that bounds the
// loop is exactly what replays out of the journal.
//
// Quotes is a list because a gap may be a failure of several things at once:
// the mechanical half of the gate names one citation per file the plan promised
// and the disk does not hold. Quote stays, holding the same citations joined,
// because it is what every existing reader and every already-written journal
// row has — see Cited, which is how the ledger reads either.
//
// Mechanical distinguishes those two halves, and it is recorded rather than
// inferred because the exit code depends on it. A refused gap from a model
// judge is the gate being wrong; a refused gap from the mechanical half is a
// file that is still not on disk, and no refusal of a citation makes it appear.
type DeliveryGate struct {
	Pass         bool     `json:"pass"`
	Gap          string   `json:"gap,omitempty"`
	PolishClosed bool     `json:"polish_closed"`
	Quote        string   `json:"quote,omitempty"`
	Quotes       []string `json:"quotes,omitempty"`
	Round        int      `json:"round,omitempty"`
	Extended     bool     `json:"extended,omitempty"`
	Refused      string   `json:"refused,omitempty"`
	Mechanical   bool     `json:"mechanical,omitempty"`

	// Unclosed says the gap STANDS: the repair that would have closed it was
	// never bought, so nothing ran and nothing about the shortfall changed.
	//
	// It is the distinction the exit code turns on, and it is recorded rather
	// than read out of the refusal sentence because those are two categorically
	// different refusals wearing the same field. A gap refused as ungrounded, or
	// as one somebody already paid to close, is the GATE being wrong and caught
	// at it — the deliverable stands whole. A gap whose repair a governor would
	// not fund, or that nothing could plan, is the gate being RIGHT and
	// unaffordable: the thing it named is still missing, and a run that hands
	// that over is handing over less than it promised. One measured run shipped
	// "Deliverable is empty - contains no implementation" over exit 0 because
	// the two were one field (2026-08-28, meta/muse-spark-1.1).
	Unclosed bool `json:"unclosed,omitempty"`
}

// Cited is the gate's citations however they were written down. A row recorded
// before the list existed carries only the joined line, and reading it as one
// citation is the honest reading of it: that is exactly what it was when it was
// written, and a ledger that treated it as nothing would hand an old job a
// fresh allowance on replay.
func (g DeliveryGate) Cited() []string {
	cited := make([]string, 0, len(g.Quotes))
	for _, quote := range g.Quotes {
		if quote = strings.TrimSpace(quote); quote != "" {
			cited = append(cited, quote)
		}
	}
	if len(cited) > 0 {
		return cited
	}
	if quote := strings.TrimSpace(g.Quote); quote != "" {
		return []string{quote}
	}
	return nil
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
	// Per citation, not on the list as a whole. The bound exists so one event
	// cannot carry an unbounded string, and a citation clipped to a share of a
	// budget it does not know the size of would be clipped mid-word — which is
	// a citation that no longer matches the words it was taken from.
	quotes := make([]string, 0, len(gate.Quotes))
	for _, quote := range gate.Quotes {
		if quote = bounded(strings.TrimSpace(quote), MaxDigestBytes); quote != "" {
			quotes = append(quotes, quote)
		}
	}
	gate.Quotes = quotes
	if len(gate.Quotes) == 0 {
		// An empty list and a nil one are the same fact, and only one of them
		// round-trips through the journal as the value it was given.
		gate.Quotes = nil
	}

	tx, err := s.beginWrite()
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
