package store

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	// TraitRefreshInterval keeps behavioral priors slow when their sample count is unchanged.
	TraitRefreshInterval = 24 * time.Hour
)

const (
	TraitCorrectionStyle      = "correction-style"
	TraitDefaultAcceptance    = "default-acceptance"
	TraitProposalAppetite     = "proposal-appetite"
	TraitSpecGranularity      = "spec-granularity"
	TraitExplorationTolerance = "exploration-tolerance"
)

// CorrectionStyleValue is measured only from non-default question resolutions.
type CorrectionStyleValue struct {
	Style              string  `json:"style"`
	MeanLatencySeconds float64 `json:"mean_latency_seconds"`
}

// DefaultAcceptanceValue keeps a rate and sample count per durable question category.
type DefaultAcceptanceValue map[QuestionCategory]struct {
	Rate float64 `json:"rate"`
	N    int     `json:"n"`
}

// ProposalAppetiteValue is the accepted share of journaled standing proposals.
type ProposalAppetiteValue struct {
	Acceptance float64 `json:"acceptance"`
}

// SpecGranularityValue projects ask size and subsequent redirect density.
type SpecGranularityValue struct {
	MedianBriefBytes  int     `json:"median_brief_bytes"`
	CorrectionDensity float64 `json:"correction_density"`
}

// ExplorationToleranceValue compares correction rates on trial and plain work.
type ExplorationToleranceValue struct {
	TrialCorrectionRate float64 `json:"trial_correction_rate"`
	PlainCorrectionRate float64 `json:"plain_correction_rate"`
	Tolerance           float64 `json:"tolerance"`
}

// NamedTrait is one pure journal projection ready to persist as a singleton fact.
type NamedTrait struct {
	Name        string
	Measurement TraitMeasurement
}

// MeasureTraits computes all five v1 traits without creating telemetry.
func (s *Store) MeasureTraits(now time.Time) ([]NamedTrait, error) {
	traits := make([]NamedTrait, 0, 5)
	correction, correctionN, err := s.measureCorrectionStyle()
	if err != nil {
		return nil, err
	}
	traits = append(traits, NamedTrait{TraitCorrectionStyle, TraitMeasurement{Value: correction, N: correctionN, Updated: now}})
	acceptance, acceptanceN, err := s.measureDefaultAcceptance()
	if err != nil {
		return nil, err
	}
	traits = append(traits, NamedTrait{TraitDefaultAcceptance, TraitMeasurement{Value: acceptance, N: acceptanceN, Updated: now}})
	appetite, appetiteN, err := s.measureProposalAppetite()
	if err != nil {
		return nil, err
	}
	traits = append(traits, NamedTrait{TraitProposalAppetite, TraitMeasurement{Value: appetite, N: appetiteN, Updated: now}})
	granularity, granularityN, err := s.measureSpecGranularity()
	if err != nil {
		return nil, err
	}
	traits = append(traits, NamedTrait{TraitSpecGranularity, TraitMeasurement{Value: granularity, N: granularityN, Updated: now}})
	tolerance, toleranceN, err := s.measureExplorationTolerance()
	if err != nil {
		return nil, err
	}
	traits = append(traits, NamedTrait{TraitExplorationTolerance, TraitMeasurement{Value: tolerance, N: toleranceN, Updated: now}})
	return traits, nil
}

// ProjectTraits re-measures supported traits and supersedes only measurements with evidence.
func (s *Store) ProjectTraits(now time.Time) ([]Fact, error) {
	measured, err := s.MeasureTraits(now)
	if err != nil {
		return nil, err
	}
	var facts []Fact
	for _, trait := range measured {
		if trait.Measurement.N == 0 {
			continue
		}
		if prior, _, ok, err := s.Trait(trait.Name); err == nil && ok && prior.N == trait.Measurement.N && now.Sub(prior.Updated) < TraitRefreshInterval {
			continue
		}
		fact, err := s.RecordTrait(trait.Name, trait.Measurement)
		if err != nil {
			return facts, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func (s *Store) measureCorrectionStyle() (CorrectionStyleValue, int, error) {
	rows, err := s.db.Query(`SELECT asked_at,resolved_at,default_answer,resolution,options FROM agent_questions
		WHERE status=? AND default_answer<>'' AND resolved_at IS NOT NULL`, QuestionAnswered)
	if err != nil {
		return CorrectionStyleValue{}, 0, err
	}
	var total time.Duration
	n := 0
	for rows.Next() {
		var asked, resolved, offered, answer, encoded string
		if err := rows.Scan(&asked, &resolved, &offered, &answer, &encoded); err != nil {
			return CorrectionStyleValue{}, 0, err
		}
		var options []QuestionOption
		_ = json.Unmarshal([]byte(encoded), &options)
		if questionAnswerMatchesDefault(answer, offered, options) {
			continue
		}
		left, leftErr := parseTime(asked)
		right, rightErr := parseTime(resolved)
		if leftErr == nil && rightErr == nil && !right.Before(left) {
			total += right.Sub(left)
			n++
		}
	}
	if err := rows.Close(); err != nil {
		return CorrectionStyleValue{}, 0, err
	}
	// A skipped ask is measured against the one user turn that answered it, so
	// both sides are indexed lookups rather than a walk of journal x thread.
	assumptions, err := s.assumedWithDefaults("")
	if err != nil {
		return CorrectionStyleValue{}, 0, err
	}
	for _, assumption := range assumptions {
		turn, found, err := s.nextUserMessageAfter(assumption.SessionID, assumption.Seq)
		if err != nil {
			return CorrectionStyleValue{}, 0, err
		}
		if !found {
			continue
		}
		if !questionAnswerMatchesDefault(turn.Body, assumption.Default, nil) {
			total += turn.Time.Sub(assumption.Time)
			n++
		}
	}
	value := CorrectionStyleValue{Style: "batched"}
	if n > 0 {
		mean := total / time.Duration(n)
		value.MeanLatencySeconds = mean.Seconds()
		if mean <= CorrectionImmediateWindow {
			value.Style = "immediate"
		}
	}
	return value, n, nil
}

// measureDefaultAcceptance counts every answered durable question once. The
// acceptance rate is the only thing this trait reads, so asking the gate for
// full per-category statistics — each of which re-derives rework cost — would
// pay for arithmetic the measurement then discards.
func (s *Store) measureDefaultAcceptance() (DefaultAcceptanceValue, int, error) {
	rows, err := s.db.Query(`SELECT category, default_answer, resolution, options
		FROM agent_questions WHERE status=? AND default_answer<>''`, QuestionAnswered)
	if err != nil {
		return nil, 0, err
	}
	type tally struct{ n, accepted int }
	tallies := make(map[QuestionCategory]*tally)
	for rows.Next() {
		var category QuestionCategory
		var offered, resolution, encoded string
		if err := rows.Scan(&category, &offered, &resolution, &encoded); err != nil {
			rows.Close()
			return nil, 0, err
		}
		var options []QuestionOption
		_ = json.Unmarshal([]byte(encoded), &options)
		counted := tallies[category]
		if counted == nil {
			counted = &tally{}
			tallies[category] = counted
		}
		counted.n++
		if questionAnswerMatchesDefault(resolution, offered, options) {
			counted.accepted++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	value := DefaultAcceptanceValue{}
	total := 0
	for category, counted := range tallies {
		rate := 0.0
		if counted.n > 0 {
			rate = float64(counted.accepted) / float64(counted.n)
		}
		value[category] = struct {
			Rate float64 `json:"rate"`
			N    int     `json:"n"`
		}{rate, counted.n}
		total += counted.n
	}
	return value, total, nil
}

func (s *Store) measureProposalAppetite() (ProposalAppetiteValue, int, error) {
	var accepted, declined int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM commands WHERE kind=? AND status=?`, CommandCharterRatify, CommandApplied).Scan(&accepted); err != nil {
		return ProposalAppetiteValue{}, 0, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind=?`, EventCharterProposalDeclined).Scan(&declined); err != nil {
		return ProposalAppetiteValue{}, 0, err
	}
	total := accepted + declined
	return ProposalAppetiteValue{Acceptance: float64(accepted) / float64(max(total, 1))}, total, nil
}

func (s *Store) measureSpecGranularity() (SpecGranularityValue, int, error) {
	rows, err := s.db.Query(`SELECT length(instruction) FROM commands WHERE kind=? ORDER BY length(instruction)`, CommandSplice)
	if err != nil {
		return SpecGranularityValue{}, 0, err
	}
	var lengths []int
	for rows.Next() {
		var length int
		if err := rows.Scan(&length); err != nil {
			rows.Close()
			return SpecGranularityValue{}, 0, err
		}
		lengths = append(lengths, length)
	}
	if err := rows.Close(); err != nil {
		return SpecGranularityValue{}, 0, err
	}
	var corrections int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM commands WHERE kind IN (?,?,?,?)`, CommandAmend, CommandCancel, CommandRestart, CommandReprioritize).Scan(&corrections); err != nil {
		return SpecGranularityValue{}, 0, err
	}
	median := 0
	if len(lengths) > 0 {
		sort.Ints(lengths)
		median = lengths[len(lengths)/2]
	}
	return SpecGranularityValue{MedianBriefBytes: median, CorrectionDensity: float64(corrections) / float64(max(len(lengths), 1))}, len(lengths), nil
}

func (s *Store) measureExplorationTolerance() (ExplorationToleranceValue, int, error) {
	type count struct{ jobs, corrected int }
	counts := map[bool]*count{true: {}, false: {}}
	rows, err := s.db.Query(`SELECT id,trial_of FROM nodes WHERE parent_id=? AND origin=?`, RootID, OriginUser)
	if err != nil {
		return ExplorationToleranceValue{}, 0, err
	}
	type job struct {
		id    string
		trial bool
	}
	var jobs []job
	for rows.Next() {
		var id string
		var trial int64
		if err := rows.Scan(&id, &trial); err != nil {
			rows.Close()
			return ExplorationToleranceValue{}, 0, err
		}
		jobs = append(jobs, job{id: id, trial: trial > 0})
	}
	if err := rows.Close(); err != nil {
		return ExplorationToleranceValue{}, 0, err
	}
	for _, current := range jobs {
		bucket := counts[current.trial]
		bucket.jobs++
		var corrected int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM commands WHERE target=? AND kind IN (?,?,?,?)`, current.id, CommandAmend, CommandCancel, CommandRestart, CommandReprioritize).Scan(&corrected); err == nil && corrected > 0 {
			bucket.corrected++
		}
	}
	trialRate := float64(counts[true].corrected) / float64(max(counts[true].jobs, 1))
	plainRate := float64(counts[false].corrected) / float64(max(counts[false].jobs, 1))
	return ExplorationToleranceValue{TrialCorrectionRate: trialRate, PlainCorrectionRate: plainRate, Tolerance: math.Max(0, 1-math.Max(0, trialRate-plainRate))}, counts[true].jobs + counts[false].jobs, nil
}

// ProposalCadenceRuns scales the tuned cadence within hard rails by observed appetite.
func (s *Store) ProposalCadenceRuns() int {
	base := s.Parameter(ParameterProposalCadenceRuns)
	measurement, _, ok, _ := s.Trait(TraitProposalAppetite)
	if !ok {
		return int(base)
	}
	var value ProposalAppetiteValue
	if !decodeTraitValue(measurement.Value, &value) {
		return int(base)
	}
	traitCadence := float64(ProposalCadenceMaxRuns) - (float64(ProposalCadenceMaxRuns-ProposalCadenceMinRuns) * clampFloat(value.Acceptance, 0, 1))
	return int(math.Round(clampFloat(traitCadence+(base-1), ProposalCadenceMinRuns, ProposalCadenceMaxRuns)))
}

// CompilerAssumptionGuidance exposes the measured compiler seam without narrating a personality.
func (s *Store) CompilerAssumptionGuidance() string {
	measurement, _, ok, _ := s.Trait(TraitSpecGranularity)
	if !ok || measurement.N < VOIMinSamples {
		return ""
	}
	var value SpecGranularityValue
	if !decodeTraitValue(measurement.Value, &value) {
		return ""
	}
	if value.MedianBriefBytes < 160 || value.CorrectionDensity > .25 {
		return "When details are absent, ask one compact compile question instead of stacking assumptions."
	}
	return "When details are absent, prefer an explicit, declared assumption over an extra compile question."
}

// SilenceConsentWait expands the existing wait seam to the observed correction latency.
func (s *Store) SilenceConsentWait(base time.Duration) time.Duration {
	measurement, _, ok, _ := s.Trait(TraitCorrectionStyle)
	if !ok {
		return base
	}
	var value CorrectionStyleValue
	if !decodeTraitValue(measurement.Value, &value) {
		return base
	}
	observed := time.Duration(value.MeanLatencySeconds * float64(time.Second))
	if observed > base {
		return min(observed, 4*base)
	}
	return base
}

// MeasuredTraitBlock renders the measured traits as plain sentences, bounded by
// maxBytes. It exists because promptEligible excludes FactTrait outright and
// always will: a trait is a number about the user, and letting it into ordinary
// cue retrieval would put behavioural statistics in front of a worker who asked
// about a parser. This is the explicit carve-out — a caller that wants
// self-knowledge asks for it by name, and gets a byte-capped block rather than
// a retrieval.
//
// Without it, five second-order traits were re-measured on every retrospective
// and reached no model at all except as the one derived policy line
// CompilerAssumptionGuidance already emits.
func (s *Store) MeasuredTraitBlock(maxBytes int) string {
	if s == nil || maxBytes <= 0 {
		return ""
	}
	var lines []string
	add := func(line string) {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, "- "+line)
		}
	}
	if measurement, _, ok, err := s.Trait(TraitCorrectionStyle); err == nil && ok && measurement.N > 0 {
		var value CorrectionStyleValue
		if decodeTraitValue(measurement.Value, &value) && value.Style != "" {
			add(fmt.Sprintf("corrects %s (%d observed, mean %.0fs to reply)",
				value.Style, measurement.N, value.MeanLatencySeconds))
		}
	}
	if measurement, _, ok, err := s.Trait(TraitDefaultAcceptance); err == nil && ok && measurement.N > 0 {
		var value DefaultAcceptanceValue
		if decodeTraitValue(measurement.Value, &value) && len(value) > 0 {
			categories := make([]string, 0, len(value))
			for category := range value {
				categories = append(categories, string(category))
			}
			sort.Strings(categories)
			parts := make([]string, 0, len(categories))
			for _, category := range categories {
				counted := value[QuestionCategory(category)]
				parts = append(parts, fmt.Sprintf("%s %.0f%%", category, 100*counted.Rate))
			}
			add("takes the offered default: " + strings.Join(parts, ", "))
		}
	}
	if measurement, _, ok, err := s.Trait(TraitProposalAppetite); err == nil && ok && measurement.N > 0 {
		var value ProposalAppetiteValue
		if decodeTraitValue(measurement.Value, &value) {
			add(fmt.Sprintf("accepts %.0f%% of standing proposals (%d judged)",
				100*value.Acceptance, measurement.N))
		}
	}
	if measurement, _, ok, err := s.Trait(TraitSpecGranularity); err == nil && ok && measurement.N > 0 {
		var value SpecGranularityValue
		if decodeTraitValue(measurement.Value, &value) {
			add(fmt.Sprintf("asks in about %d characters, and corrects %.0f%% of jobs afterwards",
				value.MedianBriefBytes, 100*value.CorrectionDensity))
		}
	}
	if measurement, _, ok, err := s.Trait(TraitExplorationTolerance); err == nil && ok && measurement.N > 0 {
		var value ExplorationToleranceValue
		if decodeTraitValue(measurement.Value, &value) {
			add(fmt.Sprintf("tolerates exploratory work at %.2f of ordinary work", value.Tolerance))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	block := "Measured about this user (journal-derived, not asserted):"
	for _, line := range lines {
		if len(block)+len(line)+1 > maxBytes {
			break
		}
		block += "\n" + line
	}
	if !strings.Contains(block, "\n") {
		return ""
	}
	return block
}

func decodeTraitValue(value any, target any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(encoded, target) == nil
}
