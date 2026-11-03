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
	reposReadEvery = 5 * time.Minute
	reposReadRetry = 30 * time.Second
)

type reposReader struct {
	mu     sync.Mutex
	done   chan struct{}
	cancel context.CancelFunc
	closed bool
	tried  time.Time
	key    string
	err    error
}

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
func (h *Handler) readRepos(ctx context.Context) ([]string, []factory.RepoInfo, time.Time, error) {
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
		return nil, nil, time.Time{}, errGitHubExpired
	}
	identity := strings.ToLower(link.Login)
	for {
		h.reposRead.mu.Lock()
		saved, err := h.config.Store.AvailableRepos()
		if err != nil {
			h.reposRead.mu.Unlock()
			return nil, nil, time.Time{}, err
		}
		known := saved.Identity == identity && saved.Credential == key && !saved.ReadAt.IsZero()
		wait := reposReadEvery
		if h.reposRead.err != nil {
			wait = reposReadRetry
		}
		if h.reposRead.done == nil && !h.reposRead.closed && (h.reposRead.key != key || h.clock().Sub(h.reposRead.tried) >= wait) && (!known || h.clock().Sub(saved.ReadAt) >= reposReadEvery) {
			h.startReposRead(identity, key)
		}
		done := h.reposRead.done
		readErr := h.reposRead.err
		if h.reposRead.key != key {
			readErr = nil
		}
		h.reposRead.mu.Unlock()
		if known {
			watched, err := h.config.Store.Repos()
			if err == nil && reposSignInRefused(readErr) {
				err = readErr
			}
			return watched, saved.Repos, saved.ReadAt, err
		}
		if done == nil {
			return nil, nil, time.Time{}, readErr
		}
		select {
		case <-ctx.Done():
			return nil, nil, time.Time{}, ctx.Err()
		case <-done:
		}
		h.reposRead.mu.Lock()
		err = h.reposRead.err
		h.reposRead.mu.Unlock()
		if err != nil {
			return nil, nil, time.Time{}, err
		}
	}
}

// startReposRead is called with reposRead.mu held. The worker's token is
// checked again before publishing, because sign-in can change during a walk.
func (h *Handler) startReposRead(identity, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	done := make(chan struct{})
	h.reposRead.done, h.reposRead.cancel = done, cancel
	h.reposRead.tried, h.reposRead.key = h.clock(), key
	guard.Go("factory/repositories", func() {
		var err error
		defer func() {
			cancel()
			if fault := recover(); fault != nil {
				err = guard.Note("factory/repositories", fault)
			}
			h.reposRead.mu.Lock()
			h.reposRead.err, h.reposRead.done, h.reposRead.cancel = err, nil, nil
			close(done)
			h.reposRead.mu.Unlock()
		}()
		var repos []factory.RepoInfo
		_, repos, err = h.seam.Repos(ctx)
		if err == nil {
			current, e := h.reposCredential(ctx)
			if e != nil {
				err = e
			} else if current != key {
				err = fmt.Errorf("GitHub connection changed during repository discovery")
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
