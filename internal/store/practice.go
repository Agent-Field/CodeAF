package store

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// PracticeGroup is the scheduler marker for disposable, lowest-priority
// curriculum work. It is deliberately not an organizational group.
const PracticeGroup = "practice"

// PracticeCharterShape identifies the one programmatic standing-intent family
// that the deterministic practice sentinel owns. The ordinary watch engine
// must not send these poll wakes to a model sentinel.
const PracticeCharterShape = "system:practice-loop"

const (
	EventQuestionStatusChanged     EventKind = "question_status_changed"
	EventQuestionPracticeStarted   EventKind = "question_practice_started"
	EventQuestionPracticeCompleted EventKind = "question_practice_completed"
)

const (
	// LearningProgressWindow is the recent/prior residual window per scope.
	LearningProgressWindow = 4
	// LearningProgressHorizon is the sample count after which zero progress receives no curiosity budget.
	LearningProgressHorizon = 2 * LearningProgressWindow
	// LearningProgressEpsilon is the cold-start allocation floor for unexplored scopes.
	LearningProgressEpsilon = 0.05
)

type questionStatusPayload struct {
	QuestionSeq int64  `json:"question_seq"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

// QuestionPracticeStarted is the durable link between one question and the
// self-origin subtree admitted to exercise it.
type QuestionPracticeStarted struct {
	QuestionSeq      int64   `json:"question_seq"`
	JobID            string  `json:"job_id"`
	BaselineSurprise float64 `json:"baseline_surprise"`
	ExpectedTokens   int     `json:"expected_tokens"`
}

type questionPracticeCompleted struct {
	QuestionSeq    int64   `json:"question_seq"`
	JobID          string  `json:"job_id"`
	ResultSurprise float64 `json:"result_surprise"`
	Reduced        bool    `json:"reduced"`
	Status         string  `json:"status"`
	Reason         string  `json:"reason"`
}

// QuestionPractice is one round, including unfinished rounds recovered after
// restart. CompletionSeq zero means the job still needs an outcome landing.
type QuestionPractice struct {
	StartSeq         int64
	QuestionSeq      int64
	JobID            string
	BaselineSurprise float64
	ExpectedTokens   int
	ResultSurprise   *float64
	Reduced          *bool
	CompletionSeq    int64
	Reason           string
}

// ScopeSurprise is the journal-derived learning signal for one notebook
// scope. Surprise is averaged over the newest requested sample window while
// relevance counts all settled user jobs and territories carrying the scope.
type ScopeSurprise struct {
	Scope            string
	Samples          int
	SettledJobs      int
	Territories      int
	AverageSurprise  float64
	ExpectedTokens   int
	Evidence         string
	RecentSurprise   float64
	PriorSurprise    float64
	LearningProgress float64
	ColdStart        bool
	Allocation       float64
}

// AllocateLearningProgress normalizes only positive improvement plus a cold-start floor.
func AllocateLearningProgress(metrics []ScopeSurprise) []ScopeSurprise {
	allocated := append([]ScopeSurprise(nil), metrics...)
	total := 0.0
	for index := range allocated {
		weight := math.Max(0, allocated[index].LearningProgress)
		if allocated[index].ColdStart {
			weight = math.Max(weight, LearningProgressEpsilon)
		}
		allocated[index].Allocation = weight
		total += weight
	}
	if total > 0 {
		for index := range allocated {
			allocated[index].Allocation /= total
		}
	}
	return allocated
}

// RecordQuestion journals one open knowledge gap. A scope has at most one
// question over its lifetime: resolved and retired gaps are hysteresis, not an
// invitation for a periodic scan to manufacture the same curriculum again.
func (s *Store) RecordQuestion(nodeID, scope, body string) (Fact, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Fact{}, fmt.Errorf("record question: %w: empty gap", ErrInvalid)
	}
	if strings.ContainsAny(body, "\r\n") {
		return Fact{}, fmt.Errorf("record question: %w: gap must be one line", ErrInvalid)
	}
	scope = normalizeScope(scope)
	resolved, err := resolveScope(s.db, scope)
	if err != nil {
		return Fact{}, fmt.Errorf("record question: %w", err)
	}
	existing, err := s.factsWhere(`kind = ? AND scope = ? ORDER BY seq DESC LIMIT 1`, FactQuestion, resolved)
	if err != nil {
		return Fact{}, fmt.Errorf("record question: %w", err)
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	return s.recordFact(FactWriterOther, nodeID, resolved, FactQuestion, body, nil, 0, QuestionOpen, "", "", false)
}

// Questions lists knowledge gaps in newest-first order. Empty status includes
// the complete question history.
func (s *Store) Questions(status string, limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 100
	}
	if status == "" {
		return s.factsWhere(`kind = ? ORDER BY seq DESC LIMIT ?`, FactQuestion, limit)
	}
	if !validFactStatusForKind(FactQuestion, status) {
		return nil, fmt.Errorf("query questions: %w: invalid status %q", ErrInvalid, status)
	}
	return s.factsWhere(`kind = ? AND status = ? ORDER BY seq DESC LIMIT ?`,
		FactQuestion, status, limit)
}

func applyQuestionStatus(tx *sql.Tx, payload questionStatusPayload, seq int64) error {
	if !validFactStatusForKind(FactQuestion, payload.Status) {
		return fmt.Errorf("invalid question status %q", payload.Status)
	}
	result, err := tx.Exec(`UPDATE facts SET status=?, status_note=?, status_seq=?
		WHERE seq=? AND kind=?`, payload.Status, payload.Reason, seq,
		payload.QuestionSeq, FactQuestion)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("question %d is missing", payload.QuestionSeq)
	}
	return nil
}

func applyQuestionPracticeStarted(tx *sql.Tx, payload QuestionPracticeStarted, seq int64) error {
	if payload.QuestionSeq <= 0 || strings.TrimSpace(payload.JobID) == "" ||
		payload.BaselineSurprise <= 0 || math.IsNaN(payload.BaselineSurprise) ||
		math.IsInf(payload.BaselineSurprise, 0) || payload.ExpectedTokens < 0 {
		return fmt.Errorf("invalid question practice start")
	}
	var status string
	if err := tx.QueryRow(`SELECT status FROM facts WHERE seq=? AND kind=?`,
		payload.QuestionSeq, FactQuestion).Scan(&status); err != nil {
		return err
	}
	if status != QuestionOpen {
		return fmt.Errorf("question %d is %s, not open", payload.QuestionSeq, status)
	}
	if err := requireNode(tx, payload.JobID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO question_practices
		(start_seq, question_seq, job_id, baseline_surprise, expected_tokens)
		VALUES (?, ?, ?, ?, ?)`, seq, payload.QuestionSeq, payload.JobID,
		payload.BaselineSurprise, payload.ExpectedTokens); err != nil {
		return err
	}
	payloadStatus := questionStatusPayload{QuestionSeq: payload.QuestionSeq,
		Status: QuestionPracticing, Reason: "practice job " + payload.JobID}
	return applyQuestionStatus(tx, payloadStatus, seq)
}

// CompleteQuestionPractice lands one measured round and deterministically
// chooses the next question state. A ten-percent improvement clears the gap;
// two measured non-improvements retire it as noisy television.
func (s *Store) CompleteQuestionPractice(jobID string, resultSurprise float64) (string, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" || resultSurprise < 0 || math.IsNaN(resultSurprise) || math.IsInf(resultSurprise, 0) {
		return "", fmt.Errorf("complete question practice: %w: invalid outcome", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return "", fmt.Errorf("complete question practice: %w", err)
	}
	defer tx.Rollback()
	var start QuestionPracticeStarted
	var completion sql.NullInt64
	if err := tx.QueryRow(`SELECT question_seq, job_id, baseline_surprise, expected_tokens, completion_seq
		FROM question_practices WHERE job_id=?`, jobID).Scan(&start.QuestionSeq,
		&start.JobID, &start.BaselineSurprise, &start.ExpectedTokens, &completion); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("complete question practice: %w: job %q", ErrNotFound, jobID)
		}
		return "", fmt.Errorf("complete question practice: %w", err)
	}
	if completion.Valid {
		var status string
		if err := tx.QueryRow(`SELECT status FROM facts WHERE seq=?`, start.QuestionSeq).Scan(&status); err != nil {
			return "", err
		}
		return status, nil
	}
	var currentStatus string
	if err := tx.QueryRow(`SELECT status FROM facts WHERE seq=?`, start.QuestionSeq).Scan(&currentStatus); err != nil {
		return "", fmt.Errorf("complete question practice: %w", err)
	}
	if currentStatus != QuestionPracticing {
		return "", fmt.Errorf("complete question practice: %w: question %d is %s, not practicing",
			ErrInvalid, start.QuestionSeq, currentStatus)
	}
	reduced := resultSurprise <= start.BaselineSurprise*0.9
	status := QuestionResolved
	reason := fmt.Sprintf("surprise fell from %.3f to %.3f in practice job %s",
		start.BaselineSurprise, resultSurprise, jobID)
	if !reduced {
		var earlier int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM question_practices
			WHERE question_seq=? AND completion_seq IS NOT NULL AND reduced=0`, start.QuestionSeq).Scan(&earlier); err != nil {
			return "", fmt.Errorf("complete question practice: %w", err)
		}
		if earlier+1 >= 2 {
			status = QuestionRetired
			reason = fmt.Sprintf("no surprise reduction after %d practice rounds (baseline %.3f, latest %.3f)",
				earlier+1, start.BaselineSurprise, resultSurprise)
		} else {
			status = QuestionOpen
			reason = fmt.Sprintf("practice round %d did not reduce surprise (baseline %.3f, result %.3f)",
				earlier+1, start.BaselineSurprise, resultSurprise)
		}
	}
	payload := questionPracticeCompleted{QuestionSeq: start.QuestionSeq, JobID: jobID,
		ResultSurprise: resultSurprise, Reduced: reduced, Status: status, Reason: reason}
	seq, _, err := appendEvent(tx, jobID, EventQuestionPracticeCompleted, payload)
	if err != nil {
		return "", fmt.Errorf("complete question practice: %w", err)
	}
	if err := applyQuestionPracticeCompleted(tx, payload, seq); err != nil {
		return "", fmt.Errorf("complete question practice: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("complete question practice: %w", err)
	}
	return status, nil
}

func applyQuestionPracticeCompleted(tx *sql.Tx, payload questionPracticeCompleted, seq int64) error {
	if !validFactStatusForKind(FactQuestion, payload.Status) ||
		payload.Status == QuestionPracticing || payload.ResultSurprise < 0 {
		return fmt.Errorf("invalid question practice completion")
	}
	result, err := tx.Exec(`UPDATE question_practices SET result_surprise=?, reduced=?,
		completion_seq=?, reason=? WHERE question_seq=? AND job_id=? AND completion_seq IS NULL`,
		payload.ResultSurprise, payload.Reduced, seq, payload.Reason,
		payload.QuestionSeq, payload.JobID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("practice job %q is missing or complete", payload.JobID)
	}
	return applyQuestionStatus(tx, questionStatusPayload{QuestionSeq: payload.QuestionSeq,
		Status: payload.Status, Reason: payload.Reason}, seq)
}

// IncompleteQuestionPractices returns every admitted round still waiting for a
// terminal job and a surprise measurement.
func (s *Store) IncompleteQuestionPractices() ([]QuestionPractice, error) {
	return s.questionPractices(`completion_seq IS NULL ORDER BY start_seq`)
}

// QuestionPractices lists every round for one question.
func (s *Store) QuestionPractices(questionSeq int64) ([]QuestionPractice, error) {
	return s.questionPractices(`question_seq=? ORDER BY start_seq`, questionSeq)
}

func (s *Store) questionPractices(where string, args ...any) ([]QuestionPractice, error) {
	rows, err := s.db.Query(`SELECT start_seq, question_seq, job_id, baseline_surprise,
		expected_tokens, result_surprise, reduced, completion_seq, reason
		FROM question_practices WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query question practices: %w", err)
	}
	defer rows.Close()
	var rounds []QuestionPractice
	for rows.Next() {
		var round QuestionPractice
		var result sql.NullFloat64
		var reduced sql.NullBool
		var completion sql.NullInt64
		if err := rows.Scan(&round.StartSeq, &round.QuestionSeq, &round.JobID,
			&round.BaselineSurprise, &round.ExpectedTokens, &result, &reduced,
			&completion, &round.Reason); err != nil {
			return nil, fmt.Errorf("query question practices: %w", err)
		}
		if result.Valid {
			value := result.Float64
			round.ResultSurprise = &value
		}
		if reduced.Valid {
			value := reduced.Bool
			round.Reduced = &value
		}
		if completion.Valid {
			round.CompletionSeq = completion.Int64
		}
		rounds = append(rounds, round)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query question practices: %w", err)
	}
	return rounds, nil
}

// UserIdle is the cheap reconciler gate: one indexed materialized-view check
// for live user work and one journal timestamp lookup for the quiet period.
func (s *Store) UserIdle(now time.Time, quietFor time.Duration) (bool, error) {
	if quietFor < 0 {
		return false, fmt.Errorf("user idle: %w: negative quiet period", ErrInvalid)
	}
	var inFlight bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM nodes
		WHERE origin=? AND folded=0 AND status IN (?, ?, ?))`, OriginUser,
		Pending, Claimed, Running).Scan(&inFlight); err != nil {
		return false, fmt.Errorf("user idle: %w", err)
	}
	if inFlight {
		return false, nil
	}
	if quietFor == 0 {
		return true, nil
	}
	var latest sql.NullString
	if err := s.db.QueryRow(`SELECT MAX(ts) FROM events WHERE kind=?
		AND json_extract(payload, '$.provenance.origin')=?`,
		EventSubtreeSpliced, OriginUser).Scan(&latest); err != nil {
		return false, fmt.Errorf("user idle: %w", err)
	}
	if !latest.Valid {
		return true, nil
	}
	at, err := parseTime(latest.String)
	if err != nil {
		return false, fmt.Errorf("user idle: %w", err)
	}
	return !now.Before(at.Add(quietFor)), nil
}

// ScopeSurprises joins journaled residuals back to the scopes distilled from
// their settled user jobs. Only the newest minSamples residuals determine the
// current error level; older jobs still contribute to relevance.
func (s *Store) ScopeSurprises(minSamples int) ([]ScopeSurprise, error) {
	if minSamples <= 0 {
		return nil, fmt.Errorf("scope surprises: %w: positive sample floor required", ErrInvalid)
	}
	rows, err := s.db.Query(`
		WITH RECURSIVE job_roots(job_id, territory_id, intent, brief, summary, error, fold_digest) AS (
			SELECT node.id,
			       CASE WHEN parent.grp=? THEN parent.id ELSE '' END,
			       node.intent, node.brief, node.summary, node.error, node.fold_digest
			FROM nodes AS node
			LEFT JOIN nodes AS parent ON parent.id=node.parent_id
			WHERE node.grp<>? AND node.origin=?
			  AND node.status IN (?, ?, ?)
			  AND (node.parent_id=? OR parent.grp=?)
		), descendants(job_id, node_id) AS (
			SELECT job_id, job_id FROM job_roots
			UNION ALL
			SELECT descendants.job_id, child.id
			FROM descendants JOIN nodes AS child ON child.parent_id=descendants.node_id
		), job_scopes(job_id, scope, fact_evidence) AS (
			SELECT descendants.job_id, facts.scope, GROUP_CONCAT(facts.body, ' ')
			FROM descendants JOIN facts ON facts.node_id=descendants.node_id
			WHERE facts.kind<>?
			GROUP BY descendants.job_id, facts.scope
		)
		SELECT job_scopes.scope, job_roots.job_id, job_roots.territory_id,
		       surprises.seq, surprises.surprise, surprises.expected_tokens,
		       job_roots.intent || ' ' || job_roots.brief || ' ' || job_roots.summary ||
		       ' ' || job_roots.error || ' ' || job_roots.fold_digest || ' ' || job_scopes.fact_evidence
		FROM job_scopes
		JOIN job_roots ON job_roots.job_id=job_scopes.job_id
		JOIN descendants ON descendants.job_id=job_scopes.job_id
		JOIN surprises ON surprises.node_id=descendants.node_id
		ORDER BY job_scopes.scope, surprises.seq DESC`, TerritoryGroup, TerritoryGroup,
		OriginUser, Done, Failed, Cancelled, RootID, TerritoryGroup, FactQuestion)
	if err != nil {
		return nil, fmt.Errorf("scope surprises: %w", err)
	}
	defer rows.Close()
	type sample struct {
		seq      int64
		surprise float64
		expected int
	}
	type collected struct {
		samples     []sample
		jobs        map[string]bool
		territories map[string]bool
		evidence    []string
	}
	byScope := make(map[string]*collected)
	for rows.Next() {
		var scope, jobID, territoryID, evidence string
		var current sample
		if err := rows.Scan(&scope, &jobID, &territoryID, &current.seq,
			&current.surprise, &current.expected, &evidence); err != nil {
			return nil, fmt.Errorf("scope surprises: %w", err)
		}
		bucket := byScope[scope]
		if bucket == nil {
			bucket = &collected{jobs: make(map[string]bool), territories: make(map[string]bool)}
			byScope[scope] = bucket
		}
		bucket.samples = append(bucket.samples, current)
		if !bucket.jobs[jobID] {
			bucket.jobs[jobID] = true
			if trimmed := strings.TrimSpace(evidence); trimmed != "" {
				bucket.evidence = append(bucket.evidence, trimmed)
			}
		}
		if territoryID != "" {
			bucket.territories[territoryID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scope surprises: %w", err)
	}
	metrics := make([]ScopeSurprise, 0, len(byScope))
	for scope, bucket := range byScope {
		if len(bucket.samples) < minSamples {
			continue
		}
		sort.SliceStable(bucket.samples, func(i, j int) bool {
			return bucket.samples[i].seq > bucket.samples[j].seq
		})
		window := min(len(bucket.samples), max(minSamples, LearningProgressHorizon))
		latest := bucket.samples[:window]
		var surprise float64
		var expected int
		recentCount := min(len(latest), LearningProgressWindow)
		var recent, prior float64
		for index, current := range latest {
			surprise += current.surprise
			expected += current.expected
			if index < recentCount {
				recent += current.surprise
			} else if index < recentCount+LearningProgressWindow {
				prior += current.surprise
			}
		}
		recent /= float64(max(recentCount, 1))
		priorCount := min(max(len(latest)-recentCount, 0), LearningProgressWindow)
		if priorCount > 0 {
			prior /= float64(priorCount)
		}
		progress := 0.0
		if priorCount == LearningProgressWindow {
			progress = prior - recent
		}
		metrics = append(metrics, ScopeSurprise{
			Scope: scope, Samples: len(latest), SettledJobs: len(bucket.jobs),
			Territories: len(bucket.territories), AverageSurprise: surprise / float64(len(latest)),
			ExpectedTokens: expected / len(latest), Evidence: strings.Join(bucket.evidence, " "),
			RecentSurprise: recent, PriorSurprise: prior, LearningProgress: progress,
			ColdStart: len(latest) < LearningProgressHorizon,
		})
	}
	metrics = AllocateLearningProgress(metrics)
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Scope < metrics[j].Scope })
	return metrics, nil
}

// IsPracticeCharter keeps the ordinary model sentinel from handling the
// deterministic practice poll.
func IsPracticeCharter(charter Charter) bool {
	return charter.ProposalShape == PracticeCharterShape
}
