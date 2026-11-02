package serve

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

const reposReadEvery = 5 * time.Minute

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
		return nil, nil, time.Time{}, err
	}
	key, err := h.reposCredential(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if key == "" || !link.Connected() || link.Login == "" {
		watched, repos, err := h.seam.Repos(ctx)
		return watched, repos, time.Time{}, err
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
		if h.reposRead.done == nil && !h.reposRead.closed && (h.reposRead.key != key || h.clock().Sub(h.reposRead.tried) >= reposReadEvery) && (!known || h.clock().Sub(saved.ReadAt) >= reposReadEvery) {
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
			if err == nil {
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
