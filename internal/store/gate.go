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

	// Overturned says the refusal was CHECKED AGAINST THE WORLD and the finding
	// lost: the file the review says is missing is on disk under the name the
	// request used, or the things it says are absent are in the delivered text.
	//
	// It is the other half of the distinction Unclosed opened, and it is the one
	// the exit code should have been reading all along. Refused holds refusals
	// of two categorically different kinds. One looks at the filesystem or at
	// the deliverable and finds the review wrong — that acquits, and charging it
	// a non-zero code would teach a harness to distrust the gate's own
	// corrections. The other looks only at where the review's words came from
	// and declines to BUY a round; it settles nothing about whether the work
	// landed, because no ruling on a citation makes missing work appear.
	//
	// Recorded rather than inferred from the sentence, for the reason every
	// other field here is: the exit code turns on it, and a sentence is not a
	// field. Seven of eight measured runs exited 0 over a provenance refusal
	// while the review that named the missing work was right every time
	// (2026-08-28, bench/deepswe; see docs/design/gate/SETTLEMENT.md §2).
	//
	// THE WORLD IS THE DISK, A READING, OR THE RECORD — NEVER THE DELIVERABLE'S
	// OWN PROSE. The deliverable is the component the gate is checking, and a
	// refusal that reads it is FAILSAFE clause 2 broken in the strict sense the
	// clause states it. One measured run set this field because the words of the
	// request appeared in a summary the worker had written about work it had not
	// done, and shipped 1 of 20 hidden checks over exit 0 (2026-08-29,
	// bench/deepswe textual s5; SETTLEMENT.md §6). The delivered text may settle
	// a finding only where it IS the whole of what the run left behind — a
	// question answered in prose, whose message is its own artifact.
	Overturned bool `json:"overturned,omitempty"`

	// Exercised is the acceptance mapping as the gate settled it: one row per
	// behaviour the request stated, naming the check that exercises it, or
	// naming nothing when no check does.
	//
	// It is recorded rather than reduced to the finding it produced, because the
	// mapping is the evidence and the finding is only its conclusion. A run that
	// passed with every point exercised and a run that passed because the
	// checklist was empty are the same event without it, and telling those two
	// apart is the whole of what an autopsy of this mechanism has to do.
	Exercises []ExercisedPoint `json:"exercises,omitempty"`

	// Unmeasured says the gate held a checklist and could settle none of it:
	// the project declares no verification this run could read and the change
	// produced no readable diff, so nothing could be matched to what the
	// request asked for.
	//
	// It is a field rather than a silence because NOBODY LOOKED IS NOT NOTHING
	// WRONG, and the two are the same event without it. A delivery that
	// satisfied every point and one that was measured against nothing both
	// journal a passing gate; only this tells them apart, and an autopsy of
	// this mechanism has nothing else to read.
	Unmeasured string `json:"unmeasured,omitempty"`
}

// ExercisedPoint is one row of that mapping: a behaviour the request stated and
// the check that exercises it. An empty Check is the finding — nothing in the
// project's own verification touches this.
type ExercisedPoint struct {
	Point string `json:"point"`
	Check string `json:"check,omitempty"`
}

// Whole is THE reading of what this gate settled, and it is a method because it
// had been two readings.
//
// Three fields say the delivery stands: the first judgement passed; the one
// permitted repair was re-judged and passed (PolishClosed); or the finding was
// weighed against the world and lost (Overturned). Everything else leaves the
// finding STANDING — a fail nothing repaired, a refusal about where a review got
// its words, a gap nothing could fund, a promised file the disk does not hold.
//
// It lives here, on the event, because two readers spent this differently and
// disagreed out loud. deliveredWhole in cmd/aforge/do.go combined all three
// fields to decide the exit code; gateWords, in the same file, built the line a
// person watching reads from Pass and Refused alone — so ink s5 and ofetch s5
// printed "gate: fail — The deliverable is a listing of files, not the answer
// itself" as the last thing anybody saw and left with exit 0
// (2026-08-29, bench/deepswe; docs/design/gate/SETTLEMENT.md §7). A verdict a
// person reads and a verdict an exit code carries are one fact, and one fact is
// one reading.
func (g DeliveryGate) Whole() bool {
	return g.Pass || g.PolishClosed || g.Overturned
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

// EventAcceptance is the acceptance checklist journaled against the piece of
// work it will be used to judge: the behaviours the person's own request states,
// read from the request before any work began.
//
// It is a first-class event beside the plan blob for the reason EventNodeBrief
// is: the checklist lives on plan.Spec, inside a document held in memory for the
// life of a run, and "what was this work actually asked for" is exactly the
// question an autopsy of a finished run needs answered from the run's own
// journal. It is also what the headless stream reads to say the checklist exists
// at all — a fail-safe that does not reach the person watching is decoration
// (docs/design/failsafe/FAILSAFE.md clause 3).
const EventAcceptance EventKind = "acceptance"

// AcceptancePoint is one behaviour the request states, and the words of the
// request it is a reading of.
//
// The store learns no more about a point than that, and deliberately: Quote is
// what the grounding rule weighs and Behaviour is what a person reads, and the
// rules that weigh them live where the gate lives. This is the journal's copy.
type AcceptancePoint struct {
	Behaviour string `json:"behaviour"`
	Quote     string `json:"quote"`
}

// Acceptance is the whole checklist for one piece of work.
type Acceptance struct {
	Points []AcceptancePoint `json:"points"`
}

// RecordAcceptance journals the checklist against the node whose delivery it
// governs. An empty checklist writes nothing: a request that states no checkable
// behaviour has no checklist, and an event saying so would be a row every reader
// has to learn to ignore.
func (s *Store) RecordAcceptance(nodeID string, acceptance Acceptance) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record acceptance: %w: empty node id", ErrInvalid)
	}
	points := make([]AcceptancePoint, 0, len(acceptance.Points))
	for _, point := range acceptance.Points {
		point.Behaviour = bounded(strings.TrimSpace(point.Behaviour), MaxDigestBytes)
		point.Quote = bounded(strings.TrimSpace(point.Quote), MaxDigestBytes)
		// Bounded per point rather than over the list, for the reason the gate's
		// citations are: a quotation clipped to a share of a budget it does not
		// know the size of is clipped mid-word, and a citation that no longer
		// matches the words it was taken from grounds against nothing.
		if point.Behaviour == "" || point.Quote == "" {
			continue
		}
		points = append(points, point)
	}
	if len(points) == 0 {
		return nil
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	if _, _, err := appendEvent(tx, nodeID, EventAcceptance, Acceptance{Points: points}); err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record acceptance: %w", err)
	}
	return nil
}

// AcceptanceFor returns the newest checklist journaled for a node. It reads the
// event directly, exactly as DeliveryGateFor does and for the same reason: the
// payload is sparse, looked up by id, and has no query anyone would run across
// it.
func (s *Store) AcceptanceFor(nodeID string) (Acceptance, bool, error) {
	var payload string
	err := s.db.QueryRow(`
		SELECT payload FROM events
		WHERE node_id = ? AND kind = ?
		ORDER BY seq DESC LIMIT 1`, nodeID, EventAcceptance).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Acceptance{}, false, nil
	}
	if err != nil {
		return Acceptance{}, false, fmt.Errorf("read acceptance: %w", err)
	}
	var acceptance Acceptance
	if err := json.Unmarshal([]byte(payload), &acceptance); err != nil {
		return Acceptance{}, false, fmt.Errorf("read acceptance: %w", err)
	}
	return acceptance, true, nil
}
