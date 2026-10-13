package secaf

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// RECON SUCCESS CANNOT MASK FAILED HUNTS. Fail the actual hunter schema calls
// after the real recon phase, and inspect the ending and all three reports.
func TestFailedHuntsCannotReportACleanAudit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial bool
		code    int
		status  string
	}{
		{"all", false, 400, delegate.StatusFail},
		{"partial", true, 400, delegate.StatusFail},
		{"dollar ceiling", false, 402, delegate.StatusBudget},
		{"refused key", false, 403, delegate.StatusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var scans atomic.Int32
			models := &stubModels{reject: func(title string) int {
				if title != "ScanLocationsResult" {
					return 0
				}
				n := scans.Add(1)
				if tc.partial && n > 1 {
					return 0
				}
				return tc.code
			}}
			repo := vulnerableRepo(t)
			before := snapshot(t, repo)
			host := &stubHost{workspace: repo, records: t.TempDir(), api: models.serve(t)}
			run(context.Background(), host, options{brief: "whole repository quick", severity: "low", sessions: 4, maxTurns: 5}, io.Discard)
			if host.ending == nil || host.ending.Status != tc.status {
				t.Fatalf("failed analysis ended %+v, want %s", host.ending, tc.status)
			}
			ending := host.ending
			if ending.Extra["status"] == "pass" || !strings.Contains(ending.Message, "incomplete") || !strings.Contains(firstLine(ending.Deliverable), "incomplete") {
				t.Fatalf("failure hidden in ending %+v", ending)
			}
			data, err := os.ReadFile(filepath.Join(host.records, reportJSON))
			if err != nil {
				t.Fatal(err)
			}
			var result schemas.SecurityAuditResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			coverage, ok := result.Metadata["hunt_coverage"].(map[string]any)
			if !ok || coverage["attempted"] != json.Number("6") {
				t.Fatalf("missing hunt accounting: %#v", result.Metadata)
			}
			failed, _ := coverage["failures"].([]any)
			want := 6
			if tc.partial {
				want = 1
			}
			if len(failed) != want {
				t.Fatalf("failed attempts %d, want %d: %#v", len(failed), want, coverage)
			}
			if tc.partial && len(result.Findings) == 0 {
				t.Fatal("findings from successful hunts were discarded")
			}
			readable, err := os.ReadFile(filepath.Join(host.records, reportMarkdown))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(readable), "incomplete") {
				t.Fatalf("readable report conceals failed hunts: %s", readable)
			}
			if !tc.partial && strings.Contains(strings.ToLower(string(readable)), "nothing it could show exploitable") {
				t.Fatal("all-failed report makes a clean claim")
			}
			data, err = os.ReadFile(filepath.Join(host.records, reportSARIF))
			if err != nil {
				t.Fatal(err)
			}
			var sarif struct {
				Runs []struct {
					Invocations []struct {
						ExecutionSuccessful bool  `json:"executionSuccessful"`
						Notifications       []any `json:"toolExecutionNotifications"`
					} `json:"invocations"`
				} `json:"runs"`
			}
			if err := json.Unmarshal(data, &sarif); err != nil {
				t.Fatal(err)
			}
			if len(sarif.Runs) != 1 || len(sarif.Runs[0].Invocations) != 1 || sarif.Runs[0].Invocations[0].ExecutionSuccessful || len(sarif.Runs[0].Invocations[0].Notifications) == 0 {
				t.Fatalf("SARIF conceals incomplete execution: %s", data)
			}
			if after := snapshot(t, repo); !reflect.DeepEqual(after, before) {
				t.Fatal("audit changed the fixture")
			}
		})
	}
}

// A refusal can arrive after every hunter answered. The proof chain may keep
// unclear findings, but that partial result must retain the global stop.
func TestAModelRefusalDuringProofCannotBecomeAPass(t *testing.T) {
	for _, code := range []int{402, 403} {
		t.Run(httpStatusWord(code), func(t *testing.T) {
			var refused atomic.Int32
			models := &stubModels{reject: func(title string) int {
				if title == "DataFlowTrace" {
					refused.Add(1)
					return code
				}
				return 0
			}}
			host := &stubHost{workspace: vulnerableRepo(t), records: t.TempDir(), api: models.serve(t)}
			run(t.Context(), host, options{brief: "whole repository quick", severity: "low", sessions: 4, maxTurns: 5}, io.Discard)
			want := delegate.StatusFail
			if code == 402 {
				want = delegate.StatusBudget
			}
			if refused.Load() == 0 || host.ending == nil || host.ending.Status != want {
				t.Fatalf("late refusal count=%d ending=%+v want=%s", refused.Load(), host.ending, want)
			}
			if !strings.Contains(firstLine(host.ending.Deliverable), "analysis incomplete") {
				t.Fatalf("global stop concealed: %+v", host.ending)
			}
			data, err := os.ReadFile(filepath.Join(host.records, reportJSON))
			if err != nil {
				t.Fatal(err)
			}
			var result schemas.SecurityAuditResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Findings) == 0 || result.Metadata["hunt_coverage"] != nil || result.Metadata["analysis_incomplete"] == nil {
				t.Fatalf("retained result/stop incorrect: %+v", result)
			}
			data, err = os.ReadFile(filepath.Join(host.records, reportSARIF))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"executionSuccessful": false`) {
				t.Fatalf("SARIF lost late refusal: %s", data)
			}
		})
	}
}

func httpStatusWord(code int) string {
	if code == 402 {
		return "dollar ceiling"
	}
	return "refused key"
}

func TestAStopDuringProofKeepsTheControlledFailureEnding(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stopped atomic.Bool
	models := &stubModels{reject: func(title string) int {
		if title == "DataFlowTrace" {
			stopped.Store(true)
			cancel()
			return 400
		}
		return 0
	}}
	host := &stubHost{workspace: vulnerableRepo(t), records: t.TempDir(), api: models.serve(t)}
	run(ctx, host, options{brief: "whole repository quick", severity: "low", sessions: 4, maxTurns: 5}, io.Discard)
	if !stopped.Load() || host.ending == nil || host.ending.Status != delegate.StatusFail || !strings.Contains(host.ending.Message, "stopped") {
		t.Fatalf("controlled stop became %+v", host.ending)
	}
}

func TestUnreadableAccountingMakesNoCleanClaim(t *testing.T) {
	result := schemas.NewSecurityAuditResult()
	result.Metadata["hunt_coverage"] = map[string]any{"attempted": 6}
	for _, text := range []string{summary(Scope{}, nil, result, nil, "", ""), outcomeLine(result, ""), markdownReport(result, runFacts{})} {
		if !strings.Contains(strings.ToLower(text), "incomplete") || strings.Contains(strings.ToLower(text), "nothing it could show exploitable") || strings.Contains(text, "all 1 hunts failed") || strings.Contains(text, "completed hunts") {
			t.Fatalf("unknown accounting became a claim: %s", text)
		}
	}
}
