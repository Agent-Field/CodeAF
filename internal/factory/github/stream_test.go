package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// memAPI is a GitHub held in memory: per repository, its open issues and
// pull requests, every one with two comments. A repository named in down
// cannot be listed. NOTHING HERE TOUCHES THE NETWORK.
type memAPI struct {
	API
	mu       sync.Mutex
	t0       time.Time
	issues   map[string][]forge.Issue
	pulls    map[string][]forge.Pull
	down     map[string]bool
	comments int
	details  int
	// listed, when set, is told each repository whose issues are asked for,
	// before the answer.
	listed func(full string)
}

func newMemAPI(t0 time.Time) *memAPI {
	return &memAPI{t0: t0, issues: map[string][]forge.Issue{}, pulls: map[string][]forge.Pull{}, down: map[string]bool{}}
}

func (m *memAPI) addIssues(full string, n int) {
	for i := 1; i <= n; i++ {
		m.issues[full] = append(m.issues[full], forge.Issue{Number: i, Title: fmt.Sprintf("%s issue %d", full, i), User: "bob",
			Created: m.t0, Updated: m.t0.Add(time.Duration(i) * time.Minute), Comments: 2})
	}
}

func (m *memAPI) addPulls(full string, from, n int) {
	for i := from; i < from+n; i++ {
		m.pulls[full] = append(m.pulls[full], forge.Pull{Number: i, Title: fmt.Sprintf("%s pull %d", full, i), User: "bob",
			Created: m.t0, Updated: m.t0.Add(time.Duration(i) * time.Minute), HeadSHA: fmt.Sprintf("sha%d", i)})
	}
}

func (m *memAPI) Me(context.Context) (string, error) { return "me", nil }

func (m *memAPI) ListIssues(_ context.Context, owner, repo string, since time.Time, _ string) ([]forge.Issue, string, error) {
	full := owner + "/" + repo
	if m.listed != nil {
		m.listed(full)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.down[full] {
		return nil, "", errors.New("dial tcp: no route")
	}
	var out []forge.Issue
	for _, is := range m.issues[full] {
		// GitHub's since is inclusive.
		if since.IsZero() || !is.Updated.Before(since) {
			out = append(out, is)
		}
	}
	return out, "", nil
}

func (m *memAPI) ListPulls(_ context.Context, owner, repo, _ string) ([]forge.Pull, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]forge.Pull(nil), m.pulls[owner+"/"+repo]...), "", nil
}

func (m *memAPI) Pull(_ context.Context, owner, repo string, number int) (forge.Pull, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.details++
	for _, p := range m.pulls[owner+"/"+repo] {
		if p.Number == number {
			p.Additions, p.Deletions, p.Files, p.Comments = 10, 2, 1, 2
			return p, nil
		}
	}
	return forge.Pull{}, errors.New("no such pull")
}

func (m *memAPI) PullFiles(context.Context, string, string, int) ([]forge.FileChange, error) {
	return []forge.FileChange{{Path: "a.go", Additions: 10, Deletions: 2}}, nil
}

func (m *memAPI) Permission(context.Context, string, string, string) (string, error) {
	return "read", nil
}

func (m *memAPI) CheckRuns(context.Context, string, string, string) (forge.CheckState, []forge.CheckRun, error) {
	return forge.ChecksPassed, []forge.CheckRun{{Name: "build", Status: "completed", Conclusion: "success"}}, nil
}

func (m *memAPI) IssueComments(_ context.Context, _, _ string, number, count, most int) ([]forge.Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments++
	var out []forge.Comment
	for i := 1; i <= min(count, most); i++ {
		out = append(out, forge.Comment{User: "carol", Body: fmt.Sprintf("comment %d on #%d", i, number), Created: m.t0})
	}
	return out, nil
}

func floorByRepo(t *testing.T, st *store.Store) map[string]int {
	t.Helper()
	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, it := range items {
		out[it.Product+"/"+it.Repo]++
	}
	return out
}

// THE FLOOR FILLS REPOSITORY BY REPOSITORY. Three repositories, the second
// down: the first's items are on the floor before the second is even asked,
// the third's come in past the failure, the cursor gives the second no mark,
// and the next tick folds the second's items without doubling anyone's.
func TestTheFirstReadFoldsRepositoryByRepository(t *testing.T) {
	m := newMemAPI(time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC))
	m.addIssues("acme/one", 3)
	m.addPulls("acme/one", 100, 1)
	m.addIssues("acme/two", 2)
	m.addIssues("acme/three", 2)
	m.down["acme/two"] = true
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/one", "acme/two", "acme/three"})
	var atTwo map[string]int
	m.listed = func(full string) {
		if full == "acme/two" && atTwo == nil {
			atTwo = floorByRepo(t, st)
		}
	}
	src := New(m, nil, nil)
	cursor, err := PollOnce(context.Background(), src, st, "", time.Now)
	if err == nil || !strings.Contains(err.Error(), "acme/two") {
		t.Fatalf("the down repository was not named: %v", err)
	}
	if atTwo["acme/one"] != 4 {
		t.Fatalf("the first repository was not on the floor before the second was read: %v", atTwo)
	}
	if got := floorByRepo(t, st); got["acme/one"] != 4 || got["acme/two"] != 0 || got["acme/three"] != 2 {
		t.Fatalf("floor after the failed tick: %v", got)
	}
	if strings.Contains(cursor, "acme/two") || !strings.Contains(cursor, "acme/one") {
		t.Fatalf("the cursor moved for the failed repository, or not for the good one: %s", cursor)
	}
	m.mu.Lock()
	m.down["acme/two"] = false
	m.mu.Unlock()
	if _, err := PollOnce(context.Background(), src, st, cursor, time.Now); err != nil {
		t.Fatal(err)
	}
	if got := floorByRepo(t, st); got["acme/one"] != 4 || got["acme/two"] != 2 || got["acme/three"] != 2 {
		t.Fatalf("floor after the next tick: %v", got)
	}
}

// THE COMMENTS ARE SPENT OVER TICKS. With a budget of three requests, a
// repository of five issues and two pull requests lands all seven rows on
// the first tick; the issues past the budget carry nil comments (not read
// yet), the pull requests no files and no diff; each later tick reads the
// next three requests' worth, through issues a cursor-asked list no longer
// carries, until every item has its comments and every pull request its
// files, and nothing is read twice.
func TestCommentsPastTheBudgetAreReadOnLaterTicks(t *testing.T) {
	m := newMemAPI(time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC))
	m.addIssues("acme/busy", 5)
	m.addPulls("acme/busy", 100, 2)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/busy"})
	src := New(m, nil, nil)
	src.budget = 3
	cursor, err := PollOnce(context.Background(), src, st, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	if len(items) != 7 {
		t.Fatalf("the first tick folded %d items, want 7", len(items))
	}
	read, unread := 0, 0
	for _, it := range items {
		switch {
		case it.Kind == factory.KindPR:
			if it.Files != nil || it.Diff != "" || it.Comments != nil {
				t.Fatalf("a pull request past the budget says it was read: %+v", it)
			}
		case it.Comments == nil:
			unread++
		case len(it.Comments) == 2:
			read++
		}
	}
	if read != 3 || unread != 2 {
		t.Fatalf("first tick: %d issues with comments, %d not read yet; want 3 and 2", read, unread)
	}
	for tick := 2; tick <= 6; tick++ {
		if cursor, err = PollOnce(context.Background(), src, st, cursor, time.Now); err != nil {
			t.Fatal(err)
		}
	}
	items, _ = st.List()
	for _, it := range items {
		if len(it.Comments) != 2 {
			t.Fatalf("#%d still has %d comments after the later ticks", it.Num, len(it.Comments))
		}
		if it.Kind == factory.KindPR && (len(it.Files) != 1 || it.Diff == "") {
			t.Fatalf("pull request #%d has no files or diff after the later ticks: %+v", it.Num, it)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.comments != 7 || m.details != 2 {
		t.Fatalf("comments read %d times (want 7), details %d (want 2)", m.comments, m.details)
	}
}

// WHAT WAS NOT READ YET IS NOT A CHANGE: an item handed over without its
// comments keeps the floor's, and a pull request without its files keeps
// its files and diff; an empty list does say there are none.
func TestMergeKeepsWhatWasNotReadYet(t *testing.T) {
	st := openStore(t)
	said := []factory.Comment{{Author: "carol", Body: "first"}}
	full := factory.Item{Product: "acme", Repo: "api", Num: 9, Kind: factory.KindPR, Title: "t", Origin: factory.OriginForge,
		Diff: "+1 −1", Files: []factory.FileChange{{Path: "a.go", Added: 1, Removed: 1}}, Comments: said}
	if err := Merge(st, Name, []factory.Item{full}); err != nil {
		t.Fatal(err)
	}
	bare := full
	bare.Diff, bare.Files, bare.Comments = "", nil, nil
	if err := Merge(st, Name, []factory.Item{bare}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.List()
	if len(items) != 1 || items[0].Diff != "+1 −1" || len(items[0].Files) != 1 || len(items[0].Comments) != 1 {
		t.Fatalf("an unread pull request wiped what the floor had: %+v", items)
	}
	none := full
	none.Comments = []factory.Comment{}
	if err := Merge(st, Name, []factory.Item{none}); err != nil {
		t.Fatal(err)
	}
	items, _ = st.List()
	if items[0].Comments == nil || len(items[0].Comments) != 0 {
		t.Fatalf("read and nothing said did not land as an empty list: %#v", items[0].Comments)
	}
}

// itemsWatcher records the source's record each time the read says where it
// is.
type itemsWatcher struct {
	*Source
	st   *store.Store
	seen []store.SourceMeta
}

func (p *itemsWatcher) SetProgressItems(fn func(full string, done, of, items int)) {
	if fn == nil {
		p.Source.SetProgressItems(nil)
		return
	}
	p.Source.SetProgressItems(func(full string, done, of, items int) {
		fn(full, done, of, items)
		m, _ := p.st.SourceMeta(Name)
		p.seen = append(p.seen, m)
	})
}

// A BIG REPOSITORY'S READ IS NOT SILENT: the record says how many items the
// read has listed so far as each list comes in, across repositories, and
// the count is taken off at the end.
func TestProgressCountsTheItemsSoFar(t *testing.T) {
	m := newMemAPI(time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC))
	m.addIssues("acme/one", 3)
	m.addPulls("acme/one", 100, 2)
	m.addIssues("acme/two", 4)
	st := openStore(t)
	_ = st.SetRepos([]string{"acme/one", "acme/two"})
	w := &itemsWatcher{Source: New(m, nil, nil), st: st}
	if _, err := PollOnce(context.Background(), w, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range w.seen {
		got = append(got, fmt.Sprintf("%s %d/%d %d", s.Reading, s.Read, s.Of, s.Items))
	}
	want := []string{"acme/one 0/2 0", "acme/one 0/2 3", "acme/one 0/2 5", "acme/two 1/2 5", "acme/two 1/2 9", "acme/two 1/2 9"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Fatalf("progress\n got %v\nwant %v", got, want)
	}
	if m, _ := st.SourceMeta(Name); m.Items != 0 || m.Polling {
		t.Fatalf("the end of the read left its count: %+v", m)
	}
	_ = st.SetPolling(Name, true)
	_ = st.SetReadingItems(Name, "acme/one", 0, 2, 200)
	_ = st.SetSourceMeta(Name, func() store.SourceMeta { m, _ := st.SourceMeta(Name); m.Tried = time.Now(); return m }())
	if info, ok := SourceFacts(st, time.Now()); !ok || info.Items != 200 {
		t.Fatalf("the floor's facts did not carry the items so far: %+v", info)
	}
}
