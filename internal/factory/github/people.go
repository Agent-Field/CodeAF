package github

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/guard"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// PeopleAPI is the public-profile and default-branch contents forge boundary.
type PeopleAPI interface {
	PipelineAPI
	User(context.Context, string, string) (forge.UserProfile, string, error)
}

// ProfileTTL is the freshness window shared by queue summaries and cards.
const ProfileTTL = 24 * time.Hour

// ProfileRetry is how long a profile that could not be read in the background
// waits before it is asked for again. A forge that is rate limiting, or a
// handle it no longer knows, would otherwise be asked once per person on
// every read of every list that names them.
const ProfileRetry = 15 * time.Minute

// profileReadTimeout bounds one background profile read, so a forge that
// stops answering cannot hold the queue of handles waiting behind it forever.
const profileReadTimeout = 20 * time.Second

type profileRead struct {
	profile factory.GitHubPerson
	tag     string
	until   time.Time
}

type ownerRead struct {
	rules []factory.CodeOwner
	err   error
}

// PeopleLoader reuses profiles for 24 hours and ownership until the next poll.
//
// THE PROFILE LOCK IS NEVER HELD OVER A NETWORK CALL. A list answers from
// [PeopleLoader.Known] while a card waits on [PeopleLoader.Profile] or the
// background reader fetches, and neither may make the list wait for GitHub.
type PeopleLoader struct {
	mu       sync.Mutex
	api      func(context.Context) PeopleAPI
	now      func() time.Time
	profiles map[string]profileRead
	// wanted is the handles a list asked for that are not read yet, in the
	// order they were asked; asked dedupes it. failed holds when a handle
	// whose background read failed may be asked for again. reading is true
	// while the one background reader runs.
	wanted  []string
	asked   map[string]bool
	failed  map[string]time.Time
	reading bool

	// ownersMu is separate because an ownership read does hold its lock over
	// the forge call: it happens once per repository per poll, and two
	// callers asking for the same file should share one read.
	ownersMu sync.Mutex
	owners   map[string]ownerRead
}

// NewPeopleLoader binds the forge and an optional deterministic clock.
func NewPeopleLoader(api func(context.Context) PeopleAPI, now func() time.Time) *PeopleLoader {
	if now == nil {
		now = time.Now
	}
	return &PeopleLoader{api: api, now: now, profiles: map[string]profileRead{}, owners: map[string]ownerRead{},
		asked: map[string]bool{}, failed: map[string]time.Time{}}
}

// Profile keeps a successful conditional response without dropping its fields.
// It waits for GitHub when the cached profile is missing or old, which is what
// a card about one person wants; a list of many people reads [PeopleLoader.Known].
func (l *PeopleLoader) Profile(ctx context.Context, handle string) (factory.GitHubPerson, error) {
	handle = factory.NormalizeHandle(handle)
	l.mu.Lock()
	seen, ok := l.profiles[handle]
	fresh := ok && l.now().Before(seen.until)
	l.mu.Unlock()
	if fresh {
		return seen.profile, nil
	}
	return l.read(ctx, handle, seen, ok)
}

// Known answers the profile already read for handle without waiting on
// GitHub, and whether there is one. A missing or old profile is asked for in
// the background, so the next read of the same list carries it. A LIST OF
// HUNDREDS OF PEOPLE NEVER WAITS ON HUNDREDS OF PROFILE READS: that was one
// forge call per author, one after another, on every queue read.
func (l *PeopleLoader) Known(handle string) (factory.GitHubPerson, bool) {
	handle = factory.NormalizeHandle(handle)
	if handle == "" {
		return factory.GitHubPerson{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen, ok := l.profiles[handle]
	if !ok || !l.now().Before(seen.until) {
		l.wantLocked(handle)
	}
	return seen.profile, ok
}

// wantLocked queues handle for the background reader unless it is queued
// already or failed recently, and starts the reader when none is running.
func (l *PeopleLoader) wantLocked(handle string) {
	if l.asked[handle] || l.now().Before(l.failed[handle]) {
		return
	}
	l.asked[handle] = true
	l.wanted = append(l.wanted, handle)
	if l.reading {
		return
	}
	l.reading = true
	guard.Go("factory/people-profiles", l.readWanted)
}

// readWanted reads queued handles one at a time until none are left. One
// reader, one request at a time, is deliberate: the forge's rate limit is
// shared with the poll, and a list's names arriving a little later is better
// than the poll being refused.
func (l *PeopleLoader) readWanted() {
	for {
		l.mu.Lock()
		if len(l.wanted) == 0 {
			l.reading = false
			l.mu.Unlock()
			return
		}
		handle := l.wanted[0]
		l.wanted = l.wanted[1:]
		seen, ok := l.profiles[handle]
		asked := l.now()
		l.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), profileReadTimeout)
		_, err := l.read(ctx, handle, seen, ok)
		cancel()
		l.mu.Lock()
		delete(l.asked, handle)
		if err != nil {
			// The wait counts from the ask, so a slow refusal does not
			// lengthen it.
			l.failed[handle] = asked.Add(ProfileRetry)
		} else {
			delete(l.failed, handle)
		}
		l.mu.Unlock()
	}
}

// read asks the forge for handle's profile, sending the tag of the copy seen
// so an unchanged profile costs a conditional request, and keeps the answer.
func (l *PeopleLoader) read(ctx context.Context, handle string, seen profileRead, ok bool) (factory.GitHubPerson, error) {
	var api PeopleAPI
	if l.api != nil {
		api = l.api(ctx)
	}
	if api == nil {
		return seen.profile, errors.New("github is not connected")
	}
	profile, tag, err := api.User(ctx, handle, seen.tag)
	if errors.Is(err, forge.ErrNotModified) && ok {
		err = nil
	} else if err == nil {
		seen.profile = factory.GitHubPerson{PersonSummary: factory.PersonSummary{Name: profile.Name, AvatarURL: profile.AvatarURL}, HTMLURL: profile.HTMLURL}
		seen.tag = tag
	}
	if err != nil {
		return seen.profile, err
	}
	l.mu.Lock()
	seen.until = l.now().Add(ProfileTTL)
	l.profiles[handle] = seen
	l.mu.Unlock()
	return seen.profile, nil
}

// RefreshOwners invalidates both missing files and failures on every poll.
func (l *PeopleLoader) RefreshOwners(ctx context.Context, repo string) {
	l.ownersMu.Lock()
	defer l.ownersMu.Unlock()
	l.owners[repo] = l.readOwners(ctx, repo)
}

// Owners returns independent rules so a caller cannot mutate the poll cache.
func (l *PeopleLoader) Owners(ctx context.Context, repo string) ([]factory.CodeOwner, error) {
	l.ownersMu.Lock()
	defer l.ownersMu.Unlock()
	seen, ok := l.owners[repo]
	if !ok {
		seen = l.readOwners(ctx, repo)
		l.owners[repo] = seen
	}
	out := make([]factory.CodeOwner, len(seen.rules))
	for i, rule := range seen.rules {
		out[i] = factory.CodeOwner{Pattern: rule.Pattern, Handles: append([]string(nil), rule.Handles...)}
	}
	return out, seen.err
}

func (l *PeopleLoader) readOwners(ctx context.Context, repo string) ownerRead {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return ownerRead{}
	}
	var api PeopleAPI
	if l.api != nil {
		api = l.api(ctx)
	}
	if api == nil {
		return ownerRead{}
	}
	for _, path := range []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"} {
		text, _, _, err := api.Contents(ctx, owner, name, path, "", "")
		if missing(err) {
			continue
		}
		if err != nil {
			return ownerRead{err: err}
		}
		return ownerRead{rules: factory.ParseCodeOwners(text)}
	}
	return ownerRead{}
}
