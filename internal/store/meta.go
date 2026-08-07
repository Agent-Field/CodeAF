package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	// ChannelPriorSamples is the empirical-Bayes prior strength shared with profile shrinkage.
	ChannelPriorSamples = 8
	// LowCredibilityThreshold makes a weak channel earn a second occurrence before prompt use.
	LowCredibilityThreshold = 0.45
	// CredibilityHighThreshold labels the calm high-confidence notebook word.
	CredibilityHighThreshold = 0.80
	// CredibilitySteadyThreshold labels the middle notebook confidence word.
	CredibilitySteadyThreshold = 0.60

	// VOIMinSamples prevents interruption policy from moving on anecdotes.
	VOIMinSamples = 8
	// VOIRegretThreshold is the normalized regret below which a default is assumed.
	VOIRegretThreshold = 0.05
	// VOIUnmeasuredReworkCost is one normalized unit when no dollar rework is attributable.
	VOIUnmeasuredReworkCost = 1.0

	// ActivationDefaultDecay is the ACT-R prior and low-sample fallback.
	ActivationDefaultDecay = 0.50
	// ActivationMinDecay is the hard lower rail for learned decay exponents.
	ActivationMinDecay = 0.10
	// ActivationMaxDecay is the hard upper rail for learned decay exponents.
	ActivationMaxDecay = 0.90
	// ActivationMinRevisits is the evidence floor before a kind can move off its prior.
	ActivationMinRevisits = 8
	// ActivationMinimumAge avoids infinite activation for an access at the current instant.
	ActivationMinimumAge = time.Minute

	// ProposalCadenceMinRuns is the eager-user floor between proposal passes.
	ProposalCadenceMinRuns = 1
	// ProposalCadenceMaxRuns is the quiet-user ceiling between proposal passes.
	ProposalCadenceMaxRuns = 4
	// CorrectionImmediateWindow separates immediate from batched correction behavior.
	CorrectionImmediateWindow = 10 * time.Minute
)

// ChannelSurvival is the rebuildable survival projection for one kind/channel sensor.
type ChannelSurvival struct {
	Kind        FactKind
	Channel     FactChannel
	N           int
	Survived    int
	Reversed    int
	Raw         float64
	Global      float64
	Credibility float64
}

// ShrunkRate applies n/(n+8) shrinkage toward a global empirical rate.
func ShrunkRate(local, global float64, n int) float64 {
	if n <= 0 {
		return global
	}
	weight := float64(n) / float64(n+ChannelPriorSamples)
	return weight*local + (1-weight)*global
}

// ChannelSurvivalStats derives every rate from fact lifecycle events.
func (s *Store) ChannelSurvivalStats() ([]ChannelSurvival, error) {
	rows, err := s.db.Query(`
		WITH reversed AS (
			SELECT DISTINCT CAST(json_extract(payload, '$.fact_seq') AS INTEGER) AS fact_seq
			FROM events WHERE kind IN (?, ?)
		)
		SELECT f.kind, f.channel, COUNT(*),
		       SUM(CASE WHEN reversed.fact_seq IS NULL THEN 1 ELSE 0 END)
		FROM facts AS f LEFT JOIN reversed ON reversed.fact_seq=f.seq
		WHERE f.kind NOT IN (?, ?)
		GROUP BY f.kind, f.channel ORDER BY f.kind, f.channel`,
		EventFactSuperseded, EventFactQuarantined, FactQuestion, FactTrait)
	if err != nil {
		return nil, fmt.Errorf("channel survival: %w", err)
	}
	defer rows.Close()
	var stats []ChannelSurvival
	total, survived := 0, 0
	for rows.Next() {
		var stat ChannelSurvival
		if err := rows.Scan(&stat.Kind, &stat.Channel, &stat.N, &stat.Survived); err != nil {
			return nil, fmt.Errorf("channel survival: %w", err)
		}
		stat.Reversed = stat.N - stat.Survived
		stat.Raw = float64(stat.Survived) / float64(max(stat.N, 1))
		total += stat.N
		survived += stat.Survived
		stats = append(stats, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("channel survival: %w", err)
	}
	global := 1.0
	if total > 0 {
		global = float64(survived) / float64(total)
	}
	for index := range stats {
		stats[index].Global = global
		stats[index].Credibility = ShrunkRate(stats[index].Raw, global, stats[index].N)
	}
	return stats, nil
}

func (s *Store) credibilityFor(kind FactKind, channel FactChannel) (float64, error) {
	stats, err := s.ChannelSurvivalStats()
	if err != nil {
		return 0, err
	}
	global := 1.0
	if len(stats) > 0 {
		global = stats[0].Global
	}
	for _, stat := range stats {
		if stat.Kind == kind && stat.Channel == channel {
			return stat.Credibility, nil
		}
	}
	return global, nil
}

func (s *Store) applyFactCredibility(facts []Fact) {
	stats, err := s.ChannelSurvivalStats()
	if err != nil {
		return
	}
	global := 1.0
	if len(stats) > 0 {
		global = stats[0].Global
	}
	lookup := make(map[string]float64, len(stats))
	for _, stat := range stats {
		lookup[string(stat.Kind)+"\x00"+string(stat.Channel)] = stat.Credibility
	}
	for index := range facts {
		facts[index].Confidence = global
		if value, ok := lookup[string(facts[index].Kind)+"\x00"+string(facts[index].Channel)]; ok {
			facts[index].Confidence = value
		}
	}
}

// CredibilityWord renders one muted notebook word from a shrunk channel rate.
func CredibilityWord(value float64) string {
	switch {
	case value >= CredibilityHighThreshold:
		return "strong"
	case value >= CredibilitySteadyThreshold:
		return "steady"
	default:
		return "tentative"
	}
}

// PromptEligible applies the two-occurrence bar only to empirically weak channels.
func (s *Store) PromptEligible(fact Fact) (bool, error) {
	if fact.Kind == FactQuestion || fact.Kind == FactTrait || fact.Status != FactActive {
		return false, nil
	}
	credibility, err := s.credibilityFor(fact.Kind, fact.Channel)
	if err != nil || credibility >= LowCredibilityThreshold {
		return err == nil, err
	}
	var restored int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind=? AND CAST(json_extract(payload, '$.fact_seq') AS INTEGER)=?`,
		EventFactRestored, fact.Seq).Scan(&restored); err != nil {
		return false, fmt.Errorf("prompt fact restore evidence: %w", err)
	}
	if restored > 0 {
		return true, nil
	}
	if fact.Unsettled != nil {
		evidence := 0
		for _, approach := range fact.Unsettled.Approaches {
			evidence += len(approach.Evidence)
		}
		if evidence >= 2 {
			return true, nil
		}
	}
	var occurrences int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM facts
		WHERE kind=? AND channel=? AND scope=? AND lower(body)=lower(?) AND seq<=?`,
		fact.Kind, fact.Channel, fact.Scope, fact.Body, fact.Seq).Scan(&occurrences)
	if err != nil {
		return false, fmt.Errorf("prompt fact evidence: %w", err)
	}
	return occurrences >= 2, nil
}

// TraitMeasurement is the structured body stored by every trait fact.
type TraitMeasurement struct {
	Value   any       `json:"value"`
	N       int       `json:"n"`
	Updated time.Time `json:"updated"`
}

// RecordTrait supersedes the active singleton for name with one re-measurement.
func (s *Store) RecordTrait(name string, measurement TraitMeasurement) (Fact, error) {
	name = normalizeTraitName(name)
	if name == "" || measurement.N < 0 || measurement.Updated.IsZero() {
		return Fact{}, fmt.Errorf("record trait: %w: name, n, and updated are required", ErrInvalid)
	}
	body, err := json.Marshal(measurement)
	if err != nil {
		return Fact{}, fmt.Errorf("record trait: %w", err)
	}
	scope := "trait:" + name
	existing, err := s.factsWhere(`kind=? AND scope=? AND status=? ORDER BY seq DESC LIMIT 1`, FactTrait, scope, FactActive)
	if err != nil {
		return Fact{}, err
	}
	var replaces int64
	if len(existing) > 0 {
		replaces = existing[0].Seq
	}
	return s.recordFact(FactWriterDistiller, RootID, scope, FactTrait, string(body), nil,
		replaces, FactActive, "", false)
}

// Trait returns the current singleton measurement for name.
func (s *Store) Trait(name string) (TraitMeasurement, Fact, bool, error) {
	scope := "trait:" + normalizeTraitName(name)
	facts, err := s.factsWhere(`kind=? AND scope=? AND status=? ORDER BY seq DESC LIMIT 1`, FactTrait, scope, FactActive)
	if err != nil || len(facts) == 0 {
		return TraitMeasurement{}, Fact{}, false, err
	}
	var measurement TraitMeasurement
	if err := json.Unmarshal([]byte(facts[0].Body), &measurement); err != nil {
		return TraitMeasurement{}, Fact{}, false, fmt.Errorf("decode trait %q: %w", name, err)
	}
	return measurement, facts[0], true, nil
}

func normalizeTraitName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}), "-")
	return name
}

// QuestionCategory is the stable class used by the empirical ask gate.
type QuestionCategory string

const (
	QuestionCategoryCharterRatification QuestionCategory = "charter-ratification"
	QuestionCategorySurgeryConfirm      QuestionCategory = "surgery-confirm"
	QuestionCategoryCompileAssumption   QuestionCategory = "compile-assumption"
	QuestionCategoryRailRaise           QuestionCategory = "rail-raise"
	QuestionCategoryCharterCadence      QuestionCategory = "charter-cadence"
	QuestionCategoryGeneric             QuestionCategory = "generic"
)

// CategoryStats is the journal-derived acceptance and regret projection.
type CategoryStats struct {
	Category       QuestionCategory
	N              int
	Accepted       int
	Different      int
	AcceptanceRate float64
	MeanReworkCost float64
	ExpectedRegret float64
}

// QuestionCategoryStats measures answered durable questions by offered default.
func (s *Store) QuestionCategoryStats(category QuestionCategory) (CategoryStats, error) {
	stat := CategoryStats{Category: category}
	rows, err := s.db.Query(`SELECT default_answer, resolution, options
		FROM agent_questions WHERE category=? AND default_answer<>'' AND status=?`,
		category, QuestionAnswered)
	if err != nil {
		return stat, fmt.Errorf("question category stats: %w", err)
	}
	for rows.Next() {
		var offered, resolution, encoded string
		if err := rows.Scan(&offered, &resolution, &encoded); err != nil {
			return stat, fmt.Errorf("question category stats: %w", err)
		}
		var options []QuestionOption
		_ = json.Unmarshal([]byte(encoded), &options)
		stat.N++
		if questionAnswerMatchesDefault(resolution, offered, options) {
			stat.Accepted++
		} else {
			stat.Different++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return stat, fmt.Errorf("question category stats: %w", err)
	}
	if err := rows.Close(); err != nil {
		return stat, fmt.Errorf("question category stats: %w", err)
	}
	if stat.N > 0 {
		stat.AcceptanceRate = float64(stat.Accepted) / float64(stat.N)
	}
	stat.MeanReworkCost = s.meanCategoryReworkCost(category)
	if stat.MeanReworkCost <= 0 {
		stat.MeanReworkCost = VOIUnmeasuredReworkCost
	}
	stat.ExpectedRegret = float64(stat.Different) / float64(max(stat.N, 1)) * stat.MeanReworkCost
	return stat, nil
}

func questionAnswerMatchesDefault(answer, offered string, options []QuestionOption) bool {
	normalize := func(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
	answer, offered = normalize(answer), normalize(offered)
	if answer == offered {
		return true
	}
	for index, option := range options {
		if offered == normalize(option.Value) || offered == normalize(option.Label) || offered == fmt.Sprint(index+1) {
			return answer == normalize(option.Value) || answer == normalize(option.Label) || answer == fmt.Sprint(index+1)
		}
	}
	return false
}

func (s *Store) meanCategoryReworkCost(category QuestionCategory) float64 {
	// Dollar attribution is available only when a non-default answer is followed
	// by a user top-level job in the same session. Unmeasurable cases remain out.
	rows, err := s.db.Query(`SELECT session_id, answer_message_seq, default_answer, resolution, options
		FROM agent_questions WHERE category=? AND status=? AND answer_message_seq>0`, category, QuestionAnswered)
	if err != nil {
		return 0
	}
	type resolutionRow struct {
		session                      string
		answerSeq                    int64
		offered, resolution, encoded string
	}
	var resolved []resolutionRow
	for rows.Next() {
		var row resolutionRow
		if rows.Scan(&row.session, &row.answerSeq, &row.offered, &row.resolution, &row.encoded) == nil {
			resolved = append(resolved, row)
		}
	}
	if rows.Close() != nil {
		return 0
	}
	usage, err := s.TopLevelJobUsage()
	if err != nil {
		return 0
	}
	nodes, err := s.Nodes()
	if err != nil {
		return 0
	}
	var costs []float64
	appendNextCost := func(session string, afterSeq int64) {
		bestSeq := int64(math.MaxInt64)
		bestID := ""
		for _, node := range nodes {
			if node.Parent == RootID && node.Provenance.Origin == OriginUser &&
				node.Provenance.SessionID == session && node.CreatedSeq > afterSeq && node.CreatedSeq < bestSeq {
				bestSeq, bestID = node.CreatedSeq, node.ID
			}
		}
		if bestID != "" && usage[bestID].Cost > 0 {
			costs = append(costs, usage[bestID].Cost)
		}
	}
	for _, row := range resolved {
		var options []QuestionOption
		_ = json.Unmarshal([]byte(row.encoded), &options)
		if questionAnswerMatchesDefault(row.resolution, row.offered, options) {
			continue
		}
		appendNextCost(row.session, row.answerSeq)
	}
	// A skipped ask becomes a measurable wrong default when the next user turn
	// in that session differs, and the following user job carries actual spend.
	events, _ := s.Events(0, 0)
	messages, _ := s.Messages("", 0, 100000)
	for _, event := range events {
		if event.Kind != EventAssumedWithDefault {
			continue
		}
		var assumption assumedWithDefaultPayload
		if json.Unmarshal(event.Payload, &assumption) != nil || assumption.Category != category {
			continue
		}
		for _, message := range messages {
			if message.Seq <= event.Seq || message.SessionID != assumption.SessionID || message.Role != RoleUser {
				continue
			}
			if !questionAnswerMatchesDefault(message.Body, assumption.Default, nil) {
				appendNextCost(message.SessionID, message.Seq)
			}
			break
		}
	}
	if len(costs) == 0 {
		return 0
	}
	var total float64
	for _, cost := range costs {
		total += cost
	}
	return total / float64(len(costs))
}

// ShouldAsk applies empirical VOI while preserving consent-bearing exceptions.
func (s *Store) ShouldAsk(category QuestionCategory) (bool, CategoryStats, error) {
	stat, err := s.QuestionCategoryStats(category)
	if err != nil {
		return true, stat, err
	}
	if category == QuestionCategoryCharterRatification || category == QuestionCategoryRailRaise {
		return true, stat, nil
	}
	if stat.N < VOIMinSamples {
		return true, stat, nil
	}
	return stat.ExpectedRegret > VOIRegretThreshold, stat, nil
}

type assumedWithDefaultPayload struct {
	Category  QuestionCategory `json:"category"`
	Default   string           `json:"default"`
	SessionID string           `json:"session_id,omitempty"`
	Question  string           `json:"question,omitempty"`
}

// RecordAssumedWithDefault journals every skipped ask for later correction matching.
func (s *Store) RecordAssumedWithDefault(category QuestionCategory, defaultAnswer, sessionID, question string) error {
	defaultAnswer = strings.TrimSpace(defaultAnswer)
	if category == "" || defaultAnswer == "" {
		return fmt.Errorf("record assumed default: %w: category and default are required", ErrInvalid)
	}
	payload := assumedWithDefaultPayload{Category: category, Default: defaultAnswer,
		SessionID: strings.TrimSpace(sessionID), Question: bounded(strings.TrimSpace(question), MaxDigestBytes)}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, "", EventAssumedWithDefault, payload); err != nil {
		return err
	}
	return tx.Commit()
}

// BaseLevelActivation computes log(sum(t_i^-d)) using ages in days.
func BaseLevelActivation(accesses []time.Time, now time.Time, decay float64) float64 {
	decay = clampFloat(decay, ActivationMinDecay, ActivationMaxDecay)
	var total float64
	for _, access := range accesses {
		age := now.Sub(access)
		if age < ActivationMinimumAge {
			age = ActivationMinimumAge
		}
		days := age.Hours() / 24
		total += math.Pow(days, -decay)
	}
	if total <= 0 {
		return math.Inf(-1)
	}
	return math.Log(total)
}

// LearnedDecay estimates one kind's exponent from revisit intervals and shrinks to its prior.
func (s *Store) LearnedDecay(kind FactKind) (float64, int, error) {
	rows, err := s.db.Query(`
		SELECT facts.ts, injected.ts
		FROM events AS injected, json_each(injected.payload, '$.fact_seqs') AS item
		JOIN facts ON facts.seq=CAST(item.value AS INTEGER)
		WHERE injected.kind=? AND facts.kind=? ORDER BY injected.seq`, EventFactInjected, kind)
	if err != nil {
		return ActivationDefaultDecay, 0, fmt.Errorf("learn decay: %w", err)
	}
	defer rows.Close()
	var days []float64
	for rows.Next() {
		var learned, revisited string
		if err := rows.Scan(&learned, &revisited); err != nil {
			return ActivationDefaultDecay, 0, err
		}
		left, leftErr := parseTime(learned)
		right, rightErr := parseTime(revisited)
		if leftErr == nil && rightErr == nil && right.After(left) {
			days = append(days, right.Sub(left).Hours()/24)
		}
	}
	if len(days) < ActivationMinRevisits {
		return ActivationDefaultDecay, len(days), rows.Err()
	}
	sort.Float64s(days)
	median := days[len(days)/2]
	empirical := ActivationMinDecay + (ActivationMaxDecay-ActivationMinDecay)*
		math.Log1p(median)/math.Log1p(90)
	empirical = clampFloat(empirical, ActivationMinDecay, ActivationMaxDecay)
	weight := float64(len(days)) / float64(len(days)+ChannelPriorSamples)
	learned := weight*empirical + (1-weight)*ActivationDefaultDecay
	return clampFloat(learned, ActivationMinDecay, ActivationMaxDecay), len(days), rows.Err()
}

// FactActivation projects one belief's journaled access history at now.
func (s *Store) FactActivation(factSeq int64, now time.Time) (float64, float64, int, error) {
	fact, found, err := s.FactBySeq(factSeq)
	if err != nil || !found {
		if err == nil {
			err = ErrNotFound
		}
		return 0, ActivationDefaultDecay, 0, err
	}
	accesses := []time.Time{fact.Time}
	events, err := s.Events(fact.Seq, 0)
	if err != nil {
		return 0, ActivationDefaultDecay, 0, err
	}
	for _, event := range events {
		switch event.Kind {
		case EventFactInjected:
			var payload factInjectionPayload
			if json.Unmarshal(event.Payload, &payload) == nil && containsInt64(payload.FactSeqs, factSeq) {
				accesses = append(accesses, event.Time)
			}
		case EventFactLearned:
			var payload factPayload
			if json.Unmarshal(event.Payload, &payload) == nil && payload.Unsettled != nil {
				for _, approach := range payload.Unsettled.Approaches {
					if containsInt64(approach.Evidence, factSeq) {
						accesses = append(accesses, event.Time)
					}
				}
			}
		}
	}
	decay, _, err := s.LearnedDecay(fact.Kind)
	if err != nil {
		return 0, ActivationDefaultDecay, 0, err
	}
	return BaseLevelActivation(accesses, now, decay), decay, len(accesses), nil
}

func containsInt64(values []int64, want int64) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func clampFloat(value, floor, ceiling float64) float64 {
	return math.Max(floor, math.Min(ceiling, value))
}

const metaParameterSchema = `
CREATE TABLE IF NOT EXISTS meta_parameters (
    name       TEXT PRIMARY KEY,
    value      REAL NOT NULL,
    event_seq  INTEGER NOT NULL REFERENCES events(seq),
    evidence   JSON NOT NULL CHECK (json_valid(evidence))
);
`

// Tunable names the registry entry that is the only meta-learning mutation surface.
type Tunable struct {
	Name    string
	Default float64
	Floor   float64
	Ceiling float64
	Step    float64
	Label   string
}

const (
	ParameterSkillPromotionOccurrences = "skill_promotion_occurrences"
	ParameterBeliefRetentionThreshold  = "belief_retention_threshold"
	ParameterProposalCadenceRuns       = "proposal_cadence_runs"
	ParameterConsolidationThreshold    = "consolidation_threshold"
)

var tunableRegistry = map[string]Tunable{
	ParameterSkillPromotionOccurrences: {Name: ParameterSkillPromotionOccurrences, Default: 2, Floor: 1, Ceiling: 4, Step: 1, Label: "skill promotion"},
	ParameterBeliefRetentionThreshold:  {Name: ParameterBeliefRetentionThreshold, Default: -2.0, Floor: -3.0, Ceiling: -1.0, Step: 0.25, Label: "belief retention"},
	ParameterProposalCadenceRuns:       {Name: ParameterProposalCadenceRuns, Default: 1, Floor: 1, Ceiling: 4, Step: 1, Label: "proposal cadence"},
	ParameterConsolidationThreshold:    {Name: ParameterConsolidationThreshold, Default: 12, Floor: 8, Ceiling: 20, Step: 2, Label: "belief consolidation"},
}

// ParameterEvidence is attached to every parameter_changed journal event.
type ParameterEvidence struct {
	Reversals int     `json:"reversals"`
	Total     int     `json:"total"`
	Rate      float64 `json:"rate"`
}

// ParameterChange is one bounded, journaled dial movement.
type ParameterChange struct {
	Name      string            `json:"name"`
	Old       float64           `json:"old"`
	New       float64           `json:"new"`
	Evidence  ParameterEvidence `json:"evidence"`
	Direction string            `json:"direction"`
	Phrase    string            `json:"phrase"`
	Seq       int64             `json:"-"`
}

// Parameter returns the replayed value or the coded registry default.
func (s *Store) Parameter(name string) float64 {
	tunable, ok := tunableRegistry[name]
	if !ok {
		return 0
	}
	var value float64
	if err := s.db.QueryRow(`SELECT value FROM meta_parameters WHERE name=?`, name).Scan(&value); err == nil {
		return value
	}
	return tunable.Default
}

// TuneParameter moves one registry dial by at most one step inside its rails.
func (s *Store) TuneParameter(name string, direction int, evidence ParameterEvidence, phrase string) (ParameterChange, bool, error) {
	tunable, ok := tunableRegistry[name]
	if !ok || (direction != -1 && direction != 1) {
		return ParameterChange{}, false, fmt.Errorf("tune parameter: %w: unknown dial or direction", ErrInvalid)
	}
	old := s.Parameter(name)
	next := clampFloat(old+float64(direction)*tunable.Step, tunable.Floor, tunable.Ceiling)
	if next == old {
		return ParameterChange{}, false, nil
	}
	change := ParameterChange{Name: name, Old: old, New: next, Evidence: evidence,
		Direction: map[bool]string{true: "eased", false: "tightened"}[direction < 0],
		Phrase:    strings.TrimSpace(phrase)}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return ParameterChange{}, false, err
	}
	defer tx.Rollback()
	seq, _, err := appendEvent(tx, "", EventParameterChanged, change)
	if err != nil {
		return ParameterChange{}, false, err
	}
	change.Seq = seq
	if err := applyParameterChange(tx, change, seq); err != nil {
		return ParameterChange{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ParameterChange{}, false, err
	}
	return change, true, nil
}

func applyParameterChange(tx *sql.Tx, change ParameterChange, seq int64) error {
	tunable, ok := tunableRegistry[change.Name]
	if !ok || change.New < tunable.Floor || change.New > tunable.Ceiling ||
		math.Abs(change.New-change.Old)-tunable.Step > 1e-9 {
		return fmt.Errorf("invalid parameter change %q", change.Name)
	}
	var current float64
	err := tx.QueryRow(`SELECT value FROM meta_parameters WHERE name=?`, change.Name).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = tunable.Default
	} else if err != nil {
		return err
	}
	if math.Abs(current-change.Old) > 1e-9 {
		return fmt.Errorf("parameter %q replay expected %.3f, found %.3f", change.Name, change.Old, current)
	}
	evidence, _ := json.Marshal(change.Evidence)
	_, err = tx.Exec(`INSERT INTO meta_parameters(name,value,event_seq,evidence) VALUES(?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET value=excluded.value,event_seq=excluded.event_seq,evidence=excluded.evidence`,
		change.Name, change.New, seq, string(evidence))
	return err
}

// ReversalRate is the evidence for one retrospective action class.
type ReversalRate struct {
	Class     string
	Reversals int
	Total     int
	Rate      float64
}

// MetaReversalRates derives auditable action-class reversals from existing events.
func (s *Store) MetaReversalRates() (map[string]ReversalRate, error) {
	events, err := s.Events(0, 0)
	if err != nil {
		return nil, err
	}
	negative := make(map[int64]bool)
	activated := make(map[int64]bool)
	consolidated := make(map[int64]bool)
	aged := make(map[int64]bool)
	restored := make(map[int64]bool)
	proposalTotal, proposalDeclined := 0, 0
	learned := make(map[int64]factPayload)
	for _, event := range events {
		switch event.Kind {
		case EventFactLearned:
			var payload factPayload
			if json.Unmarshal(event.Payload, &payload) == nil {
				learned[event.Seq] = payload
			}
		case EventFactActivated:
			var payload factActivatedPayload
			if json.Unmarshal(event.Payload, &payload) == nil {
				activated[payload.FactSeq] = true
			}
		case EventFactSuperseded:
			var payload factSupersededPayload
			if json.Unmarshal(event.Payload, &payload) == nil {
				negative[payload.FactSeq] = true
				if replacement, ok := learned[payload.BySeq]; ok && replacement.Channel == FactChannelDistilled {
					consolidated[payload.BySeq] = true
				}
			}
		case EventFactQuarantined:
			var payload factStatusPayload
			if json.Unmarshal(event.Payload, &payload) == nil {
				negative[payload.FactSeq] = true
				if payload.Origin == FactOriginConsolidator {
					aged[payload.FactSeq] = true
				}
			}
		case EventFactRestored:
			var payload factStatusPayload
			if json.Unmarshal(event.Payload, &payload) == nil {
				restored[payload.FactSeq] = true
			}
		case EventCharterCreated:
			var payload map[string]any
			if json.Unmarshal(event.Payload, &payload) == nil && payload["proposal_shape"] != nil {
				proposalTotal++
			}
		case EventCharterProposalDeclined:
			proposalDeclined++
		}
	}
	result := make(map[string]ReversalRate)
	makeRate := func(class string, total, reversals int) {
		rate := 0.0
		if total > 0 {
			rate = float64(reversals) / float64(total)
		}
		result[class] = ReversalRate{Class: class, Total: total, Reversals: reversals, Rate: rate}
	}
	reversals := 0
	for seq := range activated {
		if negative[seq] {
			reversals++
		}
	}
	makeRate("skill_promotions", len(activated), reversals)
	reversals = 0
	for seq := range consolidated {
		if negative[seq] {
			reversals++
		}
	}
	makeRate("consolidations", len(consolidated), reversals)
	reversals = 0
	for seq := range aged {
		if restored[seq] {
			reversals++
		}
	}
	makeRate("aging", len(aged), reversals)
	makeRate("proposals", proposalTotal, proposalDeclined)
	return result, nil
}

// RetrospectiveRuns counts existing checkpoints; it is rebuild-stable by construction.
func (s *Store) RetrospectiveRuns() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind=?`, EventRetrospectiveCheckpointed).Scan(&count)
	return count, err
}

// AgeFacts applies ACT-R retention and reuses quarantine as the visible reversible outcome.
func (s *Store) AgeFacts(now time.Time) (int, error) {
	facts, err := s.ActiveFacts("", 10000)
	if err != nil {
		return 0, err
	}
	threshold := s.Parameter(ParameterBeliefRetentionThreshold)
	aged := 0
	for _, fact := range facts {
		if fact.Kind == FactTrait || fact.Kind == FactQuestion {
			continue
		}
		activation, _, _, err := s.FactActivation(fact.Seq, now)
		if err != nil || activation >= threshold {
			continue
		}
		if err := s.QuarantineFact(fact.Seq, 0, FactOriginConsolidator); err != nil {
			return aged, err
		}
		aged++
	}
	return aged, nil
}
