package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// fixture is a GitHub that lives in this test: one repository, acme/api, with
// an issue from a stranger, a pull request from the token's owner (which the
// issues list also carries, as GitHub's does), and checks that are running
// until the test says they finished. NOTHING HERE TOUCHES THE NETWORK.
type fixture struct {
	mu        sync.Mutex
	t0        time.Time
	issueBody string
	issueTag  string
	pullTag   string
	checksRun bool
	hits      map[string]int
	sinces    []string
	posted    []map[string]any
	paths     []string
}

func newFixture() *fixture {
	return &fixture{
		t0:        time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC),
		issueBody: strings.Repeat("words ", 500),
		issueTag:  `"i1"`,
		pullTag:   `"p1"`,
		checksRun: true,
		hits:      map[string]int{},
	}
}

func (f *fixture) serve(t *testing.T) (*httptest.Server, API) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	return srv, api
}

func (f *fixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	f.hits[key]++
	f.paths = append(f.paths, key)
	if r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	js := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	tagged := func(tag string, v any) {
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", tag)
		js(v)
	}
	at := func(h int) string { return f.t0.Add(time.Duration(h) * time.Hour).Format(time.RFC3339) }
	if r.Method != http.MethodGet {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted = append(f.posted, body)
	}
	switch {
	case key == "GET /user":
		js(map[string]any{"login": "santosh"})
	case key == "GET /repos/acme/api/issues":
		f.sinces = append(f.sinces, r.URL.Query().Get("since"))
		tagged(f.issueTag, []map[string]any{
			{"number": 7, "title": "fix the ledger double count", "body": f.issueBody, "user": map[string]any{"login": "bob"},
				"labels": []map[string]any{{"name": "bug"}}, "created_at": at(0), "updated_at": at(1), "html_url": "https://github.com/acme/api/issues/7"},
			{"number": 9, "title": "the pull request as an issue", "user": map[string]any{"login": "santosh"},
				"created_at": at(0), "updated_at": at(2), "pull_request": map[string]any{"url": "x"}},
		})
	case key == "GET /repos/acme/api/pulls" && r.URL.Query().Get("head") != "":
		js([]map[string]any{})
	case key == "GET /repos/acme/api/pulls":
		tagged(f.pullTag, []map[string]any{
			{"number": 9, "title": "feat: tree rails", "body": "rails", "user": map[string]any{"login": "santosh"},
				"labels": []map[string]any{{"name": "ui"}}, "created_at": at(0), "updated_at": at(2),
				"html_url": "https://github.com/acme/api/pull/9", "head": map[string]any{"sha": "abc"}},
		})
	case key == "GET /repos/acme/api/pulls/9":
		js(map[string]any{"number": 9, "additions": 30, "deletions": 25, "changed_files": 2, "head": map[string]any{"sha": "abc"}, "updated_at": at(2)})
	case key == "GET /repos/acme/api/collaborators/bob/permission":
		w.WriteHeader(http.StatusNotFound)
	case key == "GET /repos/acme/api/collaborators/santosh/permission":
		js(map[string]any{"permission": "admin"})
	case key == "GET /repos/acme/api/commits/abc/status":
		js(map[string]any{"state": "success", "statuses": []map[string]any{{"state": "success"}}})
	case key == "GET /repos/acme/api/commits/abc/check-runs":
		run := map[string]any{"status": "completed", "conclusion": "success"}
		if f.checksRun {
			run = map[string]any{"status": "in_progress"}
		}
		js(map[string]any{"check_runs": []map[string]any{run}})
	case key == "GET /user/repos":
		js([]map[string]any{
			{"full_name": "acme/old", "name": "old", "owner": map[string]any{"login": "acme"}, "pushed_at": at(0)},
			{"full_name": "acme/api", "name": "api", "owner": map[string]any{"login": "acme"}, "private": true, "pushed_at": at(5)},
		})
	case key == "POST /repos/acme/api/issues/7/comments":
		js(map[string]any{"id": 555, "html_url": "https://github.com/acme/api/issues/7#issuecomment-555"})
	case key == "POST /repos/acme/api/issues/7/labels":
		js([]map[string]any{{"name": "triaged"}})
	case key == "PATCH /repos/acme/api/issues/7":
		js(map[string]any{"number": 7, "html_url": "https://github.com/acme/api/issues/7"})
	case key == "GET /repos/acme/api":
		js(map[string]any{"default_branch": "main"})
	case key == "POST /repos/acme/api/pulls":
		js(map[string]any{"id": 1, "number": 10, "html_url": "https://github.com/acme/api/pull/10"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fixture) hit(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[key]
}

func byNum(items []factory.Item) map[int]factory.Item {
	out := map[int]factory.Item{}
	for _, it := range items {
		out[it.Num] = it
	}
	return out
}

// Every field the floor draws is mapped from what GitHub said.
func TestReadMapsEveryField(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	src := New(api, []string{"acme/api"}, nil)
	items, next, err := src.Read(context.Background(), "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := byNum(items)
	if len(items) != 2 {
		t.Fatalf("read %d items, want the issue and the pull request once each: %+v", len(items), items)
	}
	is := got[7]
	if is.Kind != factory.KindIssue || is.Repo != "api" || is.Product != "acme" || is.Title != "fix the ledger double count" ||
		is.Author != "bob" || is.Origin != factory.OriginForge || !is.Synced || is.State != factory.StateNew ||
		is.Tier != factory.TierStranger || len(is.Labels) != 1 || is.Labels[0] != "bug" ||
		!is.Created.Equal(f.t0) || !is.Changed.Equal(f.t0.Add(time.Hour)) || is.Triage.Size != "L" || is.Triage.Type != "bug" {
		t.Fatalf("the issue is mapped wrong: %+v", is)
	}
	if n := len([]rune(is.Body)); n != bodyLimit {
		t.Fatalf("the issue body kept %d characters, want %d", n, bodyLimit)
	}
	pr := got[9]
	if pr.Kind != factory.KindPR || pr.Tier != factory.TierOwner || pr.Checks != "ci running" || pr.Diff != "+30 −25" ||
		pr.Triage.Size != "M" || pr.Author != "santosh" || pr.Labels[0] != "ui" {
		t.Fatalf("the pull request is mapped wrong: %+v", pr)
	}
	var marks map[string]time.Time
	if err := json.Unmarshal([]byte(next), &marks); err != nil || !marks["acme/api"].Equal(f.t0.Add(2*time.Hour)) {
		t.Fatalf("the cursor is %q, want acme/api at the newest change", next)
	}
}

// An unchanged list is a 304, read for nothing; the next read asks since the
// mark; checks still running are asked again until they end, and their end
// brings the pull request back changed.
func TestReadUsesTheCursorAndTheETags(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	src := New(api, []string{"acme/api"}, nil)
	ctx := context.Background()
	_, next, err := src.Read(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	items, next2, err := src.Read(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("an unchanged repository read %d items: %+v", len(items), items)
	}
	if next2 != next {
		t.Fatalf("the cursor moved on nothing: %q then %q", next, next2)
	}
	if got := f.sinces[len(f.sinces)-1]; got != f.t0.Add(2*time.Hour).Format(time.RFC3339) {
		t.Fatalf("the second read asked since %q", got)
	}
	if n := f.hit("GET /repos/acme/api/pulls/9"); n != 1 {
		t.Fatalf("an unchanged pull request's line counts were read %d times", n)
	}
	if n := f.hit("GET /repos/acme/api/collaborators/bob/permission"); n != 1 {
		t.Fatalf("a standing was asked %d times; it is kept for the process", n)
	}
	f.mu.Lock()
	f.checksRun = false
	f.mu.Unlock()
	items, _, err = src.Read(ctx, next2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Num != 9 || items[0].Checks != "ci ✓" {
		t.Fatalf("finished checks did not bring the pull request back green: %+v", items)
	}
	items, _, _ = src.Read(ctx, next2)
	if len(items) != 0 {
		t.Fatalf("green checks were reported again: %+v", items)
	}
	runs := f.hit("GET /repos/acme/api/commits/abc/check-runs")
	_, _, _ = src.Read(ctx, next2)
	if f.hit("GET /repos/acme/api/commits/abc/check-runs") != runs {
		t.Fatal("finished checks on an unchanged head were asked again")
	}
}

// A repository that cannot be read is named in the error, and the others
// still come back.
func TestReadKeepsGoingPastABrokenRepository(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	src := New(api, []string{"acme/api", "acme/gone"}, nil)
	items, next, err := src.Read(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "acme/gone") {
		t.Fatalf("a missing repository was not named: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("the good repository's items were lost: %d", len(items))
	}
	if strings.Contains(next, "acme/gone") {
		t.Fatalf("the broken repository got a mark: %s", next)
	}
	if TroubleWords(err) != "repository not found" {
		t.Fatalf("trouble words %q", TroubleWords(err))
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// The poll creates what is new, then brings forge words up to date without
// undoing what a person set.
func TestPollCreatesThenUpdatesWithoutClobbering(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	src := New(api, nil, nil)
	ctx := context.Background()
	now := func() time.Time { return f.t0.Add(3 * time.Hour) }

	// No repositories watched: the tick does nothing at all.
	if _, err := PollOnce(ctx, src, st, "", now); err != nil {
		t.Fatal(err)
	}
	if f.hit("GET /repos/acme/api/issues") != 0 {
		t.Fatal("a floor watching nothing asked GitHub")
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	cursor, err := PollOnce(ctx, src, st, "", now)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	if len(items) != 2 {
		t.Fatalf("the first poll made %d items", len(items))
	}
	issue := byNum(items)[7]
	if len(issue.Stages) == 0 || issue.Gate != factory.GateShip || len(issue.Places) != 1 {
		t.Fatalf("a new forge item has no stages or gate: %+v", issue)
	}
	// The person's own edits.
	if err := st.Update(issue.ID, func(it *factory.Item) error {
		it.Gate, it.Cap, it.State = factory.GatePlan, 7, factory.StateDismissed
		it.Stages[0].On = false
		it.Triage.Readiness = 80
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Nothing changed: no revision is written.
	rev, _ := st.Revision(issue.ID)
	if _, err := PollOnce(ctx, src, st, cursor, now); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.Revision(issue.ID); again != rev {
		t.Fatal("an unchanged item was rewritten")
	}
	// The issue changes on the forge.
	f.mu.Lock()
	f.issueBody, f.issueTag = "short now", `"i2"`
	f.mu.Unlock()
	if _, err := PollOnce(ctx, src, st, cursor, now); err != nil {
		t.Fatal(err)
	}
	items, _ = st.List()
	if len(items) != 2 {
		t.Fatalf("an update made a second copy: %d items", len(items))
	}
	got := byNum(items)[7]
	if got.Body != "short now" {
		t.Fatalf("the forge's new body did not land: %q", got.Body)
	}
	if got.Gate != factory.GatePlan || got.Cap != 7 || got.Stages[0].On || got.Triage.Readiness != 80 || got.Triage.Size != "L" {
		t.Fatalf("the poll undid a person's edit: %+v", got)
	}
	if got.State != factory.StateNew {
		t.Fatalf("a dismissed item that changed did not come back: %s", got.State)
	}
	meta, _ := st.SourceMeta(Name)
	if !meta.Polled.Equal(now()) || meta.Trouble != "" {
		t.Fatalf("the poll's record is %+v", meta)
	}
}

// A failed poll records its trouble and keeps the last good time; the facts
// row names it; a stale record names nothing.
func TestFactsNameTheSourceOnlyWhileAPollIsAlive(t *testing.T) {
	st := openStore(t)
	t0 := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	if _, ok := SourceFacts(st, t0); ok {
		t.Fatal("a floor watching nothing names github")
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := SourceFacts(st, t0); ok {
		t.Fatal("a floor no poll ever read names github")
	}
	src := New(failing{}, nil, nil)
	if _, err := PollOnce(context.Background(), src, st, "", func() time.Time { return t0 }); err == nil {
		t.Fatal("a failing read reported no error")
	}
	info, ok := SourceFacts(st, t0.Add(time.Minute))
	if !ok || info.Name != "github" || info.Trouble != "not reachable" || !info.Polled.IsZero() || info.Writes {
		t.Fatalf("facts after a failure: %+v %v", info, ok)
	}
	if _, ok := SourceFacts(st, t0.Add(FactsFresh+time.Minute)); ok {
		t.Fatal("a record nobody has written for long names github")
	}
	seam := Facts(factory.Seam{Load: func() (factory.Snapshot, error) { return factory.Snapshot{}, nil }}, st, func() time.Time { return t0 })
	snap, _ := seam.Load()
	if len(snap.Sources) != 1 || snap.Sources[0].Name != "github" {
		t.Fatalf("the wrapped load did not name github: %+v", snap.Sources)
	}
}

// Another window holding the poller lock means this one skips the tick.
func TestPollSkipsWhileAnotherWindowPolls(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/api"})
	release, ok := st.TryPoller()
	if !ok {
		t.Fatal("the first poller lock was refused")
	}
	defer release()
	if _, err := PollOnce(context.Background(), New(api, nil, nil), st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	if f.hit("GET /repos/acme/api/issues") != 0 {
		t.Fatal("a second poller read while the first held the lock")
	}
}

func TestBackoffDoublesUpToTenMinutes(t *testing.T) {
	every := time.Minute
	for failed, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute} {
		if got := Backoff(every, failed); got != want {
			t.Fatalf("after %d failures the wait is %s, want %s", failed, got, want)
		}
	}
}

// Write's four verbs come back with receipts; anything else, and any
// repository nobody watches, is refused as read-only.
func TestWriteReceiptsAndRefusals(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	src := New(api, []string{"acme/api"}, func() time.Time { return at })
	ctx := context.Background()
	item := factory.Item{Repo: "api", Product: "acme", Num: 7, Title: "fix the ledger", Origin: factory.OriginForge}

	r, err := src.Write(ctx, factory.Action{Verb: "comment", Item: item, Body: "on it"})
	if err != nil || r.URL != "https://github.com/acme/api/issues/7#issuecomment-555" || r.ID != "555" || !r.At.Equal(at) {
		t.Fatalf("comment receipt %+v %v", r, err)
	}
	if r, err = src.Write(ctx, factory.Action{Verb: "label", Item: item, Labels: []string{"triaged"}}); err != nil || r.ID != "7" {
		t.Fatalf("label receipt %+v %v", r, err)
	}
	if r, err = src.Write(ctx, factory.Action{Verb: "close", Item: item}); err != nil || r.URL != "https://github.com/acme/api/issues/7" {
		t.Fatalf("close receipt %+v %v", r, err)
	}
	if r, err = src.Write(ctx, factory.Action{Verb: "pr", Item: item, Branch: "feat", Draft: true, Body: "why"}); err != nil || r.ID != "10" {
		t.Fatalf("pr receipt %+v %v", r, err)
	}
	f.mu.Lock()
	last := f.posted[len(f.posted)-1]
	f.mu.Unlock()
	if last["draft"] != true || last["base"] != "main" || last["head"] != "feat" {
		t.Fatalf("the pull request was asked for as %+v", last)
	}
	if _, err := src.Write(ctx, factory.Action{Verb: "review", Item: item}); !errors.Is(err, factory.ErrReadOnly) {
		t.Fatalf("an unknown verb was not refused as read-only: %v", err)
	}
	other := item
	other.Product = "someone"
	if _, err := src.Write(ctx, factory.Action{Verb: "comment", Item: other, Body: "x"}); !errors.Is(err, factory.ErrReadOnly) {
		t.Fatalf("an unwatched repository was written to: %v", err)
	}
	if _, err := src.Write(ctx, factory.Action{Verb: "comment", Item: item}); err == nil {
		t.Fatal("an empty comment was posted")
	}
}

func TestListReposIsNewestPushedFirst(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	repos, err := New(api, nil, nil).ListRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].Full != "acme/api" || !repos[0].Private || repos[0].Owner != "acme" || repos[0].Name != "api" {
		t.Fatalf("repos %+v", repos)
	}
}

// The poll never writes: a whole tick against the fixture makes no request
// that is not a GET.
func TestThePollOnlyReads(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/api"})
	if _, err := PollOnce(context.Background(), New(api, nil, nil), st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	for _, p := range f.paths {
		if !strings.HasPrefix(p, "GET ") {
			t.Fatalf("the poll made %s", p)
		}
	}
	if _, err := os.Stat(st.MetaPath()); err != nil {
		t.Fatalf("the poll left no record: %v", err)
	}
}

// failing is a GitHub that cannot be reached.
type failing struct{ API }

func (failing) Me(context.Context) (string, error) { return "", errors.New("dial tcp: no route") }
func (failing) ListIssues(context.Context, string, string, time.Time, string) ([]forge.Issue, string, error) {
	return nil, "", errors.New("dial tcp: no route")
}

// AN ISSUE'S KIND WORD COMES FROM ITS LABEL, and from its title only when no
// label names one.
func TestIssueTypeReadsLabelsThenTitle(t *testing.T) {
	cases := []struct {
		title, label, want string
	}{
		{"Total double-counts an entry added twice", "bug", "bug"},
		{"Add a CSV export to the ledger command", "feat", "feat"},
		{"Flaky TestTotal on an empty ledger", "bug", "bug"},
		{"Should Add refuse a negative amount?", "question", "question"},
		{"Rename Total to Sum across the package", "chore", "chore"},
		{"Ledger loses entries after 1000 adds", "bug", "bug"},
	}
	for _, c := range cases {
		labelled := forge.Issue{Title: c.title, Labels: []string{"help wanted", c.label}}
		if got := issueType(labelled); got != c.want {
			t.Errorf("labelled %q = %q, want %q", c.title, got, c.want)
		}
		// A label that disagrees with the title wins.
		if got := issueType(forge.Issue{Title: "Add a thing", Labels: []string{"bug"}}); got != "bug" {
			t.Errorf("label did not beat the title: %q", got)
		}
		if got := issueType(forge.Issue{Title: c.title}); got != c.want {
			t.Errorf("unlabelled %q = %q, want %q", c.title, got, c.want)
		}
	}
}
