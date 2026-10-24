package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── add_step, remove_step and move_step ─────────────────────────────────────
//
// editBase's issue pipeline is [plan approve write test lint-pass proof], its pr
// pipeline [read review proof], and ci is the shipped default [bisect fix test
// proof approve]. approve and proof are fixed.

func add(kind, name, after, prompt string) EditOp {
	return EditOp{Op: OpAddStep, Pipeline: kind, Name: name, After: s(after), Prompt: s(prompt)}
}

func remove(kind, name string) EditOp {
	return EditOp{Op: OpRemoveStep, Pipeline: kind, Name: name}
}

func move(kind, name, after string) EditOp {
	return EditOp{Op: OpMoveStep, Pipeline: kind, Name: name, After: s(after)}
}

func reread(t *testing.T, text string) File {
	t.Helper()
	back, err := Parse(text)
	if err != nil || len(back.Problems) > 0 {
		t.Fatalf("the preview does not read back: %v %v", err, back.Problems)
	}
	return back
}

func joined(names []string) string { return strings.Join(names, ",") }

func wantCode(t *testing.T, e *EditError, code string, op int) {
	t.Helper()
	if e.Code != code || e.Op != op {
		t.Fatalf("got %s at %d (%s), want %s at %d", e.Code, e.Op, e.Why, code, op)
	}
}

func TestAddStepInsertsAfterAnchorAndRoundTrips(t *testing.T) {
	got := mustApply(t, editBase, add("issue", "lint", "write", "run the linter on the diff"))
	if !got.Changed || !got.Structural {
		t.Fatalf("%+v", got)
	}
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindIssue]); g != "plan,approve,write,lint,test,lint-pass,proof" {
		t.Fatalf("issue %s", g)
	}
	if g := joined(back.Pipeline[factory.KindPR]); g != "read,review,proof" {
		t.Fatalf("another kind moved: %s", g)
	}
	if len(got.Order) != 1 || got.Order[0].Pipeline != "issue" ||
		joined(got.Order[0].Before) != "plan,approve,write,test,lint-pass,proof" ||
		joined(got.Order[0].After) != "plan,approve,write,lint,test,lint-pass,proof" {
		t.Fatalf("order %+v", got.Order)
	}
	if !reflect.DeepEqual(got.Added, []string{"lint"}) || len(got.Steps) != 0 || len(got.Gates) != 0 {
		t.Fatalf("added %v steps %+v gates %+v", got.Added, got.Steps, got.Gates)
	}
	var lint *factory.Stage
	for i, st := range back.Recipe().Stages {
		if st.Name == "lint" {
			lint = &back.Recipe().Stages[i]
		}
	}
	if lint == nil || lint.Kind != factory.StageChat || lint.Ask != "run the linter on the diff" {
		t.Fatalf("the runner does not run the new step as a chat: %+v", back.Recipe().Stages)
	}
	// D1: the existing canonical encoder still rewrites the file and drops its
	// comments, and the preview says so.
	if !got.CommentsDropped || !got.FormatRewritten || strings.Contains(got.Text, "#") {
		t.Fatalf("comments=%v format=%v", got.CommentsDropped, got.FormatRewritten)
	}
}

func TestAddStepFirstAndWithEveryField(t *testing.T) {
	op := add("pr", "triage", "", "sort the thread")
	op.Until, op.Max = s("the thread is sorted"), i(2)
	op.When, op.Effort, op.Fanout = s("thin"), s("cheap"), s("per-file")
	got := mustApply(t, editBase, op)
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindPR]); g != "triage,read,review,proof" {
		t.Fatalf("pr %s", g)
	}
	st := back.Steps["triage"]
	if st.Prompt != "sort the thread" || st.Until != "the thread is sorted" || st.Max != 2 || st.When != "thin" || st.Effort != "cheap" || st.Fanout != "per-file" || st.Kind != "" {
		t.Fatalf("%+v", st)
	}
	// Adding before every gate of pr changes what runs unattended there: nothing.
	if len(got.Gates) != 0 {
		t.Fatalf("pr has no gate: %+v", got.Gates)
	}
}

func TestStructureWorksOnTheDefaultAMinimalFileAndACustomKind(t *testing.T) {
	for name, c := range map[string]struct {
		base, kind, after, want string
	}{
		"default ci":   {editBase, "ci", "fix", "bisect,fix,bench,test,proof,approve"},
		"minimal file": {"default_budget: 3\n", "issue", "write", "plan,approve,write,bench,test,review,proof"},
		"custom kind":  {"pipeline:\n  docs: [read, write, proof]\n", "docs", "write", "read,write,bench,proof"},
	} {
		t.Run(name, func(t *testing.T) {
			got := mustApply(t, c.base, add(c.kind, "bench", c.after, "time the hot path"))
			back := reread(t, got.Text)
			if g := joined(back.Pipeline[factory.Kind(c.kind)]); g != c.want {
				t.Fatalf("%s", g)
			}
			counts := map[string]int{}
			for _, k := range got.Counts {
				counts[k.Pipeline] = k.Steps
			}
			for kind, names := range back.Pipeline {
				if counts[string(kind)] != len(names) {
					t.Fatalf("count of %s is %d, the file has %d", kind, counts[string(kind)], len(names))
				}
			}
			if len(counts) != len(back.Pipeline) {
				t.Fatalf("counts %+v for %d kinds", got.Counts, len(back.Pipeline))
			}
		})
	}
}

func TestAddStepNameTheRunnerWouldDropIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		op   EditOp
		code string
	}{
		"over twelve":     {add("issue", "lint-pass-checker-x", "write", "x"), EditBadValue},
		"same stage":      {add("issue", "lintpass", "write", "x"), EditBadValue},
		"upper case":      {add("issue", "Lint", "write", "x"), EditUnknownStep},
		"spaces":          {add("issue", "li nt", "write", "x"), EditUnknownStep},
		"approve":         {add("issue", "approve", "write", "x"), EditLocked},
		"approve spelled": {add("issue", "approve-2", "write", "x"), EditBadValue},
		"already listed":  {add("issue", "test", "write", "run the tests"), EditBadValue},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, refusal(t, editBase, EditOp{Op: OpSetBudget, Value: f(6)}, c.op), c.code, 1)
		})
	}
}

func TestAddStepReusingADefinitionIsListOnlyAndNeverRewritesIt(t *testing.T) {
	// The same words: review joins ci and its definition is not touched.
	got := mustApply(t, editBase, add("ci", "review", "test", "review the diff"))
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindCI]); g != "bisect,fix,test,review,proof,approve" || len(got.Added) != 0 || len(got.Steps) != 0 {
		t.Fatalf("%s added=%v steps=%+v", g, got.Added, got.Steps)
	}
	if back.Steps["review"].Until != "clean" {
		t.Fatalf("%+v", back.Steps["review"])
	}
	// Other words: refused, whether pr uses it or nobody does (a shipped step).
	wantCode(t, refusal(t, editBase, add("ci", "review", "test", "review it my way")), EditBadValue, 0)
	wantCode(t, refusal(t, editBase, add("issue", "security", "write", "look for leaks")), EditBadValue, 0)
	withUntil := add("ci", "review", "test", "review the diff")
	withUntil.Until = s("no findings")
	wantCode(t, refusal(t, editBase, withUntil), EditBadValue, 0)
	// A shipped step under its shipped words is reused.
	got = mustApply(t, editBase, add("issue", "security", "test", "review for injection, authz, secrets handling"))
	if len(got.Added) != 0 || !strings.Contains(joined(reread(t, got.Text).Pipeline[factory.KindIssue]), "security") {
		t.Fatalf("%+v", got)
	}
}

func TestAddStepNeverAddsALockedDefinition(t *testing.T) {
	base := `pipeline:
  issue: [plan, write, proof]
steps:
  deploy:
    kind: check
    prompt: make deploy
  signoff:
    kind: gate
  audit:
    prompt: audit it
fixed: [approve, proof, audit]
`
	for i, op := range []EditOp{
		add("issue", "deploy", "write", "make deploy"),
		add("issue", "signoff", "write", "x"),
		add("issue", "audit", "write", "audit it"),
	} {
		wantCode(t, refusal(t, base, op), EditLocked, 0)
		_ = i
	}
}

func TestNinthStepIsAllowedAndTenthIsRefusedNotTruncated(t *testing.T) {
	base := "pipeline:\n  issue: [plan, approve, write, test, review, proof]\n"
	got := mustApply(t, base, add("issue", "one", "write", "1"), add("issue", "two", "one", "2"), add("issue", "three", "two", "3"))
	if n := len(reread(t, got.Text).Pipeline[factory.KindIssue]); n != factory.StageMost {
		t.Fatalf("%d steps", n)
	}
	wantCode(t, refusal(t, base, add("issue", "one", "write", "1"), add("issue", "two", "one", "2"),
		add("issue", "three", "two", "3"), add("issue", "four", "three", "4")), EditTooLarge, 3)
}

func TestRemoveStepKeepsDefinitionAndOtherKinds(t *testing.T) {
	got := mustApply(t, editBase, remove("pr", "review"))
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindPR]); g != "read,proof" {
		t.Fatalf("pr %s", g)
	}
	if back.Steps["review"].Prompt != "review the diff" || !strings.Contains(got.Text, "review the diff") {
		t.Fatalf("the definition went with the row: %+v", back.Steps["review"])
	}
	if g := joined(back.Pipeline[factory.KindIssue]); g != "plan,approve,write,test,lint-pass,proof" {
		t.Fatalf("issue %s", g)
	}
	if len(got.Order) != 1 || got.Order[0].Pipeline != "pr" || len(got.Added) != 0 || len(got.Steps) != 0 {
		t.Fatalf("%+v", got)
	}
	// A pipeline is never emptied: an empty kind runs the issue steps instead.
	wantCode(t, refusal(t, "pipeline:\n  docs: [read]\n", remove("docs", "read")), EditBadValue, 0)
}

func TestRemoveOrMoveLockedStepRefused(t *testing.T) {
	base := strings.Replace(editBase, "  lint-pass:\n    prompt: run the linter\n", "  lint-pass:\n    kind: check\n    prompt: make lint\n", 1)
	if _, problems, ok := Inspect(base); !ok {
		t.Fatal(problems)
	}
	for _, name := range []string{"approve", "proof", "lint-pass"} {
		wantCode(t, refusal(t, base, remove("issue", name)), EditLocked, 0)
		wantCode(t, refusal(t, base, move("issue", name, "plan")), EditLocked, 0)
	}
}

func TestMoveAcrossAGateIsAllowedAndSaysWhatNowRunsUnattended(t *testing.T) {
	got := mustApply(t, editBase, move("issue", "write", "plan"))
	if g := joined(reread(t, got.Text).Pipeline[factory.KindIssue]); g != "plan,write,approve,test,lint-pass,proof" {
		t.Fatalf("%s", g)
	}
	want := []GateChange{{Pipeline: "issue", Gate: "approve", Before: []string{"plan"}, After: []string{"plan", "write"}}}
	if !reflect.DeepEqual(got.Gates, want) {
		t.Fatalf("gates %+v", got.Gates)
	}
	// Removing the only step before a person, and adding one, say so too.
	got = mustApply(t, editBase, remove("issue", "plan"))
	if !reflect.DeepEqual(got.Gates, []GateChange{{Pipeline: "issue", Gate: "approve", Before: []string{"plan"}, After: []string{}}}) {
		t.Fatalf("gates %+v", got.Gates)
	}
	got = mustApply(t, editBase, add("issue", "scope", "", "size the issue"))
	if !reflect.DeepEqual(got.Gates, []GateChange{{Pipeline: "issue", Gate: "approve", Before: []string{"plan"}, After: []string{"scope", "plan"}}}) {
		t.Fatalf("gates %+v", got.Gates)
	}
	// A move on one side of the gate says nothing about it.
	got = mustApply(t, editBase, move("issue", "lint-pass", "write"))
	if g := joined(got.Order[0].After); g != "plan,approve,write,lint-pass,test,proof" || len(got.Gates) != 0 {
		t.Fatalf("%s gates %+v", g, got.Gates)
	}
}

func TestAGateByKindIsAGateToo(t *testing.T) {
	base := `pipeline:
  pr: [read, signoff, review, proof]
steps:
  signoff:
    kind: gate
`
	got := mustApply(t, base, move("pr", "review", "read"))
	want := []GateChange{{Pipeline: "pr", Gate: "signoff", Before: []string{"read"}, After: []string{"read", "review"}}}
	if !reflect.DeepEqual(got.Gates, want) {
		t.Fatalf("%+v", got.Gates)
	}
}

// The base has bench after proof, a shape only a hand edit makes, and lists
// lint-pass under fixed; moving bench past the fixed lint-pass to before proof
// is allowed, and is also how that shape is repaired.
func TestMovePastFixedNonGateAllowed(t *testing.T) {
	base := strings.Replace(editBase, "lint-pass, proof]", "lint-pass, proof, bench]", 1) + "  # trailing\n"
	base = strings.Replace(base, "steps:\n", "steps:\n  bench:\n    prompt: time it\n", 1)
	base = strings.Replace(base, "fixed: [approve, proof]", "fixed: [approve, lint-pass, proof]", 1)
	if !listed(reread(t, base).Fixed, "lint-pass") {
		t.Fatal("the fixture does not fix lint-pass")
	}
	got := mustApply(t, base, move("issue", "bench", "lint-pass"))
	back := reread(t, got.Text)
	if g := joined(back.Pipeline[factory.KindIssue]); g != "plan,approve,write,test,lint-pass,bench,proof" {
		t.Fatalf("%s", g)
	}
	if p := back.Steps["proof"]; p.Prompt != "show the evidence" || !listed(back.Fixed, "proof") || !listed(back.Fixed, "lint-pass") || len(got.Steps) != 0 || len(got.Gates) != 0 {
		t.Fatalf("%+v %+v", p, got)
	}
}

func TestStructuralNoOpIsUnchanged(t *testing.T) {
	got := mustApply(t, editBase, move("issue", "test", "write"))
	if got.Changed || got.Text != editBase || !got.Structural || len(got.Order) != 0 || len(got.Gates) != 0 || len(got.Added) != 0 {
		t.Fatalf("%+v", got)
	}
	if len(got.Counts) != 3 {
		t.Fatalf("an unchanged preview still counts every kind: %+v", got.Counts)
	}
}

func TestStructuralShapeIsCheckedBeforeTheFile(t *testing.T) {
	for name, op := range map[string]EditOp{
		"add no pipeline":       {Op: OpAddStep, Name: "lint", After: s("write"), Prompt: s("x")},
		"add no after":          {Op: OpAddStep, Pipeline: "issue", Name: "lint", Prompt: s("x")},
		"add no prompt":         {Op: OpAddStep, Pipeline: "issue", Name: "lint", After: s("write")},
		"add a kind":            {Op: OpAddStep, Pipeline: "issue", Name: "lint", After: s("write"), Prompt: s("x"), Kind: s("check")},
		"add chat kind":         {Op: OpAddStep, Pipeline: "issue", Name: "lint", After: s("write"), Prompt: s("x"), Kind: s("chat")},
		"add a value":           {Op: OpAddStep, Pipeline: "issue", Name: "lint", After: s("write"), Prompt: s("x"), Value: f(2)},
		"remove an anchor":      {Op: OpRemoveStep, Pipeline: "issue", Name: "test", After: s("write")},
		"remove a prompt":       {Op: OpRemoveStep, Pipeline: "issue", Name: "test", Prompt: s("x")},
		"move no after":         {Op: OpMoveStep, Pipeline: "issue", Name: "test"},
		"move a field":          {Op: OpMoveStep, Pipeline: "issue", Name: "test", After: s("plan"), Max: i(2)},
		"move after itself":     {Op: OpMoveStep, Pipeline: "issue", Name: "test", After: s("test")},
		"set_step a pipeline":   {Op: OpSetStep, Pipeline: "issue", Name: "test", Prompt: s("x")},
		"set_step an anchor":    {Op: OpSetStep, Name: "test", Prompt: s("x"), After: s("plan")},
		"set_budget a pipeline": {Op: OpSetBudget, Pipeline: "issue", Value: f(3)},
		"bad pipeline":          {Op: OpRemoveStep, Pipeline: "Issue", Name: "test"},
		"bad anchor":            {Op: OpMoveStep, Pipeline: "issue", Name: "test", After: s("../plan")},
		"loop":                  {Op: "add_loop", Pipeline: "issue", Name: "test"},
		"reorder":               {Op: "reorder", Pipeline: "issue", Name: "test", After: s("plan")},
	} {
		if _, bad := CheckEdit(0, op); bad == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStructuralRefusalRefusesTheWholeList(t *testing.T) {
	for name, c := range map[string]struct {
		ops  []EditOp
		code string
		at   int
	}{
		"stale anchor":         {[]EditOp{add("issue", "lint", "write", "x"), move("issue", "test", "gone")}, EditUnknownStep, 1},
		"stale name":           {[]EditOp{remove("issue", "review")}, EditUnknownStep, 0},
		"no such pipeline":     {[]EditOp{remove("deploy", "plan")}, EditUnknownStep, 0},
		"twice in one kind":    {[]EditOp{move("issue", "test", "plan"), move("issue", "test", "write")}, EditBadOp, 1},
		"set_step after add":   {[]EditOp{add("issue", "lint", "write", "x"), {Op: OpSetStep, Name: "lint", Max: i(2)}}, EditBadOp, 1},
		"set_step before add":  {[]EditOp{{Op: OpSetStep, Name: "write", Max: i(2)}, add("ci", "write", "fix", "carry out the plan")}, EditBadOp, 1},
		"locked after a valid": {[]EditOp{add("issue", "lint", "write", "x"), remove("issue", "approve")}, EditLocked, 1},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, refusal(t, editBase, c.ops...), c.code, c.at)
		})
	}
	// The same name in two kinds is two rows, and both apply.
	got := mustApply(t, editBase, remove("issue", "test"), remove("ci", "test"))
	back := reread(t, got.Text)
	if joined(back.Pipeline[factory.KindIssue]) != "plan,approve,write,lint-pass,proof" || joined(back.Pipeline[factory.KindCI]) != "bisect,fix,proof,approve" || len(got.Order) != 2 {
		t.Fatalf("%+v", got.Order)
	}
}

func TestStructureOnABaseWithAnUnknownKeyIsBaseInvalid(t *testing.T) {
	base := editBase + "loops:\n  - [write, test]\n"
	wantCode(t, refusal(t, base, add("issue", "lint", "write", "x")), EditBaseInvalid, -1)
	base = strings.Replace(editBase, "    max: 3\n", "    max: 3\n    model: gpt\n", 1)
	wantCode(t, refusal(t, base, move("issue", "test", "plan")), EditBaseInvalid, -1)
}

func TestSetStepPreviewCarriesNoStructureFacts(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetStep, Name: "write", Max: i(4)})
	if got.Structural || got.Order != nil || got.Added != nil || got.Counts != nil || got.Gates != nil {
		t.Fatalf("%+v", got)
	}
}

func TestNotRoundTripComparesPipelineOrder(t *testing.T) {
	want, _ := Parse(editBase)
	text, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	if why := notRoundTrip(text, want); why != "" {
		t.Fatal(why)
	}
	other := want
	other.Pipeline = map[factory.Kind][]string{}
	for k, v := range want.Pipeline {
		other.Pipeline[k] = append([]string(nil), v...)
	}
	other.Pipeline[factory.KindIssue] = []string{"plan", "write", "approve", "test", "lint-pass", "proof"}
	if why := notRoundTrip(text, other); why == "" {
		t.Fatal("a different order read back as the same file")
	}
}

// ── proof stays, and no chat runs after it ──────────────────────────────────
//
// Proof is what the runner calls proof ([factory.IsProofStage]): named proof,
// named prove*, or until proven. These tests read the committed file back
// through File.Recipe, so they judge the stages a job would run, not the list.

// strayInRecipe is the first chat stage of kind that runs after its first
// proof stage once the file is compiled, or "".
func strayInRecipe(t *testing.T, text string, kind factory.Kind) string {
	t.Helper()
	stages := reread(t, text).Recipe().For(kind)
	proof := -1
	for at, st := range stages {
		if proof < 0 && factory.IsProofStage(st) {
			proof = at
			continue
		}
		if proof >= 0 && st.Kind == factory.StageChat {
			return st.Name
		}
	}
	return ""
}

// proofAliases are bases whose issue pipeline is [plan, write, <proof>] and
// whose fixed list is empty, one for each way the runner reads a stage as proof.
var proofAliases = map[string]string{
	"proof":                          "fixed: []\npipeline:\n  issue: [plan, write, proof]\n",
	"prove":                          "fixed: []\npipeline:\n  issue: [plan, write, prove-it]\nsteps:\n  prove-it:\n    prompt: show it works\n",
	"until proven":                   "fixed: []\npipeline:\n  issue: [plan, write, audit]\nsteps:\n  audit:\n    prompt: check every claim\n    until: proven\n",
	"until every claim has evidence": "fixed: []\npipeline:\n  issue: [plan, write, audit]\nsteps:\n  audit:\n    prompt: check every claim\n    until: every claim has evidence\n",
}

func proofName(t *testing.T, base string) string {
	t.Helper()
	list := reread(t, base).Pipeline[factory.KindIssue]
	return list[len(list)-1]
}

func TestProofIsLockedWhateverFixedSays(t *testing.T) {
	for alias, base := range proofAliases {
		t.Run(alias, func(t *testing.T) {
			name := proofName(t, base)
			stages := reread(t, base).Recipe().Stages
			if !factory.IsProofStage(stages[len(stages)-1]) {
				t.Fatalf("the runner does not read %s as proof: %+v", name, stages)
			}
			for _, op := range []EditOp{remove("issue", name), move("issue", name, ""), move("issue", name, "plan")} {
				wantCode(t, refusal(t, base, op), EditLocked, 0)
			}
		})
	}
}

// untilAliases are the proofAliases whose row is proof only by until, so a
// set_step that changes until can end it.
var untilAliases = []string{"until proven", "until every claim has evidence"}

func untilDone(name string) EditOp {
	return EditOp{Op: OpSetStep, Name: name, Until: s("done")}
}

// hasProof says whether the committed file's issue pipeline runs any proof.
func hasProof(t *testing.T, text string) bool {
	t.Helper()
	for _, st := range reread(t, text).Recipe().For(factory.KindIssue) {
		if factory.IsProofStage(st) {
			return true
		}
	}
	return false
}

// A set_step that ends the proof of a row proof only by until may not ride a
// structural preview of that pipeline, in either order: the pipeline would
// land with no sheet. A set_step alone is a field edit and still may.
func TestASetStepCannotEndProofInsideAStructuralPreview(t *testing.T) {
	for _, alias := range untilAliases {
		base := proofAliases[alias]
		t.Run(alias, func(t *testing.T) {
			name := proofName(t, base)
			for label, c := range map[string]struct {
				ops []EditOp
				at  int
			}{
				"then add after it":   {[]EditOp{untilDone(name), add("issue", "deploy", name, "deploy it")}, 0},
				"then move work past": {[]EditOp{untilDone(name), move("issue", "write", name)}, 0},
				"then add before it":  {[]EditOp{untilDone(name), add("issue", "lint", "write", "lint it")}, 0},
				"then remove work":    {[]EditOp{untilDone(name), remove("issue", "plan")}, 0},
				"after an add":        {[]EditOp{add("issue", "lint", "write", "lint it"), untilDone(name)}, 1},
				"after a move":        {[]EditOp{move("issue", "write", ""), untilDone(name)}, 1},
			} {
				t.Run(label, func(t *testing.T) {
					e := refusal(t, base, c.ops...)
					wantCode(t, e, EditLocked, c.at)
					if !strings.Contains(e.Why, name+" would no longer be proof") {
						t.Fatalf("%s", e.Why)
					}
				})
			}
			// A new proof row placed before the de-proofed row leaves it as
			// chat work after proof, which is the stray rule's refusal.
			verify := add("issue", "verify", "write", "check it")
			verify.Until = s("proven")
			wantCode(t, refusal(t, base, untilDone(name), verify), EditLocked, 1)
		})
	}
}

// The refusal names the set_step that ended a later base proof row's proof,
// not the innocent add beside it, in either op order.
func TestASetStepThatDeProofsALaterProofRowIsNamed(t *testing.T) {
	base := "fixed: []\npipeline:\n  issue: [plan, write, proof, audit]\nsteps:\n  audit:\n    prompt: check every claim\n    until: proven\n"
	lint := add("issue", "lint", "plan", "lint it")
	for label, c := range map[string]struct {
		ops []EditOp
		at  int
	}{
		"set_step first": {[]EditOp{untilDone("audit"), lint}, 0},
		"add first":      {[]EditOp{lint, untilDone("audit")}, 1},
	} {
		t.Run(label, func(t *testing.T) {
			e := refusal(t, base, c.ops...)
			wantCode(t, e, EditLocked, c.at)
			if !strings.Contains(e.Why, "audit would run after proof") {
				t.Fatalf("%s", e.Why)
			}
		})
	}
}

// LEGACY PIN: a field-only preview is never judged by the structural law, so
// a set_step alone may still end a row's proof, as it could before.
func TestASetStepAloneMayStillEndProof(t *testing.T) {
	for _, alias := range untilAliases {
		base := proofAliases[alias]
		t.Run(alias, func(t *testing.T) {
			got := mustApply(t, base, untilDone(proofName(t, base)))
			if !got.Changed || got.Structural || hasProof(t, got.Text) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

// A structural preview that keeps the row proof, or keeps another proof row,
// is not refused for a set_step on it.
func TestAStructuralPreviewThatKeepsProofIsAllowed(t *testing.T) {
	for _, alias := range untilAliases {
		base := proofAliases[alias]
		t.Run(alias, func(t *testing.T) {
			name := proofName(t, base)
			reworded := EditOp{Op: OpSetStep, Name: name, Prompt: s("check every claim twice")}
			got := mustApply(t, base, reworded, add("issue", "lint", "write", "lint it"))
			if g := joined(reread(t, got.Text).Pipeline[factory.KindIssue]); g != "plan,write,lint,"+name || !hasProof(t, got.Text) {
				t.Fatalf("%s", g)
			}
			twin := base + "  prove-it:\n    prompt: show it works\n"
			twin = strings.Replace(twin, "write, "+name+"]", "write, "+name+", prove-it]", 1)
			got = mustApply(t, twin, untilDone(name), remove("issue", "plan"))
			if g := joined(reread(t, got.Text).Pipeline[factory.KindIssue]); g != "write,"+name+",prove-it" || !hasProof(t, got.Text) {
				t.Fatalf("%s", g)
			}
		})
	}
}

func TestNoChatIsAddedOrMovedAfterProof(t *testing.T) {
	const minimal = "default_budget: 3\n"
	const custom = "pipeline:\n  docs: [read, write, proof]\n"
	const approveLast = "pipeline:\n  issue: [plan, write, test, proof, approve]\n"
	const postLast = "pipeline:\n  issue: [plan, write, proof, ship]\nsteps:\n  ship:\n    prompt: pr draft\n    kind: post\n"
	for name, c := range map[string]struct {
		base string
		ops  []EditOp
		at   int
	}{
		"default add after proof":        {editBase, []EditOp{add("ci", "deploy", "proof", "deploy it")}, 0},
		"minimal add after proof":        {minimal, []EditOp{add("issue", "deploy", "proof", "deploy it")}, 0},
		"custom kind add after proof":    {custom, []EditOp{add("docs", "polish", "proof", "polish it")}, 0},
		"add after approve after proof":  {approveLast, []EditOp{add("issue", "polish", "approve", "polish it")}, 0},
		"add before a post after proof":  {postLast, []EditOp{add("issue", "hotfix", "proof", "fix it")}, 0},
		"minimal move write past proof":  {minimal, []EditOp{move("issue", "write", "proof")}, 0},
		"default move review past proof": {editBase, []EditOp{move("pr", "review", "proof")}, 0},
		"a second proof after proof":     {minimal, []EditOp{add("issue", "prove-more", "proof", "show more")}, 0},
		"a proof before the work":        {minimal, []EditOp{add("issue", "prove-first", "", "show it")}, 0},
		"after a valid edit":             {minimal, []EditOp{add("issue", "lint", "write", "lint it"), move("issue", "test", "proof")}, 1},
		"set_step makes plan proof":      {minimal, []EditOp{add("issue", "lint", "write", "lint it"), {Op: OpSetStep, Name: "plan", Until: s("proven")}}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, refusal(t, c.base, c.ops...), EditLocked, c.at)
		})
	}
	for alias, base := range proofAliases {
		t.Run("move write past "+alias, func(t *testing.T) {
			wantCode(t, refusal(t, base, move("issue", "write", proofName(t, base))), EditLocked, 0)
		})
		t.Run("add after "+alias, func(t *testing.T) {
			wantCode(t, refusal(t, base, add("issue", "deploy", proofName(t, base), "deploy it")), EditLocked, 0)
		})
	}
}

func TestARefusalAtTheProofBoundaryLeavesNothingBehind(t *testing.T) {
	before := reread(t, editBase).Recipe()
	got, err := ApplyEdits(editBase, []EditOp{add("issue", "lint", "write", "x"), add("issue", "deploy", "proof", "deploy it")})
	if err == nil || !reflect.DeepEqual(got, Edited{}) {
		t.Fatalf("%+v %v", got, err)
	}
	if after := reread(t, editBase).Recipe(); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused preview changed what the base reads as")
	}
	again := mustApply(t, editBase, add("issue", "lint", "write", "x"))
	if g := joined(reread(t, again.Text).Pipeline[factory.KindIssue]); g != "plan,approve,write,lint,test,lint-pass,proof" {
		t.Fatalf("the next preview saw the refused one: %s", g)
	}
}

func TestAStrayChatAfterProofIsRepairedNotKept(t *testing.T) {
	const stray = "pipeline:\n  issue: [plan, approve, write, proof, polish]\n  pr: [read, proof]\nsteps:\n  polish:\n    prompt: polish it\n"
	for name, ops := range map[string][]EditOp{
		"removed":                {remove("issue", "polish")},
		"moved before proof":     {move("issue", "polish", "write")},
		"repaired beside an add": {add("issue", "lint", "write", "lint it"), remove("issue", "polish")},
	} {
		t.Run(name, func(t *testing.T) {
			got := mustApply(t, stray, ops...)
			if s := strayInRecipe(t, got.Text, factory.KindIssue); s != "" {
				t.Fatalf("%s still runs after proof", s)
			}
		})
	}
	// An edit of that pipeline that keeps the stray row is refused at the
	// edit, naming the row; another pipeline is not judged by it.
	e := refusal(t, stray, add("issue", "lint", "write", "lint it"))
	wantCode(t, e, EditLocked, 0)
	if !strings.Contains(e.Why, "polish") {
		t.Fatalf("%s", e.Why)
	}
	wantCode(t, refusal(t, stray, move("issue", "proof", "plan"), remove("issue", "polish")), EditLocked, 0)
	got := mustApply(t, stray, add("pr", "triage", "", "sort the thread"))
	if joined(reread(t, got.Text).Pipeline[factory.KindIssue]) != "plan,approve,write,proof,polish" {
		t.Fatal("an untouched pipeline changed")
	}
}

func TestWhatStandsAfterProofKeepsItsLocks(t *testing.T) {
	const base = `fixed: [approve, proof, notes]
pipeline:
  issue: [plan, write, proof, lint, ship, signoff, notes]
steps:
  lint:
    prompt: go vet ./...
    kind: check
  ship:
    prompt: pr draft
    kind: post
  signoff:
    prompt: read it
    kind: gate
  notes:
    prompt: write the release note
`
	got := mustApply(t, base, add("issue", "bench", "write", "time it"), move("issue", "plan", ""))
	if g := joined(reread(t, got.Text).Pipeline[factory.KindIssue]); g != "plan,write,bench,proof,lint,ship,signoff,notes" {
		t.Fatalf("%s", g)
	}
	for _, name := range []string{"lint", "ship", "signoff", "notes"} {
		wantCode(t, refusal(t, base, remove("issue", name)), EditLocked, 0)
		wantCode(t, refusal(t, base, move("issue", name, "plan")), EditLocked, 0)
	}
}

func TestACrossedPersonBeforeProofIsStillAllowedAndNamed(t *testing.T) {
	const base = "pipeline:\n  issue: [plan, approve, write, test, proof]\n"
	got := mustApply(t, base, move("issue", "write", "plan"), add("issue", "lint", "approve", "lint it"))
	if g := joined(reread(t, got.Text).Pipeline[factory.KindIssue]); g != "plan,write,approve,lint,test,proof" {
		t.Fatalf("%s", g)
	}
	want := []GateChange{{Pipeline: "issue", Gate: "approve", Before: []string{"plan"}, After: []string{"plan", "write"}}}
	if !reflect.DeepEqual(got.Gates, want) {
		t.Fatalf("gates %+v", got.Gates)
	}
	if s := strayInRecipe(t, got.Text, factory.KindIssue); s != "" {
		t.Fatalf("%s runs after proof", s)
	}
}
