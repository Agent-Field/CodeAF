// Package github is the factory's GitHub source: the open issues and pull
// requests of the repositories a person chose to watch, read as floor items,
// and the four outward moves a post stage may one day make on them. It fills
// [factory.Source] and nothing more; the floor never learns a vendor.
//
// IT READS FOR NOTHING WHEN NOTHING CHANGED. Every list is asked with the ETag
// its last answer carried, kept in memory per list, and GitHub answers an
// unchanged list with a 304 that costs no rate limit. The issues list is also
// asked `since` the newest change this source has already seen, per
// repository, so a changed list is a short one.
//
// IT NEVER WRITES BY ITSELF. Write exists for a post stage, which is consent
// gated by the surface and does not run yet; the poll (poll.go) only reads.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// Name is the origin written on every item this source makes.
const Name = string(factory.OriginForge)

// bodyLimit is how much of an issue's or pull request's text the floor keeps.
const bodyLimit = 2000

// CommentsMost is how many of an item's comments the floor keeps: the last
// three, which is the discussion's latest turn and all a read needs.
const CommentsMost = 3

// commentLimit is how much of one comment the floor keeps.
const commentLimit = 1000

// commentBudget is how many requests one Read may spend on the per-item reads
// the lists leave out: an issue's or pull request's comments (one request) and
// a pull request's line counts and files (two). PAST IT THE ITEMS ARE FOLDED
// WITHOUT THEM and owed to the next Read, so a first read of a repository
// with three hundred open items lands its rows in seconds and fills their
// comments in over the next minutes, forty requests a tick, instead of
// keeping the floor empty for the whole walk.
const commentBudget = 40

// API is what this source needs of a GitHub client; internal/praf/github's
// client answers it, and a test serves that same client from an
// httptest.Server.
type API interface {
	Me(ctx context.Context) (string, error)
	ListIssues(ctx context.Context, owner, repo string, since time.Time, etag string) ([]forge.Issue, string, error)
	ListPulls(ctx context.Context, owner, repo, etag string) ([]forge.Pull, string, error)
	Pull(ctx context.Context, owner, repo string, number int) (forge.Pull, error)
	Permission(ctx context.Context, owner, repo, user string) (string, error)
	Checks(ctx context.Context, owner, repo, sha string) (forge.CheckState, error)
	CheckRuns(ctx context.Context, owner, repo, sha string) (forge.CheckState, []forge.CheckRun, error)
	Issue(ctx context.Context, owner, repo string, number int) (forge.Issue, error)
	IssueComments(ctx context.Context, owner, repo string, number, count, most int) ([]forge.Comment, error)
	PullFiles(ctx context.Context, owner, repo string, number int) ([]forge.FileChange, error)
	Repos(ctx context.Context) ([]forge.RepoInfo, error)
	Comment(ctx context.Context, owner, repo string, number int, body string) (forge.Posted, error)
	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) (forge.Posted, error)
	Close(ctx context.Context, owner, repo string, number int) (forge.Posted, error)
	OpenPullRequest(ctx context.Context, owner, repo, head string) (int, error)
	DefaultBranch(ctx context.Context, owner, repo string) (string, error)
	CreatePullRequest(ctx context.Context, owner, repo, head, base, title, body string, draft bool) (forge.Posted, error)
}

// RepoInfo is one repository the token can see, for the picker.
type RepoInfo = forge.RepoInfo

// Source reads and writes the watched repositories.
type Source struct {
	api API
	now func() time.Time

	mu    sync.Mutex
	repos []string
	// me is the token's own login, asked once; an author who is the token's
	// owner and may write is the floor's owner.
	me     string
	meDone bool
	// issueTags and pullTags are the last ETag of each repository's lists.
	issueTags map[string]string
	pullTags  map[string]string
	// pulls is each repository's last open-pull list with the line counts read
	// into it, which is what a 304 on the list answers with.
	pulls map[string][]forge.Pull
	// checks is what the head of each pull request added up to last time.
	checks map[string]seenChecks
	// tiers is each repository and login's standing, for the process.
	tiers map[string]factory.Tier
	// comments is each item's last comments and the count they were read at.
	// THE COMMENTS ARE READ ONLY WHEN THE COUNT MOVES: the list already says
	// how many an issue has, so an item whose count is the one this process
	// last read at costs no request, and an item with none costs none ever.
	comments map[string]seenComments
	// files is each pull request's changed files, read with its line counts,
	// once per change.
	files map[string][]factory.FileChange
	// progress, when set, is told before each repository is read which one
	// and how many of how many are done, and again as each of its lists is
	// in with how many items the read has listed so far
	// ([Source.SetProgress], [Source.SetProgressItems]).
	progress func(full string, done, of, items int)
	// sink, when set, is handed each batch of items the moment a list is in,
	// before the slow per-item reads ([Source.SetSink]).
	sink func(items []factory.Item)
	// budget is [commentBudget] unless a test sets less.
	budget int
	// owed is, per repository, the issues listed whose comments the budget
	// did not reach, kept by number with the listing they came in. They are
	// read on a later Read even though a list asked since the cursor will
	// not carry them again.
	owed map[string]map[int]forge.Issue
	// undetailed is the pull requests whose line counts and files the budget
	// did not reach, and pullOwed the ones whose comments it did not; each
	// is read on a later Read and its item handed over again then.
	undetailed map[string]bool
	pullOwed   map[string]bool
}

type seenComments struct {
	count int
	list  []factory.Comment
}

type seenChecks struct {
	sha   string
	state forge.CheckState
	runs  []factory.CheckRun
}

var _ factory.Source = (*Source)(nil)

// New is a source over api watching repos (`owner/name`), with now as its
// clock (time.Now when nil).
func New(api API, repos []string, now func() time.Time) *Source {
	if now == nil {
		now = time.Now
	}
	return &Source{
		api: api, now: now, repos: append([]string(nil), repos...),
		issueTags: map[string]string{}, pullTags: map[string]string{},
		pulls: map[string][]forge.Pull{}, checks: map[string]seenChecks{},
		tiers: map[string]factory.Tier{}, comments: map[string]seenComments{},
		files: map[string][]factory.FileChange{}, budget: commentBudget,
		owed: map[string]map[int]forge.Issue{}, undetailed: map[string]bool{}, pullOwed: map[string]bool{},
	}
}

// Name is "github".
func (s *Source) Name() string { return Name }

// Watch replaces the watched repositories. A repository newly watched is read
// whole on the next Read, because the cursor has no mark for it.
func (s *Source) Watch(repos []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos = append([]string(nil), repos...)
}

// SetProgress hands the source a function told, before each repository a
// Read reads and again as each of its lists is in, which one it is and how
// many of how many are done, so the poll can write where the read is
// ([PollOnce]). nil takes it off. [Source.SetProgressItems] is the same with
// the items listed so far.
func (s *Source) SetProgress(fn func(full string, done, of int)) {
	if fn == nil {
		s.SetProgressItems(nil)
		return
	}
	s.SetProgressItems(func(full string, done, of, _ int) { fn(full, done, of) })
}

// SetProgressItems is [Source.SetProgress] with how many issues and pull
// requests this Read has listed so far, across every repository, so a big
// repository's read says `200 items so far` rather than go quiet. nil takes
// it off.
func (s *Source) SetProgressItems(fn func(full string, done, of, items int)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress = fn
}

// SetSink hands the source a function a Read gives each batch of items to the
// moment it has them: a repository's issues as soon as their list is in
// (before any comment is read), and its pull requests once theirs are. The
// poll folds each batch into the store at once ([PollOnce]), so THE FLOOR
// FILLS AS THE READ GOES. Read still answers every item at the end; a batch
// handed early is handed again there, which [Merge] skips when nothing moved.
// nil takes it off.
func (s *Source) SetSink(fn func(items []factory.Item)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sink = fn
}

// Watched is the repositories this source reads.
func (s *Source) Watched() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.repos...)
}

// ListRepos is every repository the token can see, most recently pushed
// first, for the picker.
func (s *Source) ListRepos(ctx context.Context) ([]RepoInfo, error) {
	return s.api.Repos(ctx)
}

// cursor is the mark Read hands back: per repository, the newest change seen.
// It is JSON so a repository watched later starts with no mark of its own. A
// bare RFC 3339 time, which is what a caller may hand in by hand, is the mark
// for every repository.
type cursor map[string]time.Time

func parseCursor(since string) (cursor, time.Time) {
	since = strings.TrimSpace(since)
	if since == "" {
		return cursor{}, time.Time{}
	}
	var c cursor
	if json.Unmarshal([]byte(since), &c) == nil && c != nil {
		return c, time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, since); err == nil {
		return cursor{}, t
	}
	return cursor{}, time.Time{}
}

func (c cursor) String() string {
	if len(c) == 0 {
		return ""
	}
	b, _ := json.Marshal(map[string]time.Time(c))
	return string(b)
}

// readRun is one Read's running state: what it may still spend, how many
// items it has listed, and where to hand batches and progress.
type readRun struct {
	left     int
	items    int
	done, of int
	sink     func([]factory.Item)
	progress func(full string, done, of, items int)
}

// take spends n requests of the budget, and says whether there were n left.
func (r *readRun) take(n int) bool {
	if r.left < n {
		return false
	}
	r.left -= n
	return true
}

func (r *readRun) hand(items []factory.Item) {
	if r.sink != nil && len(items) > 0 {
		r.sink(items)
	}
}

func (r *readRun) tell(full string) {
	if r.progress != nil {
		r.progress(full, r.done, r.of, r.items)
	}
}

// Read lists what changed in every watched repository since the mark. A
// repository that cannot be read is skipped with its error joined into the
// answer and its mark left where it was, so the next Read asks it again from
// the same place; the other repositories' items still come back. Items the
// per-read budget did not reach come back without their comments (nil, not
// read yet) or a pull request without its line counts and files (Files nil),
// and are read on a later Read.
func (s *Source) Read(ctx context.Context, since string) ([]factory.Item, string, error) {
	marks, all := parseCursor(since)
	s.mu.Lock()
	repos := append([]string(nil), s.repos...)
	run := &readRun{left: s.budget, of: len(repos), sink: s.sink, progress: s.progress}
	s.mu.Unlock()
	s.whoAmI(ctx)

	next := cursor{}
	for repo, t := range marks {
		next[repo] = t
	}
	var items []factory.Item
	var errs []error
	for i, full := range repos {
		owner, name, ok := strings.Cut(full, "/")
		if !ok {
			continue
		}
		run.done = i
		run.tell(full)
		mark, ok := marks[full]
		if !ok {
			mark = all
		}
		got, newest, err := s.readRepo(ctx, run, owner, name, full, mark)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", full, err))
			continue
		}
		items = append(items, got...)
		if newest.After(mark) {
			next[full] = newest
		} else if !mark.IsZero() {
			next[full] = mark
		}
	}
	return items, next.String(), errors.Join(errs...)
}

// whoAmI learns the token's login once. A failure is tried again next Read;
// until then nobody is the owner, which only under-trusts.
func (s *Source) whoAmI(ctx context.Context) {
	s.mu.Lock()
	done := s.meDone
	s.mu.Unlock()
	if done {
		return
	}
	me, err := s.api.Me(ctx)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.me, s.meDone = me, true
	s.mu.Unlock()
}

// readRepo reads one repository: its issues list, handed to the sink at once;
// their comments within the budget; its pull requests with their line counts,
// files and checks, handed to the sink once they are in. THE LISTS' ETAGS ARE
// KEPT ONLY WHEN THE WHOLE REPOSITORY READ WELL, so a repository that fails
// halfway is listed in full again next time rather than answered with a 304
// that would hide what this read never finished.
func (s *Source) readRepo(ctx context.Context, run *readRun, owner, name, full string, mark time.Time) ([]factory.Item, time.Time, error) {
	var items []factory.Item
	newest := mark

	s.mu.Lock()
	issueTag, pullTag := s.issueTags[full], s.pullTags[full]
	s.mu.Unlock()

	issues, newIssueTag, err := s.api.ListIssues(ctx, owner, name, mark, issueTag)
	switch {
	case errors.Is(err, forge.ErrNotModified):
		issues, newIssueTag = nil, issueTag
	case err != nil:
		return nil, mark, err
	}
	var listed []factory.Item
	var listedIssues []forge.Issue
	for _, is := range issues {
		if is.Updated.After(newest) {
			newest = is.Updated
		}
		if is.Pull {
			continue
		}
		listed = append(listed, s.issueItem(ctx, owner, name, is))
		listedIssues = append(listedIssues, is)
	}
	// THE ROWS FIRST: the issues as listed, comments not read yet, so the
	// floor has them while their comments are read.
	run.items += len(listed)
	run.hand(listed)
	run.tell(full)
	s.mu.Lock()
	owed := s.owed[full]
	if owed == nil {
		owed = map[int]forge.Issue{}
		s.owed[full] = owed
	}
	s.mu.Unlock()
	for i, is := range listedIssues {
		it := listed[i]
		list, ok, err := s.commentsWithin(ctx, run, owner, name, is.Number, is.Comments)
		if err != nil {
			return nil, mark, err
		}
		s.mu.Lock()
		if ok {
			it.Comments = list
			delete(owed, is.Number)
		} else {
			owed[is.Number] = is
		}
		s.mu.Unlock()
		items = append(items, it)
	}
	// AND THE ISSUES AN EARLIER READ OWED, which a list asked since the
	// cursor no longer carries, oldest number first, while the budget lasts.
	s.mu.Lock()
	var due []forge.Issue
	for n, is := range owed {
		if !listedNumber(listedIssues, n) {
			due = append(due, is)
		}
	}
	s.mu.Unlock()
	sort.Slice(due, func(i, j int) bool { return due[i].Number < due[j].Number })
	for _, is := range due {
		list, ok, err := s.commentsWithin(ctx, run, owner, name, is.Number, is.Comments)
		if err != nil {
			return nil, mark, err
		}
		if !ok {
			break
		}
		it := s.issueItem(ctx, owner, name, is)
		it.Comments = list
		s.mu.Lock()
		delete(owed, is.Number)
		s.mu.Unlock()
		items = append(items, it)
	}

	pulls, newPullTag, err := s.api.ListPulls(ctx, owner, name, pullTag)
	switch {
	case errors.Is(err, forge.ErrNotModified):
		s.mu.Lock()
		pulls = append([]forge.Pull(nil), s.pulls[full]...)
		s.mu.Unlock()
		newPullTag = pullTag
	case err != nil:
		return nil, mark, err
	}
	run.items += len(pulls)
	run.tell(full)
	s.mu.Lock()
	known := map[int]forge.Pull{}
	for _, p := range s.pulls[full] {
		known[p.Number] = p
	}
	s.mu.Unlock()
	kept := make([]forge.Pull, 0, len(pulls))
	var pullItems []factory.Item
	for _, p := range pulls {
		if p.Updated.After(newest) {
			newest = p.Updated
		}
		// GitHub's since is inclusive, so the mark itself is a change this source
		// has already seen, and only a later one is a move.
		moved := mark.IsZero() || p.Updated.After(mark)
		key := fmt.Sprintf("%s#%d", full, p.Number)
		s.mu.Lock()
		undetailed, owedComments := s.undetailed[key], s.pullOwed[key]
		s.mu.Unlock()
		// THE LINE COUNTS ARE READ ONCE PER CHANGE. The list leaves them out, so
		// a pull request read before and unchanged since keeps the counts it
		// had, and only one that moved costs a second request. One the budget
		// did not reach keeps the list's zero counts and is marked undetailed,
		// so its counts are read on a later Read.
		detailed := false
		if old, ok := known[p.Number]; ok && old.Updated.Equal(p.Updated) && !undetailed {
			p.Additions, p.Deletions, p.Files, p.Comments = old.Additions, old.Deletions, old.Files, old.Comments
		} else if !run.take(2) {
			undetailed = true
		} else if whole, err := s.api.Pull(ctx, owner, name, p.Number); err == nil {
			p.Additions, p.Deletions, p.Files, p.Comments = whole.Additions, whole.Deletions, whole.Files, whole.Comments
			if whole.HeadSHA != "" {
				p.HeadSHA = whole.HeadSHA
			}
			// THE FILES ARE READ WITH THE LINE COUNTS, once per change, for
			// the same reason: the list carries neither.
			changed, err := s.api.PullFiles(ctx, owner, name, p.Number)
			if err != nil {
				return nil, mark, err
			}
			detailed, undetailed = true, false
			s.mu.Lock()
			s.files[key] = fileChanges(changed)
			s.mu.Unlock()
		} else {
			return nil, mark, err
		}
		s.mu.Lock()
		if undetailed {
			s.undetailed[key] = true
		} else {
			delete(s.undetailed, key)
		}
		s.mu.Unlock()
		kept = append(kept, p)
		// THE CHECKS ARE ASKED AGAIN WHILE THEY RUN. A head that went green
		// does not change the pull request's own updated time, so one whose
		// checks were still running is asked again every Read until they end.
		s.mu.Lock()
		seen, had := s.checks[key]
		s.mu.Unlock()
		state, runs := seen.state, seen.runs
		if !had || seen.sha != p.HeadSHA || seen.state == forge.ChecksRunning || moved {
			var got []forge.CheckRun
			state, got, err = s.api.CheckRuns(ctx, owner, name, p.HeadSHA)
			if err != nil {
				return nil, mark, err
			}
			runs = checkRuns(got)
			s.mu.Lock()
			s.checks[key] = seenChecks{sha: p.HeadSHA, state: state, runs: runs}
			s.mu.Unlock()
		}
		if moved || !had || seen.state != state || seen.sha != p.HeadSHA || detailed || owedComments {
			it := s.pullItem(ctx, owner, name, p, state)
			it.CheckRuns = runs
			if undetailed {
				// Not read yet: no files, no line counts, no comment count.
				it.Diff, it.Files, it.Comments = "", nil, nil
				s.mu.Lock()
				s.pullOwed[key] = true
				s.mu.Unlock()
			} else {
				s.mu.Lock()
				it.Files = s.files[key]
				s.mu.Unlock()
				list, ok, err := s.commentsWithin(ctx, run, owner, name, p.Number, p.Comments)
				if err != nil {
					return nil, mark, err
				}
				s.mu.Lock()
				if ok {
					it.Comments = list
					delete(s.pullOwed, key)
				} else {
					s.pullOwed[key] = true
				}
				s.mu.Unlock()
			}
			pullItems = append(pullItems, it)
		}
	}
	run.hand(pullItems)
	items = append(items, pullItems...)
	s.mu.Lock()
	s.pulls[full] = kept
	if newIssueTag != "" {
		s.issueTags[full] = newIssueTag
	}
	if newPullTag != "" {
		s.pullTags[full] = newPullTag
	}
	s.mu.Unlock()
	return items, newest, nil
}

func listedNumber(issues []forge.Issue, n int) bool {
	for _, is := range issues {
		if is.Number == n {
			return true
		}
	}
	return false
}

// commentsWithin is [Source.lastComments] on the read's budget: the comments
// and true when they cost nothing (none, or the count this process already
// read them at) or the budget had a request left, and nil and false when it
// did not, so the item is folded with its comments not read yet.
func (s *Source) commentsWithin(ctx context.Context, run *readRun, owner, name string, number, count int) ([]factory.Comment, bool, error) {
	if count > 0 {
		key := strings.ToLower(fmt.Sprintf("%s/%s#%d", owner, name, number))
		s.mu.Lock()
		seen, ok := s.comments[key]
		s.mu.Unlock()
		if !(ok && seen.count == count) && !run.take(1) {
			return nil, false, nil
		}
	}
	list, err := s.lastComments(ctx, owner, name, number, count, false)
	if err != nil {
		return nil, false, err
	}
	return list, true, nil
}

func (s *Source) issueItem(ctx context.Context, owner, name string, is forge.Issue) factory.Item {
	body := clip(is.Body, bodyLimit)
	return factory.Item{
		Repo: name, Product: owner, Places: []string{name},
		Num: is.Number, Kind: factory.KindIssue,
		Title: is.Title, Body: body, Author: is.User,
		Tier:   s.tier(ctx, owner, name, is.User),
		Origin: factory.OriginForge, Synced: true,
		Created: is.Created, Changed: is.Updated,
		State:  factory.StateNew,
		Labels: is.Labels,
		URL:    is.URL,
		Triage: factory.Triage{Type: issueType(is), Size: issueSize(is.Body)},
	}
}

// issueType is the kind word the floor draws for an issue: the first label that
// names one (bug, feature, chore, question and their usual synonyms), else a
// guess from the title and the first line of the body. A pull request keeps
// "review" in pullItem and is not asked.
func issueType(is forge.Issue) string {
	for _, l := range is.Labels {
		if t := factory.LabelType(l); t != "" {
			return t
		}
	}
	return factory.GuessType(is.Title)
}

func (s *Source) pullItem(ctx context.Context, owner, name string, p forge.Pull, state forge.CheckState) factory.Item {
	return factory.Item{
		Repo: name, Product: owner, Places: []string{name},
		Num: p.Number, Kind: factory.KindPR,
		Title: p.Title, Body: clip(p.Body, bodyLimit), Author: p.User,
		Tier:   s.tier(ctx, owner, name, p.User),
		Origin: factory.OriginForge, Synced: true,
		Created: p.Created, Changed: p.Updated,
		State:  factory.StateNew,
		Labels: p.Labels,
		URL:    p.URL,
		Checks: ChecksWords(state),
		Diff:   DiffWords(p.Additions, p.Deletions),
		Triage: factory.Triage{Type: "review", Size: pullSize(p.Additions + p.Deletions)},
	}
}

// lastComments is an item's last [CommentsMost] comments: the ones this
// process already read when the count is the one it read them at, and a
// fresh read otherwise, or always when force is set (a person's refresh). A
// count of none is none, and costs nothing. THE ANSWER IS NEVER NIL: an empty
// list is read and nothing said, and nil on an item is not read yet.
func (s *Source) lastComments(ctx context.Context, owner, name string, number, count int, force bool) ([]factory.Comment, error) {
	key := strings.ToLower(fmt.Sprintf("%s/%s#%d", owner, name, number))
	if count <= 0 {
		s.mu.Lock()
		delete(s.comments, key)
		s.mu.Unlock()
		return []factory.Comment{}, nil
	}
	s.mu.Lock()
	seen, ok := s.comments[key]
	s.mu.Unlock()
	if ok && seen.count == count && !force {
		return seen.list, nil
	}
	got, err := s.api.IssueComments(ctx, owner, name, number, count, CommentsMost)
	if err != nil {
		return nil, err
	}
	list := make([]factory.Comment, 0, len(got))
	for _, c := range got {
		list = append(list, factory.Comment{Author: c.User, Body: clip(strings.TrimSpace(c.Body), commentLimit), At: c.Created})
	}
	s.mu.Lock()
	s.comments[key] = seenComments{count: count, list: list}
	s.mu.Unlock()
	return list, nil
}

// fileChanges converts the forge's files to the floor's. It is never nil: a
// pull request's Files nil is its files not read yet.
func fileChanges(in []forge.FileChange) []factory.FileChange {
	if len(in) == 0 {
		return []factory.FileChange{}
	}
	out := make([]factory.FileChange, 0, len(in))
	for _, f := range in {
		out = append(out, factory.FileChange{Path: f.Path, Added: f.Additions, Removed: f.Deletions})
	}
	return out
}

// checkRuns converts the forge's check runs to the floor's: a finished run
// says its conclusion, an unfinished one its status.
func checkRuns(in []forge.CheckRun) []factory.CheckRun {
	if len(in) == 0 {
		return nil
	}
	out := make([]factory.CheckRun, 0, len(in))
	for _, r := range in {
		state := r.Conclusion
		if r.Status != "completed" || state == "" {
			state = r.Status
		}
		out = append(out, factory.CheckRun{Name: r.Name, State: state, URL: r.URL})
	}
	return out
}

// Refetch reads one item again from GitHub now, whatever this source last
// saw: the issue or pull request by its number, its last comments (read even
// when the count did not move, because a person asked), and for a pull
// request its line counts, files and check runs. It answers the item as a
// fresh read would make it, for [Merge] to fold in. An item that is not from
// GitHub, or has no number, is refused.
func (s *Source) Refetch(ctx context.Context, it factory.Item) (factory.Item, error) {
	if it.Origin != factory.OriginForge || it.Num <= 0 || it.Product == "" || it.Repo == "" {
		return factory.Item{}, factory.ErrNotOnSource
	}
	owner, name := it.Product, it.Repo
	s.whoAmI(ctx)
	if it.Kind != factory.KindPR {
		is, err := s.api.Issue(ctx, owner, name, it.Num)
		if err != nil {
			return factory.Item{}, err
		}
		out := s.issueItem(ctx, owner, name, is)
		if out.Comments, err = s.lastComments(ctx, owner, name, is.Number, is.Comments, true); err != nil {
			return factory.Item{}, err
		}
		s.mu.Lock()
		delete(s.owed[owner+"/"+name], is.Number)
		s.mu.Unlock()
		return out, nil
	}
	p, err := s.api.Pull(ctx, owner, name, it.Num)
	if err != nil {
		return factory.Item{}, err
	}
	changed, err := s.api.PullFiles(ctx, owner, name, it.Num)
	if err != nil {
		return factory.Item{}, err
	}
	state, runs, err := s.api.CheckRuns(ctx, owner, name, p.HeadSHA)
	if err != nil {
		return factory.Item{}, err
	}
	key := fmt.Sprintf("%s/%s#%d", owner, name, p.Number)
	s.mu.Lock()
	s.files[key] = fileChanges(changed)
	s.checks[key] = seenChecks{sha: p.HeadSHA, state: state, runs: checkRuns(runs)}
	delete(s.undetailed, key)
	delete(s.pullOwed, key)
	s.mu.Unlock()
	out := s.pullItem(ctx, owner, name, p, state)
	out.Files, out.CheckRuns = fileChanges(changed), checkRuns(runs)
	if out.Comments, err = s.lastComments(ctx, owner, name, p.Number, p.Comments, true); err != nil {
		return factory.Item{}, err
	}
	return out, nil
}

// tier is an author's standing on one repository, asked once per process.
// Write access makes a collaborator, and the token's own login with write
// access is the owner; read access, none, a 404 or a refusal is a stranger. A
// failure to ask is a stranger for this Read and is asked again next time.
func (s *Source) tier(ctx context.Context, owner, name, login string) factory.Tier {
	if login == "" {
		return factory.TierStranger
	}
	key := strings.ToLower(owner + "/" + name + "|" + login)
	s.mu.Lock()
	t, ok := s.tiers[key]
	me := s.me
	s.mu.Unlock()
	if ok {
		return t
	}
	perm, err := s.api.Permission(ctx, owner, name, login)
	if err != nil {
		return factory.TierStranger
	}
	switch perm {
	case "admin", "maintain", "write":
		t = factory.TierCollab
		if me != "" && strings.EqualFold(me, login) {
			t = factory.TierOwner
		}
	default:
		t = factory.TierStranger
	}
	s.mu.Lock()
	s.tiers[key] = t
	s.mu.Unlock()
	return t
}

// ChecksWords is a head's checks in the words the floor's pull-request row
// already draws: `ci ✓`, `ci running`, `ci ✕`, and nothing for a head nothing
// checked.
func ChecksWords(state forge.CheckState) string {
	switch state {
	case forge.ChecksPassed:
		return "ci ✓"
	case forge.ChecksRunning:
		return "ci running"
	case forge.ChecksFailed:
		return "ci ✕"
	}
	return ""
}

// DiffWords is `+N −M`; a pull request with no lines either way has no diff
// to speak of and says nothing.
func DiffWords(adds, dels int) string {
	if adds == 0 && dels == 0 {
		return ""
	}
	return fmt.Sprintf("+%d −%d", adds, dels)
}

// pullSize is S up to fifty changed lines, M up to four hundred, L beyond.
func pullSize(lines int) string {
	switch {
	case lines <= 50:
		return "S"
	case lines <= 400:
		return "M"
	}
	return "L"
}

// issueSize is the same three letters read off how much the author wrote: S
// up to four hundred characters, M up to two thousand, L beyond. It is a
// cheap guess, and the floor treats it as one.
func issueSize(body string) string {
	n := utf8.RuneCountInString(strings.TrimSpace(body))
	switch {
	case n <= 400:
		return "S"
	case n <= 2000:
		return "M"
	}
	return "L"
}

// clip keeps the first n characters, never splitting one.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

// Write performs one outward move on one item: `comment`, `label`, `pr` (or
// `open-pr`) and `close`. Anything else is refused as this source not having
// that verb, and so is an item on a repository nobody watches.
func (s *Source) Write(ctx context.Context, a factory.Action) (factory.Receipt, error) {
	owner, name := a.Item.Product, a.Item.Repo
	full := owner + "/" + name
	if !s.watching(full) {
		return factory.Receipt{}, fmt.Errorf("github writes only to a watched repository, and %s is not one: %w", full, factory.ErrReadOnly)
	}
	var (
		posted forge.Posted
		err    error
	)
	switch a.Verb {
	case "comment":
		if a.Item.Num <= 0 || strings.TrimSpace(a.Body) == "" {
			return factory.Receipt{}, errors.New("a comment needs an item with a number and some words")
		}
		posted, err = s.api.Comment(ctx, owner, name, a.Item.Num, a.Body)
	case "label":
		if a.Item.Num <= 0 || len(a.Labels) == 0 {
			return factory.Receipt{}, errors.New("labels need an item with a number and at least one label")
		}
		posted, err = s.api.AddLabels(ctx, owner, name, a.Item.Num, a.Labels)
	case "close":
		if a.Item.Num <= 0 {
			return factory.Receipt{}, errors.New("only an item with a number can be closed")
		}
		posted, err = s.api.Close(ctx, owner, name, a.Item.Num)
	case "pr", "open-pr":
		posted, err = s.openPull(ctx, owner, name, a)
	default:
		return factory.Receipt{}, fmt.Errorf("github cannot %q: %w", a.Verb, factory.ErrReadOnly)
	}
	if err != nil {
		return factory.Receipt{}, err
	}
	return factory.Receipt{URL: posted.URL, ID: posted.ID, At: s.now()}, nil
}

// openPull opens a pull request from the action's branch into the
// repository's default branch, or answers the one already open from that
// branch, so a stage that runs twice never opens two.
func (s *Source) openPull(ctx context.Context, owner, name string, a factory.Action) (forge.Posted, error) {
	branch := strings.TrimSpace(a.Branch)
	if branch == "" {
		return forge.Posted{}, errors.New("a pull request needs a branch")
	}
	head := branch
	if !strings.Contains(head, ":") {
		head = owner + ":" + branch
	}
	if n, err := s.api.OpenPullRequest(ctx, owner, name, head); err != nil {
		return forge.Posted{}, err
	} else if n > 0 {
		return forge.Posted{URL: fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, name, n), ID: fmt.Sprint(n), Number: n}, nil
	}
	base, err := s.api.DefaultBranch(ctx, owner, name)
	if err != nil {
		return forge.Posted{}, err
	}
	title := strings.TrimSpace(a.Item.Title)
	if title == "" {
		title = branch
	}
	return s.api.CreatePullRequest(ctx, owner, name, branch, base, title, a.Body, a.Draft)
}

func (s *Source) watching(full string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if strings.EqualFold(r, full) {
			return true
		}
	}
	return false
}
