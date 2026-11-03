package api_test

import (
 "bytes"
 "encoding/json"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
 "github.com/Agent-Field/codeaf/internal/factory/client"
)

// This is the StepReport schema at the specified merge base, using unchanged
// exported types for its nested fields. serve.decode sets DisallowUnknownFields.
type reviewOldStepReport struct {
 RoundLimit int `json:"round_limit,omitempty"`
 Verification *factory.StepVerification `json:"verification,omitempty"`
 Chat string `json:"chat,omitempty"`
 Name string `json:"name"`
 Model string `json:"model,omitempty"`
 State string `json:"state,omitempty"`
 Round int `json:"round,omitempty"`
 StartedAt *time.Time `json:"started_at,omitempty"`
 Result *api.StepResult `json:"result,omitempty"`
 Rounds []api.RoundLine `json:"rounds,omitempty"`
 Failure *api.Failure `json:"failure,omitempty"`
}

func TestReviewNewWorkerOldFactoryPaidReport(t *testing.T) {
 const realSpend = .2
 report := api.FromRunReport(factory.RunReport{Stages: []factory.StageReport{{
  Name: "proof", Round: 1, State: factory.PhaseDone,
  RoundCosts: map[int]float64{1: realSpend}, Result: &factory.StageResult{Done: true, Spent: realSpend},
 }}})
 request, err := client.PrepareWorkerReport(report, "compat-test")
 if err != nil { t.Fatal(err) }
 raw := request.Body
 var oldReport struct {
  Cost float64 `json:"cost,omitempty"`
  Steps []reviewOldStepReport `json:"steps,omitempty"`
 }
 decoder := json.NewDecoder(bytes.NewReader(raw))
 decoder.DisallowUnknownFields()
 if err := decoder.Decode(&oldReport); err != nil {
  t.Fatalf("old factory rejected paid report: job spend 0 < real spend %.2f: %v", realSpend, err)
 }
 if oldReport.Cost < realSpend {
  t.Fatalf("old factory job spend %.2f < real spend %.2f", oldReport.Cost, realSpend)
 }
}
