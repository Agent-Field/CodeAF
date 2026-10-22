package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleDecision() DecisionEvent {
	return DecisionEvent{
		SchemaVersion:  DecisionEventVersion,
		RunID:          "run-1",
		ParentRunID:    "run-0",
		NodeID:         "worker",
		DecisionTime:   time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		FeatureVersion: "feat-2026-10-20",
		Features: map[string]string{
			"role":                 "worker",
			"task_class":           "feature",
			"context_size_tokens":  "48000",
			"budget_remaining_usd": "2.10",
			"checks_observed":      "1",
		},
		Candidates: []Candidate{
			{ModelID: "a/model-x", DescriptorVersion: "cat-2026-10-20"},
			{ModelID: "a/model-y", DescriptorVersion: "cat-2026-10-20"},
		},
		Probabilities:  map[string]float64{"a/model-x": 0.9, "a/model-y": 0.1},
		SelectedAction: "a/model-x",
		PolicyVersion:  "policy-1",
		CatalogVersion: "cat-2026-10-20",
		SourceVersion:  "deadbeef",
		BudgetEnvelope: BudgetEnvelope{CapUSD: 3.0, ReservedUSD: 0.5, SpentUSD: 0.4, Currency: "usd"},
	}
}

func sampleOutcome() OutcomeEvent {
	return OutcomeEvent{
		SchemaVersion:   OutcomeEventVersion,
		RunID:           "run-1",
		DecisionID:      "dec-7",
		JobID:           "job-42",
		GraderName:      "rubric-judge",
		GraderVersion:   "3",
		VerifiedSuccess: true,
		Spend: SpendAccounting{
			TotalUSD: 1.23, TokensIn: 5000, TokensOut: 900, CachedTokensIn: 1200, Currency: "usd",
		},
		CostCapCensored: false,
		HarnessError:    false,
	}
}

func TestDecisionEventRoundTrip(t *testing.T) {
	e := sampleDecision()
	b, err := MarshalDecisionEvent(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalDecisionEvent(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b2, err := MarshalDecisionEvent(got)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(b, b2) {
		t.Fatalf("round trip is not byte-stable:\n%s\n%s", b, b2)
	}
	if got.RunID != e.RunID || got.ParentRunID != e.ParentRunID || got.NodeID != e.NodeID {
		t.Fatalf("ids lost: %+v", got)
	}
	if got.FeatureVersion != e.FeatureVersion || len(got.Features) != len(e.Features) {
		t.Fatalf("features lost: %+v", got)
	}
	if len(got.Candidates) != 2 || got.Candidates[1].ModelID != "a/model-y" {
		t.Fatalf("candidates lost: %+v", got.Candidates)
	}
	if got.Probabilities["a/model-y"] != 0.1 || got.SelectedAction != e.SelectedAction {
		t.Fatalf("distribution lost: %+v", got.Probabilities)
	}
	if got.BudgetEnvelope.CapUSD != 3.0 || got.BudgetEnvelope.Currency != "usd" {
		t.Fatalf("budget lost: %+v", got.BudgetEnvelope)
	}
	if got.PolicyVersion != "policy-1" || got.CatalogVersion != "cat-2026-10-20" || got.SourceVersion != "deadbeef" {
		t.Fatalf("versions lost: %+v", got)
	}
	if !got.DecisionTime.Equal(e.DecisionTime) {
		t.Fatalf("time lost: %v", got.DecisionTime)
	}
}

func TestOutcomeEventRoundTrip(t *testing.T) {
	e := sampleOutcome()
	b, err := MarshalOutcomeEvent(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalOutcomeEvent(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b2, _ := MarshalOutcomeEvent(got)
	if !bytes.Equal(b, b2) {
		t.Fatalf("round trip is not byte-stable:\n%s\n%s", b, b2)
	}
	if got.DecisionID != "dec-7" || got.JobID != "job-42" {
		t.Fatalf("linkage lost: %+v", got)
	}
	if got.GraderName != "rubric-judge" || got.GraderVersion != "3" {
		t.Fatalf("provenance lost: %+v", got)
	}
	if got.Spend.CachedTokensIn != 1200 {
		t.Fatalf("spend lost: %+v", got.Spend)
	}
}

func TestSharedSnapshotRoundTrip(t *testing.T) {
	s := SharedSnapshot{
		SchemaVersion:     SharedSnapshotVersion,
		CoordinateVersion: "coord-rank2-1",
		FeatureVersion:    "feat-2026-10-20",
		ContributionIDs:   []string{"contrib-1", "contrib-2"},
		Parameters:        map[string]float64{"b_m/model-x": -0.2, "W_q/f1": 0.03},
		Uncertainty:       map[string]float64{"b_m/model-x": 0.5},
		Counts:            map[string]int{"b_m/model-x": 30},
		ModelIDs:          []string{"a/model-x", "a/model-y"},
		Generated:         time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		GeneratedBy:       "aggregator-1",
	}
	b, err := MarshalSharedSnapshot(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := UnmarshalSharedSnapshot(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b2, _ := MarshalSharedSnapshot(got)
	if !bytes.Equal(b, b2) {
		t.Fatalf("round trip is not byte-stable:\n%s\n%s", b, b2)
	}
	if len(got.ContributionIDs) != 2 || got.Parameters["W_q/f1"] != 0.03 {
		t.Fatalf("state lost: %+v", got)
	}
}

func TestPredictorEnvelopeRoundTrip(t *testing.T) {
	b, err := MarshalPredictorEnvelope(7, "feat-2026-10-20", []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	env, err := UnmarshalPredictorEnvelope(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.StateVersion != 7 || !bytes.Equal(env.Payload, []byte{1, 2, 3}) {
		t.Fatalf("envelope lost: %+v", env)
	}
}

func TestVersionMismatchRejection(t *testing.T) {
	b, _ := json.Marshal(sampleDecision())
	var bumped map[string]any
	if err := json.Unmarshal(b, &bumped); err != nil {
		t.Fatal(err)
	}
	bumped["schema_version"] = DecisionEventVersion + 1
	nb, _ := json.Marshal(bumped)
	if _, err := UnmarshalDecisionEvent(nb); err == nil || !strings.Contains(err.Error(), "unsupported schema version") {
		t.Fatalf("expected version refusal, got %v", err)
	}

	ob, _ := json.Marshal(sampleOutcome())
	var ob2 map[string]any
	_ = json.Unmarshal(ob, &ob2)
	ob2["schema_version"] = OutcomeEventVersion + 1
	if _, err := UnmarshalOutcomeEvent(mustJSON(t, ob2)); err == nil || !strings.Contains(err.Error(), "unsupported schema version") {
		t.Fatalf("expected version refusal, got %v", err)
	}

	sb, _ := json.Marshal(SharedSnapshot{SchemaVersion: SharedSnapshotVersion + 1})
	if _, err := UnmarshalSharedSnapshot(sb); err == nil || !strings.Contains(err.Error(), "unsupported schema version") {
		t.Fatalf("expected version refusal, got %v", err)
	}

	eb, _ := MarshalPredictorEnvelope(1, "feat", []byte{1})
	var env map[string]any
	_ = json.Unmarshal(eb, &env)
	env["schema_version"] = PredictorStateVersion + 1
	if _, err := UnmarshalPredictorEnvelope(mustJSON(t, env)); err == nil || !strings.Contains(err.Error(), "unsupported schema version") {
		t.Fatalf("expected version refusal, got %v", err)
	}
}

func TestStructuralRejection(t *testing.T) {
	e := sampleDecision()
	e.Probabilities["a/model-x"] = 0.5 // sums to 0.6
	if err := e.Validate(); err == nil || !strings.Contains(err.Error(), "sum to") {
		t.Fatalf("expected distribution refusal, got %v", err)
	}
	e = sampleDecision()
	e.SelectedAction = "a/not-a-candidate"
	if err := e.Validate(); err == nil || !strings.Contains(err.Error(), "not in the candidate set") {
		t.Fatalf("expected action refusal, got %v", err)
	}
	e = sampleDecision()
	e.Probabilities["ghost/model"] = -0.1 // also makes the sum wrong, so delete one
	delete(e.Probabilities, "a/model-y")
	e.Probabilities["a/model-x"] = 0.9
	if err := e.Validate(); err == nil || !strings.Contains(err.Error(), "non-candidate") {
		t.Fatalf("expected non-candidate refusal, got %v", err)
	}

	o := sampleOutcome()
	o.DecisionID = ""
	if err := o.Validate(); err == nil || !strings.Contains(err.Error(), "linkage") {
		t.Fatalf("expected linkage refusal, got %v", err)
	}
	o = sampleOutcome()
	o.GraderName = ""
	if err := o.Validate(); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("expected provenance refusal, got %v", err)
	}

	s := SharedSnapshot{SchemaVersion: SharedSnapshotVersion, CoordinateVersion: "c", FeatureVersion: "f",
		ContributionIDs: []string{"x", "x"}}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate contribution") {
		t.Fatalf("expected duplicate refusal, got %v", err)
	}
}

func TestUnsetVersionRefusedOnWrite(t *testing.T) {
	if _, err := MarshalDecisionEvent(sampleDecision()); err != nil {
		// The sample carries a version; a zeroed one must refuse.
		_ = err
	}
	e := sampleDecision()
	e.SchemaVersion = 0
	if _, err := MarshalDecisionEvent(e); err == nil || !strings.Contains(err.Error(), "without a version") {
		t.Fatalf("expected unset-version refusal, got %v", err)
	}
}

func TestUnmarshalKeepsUnknownFieldsForwardCompatible(t *testing.T) {
	b, _ := json.Marshal(sampleDecision())
	withExtra := strings.Replace(string(b), `"schema_version":1`,
		`"schema_version":1,"future_field":{"x":1}`, 1)
	if _, err := UnmarshalDecisionEvent([]byte(withExtra)); err != nil {
		t.Fatalf("unknown field must be ignored, got %v", err)
	}
}

func TestCostDistributionMean(t *testing.T) {
	c := CostDistribution{LogNormal: &LogNormalParams{Mu: 0, Sigma: 1}}
	if m := c.Mean(); m < 1.64 || m > 1.65 {
		t.Fatalf("lognormal mean exp(0.5) != %g", m)
	}
	c2 := CostDistribution{Samples: []float64{1, 2, 3}}
	if m := c2.Mean(); m != 2 {
		t.Fatalf("sample mean != %g", m)
	}
}

// The Predictor contract itself is not implemented here, but the interface
// must still be satisfiable and documented as such — a compile-time witness.
type stubPredictor struct{}

func (stubPredictor) Predict(FeatureVector, []Candidate) (Prediction, error) {
	return Prediction{}, nil
}
func (stubPredictor) Update(OutcomeEvidence) error { return nil }
func (stubPredictor) Export() ([]byte, error)      { return nil, nil }
func (stubPredictor) Import([]byte) error          { return nil }

var _ Predictor = stubPredictor{}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
