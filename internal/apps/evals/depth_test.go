package evals

import "testing"

func TestStaticDepthCountsWorkInSavedDefinition(t *testing.T) {
	depth := measureDepth(map[string]string{
		"app.json":    `{"triggers":[{"on":"cron","run":"review"},{"on":"cron","run":"missing"}]}`,
		"sample.json": `{"records":{"bill":[{"id":"1"},{"id":"2"}]}}`,
		"runs/review.yaml": `
run: review
steps:
  - id: extract
    ai: {prompt: Extract, output: {type: object}}
  - id: items
    loop: {over: "$params.bills"}
    steps:
      - id: summarize
        agent: summarize
        prompt: Summarize
      - id: write
        tool: ledger.draft
        gate: {approval: write, who: $me}
  - id: signoff
    gate: {approval: review, who: $me}
`,
	})
	if depth.AISteps != 2 || depth.Triggers != 1 || depth.GatedWritebacks != 1 || depth.SampleRows != 2 || depth.Score != 3 {
		t.Fatalf("depth = %+v", depth)
	}
}
