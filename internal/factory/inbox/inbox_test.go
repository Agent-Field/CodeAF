package inbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

var at = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func entry() Entry {
	return Entry{Source: "inbox:ops", ExternalID: "A1", Title: "Fix the login timeout", Body: "It times out after 3s.",
		URL: "https://ops.example.com/A1", Labels: []string{"bug"}, Priority: "p1", Repo: "acme/web"}
}

func TestAddFilesAJobLikeOneTypedOnTheFloor(t *testing.T) {
	st := openStore(t)
	recipe := factory.DefaultRecipe()
	res, err := Add(context.Background(), st, entry(), at, WithRecipe(func(repo string) (factory.Recipe, error) {
		if repo != "acme/web" {
			t.Fatalf("recipe read for %q", repo)
		}
		return recipe, nil
	}))
	if err != nil || !res.Created || res.ID < 1 {
		t.Fatalf("Add = %+v, %v", res, err)
	}
	it, err := st.Get(res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if it.SourceRef != "inbox:ops:A1" || it.Origin != "inbox:ops" || it.Num != 0 || it.Kind != factory.KindIssue || it.State != factory.StateNew {
		t.Fatalf("identity = %q %q #%d %s %s", it.SourceRef, it.Origin, it.Num, it.Kind, it.State)
	}
	if it.Tier != factory.TierOwner || it.Repo != "acme/web" || len(it.Places) != 1 || it.URL != "https://ops.example.com/A1" {
		t.Fatalf("job = tier %s repo %q places %v url %q", it.Tier, it.Repo, it.Places, it.URL)
	}
	if it.Priority == nil || it.Priority.Value != "high" || it.Priority.SetBy != SetBySource {
		t.Fatalf("priority = %+v", it.Priority)
	}
	if len(it.Stages) != len(recipe.For(factory.KindIssue)) {
		t.Fatalf("stages = %d", len(it.Stages))
	}
	if len(it.Activity) == 0 || it.Activity[0].What != "arrived from inbox:ops · https://ops.example.com/A1" {
		t.Fatalf("activity = %+v", it.Activity)
	}
	// EVERY GITHUB-ONLY SIDE EFFECT IS GUARDED THE WAY A FLOOR JOB IS.
	if factory.GitHubIssue(it) || it.RefusesWrite() || factory.AskText(it) != "It times out after 3s." {
		t.Fatalf("github issue %v, refuses write %v, ask %q", factory.GitHubIssue(it), it.RefusesWrite(), factory.AskText(it))
	}
	if src, id, ok := Split(it); !ok || src != "inbox:ops" || id != "A1" {
		t.Fatalf("Split = %q %q %v", src, id, ok)
	}
}

func TestTheSamePairTwiceIsOneJob(t *testing.T) {
	st := openStore(t)
	first, err := Add(context.Background(), st, entry(), at)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Add(context.Background(), st, entry(), at.Add(time.Minute))
	if err != nil || again.Created || again.ID != first.ID {
		t.Fatalf("second Add = %+v, %v", again, err)
	}
	before, _ := st.Get(first.ID)
	// An unchanged delivery writes nothing.
	if _, err := Add(context.Background(), st, entry(), at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Get(first.ID)
	if !after.Changed.Equal(before.Changed) {
		t.Fatalf("an unchanged delivery wrote the job: %v → %v", before.Changed, after.Changed)
	}
	items, _ := st.List()
	if len(items) != 1 {
		t.Fatalf("jobs = %d", len(items))
	}
	// The same id from another source is another job.
	other := entry()
	other.Source = "jira"
	res, err := Add(context.Background(), st, other, at)
	if err != nil || !res.Created || res.ID == first.ID {
		t.Fatalf("other source = %+v, %v", res, err)
	}
}

func TestConcurrentDeliveriesMakeOneJob(t *testing.T) {
	st := openStore(t)
	var wg sync.WaitGroup
	ids := make([]int, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := Add(context.Background(), st, entry(), at)
			if err != nil {
				t.Error(err)
			}
			ids[i] = res.ID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("ids = %v", ids)
		}
	}
}

func TestARepeatUpdatesWordsAndKeepsWhatAPersonSet(t *testing.T) {
	st := openStore(t)
	res, err := Add(context.Background(), st, entry(), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(res.ID, func(it *factory.Item) error {
		it.Priority = &factory.JobPriority{Value: "low", SetBy: "santosh"}
		it.Cap = 7
		it.State = factory.StateDismissed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	next := entry()
	next.Title, next.Body, next.Labels, next.Priority = "Fix the login timeout for good", "Now 5s.", []string{"bug", "auth"}, "p0"
	if _, err := Add(context.Background(), st, next, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(res.ID)
	if it.Title != next.Title || it.Body != "Now 5s." || len(it.Labels) != 2 || it.Ask.Text != "Now 5s." {
		t.Fatalf("words = %q %q %v %q", it.Title, it.Body, it.Labels, it.Ask.Text)
	}
	if it.Priority.Value != "low" || it.Priority.SetBy != "santosh" || it.Cap != 7 {
		t.Fatalf("a person's choices moved: %+v cap %v", it.Priority, it.Cap)
	}
	if it.State != factory.StateNew {
		t.Fatalf("a dismissed job that changed stays %s", it.State)
	}
	if last := it.Activity[len(it.Activity)-1].What; last != "changed on inbox:ops" {
		t.Fatalf("activity = %q", last)
	}
}

func TestASourcePriorityFollowsTheSource(t *testing.T) {
	st := openStore(t)
	res, _ := Add(context.Background(), st, entry(), at)
	next := entry()
	next.Priority = "p3"
	if _, err := Add(context.Background(), st, next, at); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(res.ID)
	if it.Priority == nil || it.Priority.Value != "low" {
		t.Fatalf("priority = %+v", it.Priority)
	}
}

func TestAJobWithNoRepositoryIsFiledAndWaits(t *testing.T) {
	st := openStore(t)
	e := entry()
	e.Repo = ""
	res, err := Add(context.Background(), st, e, at)
	if err != nil || !res.Created {
		t.Fatalf("Add = %+v, %v", res, err)
	}
	it, _ := st.Get(res.ID)
	if it.Repo != "" || factory.RepoOf(it) != "" || len(it.Stages) == 0 {
		t.Fatalf("job = repo %q stages %d", it.Repo, len(it.Stages))
	}
	if it.Activity[len(it.Activity)-1].What != WaitingWords {
		t.Fatalf("activity = %+v", it.Activity)
	}
	// A later delivery that names the repository gives it one.
	e.Repo = "acme/api"
	if _, err := Add(context.Background(), st, e, at); err != nil {
		t.Fatal(err)
	}
	it, _ = st.Get(res.ID)
	if it.Repo != "acme/api" || len(it.Places) != 1 {
		t.Fatalf("repo = %q places %v", it.Repo, it.Places)
	}
	// And never moves a repository it already has.
	e.Repo = "acme/other"
	if _, err := Add(context.Background(), st, e, at); err != nil {
		t.Fatal(err)
	}
	it, _ = st.Get(res.ID)
	if it.Repo != "acme/api" {
		t.Fatalf("repo moved to %q", it.Repo)
	}
}

func TestValidateRefusesInOneSentence(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*Entry)
		want string
	}{
		{"no source", func(e *Entry) { e.Source = "" }, "source is required"},
		{"github", func(e *Entry) { e.Source = "github" }, "the factory's own"},
		{"upper", func(e *Entry) { e.Source = "Jira" }, "lowercase"},
		{"no id", func(e *Entry) { e.ExternalID = " " }, "id is required"},
		{"no title", func(e *Entry) { e.Title = "" }, "title is required"},
		{"long title", func(e *Entry) { e.Title = strings.Repeat("x", MaxTitle+1) }, "title must be one line"},
		{"two lines", func(e *Entry) { e.Title = "a\nb" }, "title must be one line"},
		{"url", func(e *Entry) { e.URL = "ftp://x" }, "url must be"},
		{"repo", func(e *Entry) { e.Repo = "web" }, "repo must be owner/name"},
		{"priority", func(e *Entry) { e.Priority = "p9" }, "priority must be"},
		{"labels", func(e *Entry) { e.Labels = make([]string, MaxLabels+1); fill(e.Labels) }, "labels"},
	} {
		e := entry()
		c.edit(&e)
		if _, err := Validate(e); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	e := entry()
	e.Priority, e.Labels, e.Source = " High ", []string{" bug ", "bug", ""}, " jira "
	got, err := Validate(e)
	if err != nil || got.Priority != "p1" || len(got.Labels) != 1 || got.Labels[0] != "bug" || got.Source != "jira" {
		t.Fatalf("Validate = %+v, %v", got, err)
	}
}

func fill(s []string) {
	for i := range s {
		s[i] = "l" + strings.Repeat("x", i)
	}
}

type fakeFeed struct {
	name    string
	entries []Entry
	err     error
	cursors []string
}

func (f *fakeFeed) Name() string { return f.name }
func (f *fakeFeed) Fetch(_ context.Context, cursor string) ([]Entry, string, error) {
	f.cursors = append(f.cursors, cursor)
	return f.entries, cursor + "+", f.err
}

func TestPollOnceFilesWhatTheFeedFetchedAndKeepsItsRecord(t *testing.T) {
	st := openStore(t)
	bad := entry()
	bad.ExternalID, bad.Title = "A2", ""
	f := &fakeFeed{name: "inbox:ops", entries: []Entry{entry(), bad, {ExternalID: "A3", Title: "Other"}}}
	next, err := PollOnce(context.Background(), f, st, "c0", func() time.Time { return at })
	if next != "c0+" || err == nil || !strings.Contains(err.Error(), "A2") {
		t.Fatalf("PollOnce = %q, %v", next, err)
	}
	items, _ := st.List()
	if len(items) != 2 {
		t.Fatalf("jobs = %d", len(items))
	}
	meta, _ := st.SourceMeta("inbox:ops")
	if !meta.Polled.Equal(at) || meta.Polling || !strings.Contains(meta.Trouble, "title is required") {
		t.Fatalf("meta = %+v", meta)
	}
	// A failed fetch keeps the cursor.
	f.err, f.entries = errors.New("down"), nil
	next, err = PollOnce(context.Background(), f, st, "c1", func() time.Time { return at.Add(time.Minute) })
	if next != "c1" || err == nil {
		t.Fatalf("failed PollOnce = %q, %v", next, err)
	}
	meta, _ = st.SourceMeta("inbox:ops")
	if !meta.Polled.Equal(at) || !meta.Tried.Equal(at.Add(time.Minute)) {
		t.Fatalf("meta after failure = %+v", meta)
	}
}

func TestPollOnceSkipsWhileAnotherPollHoldsTheLock(t *testing.T) {
	st := openStore(t)
	release, ok := st.TryPoller()
	if !ok {
		t.Fatal("no poller lock")
	}
	defer release()
	f := &fakeFeed{name: "inbox:ops", entries: []Entry{entry()}}
	if next, err := PollOnce(context.Background(), f, st, "c", time.Now); next != "c" || err != nil || len(f.cursors) != 0 {
		t.Fatalf("PollOnce under another lock = %q %v, fetched %d", next, err, len(f.cursors))
	}
}

func TestPollRefusesAnEntryFromAnotherSource(t *testing.T) {
	st := openStore(t)
	e := entry()
	e.Source = "jira"
	f := &fakeFeed{name: "inbox:ops", entries: []Entry{e}}
	if _, err := PollOnce(context.Background(), f, st, "", time.Now); err == nil || !strings.Contains(err.Error(), `"jira"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestBackoffDoublesToTenMinutes(t *testing.T) {
	if backoff(time.Minute, 0) != time.Minute || backoff(time.Minute, 2) != 4*time.Minute || backoff(time.Minute, 10) != maxBackoff {
		t.Fatal("backoff")
	}
}
