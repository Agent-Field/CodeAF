package auditorgate

import (
	"bufio"
	"bytes"
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
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var row fixture
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func decodeArg(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		t.Fatalf("decode arg: %v\n%s", err, raw)
	}
}

func callFixture(t *testing.T, row fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(row.ArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	switch row.Fn {
	case "countSpecClauses":
		var spec string
		decodeArg(t, args[0], &spec)
		return CountSpecClauses(spec)
	case "uncoveredInventoryClauses":
		var coverage []ClauseCoverage
		var inventory []string
		decodeArg(t, args[0], &coverage)
		decodeArg(t, args[1], &inventory)
		return UncoveredInventoryClauses(coverage, inventory)
	case "uncoveredMatrixCells":
		var coverage []ClauseCoverage
		var matrix []string
		decodeArg(t, args[0], &coverage)
		decodeArg(t, args[1], &matrix)
		return UncoveredMatrixCells(coverage, matrix)
	case "isAdmissibleVerdict":
		var verdict AuditorVerdict
		decodeArg(t, args[0], &verdict)
		if len(args) == 1 {
			return IsAdmissibleVerdict(verdict)
		}
		var opts AdmissibilityOptions
		decodeArg(t, args[1], &opts)
		return IsAdmissibleVerdict(verdict, &opts)
	case "synthesizeScopeBlockers":
		var verdict AuditorVerdict
		var inventory, matrix []string
		decodeArg(t, args[0], &verdict)
		decodeArg(t, args[1], &inventory)
		decodeArg(t, args[2], &matrix)
		return SynthesizeScopeBlockers(verdict, inventory, matrix)
	case "convertInadmissiblePassToFail":
		var verdict AuditorVerdict
		var reason string
		decodeArg(t, args[0], &verdict)
		decodeArg(t, args[1], &reason)
		return ConvertInadmissiblePassToFail(verdict, reason)
	case "noVerdictRetryExhausted":
		var value float64
		decodeArg(t, args[0], &value)
		return NoVerdictRetryExhausted(value)
	case "commandExit", "commandName", "commandOutputTail":
		var command any
		decodeArg(t, args[0], &command)
		if row.Fn == "commandExit" {
			return CommandExit(command)
		}
		if row.Fn == "commandName" {
			return CommandName(command)
		}
		var limit float64
		decodeArg(t, args[1], &limit)
		return CommandOutputTail(command, limit)
	case "verifiedTestsPassed":
		var verdict AuditorVerdict
		decodeArg(t, args[0], &verdict)
		return VerifiedTestsPassed(verdict)
	case "buildAuditEvidenceSummary":
		if len(args) == 0 {
			return BuildAuditEvidenceSummary()
		}
		var verdict AuditorVerdict
		decodeArg(t, args[0], &verdict)
		return BuildAuditEvidenceSummary(&verdict)
	case "computeChangedSince":
		var input ChangedSinceArgs
		decodeArg(t, args[0], &input)
		return ComputeChangedSince(input)
	case "crossCheckExecutedCommands":
		var verdict AuditorVerdict
		var executed []string
		decodeArg(t, args[0], &verdict)
		decodeArg(t, args[1], &executed)
		return CrossCheckExecutedCommands(verdict, executed)
	case "extractExecutedBashCommands":
		var messages []any
		decodeArg(t, args[0], &messages)
		return ExtractExecutedBashCommands(messages)
	case "buildAuditCarryForwardBlock":
		var input AuditCarryForwardArgs
		decodeArg(t, args[0], &input)
		return BuildAuditCarryForwardBlock(input)
	case "buildInadmissibleRetryReminder":
		var inventory []string
		var verdict AuditorVerdict
		decodeArg(t, args[0], &inventory)
		decodeArg(t, args[1], &verdict)
		if len(args) == 2 {
			return BuildInadmissibleRetryReminder(inventory, verdict)
		}
		var reason *string
		decodeArg(t, args[2], &reason)
		return BuildInadmissibleRetryReminder(inventory, verdict, reason)
	case "resolveAuditMode":
		var input ResolveAuditModeArgs
		decodeArg(t, args[0], &input)
		return ResolveAuditMode(input)
	case "shouldAdjudicate":
		var input ShouldAdjudicateArgs
		decodeArg(t, args[0], &input)
		return ShouldAdjudicate(input)
	case "shouldDeltaScope":
		var input ShouldDeltaScopeArgs
		decodeArg(t, args[0], &input)
		return ShouldDeltaScope(input)
	case "contractEvidenceIndicatesPass":
		var evidence *string
		decodeArg(t, args[0], &evidence)
		return ContractEvidenceIndicatesPass(evidence)
	case "applyAdjudication":
		var input ApplyAdjudicationArgs
		decodeArg(t, args[0], &input)
		return ApplyAdjudication(input)
	case "buildAdjudicationEvidencePack":
		var input AdjudicationEvidencePackArgs
		decodeArg(t, args[0], &input)
		return BuildAdjudicationEvidencePack(input)
	case "topDiffHunks":
		var diff string
		var maxLines float64
		decodeArg(t, args[0], &diff)
		decodeArg(t, args[1], &maxLines)
		return TopDiffHunks(diff, maxLines)
	case "parseAdjudicatorVerdict":
		var messages []any
		decodeArg(t, args[0], &messages)
		return ParseAdjudicatorVerdict(messages)
	case "buildAuditPrompt":
		var input AuditPromptArgs
		decodeArg(t, args[0], &input)
		return BuildAuditPrompt(input)
	case "buildLightAuditPrompt":
		var input LightAuditPromptArgs
		decodeArg(t, args[0], &input)
		return BuildLightAuditPrompt(input)
	default:
		t.Fatalf("unknown fixture function %q", row.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	t.Setenv("CODEAF_ARTIFACT_REFS", "")
	fixtures := loadFixtures(t)
	if len(fixtures) < 100 {
		t.Fatalf("expected broad corpus, got %d", len(fixtures))
	}
	for _, row := range fixtures {
		t.Run(row.Name, func(t *testing.T) {
			got := callFixture(t, row)
			encoded, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != row.OutJSON {
				t.Errorf("%s(%s)\n got: %s\nwant: %s", row.Fn, row.ArgsJSON, encoded, row.OutJSON)
			}
		})
	}
}

func TestFixtureCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, row := range loadFixtures(t) {
		seen[row.Fn] = true
	}
	for _, name := range []string{
		"countSpecClauses", "uncoveredInventoryClauses", "uncoveredMatrixCells", "isAdmissibleVerdict",
		"synthesizeScopeBlockers", "convertInadmissiblePassToFail",
		"noVerdictRetryExhausted", "verifiedTestsPassed", "buildAuditEvidenceSummary",
		"computeChangedSince", "commandExit", "commandName", "commandOutputTail",
		"crossCheckExecutedCommands", "extractExecutedBashCommands",
		"buildAuditCarryForwardBlock", "buildInadmissibleRetryReminder",
		"resolveAuditMode", "shouldAdjudicate", "shouldDeltaScope",
		"contractEvidenceIndicatesPass", "applyAdjudication",
		"buildAdjudicationEvidencePack", "topDiffHunks", "parseAdjudicatorVerdict",
		"buildAuditPrompt", "buildLightAuditPrompt",
	} {
		if !seen[name] {
			t.Errorf("missing fixture coverage for %s", name)
		}
	}
}
