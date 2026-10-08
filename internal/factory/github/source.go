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
}

type seenChecks struct {
	sha   string
	state forge.CheckState
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
		tiers: map[string]factory.Tier{},
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

// Read lists what changed in every watched repository since the mark. A
// repository that cannot be read is skipped with its error joined into the
// answer and its mark left where it was, so the next Read asks it again from
// the same place; the other repositories' items still come back.
func (s *Source) Read(ctx context.Context, since string) ([]factory.Item, string, error) {
	marks, all := parseCursor(since)
	s.mu.Lock()
	repos := append([]string(nil), s.repos...)
	s.mu.Unlock()
	s.whoAmI(ctx)

	next := cursor{}
	for repo, t := range marks {
		next[repo] = t
	}
	var items []factory.Item
	var errs []error
	for _, full := range repos {
		owner, name, ok := strings.Cut(full, "/")
		if !ok {
			continue
		}
		mark, ok := marks[full]
		if !ok {
			mark = all
		}
		got, newest, err := s.readRepo(ctx, owner, name, full, mark)
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

func (s *Source) readRepo(ctx context.Context, owner, name, full string, mark time.Time) ([]factory.Item, time.Time, error) {
	var items []factory.Item
	newest := mark

	s.mu.Lock()
	issueTag, pullTag := s.issueTags[full], s.pullTags[full]
	s.mu.Unlock()

	issues, tag, err := s.api.ListIssues(ctx, owner, name, mark, issueTag)
	switch {
	case errors.Is(err, forge.ErrNotModified):
		issues = nil
	case err != nil:
		return nil, mark, err
	default:
		s.mu.Lock()
		s.issueTags[full] = tag
		s.mu.Unlock()
	}
	for _, is := range issues {
		if is.Updated.After(newest) {
			newest = is.Updated
		}
		if is.Pull {
			continue
		}
		items = append(items, s.issueItem(ctx, owner, name, is))
	}

	pulls, tag, err := s.api.ListPulls(ctx, owner, name, pullTag)
	switch {
	case errors.Is(err, forge.ErrNotModified):
		s.mu.Lock()
		pulls = append([]forge.Pull(nil), s.pulls[full]...)
		s.mu.Unlock()
	case err != nil:
		return nil, mark, err
	default:
		s.mu.Lock()
		s.pullTags[full] = tag
		s.mu.Unlock()
	}
	s.mu.Lock()
	known := map[int]forge.Pull{}
	for _, p := range s.pulls[full] {
		known[p.Number] = p
	}
	s.mu.Unlock()
	kept := make([]forge.Pull, 0, len(pulls))
	for _, p := range pulls {
		if p.Updated.After(newest) {
			newest = p.Updated
		}
		// GitHub's since is inclusive, so the mark itself is a change this source
		// has already seen, and only a later one is a move.
		moved := mark.IsZero() || p.Updated.After(mark)
		// THE LINE COUNTS ARE READ ONCE PER CHANGE. The list leaves them out, so
		// a pull request read before and unchanged since keeps the counts it
		// had, and only one that moved costs a second request.
		if old, ok := known[p.Number]; ok && old.Updated.Equal(p.Updated) {
			p.Additions, p.Deletions, p.Files = old.Additions, old.Deletions, old.Files
		} else if whole, err := s.api.Pull(ctx, owner, name, p.Number); err == nil {
			p.Additions, p.Deletions, p.Files = whole.Additions, whole.Deletions, whole.Files
			if whole.HeadSHA != "" {
				p.HeadSHA = whole.HeadSHA
			}
		} else {
			return nil, mark, err
		}
		kept = append(kept, p)
		// THE CHECKS ARE ASKED AGAIN WHILE THEY RUN. A head that went green
		// does not change the pull request's own updated time, so one whose
		// checks were still running is asked again every Read until they end.
		key := fmt.Sprintf("%s#%d", full, p.Number)
		s.mu.Lock()
		seen, had := s.checks[key]
		s.mu.Unlock()
		state := seen.state
		if !had || seen.sha != p.HeadSHA || seen.state == forge.ChecksRunning || moved {
			state, err = s.api.Checks(ctx, owner, name, p.HeadSHA)
			if err != nil {
				return nil, mark, err
			}
			s.mu.Lock()
			s.checks[key] = seenChecks{sha: p.HeadSHA, state: state}
			s.mu.Unlock()
		}
		if moved || !had || seen.state != state || seen.sha != p.HeadSHA {
			items = append(items, s.pullItem(ctx, owner, name, p, state))
		}
	}
	s.mu.Lock()
	s.pulls[full] = kept
	s.mu.Unlock()
	return items, newest, nil
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
		Checks: ChecksWords(state),
		Diff:   DiffWords(p.Additions, p.Deletions),
		Triage: factory.Triage{Type: "review", Size: pullSize(p.Additions + p.Deletions)},
	}
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
