package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// SelfReceipt is the durable cost-and-learning account for one settled
// OriginSelf splice. Cost is derived from ordinary usage rows; receipts never
// accept or land spend independently.
type SelfReceipt struct {
	Seq           int64     `json:"-"`
	Time          time.Time `json:"-"`
	NodeID        string    `json:"node_id"`
	Origin        string    `json:"origin"`
	Scope         string    `json:"scope"`
	Cost          float64   `json:"cost"`
	FactIDs       []int64   `json:"fact_ids"`
	SkillIDs      []int64   `json:"skill_ids"`
	Surprise      *float64  `json:"surprise,omitempty"`
	SurpriseDelta *float64  `json:"surprise_delta,omitempty"`
	Nothing       bool      `json:"nothing"`

	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
}

// SelfInquiryRetirement is the journaled explanation for automatically
// stopping a line of inquiry. Action says which existing retirement mechanism
// was used.
type SelfInquiryRetirement struct {
	Scope      string `json:"scope"`
	Origin     string `json:"origin"`
	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
}

const selfReceiptSchema = `
CREATE TABLE IF NOT EXISTS self_receipts (
    seq             INTEGER PRIMARY KEY REFERENCES events(seq),
    ts              TEXT NOT NULL,
    node_id         TEXT NOT NULL,
    origin          TEXT NOT NULL,
    scope           TEXT NOT NULL,
    cost            REAL NOT NULL DEFAULT 0,
    fact_ids        JSON NOT NULL DEFAULT '[]' CHECK (json_valid(fact_ids)),
    skill_ids       JSON NOT NULL DEFAULT '[]' CHECK (json_valid(skill_ids)),
    surprise        REAL,
    surprise_delta  REAL,
    learned_nothing INTEGER NOT NULL DEFAULT 0 CHECK (learned_nothing IN (0, 1)),
    target_kind     TEXT NOT NULL DEFAULT '',
    target_id       TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS self_receipts_node ON self_receipts (node_id);
CREATE INDEX IF NOT EXISTS self_receipts_time ON self_receipts (ts, seq);
CREATE INDEX IF NOT EXISTS self_receipts_scope ON self_receipts (scope, seq);

CREATE TABLE IF NOT EXISTS self_inquiry_lines (
    scope                TEXT PRIMARY KEY,
    origin               TEXT NOT NULL,
    target_kind          TEXT NOT NULL DEFAULT '',
    target_id            TEXT NOT NULL DEFAULT '',
    consecutive_nothing  INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_nothing >= 0),
    retired              INTEGER NOT NULL DEFAULT 0 CHECK (retired IN (0, 1)),
    retirement_reason    TEXT NOT NULL DEFAULT '',
    updated_seq          INTEGER NOT NULL REFERENCES events(seq)
);
`

const selfInquiryNothingLimit = 2

const selfInquiryRetirementReason = "auto-retired after 2 consecutive self receipts learned nothing"

// SelfReceipts returns receipts at or after since in journal order. A zero
// time returns the full history.
func (s *Store) SelfReceipts(since time.Time) ([]SelfReceipt, error) {
	statement := `SELECT seq, ts, node_id, origin, scope, cost, fact_ids, skill_ids,
		surprise, surprise_delta, learned_nothing, target_kind, target_id
		FROM self_receipts`
	var args []any
	if !since.IsZero() {
		statement += ` WHERE ts >= ?`
		args = append(args, formatTime(since))
	}
	statement += ` ORDER BY seq`
	rows, err := s.db.Query(statement, args...)
	if err != nil {
		return nil, fmt.Errorf("self receipts: %w", err)
	}
	defer rows.Close()
	receipts := make([]SelfReceipt, 0)
	for rows.Next() {
		receipt, err := scanSelfReceipt(rows)
		if err != nil {
			return nil, fmt.Errorf("self receipts: %w", err)
		}
		receipts = append(receipts, receipt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("self receipts: %w", err)
	}
	return receipts, nil
}

// SelfSpendToday sums today's settled self-work receipts. The receipt is the
// attribution boundary, while its Cost came from the same usage table read by
// SpendToday.
func (s *Store) SelfSpendToday() (float64, error) {
	return s.selfSpendTodayAt(time.Now())
}

func (s *Store) selfSpendTodayAt(now time.Time) (float64, error) {
	start, end := localDayBounds(now)
	var spend float64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(cost), 0) FROM self_receipts
		WHERE ts >= ? AND ts < ?`, formatTime(start), formatTime(end)).Scan(&spend); err != nil {
		return 0, fmt.Errorf("self spend today: %w", err)
	}
	return spend, nil
}

// settleSelfReceiptForNodeTx emits a receipt once the atomic splice batch
// containing nodeID is wholly terminal. All nodes admitted by one splice share
// created_seq, which keeps later nested splices from being charged twice.
func settleSelfReceiptForNodeTx(tx *sql.Tx, nodeID string) error {
	var createdSeq int64
	var origin Origin
	if err := tx.QueryRow(`SELECT created_seq, origin FROM nodes WHERE id = ?`, nodeID).
		Scan(&createdSeq, &origin); err != nil {
		return err
	}
	if origin != OriginSelf || nodeID == RootID {
		return nil
	}
	var open int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes
		WHERE created_seq = ? AND status NOT IN (?, ?, ?)`, createdSeq, Done, Failed, Cancelled).
		Scan(&open); err != nil {
		return err
	}
	if open != 0 {
		return nil
	}

	var rootID, title, group, brief, intent, charterID string
	var trialOf int64
	if err := tx.QueryRow(`
		SELECT id, title, grp, brief, intent, charter_id, trial_of
		FROM nodes AS candidate
		WHERE created_seq = ?
		ORDER BY CASE WHEN parent_id IN (SELECT id FROM nodes WHERE created_seq = ?) THEN 1 ELSE 0 END,
		         created_order, id
		LIMIT 1`, createdSeq, createdSeq).
		Scan(&rootID, &title, &group, &brief, &intent, &charterID, &trialOf); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM self_receipts WHERE node_id = ?)`, rootID).
		Scan(&exists); err != nil || exists {
		return err
	}

	receipt := SelfReceipt{
		NodeID:  rootID,
		FactIDs: make([]int64, 0), SkillIDs: make([]int64, 0),
	}
	receipt.Origin, receipt.Scope, receipt.TargetKind, receipt.TargetID =
		selfReceiptIdentity(tx, rootID, title, group, brief, intent, charterID, trialOf)
	if err := tx.QueryRow(`SELECT COALESCE(SUM(usage.cost), 0)
		FROM usage JOIN nodes ON nodes.id = usage.node_id
		WHERE nodes.created_seq = ?`, createdSeq).Scan(&receipt.Cost); err != nil {
		return err
	}

	factRows, err := tx.Query(`SELECT facts.seq, facts.kind
		FROM facts JOIN nodes ON nodes.id = facts.node_id
		WHERE nodes.created_seq = ? ORDER BY facts.seq`, createdSeq)
	if err != nil {
		return err
	}
	for factRows.Next() {
		var seq int64
		var kind FactKind
		if err := factRows.Scan(&seq, &kind); err != nil {
			factRows.Close()
			return err
		}
		if kind == FactSkill {
			receipt.SkillIDs = append(receipt.SkillIDs, seq)
		} else {
			receipt.FactIDs = append(receipt.FactIDs, seq)
		}
	}
	if err := factRows.Close(); err != nil {
		return err
	}

	var surprise sql.NullFloat64
	if err := tx.QueryRow(`SELECT AVG(surprises.surprise)
		FROM surprises JOIN nodes ON nodes.id = surprises.node_id
		WHERE nodes.created_seq = ?`, createdSeq).Scan(&surprise); err != nil {
		return err
	}
	if surprise.Valid {
		receipt.Surprise = selfReceiptFloat(surprise.Float64)
		var previous sql.NullFloat64
		err := tx.QueryRow(`SELECT surprise FROM self_receipts
			WHERE scope = ? AND surprise IS NOT NULL ORDER BY seq DESC LIMIT 1`, receipt.Scope).
			Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if previous.Valid {
			delta := previous.Float64 - surprise.Float64
			if math.Abs(delta) < 1e-12 {
				delta = 0
			}
			receipt.SurpriseDelta = selfReceiptFloat(delta)
		}
	}
	receipt.Nothing = len(receipt.FactIDs) == 0 && len(receipt.SkillIDs) == 0 &&
		(receipt.SurpriseDelta == nil || *receipt.SurpriseDelta <= 0)

	seq, at, err := appendEvent(tx, rootID, EventSelfReceipt, receipt)
	if err != nil {
		return err
	}
	receipt.Seq, receipt.Time = seq, at
	if err := applySelfReceiptView(tx, receipt, seq, at); err != nil {
		return err
	}
	return maybeRetireSelfInquiryTx(tx, receipt)
}

func selfReceiptIdentity(tx *sql.Tx, nodeID, title, group, brief, intent, charterID string,
	trialOf int64) (origin, scope, targetKind, targetID string) {
	if charterID != "" {
		origin = charterID
		var invariant string
		if err := tx.QueryRow(`SELECT invariant FROM charters WHERE id = ?`, charterID).Scan(&invariant); err == nil {
			origin = firstCharterLine(invariant)
		} else if err := tx.QueryRow(`SELECT invariant FROM legacy_charters WHERE id = ?`, charterID).Scan(&invariant); err == nil {
			origin = firstCharterLine(invariant)
		}
		return origin, "charter:" + charterID, "charter", charterID
	}
	if trialOf > 0 {
		origin = "question #" + strconv.FormatInt(trialOf, 10)
		var factScope string
		if err := tx.QueryRow(`SELECT scope FROM facts WHERE seq = ?`, trialOf).Scan(&factScope); err == nil {
			origin += " · " + factScope
		}
		return origin, "fact:" + strconv.FormatInt(trialOf, 10), "fact", strconv.FormatInt(trialOf, 10)
	}
	origin = firstNonEmpty(strings.TrimSpace(intent), strings.TrimSpace(title), strings.TrimSpace(group),
		strings.TrimSpace(brief), nodeID)
	origin = bounded(strings.SplitN(origin, "\n", 2)[0], 240)
	key := strings.ToLower(strings.Join(strings.Fields(intent), " "))
	if key == "" {
		key = strings.ToLower(firstNonEmpty(strings.TrimSpace(group), strings.TrimSpace(title), nodeID))
	}
	return origin, "self:" + bounded(key, MaxDigestBytes-5), "", ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func maybeRetireSelfInquiryTx(tx *sql.Tx, receipt SelfReceipt) error {
	var consecutive int
	var retired bool
	if err := tx.QueryRow(`SELECT consecutive_nothing, retired FROM self_inquiry_lines WHERE scope = ?`,
		receipt.Scope).Scan(&consecutive, &retired); err != nil {
		return err
	}
	if retired || consecutive < selfInquiryNothingLimit {
		return nil
	}

	action := "line_retired"
	switch receipt.TargetKind {
	case "charter":
		charter, err := charterInTx(tx, receipt.TargetID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && charter.Status == CharterActive {
			payload := charterStatusPayload{
				Status: CharterPaused,
				Ratification: Ratification{
					Origin:    OriginSelf,
					SessionID: charter.Ratification.SessionID,
					Evidence:  selfInquiryRetirementReason,
				},
			}
			seq, _, err := appendEvent(tx, receipt.TargetID, EventCharterStatusChanged, payload)
			if err != nil {
				return err
			}
			if err := applyCharterStatus(tx, receipt.TargetID, payload, seq); err != nil {
				return err
			}
			action = "charter_paused"
		}
	case "fact":
		factSeq, err := strconv.ParseInt(receipt.TargetID, 10, 64)
		if err == nil {
			var status string
			lookupErr := tx.QueryRow(`SELECT status FROM facts WHERE seq = ?`, factSeq).Scan(&status)
			if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
				return lookupErr
			}
			if lookupErr == nil && status != FactSuperseded {
				payload := factSupersededPayload{FactSeq: factSeq, Reason: selfInquiryRetirementReason}
				seq, _, err := appendEvent(tx, "", EventFactSuperseded, payload)
				if err != nil {
					return err
				}
				if err := applyFactSupersession(tx, payload, seq); err != nil {
					return err
				}
				action = "fact_retired"
			}
		}
	}
	retirement := SelfInquiryRetirement{
		Scope: receipt.Scope, Origin: receipt.Origin,
		TargetKind: receipt.TargetKind, TargetID: receipt.TargetID,
		Action: action, Reason: selfInquiryRetirementReason,
	}
	seq, _, err := appendEvent(tx, receipt.NodeID, EventSelfInquiryRetired, retirement)
	if err != nil {
		return err
	}
	return applySelfInquiryRetirement(tx, retirement, seq)
}

func applySelfReceiptView(tx *sql.Tx, receipt SelfReceipt, seq int64, at time.Time) error {
	facts, err := json.Marshal(receipt.FactIDs)
	if err != nil {
		return err
	}
	skills, err := json.Marshal(receipt.SkillIDs)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO self_receipts (
		seq, ts, node_id, origin, scope, cost, fact_ids, skill_ids, surprise,
		surprise_delta, learned_nothing, target_kind, target_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), receipt.NodeID, receipt.Origin, receipt.Scope, receipt.Cost,
		string(facts), string(skills), nullableFloat(receipt.Surprise),
		nullableFloat(receipt.SurpriseDelta), receipt.Nothing, receipt.TargetKind, receipt.TargetID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO self_inquiry_lines (
		scope, origin, target_kind, target_id, consecutive_nothing, updated_seq
	) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(scope) DO UPDATE SET
		origin=excluded.origin,
		target_kind=excluded.target_kind,
		target_id=excluded.target_id,
		consecutive_nothing=CASE WHEN excluded.consecutive_nothing=1
			THEN self_inquiry_lines.consecutive_nothing+1 ELSE 0 END,
		updated_seq=excluded.updated_seq`,
		receipt.Scope, receipt.Origin, receipt.TargetKind, receipt.TargetID, boolInt(receipt.Nothing), seq)
	return err
}

func applySelfInquiryRetirement(tx *sql.Tx, retirement SelfInquiryRetirement, seq int64) error {
	result, err := tx.Exec(`UPDATE self_inquiry_lines
		SET retired=1, retirement_reason=?, updated_seq=? WHERE scope=? AND retired=0`,
		retirement.Reason, seq, retirement.Scope)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("self inquiry %q is missing or retired", retirement.Scope)
	}
	return nil
}

func scanSelfReceipt(scanner rowScanner) (SelfReceipt, error) {
	var receipt SelfReceipt
	var timestamp, facts, skills string
	var surprise, delta sql.NullFloat64
	if err := scanner.Scan(&receipt.Seq, &timestamp, &receipt.NodeID, &receipt.Origin,
		&receipt.Scope, &receipt.Cost, &facts, &skills, &surprise, &delta,
		&receipt.Nothing, &receipt.TargetKind, &receipt.TargetID); err != nil {
		return SelfReceipt{}, err
	}
	var err error
	receipt.Time, err = parseTime(timestamp)
	if err != nil {
		return SelfReceipt{}, err
	}
	if err := json.Unmarshal([]byte(facts), &receipt.FactIDs); err != nil {
		return SelfReceipt{}, err
	}
	if err := json.Unmarshal([]byte(skills), &receipt.SkillIDs); err != nil {
		return SelfReceipt{}, err
	}
	if surprise.Valid {
		receipt.Surprise = selfReceiptFloat(surprise.Float64)
	}
	if delta.Valid {
		receipt.SurpriseDelta = selfReceiptFloat(delta.Float64)
	}
	return receipt, nil
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func selfReceiptFloat(value float64) *float64 { return &value }

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// recordSelfReceipt is useful to the few store operations which synthesize a
// complete lifecycle in one transaction (territory folds). Ordinary runners
// reach the same code through Complete, Fail, or cancellation.
func recordSelfReceipt(tx *sql.Tx, nodeID string) error {
	return settleSelfReceiptForNodeTx(tx, nodeID)
}
