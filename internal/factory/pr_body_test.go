package factory

import (
	"strings"
	"testing"
)

func TestPublicationUsesFinalSummaryAndCommandScopedEvidence(t *testing.T) {
	it := Item{Kind: KindIssue, Num: 12, Title: "Merge retrieval", Stream: &Stream{Phases: []Phase{
		{Name: "write", Result: &StageResult{Output: "An obsolete partial implementation. All 50+ tests pass."}},
		{Name: "proof", Result: &StageResult{PublishSummary: []string{"Merge both retrieval sources and validate required warehouse settings."}, Notes: []string{`Command receipt: {"command":"go test -race ./retrieval","outcome":"passed"}`}, Tests: &TestCounts{Passed: 17}}},
	}}}
	body := PublishBody(it, "")
	if !strings.Contains(body, "Merge both retrieval sources") || !strings.Contains(body, "go test -race ./retrieval") || strings.Contains(body, "obsolete") || strings.Contains(body, "17 passed") || strings.Contains(body, "50+") {
		t.Fatal(body)
	}
}
