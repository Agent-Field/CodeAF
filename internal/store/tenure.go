package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type charterFiringProposalPayload struct {
	WakeSeq int64  `json:"wake_seq"`
	Intent  string `json:"intent"`
}

type charterFiringDeclinedPayload struct {
	WakeSeq int64  `json:"wake_seq"`
	Reason  string `json:"reason"`
	Pause   bool   `json:"pause,omitempty"`
}

type charterFiringReviewPayload struct {
	WakeSeq      int64   `json:"wake_seq"`
	JobID        string  `json:"job_id"`
	Success      bool    `json:"success"`
	Reason       string  `json:"reason"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	GreenFirings int     `json:"green_firings"`
}

type charterAutonomyPayload struct {
	From         CharterAutonomy `json:"from"`
	To           CharterAutonomy `json:"to"`
	Reason       string          `json:"reason"`
	GreenFirings int             `json:"green_firings"`
	Demotions    int             `json:"demotions"`
	Paused       bool            `json:"paused,omitempty"`
	Override     bool            `json:"override,omitempty"`
}

// CharterFiringAssessment is the independent, mechanical verdict used by the
// resident after a firing's graph or usage changes.
type CharterFiringAssessment struct {
	CharterID string
	WakeSeq   int64
	JobID     string
	Decided   bool
	Success   bool
	Reason    string
	CostUSD   float64
}

// ProposeCharterFiring posts exactly one selectable probation question for a
// checked-yes wake. The question and its proposal marker land atomically.
func (s *Store) ProposeCharterFiring(id string, wakeSeq int64, intent string) (bool, error) {
	intent = strings.TrimSpace(intent)
	if intent == "" {
		return false, fmt.Errorf("propose charter firing: %w: intent is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	charter, err := charterInTx(tx, id)
	if err != nil {
		return false, err
	}
	if charter.Status != CharterActive || charter.Autonomy != CharterProbation ||
		!charter.WakePending || !charter.SentinelYes || charter.WakeSeq != wakeSeq {
		return false, fmt.Errorf("propose charter firing: %w: no probation wake %d", ErrInvalid, wakeSeq)
	}
	var posted bool
	if err := tx.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM events WHERE node_id=? AND kind=?
		AND json_extract(payload, '$.wake_seq')=?)`, id, EventCharterFiringProposed, wakeSeq).Scan(&posted); err != nil {
		return false, err
	}
	if posted {
		return false, nil
	}
	payload := charterFiringProposalPayload{WakeSeq: wakeSeq, Intent: bounded(intent, MaxDigestBytes)}
	if _, _, err := appendEvent(tx, id, EventCharterFiringProposed, payload); err != nil {
		return false, err
	}
	body := fmt.Sprintf("I would have done %s now — approve? You can also always allow this.", intent)
	options := []QuestionOption{
		{Label: "approve this time", Value: "charter:fire:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
		{Label: "not now", Value: "charter:decline:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
		{Label: "always allow", Value: "charter:always:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
		{Label: "never", Value: "charter:never:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
	}
	message := messagePayload{SessionID: charter.SessionID, Role: RoleAgent, Body: bounded(body, MaxMessageBytes),
		NodeID: id, Options: options}
	seq, at, err := appendEvent(tx, id, EventMessagePosted, message)
	if err != nil {
		return false, err
	}
	if err := applyMessageView(tx, message, seq, at); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// CharterFiringApproved reports the durable one-wake authorization used to
// resume a daily-rail-deferred probation firing without asking twice.
func (s *Store) CharterFiringApproved(id string, wakeSeq int64) (bool, error) {
	instruction := "wake:" + strconv.FormatInt(wakeSeq, 10)
	var approved bool
	err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM commands WHERE target=? AND kind=? AND instruction=? AND status=?)`,
		id, CommandCharterFire, instruction, CommandApplied).Scan(&approved)
	if err != nil {
		return false, fmt.Errorf("find charter firing approval: %w", err)
	}
	return approved, nil
}

// CharterWakeFired is the crash-retry guard for an approval command whose
// FireCharter transaction committed before ResolveCommand did.
func (s *Store) CharterWakeFired(id string, wakeSeq int64) (bool, error) {
	var fired bool
	err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM events WHERE node_id=? AND kind=? AND json_extract(payload, '$.wake_seq')=?)`,
		id, EventCharterFired, wakeSeq).Scan(&fired)
	if err != nil {
		return false, fmt.Errorf("find charter firing: %w", err)
	}
	return fired, nil
}

// DeclineCharterFiring closes a probation wake and resets consecutive green
// evidence. Never uses pause=true; an ordinary decline leaves the charter armed.
func (s *Store) DeclineCharterFiring(id string, wakeSeq int64, reason string, pause bool) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "user declined the probation firing"
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	charter, err := charterInTx(tx, id)
	if err != nil {
		return err
	}
	if charter.Autonomy != CharterProbation || !charter.WakePending || charter.WakeSeq != wakeSeq {
		return fmt.Errorf("decline charter firing: %w: stale probation wake", ErrInvalid)
	}
	payload := charterFiringDeclinedPayload{WakeSeq: wakeSeq, Reason: bounded(reason, MaxDigestBytes), Pause: pause}
	seq, _, err := appendEvent(tx, id, EventCharterFiringDeclined, payload)
	if err != nil {
		return err
	}
	if err := applyCharterFiringDeclined(tx, id, payload, seq); err != nil {
		return err
	}
	return tx.Commit()
}

func applyCharterFiringDeclined(tx *sql.Tx, id string, payload charterFiringDeclinedPayload, seq int64) error {
	status := CharterActive
	if payload.Pause {
		status = CharterPaused
	}
	result, err := tx.Exec(`UPDATE charters SET status=?, green_firings=0, wake_pending=0,
		sentinel_yes=0, updated_seq=? WHERE id=? AND wake_seq=?`, status, seq, id, payload.WakeSeq)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("decline charter firing: %w: stale wake", ErrInvalid)
	}
	return nil
}

// PromoteCharter records a user override of the earned ladder. Automatic
// promotion uses the same payload with Override=false from the review path.
func (s *Store) PromoteCharter(id, reason string, override bool) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("promote charter: %w: reason is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	charter, err := charterInTx(tx, id)
	if err != nil {
		return err
	}
	if charter.Autonomy == CharterTenured {
		return nil
	}
	if charter.Autonomy != CharterProbation {
		return fmt.Errorf("promote charter: %w: unknown autonomy %q", ErrInvalid, charter.Autonomy)
	}
	payload := charterAutonomyPayload{From: CharterProbation, To: CharterTenured,
		Reason: bounded(reason, MaxDigestBytes), GreenFirings: charter.GreenFirings,
		Demotions: charter.Demotions, Override: override}
	seq, _, err := appendEvent(tx, id, EventCharterPromoted, payload)
	if err != nil {
		return err
	}
	if err := applyCharterAutonomy(tx, id, payload, seq); err != nil {
		return err
	}
	if override {
		if err := postCharterNotice(tx, charter, id, "I'll handle this on my own now — say 'back to asking' to revert"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReturnCharterToProbation is the conversational "back to asking" path. It is
// not a failure demotion and therefore does not consume the two-strike pause.
func (s *Store) ReturnCharterToProbation(id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "user asked to return to supervised firing"
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	charter, err := charterInTx(tx, id)
	if err != nil {
		return err
	}
	if charter.Autonomy == CharterProbation {
		return nil
	}
	payload := charterAutonomyPayload{From: CharterTenured, To: CharterProbation,
		Reason: bounded(reason, MaxDigestBytes), Demotions: charter.Demotions}
	seq, _, err := appendEvent(tx, id, EventCharterDemoted, payload)
	if err != nil {
		return err
	}
	if err := applyCharterAutonomy(tx, id, payload, seq); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordCharterFiringOutcome admits one independent verification result. A job
// can affect the ladder once even when several terminal/usage events mention it.
func (s *Store) RecordCharterFiringOutcome(assessment CharterFiringAssessment, tenureAfter int) (bool, error) {
	if !assessment.Decided || strings.TrimSpace(assessment.CharterID) == "" || strings.TrimSpace(assessment.JobID) == "" {
		return false, fmt.Errorf("review charter firing: %w: decided charter/job is required", ErrInvalid)
	}
	if tenureAfter <= 0 {
		tenureAfter = 3
	}
	reason := strings.TrimSpace(assessment.Reason)
	if reason == "" {
		if assessment.Success {
			reason = "firing completed successfully within its rails"
		} else {
			reason = "firing failed independent verification"
		}
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var reviewed bool
	if err := tx.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM events WHERE node_id=? AND kind=? AND json_extract(payload, '$.job_id')=?)`,
		assessment.CharterID, EventCharterFiringReviewed, assessment.JobID).Scan(&reviewed); err != nil {
		return false, err
	}
	if reviewed {
		return false, nil
	}
	charter, err := charterInTx(tx, assessment.CharterID)
	if err != nil {
		return false, err
	}
	var firedPayload string
	if err := tx.QueryRow(`SELECT payload FROM events WHERE node_id=? AND kind=?
		AND json_extract(payload, '$.job_id')=? AND json_extract(payload, '$.wake_seq')=?
		ORDER BY seq DESC LIMIT 1`, assessment.CharterID, EventCharterFired,
		assessment.JobID, assessment.WakeSeq).Scan(&firedPayload); errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("review charter firing: %w: job is not a journaled firing", ErrInvalid)
	} else if err != nil {
		return false, err
	}
	var firing charterFiringPayload
	if err := json.Unmarshal([]byte(firedPayload), &firing); err != nil {
		return false, err
	}
	if charter.Autonomy == CharterProbation && assessment.Success && !firing.ProbationApproved {
		return false, fmt.Errorf("review charter firing: %w: probation success was not user-approved", ErrInvalid)
	}
	green := charter.GreenFirings
	if assessment.Success && charter.Autonomy == CharterProbation {
		green++
	} else if !assessment.Success {
		green = 0
	}
	review := charterFiringReviewPayload{WakeSeq: assessment.WakeSeq, JobID: assessment.JobID,
		Success: assessment.Success, Reason: bounded(reason, MaxDigestBytes), CostUSD: assessment.CostUSD,
		GreenFirings: green}
	reviewSeq, _, err := appendEvent(tx, assessment.CharterID, EventCharterFiringReviewed, review)
	if err != nil {
		return false, err
	}
	if err := applyCharterFiringReview(tx, assessment.CharterID, review, reviewSeq); err != nil {
		return false, err
	}

	if assessment.Success && charter.Autonomy == CharterProbation && green >= tenureAfter {
		promotion := charterAutonomyPayload{From: CharterProbation, To: CharterTenured,
			Reason:       fmt.Sprintf("%d consecutive approved and verified-green firings", green),
			GreenFirings: green, Demotions: charter.Demotions}
		seq, _, err := appendEvent(tx, assessment.CharterID, EventCharterPromoted, promotion)
		if err != nil {
			return false, err
		}
		if err := applyCharterAutonomy(tx, assessment.CharterID, promotion, seq); err != nil {
			return false, err
		}
		if err := postCharterNotice(tx, charter, assessment.CharterID,
			"I'll handle this on my own now — say 'back to asking' to revert"); err != nil {
			return false, err
		}
	}

	if !assessment.Success && charter.Autonomy == CharterTenured {
		demotions := charter.Demotions + 1
		paused := demotions >= 2
		demotion := charterAutonomyPayload{From: CharterTenured, To: CharterProbation,
			Reason: bounded(reason, MaxDigestBytes), Demotions: demotions, Paused: paused}
		seq, _, err := appendEvent(tx, assessment.CharterID, EventCharterDemoted, demotion)
		if err != nil {
			return false, err
		}
		if err := applyCharterAutonomy(tx, assessment.CharterID, demotion, seq); err != nil {
			return false, err
		}
		notice := "I'm back to asking before I do this — " + firstCharterLine(reason)
		if paused {
			notice = "I paused this charter after its second demotion — " + firstCharterLine(reason)
		}
		if err := postCharterNotice(tx, charter, assessment.CharterID, notice); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func applyCharterFiringReview(tx *sql.Tx, id string, payload charterFiringReviewPayload, seq int64) error {
	result, err := tx.Exec(`UPDATE charters SET green_firings=?, updated_seq=? WHERE id=?`,
		payload.GreenFirings, seq, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("review charter firing: %w: charter %q", ErrNotFound, id)
	}
	return nil
}

func applyCharterAutonomy(tx *sql.Tx, id string, payload charterAutonomyPayload, seq int64) error {
	status := CharterActive
	if payload.Paused {
		status = CharterPaused
	}
	result, err := tx.Exec(`UPDATE charters SET autonomy=?, green_firings=?, demotions=?,
		status=CASE WHEN ? THEN ? ELSE status END, wake_pending=CASE WHEN ? THEN 0 ELSE wake_pending END,
		sentinel_yes=CASE WHEN ? THEN 0 ELSE sentinel_yes END, updated_seq=? WHERE id=?`,
		payload.To, payload.GreenFirings, payload.Demotions, payload.Paused, status,
		payload.Paused, payload.Paused, seq, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("change charter autonomy: %w: charter %q", ErrNotFound, id)
	}
	return nil
}

func postCharterNotice(tx *sql.Tx, charter Charter, id, body string) error {
	message := messagePayload{SessionID: charter.SessionID, Role: RoleAgent,
		Body: bounded(body, MaxMessageBytes), NodeID: id}
	seq, at, err := appendEvent(tx, id, EventMessagePosted, message)
	if err != nil {
		return err
	}
	return applyMessageView(tx, message, seq, at)
}

// AssessCharterFiring derives a verdict from current graph state. Failures,
// cancellation, a rejected final gate, or a rail breach decide immediately;
// success waits for the entire firing subtree to settle green.
func (s *Store) AssessCharterFiring(nodeID string) (CharterFiringAssessment, bool, error) {
	node, found, err := s.Node(nodeID)
	if err != nil || !found || node.Provenance.CharterID == "" {
		return CharterFiringAssessment{}, false, err
	}
	root := node
	for root.Parent != RootID && root.Parent != "" {
		parent, ok, err := s.Node(root.Parent)
		if err != nil {
			return CharterFiringAssessment{}, false, err
		}
		if !ok || parent.Provenance.CharterID != node.Provenance.CharterID {
			break
		}
		root = parent
	}
	var raw string
	err = s.db.QueryRow(`SELECT payload FROM events WHERE node_id=? AND kind=?
		AND json_extract(payload, '$.job_id')=? ORDER BY seq DESC LIMIT 1`,
		node.Provenance.CharterID, EventCharterFired, root.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return CharterFiringAssessment{}, false, nil
	}
	if err != nil {
		return CharterFiringAssessment{}, false, err
	}
	var firing charterFiringPayload
	if err := json.Unmarshal([]byte(raw), &firing); err != nil {
		return CharterFiringAssessment{}, false, err
	}
	assessment := CharterFiringAssessment{CharterID: node.Provenance.CharterID,
		WakeSeq: firing.WakeSeq, JobID: root.ID}
	charter, found, err := s.Charter(assessment.CharterID)
	if err != nil || !found {
		return CharterFiringAssessment{}, false, err
	}
	usage, err := s.TopLevelJobUsage()
	if err != nil {
		return CharterFiringAssessment{}, false, err
	}
	assessment.CostUSD = usage[root.ID].Cost
	if assessment.CostUSD > charter.Rails().PerFiringBudgetUSD {
		assessment.Decided = true
		assessment.Reason = fmt.Sprintf("per-firing budget breached: spent $%.4f over $%.4f rail",
			assessment.CostUSD, charter.Rails().PerFiringBudgetUSD)
		return assessment, true, nil
	}
	nodes, err := s.Nodes()
	if err != nil {
		return CharterFiringAssessment{}, false, err
	}
	children := make(map[string][]Node)
	for _, candidate := range nodes {
		children[candidate.Parent] = append(children[candidate.Parent], candidate)
	}
	subtree := []Node{root}
	for index := 0; index < len(subtree); index++ {
		subtree = append(subtree, children[subtree[index].ID]...)
	}
	allTerminal := true
	for _, candidate := range subtree {
		switch candidate.Status {
		case Failed, Cancelled:
			assessment.Decided = true
			failure := strings.TrimSpace(candidate.Error)
			if failure == "" {
				failure = string(candidate.Status)
			}
			if strings.Contains(strings.ToLower(failure), "cancel") || candidate.Status == Cancelled {
				assessment.Reason = "firing cancelled by user: " + firstCharterLine(failure)
			} else {
				assessment.Reason = "fired subtree failed: " + firstCharterLine(failure)
			}
			return assessment, true, nil
		case Done:
		default:
			allTerminal = false
		}
		gate, ok, err := s.DeliveryGateFor(candidate.ID)
		if err != nil {
			return CharterFiringAssessment{}, false, err
		}
		if ok && !gate.Pass && !gate.PolishClosed {
			assessment.Decided = true
			assessment.Reason = "firing output rejected: " + firstCharterLine(gate.Gap)
			return assessment, true, nil
		}
	}
	if allTerminal {
		assessment.Decided, assessment.Success = true, true
		assessment.Reason = "firing completed successfully within its rails"
	}
	return assessment, assessment.Decided, nil
}

func replayTenureEvent(tx *sql.Tx, event Event) error {
	switch event.Kind {
	case EventCharterFiringProposed:
		var payload charterFiringProposalPayload
		return json.Unmarshal(event.Payload, &payload)
	case EventCharterFiringDeclined:
		var payload charterFiringDeclinedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterFiringDeclined(tx, event.NodeID, payload, event.Seq)
	case EventCharterFiringReviewed:
		var payload charterFiringReviewPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterFiringReview(tx, event.NodeID, payload, event.Seq)
	case EventCharterPromoted, EventCharterDemoted:
		var payload charterAutonomyPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterAutonomy(tx, event.NodeID, payload, event.Seq)
	default:
		return nil
	}
}

// prepareCharterSchema reconciles the two standing branches that predate the
// unified engine. Materialized charter rows are disposable; their events are
// replayed after an incompatible head-only table is replaced.
func prepareCharterSchema(db *sql.DB) (bool, error) {
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='charters')`).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		_, err := db.Exec(charterSchema)
		return false, err
	}
	hasWatch, err := tableHasColumn(db, "charters", "watch")
	if err != nil {
		return false, err
	}
	if !hasWatch {
		tx, err := db.Begin()
		if err != nil {
			return false, err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`DROP TABLE IF EXISTS charters_fts; DROP TABLE charters;`); err != nil {
			return false, err
		}
		if _, err := tx.Exec(charterSchema); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	columns := []struct {
		name string
		sql  string
	}{
		{"autonomy", `ALTER TABLE charters ADD COLUMN autonomy TEXT NOT NULL DEFAULT 'probation' CHECK (autonomy IN ('probation', 'tenured'))`},
		{"green_firings", `ALTER TABLE charters ADD COLUMN green_firings INTEGER NOT NULL DEFAULT 0 CHECK (green_firings >= 0)`},
		{"demotions", `ALTER TABLE charters ADD COLUMN demotions INTEGER NOT NULL DEFAULT 0 CHECK (demotions >= 0)`},
		{"session_id", `ALTER TABLE charters ADD COLUMN session_id TEXT NOT NULL DEFAULT ''`},
		{"spec", `ALTER TABLE charters ADD COLUMN spec JSON NOT NULL DEFAULT '{}' CHECK (json_valid(spec))`},
		{"source_command_seq", `ALTER TABLE charters ADD COLUMN source_command_seq INTEGER NOT NULL DEFAULT 0`},
		{"created_at", `ALTER TABLE charters ADD COLUMN created_at TEXT NOT NULL DEFAULT ''`},
	}
	for _, column := range columns {
		found, err := tableHasColumn(db, "charters", column.name)
		if err != nil {
			return false, err
		}
		if !found {
			if _, err := db.Exec(column.sql); err != nil {
				return false, err
			}
		}
	}
	if _, err := db.Exec(charterSchema); err != nil {
		return false, err
	}
	if _, err := db.Exec(`DELETE FROM charters_fts;
		INSERT INTO charters_fts (charter_id, invariant) SELECT id, invariant FROM charters`); err != nil {
		return false, err
	}
	return false, nil
}
