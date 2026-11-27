package serve

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

const (
	reposReadEvery       = 5 * time.Minute
	reposRefreshCooldown = 30 * time.Second
)

type reposReader struct {
	mu     sync.Mutex
	walk   *reposWalk
	cancel context.CancelFunc
	closed bool
	tried  time.Time
	key    string
	err    error
}

type reposWalk struct {
	done     chan struct{}
	identity string
	key      string
	// A waiter reads this after done closes, even if another walk has started.
	err error
}

var errReposConnectionChanged = errors.New("GitHub connection changed during repository discovery")

func (h *Handler) reposCredential(ctx context.Context) (string, error) {
	token := ""
	var err error
	if h.config.GitHubToken != nil && h.mode() == api.FactorySolo {
		token = h.config.GitHubToken(ctx)
	} else if h.mode() == api.FactoryTeam {
		token, _, err = factorygithub.ReadTokenAt(ctx, h.config.ProfileDir, factorygithub.GrantPath(h.config.TokensPath))
	} else {
		token, err = h.setupToken(ctx)
	}
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", nil
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(token))), nil
}

// readRepos keeps the existing identity check in front of every stored read.
// A missing connection cannot inherit the previous account's repositories.
func (h *Handler) readRepos(ctx context.Context, refresh bool) ([]string, []factory.RepoInfo, time.Time, error) {
	if h.config.Store == nil || h.seam.GitHub == nil {
		watched, repos, err := h.seam.Repos(ctx)
		return watched, repos, time.Time{}, err
	}
	link, err := h.seam.GitHub(ctx)
	if err != nil {
		return h.reposAfterIdentityFailure(ctx, link.Login, err)
	}
	key, err := h.reposCredential(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if key == "" || !link.Connected() || link.Login == "" {
		// Not connected is an answer, not a failure: the repos that are on stay on
		// the page beside Connect GitHub, as before this read was stored.
		watched, err := h.config.Store.Repos()
		return watched, nil, time.Time{}, err
	}
	identity := strings.ToLower(link.Login)
	var finished *reposWalk
	for {
		h.reposRead.mu.Lock()
		saved, err := h.config.Store.AvailableRepos()
		if err != nil {
			h.reposRead.mu.Unlock()
			return nil, nil, time.Time{}, err
		}
		known := saved.Identity == identity && saved.Credential == key && !saved.ReadAt.IsZero()
		now := h.clock()
		sinceStart := now.Sub(h.reposRead.tried)
		if h.reposRead.walk == nil && !h.reposRead.closed && finished == nil {
			if refresh {
				// A CHANGED CONNECTION IS NOT A REPEAT OPEN. The cooldown protects GitHub
				// from one connection's picker opening again and again; a different
				// credential has no list of its own yet, so it reads at once, exactly as
				// an ordinary read does.
				if h.reposRead.key != key || sinceStart >= reposRefreshCooldown {
					h.startReposRead(identity, key)
				}
			} else {
				wait := reposReadEvery
				if h.reposRead.err != nil {
					wait = reposRefreshCooldown
				}
				if (h.reposRead.key != key || sinceStart >= wait) && (!known || now.Sub(saved.ReadAt) >= reposReadEvery) {
					h.startReposRead(identity, key)
				}
			}
		}
		walk := h.reposRead.walk
		readErr := h.reposRead.err
		if h.reposRead.key != key {
			readErr = nil
		}
		h.reposRead.mu.Unlock()
		if finished != nil {
			readErr = finished.err
		}
		if known && (!refresh || walk == nil || finished != nil) {
			watched, err := h.config.Store.Repos()
			if err == nil && reposSignInRefused(readErr) {
				err = readErr
			}
			return watched, saved.Repos, saved.ReadAt, err
		}
		if walk == nil || finished != nil {
			return nil, nil, time.Time{}, readErr
		}
		select {
		case <-ctx.Done():
			return nil, nil, time.Time{}, ctx.Err()
		case <-walk.done:
		}
		if refresh {
			// A failed walk can reuse a list only while its connection is still
			// the one this request checked before waiting.
			current, err := h.reposCredential(ctx)
			if err != nil {
				return nil, nil, time.Time{}, err
			}
			if current != key {
				return nil, nil, time.Time{}, errReposConnectionChanged
			}
		}
		if walk.key == key && walk.identity == identity {
			if !refresh && walk.err != nil {
				return nil, nil, time.Time{}, walk.err
			}
			finished = walk
		}
	}
}

// startReposRead is called with reposRead.mu held. The worker's token is
// checked again before publishing, because sign-in can change during a walk.
func (h *Handler) startReposRead(identity, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	walk := &reposWalk{done: make(chan struct{}), identity: identity, key: key}
	if h.reposRead.key != key {
		h.reposRead.err = nil
	}
	h.reposRead.walk, h.reposRead.cancel = walk, cancel
	h.reposRead.tried, h.reposRead.key = h.clock(), key
	guard.Go("factory/repositories", func() {
		var err error
		defer func() {
			cancel()
			if fault := recover(); fault != nil {
				err = guard.Note("factory/repositories", fault)
			}
			h.reposRead.mu.Lock()
			h.reposRead.err, h.reposRead.walk, h.reposRead.cancel = err, nil, nil
			walk.err = err
			close(walk.done)
			h.reposRead.mu.Unlock()
		}()
		var repos []factory.RepoInfo
		_, repos, err = h.seam.Repos(ctx)
		if err == nil {
			current, e := h.reposCredential(ctx)
			if e != nil {
				err = e
			} else if current != key {
				err = errReposConnectionChanged
			}
		}
		if err == nil {
			err = h.config.Store.SetAvailableRepos(store.AvailableRepos{Identity: identity, Credential: key, ReadAt: h.clock().UTC(), Repos: repos})
		}
	})
}

// reposAfterIdentityFailure trusts only the credential that established the saved
// identity. It cannot start a discovery until GitHub verifies that identity again.
func (h *Handler) reposAfterIdentityFailure(ctx context.Context, login string, failure error) ([]string, []factory.RepoInfo, time.Time, error) {
	if !reposIdentityRetryable(failure) {
		return nil, nil, time.Time{}, failure
	}
	key, err := h.reposCredential(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	h.reposRead.mu.Lock()
	readErr := h.reposRead.err
	sameCredential := h.reposRead.key == key
	h.reposRead.mu.Unlock()
	if sameCredential && reposSignInRefused(readErr) {
		return nil, nil, time.Time{}, readErr
	}
	saved, err := h.config.Store.AvailableRepos()
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if key == "" || saved.Credential != key || saved.Identity == "" || saved.ReadAt.IsZero() || (login != "" && !strings.EqualFold(login, saved.Identity)) {
		return nil, nil, time.Time{}, failure
	}
	watched, err := h.config.Store.Repos()
	return watched, saved.Repos, saved.ReadAt, err
}

func reposIdentityRetryable(err error) bool {
	if errors.Is(err, errGitHubExpired) || errors.Is(err, errSessionEnded) {
		return false
	}
	var upstream *forge.APIError
	if errors.As(err, &upstream) {
		return upstream.StatusCode >= 500 || upstream.StatusCode == http.StatusTooManyRequests
	}
	var status githubStatusError
	if errors.As(err, &status) {
		return status >= 500 || status == http.StatusTooManyRequests
	}
	return true
}

func reposSignInRefused(err error) bool {
	var status githubStatusError
	return factorygithub.TroubleWords(err) == "token refused" || errors.Is(err, errGitHubExpired) || (errors.As(err, &status) && status == http.StatusUnauthorized)
}
