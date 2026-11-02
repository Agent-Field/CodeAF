package estimate_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/estimate"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func rubric(s ...int) *factory.Rubric {
	r := &factory.Rubric{From: factory.RubricFromRead}
	for i, f := range r.Factors() {
		f.Score = s[i]
	}
	return r
}

// finished is a job that ran from start for minutes and spent usd.
func finished(id int, repo string, r *factory.Rubric, usd float64, start time.Time, minutes int) factory.Item {
	return factory.Item{ID: id, Repo: repo, Kind: factory.KindIssue, State: factory.StateShipped,
		Triage:  factory.Triage{Rubric: r},
		Stream:  &factory.Stream{Spent: usd, Started: start, Ended: start.Add(time.Duration(minutes) * time.Minute)},
		DoneHow: "answered"}
}

func open(repo string, r *factory.Rubric) factory.Item {
	return factory.Item{ID: 999, Repo: repo, Kind: factory.KindIssue, State: factory.StateNew, Triage: factory.Triage{Rubric: r}}
}

func TestTheLedgerIsTheStoresFinishedJobs(t *testing.T) {
	items := []factory.Item{
		finished(1, "acme/api", rubric(1, 0, 1, 0, 0), 0.8, t0, 20),
		{ID: 2, Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateRunning, Stream: &factory.Stream{Spent: 3, Started: t0}},
		{ID: 3, Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew},
	}
	done := estimate.Ledger(items)
	if len(done) != 1 || done[0].ID != 1 || done[0].Spent != 0.8 || done[0].Wall != 20*time.Minute || done[0].Band != factory.BandSmall {
		t.Fatalf("ledger = %+v, want the one ended job", done)
	}
}

func TestFewerThanFiveSimilarJobsGiveNoFigure(t *testing.T) {
	var items []factory.Item
	for i := range 4 {
		items = append(items, finished(i+1, "acme/api", rubric(1, 0, 1, 0, 0), 1, t0, 10))
	}
	if r, ok := estimate.Open(items).For(open("acme/api", rubric(1, 0, 1, 0, 0))); ok {
		t.Fatalf("four jobs gave a figure: %+v", r)
	}
}

func TestTheRangeIsTheMedianToThe80thPercentileOfTheNearestJobsInTheRepo(t *testing.T) {
	var items []factory.Item
	for i, usd := range []float64{0.2, 0.4, 0.5, 0.6, 0.9, 1.1, 2.0, 0.3, 0.7, 1.0} {
		items = append(items, finished(i+1, "acme/api", rubric(1, 0, 1, 0, 0), usd, t0, 10*(i+1)))
	}
	// A far job (large) and jobs in another repo are not its neighbours.
	items = append(items, finished(50, "acme/api", rubric(2, 2, 2, 2, 2), 40, t0, 300))
	for i := range 6 {
		items = append(items, finished(60+i, "acme/web", rubric(1, 0, 1, 0, 0), 9, t0, 5))
	}
	r, ok := estimate.Open(items).For(open("acme/api", rubric(1, 1, 1, 0, 0)))
	if !ok {
		t.Fatal("ten similar jobs gave no figure")
	}
	// Sorted spends: 0.2 0.3 0.4 0.5 0.6 0.7 0.9 1.0 1.1 2.0 → median 0.6, p80 1.0.
	if r.N != 10 || r.Repo != "acme/api" || r.USDLo != 0.6 || r.USDHi != 1.0 {
		t.Fatalf("range = %+v, want $0.60–1.00 from 10 jobs in acme/api", r)
	}
	// Minutes 10..100 → median 50, p80 80.
	if r.MinLo != 50 || r.MinHi != 80 {
		t.Fatalf("minutes = %v–%v, want 50–80", r.MinLo, r.MinHi)
	}
}

func TestARepoWithoutHistoryBorrowsFromAllRepos(t *testing.T) {
	var items []factory.Item
	for i := range 6 {
		items = append(items, finished(i+1, "acme/web", rubric(0, 0, 0, 0, 0), 1, t0, 5))
	}
	r, ok := estimate.Open(items).For(open("acme/new", rubric(0, 0, 1, 0, 0)))
	if !ok || r.Repo != "" || r.N != 6 {
		t.Fatalf("range = %+v %v, want 6 jobs from all repos", r, ok)
	}
}

func TestAnUnscoredItemHasNoFigure(t *testing.T) {
	var items []factory.Item
	for i := range 6 {
		items = append(items, finished(i+1, "acme/api", rubric(0, 0, 0, 0, 0), 1, t0, 5))
	}
	if _, ok := estimate.Open(items).For(open("acme/api", nil)); ok {
		t.Fatal("an item nobody scored got a figure")
	}
}

func TestCalibrationBacktestsEachJobAgainstTheJobsBeforeIt(t *testing.T) {
	var items []factory.Item
	// Five cheap jobs, then three that each spent far more than the range
	// the five gave: the hit rate is counted only on those three.
	for i := range 5 {
		items = append(items, finished(i+1, "acme/api", rubric(1, 0, 1, 0, 0), 1, t0.Add(time.Duration(i)*time.Hour), 10))
	}
	for i := range 3 {
		items = append(items, finished(10+i, "acme/api", rubric(1, 0, 1, 0, 0), 5+float64(i), t0.Add(time.Duration(10+i)*time.Hour), 10))
	}
	cal := estimate.Open(items).Calibration()
	if cal.Of != 3 || cal.Hit != 0 {
		t.Fatalf("calibration = %+v, want 0 of 3", cal)
	}
}

func TestPoorCoverageWidensTheRange(t *testing.T) {
	var items []factory.Item
	for i := range 5 {
		items = append(items, finished(i+1, "acme/api", rubric(1, 0, 1, 0, 0), 1, t0.Add(time.Duration(i)*time.Hour), 10))
	}
	for i := range 6 {
		items = append(items, finished(10+i, "acme/api", rubric(1, 0, 1, 0, 0), 1+float64(i), t0.Add(time.Duration(10+i)*time.Hour), 10))
	}
	book := estimate.Open(items)
	cal := book.Calibration()
	if cal.Of < 5 || float64(cal.Hit)/float64(cal.Of) >= estimate.Coverage {
		t.Fatalf("set-up: calibration %+v is not poor", cal)
	}
	r, ok := book.For(open("acme/api", rubric(1, 0, 1, 0, 0)))
	if !ok || !r.Widened {
		t.Fatalf("range = %+v, want widened", r)
	}
	plain, _ := estimate.Open(items[:5]).For(open("acme/api", rubric(1, 0, 1, 0, 0)))
	if r.USDHi <= plain.USDHi {
		t.Fatalf("widened hi %v is not above %v", r.USDHi, plain.USDHi)
	}
}

func TestADroppedJobsPartialSpendIsNotAnEstimate(t *testing.T) {
	var items []factory.Item
	for i := range 5 {
		it := finished(i+1, "acme/api", rubric(0, 0, 0, 0, 0), 0.05, t0, 3)
		it.DoneHow = "dropped"
		items = append(items, it)
	}
	if _, ok := estimate.Open(items).For(open("acme/api", rubric(0, 0, 0, 0, 0))); ok {
		t.Fatal("five dropped jobs made a figure")
	}
}
