// Package contract freezes the interface contracts between the three
// pareto-crewing lanes (evidence, learner, integration) before they run in
// parallel, per the approved plan at ~/pareto-handoff/IMPLEMENTATION-EXPERIMENT-PLAN.md
// section 5 and the math in ~/pareto-handoff/PROPOSED-LEARNING-MODEL.md.
//
// WHAT THIS PACKAGE IS NOT: it is not an implementation of the predictor, the
// policy or the grader. It is the wire shape and the semantics those
// implementations must agree on. The types are deliberately boring and
// explicit: they are the load-bearing interface three downstream lanes build
// against, and every field here is one a lane can be held to.
//
// VERSIONING RULE: every event and artifact carries the schema version it was
// written under. A reader that does not know that version refuses rather than
// half-reads (same rule as internal/pool/index). Any change to a field's
// meaning, or the removal/redefinition of a field, is a version bump — see
// CONTRACT.md in this package for the field-by-field pin.
package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// Schema versions frozen 2026-10-20. Bump when a field changes meaning, not
// when a field is added: adding a field is forward compatible, like the index
// document's unknown-field rule. The marshal helpers refuse to emit an unset
// version silently — writing without a version is a caller bug.
const (
	// DecisionEventVersion is the frozen version of DecisionEvent.
	DecisionEventVersion = 1
	// OutcomeEventVersion is the frozen version of OutcomeEvent.
	OutcomeEventVersion = 1
	// PredictorStateVersion is the frozen version of a Predictor's export
	// envelope. The Predictor owns the payload's interior format; this
	// version pins only the envelope the export/import contract travels in.
	PredictorStateVersion = 1
	// SharedSnapshotVersion is the frozen version of SharedSnapshot.
	SharedSnapshotVersion = 1
)

// ErrVersion is wrapped by every version-mismatch rejection.
var ErrVersion = errors.New("contract: unsupported schema version")

// ErrShape is wrapped by structural rejections: a distribution that does not
// sum to one, a probability out of range, a required linkage missing.
var ErrShape = errors.New("contract: structurally invalid document")

// ---------------------------------------------------------------
// DecisionEvent — what the policy knew at the instant of selection.
// ---------------------------------------------------------------

// DecisionEvent is recorded BEFORE the draw, once per role/node selection
// boundary. Everything on it must be provable to exist at decision time:
// features carry the version of the feature map they were built under, and a
// feature that was not observable at the decision is a future-feature leak
// that lane A's acceptance checks reject.
type DecisionEvent struct {
	// SchemaVersion is DecisionEventVersion at write time.
	SchemaVersion int `json:"schema_version"`

	// RunID identifies the whole job; ParentRunID is empty at the root and
	// names the run this run was spawned from otherwise.
	RunID       string `json:"run_id"`
	ParentRunID string `json:"parent_run_id,omitempty"`
	// NodeID names the role/node boundary this decision was taken at (the
	// seat class — one of the five classes the validated picker design
	// derives: reflex, low, worker, high, mastermind).
	NodeID string `json:"node_id"`

	// DecisionTime is when the decision was taken, UTC.
	DecisionTime time.Time `json:"decision_time"`

	// FeatureVersion pins the frozen feature map the event's features were
	// built under. A reader must not mix features from two versions.
	FeatureVersion string `json:"feature_version"`

	// Features are the allowed pre-decision feature values: role, task class,
	// context size, budget remaining, and checks already observed. Every key
	// must be one FeatureVersion defines. FROZEN ALLOWED KEYS (see
	// CONTRACT.md): "role", "task_class", "context_size_tokens",
	// "budget_remaining_usd", "checks_observed".
	Features map[string]string `json:"features"`

	// Candidates is the eligible set AFTER all filters (availability,
	// compatibility, budget): model ids with the descriptor version each was
	// read under.
	Candidates []Candidate `json:"candidates"`

	// Probabilities is the exact final categorical distribution over the
	// eligible candidates AFTER all filters and after the exploration
	// mixture, per the plan's hard requirement: record the exact final
	// probabilities before drawing, so the draw is replayable and the
	// propensity is declared, not reconstructed. Each value is in [0, 1]; the
	// values sum to 1 within tolerance and name exactly the candidates above
	// (an eligible candidate assigned no mass is listed with value 0).
	//
	// SelectedAction does NOT have to be the argmax; it must be drawn
	// according to Probabilities.
	Probabilities map[string]float64 `json:"probabilities"`
	// SelectedAction is the model id assigned to the seat.
	SelectedAction string `json:"selected_action"`

	// PolicyVersion, CatalogVersion and SourceVersion are the versions of the
	// policy that chose, the model catalog it read and the source SHA the
	// selector binary was built from, so any decision is reproducible.
	PolicyVersion  string `json:"policy_version"`
	CatalogVersion string `json:"catalog_version"`
	SourceVersion  string `json:"source_version"`

	// BudgetEnvelope is the budget this decision operated under.
	BudgetEnvelope BudgetEnvelope `json:"budget_envelope"`
}

// Candidate is one eligible model with the descriptor version it was read
// under. Descriptors themselves (benchmark scores, prices) live in the
// catalog, not here — the event records which version was read so the
// decision replays against that exact catalog document.
type Candidate struct {
	ModelID           string `json:"model_id"`
	DescriptorVersion string `json:"descriptor_version"`
}

// BudgetEnvelope is the budget state at decision time.
type BudgetEnvelope struct {
	// CapUSD is the maximum spend this decision may commit from here on.
	CapUSD float64 `json:"cap_usd"`
	// ReservedUSD is spend already reserved for this run's outstanding work
	// (checks, repairs) at decision time.
	ReservedUSD float64 `json:"reserved_usd"`
	// SpentUSD is actual spend already incurred on the run at decision time.
	SpentUSD float64 `json:"spent_usd"`
	// Currency is fixed to "usd"; the field exists so a later currency change
	// cannot silently reinterpret the three numbers above.
	Currency string `json:"currency"`
}

// Validate checks the structural rules a writer cannot be allowed to break:
// the version is the frozen one, the distribution sums to one within
// tolerance and covers the candidates, and the selected action is in both.
func (e DecisionEvent) Validate() error {
	if e.SchemaVersion != DecisionEventVersion {
		return fmt.Errorf("%w: decision event %d, reader knows %d",
			ErrVersion, e.SchemaVersion, DecisionEventVersion)
	}
	if e.RunID == "" || e.NodeID == "" {
		return fmt.Errorf("%w: decision event without run or node id", ErrShape)
	}
	return validateDistribution(e.Probabilities, candidateIDs(e.Candidates), e.SelectedAction)
}

// MarshalDecisionEvent writes the event, refusing to emit an unset version.
func MarshalDecisionEvent(e DecisionEvent) ([]byte, error) {
	if e.SchemaVersion == 0 {
		return nil, fmt.Errorf("%w: decision event written without a version", ErrShape)
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(e)
}

// UnmarshalDecisionEvent reads an event and refuses a version this reader
// does not know, then re-validates the structural rules on the wire form.
func UnmarshalDecisionEvent(data []byte) (DecisionEvent, error) {
	var e DecisionEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return e, fmt.Errorf("decode decision event: %w", err)
	}
	if err := e.Validate(); err != nil {
		return e, err
	}
	return e, nil
}

// ---------------------------------------------------------------
// OutcomeEvent — what actually happened to the job the decision fed.
// ---------------------------------------------------------------

// OutcomeEvent is recorded after independent grading. It is a WHOLE-JOB
// record: verified job success is a label of the job, never copied onto
// individual seats. One job produces one OutcomeEvent per graded job;
// several DecisionEvents may link to it.
type OutcomeEvent struct {
	// SchemaVersion is OutcomeEventVersion at write time.
	SchemaVersion int `json:"schema_version"`

	// RunID and DecisionID link the outcome to the decision that produced the
	// executed work. JobID identifies the graded job. A reader must be able
	// to walk DecisionEvent.RunID -> OutcomeEvent.RunID without guessing.
	RunID      string `json:"run_id"`
	DecisionID string `json:"decision_id"`
	JobID      string `json:"job_id"`

	// Grader provenance: which judge produced the verdict and which version
	// of that judge. A verdict without provenance is not a label.
	GraderName    string `json:"grader_name"`
	GraderVersion string `json:"grader_version"`

	// VerifiedSuccess is the whole-job label: verified job success after the
	// independent checks (and any repairs) the run's pipeline ran. This is
	// NOT the model's own success claim.
	VerifiedSuccess bool `json:"verified_success"`

	// Spend is the actual token/cache/cost accounting of the executed work.
	Spend SpendAccounting `json:"spend"`

	// CostCapCensored is true when the run terminated on the cost cap, which
	// censors completion cost: the actual incurred spend above is observable,
	// the unconstrained cost-to-completion is not, and the predictor's update
	// must treat this outcome's completion cost as censored, not as the true
	// cost.
	CostCapCensored bool `json:"cost_cap_censored"`

	// HarnessError is true when the run failed on harness/tooling error
	// rather than on the model's work. A harness error is not a success or
	// failure signal about the model; the exact treatment in the update is
	// the learner's declared approximation, not this package's.
	HarnessError bool `json:"harness_error"`
}

// SpendAccounting is the actual spend of the graded job. Every field is
// actual: predictions do not live here.
type SpendAccounting struct {
	// TotalUSD is the billed total for the job.
	TotalUSD float64 `json:"total_usd"`
	// TokensIn and TokensOut are the raw billed token counts.
	TokensIn  int64 `json:"tokens_in"`
	TokensOut int64 `json:"tokens_out"`
	// CachedTokensIn is the portion of TokensIn billed at cache terms.
	CachedTokensIn int64 `json:"cached_tokens_in"`
	// Currency is fixed to "usd", matching BudgetEnvelope.
	Currency string `json:"currency"`
}

// Validate checks version, required linkage, grader provenance and spend.
func (e OutcomeEvent) Validate() error {
	if e.SchemaVersion != OutcomeEventVersion {
		return fmt.Errorf("%w: outcome event %d, reader knows %d",
			ErrVersion, e.SchemaVersion, OutcomeEventVersion)
	}
	if e.RunID == "" || e.DecisionID == "" || e.JobID == "" {
		return fmt.Errorf("%w: outcome event without run/decision/job linkage", ErrShape)
	}
	if e.GraderName == "" || e.GraderVersion == "" {
		return fmt.Errorf("%w: outcome event without grader provenance", ErrShape)
	}
	if e.Spend.TotalUSD < 0 {
		return fmt.Errorf("%w: negative total spend", ErrShape)
	}
	return nil
}

// MarshalOutcomeEvent writes the event, refusing to emit an unset version.
func MarshalOutcomeEvent(e OutcomeEvent) ([]byte, error) {
	if e.SchemaVersion == 0 {
		return nil, fmt.Errorf("%w: outcome event written without a version", ErrShape)
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(e)
}

// UnmarshalOutcomeEvent reads an outcome and refuses a version this reader
// does not know.
func UnmarshalOutcomeEvent(data []byte) (OutcomeEvent, error) {
	var e OutcomeEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return e, fmt.Errorf("decode outcome event: %w", err)
	}
	if err := e.Validate(); err != nil {
		return e, err
	}
	return e, nil
}

// ---------------------------------------------------------------
// Predictor — the interface the learner lane implements. This file does NOT
// implement it; it freezes the contract.
// ---------------------------------------------------------------

// FeatureVector is the compact frozen feature map of the plan: the
// fixed-interaction coordinates plus descriptors. Its exact numeric layout is
// pinned by the feature-map version a DecisionEvent carries, not by this
// struct — the struct exists so the interface below is not stringly typed.
type FeatureVector struct {
	// Version is the same string DecisionEvent.FeatureVersion carries.
	Version string
	// Task are the task-side features known at decision time (role, task
	// class, context size, budget remaining, checks already observed).
	Task map[string]float64
	// Candidate are the candidate model's descriptor coordinates (benchmark
	// values with missingness masks, prices, cache terms) under the catalog
	// version the decision recorded.
	Candidate map[string]float64
}

// Prediction is what predict returns.
type Prediction struct {
	// SuccessProb is the personalized probability of verified job success.
	SuccessProb float64
	// CostDist is a distribution over total relevant cost. It is deliberately
	// a distribution, not a mean: a Gaussian head on log cost implies a
	// lognormal mean exp(mu + variance/2), not exp(mu).
	CostDist CostDistribution
	// Uncertainty is the posterior predictive uncertainty of the success
	// head, consumed by the exploration layer — which is a separate decision
	// layer, not part of predict.
	Uncertainty float64
}

// CostDistribution is a parameterized or sampled cost distribution.
// Consumers take the mean and a quantile and MUST NOT assume a mean alone.
type CostDistribution struct {
	// LogNormal is the (mu, sigma) of the log bill when the head is Gaussian
	// in log cost; nil when the head produced samples instead.
	LogNormal *LogNormalParams
	// Samples, when non-empty, are draws from the cost distribution.
	Samples []float64
}

// LogNormalParams parameterizes a lognormal bill.
type LogNormalParams struct {
	Mu    float64
	Sigma float64
}

// Mean returns the lognormal mean exp(mu + sigma^2/2), or the sample mean.
func (c CostDistribution) Mean() float64 {
	if c.LogNormal != nil {
		return math.Exp(c.LogNormal.Mu + c.LogNormal.Sigma*c.LogNormal.Sigma/2)
	}
	if len(c.Samples) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range c.Samples {
		s += v
	}
	return s / float64(len(c.Samples))
}

// OutcomeEvidence is the evidence update takes. It carries the outcome event
// plus the feature state the predictor itself recorded for that decision, so
// the update cannot be fed features the predictor never saw.
type OutcomeEvidence struct {
	Decision DecisionEvent
	Outcome  OutcomeEvent
	// Features is the predictor's own record of the feature vector it scored.
	Features FeatureVector
}

// Predictor is the interface the learner implements. CONTRACT SUMMARY:
//
//   - predict scores a task feature vector against candidate descriptors and
//     returns success probability, a cost DISTRIBUTION and uncertainty.
//   - update ingests outcome evidence. THE APPROXIMATION IS NAMED, NOT
//     HIDDEN: a vanilla Gaussian Kalman update on 0/1 outcomes is NOT an
//     exact posterior — the plan requires logistic filtering with an
//     explicitly named approximation (Laplace / assumed-density / extended),
//     documented in CONTRACT.md and in the implementation. Continuous
//     log-cost observations admit exact Kalman updates under the linear
//     model. Censored completion cost (OutcomeEvent.CostCapCensored) must be
//     handled as censored, not observed.
//   - export/import move the predictor's versioned state. The envelope is
//     versioned by PredictorStateVersion; the payload is opaque to the
//     policy. Import refuses a payload written under a state version this
//     build does not know, and refuses a state whose feature-map version
//     differs from this predictor's frozen coordinates: old coefficients are
//     not valid in a rotated basis.
type Predictor interface {
	Predict(task FeatureVector, candidates []Candidate) (Prediction, error)
	Update(ev OutcomeEvidence) error
	Export() ([]byte, error) // PredictorStateVersion envelope
	Import(blob []byte) error
}

// PredictorEnvelope is the export wire shape: the envelope version, the
// predictor's own state version, and the opaque payload.
type PredictorEnvelope struct {
	SchemaVersion int `json:"schema_version"` // PredictorStateVersion
	// StateVersion is the interior version the predictor declares.
	StateVersion int `json:"state_version"`
	// FeatureVersion must match the predictor's frozen feature coordinates.
	FeatureVersion string `json:"feature_version"`
	// Payload is opaque to everyone but the predictor implementation that
	// wrote it. Prompts, per-task texts and embeddings must NOT appear here.
	Payload []byte `json:"payload"`
}

// MarshalPredictorEnvelope wraps a payload for export.
func MarshalPredictorEnvelope(stateVersion int, featureVersion string, payload []byte) ([]byte, error) {
	if payload == nil {
		return nil, fmt.Errorf("%w: export of a nil predictor payload", ErrShape)
	}
	return json.Marshal(PredictorEnvelope{
		SchemaVersion:  PredictorStateVersion,
		StateVersion:   stateVersion,
		FeatureVersion: featureVersion,
		Payload:        payload,
	})
}

// UnmarshalPredictorEnvelope reads the envelope and refuses an envelope
// version this reader does not know. It does not interpret the payload.
func UnmarshalPredictorEnvelope(blob []byte) (PredictorEnvelope, error) {
	var env PredictorEnvelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return env, fmt.Errorf("decode predictor envelope: %w", err)
	}
	if env.SchemaVersion != PredictorStateVersion {
		return env, fmt.Errorf("%w: predictor envelope %d, reader knows %d",
			ErrVersion, env.SchemaVersion, PredictorStateVersion)
	}
	if env.FeatureVersion == "" {
		return env, fmt.Errorf("%w: predictor envelope without a feature version", ErrShape)
	}
	return env, nil
}

// ---------------------------------------------------------------
// SharedSnapshot — what travels between installations.
// ---------------------------------------------------------------

// SharedSnapshot is the shared learned artifact: numerical learned structure
// only. TEXT, PROMPTS AND PER-TASK EMBEDDINGS MUST NOT APPEAR IN IT — this is
// stated on the type, in CONTRACT.md, and enforced to the extent a schema
// can: the snapshot carries no free-text payload field at all, and the only
// string-typed content is identity and version naming.
type SharedSnapshot struct {
	// SchemaVersion is SharedSnapshotVersion at write time.
	SchemaVersion int `json:"schema_version"`

	// CoordinateVersion pins the shared representation the parameters are
	// expressed in. Old local posteriors are not valid under a rotated
	// coordinate basis, so migration is a versioned act, never a merge.
	CoordinateVersion string `json:"coordinate_version"`

	// FeatureVersion of the task feature map the parameters were fit under.
	FeatureVersion string `json:"feature_version"`

	// ContributionIDs are the deduplication keys of the contribution batches
	// already folded into this snapshot. An aggregator that receives a
	// contribution whose id is here REJECTS it rather than counting it
	// again: local posteriors and shared state share common evidence, and
	// averaging both as independent observations double-counts it.
	ContributionIDs []string `json:"contribution_ids"`

	// Parameters are the shared parameter values (shared W and b per the
	// model document), keyed by a stable parameter name under
	// CoordinateVersion.
	Parameters map[string]float64 `json:"parameters"`

	// Uncertainty is the between-install/parameter uncertainty, keyed the
	// same way as Parameters (a variance, or a covariance serialized
	// parameter-by-parameter as the writer declares).
	Uncertainty map[string]float64 `json:"uncertainty"`

	// Counts records evidence counts behind the parameters, keyed the same
	// way, so a reader can distinguish "confident" from "barely seen".
	Counts map[string]int `json:"counts"`

	// ModelIDs are the model identities the parameters cover, so a snapshot
	// that does not mention a model can never be read as scoring it.
	ModelIDs []string `json:"model_ids"`

	// Generated is the snapshot's UTC generation time; GeneratedBy names the
	// site or aggregator that produced it.
	Generated   time.Time `json:"generated"`
	GeneratedBy string    `json:"generated_by"`
}

// Validate checks version, that contribution ids are unique (the
// deduplication contract is load-bearing), and that uncertainty is
// non-negative where present.
func (s SharedSnapshot) Validate() error {
	if s.SchemaVersion != SharedSnapshotVersion {
		return fmt.Errorf("%w: shared snapshot %d, reader knows %d",
			ErrVersion, s.SchemaVersion, SharedSnapshotVersion)
	}
	if s.CoordinateVersion == "" || s.FeatureVersion == "" {
		return fmt.Errorf("%w: shared snapshot without coordinate or feature version", ErrShape)
	}
	seen := make(map[string]bool, len(s.ContributionIDs))
	for _, id := range s.ContributionIDs {
		if id == "" {
			return fmt.Errorf("%w: empty contribution id", ErrShape)
		}
		if seen[id] {
			return fmt.Errorf("%w: duplicate contribution id %q in snapshot", ErrShape, id)
		}
		seen[id] = true
	}
	for name, v := range s.Uncertainty {
		if v < 0 {
			return fmt.Errorf("%w: negative uncertainty on %q", ErrShape, name)
		}
	}
	return nil
}

// MarshalSharedSnapshot writes the snapshot, refusing to emit an unset
// version.
func MarshalSharedSnapshot(s SharedSnapshot) ([]byte, error) {
	if s.SchemaVersion == 0 {
		return nil, fmt.Errorf("%w: shared snapshot written without a version", ErrShape)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// UnmarshalSharedSnapshot reads a snapshot and refuses a version this reader
// does not know.
func UnmarshalSharedSnapshot(data []byte) (SharedSnapshot, error) {
	var s SharedSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("decode shared snapshot: %w", err)
	}
	if err := s.Validate(); err != nil {
		return s, err
	}
	return s, nil
}

// ---------------------------------------------------------------
// shared validation helpers
// ---------------------------------------------------------------

// ProbTolerance is how far the final categorical distribution may drift from
// an exact sum of one after JSON round trip and float summation.
const ProbTolerance = 1e-6

func candidateIDs(cs []Candidate) []string {
	ids := make([]string, 0, len(cs))
	for _, c := range cs {
		ids = append(ids, c.ModelID)
	}
	return ids
}

// validateDistribution is the shared check behind the plan's hard requirement:
// the recorded probabilities are a categorical distribution over the
// candidate set, and the selected action is in it.
func validateDistribution(probs map[string]float64, candidates []string, selected string) error {
	set := make(map[string]bool, len(candidates))
	for _, id := range candidates {
		set[id] = true
	}
	sum := 0.0
	for id, p := range probs {
		if !set[id] {
			return fmt.Errorf("%w: probability names non-candidate %q", ErrShape, id)
		}
		if p < 0 || p > 1 {
			return fmt.Errorf("%w: probability %g out of range on %q", ErrShape, p, id)
		}
		sum += p
	}
	if math.Abs(sum-1) > ProbTolerance {
		return fmt.Errorf("%w: assignment probabilities sum to %g, not 1", ErrShape, sum)
	}
	if !set[selected] {
		return fmt.Errorf("%w: selected action %q is not in the candidate set", ErrShape, selected)
	}
	return nil
}

// SortIDs returns the ids in sorted order, for tests and consumers that need
// a canonical candidate order.
func SortIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}
