package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// factoryPublisher resolves credentials on the leased job's machine. Watching
// repositories belongs to the server's poller, not to an assignee's checkout.
func factoryPublisher(profileDir string) func(context.Context, factory.Item) (factoryrun.PublishTarget, error) {
	return factoryPublisherWith(profileDir, func(token string) factorygithub.API { return forge.NewClient(token) })
}

// factoryPublisherWith lets the real client reach a fake forge in acceptance.
func factoryPublisherWith(profileDir string, client func(string) factorygithub.API) func(context.Context, factory.Item) (factoryrun.PublishTarget, error) {
	return func(ctx context.Context, it factory.Item) (factoryrun.PublishTarget, error) {
		token, _ := factorygithub.TokenAt(ctx, profileDir)
		if token == "" {
			return factoryrun.PublishTarget{}, errors.New("publishing permission requires the assignee's GitHub connection on this runner")
		}
		api := client(token)
		login, err := api.Me(ctx)
		if err != nil {
			return factoryrun.PublishTarget{}, err
		}
		if person := it.Person(); person != "" && factory.NormalizeHandle(login) != person {
			return factoryrun.PublishTarget{}, fmt.Errorf("publishing permission requires @%s's GitHub connection; this runner is connected as @%s", person, login)
		}
		repo := strings.TrimSpace(it.Repo)
		if !strings.Contains(repo, "/") && it.Product != "" {
			repo = it.Product + "/" + repo
		}
		owner, name, ok := strings.Cut(repo, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return factoryrun.PublishTarget{}, errors.New("publishing requires the job's repository as owner/name")
		}
		// ONE ATTEMPT, ONE TOKEN. Reconnecting while a push is underway must
		// not open its pull request as a different GitHub account.
		return factoryrun.PublishTarget{
			Source: factorygithub.New(api, []string{repo}, nil),
			Push:   factoryrun.GitPush(factoryrun.ExecGit{Token: func() string { return token }}),
		}, nil
	}
}
