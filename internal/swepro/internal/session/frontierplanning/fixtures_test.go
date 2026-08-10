package frontierplanning

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
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
	sc.Buffer(make([]byte, 0, 1<<20), 1<<22)
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

func decodeArgs(t *testing.T, fx fixture) []json.RawMessage {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
		t.Fatalf("decode args_json: %v", err)
	}
	return args
}

func callFixture(t *testing.T, fx fixture) any {
	t.Helper()
	args := decodeArgs(t, fx)
	oneString := func() string {
		var value string
		if len(args) != 1 || json.Unmarshal(args[0], &value) != nil {
			t.Fatalf("%s wants one string arg", fx.Fn)
		}
		return value
	}
	switch fx.Fn {
	case "ledgerScenario":
		var roles []FrontierPlanningRole
		if len(args) != 1 || json.Unmarshal(args[0], &roles) != nil {
			t.Fatalf("ledgerScenario wants roles")
		}
		ledger := CreateFrontierPlanningLedger()
		results := make([]bool, 0, len(roles))
		for _, role := range roles {
			results = append(results, ledger.TryTake(role))
		}
		return struct {
			Results []bool                `json:"results"`
			Used    FrontierPlanningUsage `json:"used"`
		}{results, ledger.Used}
	case "buildGlossaryPrompt":
		var input struct {
			TaskText       string           `json:"taskText"`
			Identifiers    []SpecIdentifier `json:"identifiers"`
			SiblingContext string           `json:"siblingContext"`
		}
		mustOne(t, args, &input)
		return BuildGlossaryPrompt(input)
	case "parseGlossary":
		return ParseGlossary(oneString())
	case "enforceableGlossaryIdentifiers":
		if len(args) != 2 {
			t.Fatalf("enforceableGlossaryIdentifiers wants 2 args")
		}
		var predictions []GlossaryPrediction
		var explicit []SpecIdentifier
		mustDecode(t, args[0], &predictions)
		mustDecode(t, args[1], &explicit)
		return EnforceableGlossaryIdentifiers(predictions, explicit)
	case "glossaryContextBlock":
		var predictions []GlossaryPrediction
		mustOne(t, args, &predictions)
		return GlossaryContextBlock(predictions)
	case "buildContractReviewPrompt":
		var input struct {
			TaskText              string `json:"taskText"`
			ContractJSON          string `json:"contractJson"`
			ContractFileText      string `json:"contractFileText"`
			ContractResultSummary string `json:"contractResultSummary"`
		}
		mustOne(t, args, &input)
		return BuildContractReviewPrompt(input)
	case "parseContractReview":
		return ParseContractReview(oneString())
	case "contractReviewEvidenceBlock":
		var review ContractReview
		mustOne(t, args, &review)
		return ContractReviewEvidenceBlock(review)
	case "buildSketchPrompt":
		return BuildSketchPrompt(oneString())
	case "parseSketch":
		return ParseSketch(oneString())
	case "sketchJaccard", "sketchesDisagree":
		if len(args) != 2 {
			t.Fatalf("%s wants 2 args", fx.Fn)
		}
		var a, b PlanSketch
		mustDecode(t, args[0], &a)
		mustDecode(t, args[1], &b)
		if fx.Fn == "sketchJaccard" {
			return jscompat.JSNumber(SketchJaccard(a, b))
		}
		return SketchesDisagree(a, b)
	case "buildSketchArbitrationPrompt":
		var input struct {
			TaskText string     `json:"taskText"`
			SketchA  PlanSketch `json:"sketchA"`
			SketchB  PlanSketch `json:"sketchB"`
		}
		mustOne(t, args, &input)
		return BuildSketchArbitrationPrompt(input)
	case "planContextBlock":
		return PlanContextBlock(oneString())
	case "buildRootCausePrompt":
		var input struct {
			TaskText     string             `json:"taskText"`
			Cycle        jscompat.JSNumber  `json:"cycle"`
			Blockers     []RootCauseBlocker `json:"blockers"`
			DiffStat     string             `json:"diffStat"`
			ContractTail string             `json:"contractTail"`
		}
		mustOne(t, args, &input)
		return BuildRootCausePrompt(input)
	case "rootCauseRepairHint":
		return RootCauseRepairHint(oneString())
	default:
		t.Fatalf("unknown fn %q", fx.Fn)
		return nil
	}
}

func mustOne(t *testing.T, args []json.RawMessage, out any) {
	t.Helper()
	if len(args) != 1 {
		t.Fatalf("want one arg, got %d", len(args))
	}
	mustDecode(t, args[0], out)
}

func mustDecode(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode arg: %v", err)
	}
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 60 {
		t.Fatalf("expected at least 60 fixtures, got %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fx := range fixtures {
		fx := fx
		seen[fx.Fn]++
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			got, err := jscompat.Stringify(callFixture(t, fx))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Fatalf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
	for _, fn := range []string{
		"ledgerScenario", "buildGlossaryPrompt", "parseGlossary",
		"enforceableGlossaryIdentifiers", "glossaryContextBlock",
		"buildContractReviewPrompt", "parseContractReview",
		"contractReviewEvidenceBlock", "buildSketchPrompt", "parseSketch",
		"sketchJaccard", "sketchesDisagree", "buildSketchArbitrationPrompt",
		"planContextBlock", "buildRootCausePrompt", "rootCauseRepairHint",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixture coverage for %s", fn)
		}
	}
}

func TestUTF16CapBoundary(t *testing.T) {
	got := capString(strings.Repeat("a", 3999)+"🚀z", 4000)
	if got != strings.Repeat("a", 3999)+string(appendWTF8(nil, 0xd83d))+"\n…[truncated]" {
		t.Fatalf("cap did not split at the JS UTF-16 boundary")
	}
}
