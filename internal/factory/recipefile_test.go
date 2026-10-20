package factory_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// ownerRecipe is the shape the owner decided, byte for byte.
const ownerRecipe = `# factory recipe · codeaf

## issue
1. plan · chat · read the issue and say how
2. approve · when large
3. write · chat · fanout 3
4. test · check · go test ./... · until clean · max 2
5. review · chat · read it as a stranger would · until clean · max 2
6. security · chat · when touches auth
7. proof · chat · show each claim in its own medium

## pr
1. read · chat · what changed and why
2. checks · check · ci
3. review · chat · until clean · max 2 · fanout per finding
4. proof

## policy
- tests pass before anything posts
- a stranger's PR never runs write

## habits
- factory PRs from your own issues self-ship when the proof is green
`

func TestParseTheOwnersRecipe(t *testing.T) {
	r, probs := factory.Parse(ownerRecipe)
	if len(probs) != 0 {
		t.Fatalf("problems = %+v", probs)
	}
	is := r.For(factory.KindIssue)
	if len(is) != 7 {
		t.Fatalf("issue stages = %d", len(is))
	}
	if p := is[0]; p.Name != "plan" || p.Kind != factory.StageChat || p.Ask != "read the issue and say how" || p.When != "" || !p.On {
		t.Fatalf("plan = %+v", p)
	}
	// `N. approve` is the person's gate without saying gate.
	if a := is[1]; a.Name != factory.ApproveName || a.Kind != factory.StageGate || a.When != "large" || !a.On {
		t.Fatalf("approve = %+v", a)
	}
	is = append(is[:1:1], is[2:]...)
	// A line with no ask copies the shipped stage and lays its knobs over.
	if w := is[1]; w.Ask != "make the change in the checkout" || w.Fanout != "3" || w.Until != "done" || w.Max != 2 {
		t.Fatalf("write = %+v", w)
	}
	if ts := is[2]; ts.Kind != factory.StageCheck || ts.Ask != "go test ./..." || ts.Until != "clean" || ts.Max != 2 {
		t.Fatalf("test = %+v", ts)
	}
	// Security is in no default list; a line that names it runs the shipped one.
	if s := is[4]; s.When != "touches auth" || !s.On || s.Ask != "review for injection, authz, secrets handling" {
		t.Fatalf("security = %+v", s)
	}
	pr := r.For(factory.KindPR)
	if len(pr) != 4 || pr[1].Kind != factory.StageCheck || pr[1].Ask != "ci" {
		t.Fatalf("pr = %+v", pr)
	}
	// ONE NAME IS ONE SHIPPED STAGE: a pull request's review is the issue's.
	if rv := pr[2]; rv.Ask != "review the diff as a careful colleague; fix what you find" || rv.Fanout != "per-finding" || rv.Max != 2 {
		t.Fatalf("pr review = %+v", rv)
	}
	// The copy is never fixed: only the line's own `fixed` fixes it.
	if pf := pr[3]; pf.Name != "proof" || pf.Ask != "show each claim in its own medium" || pf.Until != "proven" || pf.Max != 3 || pf.Fixed {
		t.Fatalf("pr proof shorthand = %+v", pf)
	}
	// The missing ci section is the default's.
	if !reflect.DeepEqual(r.For(factory.KindCI)[0].Name, "bisect") {
		t.Fatalf("ci = %+v", r.For(factory.KindCI))
	}
	if len(r.Policy) != 2 || r.Policy[1] != "a stranger's PR never runs write" {
		t.Fatalf("policy = %q", r.Policy)
	}
	if len(r.Habits) != 1 || !strings.HasPrefix(r.Habits[0], "factory PRs") {
		t.Fatalf("habits = %q", r.Habits)
	}
}

func TestParseEveryKnob(t *testing.T) {
	r, probs := factory.Parse("## chore\n1. tidy · post · neaten it · when has ui · until proven · rounds 3 · fanout per file · effort strong · proof a test, a screenshot · off\n2. ask · gate\n")
	if len(probs) != 0 {
		t.Fatalf("problems = %+v", probs)
	}
	got := r.For(factory.KindChore)
	want := factory.Stage{Name: "tidy", Kind: factory.StagePost, Ask: "neaten it", When: "has ui", Until: "proven", Max: 3,
		Fanout: "per-file", Effort: "strong", Proof: []string{"a test", "a screenshot"}, On: false}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("stage = %+v\nwant    %+v", got[0], want)
	}
	if got[1].Kind != factory.StageGate || got[1].Ask != "" || !got[1].On {
		t.Fatalf("a gate stage = %+v", got[1])
	}
}

func TestParseNamesBadLinesAndLoadsTheRest(t *testing.T) {
	text := "stray words\n## issue\n1. plan · chat · say how · until clen\nwrite it all\n2. test · check · go vet · max two · effort huge\n3. review · chat · one ask · another ask\n4. · chat\n## nonsense\n1. x\n## policy\nno bullet\n- kept\n"
	r, probs := factory.Parse(text)
	lines := map[int]bool{}
	for _, p := range probs {
		lines[p.Line] = true
		if p.Why == "" || p.Text == "" {
			t.Fatalf("a problem without words: %+v", p)
		}
	}
	for _, n := range []int{1, 3, 4, 5, 6, 7, 8, 11} {
		if !lines[n] {
			t.Fatalf("line %d not named; problems = %+v", n, probs)
		}
	}
	if lines[9] {
		t.Fatalf("a line under an unknown section was named twice: %+v", probs)
	}
	is := r.For(factory.KindIssue)
	if len(is) != 3 || is[0].Until != "" || is[1].Max != 0 || is[2].Ask != "one ask" {
		t.Fatalf("stages = %+v", is)
	}
	if len(r.Policy) != 1 || r.Policy[0] != "kept" {
		t.Fatalf("policy = %q", r.Policy)
	}
}

// `## approve` ON A LINE OF ITS OWN INSIDE A KIND'S SECTION is an approve step
// at that place, and a second one is approve2; the file writes them back as
// stage lines.
func TestParseApproveHeadingIsAStep(t *testing.T) {
	r := mustParse(t, "## issue\n1. plan · chat · say how\n## approve\n2. write · chat · do it\n## approve\n3. proof\n")
	var names []string
	for _, st := range r.Stages {
		names = append(names, st.Name+":"+string(st.Kind))
	}
	if strings.Join(names, " ") != "plan:chat approve:gate write:chat approve2:gate proof:chat" {
		t.Fatalf("stages = %q", names)
	}
	if again := mustParse(t, factory.Format(r)); !reflect.DeepEqual(again.Stages, r.Stages) {
		t.Fatalf("drifted: %+v", again.Stages)
	}
}

func TestParseNeverPanics(t *testing.T) {
	for _, s := range []string{"", "\n\n", "##", "## issue\n1.", "## issue\n1. ·", "## issue\n1. a · gate when", "## issue\n1. a · proof", "## issue\n1. a · fanout per", "\r\n## pr\r\n1. read\r\n"} {
		factory.Parse(s)
	}
}

func TestFormatRoundTripsTheDefault(t *testing.T) {
	first := factory.Format(factory.DefaultRecipe())
	r, probs := factory.Parse(first)
	if len(probs) != 0 {
		t.Fatalf("the default's own file has problems: %+v\n%s", probs, first)
	}
	if again := factory.Format(r); again != first {
		t.Fatalf("not byte-stable:\n%s\n---\n%s", first, again)
	}
	if !strings.Contains(first, "5. review · chat · review the diff as a careful colleague; fix what you find · until clean · max 3\n6. proof · chat · show each claim in its own medium · until proven · max 3 · fixed") {
		t.Fatalf("default file =\n%s", first)
	}
	// The owner's file comes back in the same shape it went in.
	o, _ := factory.Parse(ownerRecipe)
	o2, _ := factory.Parse(factory.Format(o))
	if !reflect.DeepEqual(o, o2) {
		t.Fatalf("owner's recipe drifted across a round trip")
	}
	if !strings.Contains(factory.Format(o), "1. plan · chat · read the issue and say how\n2. approve · gate · when large\n") {
		t.Fatalf("formatted owner's recipe =\n%s", factory.Format(o))
	}
}

func TestLoadWithNoFileIsTheDefault(t *testing.T) {
	r, probs, err := factory.Load(t.TempDir())
	if err != nil || probs != nil || !reflect.DeepEqual(r, factory.DefaultRecipe()) {
		t.Fatalf("Load = %+v, %v, %v", r, probs, err)
	}
}

func TestSaveThenLoad(t *testing.T) {
	dir := t.TempDir()
	o, _ := factory.Parse(ownerRecipe)
	if err := (pipeline.Files{}).Save(dir, o); err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Load(dir)
	got, probs := f.Recipe(), f.Problems
	if err != nil || len(probs) != 0 || len(got.Stages) != len(o.Stages) {
		t.Fatalf("Load after Save = %+v, %v, %v", got, probs, err)
	}
	left, _ := os.ReadDir(filepath.Join(dir, ".codeaf"))
	if len(left) != 1 {
		t.Fatalf(".codeaf holds %d files; a temp file was left behind", len(left))
	}
}

func TestMetReadsTheUntilWords(t *testing.T) {
	st := func(u string) factory.Stage { return factory.Stage{Until: u} }
	ok := []factory.Claim{{Text: "a", OK: true, Evidence: "test"}}
	cases := []struct {
		until string
		r     factory.StageResult
		want  bool
	}{
		{"", factory.StageResult{Done: true}, true},
		{factory.UntilDone, factory.StageResult{Done: true}, true},
		{factory.UntilDone, factory.StageResult{}, false},
		{factory.UntilClean, factory.StageResult{Done: true}, true},
		{factory.UntilClean, factory.StageResult{Done: true, Findings: 2}, false},
		{factory.UntilClean, factory.StageResult{}, false},
		{factory.UntilGreen, factory.StageResult{Done: true, Exit: 0}, true},
		{factory.UntilGreen, factory.StageResult{Done: true, Exit: 1}, false},
		{factory.UntilGreen, factory.StageResult{Exit: 0}, false},
		{factory.UntilProven, factory.StageResult{Done: true, Claims: ok}, true},
		{factory.UntilProven, factory.StageResult{Done: true}, false},
		{factory.UntilProven, factory.StageResult{Done: true, Claims: append(ok, factory.Claim{Text: "b", OK: true})}, false},
		{factory.UntilProven, factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "a", Evidence: "red"}}}, false},
		{"until whenever", factory.StageResult{Done: true}, false},
	}
	for _, c := range cases {
		if got := factory.Met(st(c.until), c.r); got != c.want {
			t.Errorf("Met(until %q, %+v) = %v, want %v", c.until, c.r, got, c.want)
		}
	}
}

// A REPORT THAT CONTRADICTS ITS OWN RULE IS AT FAULT, AND NOTHING ELSE IS. The
// fault is the fix in the step's words; an honest shortfall has none, and a
// report with a fault never meets its until, because the fault is the judge's
// own rule read early.
func TestReportFaultIsTheJudgesRuleReadEarly(t *testing.T) {
	st := func(u string) factory.Stage { return factory.Stage{Until: u} }
	shown := factory.Claim{Text: "a", OK: true, Evidence: "go test ./ledger passes"}
	cases := []struct {
		until string
		r     factory.StageResult
		want  string
	}{
		// until clean: done with findings it says are open.
		{factory.UntilClean, factory.StageResult{Done: true, Findings: 2},
			"you reported 2 findings still open: fix them and report again, or report done: false"},
		{factory.UntilClean, factory.StageResult{Done: true, Findings: 1},
			"you reported 1 finding still open: fix it and report again, or report done: false"},
		{factory.UntilClean, factory.StageResult{Done: false, Findings: 2}, ""},
		{factory.UntilClean, factory.StageResult{Done: true}, ""},
		// until proven: a claim marked ok that shows nothing, done or not.
		{factory.UntilProven, factory.StageResult{Done: true, Claims: []factory.Claim{shown, {Text: "b", OK: true, Evidence: "  "}}},
			"claim 2 has no evidence: add what shows it, or set ok: false"},
		{factory.UntilProven, factory.StageResult{Done: false, Claims: []factory.Claim{{Text: "a", OK: true}}},
			"claim 1 has no evidence: add what shows it, or set ok: false"},
		{factory.UntilProven, factory.StageResult{Done: true},
			"you reported done with no claims: add each claim with what shows it, or report done: false"},
		// An honest shortfall is no fault: a claim not shown, with or without
		// what was seen, and a proof that is not done.
		{factory.UntilProven, factory.StageResult{Done: true, Claims: []factory.Claim{shown, {Text: "b", OK: false, Evidence: "TestRefund fails"}}}, ""},
		{factory.UntilProven, factory.StageResult{Done: true, Claims: []factory.Claim{{Text: "b", OK: false}}}, ""},
		{factory.UntilProven, factory.StageResult{Done: false}, ""},
		{factory.UntilProven, factory.StageResult{Done: true, Claims: []factory.Claim{shown}}, ""},
		// The rules the judge does not read are not read here either.
		{factory.UntilDone, factory.StageResult{Done: true, Findings: 3, Claims: []factory.Claim{{Text: "a", OK: true}}}, ""},
		{"", factory.StageResult{Done: true, Findings: 3}, ""},
		{factory.UntilGreen, factory.StageResult{Done: true, Findings: 3}, ""},
		{"until whenever", factory.StageResult{Done: true, Findings: 3}, ""},
	}
	for _, c := range cases {
		got := factory.ReportFault(st(c.until), c.r)
		if got != c.want {
			t.Errorf("ReportFault(until %q, %+v) = %q, want %q", c.until, c.r, got, c.want)
		}
		if got != "" && factory.Met(st(c.until), c.r) {
			t.Errorf("until %q: a report at fault (%q) met its until", c.until, got)
		}
	}
}

func TestLocalSeamReadsAndBanksTheRecipeFile(t *testing.T) {
	_, st := openLocal(t)
	dir := t.TempDir()
	const repo = "agentfield/codeaf"
	custom := "# factory recipe · codeaf\n\n## issue\n1. plan · chat · say how first\n2. write · chat · fanout 2\n\n## pr\n1. read · chat · what changed\n2. odd · chat · a · b\n\n## habits\n- one habit\n"
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, factory.RecipeFile), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), pipeline.LocalFiles(), factory.WithRepoDirs(func(r string) string {
		if r == repo {
			return dir
		}
		return ""
	}))
	if !seam.Has("bankstages") || !seam.Has("bank") {
		t.Fatal("a seam that knows the checkout has no bank doors")
	}
	id, err := seam.New(repo, "the recipe file")
	if err != nil {
		t.Fatal(err)
	}
	other, err := seam.New("agentfield/agentfield", "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := seam.Load()
	rp, _ := snap.RepoNamed(repo)
	if len(rp.Recipe.Stages) != 2 || !strings.Contains(rp.Recipe.Stages[0].Ask, "say how first") || len(rp.Recipe.Policy) != 1 {
		t.Fatalf("repo = %+v", rp)
	}
	var it factory.Item
	for _, x := range snap.Items {
		if x.ID == id {
			it = x
		}
	}
	if len(it.Stages) != 2 {
		t.Fatalf("a new item did not take its repo's recipe: %+v", it.Stages)
	}
	if err := seam.AddStage(id, "after review, make it neater"); err != nil {
		t.Fatal(err)
	}
	if err := seam.BankStages(id); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, pipeline.RepoFile))
	text := string(data)
	if !strings.Contains(text, "prompt: make it neater") {
		t.Fatalf("banked file =\n%s", text)
	}
	// Every other section is as the person wrote it, the bad line included.
	if original, _ := os.ReadFile(filepath.Join(dir, factory.RecipeFile)); string(original) != custom {
		t.Fatalf("another section changed:\n%s", text)
	}
	if err := seam.Bank(repo, "factory PRs self-ship"); err != nil {
		t.Fatal(err)
	}
	if err := seam.Bank(repo, "factory PRs self-ship"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, pipeline.RepoFile))
	if strings.Count(string(data), "factory PRs self-ship") != 1 || !strings.Contains(string(data), "one habit") {
		t.Fatalf("after Bank =\n%s", data)
	}
	if err := seam.BankStages(other); err == nil {
		t.Fatal("banked onto a repository whose checkout is not known")
	}
}

// A first bank on a repository with no file writes one that reads whole.
func TestBankWithNoFileWritesTheDefaultAround(t *testing.T) {
	dir := t.TempDir()
	if err := (pipeline.Files{}).BankRule(dir, "a habit"); err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Load(dir)
	r, probs := f.Recipe(), f.Problems
	if err != nil || len(probs) != 0 || len(r.Policy) != 1 || !reflect.DeepEqual(factory.StageLines(r.Stages), factory.StageLines(pipeline.Defaults().Recipe().Stages)) {
		t.Fatalf("Load = %+v %+v %v", r, probs, err)
	}
}

func TestLocalSeamWithoutDirsHasNoBankDoors(t *testing.T) {
	_, st := openLocal(t)
	for _, s := range []factory.Seam{factory.LocalSeam(st, time.Now()), factory.LocalSeam(st, time.Now(), pipeline.LocalFiles(), nil)} {
		if s.Has("bankstages") || s.Has("bank") {
			t.Fatal("a bank door with nowhere to write")
		}
	}
	if _, err := st.Add(context.Background(), factory.Item{Repo: "x/y", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	snap, err := factory.LocalSeam(st, time.Now()).Load()
	if err != nil || len(snap.Repos) != 1 || !reflect.DeepEqual(snap.Repos[0].Recipe, factory.DefaultRecipe()) {
		t.Fatalf("Load = %+v, %v", snap.Repos, err)
	}
}
