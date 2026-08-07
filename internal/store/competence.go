package store

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// CompetenceClass is the current evidence-backed posture of one scope.
type CompetenceClass string

const (
	CompetenceStrong   CompetenceClass = "strong"
	CompetenceFrontier CompetenceClass = "frontier"
	CompetenceWeak     CompetenceClass = "weak"
	CompetenceStale    CompetenceClass = "stale"
)

// SurpriseTrend describes whether recent prediction error is moving enough to
// matter. Unknown means there is not yet a complete comparison window.
type SurpriseTrend string

const (
	SurpriseUnknown   SurpriseTrend = "unknown"
	SurpriseImproving SurpriseTrend = "improving"
	SurpriseFlat      SurpriseTrend = "flat"
	SurpriseWorsening SurpriseTrend = "worsening"
)

// ScopeKind names the two vocabularies admitted to the map. Territory scopes
// are canonical notebook scopes earned by folded jobs. Profile scopes are
// prefixed with "profile:" so a domain named "atomic" cannot collide with the
// planner's atomic bucket.
type ScopeKind string

const (
	CompetenceTerritory ScopeKind = "territory"
	CompetenceProfile   ScopeKind = "profile"
)

// ScopeCompetence is the narrow, serializable unit future practice loops may
// consume. Rates are fractions in [0,1]. InstalledSkills contains active skill
// docs whose canonical scope touches this scope.
type ScopeCompetence struct {
	Scope           string          `json:"scope"`
	Kind            ScopeKind       `json:"kind"`
	Class           CompetenceClass `json:"class"`
	Samples         int             `json:"samples"`
	Successes       int             `json:"successes"`
	Failures        int             `json:"failures"`
	SuccessRate     float64         `json:"success_rate"`
	FailureRate     float64         `json:"failure_rate"`
	SurpriseSamples int             `json:"surprise_samples"`
	Surprise        float64         `json:"surprise"`
	SurpriseTrend   SurpriseTrend   `json:"surprise_trend"`
	InstalledSkills []string        `json:"installed_skills,omitempty"`
	LastTouched     time.Time       `json:"last_touched,omitempty"`
}

// CompetenceMap is a pure derived view. It owns no cache and writes nothing.
type CompetenceMap struct {
	Scopes []ScopeCompetence `json:"scopes"`
}

// Frontier returns an independent, stable list of scopes in the learnable
// band. A future practice loop can consume it without depending on SQL,
// territory layout, profile files, or classification logic.
func (m CompetenceMap) Frontier() []ScopeCompetence {
	frontier := make([]ScopeCompetence, 0)
	for _, scope := range m.Scopes {
		if scope.Class != CompetenceFrontier {
			continue
		}
		scope.InstalledSkills = append([]string(nil), scope.InstalledSkills...)
		frontier = append(frontier, scope)
	}
	return frontier
}

// CompetenceThresholds keeps every policy boundary out of the arithmetic.
// Copy DefaultCompetenceThresholds(), adjust the fields a consumer owns, and
// pass it through CompetenceOptions.
type CompetenceThresholds struct {
	EstablishedSamples int
	StrongFailureBelow float64
	FrontierFailureMin float64
	FrontierFailureMax float64
	WeakFailureAbove   float64
	LowSurpriseMax     float64
	TrendWindow        int
	TrendMinSamples    int
	TrendDelta         float64
	StaleAfter         time.Duration
}

// DefaultCompetenceThresholds returns the resident's current classification
// policy. The eight-sample evidence floor matches profile.MinSamples.
func DefaultCompetenceThresholds() CompetenceThresholds {
	return CompetenceThresholds{
		EstablishedSamples: profile.MinSamples,
		StrongFailureBelow: 0.25,
		FrontierFailureMin: 0.25,
		FrontierFailureMax: 0.75,
		WeakFailureAbove:   0.75,
		LowSurpriseMax:     0.25,
		TrendWindow:        8,
		TrendMinSamples:    4,
		TrendDelta:         0.10,
		StaleAfter:         90 * 24 * time.Hour,
	}
}

// CompetenceOptions supplies the model profile that owns the generic execution
// buckets, an optional policy override, and a clock for deterministic callers.
// With no options, CompetenceMap still returns the journal-derived territory
// view.
type CompetenceOptions struct {
	Profile    *profile.Profile
	Thresholds *CompetenceThresholds
	Now        time.Time
}

type competenceAccumulator struct {
	scope       string
	kind        ScopeKind
	samples     int
	successes   int
	failures    int
	surprises   []float64
	skills      map[string]bool
	lastTouched time.Time
}

// CompetenceMap derives what the resident is currently good and bad at from
// terminal work, delivery gates, journaled surprise, active skills, territory
// scopes, and the supplied model profile. It makes no model calls and performs
// no writes.
func (s *Store) CompetenceMap(options ...CompetenceOptions) (CompetenceMap, error) {
	if s == nil || s.db == nil {
		return CompetenceMap{}, fmt.Errorf("competence map: nil store")
	}
	if len(options) > 1 {
		return CompetenceMap{}, fmt.Errorf("competence map: %w: at most one options value", ErrInvalid)
	}
	var option CompetenceOptions
	if len(options) == 1 {
		option = options[0]
	}
	thresholds := DefaultCompetenceThresholds()
	if option.Thresholds != nil {
		thresholds = *option.Thresholds
	}
	if err := thresholds.validate(); err != nil {
		return CompetenceMap{}, fmt.Errorf("competence map: %w: %v", ErrInvalid, err)
	}
	now := option.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	accumulators := make(map[string]*competenceAccumulator)
	accumulator := func(scope string, kind ScopeKind) *competenceAccumulator {
		scope = strings.TrimSpace(scope)
		key := string(kind) + "\x00" + scope
		if existing := accumulators[key]; existing != nil {
			return existing
		}
		created := &competenceAccumulator{scope: scope, kind: kind, skills: make(map[string]bool)}
		accumulators[key] = created
		return created
	}

	if err := s.addTerritoryCompetence(accumulator, accumulators); err != nil {
		return CompetenceMap{}, err
	}
	if option.Profile != nil {
		addProfileCompetence(accumulator, option.Profile)
	}

	scopes := make([]ScopeCompetence, 0, len(accumulators))
	for _, accumulated := range accumulators {
		competence := accumulated.finish(thresholds, now)
		scopes = append(scopes, competence)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].Scope != scopes[j].Scope {
			return scopes[i].Scope < scopes[j].Scope
		}
		return scopes[i].Kind < scopes[j].Kind
	})
	return CompetenceMap{Scopes: scopes}, nil
}

func (s *Store) addTerritoryCompetence(
	accumulator func(string, ScopeKind) *competenceAccumulator,
	accumulators map[string]*competenceAccumulator,
) error {
	nodes, err := s.Nodes()
	if err != nil {
		return fmt.Errorf("competence map nodes: %w", err)
	}
	jobs, err := s.TerritoryJobs()
	if err != nil {
		return fmt.Errorf("competence map territories: %w", err)
	}

	children := make(map[string][]string)
	byID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
		children[node.Parent] = append(children[node.Parent], node.ID)
	}
	nodeScope := make(map[string]string)
	for _, job := range jobs {
		scope := strings.TrimSpace(job.DominantScope)
		if scope == "" {
			continue
		}
		accumulator(scope, CompetenceTerritory)
		stack := []string{job.Node.ID}
		for len(stack) > 0 {
			last := len(stack) - 1
			id := stack[last]
			stack = stack[:last]
			if _, assigned := nodeScope[id]; assigned {
				continue
			}
			nodeScope[id] = scope
			stack = append(stack, children[id]...)
		}
	}

	latestGate := make(map[string]DeliveryGate)
	rows, err := s.db.Query(`SELECT node_id, payload FROM events WHERE kind = ? ORDER BY seq`, EventDeliveryGate)
	if err != nil {
		return fmt.Errorf("competence map gates: %w", err)
	}
	for rows.Next() {
		var nodeID, payload string
		if err := rows.Scan(&nodeID, &payload); err != nil {
			rows.Close()
			return fmt.Errorf("competence map gates: %w", err)
		}
		var gate DeliveryGate
		if err := json.Unmarshal([]byte(payload), &gate); err != nil {
			rows.Close()
			return fmt.Errorf("competence map gate %q: %w", nodeID, err)
		}
		latestGate[nodeID] = gate
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("competence map gates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("competence map gates: %w", err)
	}

	for nodeID, scope := range nodeScope {
		node := byID[nodeID]
		if IsOrganizationalGroup(node.Group) || node.Status == Cancelled {
			continue
		}
		accumulated := accumulator(scope, CompetenceTerritory)
		switch node.Status {
		case Done:
			accumulated.samples++
			if gate, found := latestGate[nodeID]; found && !gate.Pass {
				accumulated.failures++
			} else {
				accumulated.successes++
			}
		case Failed:
			accumulated.samples++
			accumulated.failures++
		default:
			continue
		}
		accumulated.touch(node.FinishedAt)
	}

	rows, err = s.db.Query(`SELECT node_id, surprise, ts FROM surprises ORDER BY seq`)
	if err != nil {
		return fmt.Errorf("competence map surprise: %w", err)
	}
	for rows.Next() {
		var nodeID, timestamp string
		var surprise float64
		if err := rows.Scan(&nodeID, &surprise, &timestamp); err != nil {
			rows.Close()
			return fmt.Errorf("competence map surprise: %w", err)
		}
		scope := nodeScope[nodeID]
		if scope == "" {
			continue
		}
		at, err := parseTime(timestamp)
		if err != nil {
			rows.Close()
			return fmt.Errorf("competence map surprise time: %w", err)
		}
		accumulated := accumulator(scope, CompetenceTerritory)
		accumulated.surprises = append(accumulated.surprises, surprise)
		accumulated.touch(at)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("competence map surprise: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("competence map surprise: %w", err)
	}

	facts, err := s.Facts(0)
	if err != nil {
		return fmt.Errorf("competence map skills: %w", err)
	}
	for _, fact := range facts {
		if fact.Kind != FactSkill || fact.Status != FactActive || strings.TrimSpace(fact.Artifact) == "" {
			continue
		}
		matched := false
		for _, accumulated := range accumulators {
			if accumulated.kind != CompetenceTerritory || !scopesTouch(accumulated.scope, fact.Scope) {
				continue
			}
			accumulated.skills[fact.Body] = true
			accumulated.touch(fact.Time)
			matched = true
		}
		if !matched {
			accumulated := accumulator(fact.Scope, CompetenceTerritory)
			accumulated.skills[fact.Body] = true
			accumulated.touch(fact.Time)
		}
	}
	return nil
}

func addProfileCompetence(accumulator func(string, ScopeKind) *competenceAccumulator, measured *profile.Profile) {
	records, modifiedAt := measured.RecordsSnapshot()
	for _, record := range records {
		if !knownProfileBucket(record.Size) || record.Verdict == provider.VerdictProviderFailure {
			continue
		}
		accumulated := accumulator("profile:"+record.Size, CompetenceProfile)
		accumulated.samples++
		positive, graded := record.Verdict.Graded()
		switch {
		case record.Promoted || graded && !positive || record.Overran():
			accumulated.failures++
		default:
			// A legacy or unverified completion is still operational success
			// evidence for this failure-rate view; surprise remains the separate
			// calibration signal that keeps it from becoming strong too early.
			accumulated.successes++
		}
		if record.Surprise != nil {
			accumulated.surprises = append(accumulated.surprises, *record.Surprise)
		}
		at := record.Time
		if at.IsZero() {
			at = modifiedAt
		}
		accumulated.touch(at)
	}
}

func knownProfileBucket(bucket string) bool {
	switch bucket {
	case profile.BucketReflex, profile.BucketDirect, "atomic", "borderline", "oversized", "synthesis":
		return true
	default:
		return false
	}
}

func (a *competenceAccumulator) touch(at time.Time) {
	if at.After(a.lastTouched) {
		a.lastTouched = at.UTC()
	}
}

func (a *competenceAccumulator) finish(thresholds CompetenceThresholds, now time.Time) ScopeCompetence {
	result := ScopeCompetence{
		Scope: a.scope, Kind: a.kind, Samples: a.samples,
		Successes: a.successes, Failures: a.failures,
		SurpriseSamples: len(a.surprises), LastTouched: a.lastTouched,
	}
	if a.samples > 0 {
		result.SuccessRate = float64(a.successes) / float64(a.samples)
		result.FailureRate = float64(a.failures) / float64(a.samples)
	}
	window := a.surprises
	if len(window) > thresholds.TrendWindow {
		window = window[len(window)-thresholds.TrendWindow:]
	}
	result.Surprise = meanFloat(window)
	result.SurpriseTrend = surpriseTrend(window, thresholds)
	for skill := range a.skills {
		result.InstalledSkills = append(result.InstalledSkills, skill)
	}
	sort.Strings(result.InstalledSkills)
	result.Class = classifyCompetence(result, thresholds, now)
	return result
}

func classifyCompetence(scope ScopeCompetence, thresholds CompetenceThresholds, now time.Time) CompetenceClass {
	if !scope.LastTouched.IsZero() && now.After(scope.LastTouched) && now.Sub(scope.LastTouched) >= thresholds.StaleAfter {
		return CompetenceStale
	}
	if scope.SurpriseTrend == SurpriseImproving ||
		scope.FailureRate >= thresholds.FrontierFailureMin && scope.FailureRate <= thresholds.FrontierFailureMax {
		return CompetenceFrontier
	}
	if scope.Samples >= thresholds.EstablishedSamples &&
		scope.FailureRate < thresholds.StrongFailureBelow &&
		scope.SurpriseTrend == SurpriseFlat && scope.Surprise <= thresholds.LowSurpriseMax {
		return CompetenceStrong
	}
	if scope.Samples >= thresholds.EstablishedSamples &&
		scope.FailureRate > thresholds.WeakFailureAbove && scope.SurpriseTrend != SurpriseImproving {
		return CompetenceWeak
	}
	// Sparse, worsening, or otherwise unsettled evidence is not a claim of
	// strength or weakness. It remains frontier until enough work resolves it.
	return CompetenceFrontier
}

func surpriseTrend(window []float64, thresholds CompetenceThresholds) SurpriseTrend {
	if len(window) < thresholds.TrendMinSamples {
		return SurpriseUnknown
	}
	middle := len(window) / 2
	change := meanFloat(window[middle:]) - meanFloat(window[:middle])
	switch {
	case change <= -thresholds.TrendDelta:
		return SurpriseImproving
	case change >= thresholds.TrendDelta:
		return SurpriseWorsening
	default:
		return SurpriseFlat
	}
}

func meanFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func scopesTouch(first, second string) bool {
	first, second = normalizeScope(first), normalizeScope(second)
	if first == second {
		return true
	}
	kindOf := func(scope string) (string, string) {
		for _, kind := range []string{"repo", "file"} {
			if strings.HasPrefix(scope, kind+":") {
				return kind, strings.TrimSuffix(strings.TrimPrefix(scope, kind+":"), "/")
			}
		}
		return "", ""
	}
	firstKind, firstPath := kindOf(first)
	secondKind, secondPath := kindOf(second)
	if firstKind == "" || secondKind == "" || firstKind == "file" && secondKind == "file" {
		return false
	}
	// Repository scopes touch nested repositories and files within them. File
	// scopes otherwise remain exact: prefix-sharing filenames are not a scope
	// hierarchy.
	return strings.HasPrefix(firstPath, secondPath+"/") || strings.HasPrefix(secondPath, firstPath+"/")
}

func (thresholds CompetenceThresholds) validate() error {
	validRate := func(value float64) bool {
		return value >= 0 && value <= 1 && !math.IsNaN(value) && !math.IsInf(value, 0)
	}
	if thresholds.EstablishedSamples <= 0 || thresholds.TrendWindow < 2 ||
		thresholds.TrendMinSamples < 2 || thresholds.TrendMinSamples > thresholds.TrendWindow ||
		thresholds.TrendDelta < 0 || math.IsNaN(thresholds.TrendDelta) || math.IsInf(thresholds.TrendDelta, 0) ||
		thresholds.LowSurpriseMax < 0 || math.IsNaN(thresholds.LowSurpriseMax) || math.IsInf(thresholds.LowSurpriseMax, 0) ||
		thresholds.StaleAfter <= 0 {
		return fmt.Errorf("sample, surprise, trend, and stale thresholds must be positive and coherent")
	}
	if !validRate(thresholds.StrongFailureBelow) || !validRate(thresholds.FrontierFailureMin) ||
		!validRate(thresholds.FrontierFailureMax) || !validRate(thresholds.WeakFailureAbove) ||
		thresholds.StrongFailureBelow > thresholds.FrontierFailureMin ||
		thresholds.FrontierFailureMin > thresholds.FrontierFailureMax ||
		thresholds.FrontierFailureMax > thresholds.WeakFailureAbove {
		return fmt.Errorf("failure-rate thresholds must be ordered within [0,1]")
	}
	return nil
}

// FormatCompetence renders a bounded, line-structured grounding block. It is
// deliberately data rather than prose: the head supplies the resident voice in
// its one ordinary routing call.
func FormatCompetence(competence CompetenceMap, maxBytes int) string {
	if len(competence.Scopes) == 0 || maxBytes <= 0 {
		return ""
	}
	var block strings.Builder
	block.WriteString("Current competence evidence (derived locally; rates are fractions):\n")
	for _, scope := range competence.Scopes {
		line, err := json.Marshal(scope)
		if err != nil {
			continue
		}
		if block.Len()+len(line)+3 > maxBytes {
			break
		}
		block.WriteString("- ")
		block.Write(line)
		block.WriteByte('\n')
	}
	return strings.TrimSpace(block.String())
}
