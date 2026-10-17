package github

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// PeopleAPI is the public-profile and default-branch contents forge boundary.
type PeopleAPI interface {
	PipelineAPI
	User(context.Context, string, string) (forge.UserProfile, string, error)
}

// ProfileTTL is the freshness window shared by queue summaries and cards.
const ProfileTTL = 24 * time.Hour

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
type PeopleLoader struct {
	mu       sync.Mutex
	api      func(context.Context) PeopleAPI
	now      func() time.Time
	profiles map[string]profileRead
	owners   map[string]ownerRead
}

// NewPeopleLoader binds the forge and an optional deterministic clock.
func NewPeopleLoader(api func(context.Context) PeopleAPI, now func() time.Time) *PeopleLoader {
	if now == nil {
		now = time.Now
	}
	return &PeopleLoader{api: api, now: now, profiles: map[string]profileRead{}, owners: map[string]ownerRead{}}
}

// Profile keeps a successful conditional response without dropping its fields.
func (l *PeopleLoader) Profile(ctx context.Context, handle string) (factory.GitHubPerson, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	handle = factory.NormalizeHandle(handle)
	seen, ok := l.profiles[handle]
	if ok && l.now().Before(seen.until) {
		return seen.profile, nil
	}
	var api PeopleAPI
	if l.api != nil {
		api = l.api(ctx)
	}
	if api == nil {
		return factory.GitHubPerson{}, errors.New("github is not connected")
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
	seen.until = l.now().Add(ProfileTTL)
	l.profiles[handle] = seen
	return seen.profile, nil
}

// RefreshOwners invalidates both missing files and failures on every poll.
func (l *PeopleLoader) RefreshOwners(ctx context.Context, repo string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.owners[repo] = l.readOwners(ctx, repo)
}

// Owners returns independent rules so a caller cannot mutate the poll cache.
func (l *PeopleLoader) Owners(ctx context.Context, repo string) ([]factory.CodeOwner, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
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
