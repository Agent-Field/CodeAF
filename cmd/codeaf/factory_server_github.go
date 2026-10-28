package main

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// factoryServerSeam keeps app grants on read doors; workers retain their own credentials.
func factoryServerSeam(st *store.Store, tokensPath string) factory.Seam {
	grantPath := factorygithub.GrantPath(tokensPath)
	readToken := func(ctx context.Context) string {
		token, _, _ := factorygithub.ReadTokenAt(ctx, home.Dir(), grantPath)
		return token
	}
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
		if via == "grant" {
			grant, err := factorygithub.LoadReadGrant(grantPath)
			return factory.GitHubLink{Login: grant.Login, Via: via}, err
		}
		login, err := forge.NewClient(token).Me(ctx)
		return factory.GitHubLink{Login: login, Via: via}, err
	}
	return nudgeOnWatch(seam, st)
}

// startFactoryServerPoll rebuilds the read source per tick so replacing a grant takes effect.
func startFactoryServerPoll(ctx context.Context, st *store.Store, tokensPath string) {
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
				if err != nil {
					failed++
				} else {
					failed = 0
				}
			}
			if !waker.Wait(ctx, factorygithub.Backoff(factoryPollEvery, failed)) {
				return
			}
		}
	})
}
