package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// These pin the review of the runtime words (kind, when, effort, fanout): a
// step that cannot run as written holds for a person, never runs as a chat,
// never vanishes, and is never written as something else.

func stageNamed(t *testing.T, stages []factory.Stage, name string) factory.Stage {
	t.Helper()
	for _, s := range stages {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no stage %s in %+v", name, stages)
	return factory.Stage{}
}

func names(stages []factory.Stage) string {
	var out []string
	for _, s := range stages {
		out = append(out, s.Name+":"+string(s.Kind))
	}
	return strings.Join(out, ",")
}

func mustHold(t *testing.T, s factory.Stage) {
	t.Helper()
	if s.Kind != factory.StageGate || !factory.IsApprove(s) || s.Ask != "" || s.When != "" {
		t.Fatalf("%s should hold for a person with nothing to run, got %+v", s.Name, s)
	}
}

// B1: a free until never reaches a shell command or a post's verb line.
func TestAFreeUntilNeverReachesACheckOrAPost(t *testing.T) {
	for _, c := range []struct{ name, kind, prompt string }{
		{"lint", "check", "go vet ./..."},
		{"ship", "post", "pr draft"},
	} {
		f, err := Parse("pipeline:\n  issue: [write, " + c.name + ", review]\nsteps:\n  " + c.name + ":\n    prompt: " + c.prompt + "\n    kind: " + c.kind + "\n    until: it is quiet\n")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(problems(f), "not on \"it is quiet\"") {
			t.Fatalf("%s: the stop sentence must be a problem: %v", c.kind, f.Problems)
		}
		stages := f.Recipe().Stages
		for _, s := range stages {
			if strings.Contains(s.Ask, "Stop when") && s.Kind != factory.StageChat {
				t.Fatalf("%s: stop sentence reached %+v", c.kind, s)
			}
		}
		mustHold(t, stageNamed(t, stages, c.name))
		if got := names(stages); got != "write:chat,"+c.name+":gate,review:chat" {
			t.Fatalf("%s: the hold must keep its place: %s", c.kind, got)
		}
		if _, err := Encode(f); err == nil {
			t.Fatalf("%s: a step that holds must not be written", c.kind)
		}
	}
	// A word the runner checks is the check's own until, and the ask is the
	// command byte for byte.
	f := parseClean(t, "pipeline:\n  issue: [lint]\nsteps:\n  lint:\n    prompt: go vet ./...\n    kind: check\n    until: green\n")
	if s := f.Recipe().Stages[0]; s.Kind != factory.StageCheck || s.Ask != "go vet ./..." || s.Until != factory.UntilGreen {
		t.Fatalf("check = %+v", s)
	}
	// A gate with a stop sentence stays a gate and its ask carries none.
	g, _ := Parse("pipeline:\n  issue: [signoff]\nsteps:\n  signoff:\n    prompt: look it over\n    kind: gate\n    until: they say so\n")
	if s := g.Recipe().Stages[0]; s.Kind != factory.StageGate || strings.Contains(s.Ask, "Stop when") {
		t.Fatalf("gate = %+v", s)
	}
	if !strings.Contains(problems(g), "until is not read") {
		t.Fatalf("gate until must be a problem: %v", g.Problems)
	}
	// A chat still carries its sentence, as every legacy file did.
	c := parseClean(t, "pipeline:\n  issue: [write]\nsteps:\n  write:\n    prompt: do it\n    until: the tests pass\n")
	if s := c.Recipe().Stages[0]; s.Ask != "do it Stop when: the tests pass" {
		t.Fatalf("chat = %+v", s)
	}
}

// A check or post never borrows a shipped prompt or stop sentence: the shell
// would run "find and run the repo's tests; fix what fails".
func TestACheckNeverRunsAShippedPrompt(t *testing.T) {
	f, _ := Parse("pipeline:\n  issue: [plan, test]\nsteps:\n  test:\n    kind: check\n  plan:\n    prompt: make lint\n    kind: check\n")
	if !strings.Contains(problems(f), "this one has none") {
		t.Fatalf("problems = %v", f.Problems)
	}
	stages := f.Recipe().Stages
	mustHold(t, stageNamed(t, stages, "test"))
	// plan wrote its own command; the shipped "the plan names its files" is a
	// model's sentence and is not inherited.
	if s := stageNamed(t, stages, "plan"); s.Kind != factory.StageCheck || s.Ask != "make lint" {
		t.Fatalf("plan = %+v", s)
	}
}

// S1: an unreadable kind is never a chat, and the gate its author meant
// cannot disappear and let the steps after it run.
func TestAnUnreadableKindHoldsWhereItStands(t *testing.T) {
	const text = "pipeline:\n  issue: [plan, signoff, write, proof]\nsteps:\n  signoff:\n    prompt: a person reads the plan\n    kind: gates\n"
	f, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(problems(f), "the run holds at this step") {
		t.Fatalf("problems = %v", f.Problems)
	}
	r := f.Recipe()
	if got := names(r.Stages); got != "plan:chat,signoff:gate,write:chat,proof:chat" {
		t.Fatalf("stages = %s", got)
	}
	mustHold(t, r.Stages[1])
	if len(r.Problems) == 0 {
		t.Fatal("the recipe must carry the problem")
	}
	if !factory.HasApprove(factory.Item{Stages: r.Stages}) {
		t.Fatal("the run must hold for a person")
	}
	if _, err := Encode(f); err == nil {
		t.Fatal("an unreadable kind must not be written as a chat")
	}
	// The runner reads it the same way through the working tree.
	dir := t.TempDir()
	writeRepoFile(t, dir, RepoFile, text)
	loaded, _ := Load(dir)
	mustHold(t, loaded.Recipe().Stages[1])
	// A post whose condition cannot be read holds rather than act everywhere.
	p, _ := Parse("pipeline:\n  issue: [ship]\nsteps:\n  ship:\n    prompt: close\n    kind: post\n    when: sometimes\n")
	mustHold(t, p.Recipe().Stages[0])
}

// A gate needs no prompt, keeps its place, and is written without one.
func TestAGateNeedsNoPromptAndNeverVanishes(t *testing.T) {
	f := parseClean(t, "pipeline:\n  issue: [plan, signoff, write]\nsteps:\n  signoff:\n    kind: gate\n")
	if got := names(f.Recipe().Stages); got != "plan:chat,signoff:gate,write:chat" {
		t.Fatalf("stages = %s", got)
	}
	data, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "signoff:\n        kind: gate\n") {
		t.Fatalf("a gate is written with no prompt:\n%s", data)
	}
	back := parseClean(t, string(data))
	if names(back.Recipe().Stages) != names(f.Recipe().Stages) {
		t.Fatalf("round trip moved the gate: %s", names(back.Recipe().Stages))
	}
	// A name the runner cannot run is not a reason to drop a person.
	long, _ := Parse("pipeline:\n  issue: [plan, security-signoff, write]\nsteps:\n  security-signoff:\n    kind: gate\n")
	if got := names(long.Recipe().Stages); got != "plan:chat,approve:gate,write:chat" {
		t.Fatalf("stages = %s", got)
	}
	if !strings.Contains(problems(long), "holds there as an approve step") {
		t.Fatalf("problems = %v", long.Problems)
	}
}

// B2: set_step never turns a step into a shell command, a forge action or a
// gate, never removes a gate, and never edits a step that is not a chat.
func TestSetStepNeverChangesWhatRunsBeyondAChat(t *testing.T) {
	const base = "pipeline:\n  issue: [plan, signoff, lint, ship, write]\nsteps:\n  signoff:\n    prompt: read it\n    kind: gate\n  lint:\n    prompt: go vet ./...\n    kind: check\n  ship:\n    prompt: pr draft\n    kind: post\n"
	str := func(s string) *string { return &s }
	cases := []struct {
		op   EditOp
		code string
	}{
		{EditOp{Op: OpSetStep, Name: "plan", Kind: str("check")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "plan", Kind: str("post")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "plan", Kind: str("gate")}, EditBadValue},
		{EditOp{Op: OpSetStep, Name: "signoff", Kind: str("chat")}, EditLocked},
		{EditOp{Op: OpSetStep, Name: "signoff", Kind: str("")}, EditLocked},
		{EditOp{Op: OpSetStep, Name: "lint", Prompt: str("rm -rf .")}, EditLocked},
		{EditOp{Op: OpSetStep, Name: "lint", Until: str("it is quiet")}, EditLocked},
		{EditOp{Op: OpSetStep, Name: "ship", Prompt: str("pr")}, EditLocked},
		{EditOp{Op: OpSetStep, Name: "ship", Max: new(int)}, EditBadValue},
	}
	for _, c := range cases {
		_, err := ApplyEdits(base, []EditOp{c.op})
		ee, ok := err.(*EditError)
		if !ok || ee.Code != c.code {
			t.Errorf("%s %+v -> %v, want %s", c.op.Name, c.op, err, c.code)
		}
	}
	src, probs, ok := Inspect(base)
	if !ok {
		t.Fatal(probs)
	}
	locked := map[string]bool{"plan": false, "write": false, "signoff": true, "lint": true, "ship": true}
	for _, s := range src.Steps {
		if want, ok := locked[s.Name]; ok && s.Locked != want {
			t.Errorf("%s locked = %v, want %v", s.Name, s.Locked, want)
		}
	}
	// The other steps still edit, and the non-chat steps ride through byte
	// for byte in meaning.
	got, err := ApplyEdits(base, []EditOp{{Op: OpSetStep, Name: "write", Effort: str("strong")}})
	if err != nil {
		t.Fatal(err)
	}
	before, after := parseClean(t, base).Recipe().Stages, parseClean(t, got.Text).Recipe().Stages
	for i := range before {
		if before[i].Name == "write" {
			continue
		}
		if before[i].Kind != after[i].Kind || before[i].Ask != after[i].Ask {
			t.Fatalf("%s moved: %+v -> %+v", before[i].Name, before[i], after[i])
		}
	}
}

// S2: chat is the default, spelled or not.
func TestChatIsTheDefaultSpelledOrNot(t *testing.T) {
	f := parseClean(t, "pipeline:\n  issue: [write]\nsteps:\n  write:\n    prompt: do it\n    kind: chat\n")
	if f.Steps["write"].Kind != "" {
		t.Fatalf("kind = %q", f.Steps["write"].Kind)
	}
	data, _ := Encode(f)
	if strings.Contains(string(data), "kind:") {
		t.Fatalf("chat must encode to nothing:\n%s", data)
	}
	// No phantom change from restating the default.
	str := func(s string) *string { return &s }
	got, err := ApplyEdits("pipeline:\n  issue: [write]\nsteps:\n  write:\n    prompt: do it\n", []EditOp{{Op: OpSetStep, Name: "write", Kind: str("chat")}})
	if err != nil || got.Changed {
		t.Fatalf("restating chat changed the file: %v %+v", err, got)
	}
	// No false refusal of a personal file that restates it.
	repo := parseClean(t, "pipeline:\n  issue: [write]\nsteps:\n  write:\n    prompt: do it\n")
	personal, err := ParsePersonal("steps:\n  write:\n    kind: chat\n")
	if err != nil || len(personal.Problems) > 0 {
		t.Fatal(err, personal.Problems)
	}
	if _, probs := Merge(repo, personal); len(probs) != 0 {
		t.Fatalf("problems = %v", probs)
	}
}

// A personal file cannot hand a check a stop sentence, which would make the
// repository's check hold.
func TestPersonalFileCannotGiveACheckAStopSentence(t *testing.T) {
	repo := parseClean(t, "pipeline:\n  issue: [lint]\nsteps:\n  lint:\n    prompt: go vet ./...\n    kind: check\n")
	personal, err := ParsePersonal("steps:\n  lint:\n    until: vet is quiet\n")
	if err != nil {
		t.Fatal(err)
	}
	merged, probs := Merge(repo, personal)
	if len(probs) != 1 || !strings.Contains(probs[0].Why, "stop sentence") {
		t.Fatalf("problems = %v", probs)
	}
	if s := merged.Recipe().Stages[0]; s.Kind != factory.StageCheck || s.Ask != "go vet ./..." {
		t.Fatalf("merged = %+v", s)
	}
}

// B3: two markdown sections that run one step name differently keep their
// own meanings, and the rename is said.
func TestMarkdownSectionsKeepTheirOwnRuntimeWords(t *testing.T) {
	for _, md := range []string{
		"# factory recipe · codeaf\n\n## issue\n1. review · chat · review it · fanout per file · when touches auth\n\n## pr\n1. review · chat · review it\n",
		"# factory recipe · codeaf\n\n## issue\n1. review · chat · review it\n\n## pr\n1. review · chat · review it · fanout per file · when touches auth\n",
	} {
		f := FromMarkdown(md)
		want, _ := factory.Parse(md)
		r := f.Recipe()
		for kind, stages := range map[factory.Kind][]factory.Stage{factory.KindIssue: want.Stages, factory.KindPR: want.ByKind[factory.KindPR]} {
			got := r.For(kind)
			if len(got) != len(stages) {
				t.Fatalf("%s: %s", kind, names(got))
			}
			for i := range stages {
				if got[i].When != stages[i].When || got[i].Kind != stages[i].Kind {
					t.Fatalf("%s %s: when %q, markdown said %q\n%s", kind, got[i].Name, got[i].When, stages[i].When, problems(f))
				}
			}
		}
		if !strings.Contains(problems(f), "runs differently for pr, so it is review-pr there") {
			t.Fatalf("the rename must be said:\n%s", problems(f))
		}
		data, err := Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		back, _ := Parse(string(data))
		if back.Recipe().For(factory.KindPR)[0].When != r.For(factory.KindPR)[0].When {
			t.Fatalf("the conversion lost the pr condition:\n%s", data)
		}
	}
	// A check in one section and the shipped chat in another stay apart.
	f := FromMarkdown("# factory recipe · codeaf\n\n## issue\n1. test · check · go test ./... · until green\n")
	if s := f.Recipe().Stages[0]; s.Kind != factory.StageCheck || s.Ask != "go test ./..." {
		t.Fatalf("issue test = %+v", s)
	}
	for _, s := range f.Recipe().For(factory.KindCI) {
		if s.Kind == factory.StageCheck {
			t.Fatalf("ci gained the issue's check: %s", names(f.Recipe().For(factory.KindCI)))
		}
	}
	// With no name free, the step holds in both sections rather than pick one.
	h := FromMarkdown("# factory recipe · codeaf\n\n## issue\n1. abcdefghijkl · chat · do it · when thin\n\n## pr\n1. abcdefghijkl · chat · do it\n")
	mustHold(t, h.Recipe().Stages[0])
	mustHold(t, h.Recipe().For(factory.KindPR)[0])
}

// S3: a markdown gate under any name stays a person.
func TestMarkdownGateUnderAnyNameStaysAPerson(t *testing.T) {
	const md = "# factory recipe · codeaf\n\n## issue\n1. plan · chat · plan it\n2. signoff · gate\n3. write · chat · write it\n"
	f := FromMarkdown(md)
	if got := names(f.Recipe().Stages); got != "plan:chat,signoff:gate,write:chat" {
		t.Fatalf("stages = %s\n%s", got, problems(f))
	}
	data, probs, err := Convert(md)
	if err != nil {
		t.Fatal(err, probs)
	}
	back := parseClean(t, string(data))
	if got := names(back.Recipe().Stages); got != "plan:chat,signoff:gate,write:chat" {
		t.Fatalf("converted stages = %s\n%s", got, data)
	}
}

// S4: a writer refuses what it cannot hold and writes nothing.
func TestWritersRefuseWhatTheFileCannotHoldAndWriteNothing(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, RepoFile, runtimeYAML)
	path := filepath.Join(dir, RepoFile)
	before, _ := os.ReadFile(path)
	f, err := Working(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []factory.Stage{
		{Name: "write", Kind: "loop", Ask: "write it", On: true},
		{Name: "ship", Kind: factory.StagePost, Ask: "close", When: "sometimes", On: true},
		{Name: "lint", Kind: factory.StageCheck, Ask: "go vet ./...", Proof: []string{"the log"}, On: true},
		{Name: "lint", Kind: factory.StageCheck, On: true},
		{Name: "lint", Kind: factory.StageCheck, Ask: "go vet ./...", Until: "vet is quiet", On: true},
	} {
		stages := append([]factory.Stage{}, f.Recipe().Stages...)
		stages = append(stages, bad)
		if err := (Files{}).BankStages(dir, factory.KindIssue, stages); err == nil {
			t.Errorf("%+v was banked", bad)
		}
		r := f.Recipe()
		r.ByKind[factory.KindCI] = []factory.Stage{bad}
		if err := (Files{}).Save(dir, r); err == nil {
			t.Errorf("%+v was saved", bad)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatalf("a refused write changed the file:\n%s", after)
	}
	// A markdown recipe with a step that holds is not converted by any writer.
	md := t.TempDir()
	writeRepoFile(t, md, factory.RecipeFile, "# factory recipe · codeaf\n\n## issue\n1. abcdefghijkl · chat · do it · when thin\n\n## pr\n1. abcdefghijkl · chat · do it\n")
	if err := (Files{}).WriteDefault(md); err == nil {
		t.Fatal("WriteDefault converted a recipe that holds")
	}
	if err := (Files{}).BankRule(md, "be kind"); err == nil {
		t.Fatal("BankRule converted a recipe that holds")
	}
	if _, err := os.Stat(filepath.Join(md, RepoFile)); !os.IsNotExist(err) {
		t.Fatalf("a refused write left %s behind: %v", RepoFile, err)
	}
}

func writeRepoFile(t *testing.T, dir, rel, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
