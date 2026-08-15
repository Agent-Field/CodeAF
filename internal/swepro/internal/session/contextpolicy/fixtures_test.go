package contextpolicy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

// Fixture replay: every line of testdata/fixtures.json was produced by the real
// src/session/context-policy.ts under bun (see tools/fixtures/gen-contextpolicy.ts).
// The gate is byte-for-byte equality between jscompat.Stringify(goResult) and
// the JSON.stringify output V8 recorded.

type fixtureCase struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

// fixtureNum decodes the generator's number encoding: plain JSON numbers, plus
// the "@@num:" tokens that stand in for the four values JSON cannot carry.
type fixtureNum float64

func (n *fixtureNum) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) > 0 && s[0] == '"' {
		var tok string
		if err := json.Unmarshal(b, &tok); err != nil {
			return err
		}
		switch tok {
		case "@@num:NaN":
			*n = fixtureNum(math.NaN())
		case "@@num:Infinity":
			*n = fixtureNum(math.Inf(1))
		case "@@num:-Infinity":
			*n = fixtureNum(math.Inf(-1))
		case "@@num:-0":
			*n = fixtureNum(math.Copysign(0, -1))
		default:
			return fmt.Errorf("unknown number token %q", tok)
		}
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*n = fixtureNum(f)
	return nil
}

type fixtureAssessInput struct {
	Band         string     `json:"band"`
	Turns        fixtureNum `json:"turns"`
	ToolErrors   fixtureNum `json:"toolErrors"`
	RepairRounds fixtureNum `json:"repairRounds"`
}

func (a fixtureAssessInput) toInput() AssessContextInput {
	return AssessContextInput{
		Band:         sizeband.SizeBand(a.Band),
		Turns:        float64(a.Turns),
		ToolErrors:   float64(a.ToolErrors),
		RepairRounds: float64(a.RepairRounds),
	}
}

type fixtureAssessment struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

type fixtureSignal struct {
	PrevBlockers      *fixtureNum `json:"prevBlockers"`
	CurrBlockers      *fixtureNum `json:"currBlockers"`
	HardMode          *bool       `json:"hardMode"`
	ObjectiveProgress *bool       `json:"objectiveProgress"`
}

func (s fixtureSignal) toSignal() RetrySignal {
	out := RetrySignal{HardMode: s.HardMode, ObjectiveProgress: s.ObjectiveProgress}
	if s.PrevBlockers != nil {
		v := float64(*s.PrevBlockers)
		out.PrevBlockers = &v
	}
	if s.CurrBlockers != nil {
		v := float64(*s.CurrBlockers)
		out.CurrBlockers = &v
	}
	return out
}

type fixtureAttemptRecord struct {
	Attempt  fixtureNum `json:"attempt"`
	Approach string     `json:"approach"`
	Outcome  string     `json:"outcome"`
	Evidence *string    `json:"evidence"`
}

type fixtureOpenBlocker struct {
	BlockerID   string     `json:"blockerId"`
	Text        string     `json:"text"`
	CycleOpened fixtureNum `json:"cycleOpened"`
}

// fixtureBriefInput mirrors the TS inline parameter object. The nil-vs-empty
// distinction on OpenBlockers is load-bearing: TS uses `??`, so an explicitly
// empty array suppresses the workspace read while an absent key does not.
// encoding/json leaves an absent key as a nil slice and materializes `[]` as a
// non-nil empty slice, which is exactly the distinction needed.
type fixtureBriefInput struct {
	TaskDescription     string                 `json:"taskDescription"`
	FailureSignals      []string               `json:"failureSignals"`
	AttemptedApproaches []string               `json:"attemptedApproaches"`
	Ledger              []fixtureAttemptRecord `json:"ledger"`
	RejectedPatchPath   string                 `json:"rejectedPatchPath"`
	Workspace           string                 `json:"workspace"`
	OpenBlockers        []fixtureOpenBlocker   `json:"openBlockers"`
}

func (b fixtureBriefInput) toInput() BuildDistilledBriefInput {
	return BuildDistilledBriefInput{
		TaskDescription:     b.TaskDescription,
		FailureSignals:      b.FailureSignals,
		AttemptedApproaches: b.AttemptedApproaches,
		Ledger:              toAttemptRecords(b.Ledger),
		RejectedPatchPath:   b.RejectedPatchPath,
		Workspace:           b.Workspace,
		OpenBlockers:        toOpenBlockers(b.OpenBlockers),
	}
}

func toAttemptRecords(in []fixtureAttemptRecord) []ledgers.AttemptRecord {
	if in == nil {
		return nil
	}
	out := make([]ledgers.AttemptRecord, 0, len(in))
	for _, r := range in {
		out = append(out, ledgers.AttemptRecord{
			Attempt:  float64(r.Attempt),
			Approach: r.Approach,
			Outcome:  r.Outcome,
			Evidence: r.Evidence,
		})
	}
	return out
}

func toOpenBlockers(in []fixtureOpenBlocker) []ledgers.OpenBlocker {
	if in == nil {
		return nil
	}
	out := make([]ledgers.OpenBlocker, 0, len(in))
	for _, b := range in {
		out = append(out, ledgers.OpenBlocker{
			BlockerID:   b.BlockerID,
			Text:        b.Text,
			CycleOpened: float64(b.CycleOpened),
		})
	}
	return out
}

// fixtureWorkspaceArg is the single argument of the synthetic
// "buildDistilledBrief#workspace" fn: files to materialize under a temp dir,
// plus the brief input whose `workspace` that dir becomes.
type fixtureWorkspaceArg struct {
	Files map[string]string `json:"files"`
	Input fixtureBriefInput `json:"input"`
}

func TestFixtures(t *testing.T) {
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	cases := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var fc fixtureCase
		if err := json.Unmarshal([]byte(line), &fc); err != nil {
			t.Fatalf("decode fixture line %d: %v", cases+1, err)
		}
		cases++
		t.Run(fc.Name, func(t *testing.T) { runFixture(t, fc) })
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if cases < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}

func runFixture(t *testing.T, fc fixtureCase) {
	t.Helper()
	var result any

	switch fc.Fn {
	case "turnBudgetFor":
		var args []string
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = jscompat.JSNumber(TurnBudgetFor(sizeband.SizeBand(args[0])))

	case "assessContext":
		var args []fixtureAssessInput
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = AssessContext(args[0].toInput())

	case "retryMode":
		var raw []json.RawMessage
		decodeArgs(t, fc, &raw)
		if len(raw) != 2 && len(raw) != 3 {
			t.Fatalf("arity: got %d args, want 2 or 3", len(raw))
		}
		var a fixtureAssessment
		if err := json.Unmarshal(raw[0], &a); err != nil {
			t.Fatalf("decode assessment: %v", err)
		}
		var attempt fixtureNum
		if err := json.Unmarshal(raw[1], &attempt); err != nil {
			t.Fatalf("decode attempt: %v", err)
		}
		assessment := ContextAssessment{Verdict: Verdict(a.Verdict), Reason: a.Reason}
		if len(raw) == 2 {
			// The two-arg TS call: the default parameter path.
			result = RetryMode(assessment, float64(attempt))
		} else {
			var s fixtureSignal
			if err := json.Unmarshal(raw[2], &s); err != nil {
				t.Fatalf("decode signal: %v", err)
			}
			result = RetryMode(assessment, float64(attempt), s.toSignal())
		}

	case "buildDistilledBrief":
		var args []fixtureBriefInput
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = BuildDistilledBrief(args[0].toInput())

	case "buildDistilledBrief#workspace":
		var args []fixtureWorkspaceArg
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		ws := t.TempDir()
		for rel, content := range args[0].Files {
			target := filepath.Join(ws, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", target, err)
			}
			if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", target, err)
			}
		}
		in := args[0].Input.toInput()
		in.Workspace = ws
		result = BuildDistilledBrief(in)

	default:
		t.Fatalf("unknown fn %q", fc.Fn)
	}

	got, err := jscompat.Stringify(result)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	if unescapeLineSeparators(string(got)) != fc.OutJSON {
		t.Fatalf("parity mismatch\n got: %s\nwant: %s", got, fc.OutJSON)
	}
}

// unescapeLineSeparators works around the one divergence jscompat/json.go
// documents in its own header: Go's encoding/json always escapes U+2028 and
// U+2029 inside strings, while V8 emits them raw (both are legal raw JSON).
// It is an encoder-level artifact of the shared toolkit, not of this module —
// context-policy only ever copies caller prose through — and it cannot mask a
// contextpolicy bug, because the ONE place this module inspects string content
// (ASSESSMENT_COUNTER_RX, where U+2028/U+2029 are members of the JS `\s` class)
// is covered by the "counter regex js whitespace class" case, whose output
// contains no separators at all. Only
// "buildDistilledBrief/adversarial line separators in prose" needs the rewrite;
// every other case is compared byte-for-byte with no rewriting at all.
func unescapeLineSeparators(s string) string {
	const prefix = `\u202`
	if !strings.Contains(s, prefix) {
		return s
	}
	s = strings.ReplaceAll(s, prefix+`8`, string(rune(0x2028)))
	return strings.ReplaceAll(s, prefix+`9`, string(rune(0x2029)))
}

func decodeArgs(t *testing.T, fc fixtureCase, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(fc.ArgsJSON), into); err != nil {
		t.Fatalf("decode args for %s: %v (%s)", fc.Name, err, fc.ArgsJSON)
	}
}

func mustArity(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("arity: got %d args, want %d", got, want)
	}
}

// TestTurnBudgetForOffUnionBandDivergence pins the ONE observable divergence
// from the TS module, which is why it has no fixture: TS `TURN_BUDGET[band]`
// yields `undefined` for an off-union band, and JSON.stringify(undefined) is
// not a JSON document, so the fixture record shape cannot carry it. Go returns
// NaN instead — the value that behaves identically under every comparison and
// every arithmetic operation this module performs, which is why assessContext
// stays byte-identical (see the "assessContext/off-union band" fixture).
func TestTurnBudgetForOffUnionBandDivergence(t *testing.T) {
	for _, band := range []sizeband.SizeBand{"zz", "", "XS", "xs "} {
		if got := TurnBudgetFor(band); !math.IsNaN(got) {
			t.Fatalf("TurnBudgetFor(%q) = %v, want NaN (TS: undefined)", band, got)
		}
	}
}
