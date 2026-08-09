package failuretriage

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	out := []fixture{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<23)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 70 {
		t.Fatalf("expected at least 70 fixtures, got %d", len(fixtures))
	}
	diagnoses := map[FailureDiagnosis]bool{}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			if fx.Fn != "classifyFailure" {
				t.Fatalf("unknown fixture function %q", fx.Fn)
			}
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
				t.Fatalf("decode args: %v", err)
			}
			if len(args) != 1 {
				t.Fatalf("classifyFailure wants 1 arg, got %d", len(args))
			}
			var input FailureTriageInput
			if err := json.Unmarshal(args[0], &input); err != nil {
				t.Fatalf("decode input: %v", err)
			}
			result := ClassifyFailure(input)
			diagnoses[result.Diagnosis] = true
			got, err := jscompat.Stringify(result)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
	for _, diagnosis := range []FailureDiagnosis{
		DiagnosisEnvFlaky,
		DiagnosisLoop,
		DiagnosisTooHard,
		DiagnosisLocalization,
		DiagnosisSpecMisread,
		DiagnosisUnknown,
	} {
		if !diagnoses[diagnosis] {
			t.Errorf("no fixture reached %s", diagnosis)
		}
	}
}

func TestThresholdExport(t *testing.T) {
	if FailureTriageThresholds.LoopIdenticalStreak != 3 ||
		FailureTriageThresholds.LoopSameFileReads != 4 ||
		FailureTriageThresholds.TooHardMinTurns != 12 ||
		FailureTriageThresholds.TooHardMinEdits != 4 ||
		FailureTriageThresholds.TooHardEscalateRepairRounds != 2 {
		t.Fatalf("unexpected thresholds: %#v", FailureTriageThresholds)
	}
}
