package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// githubLimitFallback is how long a rate limit that names no reset is
// believed to last.
const githubLimitFallback = time.Minute

// githubLimit is what a team factory knows of GitHub's rate limit. Every
// GitHub read the server makes reports to it; it logs one line when the
// limit is first met and one when a read succeeds again, and about reads it.
type githubLimit struct {
	mu    sync.Mutex
	until time.Time
	now   func() time.Time
	log   io.Writer
}

func newGitHubLimit(log io.Writer) *githubLimit {
	return &githubLimit{now: time.Now, log: log}
}

// saw reports the end of one GitHub read: a spent limit is kept until its
// reset, and any other answer means GitHub is reachable again.
func (l *githubLimit) saw(err error) {
	if l == nil {
		return
	}
	until, limited := l.resetOf(err)
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case limited && l.until.IsZero():
		l.until = until
		l.line(serve.GitHubLimitedWords(until))
	case limited:
		l.until = until
	case err == nil && !l.until.IsZero():
		l.until = time.Time{}
		l.line("GitHub's rate limit has cleared; the factory reads GitHub again")
	}
}

func (l *githubLimit) resetOf(err error) (time.Time, bool) {
	var apiErr *forge.APIError
	if !errors.As(err, &apiErr) {
		return time.Time{}, false
	}
	spent := apiErr.StatusCode == http.StatusTooManyRequests ||
		(apiErr.StatusCode == http.StatusForbidden && (apiErr.RetryAfter > 0 || strings.Contains(strings.ToLower(apiErr.Body), "rate limit")))
	if !spent {
		return time.Time{}, false
	}
	wait := apiErr.RetryAfter
	if wait <= 0 {
		wait = githubLimitFallback
	}
	return l.now().Add(wait), true
}

func (l *githubLimit) line(s string) {
	if l.log != nil {
		_, _ = fmt.Fprintln(l.log, s)
	}
}

// resets is when the spent limit resets, zero when it is not spent.
func (l *githubLimit) resets() time.Time {
	if l == nil {
		return time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.until
}

// factoryOperatorLink mints serve's operator link off the start path: the
// listener already answers, and a spent GitHub limit waits for its reset
// rather than ending serve.
func factoryOperatorLink(ctx context.Context, h *serve.Handler, recovery bool, limit *githubLimit) (api.AccessLink, error) {
	for {
		link, err := h.NewOperatorLink(ctx, recovery)
		until := limit.resets()
		if err == nil || until.IsZero() {
			return link, err
		}
		t := time.NewTimer(max(time.Second, time.Until(until)))
		select {
		case <-ctx.Done():
			t.Stop()
			return api.AccessLink{}, ctx.Err()
		case <-t.C:
		}
	}
}
