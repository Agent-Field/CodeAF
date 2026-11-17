package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// factoryPublisher resolves credentials on the leased job's machine. Watching
// repositories belongs to the server's poller, not to an assignee's checkout.
func factoryPublisher(profileDir string) func(context.Context, factory.Item) (factoryrun.PublishTarget, error) {
	return factoryPublisherWith(profileDir, func(token string) factorygithub.API { return forge.NewClient(token) })
}

// publishNeedsGitHub is a publish refused for its GitHub connection, coded so
// the job and its task say what is needed instead of "ask for write access".
func publishNeedsGitHub(code, words string) error {
	return &publish.Error{Code: code, Detail: words, Err: errors.New(words)}
}

// notConnectedWords is the refusal of a publish from a computer with no
// GitHub connection: what is needed, and where to do it.
func notConnectedWords(person string) string {
	who := "your"
	if person != "" {
		who = "@" + person + "'s"
	}
	return "Connect GitHub to publish: the pull request is opened from " + who + " own GitHub account, and the computer that ran this job has no GitHub connected. Connect GitHub in Settings › Connectors on that computer, then Retry publishing."
}

// otherAccountWords is the refusal of a publish whose computer is connected
// to GitHub as someone other than the job's person. A person who signs in to
// the organization with a password has no GitHub account here, so the other
// way through is a teammate who signs in with GitHub taking the job.
func otherAccountWords(person, login string) string {
	return fmt.Sprintf("Connect GitHub to publish: the pull request is opened from @%s's own GitHub account, and the computer that ran this job is connected to GitHub as @%s. Connect GitHub as @%s in Settings › Connectors on that computer, or have a teammate who signs in to the organization with GitHub take the job, then Retry publishing.", person, login, person)
}

// factoryPublisherWith lets the real client reach a fake forge in acceptance.
func factoryPublisherWith(profileDir string, client func(string) factorygithub.API) func(context.Context, factory.Item) (factoryrun.PublishTarget, error) {
	return func(ctx context.Context, it factory.Item) (factoryrun.PublishTarget, error) {
		token, _ := factorygithub.TokenAt(ctx, profileDir)
		if token == "" {
			return factoryrun.PublishTarget{}, publishNeedsGitHub(publish.CodeGitHubNotConnected, notConnectedWords(it.Person()))
		}
		api := client(token)
		login, err := api.Me(ctx)
		if err != nil {
			return factoryrun.PublishTarget{}, err
		}
		if person := it.Person(); person != "" && factory.NormalizeHandle(login) != person {
			return factoryrun.PublishTarget{}, publishNeedsGitHub(publish.CodeGitHubOtherAccount, otherAccountWords(person, login))
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
		git := factoryrun.ExecGit{Token: func() string { return token }}
		return factoryrun.PublishTarget{Source: factorygithub.New(api, []string{repo}, nil), Push: factoryrun.GitPush(git), Git: git}, nil
	}
}
