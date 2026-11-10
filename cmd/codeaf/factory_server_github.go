package main

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// factoryServerSeam keeps app grants on read doors; workers retain their own credentials.
func factoryServerSeam(st *store.Store, tokensPath string, limit *githubLimit) factory.Seam {
	grantPath := factorygithub.GrantPath(tokensPath)
	readToken := func(ctx context.Context) string {
		token, _, _ := factorygithub.ReadTokenAt(ctx, home.Dir(), grantPath)
		return token
	}
	registerFactoryTierResolver(st, home.Dir(), grantPath)
	seam := factory.LocalSeam(st, time.Now(), factoryRunnerDoors(st), factoryPipelineOption(st), factory.WithRepoDirs(st.CheckoutDir),
		factory.WithRepoLister(func(ctx context.Context) ([]factory.RepoInfo, error) {
			token, _, err := factorygithub.ReadTokenAt(ctx, home.Dir(), grantPath)
			if err != nil || token == "" {
				return nil, err
			}
			repos, err := forge.NewClient(token).Repos(ctx)
			return factorygithub.RepoInfos(repos), err
		}), factory.WithRefetch(factorygithub.Refetcher(st, func(ctx context.Context) *factorygithub.Source {
			return factoryGitHub(ctx, st, readToken)
		})))
	seam.GitHub = func(ctx context.Context) (factory.GitHubLink, error) {
		token, via, err := factorygithub.ReadTokenAt(ctx, home.Dir(), grantPath)
		if err != nil || token == "" {
			return factory.GitHubLink{}, err
		}
		login, err := forge.NewClient(token).Me(ctx)
		limit.saw(err)
		return factory.GitHubLink{Login: login, Via: via}, err
	}
	return nudgeOnWatch(seam, st)
}

// startFactoryServerPoll rebuilds the read source per tick so replacing a grant takes effect.
//
// AFTER RUNS ON EVERY TICK, with or without a GitHub source: it is the
// go-ahead pass ([intake.Intake.GoAheads]), which reads nothing from GitHub,
// and a team server is the factory whose go-aheads must hold with every app
// closed. Its error is never the poll's: a launch the factory refused is
// written on the job and tried again next tick, and counting it as a failed
// read would back GitHub reads off for up to ten minutes.
func startFactoryServerPoll(ctx context.Context, st *store.Store, tokensPath string, limit *githubLimit, after func(context.Context) error) {
	guard.Go("factory/server-read", func() {
		waker := factorygithub.NewWaker(st)
		cursor := ""
		failed := 0
		for ctx.Err() == nil {
			token := func(ctx context.Context) string {
				t, _, _ := factorygithub.ReadTokenAt(ctx, home.Dir(), factorygithub.GrantPath(tokensPath))
				return t
			}
			if src := factoryGitHub(ctx, st, token); src != nil {
				next, err := factorygithub.PollOnce(ctx, src, st, cursor, time.Now)
				cursor = next
				limit.saw(err)
				if err != nil {
					failed++
				} else {
					failed = 0
				}
			}
			if after != nil && ctx.Err() == nil {
				_ = after(ctx)
			}
			if !waker.Wait(ctx, factorygithub.Backoff(factoryPollEvery, failed)) {
				return
			}
		}
	})
}

// registerFactoryTierResolver runs before page reads, even without a usable poll.
// Resolve the read grant per attempt so replacing it takes effect on retry.
func registerFactoryTierResolver(st *store.Store, profileDir, grantPath string) {
	if st == nil {
		return
	}
	st.SetTierResolver(func(ctx context.Context, it factory.Item) (factory.Tier, time.Time, error) {
		token, _, err := factorygithub.ReadTokenAt(ctx, profileDir, grantPath)
		if err != nil || token == "" {
			return "", time.Time{}, errors.New("factory read credential is unavailable")
		}
		return factorygithub.New(forge.NewClient(token), nil, nil).ResolveTier(ctx, it)
	})
}
