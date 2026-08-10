package tia

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/importgraph"
)

// Golden-fixture replay against testdata/fixtures.json, produced from the real
// src/session/tia.ts by tools/fixtures/gen-tia.ts. The gate is byte-for-byte
// equality between jscompat.Stringify(goResult) and the TS JSON.stringify
// output — not a semantic compare — so `[]` vs `null`, field order and UTF-16
// sort order all fail loudly.
//
// Two things cannot cross a JSON boundary and are carried by name instead:
//   - the ImportGraph, whose three members are Maps (JSON.stringify(Map) is
//     "{}"), is encoded as [key, value][] entry lists;
//   - the testFilePattern RegExp, which is named by key into patternTwins
//     below — each entry hand-translated from the JS regex in gen-tia.ts's
//     PATTERNS, because RE2 has no sticky flag and disagrees with JS about
//     both `\s` and `.`.
//
// computeImpactedTestsForWorkspace fixtures carry an ordered op list that is
// replayed into a t.TempDir(); ops are applied in the recorded order so the
// directory hands back readdir entries in the same order the generator saw.

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

	var out []fixture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fx fixture
		if err := json.Unmarshal([]byte(line), &fx); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

// ── RegExp twins ────────────────────────────────────────────────────────────

// jsWS is the ECMAScript `\s`: WhiteSpace (TAB VT FF SP NBSP ZWNBSP + Zs)
// plus LineTerminator (LF CR LS PS). RE2's \s is only [\t\n\f\r ].
const jsWS = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// jsDot is the ECMAScript `.` without the s flag: everything except the four
// line terminators. RE2's `.` excludes \n only.
const jsDot = `[^\n\r\x{2028}\x{2029}]`

var patternTwins = map[string]TestFilePattern{
	"spec":   regexp.MustCompile(`\.spec\.ts$`).MatchString,
	"global": regexp.MustCompile(`\.test\.ts$`).MatchString,
	// /a\.test\.ts$/y tested with lastIndex pinned to 0 must START at offset 0.
	"sticky":  regexp.MustCompile(`\Aa\.test\.ts$`).MatchString,
	"ws":      regexp.MustCompile(jsWS + `test\.ts$`).MatchString,
	"any":     regexp.MustCompile(jsDot).MatchString,
	"dotall":  regexp.MustCompile(`(?s).`).MatchString,
	"never":   regexp.MustCompile(`zzz`).MatchString,
	"caseins": regexp.MustCompile(`(?i)\.TEST\.TS$`).MatchString,
}

func patternFor(t *testing.T, key *string) TestFilePattern {
	t.Helper()
	if key == nil {
		return nil
	}
	p, ok := patternTwins[*key]
	if !ok {
		t.Fatalf("unknown pattern key %q", *key)
	}
	return p
}

// ── ImportGraph decoding ────────────────────────────────────────────────────

type entryEnc struct {
	Key  string
	Vals []string
}

func (e *entryEnc) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &e.Key); err != nil {
		return err
	}
	return json.Unmarshal(raw[1], &e.Vals)
}

type graphEnc struct {
	Deps       []entryEnc `json:"deps"`
	Dependents []entryEnc `json:"dependents"`
	External   []entryEnc `json:"external"`
}

func decodeEntries(entries []entryEnc) *jscompat.OrderedMap[string, []string] {
	m := jscompat.NewOrderedMap[string, []string]()
	for _, e := range entries {
		v := e.Vals
		if v == nil {
			v = []string{}
		}
		m.Set(e.Key, v)
	}
	return m
}

func decodeGraph(g graphEnc) importgraph.ImportGraph {
	return importgraph.ImportGraph{
		Deps:       decodeEntries(g.Deps),
		Dependents: decodeEntries(g.Dependents),
		External:   decodeEntries(g.External),
	}
}

// ── per-fn argument shapes ──────────────────────────────────────────────────

type selectArgs struct {
	ChangedFiles []string `json:"changedFiles"`
	Edges        graphEnc `json:"edges"`
	AllTestFiles []string `json:"allTestFiles"`
	PatternKey   *string  `json:"patternKey"`
}

type opEnc struct {
	Op      string `json:"op"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Target  string `json:"target"`
}

type wsArgs struct {
	Ops          []opEnc  `json:"ops"`
	WorkspaceSub *string  `json:"workspaceSub"`
	ChangedFiles []string `json:"changedFiles"`
	MaxFiles     *float64 `json:"maxFiles"`
	MaxBytes     *float64 `json:"maxBytes"`
	PatternKey   *string  `json:"patternKey"`
}

func applyOps(t *testing.T, root string, ops []opEnc) {
	t.Helper()
	for _, o := range ops {
		abs := filepath.Join(root, o.Path)
		switch o.Op {
		case "dir":
			if err := os.MkdirAll(abs, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", abs, err)
			}
		case "file":
			if err := os.WriteFile(abs, []byte(o.Content), 0o644); err != nil {
				t.Fatalf("write %s: %v", abs, err)
			}
		case "symlink":
			if err := os.Symlink(o.Target, abs); err != nil {
				t.Fatalf("symlink %s: %v", abs, err)
			}
		default:
			t.Fatalf("unknown op %q", o.Op)
		}
	}
}

func unmarshalArgs(t *testing.T, fx fixture, dst ...any) {
	t.Helper()
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(fx.ArgsJSON), &raw); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if len(raw) != len(dst) {
		t.Fatalf("arg count: got %d want %d", len(raw), len(dst))
	}
	for i, d := range dst {
		if err := json.Unmarshal(raw[i], d); err != nil {
			t.Fatalf("decode arg %d: %v", i, err)
		}
	}
}

func assertJSON(t *testing.T, got any, want string) {
	t.Helper()
	b, err := jscompat.Stringify(got)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	if string(b) != want {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", b, want)
	}
}

func TestFixtures(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("expected at least 25 fixture cases, got %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fx := range fixtures {
		fx := fx
		seen[fx.Fn]++
		t.Run(fx.Name, func(t *testing.T) {
			switch fx.Fn {
			case "selectImpactedTests":
				var a selectArgs
				unmarshalArgs(t, fx, &a)
				assertJSON(t, SelectImpactedTests(SelectImpactedTestsOptions{
					ChangedFiles:    a.ChangedFiles,
					Edges:           decodeGraph(a.Edges),
					AllTestFiles:    a.AllTestFiles,
					TestFilePattern: patternFor(t, a.PatternKey),
				}), fx.OutJSON)

			case "buildTestCommand":
				var impacted []string
				var runner string
				unmarshalArgs(t, fx, &impacted, &runner)
				assertJSON(t, BuildTestCommand(impacted, Runner(runner)), fx.OutJSON)

			case "computeImpactedTestsForWorkspace":
				var a wsArgs
				unmarshalArgs(t, fx, &a)
				root := t.TempDir()
				applyOps(t, root, a.Ops)
				workspace := root
				if a.WorkspaceSub != nil {
					workspace = filepath.Join(root, *a.WorkspaceSub)
				}
				assertJSON(t, ComputeImpactedTestsForWorkspace(ComputeImpactedTestsForWorkspaceOptions{
					Workspace:       workspace,
					ChangedFiles:    a.ChangedFiles,
					MaxFiles:        a.MaxFiles,
					MaxBytes:        a.MaxBytes,
					TestFilePattern: patternFor(t, a.PatternKey),
				}), fx.OutJSON)

			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}
		})
	}
	for _, fn := range []string{"selectImpactedTests", "buildTestCommand", "computeImpactedTestsForWorkspace"} {
		if seen[fn] == 0 {
			t.Errorf("no fixture cases for %s", fn)
		}
	}
}
