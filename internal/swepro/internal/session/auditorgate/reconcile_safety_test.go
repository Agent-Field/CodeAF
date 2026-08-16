package auditorgate

import "testing"

// Contract C4: green build/test evidence refutes "you did not check". It must
// never refute "the check failed". These are the near-misses that would make
// the absence-blocker matcher dangerous if it were loosened.
func TestReconcileNeverDropsSubstantiveBlockers(t *testing.T) {
	green := []any{
		map[string]any{"cmd": "go build ./...", "kind": "build", "exit": float64(0)},
		map[string]any{"cmd": "go test ./...", "kind": "test", "exit": float64(0)},
	}
	step := float64(2)
	for _, detail := range []string{
		"TestParseEdgeCase fails on empty input",
		"Build and tests not yet verified independently for the parser package",
		"spec example 3 did not match observed output",
		"not verified: TestFoo still red",
		"repro was not reproduced; the crash persists",
	} {
		verdict := AuditorVerdict{
			Verdict:  VerdictFail,
			Blockers: []Blocker{{Detail: detail, Step: &step}},
		}
		if _, refuted := ReconcileHarnessVerification(verdict, green); refuted {
			t.Fatalf("substantive blocker was dropped: %q", detail)
		}
	}
}

// A promotion requires green evidence for both roles. Partial or red evidence
// leaves the auditor's fail standing.
func TestReconcileRequiresBothRolesGreen(t *testing.T) {
	step := float64(2)
	verdict := AuditorVerdict{
		Verdict:  VerdictFail,
		Blockers: []Blocker{{Detail: "Build and tests not yet verified independently", Step: &step}},
	}
	for name, commands := range map[string][]any{
		"test only":  {map[string]any{"kind": "test", "exit": float64(0)}},
		"build only": {map[string]any{"kind": "build", "exit": float64(0)}},
		"red test": {
			map[string]any{"kind": "build", "exit": float64(0)},
			map[string]any{"kind": "test", "exit": float64(1)},
		},
	} {
		if _, refuted := ReconcileHarnessVerification(verdict, commands); refuted {
			t.Fatalf("%s promoted a fail to a pass", name)
		}
	}
}
