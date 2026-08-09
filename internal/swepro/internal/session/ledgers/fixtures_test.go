package ledgers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/leafoutcome"
)

// Fixture replay: every line of testdata/fixtures.json was produced by the real
// five-module src/session/*-ledger.ts family under bun (see
// tools/fixtures/gen-ledgers.ts). The gate is byte-for-byte equality between
// jscompat.Stringify(goResult) and the JSON.stringify output V8 recorded —
// with NO normalization of any kind, including U+2028/U+2029 and unpaired
// surrogates, which the package's own serializer emits V8-identically.
//
// Both modules are impure, so a "result" is the scenario transcript: the value
// each op returned plus the ledger file's final bytes. The runner below mirrors
// the generator's scenario runners op for op.

const fixtureNow = 1_700_000_000_123

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

// jsStr marshals through the package's V8-compatible string quoter, so the
// scenario wrapper never re-escapes what the ledgers produced (Go's
// encoding/json would rewrite U+2028/U+2029).
type jsStr string

func (s jsStr) MarshalJSON() ([]byte, error) { return appendJSQuoted(nil, string(s)), nil }

type seedSpec struct {
	Line  string `json:"line"`
	Count int    `json:"count"`
}

type scenarioResult struct {
	Ops  []any  `json:"ops"`
	File *jsStr `json:"file"`
}

// ── attempt-ledger scenarios ─────────────────────────────────────────────

type attemptOpRecord struct {
	Approach string  `json:"approach"`
	Outcome  string  `json:"outcome"`
	Evidence *string `json:"evidence"`
}

type attemptOp struct {
	Op      string           `json:"op"`
	TaskKey string           `json:"taskKey"`
	Record  *attemptOpRecord `json:"record"`
}

type attemptSpec struct {
	WS        *string     `json:"ws"`
	Seed      *string     `json:"seed"`
	SeedLines *seedSpec   `json:"seedLines"`
	Ops       []attemptOp `json:"ops"`
}

// ── blocker-ledger scenarios ─────────────────────────────────────────────

type blockerOpRecord struct {
	BlockerID   string      `json:"blockerId"`
	Status      string      `json:"status"`
	CycleOpened fixtureNum  `json:"cycleOpened"`
	Text        string      `json:"text"`
	CycleClosed *fixtureNum `json:"cycleClosed"`
}

type blockerOp struct {
	Op     string           `json:"op"`
	Record *blockerOpRecord `json:"record"`
	Cycle  fixtureNum       `json:"cycle"`
	Texts  []string         `json:"texts"`
	Text   string           `json:"text"`
}

type blockerSpec struct {
	WS        *string     `json:"ws"`
	Seed      *string     `json:"seed"`
	SeedLines *seedSpec   `json:"seedLines"`
	Ops       []blockerOp `json:"ops"`
}

// ── decision-ledger fixtures ─────────────────────────────────────────────

type fixtureDecisionRecord struct {
	Ts           fixtureNum `json:"ts"`
	Kind         string     `json:"kind"`
	TaskID       string     `json:"taskID"`
	Model        string     `json:"model"`
	Band         string     `json:"band"`
	ReliableBand string     `json:"reliableBand"`
	Chosen       string     `json:"chosen"`
	Reason       string     `json:"reason"`
	KnobsHash    string     `json:"knobsHash"`
}

func (r fixtureDecisionRecord) production() DecisionRecord {
	return DecisionRecord{
		Ts:           float64(r.Ts),
		Kind:         DecisionKind(r.Kind),
		TaskID:       r.TaskID,
		Model:        r.Model,
		Band:         r.Band,
		ReliableBand: r.ReliableBand,
		Chosen:       r.Chosen,
		Reason:       r.Reason,
		KnobsHash:    r.KnobsHash,
	}
}

type buildDecisionFixtureInput struct {
	Kind         string      `json:"kind"`
	TaskID       string      `json:"taskID"`
	Model        string      `json:"model"`
	Band         string      `json:"band"`
	ReliableBand string      `json:"reliableBand"`
	Chosen       string      `json:"chosen"`
	Reason       string      `json:"reason"`
	KnobsHash    string      `json:"knobsHash"`
	Now          *fixtureNum `json:"now"`
}

func (in buildDecisionFixtureInput) production() BuildDecisionInput {
	out := BuildDecisionInput{
		Kind:         DecisionKind(in.Kind),
		TaskID:       in.TaskID,
		Model:        in.Model,
		Band:         in.Band,
		ReliableBand: in.ReliableBand,
		Chosen:       in.Chosen,
		Reason:       in.Reason,
		KnobsHash:    in.KnobsHash,
	}
	if in.Now != nil {
		n := jscompat.JSNumber(*in.Now)
		out.Now = &n
	}
	return out
}

type decisionOp struct {
	Op       string                 `json:"op"`
	Decision *fixtureDecisionRecord `json:"decision"`
	Text     string                 `json:"text"`
}

type decisionSpec struct {
	WS            *string      `json:"ws"`
	WorkspaceFile bool         `json:"workspaceFile"`
	Seed          *string      `json:"seed"`
	SeedLines     *seedSpec    `json:"seedLines"`
	Ops           []decisionOp `json:"ops"`
}

type fixtureOutcome struct {
	TaskID       string     `json:"taskID"`
	Verdict      string     `json:"verdict"`
	CostUsd      fixtureNum `json:"costUsd"`
	WallMs       fixtureNum `json:"wallMs"`
	RepairRounds fixtureNum `json:"repairRounds"`
}

func (o fixtureOutcome) production() *leafoutcome.LeafOutcome {
	return &leafoutcome.LeafOutcome{
		TaskID:       o.TaskID,
		Verdict:      leafoutcome.LeafVerdict(o.Verdict),
		CostUsd:      jscompat.JSNumber(o.CostUsd),
		WallMs:       jscompat.JSNumber(o.WallMs),
		RepairRounds: jscompat.JSNumber(o.RepairRounds),
	}
}

// ── cycle-ledger fixtures ────────────────────────────────────────────────

type fixtureCycleInput struct {
	Kind           string      `json:"kind"`
	Cycle          fixtureNum  `json:"cycle"`
	BlockersBefore fixtureNum  `json:"blockersBefore"`
	Verdict        string      `json:"verdict"`
	BlockersAfter  *fixtureNum `json:"blockersAfter"`
	CostUsdApprox  *fixtureNum `json:"costUsdApprox"`
	WallMs         *fixtureNum `json:"wallMs"`
}

func (in fixtureCycleInput) production() CycleInput {
	out := CycleInput{
		Kind:           CycleKind(in.Kind),
		Cycle:          float64(in.Cycle),
		BlockersBefore: float64(in.BlockersBefore),
		Verdict:        in.Verdict,
	}
	if in.BlockersAfter != nil {
		n := float64(*in.BlockersAfter)
		out.BlockersAfter = &n
	}
	if in.CostUsdApprox != nil {
		n := float64(*in.CostUsdApprox)
		out.CostUsdApprox = &n
	}
	if in.WallMs != nil {
		n := float64(*in.WallMs)
		out.WallMs = &n
	}
	return out
}

type cycleOp struct {
	Op     string             `json:"op"`
	Record *fixtureCycleInput `json:"record"`
	Text   string             `json:"text"`
}

type cycleSpec struct {
	WS            *string   `json:"ws"`
	WorkspaceFile bool      `json:"workspaceFile"`
	Seed          *string   `json:"seed"`
	SeedLines     *seedSpec `json:"seedLines"`
	Ops           []cycleOp `json:"ops"`
}

// ── run-failure-ledger fixtures ─────────────────────────────────────────

type fixtureFailureBug struct {
	Severity *string     `json:"severity"`
	File     *string     `json:"file"`
	Line     *fixtureNum `json:"line"`
	Detail   string      `json:"detail"`
}

func (b fixtureFailureBug) production() FailureBug {
	out := FailureBug{Severity: b.Severity, File: b.File, Detail: b.Detail}
	if b.Line != nil {
		n := jscompat.JSNumber(*b.Line)
		out.Line = &n
	}
	return out
}

type fixtureFailure struct {
	TaskID      string              `json:"taskID"`
	Title       string              `json:"title"`
	Reason      string              `json:"reason"`
	Bugs        []fixtureFailureBug `json:"bugs"`
	RepairHints []string            `json:"repairHints"`
	Confidence  *string             `json:"confidence"`
	Attempt     *fixtureNum         `json:"attempt"`
	Timestamp   fixtureNum          `json:"timestamp"`
}

func (f fixtureFailure) production() *LeafFailure {
	out := &LeafFailure{
		TaskID:      f.TaskID,
		Title:       f.Title,
		Reason:      f.Reason,
		Bugs:        make([]FailureBug, len(f.Bugs)),
		RepairHints: append([]string{}, f.RepairHints...),
		Confidence:  f.Confidence,
		Timestamp:   jscompat.JSNumber(f.Timestamp),
	}
	for i, bug := range f.Bugs {
		out.Bugs[i] = bug.production()
	}
	if f.Attempt != nil {
		n := jscompat.JSNumber(*f.Attempt)
		out.Attempt = &n
	}
	return out
}

type failureOp struct {
	Op      string          `json:"op"`
	Root    string          `json:"root"`
	Failure *fixtureFailure `json:"failure"`
	Limit   *fixtureNum     `json:"limit"`
	TaskID  string          `json:"taskID"`
	Index   int             `json:"index"`
	Field   string          `json:"field"`
	Value   string          `json:"value"`
}

type failureSpec struct {
	Ops []failureOp `json:"ops"`
}

type failureScenarioResult struct {
	Ops []any `json:"ops"`
}

func readOrNull(p string) *jsStr {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	s := jsStr(decodeUTF8Lossy(data))
	return &s
}

func seedWorkspace(t *testing.T, ws, rel string, seed *string, lines *seedSpec) {
	t.Helper()
	if seed == nil && lines == nil {
		return
	}
	target := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}
	body := ""
	if seed != nil {
		body = *seed
	}
	if lines != nil {
		body += strings.Repeat(lines.Line+"\n", lines.Count)
	}
	if err := os.WriteFile(target, []byte(body), 0o666); err != nil {
		t.Fatalf("seed write: %v", err)
	}
}

func runAttemptScenario(t *testing.T, spec attemptSpec) scenarioResult {
	t.Helper()
	ws := t.TempDir()
	if spec.WS != nil {
		ws = *spec.WS
	} else {
		seedWorkspace(t, ws, attemptLedgerFile, spec.Seed, spec.SeedLines)
	}
	ledger := filepath.Join(ws, attemptLedgerFile)
	results := []any{}
	for _, op := range spec.Ops {
		switch op.Op {
		case "append":
			AppendAttempt(ws, op.TaskKey, AttemptInput{
				Approach: op.Record.Approach,
				Outcome:  op.Record.Outcome,
				Evidence: op.Record.Evidence,
			})
			results = append(results, nil)
		case "read":
			results = append(results, ReadAttempts(ws, op.TaskKey))
		case "file":
			results = append(results, nilable(readOrNull(ledger)))
		case "entries":
			entries, err := os.ReadDir(filepath.Join(ws, ".codeaf"))
			if err != nil {
				results = append(results, nil)
				break
			}
			names := []string{}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".tmp-") {
					names = append(names, "<tmp>")
				} else {
					names = append(names, e.Name())
				}
			}
			sort.Strings(names)
			out := make([]jsStr, 0, len(names))
			for _, n := range names {
				out = append(out, jsStr(n))
			}
			results = append(results, out)
		default:
			t.Fatalf("unknown attempt op %q", op.Op)
		}
	}
	return scenarioResult{Ops: results, File: readOrNull(ledger)}
}

func runBlockerScenario(t *testing.T, spec blockerSpec) scenarioResult {
	t.Helper()
	ws := t.TempDir()
	if spec.WS != nil {
		ws = *spec.WS
	} else {
		seedWorkspace(t, ws, blockerLedgerFile, spec.Seed, spec.SeedLines)
	}
	ledger := filepath.Join(ws, blockerLedgerFile)
	results := []any{}
	for _, op := range spec.Ops {
		switch op.Op {
		case "append":
			in := BlockerInput{
				BlockerID:   op.Record.BlockerID,
				Status:      BlockerStatus(op.Record.Status),
				CycleOpened: float64(op.Record.CycleOpened),
				Text:        op.Record.Text,
			}
			if op.Record.CycleClosed != nil {
				c := float64(*op.Record.CycleClosed)
				in.CycleClosed = &c
			}
			AppendBlockerRecord(ws, in)
			results = append(results, nil)
		case "read":
			results = append(results, ReadBlockerRecords(ws))
		case "latest":
			entries := []any{}
			for _, e := range LatestByBlocker(ws).Entries() {
				entries = append(entries, []any{jsStr(e.Key), e.Val})
			}
			results = append(results, entries)
		case "open":
			results = append(results, LoadOpenBlockers(ws))
		case "reconcile":
			ReconcileVerdictBlockers(ws, float64(op.Cycle), op.Texts)
			results = append(results, nil)
		case "markFixed":
			MarkBlockersFixed(ws, float64(op.Cycle), op.Texts)
			results = append(results, nil)
		case "rawAppend":
			if err := os.MkdirAll(filepath.Dir(ledger), 0o777); err == nil {
				if f, err := os.OpenFile(ledger, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666); err == nil {
					_, _ = f.WriteString(op.Text)
					_ = f.Close()
				}
			}
			results = append(results, nil)
		case "file":
			results = append(results, nilable(readOrNull(ledger)))
		default:
			t.Fatalf("unknown blocker op %q", op.Op)
		}
	}
	return scenarioResult{Ops: results, File: readOrNull(ledger)}
}

func runDecisionScenario(t *testing.T, spec decisionSpec) scenarioResult {
	t.Helper()
	ws := t.TempDir()
	if spec.WS != nil {
		ws = *spec.WS
	} else if spec.WorkspaceFile {
		ws = filepath.Join(ws, "not-a-directory")
		if err := os.WriteFile(ws, []byte("x"), 0o666); err != nil {
			t.Fatalf("create file workspace: %v", err)
		}
	} else {
		seedWorkspace(t, ws, decisionLedgerFile, spec.Seed, spec.SeedLines)
	}
	ledger := filepath.Join(ws, decisionLedgerFile)
	results := []any{}
	for _, op := range spec.Ops {
		switch op.Op {
		case "append":
			AppendDecision(AppendDecisionArgs{Workspace: ws, Decision: op.Decision.production()})
			results = append(results, nil)
		case "load":
			results = append(results, LoadDecisions(ledger))
		case "rawAppend":
			if err := os.MkdirAll(filepath.Dir(ledger), 0o777); err == nil {
				if f, err := os.OpenFile(ledger, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666); err == nil {
					_, _ = f.WriteString(op.Text)
					_ = f.Close()
				}
			}
			results = append(results, nil)
		case "file":
			results = append(results, nilable(readOrNull(ledger)))
		default:
			t.Fatalf("unknown decision op %q", op.Op)
		}
	}
	return scenarioResult{Ops: results, File: readOrNull(ledger)}
}

func runCycleScenario(t *testing.T, spec cycleSpec) scenarioResult {
	t.Helper()
	ws := t.TempDir()
	if spec.WS != nil {
		ws = *spec.WS
	} else if spec.WorkspaceFile {
		ws = filepath.Join(ws, "not-a-directory")
		if err := os.WriteFile(ws, []byte("x"), 0o666); err != nil {
			t.Fatalf("create file workspace: %v", err)
		}
	} else {
		seedWorkspace(t, ws, cycleLedgerFile, spec.Seed, spec.SeedLines)
	}
	ledger := filepath.Join(ws, cycleLedgerFile)
	results := []any{}
	for _, op := range spec.Ops {
		switch op.Op {
		case "append":
			AppendCycleRecord(ws, op.Record.production())
			results = append(results, nil)
		case "read":
			results = append(results, ReadCycleRecords(ws))
		case "rawAppend":
			if err := os.MkdirAll(filepath.Dir(ledger), 0o777); err == nil {
				if f, err := os.OpenFile(ledger, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666); err == nil {
					_, _ = f.WriteString(op.Text)
					_ = f.Close()
				}
			}
			results = append(results, nil)
		case "file":
			results = append(results, nilable(readOrNull(ledger)))
		default:
			t.Fatalf("unknown cycle op %q", op.Op)
		}
	}
	return scenarioResult{Ops: results, File: readOrNull(ledger)}
}

func runFailureScenario(t *testing.T, spec failureSpec) failureScenarioResult {
	t.Helper()
	results := []any{}
	for _, op := range spec.Ops {
		switch op.Op {
		case "record":
			RecordLeafFailure(op.Root, op.Failure.production())
			results = append(results, nil)
		case "recent":
			if op.Limit == nil {
				results = append(results, GetRecentFailures(op.Root))
			} else {
				results = append(results, GetRecentFailures(op.Root, float64(*op.Limit)))
			}
		case "count":
			results = append(results, FailureCount(op.Root))
		case "digest":
			var digest *string
			if op.Limit == nil {
				digest = RenderFailureDigest(op.Root)
			} else {
				digest = RenderFailureDigest(op.Root, float64(*op.Limit))
			}
			if digest == nil {
				results = append(results, nil)
			} else {
				s := jsStr(*digest)
				results = append(results, &s)
			}
		case "mark":
			MarkFailureNudged(op.Root, op.TaskID)
			results = append(results, nil)
		case "was":
			results = append(results, WasFailureNudged(op.Root, op.TaskID))
		case "clear":
			ClearLedger(op.Root)
			results = append(results, nil)
		case "mutateRecent":
			recent := GetRecentFailures(op.Root, 100)
			if op.Index >= 0 && op.Index < len(recent) {
				switch op.Field {
				case "taskID":
					recent[op.Index].TaskID = op.Value
				case "title":
					recent[op.Index].Title = op.Value
				case "reason":
					recent[op.Index].Reason = op.Value
				default:
					t.Fatalf("unknown mutable failure field %q", op.Field)
				}
			}
			results = append(results, nil)
		default:
			t.Fatalf("unknown failure op %q", op.Op)
		}
	}
	return failureScenarioResult{Ops: results}
}

// nilable keeps a nil *jsStr from becoming a non-nil `any` holding a typed nil
// (which would still marshal to null, but this keeps the transcript honest).
func nilable(s *jsStr) any {
	if s == nil {
		return nil
	}
	return s
}

func TestFixtures(t *testing.T) {
	restore := SetClockForTesting(func() int64 { return fixtureNow })
	defer restore()

	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<26)
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
	if cases < 140 {
		t.Fatalf("expected at least 140 fixture cases, got %d", cases)
	}
	t.Logf("replayed %d fixture cases", cases)
}

func runFixture(t *testing.T, fc fixtureCase) {
	t.Helper()
	var result any

	switch fc.Fn {
	case "auditFixKey":
		var args []string
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = jsStr(AuditFixKey(args[0]))

	case "blockerId":
		var args []string
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = jsStr(BlockerID(args[0]))

	case "buildDecision":
		var args []buildDecisionFixtureInput
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = BuildDecision(args[0].production())

	case "scoreDecisions":
		var raw []json.RawMessage
		decodeArgs(t, fc, &raw)
		mustArity(t, len(raw), 2)
		var fixtureDecisions []fixtureDecisionRecord
		if err := json.Unmarshal(raw[0], &fixtureDecisions); err != nil {
			t.Fatalf("decode score decisions: %v", err)
		}
		var fixtureOutcomes []fixtureOutcome
		if err := json.Unmarshal(raw[1], &fixtureOutcomes); err != nil {
			t.Fatalf("decode score outcomes: %v", err)
		}
		decisions := make([]DecisionRecord, len(fixtureDecisions))
		for i, decision := range fixtureDecisions {
			decisions[i] = decision.production()
		}
		outcomes := make([]*leafoutcome.LeafOutcome, len(fixtureOutcomes))
		for i, outcome := range fixtureOutcomes {
			outcomes[i] = outcome.production()
		}
		result = ScoreDecisions(decisions, outcomes)

	case "attemptScenario":
		var args []attemptSpec
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = runAttemptScenario(t, args[0])

	case "blockerScenario":
		var args []blockerSpec
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = runBlockerScenario(t, args[0])

	case "decisionScenario":
		var args []decisionSpec
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = runDecisionScenario(t, args[0])

	case "cycleScenario":
		var args []cycleSpec
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = runCycleScenario(t, args[0])

	case "failureScenario":
		var args []failureSpec
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = runFailureScenario(t, args[0])

	default:
		t.Fatalf("unknown fn %q", fc.Fn)
	}

	got, err := jscompat.Stringify(result)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	if string(got) != fc.OutJSON {
		t.Fatalf("parity mismatch\n got: %s\nwant: %s", got, fc.OutJSON)
	}
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
