package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A HOUSE RULE ONLY ADDS: the repository's rules and start limits stay, the
// factory's join them once, the repository's text is untouched, and the read
// it was given is not changed in place.
func TestHouseRulesAddToARepositoryAndDropNothing(t *testing.T) {
	repo := Defaults()
	repo.Rules = []string{"tests pass before anything posts"}
	repo.Start.AskFirst = []string{"touch billing/"}
	repo.Start.Never = []string{"secrets/"}
	repo.Text = "rules: [tests pass before anything posts]\n"
	house := factory.HouseRules{
		Rules:    []string{"Tests pass before anything posts", "don't touch auth without checking with me"},
		AskFirst: []string{"touch auth/"},
		Never:    []string{"infra/"},
	}
	got := WithHouseRules(repo, house)
	if want := []string{"tests pass before anything posts", "don't touch auth without checking with me"}; !reflect.DeepEqual(got.Rules, want) {
		t.Errorf("rules %q, want %q", got.Rules, want)
	}
	if want := []string{"touch billing/", "touch auth/"}; !reflect.DeepEqual(got.Start.AskFirst, want) {
		t.Errorf("ask_first %q, want %q", got.Start.AskFirst, want)
	}
	if want := []string{"secrets/", "infra/"}; !reflect.DeepEqual(got.Start.Never, want) {
		t.Errorf("never %q, want %q", got.Start.Never, want)
	}
	if got.Text != repo.Text || !reflect.DeepEqual(got.House, house) {
		t.Errorf("text %q, house %#v", got.Text, got.House)
	}
	if len(repo.Rules) != 1 || len(repo.Start.AskFirst) != 1 || !repo.House.Empty() {
		t.Fatalf("the repository read was changed in place: %#v", repo)
	}
	if empty := WithHouseRules(repo, factory.HouseRules{}); !reflect.DeepEqual(empty.Rules, repo.Rules) || !empty.House.Empty() {
		t.Fatalf("no house rules changed the read: %#v", empty)
	}
}

// A PLAN'S FILES ARE CHECKED AGAINST THE RULES A JOB STARTED UNDER: a path an
// ask-first rule or a never line names holds the approval, in one wording
// (`protected · <path> · <file>`), a sentence rule says nothing here, and no
// rules or no files hold nothing.
func TestAPlanThatTouchesAGuardedPathIsHeld(t *testing.T) {
	rules := &factory.StartRules{AskFirst: []string{"don't touch auth without checking with me", "touch auth/"}, Never: []string{"infra/"}}
	if got := PlanHold(rules, []string{"cmd/main.go", "internal/auth/login.go"}); got != "protected · auth/ · internal/auth/login.go" {
		t.Errorf("auth plan held as %q", got)
	}
	if got := PlanHold(rules, []string{"infra/deploy.yaml"}); got != "protected · infra/ · infra/deploy.yaml" {
		t.Errorf("infra plan held as %q", got)
	}
	if got := PlanHold(rules, []string{"cmd/main.go"}); got != "" {
		t.Errorf("an unguarded plan was held: %q", got)
	}
	if PlanHold(nil, []string{"internal/auth/login.go"}) != "" {
		t.Error("a job with no rules was held")
	}
	if got := PlanHold(rules, nil); got != "protected · auth/, infra/ · the plan names no files" {
		t.Errorf("a plan with unknown files under a path rule held as %q", got)
	}
	twice := &factory.StartRules{AskFirst: []string{"touch auth/"}, Never: []string{"auth/"}}
	if got := PlanHold(twice, nil); got != "protected · auth/ · the plan names no files" {
		t.Errorf("a path two rules protect named as %q", got)
	}
	if got := PlanHold(&factory.StartRules{AskFirst: []string{"don't touch auth without checking with me"}, Never: []string{}}, nil); got != "" {
		t.Errorf("a plan with unknown files under only a sentence rule held as %q", got)
	}
	// A PATH KEEPS ITS CASE: only the prefix is read without it.
	upper := &factory.StartRules{AskFirst: []string{"Touch src/Auth/"}, Never: []string{}}
	if got := PlanHold(upper, []string{"src/Auth/login.go"}); got != "protected · src/Auth/ · src/Auth/login.go" {
		t.Errorf("a capitalised path held as %q", got)
	}
	f := WithHouseRules(Defaults(), factory.HouseRules{AskFirst: []string{"touch auth/"}})
	if got := StartRulesOf(f); !reflect.DeepEqual(got.AskFirst, []string{"touch auth/"}) || got.Never == nil {
		t.Errorf("start rules %#v", got)
	}
}

// THE ADAPT WORD: a file's `adapt:` reaches every pipeline's recipe and is
// written back by the encoder, a personal file or the factory may tighten it
// and never loosen it, and a word that is not one is a problem, not a default.
// A BARE NAME GUARDS ITS FOLDER: `touch auth` asks about auth/keys.go as
// `touch auth/` does, at a start and in a plan, and a never line named the
// same way holds the same; a name that is only a prefix of a folder does not.
func TestABareNameInAPathRuleCoversItsFolder(t *testing.T) {
	rules := &factory.StartRules{AskFirst: []string{"touch auth"}, Never: []string{}}
	if got := PlanHold(rules, []string{"auth/keys.go"}); got != "protected · auth · auth/keys.go" {
		t.Errorf("touch auth held auth/keys.go as %q", got)
	}
	if got := PlanHold(rules, []string{"authors/list.md"}); got != "" {
		t.Errorf("touch auth held authors/list.md as %q", got)
	}
	never := &factory.StartRules{AskFirst: []string{}, Never: []string{"infra"}}
	if got := PlanHold(never, []string{"infra/main.tf"}); !strings.Contains(got, "infra/main.tf") {
		t.Errorf("never infra held infra/main.tf as %q", got)
	}
	f, err := Parse("start:\n  labels: [ready]\n  ask_first: [touch auth]\n")
	if err != nil {
		t.Fatal(err)
	}
	d := f.DecideJob(factory.Item{Kind: factory.KindIssue, Labels: []string{"ready", "size:S"}, Cap: 1}, []string{"auth/keys.go"})
	if !d.Ask || !strings.Contains(strings.Join(d.Reasons, "; "), "protected · auth · auth/keys.go") {
		t.Errorf("a start that names auth/keys.go under touch auth = %+v", d)
	}
}

func TestTheAdaptWordIsReadWrittenAndOnlyTightened(t *testing.T) {
	f, err := Parse("adapt: ask\npipeline:\n  issue: [plan, approve, write, test, review, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	if f.Adapt != factory.AdaptAsk || f.Recipe().AdaptFor(factory.KindIssue) != factory.AdaptAsk {
		t.Fatalf("adapt %q, recipe %q, problems %v", f.Adapt, f.Recipe().AdaptFor(factory.KindIssue), f.Problems)
	}
	data, err := Encode(f)
	if err != nil || !strings.Contains(string(data), "adapt: ask") {
		t.Fatalf("encoded %q, %v", data, err)
	}
	if bad, _ := Parse("adapt: sometimes\n"); len(bad.Problems) == 0 || bad.Adapt != "" {
		t.Fatalf("a bad word read as %q with %v", bad.Adapt, bad.Problems)
	}
	if got := WithHouseRules(f, factory.HouseRules{Adapt: factory.AdaptFixed}); got.Adapt != factory.AdaptFixed {
		t.Fatalf("the factory could not tighten to fixed: %q", got.Adapt)
	}
	if got := WithHouseRules(f, factory.HouseRules{Adapt: factory.AdaptFree}); got.Adapt != factory.AdaptAsk {
		t.Fatalf("the factory loosened ask to %q", got.Adapt)
	}
	if got := WithHouseRules(Defaults(), factory.HouseRules{Adapt: factory.AdaptAsk}); got.Recipe().AdaptFor(factory.KindIssue) != factory.AdaptAsk {
		t.Fatalf("a repository with no file did not take the factory's ask: %q", got.Recipe().AdaptFor(factory.KindIssue))
	}
}

// THE FACTORY'S START LABELS COUNT ONLY WHILE START ON ITS OWN IS ON: on, they
// join the repository's own; off, they let nothing more start.
func TestTheFactorysStartLabelsCountOnlyWhileItsSwitchIsOn(t *testing.T) {
	repo := Defaults()
	repo.Start.Labels = []string{"agent-ready"}
	off := WithHouseRules(repo, factory.HouseRules{StartLabels: []string{"codeaf"}})
	if !reflect.DeepEqual(off.Start.Labels, []string{"agent-ready"}) {
		t.Fatalf("labels with the switch off: %q", off.Start.Labels)
	}
	on := WithHouseRules(repo, factory.HouseRules{StartOnItsOwn: true, StartLabels: []string{"codeaf", "Agent-Ready"}})
	if !reflect.DeepEqual(on.Start.Labels, []string{"agent-ready", "codeaf"}) {
		t.Fatalf("labels with the switch on: %q", on.Start.Labels)
	}
}

// A PROTECTED PATH HOLDS IN ONE WORDING WHEREVER IT IS CHECKED: an ask-first
// path line and a never path named by the issue at a start, a plan's file and
// a change's file all read `protected · <path> · <file>`; a sentence keeps its
// own words.
func TestAProtectedPathHoldsInOneWordingAtStartPlanAndChange(t *testing.T) {
	f, err := Parse("start:\n  labels: [ready]\n  ask_first: [touch auth/, change the schema]\n  never: [infra/]\n")
	if err != nil {
		t.Fatal(err)
	}
	d := f.DecideJob(factory.Item{Kind: factory.KindIssue, Labels: []string{"ready", "size:S"}, Cap: 1}, []string{"auth/keys.go", "infra/main.tf"})
	if want := []string{"protected · auth/ · auth/keys.go", "protected · infra/ · infra/main.tf", "ask first · change the schema"}; !reflect.DeepEqual(d.Reasons, want) {
		t.Fatalf("start reasons %q, want %q", d.Reasons, want)
	}
	rules := StartRulesOf(f)
	for _, tc := range []struct{ file, want string }{{"auth/keys.go", "protected · auth/ · auth/keys.go"}, {"infra/main.tf", "protected · infra/ · infra/main.tf"}} {
		if got := PlanHold(rules, []string{tc.file}); got != tc.want {
			t.Errorf("plan hold %q, want %q", got, tc.want)
		}
		if got := ChangeHold(rules, []string{tc.file}); got != tc.want {
			t.Errorf("change hold %q, want %q", got, tc.want)
		}
	}
}

// The dry run's Decide and real intake's DecideJob read an ask-first path line
// alike: a protected path held by its own reason, never an unjudged sentence.
func TestDecideAndDecideJobAgreeOnAPathLine(t *testing.T) {
	f, err := Parse("start:\n  labels: [ready]\n  ask_first: [touch auth/]\n  never: [infra/]\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, files := range [][]string{{"auth/keys.go", "infra/main.tf"}, {"docs/readme.md"}} {
		intake := f.DecideJob(factory.Item{Kind: factory.KindIssue, Labels: []string{"ready", "size:S"}, Cap: 1}, files)
		dry := f.Decide(Job{Kind: factory.KindIssue, Labels: []string{"ready", "size:S"}, Size: "S", Paths: files})
		if !reflect.DeepEqual(intake, dry) {
			t.Errorf("%v: intake %+v, dry run %+v", files, intake, dry)
		}
	}
}
