package fixgenerator

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
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
		t.Fatal(err)
	}
}

func callFixture(t *testing.T, row fixture) any {
	t.Helper()
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(row.ArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	switch row.Fn {
	case "buildAuditEvidenceSections":
		var verdict auditorgate.AuditorVerdict
		decodeArg(t, args[0], &verdict)
		if len(args) == 1 {
			return BuildAuditEvidenceSections(verdict)
		}
		var limit float64
		decodeArg(t, args[1], &limit)
		return BuildAuditEvidenceSections(verdict, limit)
	case "buildFixGenPrompt":
		var input PromptInput
		var outputPath, frozen string
		decodeArg(t, args[0], &input)
		decodeArg(t, args[1], &outputPath)
		decodeArg(t, args[2], &frozen)
		return BuildFixGenPrompt(input, outputPath, frozen)
	case "applyFixGeneratorDecision":
		var decision FixGeneratorDecision
		decodeArg(t, args[0], &decision)
		calls := [][]string{}
		runner := PlanDBRunnerFunc(func(args []string) (PlanDBResult, error) {
			calls = append(calls, append([]string{}, args...))
			return PlanDBResult{Stdout: []byte("{}")}, nil
		})
		result := ApplyFixGeneratorDecision(decision, runner)
		return struct {
			Result ApplyFixGenResult `json:"result"`
			Calls  [][]string        `json:"calls"`
		}{result, calls}
	case "fixGenFallback":
		return FixGenFallback
	default:
		t.Fatalf("unknown function %q", row.Fn)
		return nil
	}
}

func TestFixtureParity(t *testing.T) {
	t.Setenv("CODEAF_ARTIFACT_REFS", "")
	t.Setenv("CODEAF_HARD", "")
	t.Setenv("MAX_AUDIT_FIX_CYCLES", "")
	_ = os.Unsetenv("MAX_AUDIT_FIX_CYCLES")
	fixtures := loadFixtures(t)
	if len(fixtures) < 14 {
		t.Fatalf("fixture corpus too small: %d", len(fixtures))
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
	want := []string{
		"buildAuditEvidenceSections", "buildFixGenPrompt",
		"applyFixGeneratorDecision", "fixGenFallback",
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("missing fixtures for %s", name)
		}
	}
}

func TestCycleFilesAndEnvironment(t *testing.T) {
	workspace := t.TempDir()
	if got := ReadAuditCycles(workspace); got != 0 {
		t.Fatalf("missing cycles = %v", got)
	}
	if err := WriteAuditCycles(workspace, 2.5); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(workspace + "/.codeaf/audit-cycles.txt"); err != nil || string(raw) != "2.5" {
		t.Fatalf("cycle file raw=%q err=%v", raw, err)
	}
	if got := ReadAuditCycles(workspace); got != 2 {
		t.Fatalf("parseInt cycle = %v", got)
	}
	if err := os.WriteFile(workspace+"/.codeaf/audit-cycles.txt", []byte("-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadAuditCycles(workspace); got != 0 {
		t.Fatalf("negative cycle = %v", got)
	}

	t.Setenv("CODEAF_HARD", "")
	t.Setenv("MAX_AUDIT_FIX_CYCLES", "7garbage")
	if got := MaxAuditFixCycles(); got != 7 {
		t.Fatalf("parseInt env = %v", got)
	}
	t.Setenv("MAX_AUDIT_FIX_CYCLES", "bad")
	if got := MaxAuditFixCycles(); got != hardmodeDefaultForTest {
		t.Fatalf("invalid env fallback = %v", got)
	}
}

const hardmodeDefaultForTest = 2

func TestDecisionSchemaStrict(t *testing.T) {
	valid := json.RawMessage(`{"action":"dispatch_fixes","reason":"ok","fixes":[{"title":"x","kind":"code","deps":null,"description":"d"}],"summary":null}`)
	if parsed := (DecisionSchema{}).SafeParse(valid); !parsed.Success() {
		t.Fatalf("valid decision issues: %#v", parsed.Issues)
	}
	for name, raw := range map[string]string{
		"extra":        `{"action":"give_up","reason":"x","fixes":null,"summary":null,"extra":1}`,
		"missing":      `{"action":"give_up","reason":"x","fixes":null}`,
		"nested-extra": `{"action":"dispatch_fixes","reason":"x","fixes":[{"title":"x","kind":"code","deps":null,"description":"d","extra":1}],"summary":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			if parsed := (DecisionSchema{}).SafeParse(json.RawMessage(raw)); parsed.Success() {
				t.Fatal("invalid decision accepted")
			}
		})
	}
}

func TestReadFrozenLeavesKeepsMissingPlandbPrefixBug(t *testing.T) {
	var got []string
	runner := PlanDBRunnerFunc(func(args []string) (PlanDBResult, error) {
		got = append([]string{}, args...)
		return PlanDBResult{}, nil
	})
	if text := readFrozenLeaves(runner); text != "(no frozen leaves)" {
		t.Fatalf("frozen text = %q", text)
	}
	if !reflect.DeepEqual(got, []string{"contexts", "--kind", "frozen"}) {
		t.Fatalf("args = %#v", got)
	}
}
