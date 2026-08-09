package leafoutcome

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Golden-fixture replay against testdata/fixtures.json, produced from the real
// src/session/leaf-outcome.ts by tools/fixtures/gen-leafoutcome.ts. The gate is
// byte-for-byte equality between jscompat.Stringify(goResult) and the TS
// JSON.stringify output — not a tolerance compare — so any drift in the record
// key order, in V8's number formatting, in the /gi command-mention regex, in
// the taskID sanitizer, or in the lossy UTF-8 decode of `git diff` output fails
// loudly.
//
// The two impure exports are replayed as SCENARIOS: the Go runner performs the
// identical temp-workspace setup, op list and result capture as the TS runner
// in the generator, so out_json compares the resulting FILE BYTES.
//
// GIT DEPENDENCE: the preserveScenario fixtures embed real `git diff` text,
// including the abbreviated `index <blob>..<blob>` hashes. Those are pure
// content hashes, and the env pins below (identity, date, no global/system
// config) remove every other source of variation, but the diff FORMAT is still
// whatever the local git emits. Regenerate the fixtures if the toolchain's git
// ever changes its default diff rendering.

const fixtureNow int64 = 1_700_000_000_123

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

func (n *fixtureNum) js() jscompat.JSNumber { return jscompat.JSNumber(*n) }

// ── argument mirrors ─────────────────────────────────────────────────────
//
// The production structs use jscompat.JSNumber, whose UnmarshalJSON cannot see
// the "@@num:" tokens. These mirrors decode the fixture encoding and convert.

type evidenceArg struct {
	AuditCommandsRun fixtureNum `json:"auditCommandsRun"`
	AuditBlockers    fixtureNum `json:"auditBlockers"`
	InRunTestsPassed bool       `json:"inRunTestsPassed"`
}

func (e *evidenceArg) to() *LeafOutcomeEvidence {
	if e == nil {
		return nil
	}
	return &LeafOutcomeEvidence{
		AuditCommandsRun: e.AuditCommandsRun.js(),
		AuditBlockers:    e.AuditBlockers.js(),
		InRunTestsPassed: e.InRunTestsPassed,
	}
}

type classifyArg struct {
	GateStatus    GateStatus `json:"gateStatus"`
	RepairRounds  fixtureNum `json:"repairRounds"`
	MergeConflict bool       `json:"mergeConflict"`
}

func (a classifyArg) to() ClassifyVerdictInput {
	return ClassifyVerdictInput{
		GateStatus:    a.GateStatus,
		RepairRounds:  a.RepairRounds.js(),
		MergeConflict: a.MergeConflict,
	}
}

type partStateArg struct {
	Status *string `json:"status"`
}

type partArg struct {
	Type  *string       `json:"type"`
	State *partStateArg `json:"state"`
}

type infoArg struct {
	Role *string     `json:"role"`
	Cost *fixtureNum `json:"cost"`
}

type messageArg struct {
	Info  *infoArg   `json:"info"`
	Parts []*partArg `json:"parts"`
}

func toMessages(in []*messageArg) []*SessionMessage {
	out := make([]*SessionMessage, len(in))
	for i, m := range in {
		if m == nil {
			continue
		}
		msg := &SessionMessage{}
		if m.Info != nil {
			msg.Info = &SessionMessageInfo{Role: m.Info.Role}
			if m.Info.Cost != nil {
				c := m.Info.Cost.js()
				msg.Info.Cost = &c
			}
		}
		if m.Parts != nil {
			msg.Parts = make([]*SessionMessagePart, len(m.Parts))
			for j, p := range m.Parts {
				if p == nil {
					continue
				}
				part := &SessionMessagePart{Type: p.Type}
				if p.State != nil {
					part.State = &SessionMessagePartState{Status: p.State.Status}
				}
				msg.Parts[j] = part
			}
		}
		out[i] = msg
	}
	return out
}

type buildArg struct {
	TaskID          string       `json:"taskID"`
	ProviderID      string       `json:"providerID"`
	ModelID         string       `json:"modelID"`
	Tags            []string     `json:"tags"`
	Description     *string      `json:"description"`
	DependencyFanIn *fixtureNum  `json:"dependencyFanIn"`
	GateStatus      GateStatus   `json:"gateStatus"`
	RepairRounds    fixtureNum   `json:"repairRounds"`
	Turns           fixtureNum   `json:"turns"`
	ToolErrors      fixtureNum   `json:"toolErrors"`
	CostUsd         fixtureNum   `json:"costUsd"`
	WallMs          fixtureNum   `json:"wallMs"`
	MergeConflict   bool         `json:"mergeConflict"`
	Evidence        *evidenceArg `json:"evidence"`
	Now             *fixtureNum  `json:"now"`
}

func (a buildArg) to() BuildLeafOutcomeInput {
	in := BuildLeafOutcomeInput{
		TaskID:        a.TaskID,
		ProviderID:    a.ProviderID,
		ModelID:       a.ModelID,
		Tags:          a.Tags,
		Description:   a.Description,
		GateStatus:    a.GateStatus,
		RepairRounds:  a.RepairRounds.js(),
		Turns:         a.Turns.js(),
		ToolErrors:    a.ToolErrors.js(),
		CostUsd:       a.CostUsd.js(),
		WallMs:        a.WallMs.js(),
		MergeConflict: a.MergeConflict,
		Evidence:      a.Evidence.to(),
	}
	if a.DependencyFanIn != nil {
		f := float64(*a.DependencyFanIn)
		in.DependencyFanIn = &f
	}
	if a.Now != nil {
		n := a.Now.js()
		in.Now = &n
	}
	return in
}

// ── scenario specs (mirror the generator's runners exactly) ──────────────

type emitOp struct {
	Build buildArg `json:"build"`
	Ctx   string   `json:"ctx"`
}

type emitSpec struct {
	Seed      *string  `json:"seed"`
	BlockDir  bool     `json:"blockDir"`
	BlockFile bool     `json:"blockFile"`
	WsSuffix  string   `json:"wsSuffix"`
	Ops       []emitOp `json:"ops"`
}

type emitResult struct {
	Contexts []string `json:"contexts"`
	File     *string  `json:"file"`
}

type repoOp struct {
	Op      string   `json:"op"`
	File    string   `json:"file"`
	Content string   `json:"content"`
	B64     string   `json:"b64"`
	Args    []string `json:"args"`
}

type preserveSpec struct {
	// Flag is RawMessage so an absent key ("leave the env alone") stays
	// distinguishable from an explicit null ("delete the var").
	Flag     json.RawMessage `json:"flag"`
	Repo     []repoOp        `json:"repo"`
	TaskID   string          `json:"taskID"`
	BaseSha  *string         `json:"baseSha"`
	Worktree string          `json:"worktree"`
	BlockDir bool            `json:"blockDir"`
}

type preserveResult struct {
	// A nil slice marshals as `null`, matching the TS `entries = null` branch.
	Entries [][]any `json:"entries"`
}

// ── helpers ──────────────────────────────────────────────────────────────

func readOrNull(p string) *string {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

func mkws(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "leafoutcome-fixture-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func gitRun(dir string, args []string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	_ = cmd.Run()
}

func runEmitScenario(t *testing.T, spec emitSpec) emitResult {
	t.Helper()
	root := mkws(t)
	ws := root
	if spec.WsSuffix != "" {
		ws = filepath.Join(root, spec.WsSuffix)
	}
	if spec.Seed != nil {
		if err := os.MkdirAll(filepath.Join(ws, ".codeaf"), 0o777); err != nil {
			t.Fatalf("seed mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ws, ".codeaf", "outcomes.jsonl"), []byte(*spec.Seed), 0o666); err != nil {
			t.Fatalf("seed write: %v", err)
		}
	}
	if spec.BlockDir {
		if err := os.MkdirAll(ws, 0o777); err != nil {
			t.Fatalf("blockDir mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(ws, ".codeaf"), []byte("not a directory"), 0o666); err != nil {
			t.Fatalf("blockDir write: %v", err)
		}
	}
	if spec.BlockFile {
		if err := os.MkdirAll(filepath.Join(ws, ".codeaf", "outcomes.jsonl"), 0o777); err != nil {
			t.Fatalf("blockFile mkdir: %v", err)
		}
	}

	contexts := []string{}
	for _, op := range spec.Ops {
		outcome := BuildLeafOutcome(op.Build.to())
		mode := op.Ctx
		if mode == "" {
			mode = "none"
		}
		var writeContext func(string) error
		if mode != "none" {
			writeContext = func(body string) error {
				contexts = append(contexts, body)
				if mode == "throw" {
					return fmt.Errorf("context sink is down")
				}
				return nil
			}
		}
		EmitLeafOutcome(EmitLeafOutcomeArgs{Workspace: ws, Outcome: outcome, WriteContext: writeContext})
	}
	return emitResult{Contexts: contexts, File: readOrNull(filepath.Join(ws, ".codeaf", "outcomes.jsonl"))}
}

func runPreserveScenario(t *testing.T, spec preserveSpec) preserveResult {
	t.Helper()

	if len(spec.Flag) > 0 {
		prev, had := os.LookupEnv("CODEAF_ADAPTIVE_CUTS")
		if string(spec.Flag) == "null" {
			_ = os.Unsetenv("CODEAF_ADAPTIVE_CUTS")
		} else {
			var v string
			if err := json.Unmarshal(spec.Flag, &v); err != nil {
				t.Fatalf("decode flag: %v", err)
			}
			_ = os.Setenv("CODEAF_ADAPTIVE_CUTS", v)
		}
		defer func() {
			if had {
				_ = os.Setenv("CODEAF_ADAPTIVE_CUTS", prev)
			} else {
				_ = os.Unsetenv("CODEAF_ADAPTIVE_CUTS")
			}
		}()
	}

	ws := mkws(t)
	wt := filepath.Join(ws, "wt")
	if err := os.MkdirAll(wt, 0o777); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	for _, op := range spec.Repo {
		switch op.Op {
		case "init":
			gitRun(wt, []string{"init", "-q", "-b", "main"})
		case "write":
			target := filepath.Join(wt, op.File)
			if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
				t.Fatalf("repo mkdir: %v", err)
			}
			if err := os.WriteFile(target, []byte(op.Content), 0o666); err != nil {
				t.Fatalf("repo write: %v", err)
			}
		case "writeBytes":
			raw, err := base64.StdEncoding.DecodeString(op.B64)
			if err != nil {
				t.Fatalf("repo b64: %v", err)
			}
			target := filepath.Join(wt, op.File)
			if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
				t.Fatalf("repo mkdir: %v", err)
			}
			if err := os.WriteFile(target, raw, 0o666); err != nil {
				t.Fatalf("repo writeBytes: %v", err)
			}
		case "git":
			gitRun(wt, op.Args)
		default:
			t.Fatalf("unknown repo op %q", op.Op)
		}
	}

	worktree := wt
	switch spec.Worktree {
	case "":
	case "missing":
		worktree = filepath.Join(ws, "does-not-exist")
	case "file":
		if err := os.WriteFile(filepath.Join(ws, "afile"), []byte("x"), 0o666); err != nil {
			t.Fatalf("worktree file: %v", err)
		}
		worktree = filepath.Join(ws, "afile")
	case "root":
		worktree = ws
	default:
		t.Fatalf("unknown worktree override %q", spec.Worktree)
	}

	if spec.BlockDir {
		if err := os.WriteFile(filepath.Join(ws, ".codeaf"), []byte("not a directory"), 0o666); err != nil {
			t.Fatalf("blockDir write: %v", err)
		}
	}

	baseSha := spec.BaseSha
	if baseSha != nil {
		s := *baseSha
		for _, sub := range [][2]string{{"@@HEAD@@", "HEAD"}, {"@@HEAD~1@@", "HEAD~1"}} {
			if strings.Contains(s, sub[0]) {
				cmd := exec.Command("git", "rev-parse", sub[1])
				cmd.Dir = wt
				out, _ := cmd.Output()
				s = strings.ReplaceAll(s, sub[0], jscompat.Trim(string(out)))
			}
		}
		baseSha = &s
	}

	PreserveRejectedWork(PreserveRejectedWorkArgs{
		Workspace: ws,
		Worktree:  worktree,
		TaskID:    spec.TaskID,
		BaseSha:   baseSha,
	})

	dir := filepath.Join(ws, ".codeaf", "rejected-work")
	names, err := os.ReadDir(dir)
	if err != nil {
		return preserveResult{Entries: nil}
	}
	list := make([]string, 0, len(names))
	for _, e := range names {
		list = append(list, e.Name())
	}
	// JS Array.prototype.sort's default comparator orders by UTF-16 code unit.
	// Every name here is pure ASCII by construction (the sanitizer replaces
	// every byte outside [a-zA-Z0-9._-]), where byte order and UTF-16 order
	// coincide.
	sort.Strings(list)
	entries := make([][]any, 0, len(list))
	for _, name := range list {
		var content any
		if c := readOrNull(filepath.Join(dir, name)); c != nil {
			content = *c
		}
		entries = append(entries, []any{name, content})
	}
	return preserveResult{Entries: entries}
}

// ── the replay ───────────────────────────────────────────────────────────

func decodeArgs(t *testing.T, fc fixtureCase, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(fc.ArgsJSON), v); err != nil {
		t.Fatalf("decode args %s: %v", fc.ArgsJSON, err)
	}
}

func mustArity(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("expected %d args, got %d", want, got)
	}
}

func stringify(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

func TestFixtures(t *testing.T) {
	restore := SetClockForTesting(func() int64 { return fixtureNow })
	defer restore()

	// Mirrors the generator's git pinning so `git diff` renders identically.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "Fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "fixture@example.invalid")
	t.Setenv("GIT_AUTHOR_DATE", "1700000000 +0000")
	t.Setenv("GIT_COMMITTER_NAME", "Fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "fixture@example.invalid")
	t.Setenv("GIT_COMMITTER_DATE", "1700000000 +0000")

	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<26)
	seen := map[string]int{}
	cases := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fc fixtureCase
		if err := json.Unmarshal([]byte(line), &fc); err != nil {
			t.Fatalf("decode fixture line %d: %v", cases+1, err)
		}
		cases++
		seen[fc.Fn]++
		t.Run(fc.Name, func(t *testing.T) { runFixture(t, fc) })
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	if cases < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", cases)
	}
	for _, fn := range []string{
		"sizeBandFromTags",
		"classifyVerdict",
		"aggregateSessionStats",
		"buildLeafOutcome",
		"auditEvidenceFromVerdict",
		"emitScenario",
		"preserveScenario",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixture coverage for %q", fn)
		}
	}
	t.Logf("replayed %d fixture cases", cases)
}

func runFixture(t *testing.T, fc fixtureCase) {
	t.Helper()
	var result any

	switch fc.Fn {
	case "sizeBandFromTags":
		var args [][]string
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = SizeBandFromTags(args[0])

	case "classifyVerdict":
		var args []classifyArg
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = ClassifyVerdict(args[0].to())

	case "aggregateSessionStats":
		var args [][]*messageArg
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = AggregateSessionStats(toMessages(args[0]))

	case "buildLeafOutcome":
		var args []buildArg
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = BuildLeafOutcome(args[0].to())

	case "auditEvidenceFromVerdict":
		var args []json.RawMessage
		decodeArgs(t, fc, &args)
		if len(args) < 1 || len(args) > 2 {
			t.Fatalf("expected 1 or 2 args, got %d", len(args))
		}
		var verdict any
		if err := json.Unmarshal(args[0], &verdict); err != nil {
			t.Fatalf("decode verdict: %v", err)
		}
		inRunTestsPassed := false // the TS default parameter
		if len(args) == 2 {
			if err := json.Unmarshal(args[1], &inRunTestsPassed); err != nil {
				t.Fatalf("decode inRunTestsPassed: %v", err)
			}
		}
		result = AuditEvidenceFromVerdict(verdict, inRunTestsPassed)

	case "emitScenario":
		var args []emitSpec
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = runEmitScenario(t, args[0])

	case "preserveScenario":
		var args []preserveSpec
		decodeArgs(t, fc, &args)
		mustArity(t, len(args), 1)
		result = runPreserveScenario(t, args[0])

	default:
		t.Fatalf("unknown fn %q", fc.Fn)
	}

	got := stringify(t, result)
	if got != fc.OutJSON {
		t.Fatalf("args=%s\n got: %s\nwant: %s", fc.ArgsJSON, got, fc.OutJSON)
	}
}
