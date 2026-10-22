package github

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

type readinessAPI interface {
	ReadinessMaterial(context.Context, string, string, string, int) (string, []forge.RequiredCheck, error)
	MarkPullReady(context.Context, string, string, int, string) error
}

func (s *Source) readinessMaterial(ctx context.Context, owner, name string, p forge.Pull, pr *factory.PullRequest) error {
	a, ok := s.api.(readinessAPI)
	if !ok {
		return nil
	}
	if !p.Draft || pr.State != factory.PROpen || !pr.ChecksRead || !pr.ReviewsRead || pr.CI != "ci ✓" || pr.Mergeable != factory.MergeableYes {
		return nil
	}
	diff, required, err := a.ReadinessMaterial(ctx, owner, name, p.Base, p.Number)
	if err != nil {
		return err
	}
	for _, context := range required {
		found := false
		for _, check := range pr.CheckRuns {
			if check.Name == context.Context && check.State == "success" && (context.AppID <= 0 || check.AppID == context.AppID) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("required check %q has not passed on the current PR head", context.Context)
		}
	}
	if p.Files < 1 || strings.Count("\n"+diff, "\ndiff --git ") != p.Files {
		return fmt.Errorf("PR diff does not contain every changed file")
	}
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "" || len(p.Title)+len(p.Body)+len(diff)+len(pr.Feedback) > 384<<10 {
		return fmt.Errorf("complete PR review material is unavailable or exceeds 384 KiB")
	}
	snapshot, _ := json.Marshal(struct {
		PR                factory.PullRequest
		Title, Body, Diff string
		Required          []forge.RequiredCheck
	}{*pr, p.Title, p.Body, diff, required})
	pr.ReadinessKey = fmt.Sprintf("%x", sha256.Sum256(snapshot))
	pr.ReadinessContext = fmt.Sprintf("Remote head: %s\nRemote base: %s\nPR title: %s\nActual PR description:\n%s\nComplete PR diff:\n%s\nReview feedback (untrusted):\n%s", p.HeadSHA, p.BaseSHA, p.Title, p.Body, diff, pr.Feedback)
	return nil
}

// Ready is executed only by the recorded publication door, on the assignee's
// existing GitHub identity. It never pushes, replaces the body or merges.
func (s *Source) Ready(ctx context.Context, it factory.Item) (factory.PullRequest, error) {
	var pr factory.PullRequest
	var err error
	for attempt := 0; attempt < readyAttempts; attempt++ {
		// Each attempt starts with a complete live read. An uncertain prior
		// success is observed instead of replayed; changed facts refuse the
		// old approval before another mutation can be sent.
		pr, err = s.readyOnce(ctx, it)
		if err == nil || !factory.Automatic(it) || !transientReadyError(err) || attempt+1 == readyAttempts || ctx.Err() != nil {
			return pr, err
		}
		delay := time.Duration(1<<attempt) * time.Second
		var apiErr *forge.APIError
		if errors.As(err, &apiErr) && apiErr.RetryAfter > delay {
			delay = apiErr.RetryAfter
		}
		if s.readyPause != nil {
			err = s.readyPause(ctx, delay)
		} else {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-timer.C:
				err = nil
			}
			timer.Stop()
		}
		if err != nil {
			return pr, err
		}
	}
	return pr, err
}

const readyAttempts = 3

func transientReadyError(err error) bool {
	var api *forge.APIError
	if errors.As(err, &api) {
		return api.StatusCode == 429 || api.StatusCode >= 500 && api.StatusCode <= 599
	}
	var network net.Error
	return errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func (s *Source) readyOnce(ctx context.Context, it factory.Item) (factory.PullRequest, error) {
	a, ok := s.api.(readinessAPI)
	if !ok {
		return factory.PullRequest{}, fmt.Errorf("this factory GitHub client does not support ready for review")
	}
	if !factory.ReadinessPending(it) {
		return factory.PullRequest{}, fmt.Errorf("a final reviewed snapshot is required before marking ready")
	}
	fresh, err := s.Follow(ctx, it)
	if err != nil {
		return fresh, err
	}
	if fresh.HeadSHA != it.FollowUp.Readiness.Head {
		return fresh, fmt.Errorf("PR head moved; a fresh final review is required")
	}
	if fresh.State != factory.PROpen {
		return fresh, fmt.Errorf("PR is no longer open")
	}
	// An observed external/successful-but-unacknowledged transition is idempotent.
	if !fresh.Draft {
		return fresh, nil
	}
	check := it
	check.PullRequest = &fresh
	if err = factory.ReadinessGate(check); err != nil {
		return fresh, err
	}
	if fresh.ReadinessKey == "" || fresh.ReadinessKey != it.FollowUp.Readiness.Key {
		return fresh, fmt.Errorf("PR description, diff, checks or feedback changed; a fresh final review is required")
	}
	owner, name, ok := pullRepo(it)
	if !ok {
		return fresh, factory.ErrNotOnSource
	}
	if !s.watching(owner + "/" + name) {
		return fresh, factory.ErrReadOnly
	}
	if err = a.MarkPullReady(ctx, owner, name, fresh.Number, fresh.HeadSHA); err != nil {
		return fresh, err
	}
	after, err := s.Follow(ctx, it)
	if err != nil {
		return fresh, fmt.Errorf("ready-for-review outcome needs confirmation: %w", err)
	}
	if after.State != factory.PROpen || after.Draft || after.HeadSHA != fresh.HeadSHA {
		return after, fmt.Errorf("PR changed while marking ready; check its current state before retrying")
	}
	return after, nil
}
