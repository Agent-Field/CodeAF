package api

import (
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A REPOSITORY'S FACTS ARE COUNTED FROM EVERY JOB THE FACTORY KEEPS, each
// number with its noun. On 20 November with a 30-day window (22 October to
// 20 November) and two months of spend: job 1 finished and merged inside the
// window, job 2 finished in early October and counts only in October's spend,
// job 3 has sat on its review step since 17 November and asks a question, job
// 4 changed no file and has no area, job 5 started this morning, and the other
// repository's job is not counted anywhere.
func TestRepoFactsCountFinishedMergedSpendAreasAndStuckStepsForOneRepo(t *testing.T) {
	day := func(month time.Month, d, hour int) time.Time {
		return time.Date(2026, month, d, hour, 0, 0, 0, time.UTC)
	}
	files := func(paths ...string) *factory.Change {
		c := &factory.Change{}
		for _, p := range paths {
			c.Files = append(c.Files, factory.ChangedFile{Path: p})
		}
		return c
	}
	merged := func(at time.Time) *factory.PullRequest {
		return &factory.PullRequest{State: factory.PRMerged, MergedAt: at}
	}
	items := []factory.Item{
		{ID: 1, Repo: "acme/api", State: factory.StateShipped, Change: files("webhooks/a.go", "webhooks/b.go"), PullRequest: merged(day(time.October, 27, 9)),
			AnsweredQuestions: []factory.Question{{}},
			Stream:            &factory.Stream{Started: day(time.October, 25, 10), Ended: day(time.October, 26, 10), Spent: 2, Phases: []factory.Phase{{Name: "plan", Round: 1}, {Name: "code", Round: 3}}}},
		{ID: 2, Repo: "acme/api", State: factory.StateShipped, Change: files("cli/x.go"), PullRequest: merged(day(time.October, 7, 9)),
			Stream: &factory.Stream{Started: day(time.October, 5, 10), Ended: day(time.October, 6, 10), Spent: 1.5, Phases: []factory.Phase{{Name: "code", Round: 1}}}},
		{ID: 3, Repo: "acme/api", State: factory.StateNeedsYou, Question: "which database?", Change: files("webhooks/c.go"),
			Stream: &factory.Stream{Started: day(time.November, 2, 10), Spent: 0.5, Cur: 1, Phases: []factory.Phase{
				{Name: "plan", Round: 1},
				{Name: "review", Round: 2, Started: day(time.November, 18, 8), Rounds: []factory.RoundLine{{Round: 1, Started: day(time.November, 17, 8)}}},
			}}},
		{ID: 4, Repo: "acme/api", State: factory.StateShipped, PullRequest: merged(day(time.November, 11, 9)),
			Stream: &factory.Stream{Started: day(time.November, 9, 10), Ended: day(time.November, 10, 10), Spent: 0.25}},
		{ID: 5, Repo: "acme/api", State: factory.StateRunning, Change: files("cli/y.go"),
			Stream: &factory.Stream{Started: day(time.November, 20, 6), Spent: 0.25, Phases: []factory.Phase{{Name: "code", Round: 1, Started: day(time.November, 20, 6)}}}},
		{ID: 6, Repo: "acme/web", State: factory.StateShipped, Change: files("webhooks/z.go"), PullRequest: merged(day(time.November, 2, 9)),
			Stream: &factory.Stream{Started: day(time.November, 1, 10), Ended: day(time.November, 1, 12), Spent: 9}},
	}
	now := day(time.November, 20, 12)
	got := RepoFactsOf(items, []factory.Repo{{Name: "acme/api", Areas: []string{"webhooks"}}}, "ACME/api", now, 30, 2)
	want := RepoFacts{Repo: "acme/api", AsOf: now, Days: 30, Months: 2, Facts: []Fact{
		{ID: "n.finished.30d", Kind: "count", Value: 2, Phrase: "2 jobs finished in the last 30 days", About: "jobs shipped, by the day their run ended, 2026-10-22 to 2026-11-20", Source: "engine:repo_facts.finished"},
		{ID: "n.finished.month", Kind: "count", Value: 1, Phrase: "1 job finished in November 2026", About: "jobs shipped, by the day their run ended, 2026-11-01 to 2026-11-20", Source: "engine:repo_facts.finished"},
		{ID: "n.merged.30d", Kind: "count", Value: 2, Phrase: "2 pull requests merged in the last 30 days", About: "pull requests the factory opened that merged, 2026-10-22 to 2026-11-20", Source: "engine:repo_facts.merged"},
		{ID: "n.merged.month", Kind: "count", Value: 1, Phrase: "1 pull request merged in November 2026", About: "pull requests the factory opened that merged, 2026-11-01 to 2026-11-20", Source: "engine:repo_facts.merged"},
		{ID: "usd.month:2026-10", Kind: "usd", Value: 3.5, Phrase: "$3.50 spent on 2 jobs in October 2026", About: "each job's whole spend on the day its run started", Source: "engine:repo_facts.spend"},
		{ID: "usd.month:2026-11", Kind: "usd", Value: 1, Phrase: "$1.00 spent on 3 jobs in November 2026", About: "each job's whole spend on the day its run started", Source: "engine:repo_facts.spend"},
		{ID: "n.stuck", Kind: "count", Value: 1, Phrase: "1 job stuck on one step for a day or more", About: "open jobs whose current step began 24 hours or more ago", Source: "engine:repo_facts.stuck"},
		{ID: "n.stuck.review", Kind: "count", Value: 1, Phrase: "1 job stuck in review for a day or more", About: "open jobs whose current step began 24 hours or more ago", Source: "engine:repo_facts.stuck"},
		{ID: "area:webhooks", Kind: "area", Value: 2, Phrase: "webhooks: 2 jobs in the last 30 days", About: "jobs whose run started in the window, by the area their change touched most", Source: "engine:repo_facts.area"},
		{ID: "area:webhooks.rounds", Kind: "rounds", Value: 7, Phrase: "7 rounds in webhooks in the last 30 days", About: "rounds every step of those jobs ran", Source: "engine:repo_facts.area"},
		{ID: "area:webhooks.asks", Kind: "count", Value: 2, Phrase: "2 questions asked in webhooks in the last 30 days", About: "questions those jobs asked a person, answered or open", Source: "engine:repo_facts.area"},
		{ID: "area:cli/", Kind: "area", Value: 1, Phrase: "cli/: 1 job in the last 30 days", About: "jobs whose run started in the window, by the area their change touched most", Source: "engine:repo_facts.area"},
		{ID: "area:cli/.rounds", Kind: "rounds", Value: 1, Phrase: "1 round in cli/ in the last 30 days", About: "rounds every step of those jobs ran", Source: "engine:repo_facts.area"},
	}}
	if !reflect.DeepEqual(got, want) {
		for i := range max(len(got.Facts), len(want.Facts)) {
			var g, w Fact
			if i < len(got.Facts) {
				g = got.Facts[i]
			}
			if i < len(want.Facts) {
				w = want.Facts[i]
			}
			if g != w {
				t.Errorf("fact %d:\n got %+v\nwant %+v", i, g, w)
			}
		}
		t.Fatalf("repo facts = %+v", got)
	}
}

// A REPOSITORY WITH NOTHING IN IT STILL SAYS SO: the counts and the months are
// there as zero, and no area or stuck step is invented.
func TestRepoFactsOfAnEmptyRepoAreZeroWithTheirNouns(t *testing.T) {
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	got := RepoFactsOf(nil, nil, "acme/api", now, 1, 1)
	var phrases []string
	for _, f := range got.Facts {
		phrases = append(phrases, f.Phrase)
	}
	want := []string{
		"0 jobs finished in the last day", "0 jobs finished in November 2026",
		"0 pull requests merged in the last day", "0 pull requests merged in November 2026",
		"$0.00 spent on 0 jobs in November 2026", "0 jobs stuck on one step for a day or more",
	}
	if !reflect.DeepEqual(phrases, want) || got.Facts == nil {
		t.Fatalf("empty repo phrases = %q, want %q", phrases, want)
	}
}

// THE ANSWER IS BOUNDED: at most [RepoFactsAreasMost] areas and as many stuck
// steps, the busiest first, and truncated says how many were left out.
func TestRepoFactsKeepTheBusiestAreasAndSayHowManyWereLeftOut(t *testing.T) {
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	var items []factory.Item
	for i := range RepoFactsAreasMost + 3 {
		n := 1
		if i == RepoFactsAreasMost+2 {
			n = 2 // the last area named is the busiest
		}
		for range n {
			items = append(items, factory.Item{Repo: "acme/api", State: factory.StateShipped, Created: now.Add(-time.Hour),
				Change: &factory.Change{Files: []factory.ChangedFile{{Path: string(rune('a'+i)) + "/x.go"}}}})
		}
	}
	got := RepoFactsOf(items, nil, "acme/api", now, 30, 1)
	areas := 0
	for _, f := range got.Facts {
		if f.Kind == "area" {
			areas++
		}
	}
	if areas != RepoFactsAreasMost || got.Truncated != 3 {
		t.Fatalf("areas = %d truncated = %d, want %d and 3", areas, got.Truncated, RepoFactsAreasMost)
	}
	if first := got.Facts[len(got.Facts)-areas]; first.ID != "area:"+string(rune('a'+RepoFactsAreasMost+2))+"/" {
		t.Fatalf("first area fact = %+v, want the busiest", first)
	}
}
