package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	tx, err := s.beginWrite()
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
	_, proposedAt, err := appendEvent(tx, id, EventCharterFiringProposed, payload)
	if err != nil {
		return false, err
	}
	body := fmt.Sprintf("I would have done %s now — approve? You can also always allow this.", intent)
	options := []QuestionOption{
		{Label: "approve this time", Value: "charter:fire:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
		{Label: "not now", Value: "charter:decline:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
		{Label: "always allow", Value: "charter:always:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
		{Label: "never", Value: "charter:never:" + id + ":" + strconv.FormatInt(wakeSeq, 10)},
	}
	// The window makes silence answerable. Without it a proposal nobody ever
	// looked at pinned the wake open forever, so the charter could neither fire
	// nor move on to its next due moment and the ladder simply stopped — the
	// one response the ladder could not read was the most common one.
	question := agentQuestionPayload{
		Text: body, OriginCharterID: id, Urgency: QuestionNextNaturalMoment, Options: options,
		ExpiresAt: proposedAt.Add(ProbationProposalWindow),
	}
	questionSeq, questionAt, err := appendEvent(tx, id, EventAgentQuestionQueued, question)
	if err != nil {
		return false, err
	}
	if err := applyAgentQuestionView(tx, question, questionSeq, questionAt); err != nil {
		return false, err
	}
	// Firing proposals belong to the charter rather than the session that
	// happened to create it. The linked question is likewise neutral and the
	// read paths make neutral work available to whichever live session answers.
	message := messagePayload{Role: RoleAgent, Body: bounded(body, MaxMessageBytes),
		NodeID: id, QuestionSeq: questionSeq, Options: options}
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

// ProbationProposalWindow is how long "I would have done X now — approve?"
// stands before silence is read as a no.
//
// The number is the same reasoning as a clarifying question's window and lands
// on the same answer: long enough to survive a night away from the machine, so
// a proposal made at 09:00 is still approvable when the user sits down at
// 08:00 the next morning, and short enough that yesterday's moment has not
// quietly become the day after tomorrow's.
const ProbationProposalWindow = 20 * time.Hour

// CharterFiringRefusals counts every firing this charter proposed and did not
// get: explicit declines, and proposals that stood past their window with no
// answer. Both are the user saying no; one of them just costs less to say.
//
// The ladder measured whether work completed and never whether it mattered, so
// a watch the user waved away four times could still be promoted to
// unsupervised firing on its next three approvals — the refusals reset the
// consecutive run and left no other trace. This is that trace.
func (s *Store) CharterFiringRefusals(id string) (int, error) {
	tx, err := s.beginWrite()
	if err != nil {
		return 0, fmt.Errorf("count charter firing refusals: %w", err)
	}
	defer tx.Rollback()
	return charterFiringRefusalsTx(tx, id)
}

func charterFiringRefusalsTx(tx *sql.Tx, id string) (int, error) {
	var declined, lapsed int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=?`,
		id, EventCharterFiringDeclined).Scan(&declined); err != nil {
		return 0, fmt.Errorf("count charter firing declines: %w", err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM agent_questions
		WHERE origin_charter_id=? AND status=?`, id, QuestionExpired).Scan(&lapsed); err != nil {
		return 0, fmt.Errorf("count charter firing lapses: %w", err)
	}
	return declined + lapsed, nil
}

// LapsedCharterFiringProposal reports whether the proposal standing on this
// wake has run past its window unanswered. It is the read the watch pass makes
// before re-offering a firing nobody responded to.
func (s *Store) LapsedCharterFiringProposal(id string, wakeSeq int64, now time.Time) (bool, error) {
	var timestamp string
	err := s.db.QueryRow(`SELECT ts FROM events WHERE node_id=? AND kind=?
		AND json_extract(payload, '$.wake_seq')=? ORDER BY seq DESC LIMIT 1`,
		id, EventCharterFiringProposed, wakeSeq).Scan(&timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read charter firing proposal: %w", err)
	}
	at, err := parseTime(timestamp)
	if err != nil {
		return false, fmt.Errorf("read charter firing proposal: %w", err)
	}
	return !now.Before(at.Add(ProbationProposalWindow)), nil
}

// CharterHygieneAsked mirrors ServiceHygieneAsked exactly: the durable question
// is the memory, and asking twice about the same watch is what it prevents.
func (s *Store) CharterHygieneAsked(charterID string) (bool, error) {
	var asked bool
	err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM agent_questions, json_each(agent_questions.options)
		WHERE json_extract(json_each.value, '$.value') = ?)`,
		CharterHygieneKeepValue(strings.TrimSpace(charterID))).Scan(&asked)
	if err != nil {
		return false, fmt.Errorf("read charter hygiene memory: %w", err)
	}
	return asked, nil
}

// CharterHygieneKeepValue is the durable option value that marks a watch as
// already questioned. The stop half deliberately reuses the ordinary retire
// code, so the answer travels the route that already exists.
func CharterHygieneKeepValue(charterID string) string {
	return "charter:hygiene-keep:" + charterID
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
	tx, err := s.beginWrite()
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
	tx, err := s.beginWrite()
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
	tx, err := s.beginWrite()
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
	tx, err := s.beginWrite()
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

	// Every refusal raises the bar rather than merely resetting the run. A
	// watch the user keeps waving away is not one they want handled unasked,
	// however cleanly the firings they did approve happened to complete, and
	// the bar is the only place that judgment can live: mechanical success is
	// all AssessCharterFiring can see. The rise is one firing per refusal, so
	// a watch that is genuinely wanted still earns its tenure — it just has to
	// earn back what it spent.
	refusals, err := charterFiringRefusalsTx(tx, assessment.CharterID)
	if err != nil {
		return false, err
	}
	if assessment.Success && charter.Autonomy == CharterProbation && green >= tenureAfter+refusals {
		reason := fmt.Sprintf("%d consecutive approved and verified-green firings", green)
		if refusals > 0 {
			reason += fmt.Sprintf(" against %d refused", refusals)
		}
		promotion := charterAutonomyPayload{From: CharterProbation, To: CharterTenured,
			Reason: reason, GreenFirings: green, Demotions: charter.Demotions}
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
	// A job the ladder already recorded cannot change the ladder again, so the
	// short-circuit RecordCharterFiringOutcome makes at review time belongs here
	// too: an assessment nobody can act on should not be paid for.
	var reviewed bool
	if err := s.db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM events WHERE node_id=? AND kind=? AND json_extract(payload, '$.job_id')=?)`,
		node.Provenance.CharterID, EventCharterFiringReviewed, root.ID).Scan(&reviewed); err != nil {
		return CharterFiringAssessment{}, false, err
	}
	if reviewed {
		return CharterFiringAssessment{}, false, nil
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
	// The verdict only ever reads the fired subtree, so the children map is
	// built from that subtree instead of the whole graph. Both the scoped query
	// and Nodes order by admission, so the walk visits the same nodes in the
	// same order.
	nodes, err := s.SubtreeNodes(root.ID)
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
	gates, err := latestDeliveryGates(s.db, subtree)
	if err != nil {
		return CharterFiringAssessment{}, false, err
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
		raw, ok := gates[candidate.ID]
		var gate DeliveryGate
		if ok {
			if err := json.Unmarshal([]byte(raw), &gate); err != nil {
				return CharterFiringAssessment{}, false, fmt.Errorf("read delivery gate: %w", err)
			}
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

// latestDeliveryGates answers DeliveryGateFor for a whole subtree in one query
// instead of one per node. Payloads are returned undecoded so the caller keeps
// decoding in walk order: a malformed gate on a node the walk never reaches
// stays as invisible as it was before.
func latestDeliveryGates(db *sql.DB, nodes []Node) (map[string]string, error) {
	gates := make(map[string]string, len(nodes))
	if len(nodes) == 0 {
		return gates, nil
	}
	placeholders := make([]string, 0, len(nodes))
	args := []any{EventDeliveryGate}
	for _, node := range nodes {
		placeholders = append(placeholders, "?")
		args = append(args, node.ID)
	}
	rows, err := db.Query(`SELECT node_id, payload FROM events WHERE seq IN (
		SELECT MAX(seq) FROM events WHERE kind = ? AND node_id IN (`+
		strings.Join(placeholders, ", ")+`) GROUP BY node_id)`, args...)
	if err != nil {
		return nil, fmt.Errorf("read delivery gates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, payload string
		if err := rows.Scan(&nodeID, &payload); err != nil {
			return nil, fmt.Errorf("read delivery gates: %w", err)
		}
		gates[nodeID] = payload
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read delivery gates: %w", err)
	}
	return gates, nil
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
