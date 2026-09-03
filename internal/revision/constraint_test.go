package revision

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// noWrites is the rule #427 was measured on, in the person's own words.
var noWrites = []plan.Constraint{{Text: "Change no files.", Kind: plan.ConstraintNoWrites}}

// constraintWorkspace is a workspace with the run's own machinery in it beside
// whatever the caller asked for, so every test here settles the same question
// the gate does: which of these paths is a change the PERSON forbade.
func constraintWorkspace(t *testing.T, written ...string) (root string, record []string) {
	t.Helper()
	root = t.TempDir()
	// The harness's own bookkeeping, which is not a deliverable and is not a
	// change anybody forbade. verify.SkipTree is the one list that says so.
	written = append(written, ".aforge/jobs/leaf.log")
	for _, name := range written {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("body\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		record = append(record, path)
	}
	return root, record
}

// THE RULE IS HELD AGAINST WHAT THE RUN LEFT BEHIND, AND NOTHING ELSE.
// #427, replayed: an errand told "Change no files." whose run wrote a test into
// an existing file and a shell script at the workspace root.
func TestNoWritesIsBrokenByAFileTheRunLeftBehind(t *testing.T) {
	root, record := constraintWorkspace(t,
		"internal/subharness/run_command_test.go", "check_runs_in_workspace.sh")

	broken := HoldConstraints(Evidence{Artifacts: record, Workspace: root, Constraints: noWrites})
	if len(broken) != 1 {
		t.Fatalf("the rule was not held: %+v", broken)
	}
	if !strings.HasPrefix(broken[0], "Change no files. — ") {
		t.Fatalf("the record does not lead with the person's own words: %q", broken[0])
	}
	for _, want := range []string{"check_runs_in_workspace.sh", "internal/subharness/run_command_test.go"} {
		if !strings.Contains(broken[0], want) {
			t.Fatalf("the record does not name %q: %q", want, broken[0])
		}
	}
	// The machinery is not a change the person forbade, and naming it would fail
	// every run of this shape whether it wrote anything or not.
	if strings.Contains(broken[0], ".aforge") {
		t.Fatalf("the harness's own bookkeeping was held against the run: %q", broken[0])
	}

	// AND A RUN THAT KEPT ITS WORD IS CLEAN. This is the half that matters most:
	// the first leaf of #427 did exactly what it was asked and must pass.
	clean := Evidence{Workspace: root, Constraints: noWrites,
		Artifacts: []string{filepath.Join(root, ".aforge/jobs/leaf.log")}}
	if held := HoldConstraints(clean); len(held) != 0 {
		t.Fatalf("a run that changed nothing was failed: %+v", held)
	}
}

// A place the rule names is a place the rule allows, and everything else is
// evidence of breaking it. A named directory means everything beneath it,
// because that is what a person means when they write "only touch docs/".
func TestPathsOnlyIsBrokenOnlyOutsideThePlacesItNames(t *testing.T) {
	root, record := constraintWorkspace(t, "docs/notes.md", "docs/deep/more.md")
	rule := []plan.Constraint{{Text: "Only change files under docs/.",
		Kind: plan.ConstraintPathsOnly, Paths: []string{"docs"}}}

	if held := HoldConstraints(Evidence{Artifacts: record, Workspace: root, Constraints: rule}); len(held) != 0 {
		t.Fatalf("work inside the place the rule allows was failed: %+v", held)
	}

	outsideRoot, outsideRecord := constraintWorkspace(t, "docs/notes.md", "src/main.go")
	held := HoldConstraints(Evidence{Artifacts: outsideRecord, Workspace: outsideRoot, Constraints: rule})
	if len(held) != 1 {
		t.Fatalf("a file outside the named place did not break the rule: %+v", held)
	}
	if !strings.Contains(held[0], "src/main.go") || strings.Contains(held[0], "docs/notes.md") {
		t.Fatalf("the record names the wrong files: %q", held[0])
	}

	// A rule no arithmetic can settle is never settled by arithmetic. It is
	// shown to the leaf and to the judge, and that is all.
	other := []plan.Constraint{{Text: "Do not use the network.", Kind: plan.ConstraintOther}}
	if held := HoldConstraints(Evidence{Artifacts: outsideRecord, Workspace: outsideRoot,
		Constraints: other}); len(held) != 0 {
		t.Fatalf("a rule for a reader was settled mechanically: %+v", held)
	}
}

// A BROKEN RULE IS SETTLED BEFORE A JUDGE IS PAID, and it outranks every other
// reading of the work. The judge here would answer PASS on the substance — the
// command was run and the line was reported — and the delivery still fails,
// because the run did the one thing it was told not to do.
func TestABrokenRuleIsSettledBeforeAnyJudgeIsPaid(t *testing.T) {
	root, record := constraintWorkspace(t, "internal/subharness/run_command_test.go")
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{`{"pass":true,"exercised":true}`}}
	node := store.Node{ID: "task-1", Brief: "run it and report the line",
		Provenance: store.Provenance{Intent: constraintErrandIntent}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, node,
		"ok — the final line was: ok  github.com/x/y  0.4s", "",
		Evidence{Artifacts: record, Workspace: root, Observed: true, Constraints: noWrites},
		"worker/model")

	if len(judge.sent) != 0 {
		t.Fatalf("a model was paid to settle a rule arithmetic had already settled: %d calls", len(judge.sent))
	}
	if judgment.Pass || !judgment.Checked {
		t.Fatalf("the delivery was not failed on the rule: %+v", judgment)
	}
	if judgment.Finding != FindingConstraint {
		t.Fatalf("the finding is not comparable across rounds: %q", judgment.Finding)
	}
	if len(judgment.Constraint) != 1 || !strings.Contains(judgment.Constraint[0], "run_command_test.go") {
		t.Fatalf("the record does not name what was changed: %+v", judgment.Constraint)
	}
	// The quote is the person's own sentence by construction, which is what
	// satisfies the grounding invariant every other finding is weighed against.
	if judgment.Quote != "Change no files." || !judgment.Sourced || !judgment.Mechanical {
		t.Fatalf("the finding is not grounded in the person's words: %+v", judgment)
	}
	if !strings.Contains(judgment.Gaps, `"Change no files."`) ||
		!strings.Contains(judgment.Gaps, "1 file") {
		t.Fatalf("the sentence a person reads does not quote the rule:\n%s", judgment.Gaps)
	}
	// And a delivery carrying one is not whole, however the rest of it read.
	if (store.DeliveryGate{Pass: false, Gap: judgment.Gaps, Constraint: judgment.Constraint}).Whole() {
		t.Fatal("a delivery that broke a rule settled as whole")
	}
}

// constraintErrandIntent is #427's request, verbatim, so the fixture holds the
// person's actual words rather than a paraphrase of them.
const constraintErrandIntent = "Run the command 'go test ./internal/subharness/ -count=1' in this " +
	"workspace and report the final line it prints. Change no files."

// A BROKEN CONSTRAINT NEVER BUYS A ROUND. Every other finding here is something
// more work could close; this one says the work did what the person forbade, and
// a remainder planned to close it is one more worker in the same workspace with
// the same permission the last one abused.
func TestABrokenRuleBuysNoRound(t *testing.T) {
	node := store.Node{ID: "task-1", Provenance: store.Provenance{Intent: constraintErrandIntent}}
	unmet := Judgment{Pass: false, Checked: true, Sourced: true, Mechanical: true,
		Finding: FindingConstraint, Quote: "Change no files.",
		Citations:  []string{"Change no files."},
		Constraint: []string{"Change no files. — internal/subharness/run_command_test.go"},
		Gaps:       `The work broke a rule the person set: "Change no files." (1 file).`}

	planned := 0
	extension := ExtendForGap(context.Background(), &store.Store{}, node, "the line was reported",
		unmet, nil, 10,
		func(context.Context, string, string) (store.Subtree, error) {
			planned++
			return store.Subtree{}, nil
		})

	if planned != 0 {
		t.Fatalf("a remainder was planned for a broken rule: %d calls", planned)
	}
	if extension.Spliced != 0 || !extension.Unclosed {
		t.Fatalf("the gap did not stay open: %+v", extension)
	}
	if !strings.Contains(extension.Refused, "broke a rule the person set") {
		t.Fatalf("the refusal does not say what it refused: %q", extension.Refused)
	}
}

// A RUN TOLD TO CHANGE NOTHING THAT CHANGED NOTHING HAS NOT FAILED TO MOVE.
// The standstill rule is written for runs that were supposed to move; #427's
// stream said "not repaired: the repair rewrote the account and changed nothing
// on disk" over a run whose whole contract was to change nothing.
func TestARunToldToChangeNothingIsNotUnrepairedForChangingNothing(t *testing.T) {
	rejudged := Judgment{Pass: true, Checked: true}
	// A finding the world is the ground of, which is exactly what the unmoved
	// rule normally refuses a rewritten account for.
	finding := Judgment{Pass: false, Checked: true, Sourced: true,
		Gaps: "the final line reported is not the one the command printed"}
	told := Evidence{Observed: true, Constraints: noWrites}

	if !RepairClosed(rejudged, finding, told, true) {
		t.Fatal("a run keeping the rule it was given was read as a repair that did nothing")
	}
	// And the rule stays exactly where it was for every other run: a job nobody
	// forbade to write is still held to having moved something.
	free := Evidence{Observed: true}
	if RepairClosed(rejudged, finding, free, true) {
		t.Fatal("an ordinary repair that moved nothing closed a finding about the world")
	}
}

// The rules a reader has to settle are shown to that reader, and the ones
// arithmetic already settled are not — a judge shown a rule this gate has just
// cleared is a judge invited to convict on an answered question.
func TestOnlyTheRulesAReaderMustSettleReachTheJudge(t *testing.T) {
	block := ConstraintsBlock([]plan.Constraint{
		{Text: "Change no files.", Kind: plan.ConstraintNoWrites},
		{Text: "Do not use the network.", Kind: plan.ConstraintOther},
	})
	if !strings.Contains(block, "Do not use the network.") {
		t.Fatalf("the rule a reader must settle is not in front of them:\n%s", block)
	}
	if strings.Contains(block, "Change no files.") {
		t.Fatalf("a rule already settled mechanically was put back to the judge:\n%s", block)
	}
	if ConstraintsBlock(nil) != "" {
		t.Fatal("a job with no rules sent a heading with nothing under it")
	}
}

// A PATH THE RECORD HOLDS IS A CHANGE WHETHER OR NOT IT IS STILL THERE.
//
// This door is the opposite of every other reading in this package: elsewhere a
// missing file convicts a claim that it was written, and here the question is
// whether the run touched the tree at all. A run told to change nothing that
// DELETED a file changed the tree in the loudest way there is, and a dangling
// symlink is the same event wearing a stat error — both used to walk straight
// through, because the reading dropped anything it could not stat.
func TestARuleIsBrokenByAFileTheRunDeletedOrLeftDangling(t *testing.T) {
	root := t.TempDir()
	removed := filepath.Join(root, "docs/notes.md")
	if err := os.MkdirAll(filepath.Dir(removed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(removed, []byte("was here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Join(root, "gone.txt"), dangling); err != nil {
		t.Skipf("this filesystem does not make symlinks: %v", err)
	}

	broken := HoldConstraints(Evidence{Artifacts: []string{removed, dangling},
		Workspace: root, Constraints: noWrites})
	if len(broken) != 1 {
		t.Fatalf("a deleted file and a dangling link did not break the rule: %+v", broken)
	}
	for _, want := range []string{"docs/notes.md", "link.txt"} {
		if !strings.Contains(broken[0], want) {
			t.Fatalf("the record does not name %q: %q", want, broken[0])
		}
	}
	// And a directory is still not a file the run wrote: it is where files live.
	if held := HoldConstraints(Evidence{Artifacts: []string{filepath.Join(root, "docs")},
		Workspace: root, Constraints: noWrites}); len(held) != 0 {
		t.Fatalf("a directory was held against the run: %+v", held)
	}
}

// A NAME IS NOT A DIRECTION. `..config` at the root of somebody's tree is a file
// they keep there, and the containment test used to read its first two
// characters as a climb out of the workspace — so a run told to change no files
// could quietly change any dotfile whose name began with two dots.
func TestARootLevelDotDotNameIsInsideTheWorkspace(t *testing.T) {
	root, record := constraintWorkspace(t, "..config")

	broken := HoldConstraints(Evidence{Artifacts: record, Workspace: root, Constraints: noWrites})
	if len(broken) != 1 || !strings.Contains(broken[0], "..config") {
		t.Fatalf("a file at the root of the workspace was read as outside it: %+v", broken)
	}
	// And a path that really does climb out is still outside. The scratch
	// directory the harness writes its own traces into is exactly that by
	// construction, and holding it against the run would fail every job of this
	// shape whether it wrote anything or not.
	outside := filepath.Join(filepath.Dir(root), "elsewhere.txt")
	if err := os.WriteFile(outside, []byte("machinery\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if held := HoldConstraints(Evidence{Artifacts: []string{outside}, Workspace: root,
		Constraints: noWrites}); len(held) != 0 {
		t.Fatalf("a path outside the workspace was held against the run: %+v", held)
	}
}

// A RULE THE JUDGE CONVICTS ON IS A BROKEN RULE, AND BUYS WHAT A BROKEN RULE
// BUYS, WHICH IS NOTHING. The rules no arithmetic can settle are shown to the
// judge as the standard beside the request, and a verdict that quotes one was
// right about the finding with no way to say what kind of finding it is — so it
// bought the repair round and the remainder like any other gap.
func TestAJudgeThatQuotesAStatedRuleRaisesTheRuleFinding(t *testing.T) {
	rule := plan.Constraint{Text: "Do not use the network.", Kind: plan.ConstraintOther}
	settings := config.Config{Model: "worker/model"}
	judge := &recordingJudge{replies: []string{
		`{"pass":false,"gaps":"The work fetched the figures over HTTP.","quote":"Do not use the network."}`}}
	node := store.Node{ID: "task-1", Brief: "total the figures",
		Provenance: store.Provenance{Intent: "Total the figures in the file. Do not use the network."}}

	judgment := JudgeDeliverable(context.Background(), settings,
		pool.Adopt(settings, judge.Model(), judge), nil, node,
		"I fetched the current figures and totalled them.", "",
		Evidence{Observed: true, Constraints: []plan.Constraint{rule}}, "worker/model")

	if judgment.Pass {
		t.Fatalf("the judge's refusal was lost: %+v", judgment)
	}
	if judgment.Finding != FindingConstraint {
		t.Fatalf("a refusal quoting a stated rule is not a rule finding: %q", judgment.Finding)
	}
	if len(judgment.Constraint) != 1 || judgment.Constraint[0] != rule.Text {
		t.Fatalf("the rule is not on the record: %+v", judgment.Constraint)
	}
	// The containment runs one way. A quote that CONTAINS a rule is a longer span
	// of the request that happens to have one inside it, and reading that as a
	// broken rule would refuse rounds for gaps that are nothing of the sort.
	wider := ConstraintQuoted(Judgment{Pass: false,
		Quote: "Total the figures in the file. Do not use the network."}, []plan.Constraint{rule})
	if len(wider.Constraint) != 0 {
		t.Fatalf("a span that swallows a rule was read as the rule: %+v", wider.Constraint)
	}
	// And a mechanical rule is never matched here: it was held above and found
	// KEPT, which is why there was a model round at all.
	settled := ConstraintQuoted(Judgment{Pass: false, Quote: "Change no files."}, noWrites)
	if len(settled.Constraint) != 0 {
		t.Fatalf("a rule this gate already cleared was convicted on: %+v", settled.Constraint)
	}
}

// A REPAIR ACCEPTED WITHOUT A SECOND GATE IS STILL UNDER THE PERSON'S RULES.
//
// The quorum round is the one repair whose result is taken on trust: it commits
// unconditionally and nothing re-judges it. So the record it leaves behind is
// re-read against the rules by hand, and what comes back rejoins the ordinary
// failed path — a verdict that buys no round, exactly as the first gate's would
// have. This pins the two halves that wiring depends on together.
func TestARepairAcceptedWithoutASecondGateIsStillHeldToTheRules(t *testing.T) {
	root, record := constraintWorkspace(t, "quorum_fix_test.go")
	held, broke := ConstraintsHeld(Evidence{Artifacts: record, Workspace: root,
		Observed: true, Constraints: noWrites})
	if !broke || held.Pass || held.Finding != FindingConstraint {
		t.Fatalf("the round's own record did not raise the rule: %+v", held)
	}

	node := store.Node{ID: "task-1", Provenance: store.Provenance{Intent: constraintErrandIntent}}
	planned := 0
	extension := ExtendForGap(context.Background(), &store.Store{}, node, "the line was reported",
		held, nil, 10, func(context.Context, string, string) (store.Subtree, error) {
			planned++
			return store.Subtree{}, nil
		})
	if planned != 0 || extension.Spliced != 0 || !extension.Unclosed {
		t.Fatalf("a rule broken by an unjudged repair still bought work: %+v", extension)
	}
}
