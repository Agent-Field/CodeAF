package exec

// The accounting is a property of a RUN, and these tests are what keep it one.
//
// [Outcome.Account] was written for every worker that can observe its own change
// set, and then exactly one belt wrote it. That belt was removed in 05b99537 and
// the field went unassigned for a hundred commits: `grep -rn 'Account *='` over
// the whole tree answered nothing, every reader downstream went on politely
// reporting "no claim", and no test anywhere failed. A measurement wired into
// one loop leaves with that loop, quietly, and the only thing that can make the
// next removal loud is a test that reads the package's own shape rather than its
// behaviour.
//
// So the structural half below reads the package's own syntax and asserts four
// things about it: that PhotographAfter DEFERS AccountFor from its own body, that
// the seam is reached from the belt's own landing and from nowhere else, that
// nothing in this package creates an [Outcome] outside the single place
// linear.go does, and that only the belt and its landing hand one back.
//
// Each of those is spelled as narrowly as it is because every looser spelling
// was walked past by something ordinary: an identifier match passes for `if
// false { AccountFor(...) }`, a count of call sites passes for a forwarder, and
// a search for composite literals passes for `new(Outcome)` and for a named
// result never assigned. A tripwire that can be stepped over silently is the
// original defect wearing a different hat.
//
// The behavioural half then asserts what the account actually says, in a real
// git work tree with real files written into it — including the two answers it
// must refuse to give: a span or a patch where there is no repository, and a red
// suite acquitted against a baseline nobody read.

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// THE ONE SEAM DEFERS THE ONE ACCOUNTING, and it is read off the source because
// that is the only way to state it. A behavioural test can prove that some path
// accounts for some run; this states that PhotographAfter itself is where it
// happens, which is what stops the next belt carrying its own copy and the one
// after that carrying none.
//
// It asks for a DEFER STATEMENT IN THE FUNCTION'S OWN BODY and not merely for
// the name to appear somewhere inside it. Every weaker spelling of this test
// passes for a call that never runs — `if false { AccountFor(...) }` — for one
// behind a condition, for one on a goroutine that outlives the leaf, and for one
// placed before the six early exits it is supposed to survive. A tripwire that
// can be stepped over silently is the original defect wearing a different hat.
//
// What it does NOT prove is that AccountFor does anything; the behavioural tests
// below are what hold that, and they are the right place for it.
func TestTheLandingSeamDefersTheAccounting(t *testing.T) {
	file := parseExecFile(t, "photograph.go")
	seam := findFunc(file, "PhotographAfter")
	if seam == nil || seam.Body == nil {
		t.Fatal("PhotographAfter is gone from internal/exec/photograph.go — " +
			"the accounting seam moved and this test has to move with it")
	}
	for _, statement := range seam.Body.List {
		deferred, ok := statement.(*ast.DeferStmt)
		if !ok {
			continue
		}
		if named, ok := deferred.Call.Fun.(*ast.Ident); ok && named.Name == "AccountFor" {
			return
		}
	}
	t.Fatal("PhotographAfter's own body has no `defer AccountFor(...)` statement. The leaf's " +
		"account of its own change is taken at this one seam, for every belt, and it is " +
		"DEFERRED so that all six exits from here owe it — a call anywhere else in this " +
		"function is a call some landings do not reach. Restore it; see accountfor.go.")
}

// AND IT IS REACHED FROM ONE PLACE, WHICH IS THE BELT'S OWN LANDING. Two seams
// is how the accounting comes to be taken on one and not the other, which is the
// shape the verification photograph itself was in before it moved out of a belt.
//
// The test names the FUNCTION the seam is reached from and not merely how many
// call it, because a wrapper is the cheap way past a count: `land` stops naming
// the seam, a forwarder names it once, and a bare tally is still one. It also
// counts every mention rather than every call, so the seam taken as a function
// value and called through a variable is a mention where it should not be.
func TestTheSeamIsReachedOnlyFromTheBeltsOwnLanding(t *testing.T) {
	const reachedFrom = "linear.go:land"
	var sites []string
	forEachExecFile(t, func(name string, file *ast.File, fset *token.FileSet) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if named, ok := node.(*ast.Ident); ok && named.Name == "PhotographAfter" {
					sites = append(sites, name+":"+function.Name.Name)
				}
				return true
			})
		}
	})
	// The seam's own body mentions itself nowhere, so its declaration needs no
	// exemption; what is left is every place that reaches it.
	if len(sites) != 1 || sites[0] != reachedFrom {
		t.Fatalf("PhotographAfter is reached from %v, and it is meant to be reached from %s "+
			"alone — the one exit every landing of the leaf belt passes through. A second "+
			"caller, a forwarder, or the seam taken as a value is a second place the "+
			"accounting and the photograph can disagree about what a run did.", sites, reachedFrom)
	}
}

// AND NO BELT BRINGS AN OUTCOME INTO THE WORLD OF ITS OWN. Creating one is how a
// belt starts answering for a run, and a belt that creates its own outside the
// place linear.go does is a belt that will be filling in Account, or forgetting
// to, on its own terms.
//
// All four ways of creating one are read, because they are interchangeable and a
// test that knew only the composite literal would have been walked past by
// `new(Outcome)`, by `var landed Outcome`, and by a helper whose named result is
// an Outcome it never assigns.
//
// cmd/aforge/exec.go builds two of these for a run that NEVER STARTED — a
// workspace that could not be opened, a client that could not be built — and
// those legitimately carry no account: there is no change to account for. They
// are in another package and out of this walk deliberately.
func TestOnlyOnePlaceInThePackageCreatesAnOutcome(t *testing.T) {
	var created []string
	forEachExecFile(t, func(name string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(node ast.Node) bool {
			where := func(pos token.Pos) {
				created = append(created, fmt.Sprintf("%s:%d", name, fset.Position(pos).Line))
			}
			switch shape := node.(type) {
			case *ast.CompositeLit: // Outcome{...}, and &Outcome{...} through its operand.
				if isOutcomeType(shape.Type, false) {
					where(shape.Pos())
				}
			case *ast.CallExpr: // new(Outcome)
				if fn, ok := shape.Fun.(*ast.Ident); ok && fn.Name == "new" &&
					len(shape.Args) == 1 && isOutcomeType(shape.Args[0], false) {
					where(shape.Pos())
				}
			case *ast.ValueSpec: // var landed Outcome
				if isOutcomeType(shape.Type, false) {
					where(shape.Pos())
				}
			case *ast.FuncType: // func … (landed Outcome), whose zero value is one
				if shape.Results == nil {
					return true
				}
				for _, result := range shape.Results.List {
					if len(result.Names) > 0 && isOutcomeType(result.Type, false) {
						where(result.Pos())
					}
				}
			}
			return true
		})
	})
	if len(created) != 1 || !strings.HasPrefix(created[0], "linear.go") {
		t.Fatalf("an Outcome is created in %v. The leaf belt in linear.go is the one place "+
			"that brings one into existence, so that every run reaches the landing seam "+
			"that accounts for it; a belt with its own outcome is a belt with its own "+
			"accounting, or with none.", created)
	}
}

// AND THESE ARE THE ONLY TWO FUNCTIONS THAT HAND ONE BACK. A new one is not
// forbidden — it is a decision, and this is the line somebody has to add to make
// it, having read why it exists. Run is the belt and land is its single exit;
// both pass along the outcome linear.go created rather than making one, which is
// what keeps them honest.
func TestOnlyTheBeltAndItsLandingHandBackAnOutcome(t *testing.T) {
	expected := map[string]bool{"linear.go:Run": true, "linear.go:land": true}
	var handing []string
	forEachExecFile(t, func(name string, file *ast.File, fset *token.FileSet) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Type.Results == nil {
				continue
			}
			for _, result := range function.Type.Results.List {
				if isOutcomeType(result.Type, true) {
					handing = append(handing, name+":"+function.Name.Name)
					break
				}
			}
		}
	})
	for _, one := range handing {
		if !expected[one] {
			t.Errorf("%s returns an Outcome and is not one of the two functions that are "+
				"meant to. A function that hands back an outcome is a producer of one: "+
				"either it reaches PhotographAfter and is accounted for, or it is a "+
				"second belt. Decide which, then add it here with the reason.", one)
		}
		delete(expected, one)
	}
	for one := range expected {
		t.Errorf("%s no longer returns an Outcome. The landing seam moved; move this test "+
			"and the accounting with it rather than deleting the line.", one)
	}
}

// ── what the account then says ───────────────────────────────────────────────

// A leaf that changed a file in a git work tree accounts for it: the path, the
// span it was measured across, and the change's own text on disk.
func TestALandingInAGitTreeAccountsForItsOwnChange(t *testing.T) {
	root := gitTree(t)
	workspace := accountWorkspace(t, root)
	task := Task{Goal: t.Name(), NodeKey: "task-1"}

	outcome := landFakeLeaf(t, workspace, task, func() {
		writeFile(t, root, "notes.md", "the leaf's own line\n")
		writeFile(t, root, "seed.txt", "seed\nand a second line\n")
	})

	account := outcome.Account
	if account == nil {
		t.Fatal("a leaf that changed two files left no account of it")
	}
	if !named(account, "notes.md") || !named(account, "seed.txt") {
		t.Fatalf("the account does not name what the leaf changed: %+v", account.Files)
	}
	if !account.Range.Derived() {
		t.Fatalf("a change in a git work tree was measured across no span: %+v", account.Range)
	}
	if !account.Landed() {
		t.Error("work that is on disk, in the shared tree, does not read as landed")
	}
	if account.Patch == "" {
		t.Fatal("the change set's own text was not written anywhere a reader can open it")
	}
	patch, err := os.ReadFile(account.Patch)
	if err != nil {
		t.Fatalf("the patch handle opens nothing: %v", err)
	}
	// Both halves: a file git has never seen, and an edit to one it has.
	if !strings.Contains(string(patch), "the leaf's own line") {
		t.Errorf("the patch is silent about a file the leaf created:\n%s", patch)
	}
	if !strings.Contains(string(patch), "and a second line") {
		t.Errorf("the patch is silent about an edit to a tracked file:\n%s", patch)
	}
	// A tracked edit has a size, because git measured one.
	for _, file := range account.Files {
		if file.Path == "seed.txt" && file.Added == 0 {
			t.Errorf("git measured the edit and the account kept no size: %+v", file)
		}
	}
	// THE PATCH IS EVIDENCE ABOUT THE DELIVERABLE AND NEVER A DELIVERABLE.
	for _, artifact := range workspace.Artifacts(task.leafKey()) {
		if strings.HasSuffix(artifact, ".patch") {
			t.Errorf("the patch file is being offered as something the work produced: %q", artifact)
		}
	}
}

// AND A WORKSPACE THAT IS NO REPOSITORY STILL NAMES WHAT IT WROTE. What it must
// not do is claim a span or a diff it never measured: no claim, never a false
// one.
func TestALandingOutsideARepositoryNamesItsFilesAndClaimsNothingElse(t *testing.T) {
	root := t.TempDir()
	workspace := accountWorkspace(t, root)
	task := Task{Goal: t.Name(), NodeKey: "task-1"}

	outcome := landFakeLeaf(t, workspace, task, func() {
		writeFile(t, root, "notes.md", "written outside any repository\n")
	})

	account := outcome.Account
	if account == nil || !named(account, "notes.md") {
		t.Fatalf("a leaf outside a repository did not name the file it wrote: %+v", account)
	}
	if account.Range.Derived() {
		t.Errorf("a span was claimed where there is no history to measure it in: %+v", account.Range)
	}
	if account.Patch != "" {
		t.Errorf("a diff was claimed where nothing could derive one: %q", account.Patch)
	}
	if account.Landed() {
		t.Error("work with no measured span reads as landed, which is a claim nobody made")
	}
}

// A RUN THAT CHANGED NOTHING AND READ NOTHING MAKES NO CLAIM. Nil is how that is
// spelled everywhere it is rendered, and an empty account attached anyway would
// turn "nobody looked" into "we looked and there was nothing".
func TestALandingThatChangedNothingLeavesNoAccount(t *testing.T) {
	workspace := accountWorkspace(t, gitTree(t))
	task := Task{Goal: t.Name(), NodeKey: "task-1"}

	outcome := landFakeLeaf(t, workspace, task, func() {})

	if !outcome.Account.Empty() {
		t.Fatalf("a leaf that changed nothing claimed something: %+v", outcome.Account)
	}
}

// COMMANDS RIDE AN ACCOUNT THAT ALREADY EXISTS. A leaf that changed no file
// and had no finished-tree reading still leaves no account, however many shell
// commands it issued, because nobody looking is not a claim that nothing was
// found.
func TestALeafWithNothingToAccountForStillLeavesTheAccountNil(t *testing.T) {
	workspace := accountWorkspace(t, t.TempDir())
	outcome := &Outcome{Commands: []string{"go test ./..."}, CommandsRun: 1}
	AccountFor(context.Background(), workspace,
		Task{Goal: t.Name(), NodeKey: "task-1"}, "", false, outcome)
	if outcome.Account != nil {
		t.Fatalf("commands created an account where nothing was measured: %+v", outcome.Account)
	}
}

// A CHECK THAT WAS ALREADY RED IS NOT THIS WORK'S FAILURE, and one this run
// turned red is. Both halves of that sentence are what [Account.Verified] is
// asked, and a reader that got the first one wrong is the measured way correct
// work gets thrown away.
func TestTheAccountTellsAPreExistingRedFromOneThisRunCaused(t *testing.T) {
	workspace := accountWorkspace(t, t.TempDir())
	task := Task{Goal: t.Name(), NodeKey: "task-1"}
	roster := []string{"suite > alpha", "suite > beta"}

	for _, probe := range []struct {
		name    string
		failing []string
		known   bool
		settled bool
	}{
		{"already red before the work", []string{"suite > alpha"}, true, true},
		{"turned red by the work", []string{"suite > beta"}, false, false},
	} {
		t.Run(probe.name, func(t *testing.T) {
			outcome := &Outcome{Verification: verify.Reading{
				Taken: true, AfterTaken: true,
				Before: verify.Result{
					Strategy: verify.Strategy{Command: "./suite.sh"},
					Reported: roster, Failing: []string{"suite > alpha"},
				},
				After: verify.Result{
					Strategy: verify.Strategy{Command: "./suite.sh"},
					Reported: roster, Failing: probe.failing,
				},
			}}
			AccountFor(context.Background(), workspace, task, "", true, outcome)

			account := outcome.Account
			if account == nil || len(account.Checks) != 1 {
				t.Fatalf("the reading did not reach the account: %+v", account)
			}
			if account.Checks[0].Command != "./suite.sh" {
				t.Errorf("the account names %q rather than the command that ran",
					account.Checks[0].Command)
			}
			if account.Checks[0].Known != probe.known {
				t.Errorf("Known = %v, want %v for a check %s",
					account.Checks[0].Known, probe.known, probe.name)
			}
			if account.Verified() != probe.settled {
				t.Errorf("Verified() = %v, want %v", account.Verified(), probe.settled)
			}
		})
	}
}

// AND KNOWN IS NEVER CLAIMED OFF A BASELINE NOBODY READ. A reading nobody took
// names nothing failing and a CUT one stops where the clock did, so a red suite
// measured against either is trivially "red in the same places" — and the
// account would answer Verified() true over a suite anyone can watch failing.
// Nobody looked is not everything passed.
func TestKnownIsNotClaimedOffABaselineNobodyRead(t *testing.T) {
	workspace := accountWorkspace(t, t.TempDir())
	task := Task{Goal: t.Name(), NodeKey: "task-1"}
	red := []string{"suite > alpha"}

	for _, probe := range []struct {
		name    string
		reading verify.Reading
	}{
		{"nobody took the baseline", verify.Reading{AfterTaken: true}},
		{"the baseline was cut at its ceiling", verify.Reading{Taken: true, AfterTaken: true, Partial: true}},
		{"the baseline never collected a check", verify.Reading{Taken: true, AfterTaken: true,
			Before: verify.Result{Uncollected: true}}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			reading := probe.reading
			reading.Before.Strategy = verify.Strategy{Command: "./suite.sh"}
			reading.Before.Reported, reading.Before.Failing = red, red
			reading.After = verify.Result{
				Strategy: verify.Strategy{Command: "./suite.sh"}, Reported: red, Failing: red,
			}
			outcome := &Outcome{Verification: reading}
			AccountFor(context.Background(), workspace, task, "", true, outcome)

			account := outcome.Account
			if account == nil || len(account.Checks) != 1 {
				t.Fatalf("the reading did not reach the account: %+v", account)
			}
			if account.Checks[0].Known {
				t.Error("a red suite was acquitted against a baseline that was never read")
			}
			if account.Verified() {
				t.Error("Verified() answered true where nobody looked")
			}
		})
	}
}

// A PATCH TOO LARGE TO WRITE OUT IS CLIPPED AND SAYS SO. A reader who cannot see
// the cut concludes the change set ends where the bytes end, which is a false
// account of the work rather than a smaller one.
func TestAPatchTooLargeToWriteOutSaysWhereItWasCut(t *testing.T) {
	root := gitTree(t)
	workspace := accountWorkspace(t, root)
	task := Task{Goal: t.Name(), NodeKey: "task-1"}

	outcome := landFakeLeaf(t, workspace, task, func() {
		writeFile(t, root, "wide.txt", strings.Repeat("a line of the leaf's own work\n", 60000))
	})

	if outcome.Account == nil || outcome.Account.Patch == "" {
		t.Fatalf("a leaf that wrote a large file left no patch: %+v", outcome.Account)
	}
	patch, err := os.ReadFile(outcome.Account.Patch)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch) > accountPatchBytes+512 {
		t.Errorf("the patch is %d bytes against a bound of %d", len(patch), accountPatchBytes)
	}
	if !strings.Contains(string(patch), "clipped here at") {
		t.Error("the patch was cut and reads as though it were whole")
	}
}

// AND A COMMAND THAT NAMED NO CHECK IS NOT A GREEN SUITE. It is an incomplete
// observation, and letting it through would let Verified() answer yes off a
// roster of nothing — which is exactly the silence Verified exists to stop
// collapsing.
func TestAReadingThatNamedNoCheckIsNotAnAccountOfOne(t *testing.T) {
	workspace := accountWorkspace(t, t.TempDir())
	outcome := &Outcome{Verification: verify.Reading{
		Taken: true, AfterTaken: true,
		After: verify.Result{Strategy: verify.Strategy{Command: "./suite.sh"}},
	}}
	AccountFor(context.Background(), workspace, Task{Goal: t.Name(), NodeKey: "task-1"}, "", true, outcome)
	if outcome.Account != nil {
		t.Fatalf("a reading that named nothing was written down as checks that passed: %+v",
			outcome.Account)
	}
}

// AN INHERITED ROSTER IS NOT THIS LEAF'S CLAIM TO HAVE CHECKED ANYTHING.
//
// Since #460 a leaf that changed nothing in a tree its job had not moved keeps
// the reading it is holding rather than running the suite again over identical
// bytes, and the photograph stands that reading as the reading of the finished
// tree — correctly, because it is a fact about the tree. The account is a
// different question: what did THIS leaf observe about its own work. Read as the
// second, a leaf that ran no test at all came back Verified, and revision's
// receipt would tell a person it was "checked by tests" — the exact collapse
// [Account.Verified]'s own doc forbids, with a person-facing sentence on top.
//
// Both halves are asserted here together, because the defect only exists in the
// space between them: the roster must still stand, AND the account must claim
// nothing.
func TestAnInheritedRosterIsNotAClaimToHaveCheckedAnything(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	workspace, log := countingSuite(t)
	ctx := context.Background()
	task := Task{Goal: "Run the suite and report its last line. Change no files.", NodeKey: "task-1"}

	opening := PhotographBefore(ctx, workspace, nil, time.Hour, task)
	if !opening.Reading.Taken {
		t.Fatalf("the fixture's own suite was not read: %q", opening.Reading.Unread)
	}
	workspace.WatchTree(task.leafKey())
	workspace.RecordChanges(task.leafKey())
	outcome := &Outcome{Stop: StopDone, Text: "the last line was: ok 1 - a check"}
	// changed is false and the job has moved nothing, which is the whole case.
	PhotographAfter(ctx, workspace, nil, time.Hour, task, opening, false, outcome)

	// #460's half: the roster stands and the suite was not run a second time.
	if !outcome.Verification.AfterTaken {
		t.Fatal("an unchanged tree was left with no reading of it at all")
	}
	if count := readings(t, log); count != 1 {
		t.Fatalf("the suite ran %d times over a tree nothing changed, want 1", count)
	}
	// And this change's half: the leaf claims none of it as its own checking.
	if !outcome.Account.Empty() {
		t.Fatalf("a leaf that ran no check claimed one: %+v", outcome.Account)
	}
	if outcome.Account.Verified() {
		t.Fatal("a leaf that ran no test at all answers Verified — which is what puts " +
			"\"checked by tests\" in front of a person about work nobody checked")
	}

	// AND A LEAF THAT DID BUY ITS OWN READING KEEPS ITS CHECKS. The law is about
	// a roster nobody re-ran, and nothing else.
	second := PhotographBefore(ctx, workspace, nil, time.Hour, task)
	workspace.WatchTree(task.leafKey())
	writeFile(t, workspace.Root(), "answer.md", "the last line was: ok 1 - a check\n")
	workspace.RecordChanges(task.leafKey())
	ran := &Outcome{Stop: StopDone, Text: "wrote the answer"}
	ran.Artifacts = workspace.Artifacts(task.leafKey())
	PhotographAfter(ctx, workspace, nil, time.Hour, task, second,
		leafMovedTheTree(workspace, task.leafKey()), ran)

	if ran.Account == nil || len(ran.Account.Checks) != 1 {
		t.Fatalf("a leaf that ran its own reading kept no account of it: %+v", ran.Account)
	}
	if !ran.Account.Verified() {
		t.Errorf("a leaf whose own green suite ran does not answer Verified: %+v",
			ran.Account.Checks)
	}
}

// ── the fixtures ─────────────────────────────────────────────────────────────

// landFakeLeaf drives one leaf through both seams the way linear.go does: the
// opening photograph, the tree watched, the work, the tree read again, the
// landing. There is no model and no network in it — what is under test is the
// seam, not what a worker would have written.
func landFakeLeaf(t *testing.T, workspace *Workspace, task Task, work func()) *Outcome {
	t.Helper()
	ctx := context.Background()
	opening := PhotographBefore(ctx, workspace, nil, time.Minute, task)
	workspace.WatchTree(task.leafKey())
	work()
	workspace.RecordChanges(task.leafKey())
	outcome := &Outcome{Stop: StopDone, Text: "done"}
	outcome.Artifacts = workspace.Artifacts(task.leafKey())
	PhotographAfter(ctx, workspace, nil, time.Minute, task, opening,
		len(outcome.Artifacts) > 0, outcome)
	return outcome
}

// accountWorkspace is a workspace whose machinery lands outside the root, which
// is what every surface in the product does — and what keeps the patch file out
// of the next diff.
func accountWorkspace(t *testing.T, root string) *Workspace {
	t.Helper()
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return workspace.WithScratch(t.TempDir())
}

// gitTree is a real repository with one commit in it, so a span has somewhere to
// be measured from.
func gitTree(t *testing.T) string {
	t.Helper()
	if _, err := osexec.LookPath("git"); err != nil {
		t.Skip("git is not on this machine")
	}
	root := t.TempDir()
	writeFile(t, root, "seed.txt", "seed\n")
	for _, argv := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "leaf@example.test"},
		{"config", "user.name", "leaf"},
		{"add", "seed.txt"},
		{"commit", "-q", "-m", "seed"},
	} {
		command := osexec.Command("git", append([]string{"-C", root}, argv...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", argv, err, out)
		}
	}
	return root
}

func writeFile(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// named reports that the account names this path at all, whatever it says
// happened to it.
func named(account *Account, path string) bool {
	for _, file := range account.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

// parseExecFile reads one of this package's own sources. The tests above are
// about the shape of the code, so the code is what they read.
func parseExecFile(t *testing.T, name string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}

// forEachExecFile walks every non-test source in this package.
func forEachExecFile(t *testing.T, visit func(name string, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		visit(name, file, fset)
	}
}

// findFunc is one top-level function by name.
func findFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok &&
			function.Recv == nil && function.Name.Name == name {
			return function
		}
	}
	return nil
}

// isOutcomeType reports that this type expression names this package's Outcome.
// pointers says whether *Outcome counts too: it does where the question is what
// a function hands back, and it does not where the question is what creates one,
// since a *Outcome is a pointer to one somebody else made.
func isOutcomeType(node ast.Expr, pointers bool) bool {
	if star, ok := node.(*ast.StarExpr); ok {
		return pointers && isOutcomeType(star.X, false)
	}
	named, ok := node.(*ast.Ident)
	return ok && named.Name == "Outcome"
}
