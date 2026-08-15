package exec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// THE EVIDENCE IS NOT NARRATION.
//
// Two defects, one shape. A coding leaf delivered its evidence as things said on
// a wire, and everything said on a wire belongs to the process that heard it.
//
// The engine's terminal line carries no message on a clean pass, so the leaf's
// deliverable fell through to a harness sentence — "The coding run ended without
// a verdict of its own" — on EVERY successful run, while the worker's real
// account of what it had found went past as a text part and was written only to
// the trace.
//
// And the account's file rows were cut out of tool metadata as it streamed, so
// they were per PASS: a node run a second time reported the second run's change
// set, which on a tree where the fix had already landed was nothing at all.
//
// Both halves are fixed the same way: the leaf keeps what the worker actually
// said, and the change set is derived from git against a base the NODE owns
// rather than a base this process remembers.

// A. The worker's last word is the deliverable when the engine says nothing.
func TestASilentTerminalDeliversTheWorkersOwnLastWord(t *testing.T) {
	probe := newSWEProbe(t, "silent-pass")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a passing run returned an error: %v", err)
	}
	if strings.Contains(outcome.Text, "without a verdict of its own") ||
		strings.Contains(outcome.Text, "without saying how it went") {
		t.Fatalf("the harness's void sentence was delivered over the worker's own words:\n%s", outcome.Text)
	}
	if !strings.Contains(outcome.Text, "lookahead that consumed the comma") {
		t.Fatalf("the deliverable does not carry what the worker said:\n%s", outcome.Text)
	}
	if outcome.Account == nil {
		t.Fatal("a run that changed a file and ran its suite kept no account")
	}
	if outcome.Account.Final != fakeSilentPassText {
		t.Fatalf("the account's sign-off = %q, want the worker's last word verbatim", outcome.Account.Final)
	}
	// And the gate is handed it, which is the reader the field exists for: the
	// deliverable it judges has often been composed since.
	if !strings.Contains(strings.Join(outcome.Account.Lines(), "\n"), "lookahead that consumed the comma") {
		t.Fatal("the gate's evidence rows do not carry the worker's last word")
	}
}

// A, the other direction: an engine that DOES say something outranks the
// worker's monologue, because a pipeline summarising its own run is a better
// witness than one of its workers.
func TestTheEnginesOwnTerminalMessageOutranksTheLastWord(t *testing.T) {
	probe := newSWEProbe(t, "pass")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a passing run returned an error: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(outcome.Text), "The parser now accepts trailing commas") {
		t.Fatalf("the deliverable does not open with the engine's own verdict:\n%s", outcome.Text)
	}
}

// B. The change set survives a second pass, and it survives a restart, because
// nothing about it is remembered in this process.
//
// The two-pass simulation is the whole point. Pass one lands a change. Pass two
// is a fresh view value — a different process, as far as anything here can tell —
// that touches nothing. The account it derives must be the account pass one
// derived, because a node's change set is what the NODE changed, and a repair
// round that reported "nothing" over a landed diff is the defect this replaces.
func TestTheChangeSetIsDerivedPerNodeAndSurvivesARestart(t *testing.T) {
	root := newSubstrateRepo(t)
	trace := &tracer{}

	first := &sweView{root: root, dir: root, leaf: "41", trace: trace}
	first.anchor(context.Background())
	if first.substrate == "" {
		t.Fatal("the node's base commit was never recorded")
	}
	baseline := first.substrate

	// Pass one does the work and commits it, exactly as the engine does.
	writeSubstrateFile(t, root, "internal/parser/commas.go", "package parser\n\nfunc Trailing() {}\n")
	commitSubstrate(t, root, "the fix")

	swePassRef(context.Background(), root, "41", baseline, "")
	onePass, span, ok := sweChangeSet(context.Background(), root, "41", first.substrate, true)
	if !ok {
		t.Fatal("the change set could not be derived at all")
	}
	if span.Base != baseline || !span.Derived() {
		t.Fatalf("range = %+v, want it measured from the node's own base %s", span, baseline)
	}
	if len(onePass) != 1 || onePass[0].Path != "internal/parser/commas.go" ||
		onePass[0].Change != ChangeAdded {
		t.Fatalf("one-pass change set = %#v, want the one file it added", onePass)
	}
	if onePass[0].Added != 3 {
		t.Fatalf("added = %d, want the three lines the file has", onePass[0].Added)
	}

	// A sibling lands its own work in between, which is what moves HEAD out from
	// under a base anybody had kept in memory.
	writeSubstrateFile(t, root, "sibling.txt", "not this leaf's\n")
	commitSubstrate(t, root, "a sibling")

	// Pass two: a brand new view value, holding nothing from the first, which is
	// all a restarted process ever has.
	second := &sweView{root: root, dir: root, leaf: "41", trace: trace}
	second.anchor(context.Background())
	if second.substrate != baseline {
		t.Fatalf("the second pass's base = %s, want the ref the first pass wrote (%s) — "+
			"a base that moves per pass erases the earlier passes' work from the account",
			second.substrate, baseline)
	}

	// The second pass changes nothing, so it files no range of its own — and
	// the node's account is still the union of what it has recorded.
	swePassRef(context.Background(), root, "41", gitLines(context.Background(), root, "rev-parse", "HEAD")[0], "")
	twoPass, _, ok := sweChangeSet(context.Background(), root, "41", second.substrate, true)
	if !ok {
		t.Fatal("the second pass could not derive a change set")
	}
	if len(twoPass) != 1 || twoPass[0] != onePass[0] {
		t.Fatalf("two-pass change set = %#v, want it identical to the single-pass one %#v — "+
			"and never the sibling's file", twoPass, onePass)
	}
}

// The derivation reads the tree, not a narration, so it sees what the engine
// left uncommitted and it refuses the engine's own bookkeeping.
func TestTheChangeSetSeesUncommittedWorkAndRefusesTheEnginesSidecars(t *testing.T) {
	root := newSubstrateRepo(t)
	view := &sweView{root: root, dir: root, leaf: "41", trace: &tracer{}}
	view.anchor(context.Background())

	writeSubstrateFile(t, root, "committed.txt", "one\n")
	commitSubstrate(t, root, "committed")
	writeSubstrateFile(t, root, "loose.txt", "two\n")
	writeSubstrateFile(t, root, ".codeaf/state.json", "{}\n")

	swePassRef(context.Background(), root, "41", view.substrate, "")
	files, _, ok := sweChangeSet(context.Background(), root, "41", view.substrate, true)
	if !ok {
		t.Fatal("the change set could not be derived")
	}
	seen := map[string]bool{}
	for _, file := range files {
		seen[file.Path] = true
	}
	if !seen["committed.txt"] || !seen["loose.txt"] {
		t.Fatalf("change set = %#v, want both the committed and the uncommitted file", files)
	}
	if seen[".codeaf/state.json"] {
		t.Fatalf("the engine's own bookkeeping reached the account: %#v", files)
	}
}

// B, end to end: a real run's account is the derived one, it names its own
// range, and the change's text is on disk where a reader can open it.
func TestARunsAccountCarriesTheDerivedChangeAndItsPatch(t *testing.T) {
	probe := newSWEProbe(t, "silent-pass")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a passing run returned an error: %v", err)
	}
	account := outcome.Account
	if account == nil {
		t.Fatal("the run kept no account")
	}
	// The three facts the repair path turns on, taken off a real run: the
	// worker's product is a change, the change is in the repository, and the
	// work's own checks settled. Together they are what makes a failed gate buy
	// prose instead of a second run of the whole engine.
	if !Mutates(probe.worker) {
		t.Fatal("the coding worker does not declare that its deliverable is a change")
	}
	if !account.Verified() {
		t.Fatalf("a run whose suite passed does not report itself verified: %#v", account.Checks)
	}
	if !account.Landed() {
		t.Fatalf("the account does not report landed work: files=%#v range=%+v", account.Files, account.Range)
	}
	// The stub leaves its own argv record beside the work, exactly as the engine
	// leaves sidecars; what matters is that the file the engine WROTE is a row
	// derived from the repository rather than from anything said on the wire.
	written := false
	for _, file := range account.Files {
		if file.Path == "fixed.txt" && file.Change == ChangeAdded {
			written = true
		}
	}
	if !written {
		t.Fatalf("files = %#v, want the file the engine wrote, added", account.Files)
	}
	if account.ChangeRange() == "" {
		t.Fatal("the account cannot name the span it was measured over")
	}
	if account.Patch == "" {
		t.Fatal("the change's own text was never written anywhere a reader could open it")
	}
	patch, err := os.ReadFile(account.Patch)
	if err != nil {
		t.Fatalf("the patch handle names nothing on disk: %v", err)
	}
	if !strings.Contains(string(patch), "the parser is fixed") {
		t.Fatalf("the patch does not carry the change's own lines:\n%s", patch)
	}
	// The patch is evidence about the deliverable and never the deliverable, so
	// it must not turn up in the list of files the work produced.
	for _, artifact := range outcome.Artifacts {
		if strings.HasSuffix(artifact, ".patch") {
			t.Fatalf("the evidence patch was delivered as an artifact: %v", outcome.Artifacts)
		}
	}
}

// C. The repair path's decision is three structural facts and no reading of
// prose, so a finished, landed, verified change never buys a second engine run.
func TestOnlyALandedVerifiedChangeByAMutatingWorkerComposes(t *testing.T) {
	landed := &Account{
		Files: []FileChange{{Path: "a.go", Change: ChangeChanged, Added: 4, Removed: 1}},
		Range: Range{Base: "aaaaaaaaaaaa", Head: "bbbbbbbbbbbb"},
		Checks: []Check{
			{Command: "go test ./...", Passed: true},
		},
	}
	if !Mutates(&SWE{}) {
		t.Fatal("the coding worker does not declare that its deliverable is a change")
	}
	if Mutates(&Linear{}) {
		t.Fatal("the generalist declares itself a mutating worker")
	}
	if !landed.Landed() || !landed.Verified() {
		t.Fatalf("a landed, green account does not read as one: %#v", landed)
	}

	// Narration alone — rows with no derived range behind them — is exactly the
	// state that must NOT skip the worker: nothing has been shown to be in the
	// tree, so there may well be work left to do.
	narrated := &Account{
		Files:  []FileChange{{Path: "a.go", Change: ChangeChanged, Added: 4}},
		Checks: []Check{{Command: "go test ./...", Passed: true}},
	}
	if narrated.Landed() {
		t.Fatal("a change nobody derived from the repository reads as landed")
	}

	// And a green range with a red suite is unsettled work, whatever is on disk.
	red := &Account{
		Files:  landed.Files,
		Range:  landed.Range,
		Checks: []Check{{Command: "go test ./...", Passed: false}},
	}
	if red.Verified() {
		t.Fatal("an account with a failing check reports itself verified")
	}
}

// ── the substrate fixtures ───────────────────────────────────────────────────

func newSubstrateRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("the substrate account needs git")
	}
	root := t.TempDir()
	initialized, err := ensureGitRepository(context.Background(), root, obsDir+"/", traceDir+"/")
	if err != nil {
		t.Fatal(err)
	}
	if !initialized {
		t.Fatal("the fixture repository was not created")
	}
	return root
}

func writeSubstrateFile(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitSubstrate(t *testing.T, root, message string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := gitQuiet(ctx, root, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if err := sweCommit(ctx, root, message); err != nil {
		t.Fatal(err)
	}
}
