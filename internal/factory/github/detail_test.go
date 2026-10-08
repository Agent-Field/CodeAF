package github

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// An item carries its page, its last three comments, and for a pull request
// its files (most changed first) and its check runs by name.
func TestReadCarriesThePageCommentsFilesAndChecks(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	items, _, err := New(api, []string{"acme/api"}, nil).Read(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := byNum(items)
	is := got[7]
	if is.URL != "https://github.com/acme/api/issues/7" {
		t.Fatalf("the issue's page is %q", is.URL)
	}
	var said []string
	for _, c := range is.Comments {
		if c.Author != "carol" || c.At.IsZero() {
			t.Fatalf("a comment lost its author or time: %+v", c)
		}
		said = append(said, c.Body)
	}
	if strings.Join(said, "|") != "comment 2|comment 3|comment 4" {
		t.Fatalf("the issue kept %q, want the last three oldest first", said)
	}
	pr := got[9]
	if pr.URL != "https://github.com/acme/api/pull/9" || len(pr.Comments) != 1 || pr.Comments[0].Body != "looks right" {
		t.Fatalf("the pull request's page or comments: %+v", pr)
	}
	wantFiles := []factory.FileChange{{Path: "big.go", Added: 29, Removed: 24}, {Path: "small.go", Added: 1, Removed: 1}}
	if !reflect.DeepEqual(pr.Files, wantFiles) {
		t.Fatalf("files %+v, want %+v", pr.Files, wantFiles)
	}
	wantRuns := []factory.CheckRun{{Name: "build", State: "in_progress", URL: "https://github.com/acme/api/runs/1"}}
	if !reflect.DeepEqual(pr.CheckRuns, wantRuns) || pr.Checks != "ci running" {
		t.Fatalf("check runs %+v (%q), want %+v", pr.CheckRuns, pr.Checks, wantRuns)
	}
}

// THE COMMENTS ARE READ ONLY WHEN THE COUNT MOVES: an issue listed again with
// the same count costs no comment request, and one with a new count does.
func TestCommentsAreReadOnlyWhenTheCountMoves(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	src := New(api, []string{"acme/api"}, nil)
	ctx := context.Background()
	if _, _, err := src.Read(ctx, ""); err != nil {
		t.Fatal(err)
	}
	const path = "GET /repos/acme/api/issues/7/comments"
	if n := f.hit(path); n != 1 {
		t.Fatalf("the first read asked for comments %d times", n)
	}
	f.mu.Lock()
	f.issueTag = `"i2"`
	f.mu.Unlock()
	items, _, err := src.Read(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := f.hit(path); n != 1 {
		t.Fatalf("an unchanged count asked for comments again (%d)", n)
	}
	if len(byNum(items)[7].Comments) != 3 {
		t.Fatal("an unchanged count lost the comments it had")
	}
	f.mu.Lock()
	f.issueTag, f.comments = `"i3"`, 5
	f.mu.Unlock()
	items, _, err = src.Read(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := f.hit(path); n != 2 {
		t.Fatalf("a new count asked for comments %d times in all, want 2", n)
	}
	if c := byNum(items)[7].Comments; len(c) != 3 || c[2].Body != "comment 5" {
		t.Fatalf("the new comment did not arrive: %+v", c)
	}
	f.mu.Lock()
	f.issueTag, f.comments = `"i4"`, 0
	f.mu.Unlock()
	items, _, _ = src.Read(ctx, "")
	if f.hit(path) != 2 || len(byNum(items)[7].Comments) != 0 {
		t.Fatal("an issue with no comments asked for them, or kept old ones")
	}
}

// read stamps an item as the triage worker would.
func read(t *testing.T, st *store.Store, id int, words string) {
	t.Helper()
	if err := st.Annotate(id, func(it *factory.Item) error {
		it.Triage.Read, it.Triage.Est, it.Triage.Priority = words, 3, 2
		it.Triage.TriagedAt = time.Now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func whats(it factory.Item) []string {
	var out []string
	for _, e := range it.Activity {
		out = append(out, e.What)
	}
	return out
}

// A changed body takes the read off so triage reads the item again; a
// changed label alone keeps the read and takes the type again from it. Both
// are `changed on github`; the item began `arrived from github`.
func TestAnUpstreamChangeIsReadAgain(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	src := New(api, nil, nil)
	ctx := context.Background()
	if _, err := PollOnce(ctx, src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	issue := byNum(items)[7]
	if w := whats(issue); len(w) != 1 || w[0] != factory.EventArrived {
		t.Fatalf("a new item's activity is %q", w)
	}
	read(t, st, issue.ID, "a double count in the ledger")

	f.mu.Lock()
	f.label, f.issueTag = "question", `"i2"`
	f.mu.Unlock()
	if _, err := PollOnce(ctx, src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(issue.ID)
	if got.Triage.Read == "" || got.Triage.TriagedAt.IsZero() || got.Triage.Type != "question" {
		t.Fatalf("a label change lost the read or kept the old type: %+v", got.Triage)
	}

	f.mu.Lock()
	f.issueBody, f.issueTag = "it double counts refunds only", `"i3"`
	f.mu.Unlock()
	if _, err := PollOnce(ctx, src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(issue.ID)
	if got.Triage.Read != "" || !got.Triage.TriagedAt.IsZero() || got.Triage.Est != 0 || got.Triage.Priority != 0 {
		t.Fatalf("a body change kept the old read: %+v", got.Triage)
	}
	want := []string{factory.EventArrived, factory.EventRead, factory.EventChanged, factory.EventChanged}
	if w := whats(got); !reflect.DeepEqual(w, want) {
		t.Fatalf("activity %q, want %q", w, want)
	}
}

// localFloor is the local seam over st with its refresh doors read through
// src, and the GitHub fixture's two items on it.
func localFloor(t *testing.T, f *fixture) (*store.Store, factory.Seam, map[int]factory.Item) {
	t.Helper()
	_, api := f.serve(t)
	st := openStore(t)
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	src := New(api, nil, nil)
	if _, err := PollOnce(context.Background(), src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now(), factory.WithRefetch(Refetcher(st, func(context.Context) *Source { return src })))
	items, _ := st.List()
	return st, seam, byNum(items)
}

// Refresh reads one item again now, by its number, with its comments read
// even though their count did not move, and takes its read off; a pull
// request's files and checks are read again too; a terminal item only loses
// its read, and asks GitHub nothing.
func TestRefreshReadsOneItemAgain(t *testing.T) {
	f := newFixture()
	st, seam, items := localFloor(t, f)
	ctx := context.Background()
	if !seam.Has("refresh") || !seam.Has("refreshall") || !seam.Has("open") {
		t.Fatal("the source doors are missing over a source")
	}
	issue, pr := items[7], items[9]
	read(t, st, issue.ID, "a read")
	f.mu.Lock()
	f.issueBody = "changed, and the list was never asked"
	f.mu.Unlock()
	comments := f.hit("GET /repos/acme/api/issues/7/comments")
	if err := seam.Refresh(ctx, issue.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(issue.ID)
	if got.Body != "changed, and the list was never asked" || got.Triage.Read != "" || !got.Triage.TriagedAt.IsZero() {
		t.Fatalf("the refresh did not read the issue again: %+v", got)
	}
	if f.hit("GET /repos/acme/api/issues/7") != 1 || f.hit("GET /repos/acme/api/issues/7/comments") != comments+1 {
		t.Fatal("the refresh did not ask for the issue and its comments by number")
	}

	files, runs := f.hit("GET /repos/acme/api/pulls/9/files"), f.hit("GET /repos/acme/api/commits/abc/check-runs")
	f.mu.Lock()
	f.checksRun = false
	f.mu.Unlock()
	if err := seam.Refresh(ctx, pr.ID); err != nil {
		t.Fatal(err)
	}
	if f.hit("GET /repos/acme/api/pulls/9/files") != files+1 || f.hit("GET /repos/acme/api/commits/abc/check-runs") != runs+1 {
		t.Fatal("a pull request's refresh did not read its files and checks")
	}
	gotPR, _ := st.Get(pr.ID)
	if gotPR.Checks != "ci ✓" || len(gotPR.CheckRuns) != 1 || gotPR.CheckRuns[0].State != "success" {
		t.Fatalf("the refreshed checks did not land: %q %+v", gotPR.Checks, gotPR.CheckRuns)
	}

	id, err := st.Add(ctx, factory.Item{Title: "typed on the floor", Repo: "api", Origin: factory.OriginTerminal})
	if err != nil {
		t.Fatal(err)
	}
	read(t, st, id, "a read")
	before := len(f.paths)
	if err := seam.Refresh(ctx, id); err != nil {
		t.Fatal(err)
	}
	mine, _ := st.Get(id)
	if mine.Triage.Read != "" || len(f.paths) != before {
		t.Fatalf("a terminal item's refresh kept its read or asked GitHub: %+v", mine.Triage)
	}
}

// RefreshAll under a dry run answers the count and the estimate and does
// nothing; unmarked, it does it and answers the same figures. The estimate is
// the default per item until a read is priced, then the average of priced
// reads.
func TestRefreshAllEstimatesThenDoes(t *testing.T) {
	f := newFixture()
	st, seam, items := localFloor(t, f)
	ctx := context.Background()
	gone, err := st.Add(ctx, factory.Item{Title: "dismissed", Repo: "api"})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Update(gone, func(it *factory.Item) error { it.State = factory.StateDismissed; return nil })
	read(t, st, items[7].ID, "a read")
	asked := len(f.paths)

	n, est, err := seam.RefreshAll(factory.DryRun(ctx))
	if err != nil || n != 2 || est != 2*factory.DefaultReadCost {
		t.Fatalf("dry run: %d items at $%v (%v), want 2 at the default", n, est, err)
	}
	if len(f.paths) != asked {
		t.Fatal("a dry run asked GitHub")
	}
	if got, _ := st.Get(items[7].ID); got.Triage.Read == "" {
		t.Fatal("a dry run took a read off")
	}
	_ = st.NoteReadCost(0.001)
	_ = st.NoteReadCost(0.003)
	_ = st.NoteReadCost(0) // an unpriced read is not counted
	if _, est, _ = seam.RefreshAll(factory.DryRun(ctx)); est < 0.00399 || est > 0.00401 {
		t.Fatalf("the estimate is $%v, want two items at the $0.002 average", est)
	}

	n, _, err = seam.RefreshAll(ctx)
	if err != nil || n != 2 {
		t.Fatalf("the refresh read %d items (%v)", n, err)
	}
	if f.hit("GET /repos/acme/api/issues/7") != 1 || f.hit("GET /repos/acme/api/pulls/9") < 2 {
		t.Fatal("the refresh did not read every item again")
	}
	if got, _ := st.Get(items[7].ID); got.Triage.Read != "" {
		t.Fatal("the refresh kept a read")
	}
	if _, all, _ := st.Busy(); all != "" {
		t.Fatalf("the floor's refresh line stayed up: %q", all)
	}
}

// Open answers the item's page, or says it is not on github.
func TestOpenAnswersThePage(t *testing.T) {
	f := newFixture()
	st, seam, items := localFloor(t, f)
	url, err := seam.Open(items[9].ID)
	if err != nil || url != "https://github.com/acme/api/pull/9" {
		t.Fatalf("open answered %q, %v", url, err)
	}
	id, _ := st.Add(context.Background(), factory.Item{Title: "mine", Repo: "api"})
	if _, err := seam.Open(id); err == nil || err.Error() != "this item is not on github" {
		t.Fatalf("a terminal item opened: %v", err)
	}
}

// WHAT IS IN FLIGHT IS ON THE FLOOR: `refreshing` on the item while its
// refresh runs, the floor's line while RefreshAll runs, and neither after.
func TestBusyWhileRefreshing(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/api"})
	src := New(api, nil, nil)
	if _, err := PollOnce(context.Background(), src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	var seen []string
	var lines []string
	refetch := Refetcher(st, func(context.Context) *Source { return src })
	seam := factory.LocalSeam(st, time.Now(), factory.WithRefetch(func(ctx context.Context, it factory.Item) error {
		snap, _ := factory.LocalSeam(st, time.Now()).Load()
		seen = append(seen, snap.Busy[it.ID])
		lines = append(lines, snap.BusyAll)
		return refetch(ctx, it)
	}))
	items, _ := st.List()
	if err := seam.Refresh(context.Background(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != factory.BusyRefreshing {
		t.Fatalf("the item was %q while it refreshed", seen)
	}
	if busy, _, _ := st.Busy(); len(busy) != 0 {
		t.Fatalf("the item stayed busy: %v", busy)
	}
	if _, _, err := seam.RefreshAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lines[1] != "refreshing 2 items · 0 done" || lines[2] != "refreshing 2 items · 1 done" {
		t.Fatalf("the floor's line read %q", lines[1:])
	}
	snap, _ := seam.Load()
	if snap.BusyAll != "" || len(snap.Busy) != 0 {
		t.Fatalf("the floor stayed busy: %q %v", snap.BusyAll, snap.Busy)
	}
}

// pollWatcher is a source whose Read looks at the store's record mid-read.
type pollWatcher struct {
	*Source
	st      *store.Store
	polling bool
}

func (p *pollWatcher) Read(ctx context.Context, since string) ([]factory.Item, string, error) {
	info, _ := SourceFacts(p.st, time.Now())
	p.polling = info.Polling
	return p.Source.Read(ctx, since)
}

// The source is marked polling while its read is in flight, and not after,
// and a process starting clears a mark a crash left behind.
func TestPollingIsMarkedWhileTheReadIsInFlight(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/api"})
	_ = st.SetSourceMeta(Name, store.SourceMeta{Tried: time.Now()})
	w := &pollWatcher{Source: New(api, nil, nil), st: st}
	if _, err := PollOnce(context.Background(), w, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	if !w.polling {
		t.Fatal("the source was not polling while its read ran")
	}
	if info, _ := SourceFacts(st, time.Now()); info.Polling {
		t.Fatal("the source stayed polling after its read")
	}
	_ = st.SetPolling(Name, true)
	if err := st.ClearBusy(); err != nil {
		t.Fatal(err)
	}
	if info, _ := SourceFacts(st, time.Now()); info.Polling {
		t.Fatal("a process starting left a crashed poll's mark")
	}
}

// A refresh with no source says github is not connected.
func TestRefetcherWithNoSource(t *testing.T) {
	st := openStore(t)
	err := Refetcher(st, func(context.Context) *Source { return nil })(context.Background(), factory.Item{Origin: factory.OriginForge, Num: 1})
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("no source answered %v", err)
	}
	src := New(nil, nil, nil)
	if _, err := src.Refetch(context.Background(), factory.Item{Origin: factory.OriginTerminal}); !errors.Is(err, factory.ErrNotOnSource) {
		t.Fatalf("a terminal item was refetched: %v", err)
	}
}
