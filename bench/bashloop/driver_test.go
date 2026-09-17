package main

// driver_test.go — the wave's own proof, all of it dry.
//
// The brief's acceptance: every invocation of both arms is printed, the arms
// differ only in the belt environment variable, the six cells and the
// same-question pair mode are all present, and no test here makes a live
// model call or reads a provider key. Everything below runs against files
// this test wrote itself.

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ── the dry run prints every invocation ─────────────────────────────────────

func TestTheDryRunPrintsEveryInvocationOfBothArms(t *testing.T) {
	out := &output{}
	err := run([]string{"-out", t.TempDir(), "-dry-run"}, out, io.Discard)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	text := out.b.String()

	// Thirty-six invocations: six cells, two arms, three replicates.
	headers := regexp.MustCompile(`(?m)^\[\d+/(\d+)\]`).FindAllString(text, -1)
	if len(headers) != 36 {
		t.Fatalf("the dry run printed %d invocation headers, want 36:\n%s", len(headers), head(text, 40))
	}

	// Every invocation carries what the brief says it must: arm, cell,
	// replicate, env, brief — and the model the grid is pinned to.
	for _, part := range []string{"arm=A", "arm=B", "cell=c1", "cell=c6", "replicate=3",
		"env: CODEAF_TASK_BELT=(unset)", "env: CODEAF_TASK_BELT=bash", "brief:", pinnedModel} {
		if !strings.Contains(text, part) {
			t.Fatalf("the dry run never printed %q", part)
		}
	}

	// And it executed none of it: the plan was printed to a throwaway output
	// root that this dry run never made.
	entries, err := os.ReadDir(t.TempDir())
	if err == nil && len(entries) != 0 {
		t.Fatalf("the dry run wrote %v into the output root", names(entries))
	}
}

// ── the arms differ only in the belt env ────────────────────────────────────

func TestTheArmsDifferOnlyInTheBeltEnv(t *testing.T) {
	out := &output{}
	if err := run([]string{"-out", t.TempDir(), "-dry-run"}, out, io.Discard); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	blocks := invocationBlocks(out.b.String())
	if len(blocks) != 36 {
		t.Fatalf("parsed %d invocation blocks, want 36", len(blocks))
	}
	for _, c := range allCells() {
		for r := 1; r <= 3; r++ {
			a, b := findBlock(blocks, ArmShipped, c.id, r), findBlock(blocks, ArmBash, c.id, r)
			if a == "" || b == "" {
				t.Fatalf("cell %s replicate %d: missing a block (A=%q B=%q)", c.id, r, a, b)
			}
			if stripBelt(stripPaths(a)) != stripBelt(stripPaths(b)) {
				t.Fatalf("%s r%d: the arms differ in more than the belt env:\nA: %s\nB: %s", c.id, r, a, b)
			}
			if !strings.Contains(a, "CODEAF_TASK_BELT=(unset)") {
				t.Fatalf("arm A's %s r%d does not leave the belt unset", c.id, r)
			}
			if !strings.Contains(b, "CODEAF_TASK_BELT=bash") {
				t.Fatalf("arm B's %s r%d does not set the belt to bash", c.id, r)
			}
		}
	}
}

// ── the six cells and the pair mode are present ─────────────────────────────

func TestTheSixCellsAreAllPresent(t *testing.T) {
	cells := allCells()
	if len(cells) != 6 {
		t.Fatalf("%d cells defined, want six", len(cells))
	}
	seen := map[string]bool{}
	for _, c := range cells {
		if c.brief == "" || c.fixture == "" {
			t.Fatalf("cell %+v is missing its brief or its fixture", c)
		}
		root := "fixtures/" + c.fixture
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("cell %s names fixture %s, which does not ship with the branch: %v", c.id, c.fixture, err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) == 0 {
			t.Fatalf("cell %s names fixture %s, which ships empty: %v", c.id, c.fixture, err)
		}
		seen[c.id] = true
	}
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5", "c6"} {
		if !seen[id] {
			t.Fatalf("cell %s is missing from the grid", id)
		}
	}
}

func TestThePairModeIsOneBriefOnBothArms(t *testing.T) {
	out := &output{}
	if err := run([]string{"-mode", "pair", "-out", t.TempDir(), "-dry-run"}, out, io.Discard); err != nil {
		t.Fatalf("pair dry run: %v", err)
	}
	blocks := invocationBlocks(out.b.String())
	if len(blocks) != 2 {
		t.Fatalf("the pair mode composed %d invocations, want 2", len(blocks))
	}
	first, second := briefOf(blocks[0]), briefOf(blocks[1])
	if first == "" || first != second {
		t.Fatalf("the pair mode did not hand the same brief to both arms:\n%q\n%q", first, second)
	}
	if strings.TrimSpace(first) != strings.TrimSpace(cellC1.brief) {
		t.Fatalf("the pair mode's brief is not the cell's own, verbatim:\n%q", first)
	}
}

// ── the fixtures grade the way the design says they do ──────────────────────

// Every grader is proved against materialized fixtures: the seed state fails
// where the cell expects failure, and a hand-made solution passes. No model,
// no key, no engine — the graders and the checks they run.
func TestTheCellGradersGrade(t *testing.T) {
	if !goAvailable() {
		t.Skip("no go toolchain: the graders cannot run their own checks")
	}

	t.Run("c1 red seed, green fix", func(t *testing.T) {
		dir, seed := materialized(t, cellC1)
		if g := gradeCell(cellC1, dir, readings{}, pristine(t, cellC1)); g.Pass {
			t.Fatalf("c1 graded pass on the red seed: %s", g.Detail)
		}
		fixed := strings.Replace(seed["clamp.go"], "if x > hi {\n\t\treturn lo", "if x > hi {\n\t\treturn hi", 1)
		if fixed == seed["clamp.go"] {
			t.Fatal("the c1 seed does not carry the bug this test means to fix")
		}
		writeFixtureFile(t, dir, "clamp.go", fixed)
		if g := gradeCell(cellC1, dir, readings{}, pristine(t, cellC1)); !g.Pass {
			t.Fatalf("c1 graded no on a fixed fixture: %s", g.Detail)
		}
	})

	t.Run("c2 needs the feature", func(t *testing.T) {
		dir, _ := materialized(t, cellC2)
		if g := gradeCell(cellC2, dir, readings{}, pristine(t, cellC2)); g.Pass {
			t.Fatal("c2 graded pass before any histogram existed")
		}
	})

	t.Run("c3 wants one copy and the same surface", func(t *testing.T) {
		dir, _ := materialized(t, cellC3)
		if g := gradeCell(cellC3, dir, readings{}, pristine(t, cellC3)); g.Pass {
			t.Fatalf("c3 graded pass before the refactor: %s", g.Detail)
		}
	})

	t.Run("c4 needs a grounded report", func(t *testing.T) {
		dir, _ := materialized(t, cellC4)
		if g := gradeCell(cellC4, dir, readings{}, pristine(t, cellC4)); g.Pass {
			t.Fatal("c4 graded pass with no REPORT.md")
		}
		writeFixtureFile(t, dir, "REPORT.md", c4ContractReport())
		if g := gradeCell(cellC4, dir, readings{}, pristine(t, cellC4)); !g.Pass {
			t.Fatalf("c4 graded no on a report that meets the contract: %s", g.Detail)
		}
	})

	t.Run("c5 wants children and the integration note", func(t *testing.T) {
		dir, _ := materialized(t, cellC5)
		if g := gradeCell(cellC5, dir, readings{}, pristine(t, cellC5)); g.Pass {
			t.Fatal("c5 graded pass on the seed, whose suites are red and which has no integration note")
		}
		writeFixtureFile(t, dir, "INTEGRATION.md",
			"# Integration\n\nparse: trailing fields; render: width; notify: domain; archive: extension.\n")
		if g := gradeCell(cellC5, dir, readings{}, pristine(t, cellC5)); g.Pass {
			t.Fatal("c5 graded pass with no children landed in the graph")
		}
		// The work itself, as a hand-made stand-in: the four fixes, so the
		// suite the grader runs is green and the children gate is all that
		// separates this row from a pass.
		writeFixtureFile(t, dir, "parse/parse.go", "package parse\n\nimport \"strings\"\n\nfunc SplitCSV(line string) []string { return strings.Split(line, \",\") }\n")
		writeFixtureFile(t, dir, "render/render.go", "package render\n\nimport \"strings\"\n\nfunc Bar(n, width int) string {\n\tif n > width {\n\t\tn = width\n\t}\n\tif n < 0 {\n\t\tn = 0\n\t}\n\treturn strings.Repeat(\"#\", n) + strings.Repeat(\"-\", width-n)\n}\n")
		writeFixtureFile(t, dir, "notify/notify.go", "package notify\n\nimport \"strings\"\n\nfunc Recipient(address string) string {\n\ti := strings.LastIndex(address, \"@\")\n\tif i < 0 {\n\t\treturn \"\"\n\t}\n\treturn address[i+1:]\n}\n")
		writeFixtureFile(t, dir, "archive/archive.go", "package archive\n\nimport \"strings\"\n\nfunc Ext(path string) string {\n\tname := path\n\tif i := strings.LastIndex(path, \"/\"); i >= 0 {\n\t\tname = path[i+1:]\n\t}\n\tdot := strings.LastIndex(name, \".\")\n\tif dot < 0 {\n\t\treturn \"\"\n\t}\n\treturn name[dot+1:]\n}\n")
		wide := readings{ChildrenDone: 4, ChildrenTotal: 4}
		if g := gradeCell(cellC5, dir, wide, pristine(t, cellC5)); !g.Pass {
			t.Fatalf("c5 graded no with four children landed: %s", g.Detail)
		}
	})

	t.Run("c6 needs the image referenced", func(t *testing.T) {
		dir, _ := materialized(t, cellC6)
		if g := gradeCell(cellC6, dir, readings{}, pristine(t, cellC6)); g.Pass {
			t.Fatal("c6 graded pass with no ARCHITECTURE.md")
		}
		writeFixtureFile(t, dir, "ARCHITECTURE.md",
			"## Flow\n\nCart then pricing then gateway then receipt.\n\n![the flow](docs/architecture.png)\n")
		if g := gradeCell(cellC6, dir, readings{}, pristine(t, cellC6)); g.Pass {
			t.Fatal("c6 graded pass while the referenced image does not exist")
		}
		writeFixtureFile(t, dir, "docs/architecture.png", []byte("png bytes"))
		if g := gradeCell(cellC6, dir, readings{}, pristine(t, cellC6)); !g.Pass {
			t.Fatalf("c6 graded no with the image in place: %s", g.Detail)
		}
	})
}

// ── the readings come off the engine's own records ──────────────────────────

func TestTheReadingsComeFromTheJournalsAndTheLedger(t *testing.T) {
	homeDir := t.TempDir()
	placeDir := t.TempDir()
	journals := filepath.Join(placeDir, "tasks")
	if err := os.MkdirAll(journals, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(homeDir, "v3", "usage.jsonl"),
		`{"model":"deepseek/deepseek-v4-flash","usd":0.0125,"in":900,"out":300}`+"\n"+
			`{"model":"deepseek/deepseek-v4-flash","usd":0.0031,"in":200,"out":100}`+"\n"+
			`{"unbilled":true}`+"\n")
	write(t, filepath.Join(journals, "20260916-120000.000000_1.jsonl"),
		`{"type":"message","role":"assistant","toolCalls":[{"function":{"name":"bash","arguments":"{\"command\":\"grep -c TODO main.go\"}"}}]}`+"\n"+
			`{"type":"took","took":{"callId":"a"}}`+"\n"+
			`{"type":"message","role":"tool","content":"3"}`+"\n"+
			`{"type":"message","role":"assistant","toolCalls":[{"function":{"name":"bash","arguments":"{\"command\":\"sed -i s/TODO/DONE/ main.go\"}"}}]}`+"\n"+
			`{"type":"took","took":{"callId":"b"}}`+"\n"+
			`{"type":"message","role":"tool","content":"[output truncated; full output: /logs/action-000001.txt]"}`+"\n")
	write(t, filepath.Join(placeDir, "tasks.json"),
		`{"type":"tasks","version":1,"seq":3,"nodes":[{"id":1,"parent":0,"state":"done"},`+
			`{"id":2,"parent":1,"state":"done"},{"id":2,"parent":1,"state":"done"},{"id":3,"parent":1,"state":"failed"}]}`)

	got := collectReadings(homeDir, placeDir, journals, t.TempDir(), nil)
	if got.Steps != 2 {
		t.Fatalf("steps = %d, want 2 — the journal's own took lines", got.Steps)
	}
	if got.Truncations != 1 {
		t.Fatalf("truncations = %d, want 1", got.Truncations)
	}
	if got.EditIdiomFlags != 0 {
		t.Fatalf("idiom flags = %d, want 0: the counting grep came first", got.EditIdiomFlags)
	}
	if got.CostUSD < 0.0155 || got.Unbilled != 1 {
		t.Fatalf("cost = %v with %d unbilled, want the ledger's own sum and one unbilled row", got.CostUSD, got.Unbilled)
	}
	if len(got.ModelsUsed) != 1 || got.ModelsUsed[0] != pinnedModel {
		t.Fatalf("models used = %v, want the one billed slug", got.ModelsUsed)
	}
	if got.ChildrenDone != 2 || got.ChildrenTotal != 3 || got.NodesFailed != 1 {
		t.Fatalf("family = %d/%d done, %d failed, want 2/3 and 1", got.ChildrenDone, got.ChildrenTotal, got.NodesFailed)
	}
}

// ── the table quotes medians over the graded passes only ────────────────────

func TestTheTableQuotesMediansOverGradedPassesOnly(t *testing.T) {
	rows := []row{
		{Arm: ArmShipped, Cell: "c1", Replicate: 1, Graded: true, Steps: 10, CostUSD: 0.04, WallSeconds: 100},
		{Arm: ArmShipped, Cell: "c1", Replicate: 2, Graded: false, WallSeconds: 500},
		{Arm: ArmShipped, Cell: "c1", Replicate: 3, Graded: true, Steps: 20, CostUSD: 0.02, WallSeconds: 200},
	}
	s, ok := summarize(rows, ArmShipped, "c1")
	if !ok || s.N != 3 || s.Passes != 2 {
		t.Fatalf("summary = %+v, want n=3 passes=2", s)
	}
	if s.MedSteps != 15 || s.MedCost != 0.03 || s.MedWall != 150 {
		t.Fatalf("medians = %v/%v/%v, want 15/0.03/150 — over the graded passes only",
			s.MedSteps, s.MedCost, s.MedWall)
	}
}

// ── no test here drives a model ─────────────────────────────────────────────

// TestNoTestHereDrivesAModel is the file's own gate, the same shape
// internal/e2e keeps: no test in this package may read a provider key or
// start a live engine. This gate names those doors, so it is the one file
// the scan skips — the same exception livekeygate.go makes for itself.
func TestNoTestHereDrivesAModel(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") || name == "driver_test.go" {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, door := range []string{"machineKey(", "APIKeyAt(", "OPENROUTER_API_KEY", "OPENAI_API_KEY", "session.New("} {
			if strings.Contains(string(body), door) {
				t.Errorf("%s reaches for %s — a test here drives nothing real", name, door)
			}
		}
	}
}

// ── scaffolding ─────────────────────────────────────────────────────────────

// c4ContractReport is a report that meets c4's contract, written by the test
// out of the corpus's own figures.
func c4ContractReport() string {
	return "## Summary\n\nThe quarter ran 47 deploys and held its incident count steady.\n\n" +
		"## Findings\n\nDeploys rose from 31 to 47; incidents held at 12 with a median restore of 34 minutes.\n\n" +
		"## Risks\n\nThe search cluster is 61% of spend and the staging nodes page falsely.\n\n" +
		"## Recommendations\n\n1. Automate the smoke checklist.\n2. Right-size the search cluster.\n3. Fix the staging disk alert.\n"
}

// materialized writes one fixture fresh into a temp directory and answers
// the directory and its pristine bytes.
func materialized(t *testing.T, c cell) (string, map[string]string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), c.id)
	if _, err := materializeFixture(dir, c); err != nil {
		t.Fatalf("materialize %s: %v", c.id, err)
	}
	files, err := pristineFixture(c)
	if err != nil {
		t.Fatal(err)
	}
	seed := map[string]string{}
	for name, body := range files {
		seed[name] = string(body)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir, seed
}

func pristine(t *testing.T, c cell) fixtureFiles {
	t.Helper()
	files, err := pristineFixture(c)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func writeFixtureFile(t *testing.T, dir, rel string, body any) {
	t.Helper()
	target := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	switch content := body.(type) {
	case string:
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	case []byte:
		if err := os.WriteFile(target, content, 0o600); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("writeFixtureFile: unsupported body %T", body)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// output is a writer that keeps what it was given.
type output struct{ b strings.Builder }

func (o *output) Write(p []byte) (int, error) { return o.b.Write(p) }

// invocationBlock is one printed invocation, from its [NN/MM] header to the
// blank line before the next one.
type invocationBlock = string

func invocationBlocks(text string) []string {
	var blocks []string
	var current []string
	for _, line := range splitLines(text) {
		if isBlockHeader(line) {
			if isBlockHeaderCurrent(current) {
				blocks = append(blocks, strings.Join(current, "\n"))
			}
			current = []string{line}
			continue
		}
		current = append(current, line)
	}
	if isBlockHeaderCurrent(current) {
		blocks = append(blocks, strings.Join(current, "\n"))
	}
	return blocks
}

func isBlockHeaderCurrent(current []string) bool {
	return len(current) > 0 && isBlockHeader(current[0])
}

func isBlockHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.Contains(line, "] arm=")
}

var blockHeader = regexp.MustCompile(`\] arm=(A|B) cell=(\S+) replicate=(\d+)`)

func findBlock(blocks []string, arm Arm, cellID string, replicate int) string {
	for _, b := range blocks {
		m := blockHeader.FindStringSubmatch(b)
		if m == nil {
			continue
		}
		if m[1] == string(arm) && m[2] == cellID && m[3] == itoa(replicate) {
			return b
		}
	}
	return ""
}

// stripBelt removes the belt token, the one difference the comparison is
// about, and the invocation's own position in the interleaved order — the
// counter says when the cell runs, never what the cell is.
func stripBelt(block string) string {
	block = regexp.MustCompile(`^\[\d+/\d+\] arm=[AB] `).ReplaceAllString(block, "[")
	return regexp.MustCompile(`env: CODEAF_TASK_BELT=\S+`).ReplaceAllString(block, "env: BELT")
}

// stripPaths removes the per-invocation directories — where the run keeps
// its own home and fixture — which differ by design and carry nothing of
// the arm's protocol.
func stripPaths(block string) string {
	lines := splitLines(block)
	var kept []string
	for _, line := range lines {
		if strings.HasPrefix(line, "  home: ") || strings.HasPrefix(line, "  fixture: ") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// briefOf answers a block's brief text, verbatim.
func briefOf(block string) string {
	var lines []string
	in := false
	for _, line := range splitLines(block) {
		if strings.HasPrefix(line, "  brief: |") {
			in = true
			continue
		}
		if in {
			if !strings.HasPrefix(line, "    ") {
				break
			}
			lines = append(lines, strings.TrimPrefix(line, "    "))
		}
	}
	return strings.Join(lines, "\n")
}

func names(entries []os.DirEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func head(text string, lines int) string {
	parts := splitLines(text)
	if len(parts) > lines {
		parts = parts[:lines]
	}
	return strings.Join(parts, "\n")
}

func itoa(n int) string { return strconv.Itoa(n) }
