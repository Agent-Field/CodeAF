package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// ResolveTier asks for this author's permission, independently of poll caches.
// The source must be composed with the factory's read credential.
func (s *Source) ResolveTier(ctx context.Context, it factory.Item) (factory.Tier, time.Time, error) {
	owner, repo, ok := strings.Cut(factory.RepoOf(it), "/")
	if !ok || owner == "" || repo == "" || it.Author == "" {
		return "", time.Time{}, errors.New("author or repository is missing")
	}
	api, ok := s.api.(interface {
		PermissionStrict(context.Context, string, string, string) (string, error)
	})
	if !ok {
		return "", time.Time{}, errors.New("author permission lookup is unavailable")
	}
	perm, err := api.PermissionStrict(ctx, owner, repo, it.Author)
	if err != nil {
		var apiErr *forge.APIError
		if errors.As(err, &apiErr) {
			// Do not carry GitHub's response body, URL, or credential into the job view.
			return "", time.Now().Add(apiErr.RetryAfter), fmt.Errorf("GitHub permission lookup returned HTTP %d", apiErr.StatusCode)
		}
		return "", time.Time{}, errors.New("GitHub permission lookup failed or timed out")
	}
	switch perm {
	case "admin", "maintain", "write":
		s.whoAmI(ctx)
		s.mu.Lock()
		me := s.me
		s.mu.Unlock()
		if me != "" && strings.EqualFold(me, it.Author) {
			return factory.TierOwner, time.Time{}, nil
		}
		return factory.TierCollab, time.Time{}, nil
	case "read", "triage", "none":
		return factory.TierStranger, time.Time{}, nil
	default:
		return "", time.Time{}, errors.New("GitHub returned an unknown permission")
	}
}
