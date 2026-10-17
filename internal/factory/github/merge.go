package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// merger is the write a client needs beyond [API] to merge. A client without
// it cannot merge, and says so rather than pretending.
type merger interface {
	MergePullRequest(ctx context.Context, owner, repo string, number int, sha, method string) (forge.Merged, error)
	MergeMethods(ctx context.Context, owner, repo string) ([]string, error)
}

// mergePull merges the job's pull request at exactly the commit the action
// names. The gate ([factory.MergeGate]) has already passed on fresh facts;
// this is the forge's own last word, and GitHub refuses a head that moved.
func (s *Source) mergePull(ctx context.Context, owner, name string, a factory.Action) (forge.Posted, error) {
	m, ok := s.api.(merger)
	if !ok {
		return forge.Posted{}, fmt.Errorf("this github client cannot merge: %w", factory.ErrReadOnly)
	}
	pr := a.Item.PullRequest
	if pr == nil || pr.Number <= 0 {
		return forge.Posted{}, errors.New("a merge needs the job's pull request")
	}
	if strings.TrimSpace(a.Commit) == "" {
		return forge.Posted{}, errors.New("a merge needs the exact commit to merge")
	}
	method := strings.TrimSpace(a.Method)
	if method == "" {
		allowed, err := m.MergeMethods(ctx, owner, name)
		if err != nil {
			return forge.Posted{}, err
		}
		if len(allowed) == 0 {
			return forge.Posted{}, fmt.Errorf("%s/%s allows no merge method", owner, name)
		}
		method = allowed[0]
	}
	merged, err := m.MergePullRequest(ctx, owner, name, pr.Number, a.Commit, method)
	if err != nil {
		return forge.Posted{}, err
	}
	return forge.Posted{URL: pr.URL, ID: merged.SHA, Number: pr.Number}, nil
}
