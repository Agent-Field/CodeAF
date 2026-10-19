package github

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// How an item stands on GitHub, as [Standing] answers it. Open is the empty
// word: nothing about it stops a start. The other two are the job's DoneHow
// when a start finds them, the words the factory already uses for a pull
// request that ended.
const (
	StandingClosed = "closed"
	StandingMerged = "merged"
)

// Standing reads ONE GitHub item again, in ONE request, right before a
// person's start, and answers whether it is still open: "" while it is,
// [StandingClosed] for a closed issue or a pull request closed without
// merging, and [StandingMerged] for a merged pull request. It reads nothing
// else (no comments, files or checks): the poll keeps those, and a start must
// not wait on four reads to learn one fact.
//
// THE LISTS THE POLL READS ARE OF OPEN ITEMS ONLY, so an item closed after it
// arrived is never seen leaving them. This read is how a job learns it, item
// by item, at the one moment it matters: when it is chosen to run.
func Standing(ctx context.Context, api API, it factory.Item) (string, error) {
	if api == nil || it.Origin != factory.OriginForge || it.Num <= 0 {
		return "", factory.ErrNotOnSource
	}
	owner, name, ok := pullRepo(it)
	if !ok {
		return "", factory.ErrNotOnSource
	}
	if it.Kind == factory.KindPR {
		p, err := api.Pull(ctx, owner, name, it.Num)
		if err != nil {
			return "", err
		}
		switch {
		case p.Merged:
			return StandingMerged, nil
		case strings.EqualFold(p.State, "closed"):
			return StandingClosed, nil
		}
		return "", nil
	}
	is, err := api.Issue(ctx, owner, name, it.Num)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(is.State, "closed") {
		return StandingClosed, nil
	}
	return "", nil
}

// Rechecker is [Standing] over the token at the moment of asking, for the
// factory's start ([serve.Config.Recheck]). It is built fresh on every ask,
// as [Refetcher] is, because a token connected a minute ago is the one to
// use. With no token there is nothing to read with, which the caller treats
// as GitHub not reachable.
func Rechecker(token func(context.Context) string) func(context.Context, factory.Item) (string, error) {
	return func(ctx context.Context, it factory.Item) (string, error) {
		if token == nil {
			return "", errNoToken
		}
		tok := token(ctx)
		if tok == "" {
			return "", errNoToken
		}
		return Standing(ctx, forge.NewClient(tok), it)
	}
}

var errNoToken = errors.New("github is not connected")
