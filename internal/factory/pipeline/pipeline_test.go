package pipeline_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// specFile is the pipeline file from FACTORY-YAML.md, comments and all.
const specFile = `
start:
  labels: [agent-ready]
  max_size: M
  max_cost: 2
  never: [auth/**, migrations/**]
  ask_first:
    - anything that changes how secrets are read or stored

size:
  pr_lines: [50, 400]
  issue: model

pipeline:
  issue: [plan, approve, write, test, review, proof]
  pr:    [read, review, proof]
  ci:    [read, write, test, proof]

steps:
  test: run the test suite this repo uses; fix what fails; stop when it is green
  security: if the change touches api/ or auth/, review it for injection, authz gaps and secret handling and list findings as file:line; otherwise say so and stop
  proof:
    prompt: show each claim in its own medium; a screenshot for any UI change; a benchmark for anything under hot/
    until: every claim has evidence
    max: 3
  migrate-check: if the change adds a migration, run it against a fresh database and show the schema diff

fixed: [approve, proof]

rules:
  - tests pass before anything posts
  - no pull request without a green check
  - a stranger's PR never runs write
`

func TestParseTheSpecFile(t *testing.T) {
	f, err := pipeline.Parse(specFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Problems) != 0 {
		t.Fatalf("problems = %+v", f.Problems)
	}
	if got, want := f.Start.Labels, []string{"agent-ready"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v", got)
	}
	if f.Start.MaxSize != "M" || f.Start.MaxCost != 2 {
		t.Fatalf("start = %+v", f.Start)
	}
	if strings.Join(f.Start.Never, ",") != "auth/**,migrations/**" {
		t.Fatalf("never = %v", f.Start.Never)
	}
	if len(f.Start.AskFirst) != 1 || !strings.Contains(f.Start.AskFirst[0], "secrets") {
		t.Fatalf("ask_first = %v", f.Start.AskFirst)
	}
	if f.Size.PRLines != [2]int{50, 400} || f.Size.Issue != pipeline.DefaultIssueMode {
		t.Fatalf("size = %+v", f.Size)
	}
	if strings.Join(f.Pipeline[factory.KindIssue], ",") != "plan,approve,write,test,review,proof" {
		t.Fatalf("issue = %v", f.Pipeline[factory.KindIssue])
	}
	if strings.Join(f.Pipeline[factory.KindPR], ",") != "read,review,proof" {
		t.Fatalf("pr = %v", f.Pipeline[factory.KindPR])
	}
	if f.Steps["test"].Prompt != "run the test suite this repo uses; fix what fails; stop when it is green" {
		t.Fatalf("test = %+v", f.Steps["test"])
	}
	if f.Steps["test"].Until != "green" {
		t.Fatalf("test until = %q", f.Steps["test"].Until)
	}
	if f.Steps["proof"].Max != 3 || f.Steps["proof"].Until != "every claim has evidence" {
		t.Fatalf("proof = %+v", f.Steps["proof"])
	}
	if !strings.Contains(f.Steps["security"].Prompt, "file:line") {
		t.Fatalf("security = %q", f.Steps["security"].Prompt)
	}
	if !strings.Contains(f.Steps["migrate-check"].Prompt, "migration") {
		t.Fatalf("migrate-check = %+v", f.Steps["migrate-check"])
	}
	if strings.Join(f.Fixed, ",") != "approve,proof" {
		t.Fatalf("fixed = %v", f.Fixed)
	}
	if len(f.Rules) != 3 {
		t.Fatalf("rules = %v", f.Rules)
	}
}

func TestEmptyFileIsTheDefaults(t *testing.T) {
	f, err := pipeline.Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Start.Labels) != 0 || f.Start.MaxCost != pipeline.DefaultMaxCost || f.Start.MaxSize != pipeline.DefaultMaxSize {
		t.Fatalf("start = %+v", f.Start)
	}
	if len(f.Pipeline) != 1 || len(f.Pipeline[factory.KindCI]) != 0 {
		t.Fatalf("ci = %v", f.Pipeline[factory.KindCI])
	}
	if f.Steps["plan"].Prompt == "" || f.Steps["read"].Until != factory.UntilDone || f.Steps["review"].Max != 3 {
		t.Fatalf("builtins = plan %q read until %q review max %d", f.Steps["plan"].Prompt, f.Steps["read"].Until, f.Steps["review"].Max)
	}
	d := f.Decide(pipeline.Job{ID: 1, Kind: factory.KindIssue, Labels: []string{"agent-ready"}, Size: "S"})
	if d.Start || d.Ask {
		t.Fatalf("nothing starts until a label is named: %+v", d)
	}
}

func TestABadFileDoesNotReplaceTheDefaults(t *testing.T) {
	f, err := pipeline.Parse("start: [\n---\nrules: []\n")
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	if f.Start.MaxCost != pipeline.DefaultMaxCost || len(f.Problems) == 0 {
		t.Fatalf("file = %+v err=%v", f.Start, err)
	}
}

func TestUnknownKeysAndABadSizeAreProblems(t *testing.T) {
	f, err := pipeline.Parse("habits: [keep it small]\nstart:\n  max_size: huge\nsize:\n  issue: guess\n")
	if err != nil {
		t.Fatal(err)
	}
	why := problems(f)
	for _, needle := range []string{"habits", "max_size", "issue"} {
		if !strings.Contains(why, needle) {
			t.Fatalf("missing %s in %s", needle, why)
		}
	}
	if f.Start.MaxSize != pipeline.DefaultMaxSize || f.Size.Issue != pipeline.DefaultIssueMode {
		t.Fatalf("kept bad values: %+v %+v", f.Start, f.Size)
	}
}

func TestATenthStepIsRefused(t *testing.T) {
	var b strings.Builder
	b.WriteString("pipeline:\n  issue: [")
	for i := 1; i <= 10; i++ {
		if i > 1 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "s%d", i)
	}
	b.WriteString("]\nsteps:\n")
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "  s%d: do it\n", i)
	}
	f, err := pipeline.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Pipeline[factory.KindIssue]) != factory.StageMost {
		t.Fatalf("steps = %v", f.Pipeline[factory.KindIssue])
	}
	if !strings.Contains(problems(f), factory.ErrNineStages.Error()) {
		t.Fatalf("problems = %s", problems(f))
	}
}

func TestAnUnknownStepIsNotRun(t *testing.T) {
	f, err := pipeline.Parse("pipeline:\n  issue: [plan, frobnicate]\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.Pipeline[factory.KindIssue], ",") != "plan" {
		t.Fatalf("issue = %v", f.Pipeline[factory.KindIssue])
	}
	if !strings.Contains(problems(f), "frobnicate") {
		t.Fatalf("problems = %s", problems(f))
	}
}

func TestApproveHasNoPrompt(t *testing.T) {
	f, err := pipeline.Parse("steps:\n  approve: look at this\n")
	if err != nil {
		t.Fatal(err)
	}
	if f.Steps["approve"].Prompt != "" {
		t.Fatalf("prompt = %q", f.Steps["approve"].Prompt)
	}
	if !strings.Contains(problems(f), "person") {
		t.Fatalf("problems = %s", problems(f))
	}
}

func TestExplicitEmptyFixedClearsTheDefault(t *testing.T) {
	f, err := pipeline.Parse("fixed: []\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Fixed) != 0 {
		t.Fatalf("fixed = %v", f.Fixed)
	}
	proof := f.Recipe().Stages[len(f.Recipe().Stages)-1]
	if proof.Name != "proof" || proof.Fixed {
		t.Fatalf("proof = %+v", proof)
	}
}

func TestRecipeCarriesFreeUntilsInTheAsk(t *testing.T) {
	f, err := pipeline.Parse("steps:\n  plan:\n    until: the plan names its files\n")
	if err != nil {
		t.Fatal(err)
	}
	r := f.Recipe()
	plan := r.Stages[0]
	if plan.Name != "plan" || plan.Until != factory.UntilDone || !strings.Contains(plan.Ask, "Stop when: the plan names its files") {
		t.Fatalf("plan = %+v", plan)
	}
	approve := r.Stages[1]
	if approve.Name != factory.ApproveName || approve.Kind != factory.StageGate || approve.Fixed {
		t.Fatalf("approve = %+v", approve)
	}
	test := r.Stages[3]
	if test.Name != "test" || test.Until != factory.UntilGreen || strings.Contains(test.Ask, "Stop when") {
		t.Fatalf("test = %+v", test)
	}
	proof := r.Stages[len(r.Stages)-1]
	if proof.Name != "proof" || proof.Until != factory.UntilProven || !proof.Fixed {
		t.Fatalf("proof = %+v", proof)
	}
	// A hyphenated step has to become one word the runner accepts.
	f, err = pipeline.Parse("pipeline:\n  issue: [migrate-check]\nsteps:\n  migrate-check: run the migration\n")
	if err != nil {
		t.Fatal(err)
	}
	got := f.Recipe().Stages
	if len(got) != 1 || got[0].Name != "migratecheck" || got[0].Ask != "run the migration" {
		t.Fatalf("stages = %+v", got)
	}
}

func TestFormatStepsPrintsTheBuiltins(t *testing.T) {
	text := pipeline.FormatSteps()
	if !strings.Contains(text, "approve:\n  a person\n") || !strings.Contains(text, "until: green") {
		t.Fatalf("steps =\n%s", text)
	}
}

func TestDecideWhatStarts(t *testing.T) {
	f := ready()
	ok := pipeline.Job{
		ID: 7, Kind: factory.KindIssue, Labels: []string{"Agent-Ready"}, Size: "S",
		Cost: 2, CostKnown: true, Judged: true,
	}
	if d := f.Decide(ok); !d.Start || d.Ask {
		t.Fatalf("starts = %+v", d)
	}
	over := ok
	over.ID = 8
	over.Size = "L"
	if d := f.Decide(over); d.Start || !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "over size M") {
		t.Fatalf("over size = %+v", d)
	}
	cost := ok
	cost.Cost = 2.5
	if d := f.Decide(cost); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "over $2") {
		t.Fatalf("over cost = %+v", d)
	}
	cap := ok
	cap.Cost = 1.5
	cap.Cap = 1
	if d := f.Decide(cap); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "over your cap of $1") {
		t.Fatalf("cap = %+v", d)
	}
	unread := ok
	unread.Size = ""
	if d := f.Decide(unread); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "size not read yet") {
		t.Fatalf("unread = %+v", d)
	}
	plain := ok
	plain.Labels = []string{"bug"}
	if d := f.Decide(plain); d.Start || d.Ask {
		t.Fatalf("no label = %+v", d)
	}
	stranger := ok
	stranger.Kind = factory.KindIssue
	stranger.Tier = factory.TierStranger
	if d := f.Decide(stranger); !d.Start {
		t.Fatalf("a stranger's issue can start = %+v", d)
	}
}

func TestAPullRequestIsMeasured(t *testing.T) {
	f := ready()
	pr := pipeline.Job{ID: 3, Kind: factory.KindPR, Labels: []string{"agent-ready"}, Tier: factory.TierOwner, Judged: true, LinesKnown: true, Lines: 50}
	if d := f.Decide(pr); !d.Start {
		t.Fatalf("50 lines = %+v", d)
	}
	pr.Lines = 51
	if d := f.Decide(pr); !d.Start {
		t.Fatalf("51 lines is still M = %+v", d)
	}
	pr.Lines = 400
	if d := f.Decide(pr); !d.Start {
		t.Fatalf("400 lines = %+v", d)
	}
	pr.Lines = 401
	if d := f.Decide(pr); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "this one is L") {
		t.Fatalf("401 lines = %+v", d)
	}
	// A letter is not a measurement.
	guessed := pr
	guessed.LinesKnown = false
	guessed.Size = "S"
	if d := f.Decide(guessed); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "not measured yet") {
		t.Fatalf("guessed = %+v", d)
	}
	stranger := pr
	stranger.Lines = 10
	stranger.Tier = factory.TierStranger
	if d := f.Decide(stranger); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "stranger") {
		t.Fatalf("stranger = %+v", d)
	}
}

func TestNeverPathsAndAskFirst(t *testing.T) {
	f := ready()
	job := pipeline.Job{ID: 4, Kind: factory.KindPR, Labels: []string{"agent-ready"}, LinesKnown: true, Lines: 10, Judged: true}
	job.Paths = []string{"internal/auth/keys.go"}
	if d := f.Decide(job); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "auth/**") {
		t.Fatalf("auth = %+v", d)
	}
	job.Paths = []string{"db/migrations/001.sql"}
	if d := f.Decide(job); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "migrations/**") {
		t.Fatalf("migrations = %+v", d)
	}
	job.Paths = []string{"author/readme.md"}
	if d := f.Decide(job); !d.Start {
		t.Fatalf("author is not auth = %+v", d)
	}
	waiting := job
	waiting.Judged = false
	if d := f.Decide(waiting); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "not judged yet") {
		t.Fatalf("unjudged = %+v", d)
	}
	hit := job
	hit.Matched = []string{"Anything that changes how secrets are read or stored"}
	if d := f.Decide(hit); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "secrets") {
		t.Fatalf("matched = %+v", d)
	}
}

func TestLabelSizeUsesTheLargestLabel(t *testing.T) {
	f, err := pipeline.Parse("start:\n  labels: [agent-ready]\nsize:\n  issue: label\n")
	if err != nil {
		t.Fatal(err)
	}
	job := pipeline.Job{ID: 1, Kind: factory.KindIssue, Labels: []string{"agent-ready", "size/S", "size/L"}}
	if d := f.Decide(job); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "this one is L") {
		t.Fatalf("largest = %+v", d)
	}
	job.Labels = []string{"agent-ready", "size/M"}
	if d := f.Decide(job); !d.Start {
		t.Fatalf("M = %+v", d)
	}
	job.Labels = []string{"agent-ready"}
	if d := f.Decide(job); !d.Ask || !strings.Contains(strings.Join(d.Reasons, " "), "no size label") {
		t.Fatalf("none = %+v", d)
	}
}

func TestJobFromItemKeepsTheFactsDecideReads(t *testing.T) {
	when := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	issue := factory.Item{
		ID: 7, Kind: factory.KindIssue, Tier: factory.TierOwner, Labels: []string{"agent-ready"},
		Cap: 5, Triage: factory.Triage{Size: "S", Est: 1.5, TriagedAt: when},
	}
	got := pipeline.JobFromItem(issue)
	if !got.CostKnown || got.Cost != 1.5 || got.Size != "S" || got.LinesKnown || got.Cap != 5 {
		t.Fatalf("issue = %+v", got)
	}
	unread := issue
	unread.Triage.TriagedAt = time.Time{}
	if pipeline.JobFromItem(unread).CostKnown {
		t.Fatal("an unread estimate counted as a cost")
	}
	pr := factory.Item{
		ID: 8, Kind: factory.KindPR, Tier: factory.TierStranger,
		Files: []factory.FileChange{{Path: "auth/keys.go", Added: 3, Removed: 1}, {Path: "", Added: 9}},
	}
	got = pipeline.JobFromItem(pr)
	if !got.LinesKnown || got.Lines != 13 || len(got.Paths) != 1 || got.Paths[0] != "auth/keys.go" {
		t.Fatalf("pull request = %+v", got)
	}
	pr.Files = nil
	if pipeline.JobFromItem(pr).LinesKnown {
		t.Fatal("unread files counted as a measurement")
	}
}

func TestDryRunKeepsOrderAndSkipsTheUnlabelled(t *testing.T) {
	f := ready()
	jobs := []pipeline.Job{
		{ID: 1, Kind: factory.KindIssue, Labels: []string{"bug"}, Size: "S"},
		{ID: 2, Kind: factory.KindIssue, Labels: []string{"agent-ready"}, Size: "S", Judged: true},
		{ID: 3, Kind: factory.KindIssue, Labels: []string{"agent-ready"}, Size: "L", Judged: true},
	}
	got := f.Dry(jobs)
	if strings.Join(ints(got.WouldStart), ",") != "2" || strings.Join(ints(got.WouldAsk), ",") != "3" {
		t.Fatalf("dry = %+v", got)
	}
	if len(got.Why[3]) == 0 || len(got.Why[1]) != 0 {
		t.Fatalf("why = %+v", got.Why)
	}
}

func TestAPersonalFileMayOnlyBeStricter(t *testing.T) {
	repo, err := pipeline.Parse(specFile)
	if err != nil {
		t.Fatal(err)
	}
	personal, err := pipeline.ParsePersonal(`
start:
  labels: [agent-ready, urgent]
  max_size: L
  max_cost: 9
  never: [secrets/**]
size:
  pr_lines: [40, 500]
  issue: label
pipeline:
  pr: [read, write, proof]
steps:
  test: do nothing
  proof:
    max: 1
rules:
  - say please
fixed: [security]
habits: [be nice]
`)
	if err != nil {
		t.Fatal(err)
	}
	before := repo.Start.MaxCost
	got, probs := pipeline.Merge(repo, personal)
	if repo.Start.MaxCost != before {
		t.Fatal("merge changed the repo file")
	}
	why := joinProblems(probs)
	if got.Start.MaxCost != 2 || got.Start.MaxSize != "M" || got.Size.Issue != pipeline.DefaultIssueMode {
		t.Fatalf("loosened = %+v %+v", got.Start, got.Size)
	}
	if strings.Join(got.Start.Labels, ",") != "agent-ready" {
		t.Fatalf("labels = %v", got.Start.Labels)
	}
	if !has(got.Start.Never, "secrets/**") || !has(got.Start.Never, "auth/**") {
		t.Fatalf("never = %v", got.Start.Never)
	}
	if got.Size.PRLines != [2]int{50, 400} {
		t.Fatalf("lines = %v", got.Size.PRLines)
	}
	if strings.Join(got.Pipeline[factory.KindPR], ",") != "read,review,proof" {
		t.Fatalf("pr = %v", got.Pipeline[factory.KindPR])
	}
	if got.Steps["test"].Prompt == "do nothing" {
		t.Fatal("prompt changed")
	}
	if got.Steps["proof"].Max != 1 {
		t.Fatalf("proof max = %d", got.Steps["proof"].Max)
	}
	if !has(got.Rules, "say please") || len(got.Rules) != 4 {
		t.Fatalf("rules = %v", got.Rules)
	}
	if !has(got.Fixed, "security") || !has(got.Fixed, "proof") {
		t.Fatalf("fixed = %v", got.Fixed)
	}
	for _, needle := range []string{"urgent", "looser", "pr_lines", "issue", "cannot add write", "cannot change the prompt", "cannot drop", "habits"} {
		if !strings.Contains(why, needle) {
			t.Fatalf("missing %q in %s", needle, why)
		}
	}

	tighter, err := pipeline.ParsePersonal("start:\n  max_cost: 1\n  max_size: S\nsize:\n  pr_lines: [40, 300]\npipeline:\n  pr: [read, proof]\nsteps:\n  proof:\n    max: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	got, probs = pipeline.Merge(repo, tighter)
	if len(probs) != 0 {
		t.Fatalf("tighter problems = %+v", probs)
	}
	if got.Start.MaxCost != 1 || got.Start.MaxSize != "S" || got.Size.PRLines != [2]int{40, 300} {
		t.Fatalf("not tighter: %+v %+v", got.Start, got.Size)
	}
	if strings.Join(got.Pipeline[factory.KindPR], ",") != "read,proof" {
		t.Fatalf("pr = %v", got.Pipeline[factory.KindPR])
	}
	if got.Steps["proof"].Max != 1 {
		t.Fatalf("proof max = %d", got.Steps["proof"].Max)
	}
}

func TestMarkdownBecomesAPipeline(t *testing.T) {
	const md = `# factory recipe · codeaf

## issue
1. plan · chat · read the issue and say how
2. approve · gate · when large
3. write · chat · make the change · fanout per file
4. test · check · go test ./... · until green · max 2
5. security · chat · secrets · off

## pr
1. read · chat · what changed
2. review · chat · a different review

## policy
- tests pass before anything posts

## habits
- keep the diff small
`
	f := pipeline.FromMarkdown(md)
	if f.Source != pipeline.SourceMarkdown {
		t.Fatalf("source = %s", f.Source)
	}
	if strings.Join(f.Pipeline[factory.KindIssue], ",") != "plan,approve,write,test" {
		t.Fatalf("issue = %v", f.Pipeline[factory.KindIssue])
	}
	if f.Steps["plan"].Prompt != "read the issue and say how" {
		t.Fatalf("plan = %+v", f.Steps["plan"])
	}
	if f.Steps["test"].Until != "green" || f.Steps["test"].Max != 2 || f.Steps["test"].Prompt != "go test ./..." {
		t.Fatalf("test = %+v", f.Steps["test"])
	}
	if f.Steps["review"].Prompt != "a different review" {
		t.Fatalf("review = %+v", f.Steps["review"])
	}
	why := problems(f)
	for _, needle := range []string{"off in the markdown", "no condition"} {
		if !strings.Contains(why, needle) {
			t.Fatalf("missing %q in %s", needle, why)
		}
	}
	if !has(f.Rules, "tests pass before anything posts") || !has(f.Rules, "keep the diff small") {
		t.Fatalf("rules = %v", f.Rules)
	}
	r := f.Recipe()
	if r.Stages[1].Kind != factory.StageGate || r.Stages[3].Kind != factory.StageCheck || r.Stages[2].Fanout != "per-file" || r.Stages[3].Until != factory.UntilGreen {
		t.Fatalf("recipe = %+v", r.Stages)
	}
}

func ready() pipeline.File {
	f, err := pipeline.Parse(specFile)
	if err != nil {
		panic(err)
	}
	return f
}

func problems(f pipeline.File) string {
	return joinProblems(f.Problems)
}

func joinProblems(ps []pipeline.Problem) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Where)
		b.WriteByte(' ')
		b.WriteString(p.Why)
		b.WriteByte('\n')
	}
	return b.String()
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func ints(ns []int) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = strconv.Itoa(n)
	}
	return out
}
