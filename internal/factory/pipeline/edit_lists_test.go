package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── set_list, and a repository with no file ─────────────────────────────────
//
// editBase's rules are [never rewrite history], its ask_first [touching
// billing] and its never [secrets/**].

func setList(list string, lines ...string) EditOp {
	if lines == nil {
		lines = []string{}
	}
	return EditOp{Op: OpSetList, List: list, Lines: lines}
}

func TestInspectCarriesTheFilesOwnLists(t *testing.T) {
	src, _, ok := Inspect(editBase)
	if !ok {
		t.Fatal("editBase does not inspect")
	}
	if !reflect.DeepEqual(src.Rules, []string{"never rewrite history"}) || !reflect.DeepEqual(src.AskFirst, []string{"touching billing"}) ||
		!reflect.DeepEqual(src.Never, []string{"secrets/**"}) {
		t.Fatalf("rules %q ask_first %q never %q", src.Rules, src.AskFirst, src.Never)
	}
	// A file without them says so with empty lists, never with nothing.
	src, _, ok = Inspect("default_budget: 3\n")
	if !ok || src.Rules == nil || src.AskFirst == nil || src.Never == nil || len(src.Rules)+len(src.AskFirst)+len(src.Never) != 0 {
		t.Fatalf("%v %+v", ok, src)
	}
}

func TestSetListReplacesOneListWholeAndRoundTrips(t *testing.T) {
	got := mustApply(t, editBase,
		setList(ListRules, "  never rewrite history ", "Keep the changelog current"),
		setList(ListNever, "secrets/**", "infra/**", "auth/"))
	if !got.Changed || !got.Listed || got.Structural || len(got.Steps) != 0 || got.Budget != nil {
		t.Fatalf("%+v", got)
	}
	back := reread(t, got.Text)
	if !reflect.DeepEqual(back.Rules, []string{"never rewrite history", "Keep the changelog current"}) ||
		!reflect.DeepEqual(back.Start.Never, []string{"secrets/**", "infra/**", "auth/"}) ||
		!reflect.DeepEqual(back.Start.AskFirst, []string{"touching billing"}) {
		t.Fatalf("read back rules %q never %q ask_first %q", back.Rules, back.Start.Never, back.Start.AskFirst)
	}
	want := []ListChange{
		{List: ListRules, Before: []string{"never rewrite history"}, After: []string{"never rewrite history", "Keep the changelog current"}},
		{List: ListNever, Before: []string{"secrets/**"}, After: []string{"secrets/**", "infra/**", "auth/"}},
	}
	if !reflect.DeepEqual(got.Lists, want) {
		t.Fatalf("lists %+v", got.Lists)
	}
	// Everything else in the file is what it was.
	base := reread(t, editBase)
	if !reflect.DeepEqual(back.Pipeline, base.Pipeline) || back.DefaultBudget != base.DefaultBudget || !reflect.DeepEqual(back.Start.Labels, base.Start.Labels) {
		t.Fatalf("set_list touched another key")
	}
	// The runner reads the new lines: the never path holds a plan, and the
	// rule reaches every step.
	if hold := PlanHold(StartRulesOf(back), []string{"infra/main.tf"}); !strings.Contains(hold, "infra/**") {
		t.Fatalf("hold %q", hold)
	}
	if pol := back.Recipe().Policy; len(pol) != 2 || pol[1] != "Keep the changelog current" {
		t.Fatalf("policy %q", pol)
	}
}

func TestSetListClearsAList(t *testing.T) {
	got := mustApply(t, editBase, setList(ListAskFirst))
	back := reread(t, got.Text)
	if !got.Changed || len(back.Start.AskFirst) != 0 || back.Start.AskFirst == nil {
		t.Fatalf("%+v %q", got, back.Start.AskFirst)
	}
	if len(got.Lists) != 1 || got.Lists[0].List != ListAskFirst || len(got.Lists[0].After) != 0 || got.Lists[0].After == nil {
		t.Fatalf("%+v", got.Lists)
	}
}

func TestSetListToTheSameLinesIsNoChange(t *testing.T) {
	got := mustApply(t, editBase, setList(ListRules, "never rewrite history"))
	if got.Changed || got.Text != editBase || !got.Listed || got.Lists == nil || len(got.Lists) != 0 {
		t.Fatalf("%+v", got)
	}
}

// A preview without set_list answers exactly what it did before set_list.
func TestAPreviewWithoutSetListHasNoLists(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetBudget, Value: f(7)})
	if got.Listed || got.Lists != nil {
		t.Fatalf("%+v", got)
	}
}

func TestSetListMixesWithOtherEdits(t *testing.T) {
	got := mustApply(t, editBase, EditOp{Op: OpSetStep, Name: "write", Max: i(5)}, setList(ListNever, "vendor/**"),
		add("issue", "lint", "write", "run the linter"))
	back := reread(t, got.Text)
	if back.Steps["write"].Max != 5 || !reflect.DeepEqual(back.Start.Never, []string{"vendor/**"}) || !got.Structural || !got.Listed {
		t.Fatalf("%+v", got)
	}
}

func TestSetListRefusals(t *testing.T) {
	long := strings.Repeat("x", EditLineMax+1)
	many := make([]string, EditListMost+1)
	for n := range many {
		many[n] = "rule " + strings.Repeat("a", n+1)
	}
	for name, c := range map[string]struct {
		ops  []EditOp
		code string
		op   int
	}{
		"an unknown list":          {[]EditOp{setList("labels", "codeaf")}, EditBadValue, 0},
		"no list":                  {[]EditOp{{Op: OpSetList, Lines: []string{"x"}}}, EditBadValue, 0},
		"no lines":                 {[]EditOp{{Op: OpSetList, List: ListRules}}, EditBadValue, 0},
		"a blank line":             {[]EditOp{setList(ListRules, "a", "   ")}, EditBadValue, 0},
		"two lines in one":         {[]EditOp{setList(ListRules, "a\nb")}, EditBadValue, 0},
		"a carriage return":        {[]EditOp{setList(ListNever, "a\rb")}, EditBadValue, 0},
		"a tab":                    {[]EditOp{setList(ListRules, "a\tb")}, EditBadValue, 0},
		"a line separator":         {[]EditOp{setList(ListRules, "a b")}, EditBadValue, 0},
		"a nul":                    {[]EditOp{setList(ListAskFirst, "a\x00b")}, EditBadValue, 0},
		"bad utf-8":                {[]EditOp{setList(ListRules, "a\xffb")}, EditBadValue, 0},
		"a line too long":          {[]EditOp{setList(ListRules, long)}, EditBadValue, 0},
		"too many lines":           {[]EditOp{setList(ListRules, many...)}, EditBadValue, 0},
		"a rule twice by case":     {[]EditOp{setList(ListRules, "Keep it small", "keep it small")}, EditBadValue, 0},
		"an ask twice by case":     {[]EditOp{setList(ListAskFirst, "touch auth/", "Touch auth/")}, EditBadValue, 0},
		"a path twice after trim":  {[]EditOp{setList(ListNever, "auth/", " auth/ ")}, EditBadValue, 0},
		"a list set twice":         {[]EditOp{setList(ListRules, "a"), setList(ListRules, "b")}, EditBadOp, 1},
		"a list with a name":       {[]EditOp{{Op: OpSetList, List: ListRules, Lines: []string{}, Name: "write"}}, EditBadOp, 0},
		"a list with a pipeline":   {[]EditOp{{Op: OpSetList, List: ListRules, Lines: []string{}, Pipeline: "issue"}}, EditBadOp, 0},
		"a list with a position":   {[]EditOp{{Op: OpSetList, List: ListRules, Lines: []string{}, After: s("")}}, EditBadOp, 0},
		"a list with a prompt":     {[]EditOp{{Op: OpSetList, List: ListRules, Lines: []string{}, Prompt: s("x")}}, EditBadOp, 0},
		"a list with a value":      {[]EditOp{{Op: OpSetList, List: ListRules, Lines: []string{}, Value: f(3)}}, EditBadOp, 0},
		"a list with a when":       {[]EditOp{{Op: OpSetList, List: ListRules, Lines: []string{}, When: s("thin")}}, EditBadOp, 0},
		"a step edit with a list":  {[]EditOp{{Op: OpSetStep, Name: "write", Max: i(2), List: ListRules}}, EditBadOp, 0},
		"a budget with lines":      {[]EditOp{{Op: OpSetBudget, Value: f(3), Lines: []string{}}}, EditBadOp, 0},
		"an add with a list":       {[]EditOp{{Op: OpAddStep, Pipeline: "issue", Name: "lint", After: s(""), Prompt: s("x"), List: ListNever}}, EditBadOp, 0},
		"one bad list refuses all": {[]EditOp{setList(ListNever, "vendor/**"), setList(ListRules, "")}, EditBadValue, 1},
	} {
		t.Run(name, func(t *testing.T) {
			wantCode(t, refusal(t, editBase, c.ops...), c.code, c.op)
		})
	}
	// Forty lines of three hundred characters fit, and differ only by case
	// under never, where a path is exact.
	full := make([]string, EditListMost)
	for n := range full {
		full[n] = strings.Repeat("r", EditLineMax-3) + string(rune('a'+n%26)) + string(rune('a'+n/26)) + "x"
	}
	mustApply(t, editBase, setList(ListRules, full...), setList(ListNever, "Auth/", "auth/"))
}

// Text the YAML encoder has to quote is written so it reads back as given.
func TestListLinesYAMLWouldMisreadSurvive(t *testing.T) {
	lines := []string{"null", "~", "yes", "123", "- dash", "key: value", "# not a comment", "*", "'quoted'", `"double"`, "a: b # c", "@at", "%pct", "!tag", "&anchor", "[x]", "{y}"}
	got := mustApply(t, editBase, setList(ListRules, lines...), setList(ListNever, "**/*.pem", "/root/only", "a b/c"))
	back := reread(t, got.Text)
	if !reflect.DeepEqual(back.Rules, lines) || !reflect.DeepEqual(back.Start.Never, []string{"**/*.pem", "/root/only", "a b/c"}) {
		t.Fatalf("rules %q never %q", back.Rules, back.Start.Never)
	}
}

// ── no file yet ─────────────────────────────────────────────────────────────

func TestInspectDefaultsDescribesTheShippedPipelineAsNotInAnyFile(t *testing.T) {
	src := InspectDefaults()
	d := Defaults()
	if src.Budget != d.DefaultBudget || src.BudgetInFile {
		t.Fatalf("budget %v in file %v", src.Budget, src.BudgetInFile)
	}
	if len(src.Kinds) != 1 || src.Kinds[0].Kind != "issue" || src.Kinds[0].InFile || joined(src.Kinds[0].Steps) != joined(d.Pipeline["issue"]) {
		t.Fatalf("kinds %+v", src.Kinds)
	}
	if len(src.Steps) != len(d.Pipeline["issue"]) {
		t.Fatalf("steps %+v", src.Steps)
	}
	for _, st := range src.Steps {
		if st.InFile {
			t.Fatalf("%s says it is in a file", st.Name)
		}
		if st.Name == "proof" && !st.Locked || st.Name == "approve" && !(st.Gate && st.Locked) || st.Name == "plan" && (st.Locked || st.Prompt == "") {
			t.Fatalf("%+v", st)
		}
	}
	if !reflect.DeepEqual(src.Fixed, d.Fixed) || src.Rules == nil || src.AskFirst == nil || src.Never == nil || len(src.Rules)+len(src.AskFirst)+len(src.Never) != 0 {
		t.Fatalf("%+v", src)
	}
	// It is a description of the defaults, not of the file the defaults encode
	// to: that file would say everything is in it.
	enc, _ := Encode(Defaults())
	if fromFile, _, ok := Inspect(string(enc)); !ok || !fromFile.BudgetInFile {
		t.Fatalf("%v %+v", ok, fromFile)
	}
}

func TestAnEmptyBaseEditsTheShippedDefaults(t *testing.T) {
	got := mustApply(t, "", setList(ListRules, "Ask before touching billing"), setList(ListNever, "auth/"))
	if !got.Changed || got.FormatRewritten || got.CommentsDropped || got.Text == "" {
		t.Fatalf("%+v", got)
	}
	back := reread(t, got.Text)
	if !reflect.DeepEqual(back.Rules, []string{"Ask before touching billing"}) || !reflect.DeepEqual(back.Start.Never, []string{"auth/"}) {
		t.Fatalf("%q %q", back.Rules, back.Start.Never)
	}
	if !reflect.DeepEqual(got.Lists, []ListChange{
		{List: ListRules, Before: []string{}, After: []string{"Ask before touching billing"}},
		{List: ListNever, Before: []string{}, After: []string{"auth/"}},
	}) {
		t.Fatalf("%+v", got.Lists)
	}
	// The committed file runs what the repository ran with no file, and only
	// the edit besides.
	runs, ran := back.Recipe(), Defaults().Recipe()
	for _, kind := range []string{"issue", "pr", "ci", "chore"} {
		if !reflect.DeepEqual(runs.For(factory.Kind(kind)), ran.For(factory.Kind(kind))) {
			t.Fatalf("%s runs other steps than the defaults", kind)
		}
	}
	if runs.DefaultBudget != ran.DefaultBudget || !reflect.DeepEqual(back.Fixed, Defaults().Fixed) {
		t.Fatalf("budget or fixed moved")
	}
	// It is exactly the encoding of the defaults with the edit applied.
	want := Defaults()
	want.Rules, want.Start.Never = []string{"Ask before touching billing"}, []string{"auth/"}
	if enc, _ := Encode(want); string(enc) != got.Text {
		t.Fatalf("text\n%s\nwant\n%s", got.Text, enc)
	}
}

func TestAnEmptyBaseTakesEveryEdit(t *testing.T) {
	got := mustApply(t, "", EditOp{Op: OpSetBudget, Value: f(9)}, EditOp{Op: OpSetStep, Name: "write", Prompt: s("make the change carefully")},
		add("issue", "lint", "write", "run the linter"))
	back := reread(t, got.Text)
	if back.DefaultBudget != 9 || back.Steps["write"].Prompt != "make the change carefully" || joined(back.Pipeline["issue"]) != "plan,approve,write,lint,test,review,proof" {
		t.Fatalf("%+v", back)
	}
	if got.Budget == nil || got.Budget.Before != Defaults().DefaultBudget || len(got.Steps) != 1 || got.Steps[0].Name != "write" || got.Listed || got.Lists != nil {
		t.Fatalf("%+v", got)
	}
	if got.FormatRewritten || got.CommentsDropped {
		t.Fatalf("an empty base has no layout or comment to lose: %+v", got)
	}
}

func TestAnEmptyBaseThatChangesNothingOffersNothing(t *testing.T) {
	got := mustApply(t, "", setList(ListRules), EditOp{Op: OpSetBudget, Value: f(Defaults().DefaultBudget)})
	if got.Changed || got.Text != "" || got.FormatRewritten || got.CommentsDropped || len(got.Lists) != 0 || got.Budget != nil {
		t.Fatalf("%+v", got)
	}
}

func TestAnEmptyBaseStillRefusesABadEdit(t *testing.T) {
	for name, c := range map[string]struct {
		op   EditOp
		code string
	}{
		"a locked step":  {EditOp{Op: OpSetStep, Name: "proof", Prompt: s("x")}, EditLocked},
		"approve":        {EditOp{Op: OpSetStep, Name: "approve", Prompt: s("x")}, EditLocked},
		"not listed":     {EditOp{Op: OpSetStep, Name: "security", Prompt: s("x")}, EditUnknownStep},
		"no pipeline":    {remove("pr", "review"), EditUnknownStep},
		"remove proof":   {remove("issue", "proof"), EditLocked},
		"a bad budget":   {EditOp{Op: OpSetBudget, Value: f(-1)}, EditBadValue},
		"a bad list":     {setList("labels", "x"), EditBadValue},
		"a blank line":   {setList(ListNever, " "), EditBadValue},
		"no edit at all": {EditOp{Op: "reorder"}, EditBadOp},
	} {
		t.Run(name, func(t *testing.T) {
			e := refusal(t, "", c.op)
			if e.Code != c.code {
				t.Fatalf("%s (%s), want %s", e.Code, e.Why, c.code)
			}
		})
	}
	if e := refusal(t, ""); e.Code != EditBadOp {
		t.Fatalf("%+v", e)
	}
	// Only "" is no file. A file of blanks is a blank file, as it always was.
	if e := refusal(t, " \n", setList(ListRules, "x")); e.Code != EditBaseInvalid {
		t.Fatalf("%+v", e)
	}
}
