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

func TestTheRangeIsThe10thToThe90thPercentileOfTheNearestJobsInTheRepo(t *testing.T) {
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
	// Sorted spends: 0.2 0.3 0.4 0.5 0.6 0.7 0.9 1.0 1.1 2.0 → p10 0.2, p90
	// 1.1: about eight in ten similar jobs spent inside it.
	if r.N != 10 || r.Repo != "acme/api" || r.USDLo != 0.2 || r.USDHi != 1.1 {
		t.Fatalf("range = %+v, want $0.20–1.10 from 10 jobs in acme/api", r)
	}
	// Minutes 10..100 → p10 10, p90 90.
	if r.MinLo != 10 || r.MinHi != 90 {
		t.Fatalf("minutes = %v–%v, want 10–90", r.MinLo, r.MinHi)
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

// unread is a job that finished with no read: no rubric, no size from one.
func unread(id int, repo string, kind factory.Kind, usd float64, start time.Time, minutes int) factory.Item {
	it := finished(id, repo, nil, usd, start, minutes)
	it.Kind = kind
	return it
}

func TestAnUnreadItemIsEstimatedFromItsRepositorysJobsOfTheSameKind(t *testing.T) {
	var items []factory.Item
	for i := range 6 {
		items = append(items, unread(i+1, "acme/api", factory.KindIssue, float64(i+1), t0.Add(time.Duration(i)*time.Hour), 10))
	}
	r, ok := estimate.Open(items).For(open("acme/api", nil))
	if !ok || r.Basis != estimate.BasisKind || r.Repo != "acme/api" || r.N != 6 {
		t.Fatalf("range = %+v ok %v, want six acme/api issue jobs by kind", r, ok)
	}
}

func TestAReadItemWithTooFewSimilarReadJobsFallsBackToItsKind(t *testing.T) {
	var items []factory.Item
	for i := range 5 {
		items = append(items, unread(i+1, "acme/api", factory.KindIssue, 1, t0, 10))
	}
	r, ok := estimate.Open(items).For(open("acme/api", rubric(1, 0, 1, 0, 0)))
	if !ok || r.Basis != estimate.BasisKind {
		t.Fatalf("range = %+v ok %v, want the kind pool", r, ok)
	}
}

func TestAMatchOnTheReadComesBeforeTheKindPool(t *testing.T) {
	var items []factory.Item
	for i := range 5 {
		items = append(items, finished(i+1, "acme/api", rubric(1, 0, 1, 0, 0), 1, t0, 10),
			unread(10+i, "acme/api", factory.KindIssue, 9, t0, 10))
	}
	r, ok := estimate.Open(items).For(open("acme/api", rubric(1, 0, 1, 0, 0)))
	if !ok || r.Basis != estimate.BasisRead || r.USDHi != 1 {
		t.Fatalf("range = %+v ok %v, want the five read jobs", r, ok)
	}
}

func TestTheKindPoolBorrowsFromEveryRepositoryButNeverAnotherKind(t *testing.T) {
	var items []factory.Item
	for i := range 5 {
		items = append(items, unread(i+1, "acme/web", factory.KindIssue, 1, t0, 10),
			unread(10+i, "acme/api", factory.KindPR, 1, t0, 10))
	}
	book := estimate.Open(items)
	r, ok := book.For(open("acme/api", nil))
	if !ok || r.Basis != estimate.BasisKind || r.Repo != "" || r.N != 5 {
		t.Fatalf("range = %+v ok %v, want five issue jobs from every repository", r, ok)
	}
	if r, ok := book.ForNew("acme/api", factory.KindChore); ok {
		t.Fatalf("a kind with no finished jobs got a figure: %+v", r)
	}
}

func TestTheKindPoolIsItsMostRecentJobs(t *testing.T) {
	var items []factory.Item
	for i := range estimate.Nearest + 3 {
		usd := 100.0
		if i >= 3 {
			usd = 1
		}
		items = append(items, unread(i+1, "acme/api", factory.KindIssue, usd, t0.Add(time.Duration(i)*time.Hour), 10))
	}
	r, ok := estimate.Open(items).For(open("acme/api", nil))
	if !ok || r.N != estimate.Nearest || r.USDHi != 1 {
		t.Fatalf("range = %+v ok %v, want the %d latest jobs only", r, ok, estimate.Nearest)
	}
}

// Before a job exists there is only its repository and kind: the figure
// comes from the kind pool, with the same five-job floor.
func TestANewJobIsEstimatedFromItsKindBeforeItExists(t *testing.T) {
	var items []factory.Item
	for i := range 4 {
		items = append(items, unread(i+1, "acme/api", factory.KindIssue, 2, t0, 30))
	}
	if r, ok := estimate.Open(items).ForNew("acme/api", factory.KindIssue); ok {
		t.Fatalf("four jobs gave a figure: %+v", r)
	}
	items = append(items, finished(9, "acme/api", rubric(2, 2, 2, 2, 2), 2, t0, 30))
	r, ok := estimate.Open(items).ForNew("ACME/api", factory.KindIssue)
	if !ok || r.Basis != estimate.BasisKind || r.N != 5 || r.USDLo != 2 || r.MinHi != 30 || r.Repo != "acme/api" {
		t.Fatalf("range = %+v ok %v, want five acme/api issue jobs", r, ok)
	}
}

// Each basis is backtested on its own: read matches that land do not hide
// a kind pool that misses, and only the pool that misses is widened.
func TestCalibrationIsKeptPerBasis(t *testing.T) {
	var items []factory.Item
	for i := range 11 {
		items = append(items, finished(i+1, "acme/api", rubric(1, 0, 1, 0, 0), 1, t0.Add(time.Duration(i)*time.Hour), 10))
	}
	for i := range 11 {
		items = append(items, unread(20+i, "acme/web", factory.KindIssue, 1+float64(i*i), t0.Add(time.Duration(i)*time.Hour), 10))
	}
	book := estimate.Open(items)
	read, kind := book.CalibrationBy(estimate.BasisRead), book.CalibrationBy(estimate.BasisKind)
	if read.Of == 0 || read.Poor() || kind.Of == 0 || !kind.Poor() {
		t.Fatalf("read %+v kind %+v, want read good and kind poor", read, kind)
	}
	if all := book.Calibration(); all.Hit != read.Hit+kind.Hit || all.Of != read.Of+kind.Of {
		t.Fatalf("all %+v is not read %+v plus kind %+v", all, read, kind)
	}
	if r, _ := book.For(open("acme/api", rubric(1, 0, 1, 0, 0))); r.Widened {
		t.Fatalf("a read match was widened by the kind pool's misses: %+v", r)
	}
	if r, _ := book.For(open("acme/web", nil)); !r.Widened {
		t.Fatalf("the missing kind pool was not widened: %+v", r)
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
	// Widening stretches the same jobs' range by how far real spend strayed
	// from the median: the unwidened p90 of these eleven jobs is $5.
	if r.USDHi <= 5 {
		t.Fatalf("widened hi %v is not above the unwidened $5", r.USDHi)
	}
	plain, _ := estimate.Open(items[:5]).For(open("acme/api", rubric(1, 0, 1, 0, 0)))
	if plain.Widened {
		t.Fatalf("five agreeing jobs were widened: %+v", plain)
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

func TestARunningJobsLeftIsWhatSimilarJobsSpentPastItsSpendSoFar(t *testing.T) {
	var items []factory.Item
	// Spends 0.5 and 1 never got as far as 2; 3, 4, 5, 6, 7 did.
	for i, usd := range []float64{0.5, 1, 3, 4, 5, 6, 7} {
		items = append(items, finished(i+1, "acme/api", rubric(1, 0, 1, 0, 0), usd, t0, 10))
	}
	running := open("acme/api", rubric(1, 0, 1, 0, 0))
	running.State = factory.StateRunning
	running.Stream = &factory.Stream{Spent: 2, Started: t0}
	left, ok := estimate.Open(items).Left(running)
	// Past $2 they spent 1, 2, 3, 4, 5 more: p10 1, p90 5.
	if !ok || left.N != 5 || left.USDLo != 1 || left.USDHi != 5 {
		t.Fatalf("left = %+v %v, want $1–5 from 5 jobs", left, ok)
	}
	running.Stream.Spent = 5
	if _, ok := estimate.Open(items).Left(running); ok {
		t.Fatal("three jobs got past $5, yet a figure was made")
	}
}

// A repository is one repository whatever the case of its name, both when
// counting its jobs and when picking the nearest of them.
func TestARepositorysJobsMatchWhateverTheCaseOfItsName(t *testing.T) {
	var items []factory.Item
	for i := range 5 {
		items = append(items, finished(i+1, "Acme/API", rubric(1, 0, 1, 0, 0), 1, t0, 20))
	}
	r, ok := estimate.Open(items).For(open("acme/api", rubric(1, 0, 1, 0, 0)))
	if !ok || r.Repo != "Acme/API" && r.Repo != "acme/api" {
		t.Fatalf("range = %+v ok %v, want the repository's own jobs", r, ok)
	}
}
