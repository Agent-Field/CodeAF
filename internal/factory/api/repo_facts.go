package api

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Fact is one figure about a repository, worked out here so a model that
// writes about it never counts: a stable id to cite, the number, and the
// fact's own phrase, which never shows a number without its noun. It is the
// desk's fact shape (id, kind, value, phrase, about, source); every fact the
// factory answers carries a number and a source beginning "engine:".
type Fact struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"`
	Value  float64 `json:"value"`
	Phrase string  `json:"phrase"`
	About  string  `json:"about,omitempty"`
	Source string  `json:"source"`
}

// RepoFacts is GET /v1/repos/{owner}/{repo}/facts: one repository's facts
// counted over every job document the factory keeps, as of as_of in the
// factory's zone. Days is the recent window (calendar days ending today) and
// Months how many calendar months of spend, this one included. Truncated is
// how many areas and stuck steps the bounds left out.
type RepoFacts struct {
	Repo      string    `json:"repo"`
	AsOf      time.Time `json:"as_of"`
	TimeZone  string    `json:"time_zone,omitempty"`
	Days      int       `json:"days"`
	Months    int       `json:"months"`
	Facts     []Fact    `json:"facts"`
	Truncated int       `json:"truncated,omitempty"`
}

// The read's bounds. A window is at most a spend read's days and the spend
// at most a year of months. At most RepoFactsAreasMost areas (three facts
// each) and RepoFactsAreasMost stuck steps are answered, the busiest first,
// so an answer holds at most 4 + 12 + 1 + 20 + 60 = 97 facts.
const (
	RepoFactsDaysDefault   = 30
	RepoFactsMonthsDefault = 3
	RepoFactsMonthsMost    = 12
	RepoFactsAreasMost     = 20
)

const stuckAfter = 24 * time.Hour

// window is a span of the factory's days up to now, with the words a phrase
// ends on and the suffix its ids carry.
type window struct {
	start, end time.Time
	id, words  string
}

func (w window) holds(t time.Time) bool { return !t.Before(w.start) && !t.After(w.end) }

func (w window) about(what string) string {
	return what + ", " + w.start.Format(time.DateOnly) + " to " + w.end.Format(time.DateOnly)
}

// RepoFactsOf counts repo's facts from items as of now, in now's zone. areas
// are what the repository declares, for each job's area ([factory.JobArea]).
func RepoFactsOf(items []factory.Item, repo string, areas []string, now time.Time, days, months int) RepoFacts {
	repo = strings.ToLower(strings.TrimSpace(repo))
	var mine []factory.Item
	for _, it := range items {
		if strings.EqualFold(factory.RepoOf(it), repo) {
			mine = append(mine, it)
		}
	}
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	recent := window{start: today.AddDate(0, 0, 1-days), end: now, id: fmt.Sprintf("%dd", days), words: "in the last day"}
	if days > 1 {
		recent.words = fmt.Sprintf("in the last %d days", days)
	}
	month := window{start: today.AddDate(0, 0, 1-d), end: now, id: "month", words: "in " + now.Format("January 2006")}

	out := RepoFacts{Repo: repo, AsOf: now, Days: days, Months: months}
	out.Facts = []Fact{finishedFact(mine, recent), finishedFact(mine, month), mergedFact(mine, recent), mergedFact(mine, month)}
	out.Facts = append(out.Facts, spendFacts(mine, month.start, months)...)
	stuck, cut := stuckFacts(mine, now)
	out.Facts = append(out.Facts, stuck...)
	area, cutAreas := areaFacts(mine, areas, recent)
	out.Facts = append(out.Facts, area...)
	out.Truncated = cut + cutAreas
	return out
}

func finishedFact(items []factory.Item, w window) Fact {
	n := 0
	for _, it := range items {
		if factory.IsIssueJob(it) && it.State == factory.StateShipped && w.holds(factory.ShippedAt(it)) {
			n++
		}
	}
	return Fact{ID: "n.finished." + w.id, Kind: "count", Value: float64(n), Phrase: counted(n, "job", "jobs") + " finished " + w.words,
		About: w.about("jobs shipped, by the day their run ended"), Source: "engine:repo_facts.finished"}
}

func mergedFact(items []factory.Item, w window) Fact {
	n := 0
	for _, it := range items {
		if pr := it.PullRequest; pr != nil && pr.State == factory.PRMerged && !pr.MergedAt.IsZero() && w.holds(pr.MergedAt) {
			n++
		}
	}
	return Fact{ID: "n.merged." + w.id, Kind: "count", Value: float64(n), Phrase: counted(n, "pull request", "pull requests") + " merged " + w.words,
		About: w.about("pull requests the factory opened that merged"), Source: "engine:repo_facts.merged"}
}

// spendFacts is each of the months calendar months ending with the one that
// starts at thisMonth, oldest first, by the placement rule of
// factory.spend_by_repo ([factory.SpentIn]).
func spendFacts(items []factory.Item, thisMonth time.Time, months int) []Fact {
	facts := make([]Fact, 0, months)
	for back := months - 1; back >= 0; back-- {
		start := thisMonth.AddDate(0, -back, 0)
		var usd float64
		jobs := 0
		for _, it := range items {
			if spent, in := factory.SpentIn(it, start, start.AddDate(0, 1, 0)); in {
				usd += spent
				jobs++
			}
		}
		facts = append(facts, Fact{ID: "usd.month:" + start.Format("2006-01"), Kind: "usd", Value: usd,
			Phrase: fmt.Sprintf("$%.2f spent on %s in %s", usd, counted(jobs, "job", "jobs"), start.Format("January 2006")),
			About:  "each job's whole spend on the day its run started", Source: "engine:repo_facts.spend"})
	}
	return facts
}

// stuckFacts counts the open jobs whose current step began a day or more
// before now, in all and by step.
func stuckFacts(items []factory.Item, now time.Time) ([]Fact, int) {
	const about, source = "open jobs whose current step began 24 hours or more ago", "engine:repo_facts.stuck"
	steps := map[string]int{}
	total := 0
	for _, it := range items {
		if step, since, ok := stepSince(it); ok && now.Sub(since) >= stuckAfter {
			steps[step]++
			total++
		}
	}
	facts := []Fact{{ID: "n.stuck", Kind: "count", Value: float64(total), Phrase: counted(total, "job", "jobs") + " stuck on one step for a day or more", About: about, Source: source}}
	names, cut := busiest(steps)
	for _, step := range names {
		n := steps[step]
		facts = append(facts, Fact{ID: "n.stuck." + step, Kind: "count", Value: float64(n),
			Phrase: counted(n, "job", "jobs") + " stuck in " + step + " for a day or more", About: about, Source: source})
	}
	return facts, cut
}

// stepSince is an open job's current step and when it began: its first
// recorded round, or its latest round's start when it kept no earlier one.
func stepSince(it factory.Item) (string, time.Time, bool) {
	st := it.Stream
	open := it.State == factory.StateRunning || it.State == factory.StateNeedsYou
	if !open || !factory.IsIssueJob(it) || st == nil || st.Cur < 0 || st.Cur >= len(st.Phases) {
		return "", time.Time{}, false
	}
	p := st.Phases[st.Cur]
	since := p.Started
	for _, r := range p.Rounds {
		if !r.Started.IsZero() && (since.IsZero() || r.Started.Before(since)) {
			since = r.Started
		}
	}
	return p.Name, since, p.Name != "" && !since.IsZero()
}

type areaTally struct{ jobs, rounds, asks int }

// areaFacts is, per area, the jobs whose spend is placed in w, the rounds
// their steps ran and the questions they asked a person.
func areaFacts(items []factory.Item, areas []string, w window) ([]Fact, int) {
	const source = "engine:repo_facts.area"
	tally := map[string]areaTally{}
	jobs := map[string]int{}
	for _, it := range items {
		area := jobArea(it, areas)
		if area == "" || !factory.IsIssueJob(it) || !w.holds(factory.SpentAt(it)) {
			continue
		}
		t := tally[area]
		t.jobs, t.rounds, t.asks = t.jobs+1, t.rounds+jobRounds(it), t.asks+jobAsks(it)
		tally[area], jobs[area] = t, t.jobs
	}
	names, cut := busiest(jobs)
	var facts []Fact
	for _, area := range names {
		t := tally[area]
		facts = append(facts, Fact{ID: "area:" + area, Kind: "area", Value: float64(t.jobs), Phrase: area + ": " + counted(t.jobs, "job", "jobs") + " " + w.words,
			About: "jobs whose run started in the window, by the area their change touched most", Source: source})
		if t.rounds > 0 {
			facts = append(facts, Fact{ID: "area:" + area + ".rounds", Kind: "rounds", Value: float64(t.rounds),
				Phrase: counted(t.rounds, "round", "rounds") + " in " + area + " " + w.words, About: "rounds every step of those jobs ran", Source: source})
		}
		if t.asks > 0 {
			facts = append(facts, Fact{ID: "area:" + area + ".asks", Kind: "count", Value: float64(t.asks),
				Phrase: counted(t.asks, "question", "questions") + " asked in " + area + " " + w.words, About: "questions those jobs asked a person, answered or open", Source: source})
		}
	}
	return facts, cut
}

func jobRounds(it factory.Item) int {
	n := 0
	if it.Stream != nil {
		for _, p := range it.Stream.Phases {
			n += max(p.Round, 0)
		}
	}
	return n
}

func jobAsks(it factory.Item) int {
	n := len(it.AnsweredQuestions)
	if factory.NeedsKind(it) == factory.NeedsQuestion {
		n++
	}
	return n
}

// busiest is counts' names, most first and then by name, kept to
// [RepoFactsAreasMost], and how many it left out.
func busiest(counts map[string]int) ([]string, int) {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Or(counts[b]-counts[a], strings.Compare(a, b)) })
	if len(names) <= RepoFactsAreasMost {
		return names, 0
	}
	return names[:RepoFactsAreasMost], len(names) - RepoFactsAreasMost
}

func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
