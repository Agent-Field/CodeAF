package github

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// ── HOW THIS MACHINE REACHES GITHUB ─────────────────────────────────────────
//
// The floor's connect prompt and the settings place's `github` row ask the
// doors below, which the launch hangs on the seam with [Connect]. A token is
// found in one order, said once here: GH_TOKEN, then GITHUB_TOKEN, then a
// token kept in the profile, then gh's own login, BUT GH ONLY AFTER THE PERSON
// SAID YES TO IT on the floor (config.KeyGitHubVia). The bytes of a token go
// into a client and nowhere else.

// ghTimeout is how long `gh auth status` and `gh auth token` may take before
// the floor stops waiting and offers the token row instead.
const ghTimeout = 3 * time.Second

// TokenAt is the token the factory reads GitHub with on this profile and the
// way it was found, or "" and "" for none.
func TokenAt(ctx context.Context, profileDir string) (token, via string) {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := strings.TrimSpace(env.Value(name)); t != "" {
			return t, factory.ViaEnv
		}
	}
	if t := config.GitHubTokenAt(profileDir); t != "" {
		return t, factory.ViaToken
	}
	if config.GitHubViaAt(profileDir) == factory.ViaGH {
		if t := ghToken(ctx); t != "" {
			return t, factory.ViaGH
		}
	}
	return "", ""
}

// ghToken is what `gh auth token` answers, or "".
func ghToken(ctx context.Context) string {
	gh, err := forge.LookPath("gh")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, gh, "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// reGHAccount reads the login out of `gh auth status`, which has said it two
// ways: `Logged in to github.com account NAME (keyring)` and, before that,
// `Logged in to github.com as NAME (…)`.
var reGHAccount = regexp.MustCompile(`Logged in to github\.com (?:account|as) ([A-Za-z0-9-]+)`)

// GHLogin is the login `gh auth status` answers, and "" when gh is not
// installed, not logged in, or slower than [ghTimeout]. It never asks for a
// token: knowing who gh is logged in as is what the prompt needs to ask.
func GHLogin(ctx context.Context) (string, error) {
	gh, err := forge.LookPath("gh")
	if err != nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	// gh writes its status to stderr on some versions and stdout on others.
	out, _ := exec.CommandContext(ctx, gh, "auth", "status", "--hostname", "github.com").CombinedOutput()
	if m := reGHAccount.FindSubmatch(out); m != nil {
		return string(m[1]), nil
	}
	return "", nil
}

// Link is who the profile's token belongs to and the way it was found; a zero
// link when no token resolves. A token GitHub refuses is an error, so the
// row never names a login it could not confirm.
func Link(ctx context.Context, profileDir string) (factory.GitHubLink, error) {
	token, via := TokenAt(ctx, profileDir)
	if token == "" {
		return factory.GitHubLink{}, nil
	}
	login, err := forge.NewClient(token).Me(ctx)
	if err != nil {
		return factory.GitHubLink{Via: via}, err
	}
	return factory.GitHubLink{Login: login, Via: via}, nil
}

// Keep keeps a way to reach GitHub on the profile: "" is the person's yes to
// gh's own login, which is refused when gh answers no token; anything else is
// a token, kept once GitHub has said whose it is.
func Keep(ctx context.Context, profileDir, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		if ghToken(ctx) == "" {
			return errors.New("gh answered no token · gh auth login, then try again")
		}
		return config.WriteGitHubViaGH(profileDir)
	}
	if strings.ContainsAny(token, " \t\n") {
		return errors.New("a token is one word")
	}
	if _, err := forge.NewClient(token).Me(ctx); err != nil {
		return errors.New("github did not take that token")
	}
	return config.WriteGitHubToken(profileDir, token)
}

// Lister is the picker's list over the profile's token: every repository it
// can see, most recently pushed first, or nothing when no token resolves.
func Lister(profileDir string) factory.RepoLister {
	return func(ctx context.Context) ([]factory.RepoInfo, error) {
		token, _ := TokenAt(ctx, profileDir)
		if token == "" {
			return nil, nil
		}
		repos, err := forge.NewClient(token).Repos(ctx)
		if err != nil {
			return nil, err
		}
		return RepoInfos(repos), nil
	}
}

// RepoInfos converts the forge's repositories to the floor's.
func RepoInfos(in []RepoInfo) []factory.RepoInfo {
	out := make([]factory.RepoInfo, 0, len(in))
	for _, r := range in {
		out = append(out, factory.RepoInfo{Full: r.Full, Name: r.Name, Owner: r.Owner, Private: r.Private, Pushed: r.Pushed, Open: r.Open})
	}
	return out
}

// Connect hangs the three connection doors on seam over profileDir: who the
// token belongs to, who gh is logged in as, and keeping a way in. An empty
// profile leaves seam as it was, because there is nowhere to keep an answer.
func Connect(seam factory.Seam, profileDir string) factory.Seam {
	if strings.TrimSpace(profileDir) == "" {
		return seam
	}
	seam.GitHub = func(ctx context.Context) (factory.GitHubLink, error) { return Link(ctx, profileDir) }
	seam.GHLogin = GHLogin
	seam.ConnectGitHub = func(ctx context.Context, token string) error { return Keep(ctx, profileDir, token) }
	return seam
}
