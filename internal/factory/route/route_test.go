package route_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

func TestMatches(t *testing.T) {
	issue := factory.Item{Origin: factory.OriginForge, Repo: "acme/api", Title: "Login page 500s", Body: "Stack trace attached", Author: "Octocat", Labels: []string{"Bug", "p1"}}
	jira := factory.Item{Origin: "jira", SourceRef: "jira:ENG-142", Title: "Checkout slow", Author: "pat@example.com", Priority: &factory.JobPriority{Value: "urgent"}}
	inbox := factory.Item{Origin: "inbox:ops", SourceRef: "inbox:ops:A1", Title: "Rotate the key"}
	cases := []struct {
		name string
		m    route.Match
		it   factory.Item
		want bool
	}{
		{"empty matches anything", route.Match{}, inbox, true},
		{"source is the origin", route.Match{Sources: []string{"github"}}, issue, true},
		{"source is the ref's front", route.Match{Sources: []string{"jira"}}, jira, true},
		{"inbox matches every inbox", route.Match{Sources: []string{"inbox"}}, inbox, true},
		{"inbox:name matches that inbox", route.Match{Sources: []string{"inbox:ops"}}, inbox, true},
		{"another inbox does not", route.Match{Sources: []string{"inbox:sales"}}, inbox, false},
		{"a prefix of a word does not", route.Match{Sources: []string{"inbox:op"}}, inbox, false},
		{"inbox:name matches the origin alone", route.Match{Sources: []string{"inbox:ops"}}, factory.Item{Origin: "inbox:ops"}, true},
		{"jira matches the origin alone", route.Match{Sources: []string{"jira"}}, factory.Item{Origin: "jira"}, true},
		{"a source is not the issue's own", route.Match{Sources: []string{"jira"}}, issue, false},
		{"any one source", route.Match{Sources: []string{"github", "jira"}}, jira, true},
		{"repo", route.Match{Repos: []string{"ACME/api"}}, issue, true},
		{"repo, none on the item", route.Match{Repos: []string{"acme/api"}}, jira, false},
		{"label without regard to case", route.Match{Labels: []string{"bug"}}, issue, true},
		{"label missing", route.Match{Labels: []string{"docs"}}, issue, false},
		{"author handle", route.Match{Authors: []string{"@octocat"}}, issue, true},
		{"author email", route.Match{Authors: []string{"PAT@example.com"}}, jira, true},
		{"text in the title", route.Match{Text: []string{"login"}}, issue, true},
		{"text in the body", route.Match{Text: []string{"stack TRACE"}}, issue, true},
		{"text missing", route.Match{Text: []string{"billing"}}, issue, false},
		{"priority from a label", route.Match{Priority: []string{"p1"}}, issue, true},
		{"priority from a mark", route.Match{Priority: []string{"p0"}}, jira, true},
		{"no priority", route.Match{Priority: []string{"p3"}}, inbox, false},
		{"every field must hold", route.Match{Sources: []string{"github"}, Labels: []string{"docs"}}, issue, false},
	}
	for _, tc := range cases {
		if got := route.Matches(tc.m, tc.it); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// THE FIRST RULE THAT MATCHES DECIDES, and no match is ok false.
func TestDecideTakesTheFirstMatch(t *testing.T) {
	rules := []route.Rule{
		{Name: "docs", When: route.Match{Labels: []string{"docs"}}, Then: route.Action{Start: route.StartBacklog}},
		{Name: "jira", When: route.Match{Sources: []string{"jira"}}, Then: route.Action{Start: route.StartAuto}},
		{Name: "all", Then: route.Action{Start: route.StartAsk}},
	}
	if o := route.Decide(rules, factory.Item{Origin: "jira", Labels: []string{"docs"}}, nil); !o.Matched || o.Rule.Name != "docs" {
		t.Fatalf("got %+v, want docs", o)
	}
	if o := route.Decide(rules, factory.Item{Origin: "jira"}, nil); !o.Matched || o.Rule.Name != "jira" {
		t.Fatalf("got %+v, want jira", o)
	}
	if o := route.Decide(rules[:2], factory.Item{Origin: factory.OriginForge}, nil); o.Matched || o.Pending != "" {
		t.Fatalf("a rule matched an item no rule names: %+v", o)
	}
	if o := route.Decide(nil, factory.Item{}, nil); o.Matched || o.Pending != "" {
		t.Fatal("an empty table matched")
	}
}

// A QUESTION IS READ ONLY WITH A FRESH ANSWER: none or a stale one stops the
// reading with Pending, a yes matches, a no reads on to the next rule.
func TestDecideReadsAQuestionOnlyWithAFreshAnswer(t *testing.T) {
	ask := "Is this a small bug fix?"
	rules := []route.Rule{
		{Name: "small", When: route.Match{Sources: []string{"jira"}, Ask: ask}, Then: route.Action{Start: route.StartAuto}},
		{Name: "rest", Then: route.Action{Start: route.StartBacklog}},
	}
	it := factory.Item{Origin: "jira", Title: "Typo on login"}
	key := route.AnswerKey(ask, it)
	held := func(v route.Answer) route.Answers {
		return func(name string) (route.Answer, bool) { return v, name == "small" }
	}
	if o := route.Decide(rules, it, nil); o.Pending != "small" || o.Matched {
		t.Fatalf("no answer: %+v, want pending small", o)
	}
	if o := route.Decide(rules, it, held(route.Answer{Yes: true, Key: "old"})); o.Pending != "small" {
		t.Fatalf("stale answer: %+v, want pending small", o)
	}
	if o := route.Decide(rules, it, held(route.Answer{Yes: true, Why: "one-line fix", Key: key})); !o.Matched || o.Rule.Name != "small" || o.Answer == nil || o.Answer.Why != "one-line fix" {
		t.Fatalf("a yes: %+v", o)
	}
	if o := route.Decide(rules, it, held(route.Answer{Key: key})); !o.Matched || o.Rule.Name != "rest" {
		t.Fatalf("a no: %+v, want rest", o)
	}
	if o := route.Decide(rules, factory.Item{Origin: "github"}, nil); !o.Matched || o.Rule.Name != "rest" {
		t.Fatalf("a rule whose fields fail is never asked: %+v", o)
	}
}

// AnswerKey changes with the question and the item's words, and with
// nothing else; label order and case do not move it.
func TestAnswerKey(t *testing.T) {
	it := factory.Item{ID: 1, Title: "Typo", Body: "on login", Labels: []string{"Bug", "ui"}}
	k := route.AnswerKey("small?", it)
	if len(k) != 16 || k != route.AnswerKey("small?", it) {
		t.Fatalf("key %q is not a stable 16-digit hash", k)
	}
	same := it
	same.ID, same.Labels, same.State = 9, []string{"UI", "bug"}, factory.StateQueued
	if route.AnswerKey("small?", same) != k {
		t.Fatal("the key moved with something that is not the item's words")
	}
	for name, other := range map[string]factory.Item{
		"title":  {Title: "Typos", Body: it.Body, Labels: it.Labels},
		"body":   {Title: it.Title, Body: "on logout", Labels: it.Labels},
		"labels": {Title: it.Title, Body: it.Body, Labels: []string{"bug"}},
		"joined": {Title: "Typoon", Body: " login", Labels: it.Labels},
	} {
		if route.AnswerKey("small?", other) == k {
			t.Errorf("an edited %s kept the key", name)
		}
	}
	if route.AnswerKey("big?", it) == k {
		t.Error("another question kept the key")
	}
}

// Preview reads the newest jobs first, with kept answers only.
func TestPreviewTriesTheNewestJobsWithKeptAnswers(t *testing.T) {
	ask := "Is this small?"
	rules := []route.Rule{
		{Name: "small", When: route.Match{Labels: []string{"bug"}, Ask: ask}, Then: route.Action{Start: route.StartAuto}},
		{Name: "jira", When: route.Match{Sources: []string{"jira"}}, Then: route.Action{Start: route.StartAsk}},
	}
	t0 := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	judged := factory.Item{ID: 3, Repo: "acme/api", Num: 3, Title: "Typo", Labels: []string{"bug"}, Created: t0.Add(2 * time.Minute)}
	judged.Judged = map[string]factory.RouteAnswer{"small": {Yes: true, Why: "a typo", Key: route.AnswerKey(ask, judged)}}
	items := []factory.Item{
		{ID: 1, Repo: "acme/api", Num: 1, Title: "Old", Created: t0},
		{ID: 2, Origin: "jira", Title: "Ticket", Created: t0.Add(time.Minute)},
		judged,
		{ID: 4, Repo: "acme/api", Num: 4, Title: "Unjudged", Labels: []string{"bug"}, Created: t0.Add(3 * time.Minute)},
	}
	got := route.Preview(rules, items, 3)
	want := []route.Tried{
		{Job: 4, Ref: items[3].Ref(), Title: "Unjudged", Outcome: "pending", Rule: "small"},
		{Job: 3, Ref: judged.Ref(), Title: "Typo", Outcome: "matched", Rule: "small", Why: "a typo"},
		{Job: 2, Ref: items[1].Ref(), Title: "Ticket", Outcome: "matched", Rule: "jira"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preview\n%+v\nwant\n%+v", got, want)
	}
	if n := len(route.Preview(nil, make([]factory.Item, 300), 0)); n != route.DefaultPreview {
		t.Fatalf("limit 0 read %d jobs, want %d", n, route.DefaultPreview)
	}
}

func TestRepoFor(t *testing.T) {
	it := factory.Item{Repo: "acme/api"}
	if got := (route.Action{}).RepoFor(it); got != "acme/api" {
		t.Fatalf("empty action moved the repo to %q", got)
	}
	if got := (route.Action{Repo: "acme/web"}).RepoFor(it); got != "acme/web" {
		t.Fatalf("got %q, want acme/web", got)
	}
}

func TestCleanTrimsAndFolds(t *testing.T) {
	got, err := route.Clean([]route.Rule{{
		Name: " Jira FL ",
		When: route.Match{Sources: []string{" JIRA ", ""}, Priority: []string{"P0"}, Labels: []string{" bug "}},
		Then: route.Action{Repo: " acme/api ", Pipeline: " quick ", Priority: "P1", Start: " Auto "},
	}})
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.Name != "Jira FL" || len(r.When.Sources) != 1 || r.When.Sources[0] != "jira" || r.When.Priority[0] != "p0" || r.When.Labels[0] != "bug" ||
		r.Then.Repo != "acme/api" || r.Then.Pipeline != "quick" || r.Then.Priority != "p1" || r.Then.Start != route.StartAuto {
		t.Fatalf("cleaned %+v", r)
	}
	if out, err := route.Clean(nil); err != nil || out == nil || len(out) != 0 {
		t.Fatalf("an empty table cleans to %#v, %v; want [] and no error", out, err)
	}
}

// A TABLE IS REFUSED WHOLE, naming the rule that is wrong.
func TestCleanRefuses(t *testing.T) {
	ok := route.Action{Start: route.StartAuto}
	cases := map[string]struct {
		rules []route.Rule
		say   string
	}{
		"no name":         {[]route.Rule{{Then: ok}}, "needs a name"},
		"two lines":       {[]route.Rule{{Name: "a\nb", Then: ok}}, "one line"},
		"same name":       {[]route.Rule{{Name: "a", Then: ok}, {Name: "A", Then: ok}}, "two rules have this name"},
		"no start":        {[]route.Rule{{Name: "a"}}, "auto, ask or backlog"},
		"bad start":       {[]route.Rule{{Name: "a", Then: route.Action{Start: "later"}}}, "auto, ask or backlog"},
		"bad priority":    {[]route.Rule{{Name: "a", Then: route.Action{Start: route.StartAuto, Priority: "urgent"}}}, "p0, p1, p2 or p3"},
		"bad when prio":   {[]route.Rule{{Name: "a", When: route.Match{Priority: []string{"p4"}}, Then: ok}}, "p0, p1, p2 or p3"},
		"bad repo":        {[]route.Rule{{Name: "a", Then: route.Action{Start: route.StartAuto, Repo: "api"}}}, "owner/name"},
		"bad when repo":   {[]route.Rule{{Name: "a", When: route.Match{Repos: []string{"a/b/c"}}, Then: ok}}, "owner/name"},
		"bad pipeline":    {[]route.Rule{{Name: "a", Then: route.Action{Start: route.StartAuto, Pipeline: "Quick Fix"}}}, "not spelled as a pipeline"},
		"pr pipeline":     {[]route.Rule{{Name: "a", Then: route.Action{Start: route.StartAuto, Pipeline: "pr"}}}, "not an issue job's"},
		"too many":        {make([]route.Rule, route.MaxRules+1), "at most"},
		"long name":       {[]route.Rule{{Name: strings.Repeat("n", route.MaxNameLen+1), Then: ok}}, "at most"},
		"long ask":        {[]route.Rule{{Name: "a", When: route.Match{Ask: strings.Repeat("q", route.MaxAskLen+1)}, Then: ok}}, "ask is one line"},
		"two-line ask":    {[]route.Rule{{Name: "a", When: route.Match{Ask: "is it\nsmall?"}, Then: ok}}, "ask is one line"},
		"second is wrong": {[]route.Rule{{Name: "fine", Then: ok}, {Name: "broken", Then: route.Action{Start: "x"}}}, `"broken"`},
	}
	for name, tc := range cases {
		_, err := route.Clean(tc.rules)
		if err == nil || !strings.Contains(err.Error(), tc.say) {
			t.Errorf("%s: err %v, want one saying %q", name, err, tc.say)
		}
	}
}

// A PREVIEW SAYS A MATCHED RULE IS REFUSED when the file of the repository it
// sends the job to is known to list no pipeline of the rule's name, and
// matched when that file cannot be read.
func TestPreviewSaysARuleWhosePipelineIsMissingIsRefused(t *testing.T) {
	rules := []route.Rule{{Name: "docs", When: route.Match{Labels: []string{"docs"}}, Then: route.Action{Repo: "acme/api", Pipeline: "nope", Start: route.StartAuto}}}
	items := []factory.Item{{ID: 1, Repo: "acme/web", Num: 1, Kind: factory.KindIssue, Labels: []string{"docs"}}}
	known := func(repo, name string) (bool, bool) { return repo == "acme/api" && name == "docs", true }
	got := route.PreviewListed(rules, items, 0, known)
	if len(got) != 1 || got[0].Outcome != route.TriedRefused || got[0].Rule != "docs" || got[0].Reason != "routing rule docs: acme/api has no pipeline nope" {
		t.Fatalf("preview %+v", got)
	}
	unknown := func(string, string) (bool, bool) { return false, false }
	if got := route.PreviewListed(rules, items, 0, unknown); got[0].Outcome != route.TriedMatched {
		t.Fatalf("an unreadable file refused the rule: %+v", got)
	}
}
