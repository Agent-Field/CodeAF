package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryRepoIssuesReader = (*appBackend)(nil)

// FactoryRepoIssues reads one fixed route through the caller's connection.
func (b *appBackend) FactoryRepoIssues(ctx context.Context, p appwire.FactoryRepoIssuesParams) (appwire.FactoryRepoIssues, error) {
	p, err := appwire.CheckRepoIssues(p)
	if err != nil {
		return appwire.FactoryRepoIssues{}, err
	}
	owner, name, _ := strings.Cut(p.Repo, "/")
	query := url.Values{"page": {strconv.Itoa(*p.Page)}}
	if p.Since != "" {
		query.Set("since", p.Since)
	}
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/issues?" + query.Encode()
	got, err := b.factoryHub().readRepoIssues(ctx, p.Factory, path)
	if err != nil {
		return appwire.FactoryRepoIssues{}, err
	}
	if got.Issues == nil {
		got.Issues = []api.RepoIssue{}
	}
	return appwire.FactoryRepoIssues{Factory: p.Factory, RepoIssues: got}, nil
}

// This read keeps the response body until refusals have been decoded, because
// the ordinary read mapping has no rate-limit retry detail.
func (h *appFactoryHub) readRepoIssues(ctx context.Context, id, path string) (api.RepoIssues, error) {
	var out api.RepoIssues
	row, token, err := h.credentialed(ctx, id)
	if err != nil {
		return out, err
	}
	r, err := h.send(ctx, row.URL, token, http.MethodGet, path, nil, appFactoryBody, nil)
	if err != nil {
		return out, err
	}
	if err := h.staleCredential(id, token); err != nil {
		return out, err
	}
	if r.status != http.StatusOK {
		err := repoIssuesFailure(r)
		var wire *appwire.Error
		if errors.As(err, &wire) && wire.Code == appwire.CodeSignedOut {
			h.refused(id, token)
		}
		return out, err
	}
	if json.Unmarshal(r.body, &out) != nil {
		return out, appwire.Errorf(appwire.CodeEngine, "the factory sent an answer this app cannot read")
	}
	h.touch(id)
	return out, nil
}

func repoIssuesFailure(r appFactoryReply) error {
	err := appFactoryFailure(r)
	if routeMissing(err) {
		var wire *appwire.Error
		if errors.As(factoryTooOld(), &wire) {
			wire.Detail = &appwire.FactoryRefusal{Reason: appwire.FactoryTooOld}
			return wire
		}
	}
	if r.status == http.StatusTooManyRequests {
		var retry api.RepoIssuesRetry
		var wire *appwire.Error
		if json.Unmarshal(r.body, &retry) == nil && !retry.RetryAt.IsZero() && errors.As(err, &wire) {
			wire.Detail = &retry
		}
	}
	return err
}
