package main

// ── codeaf app: BOUNDED READS OF A REPOSITORY ON AN ATTACHED FACTORY ───────
//
// The engine's half of appwire.FactoryRepoReader: one fixed read route,
// GET /v1/repos/{owner}/{repo}/read, with the look's own fields as its query.
// The factory reads its checkout, applies the deny list and the bounds, and
// marks the answer untrusted; this side only carries it, with the factory
// named by the app's id for it. Its refusals arrive typed through
// appFactoryFailure (not_cloned, denied, binary).

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryRepoReader = (*appBackend)(nil)

func (b *appBackend) FactoryRepoRead(ctx context.Context, p appwire.FactoryRepoReadParams) (appwire.FactoryRepoRead, error) {
	q := url.Values{"op": {p.Op}}
	for key, v := range map[string]string{"path": p.Path, "pattern": p.Pattern} {
		if v != "" {
			q.Set(key, v)
		}
	}
	for key, n := range map[string]int{"from": p.From, "to": p.To, "job": p.Job} {
		if n > 0 {
			q.Set(key, strconv.Itoa(n))
		}
	}
	owner, name, _ := strings.Cut(p.Repo, "/")
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/read?" + q.Encode()
	var got api.RepoRead
	if err := b.factoryHub().read(ctx, p.Factory, path, appFactoryBody, &got); err != nil {
		if routeMissing(err) {
			return appwire.FactoryRepoRead{}, factoryTooOld()
		}
		return appwire.FactoryRepoRead{}, err
	}
	return appwire.FactoryRepoRead{
		Factory: p.Factory, Repo: got.Repo, Op: got.Op, At: got.At, Job: got.Job, Ref: got.Ref, Commit: got.Commit, Path: got.Path,
		Untrusted: true, Truncated: got.Truncated, Entries: got.Entries, Pattern: got.Pattern, Hits: got.Hits,
		From: got.From, To: got.To, Size: got.Size, EOF: got.EOF, Text: got.Text,
	}, nil
}
