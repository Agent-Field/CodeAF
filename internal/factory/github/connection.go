package github

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
)

// ── THE MACHINE'S ONE GITHUB CONNECTION, FOR THE CONNECTORS PAGE ────────────
//
// The Connectors page (internal/connect's github.go) connects GitHub for
// agents, and it is the SAME GitHub the factory reads: these three doors keep
// and forget exactly what [TokenAt] reads, the way the factory's own first run
// keeps it (internal/factory/serve's setup connect). Connecting in either
// place is connected in both, and disconnecting in either is disconnected in
// both.
//
// None of them asks GitHub anything: the caller has already asked GitHub
// whose token it is, and a token GitHub refused never reaches them.

// KeepToken keeps a token GitHub accepted as this profile's connection. In the
// desktop app the token is handed to the app to keep in its keychain and the
// yes to gh is taken back, so the token is the one answer; in a terminal it is
// written to the profile, which forgets the yes in the same write.
func KeepToken(profileDir, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("there is no token to keep")
	}
	if credentials := config.AppCredentialSource(); credentials != nil {
		if err := config.ForgetGitHubViaGH(profileDir); err != nil {
			return errors.New("codeaf could not keep this GitHub connection")
		}
		credentials.Keep("github", token)
		return nil
	}
	return config.WriteGitHubToken(profileDir, token)
}

// KeepGH keeps the person's yes to gh's own login and answers the token gh
// holds, so the caller can ask GitHub whose it is. THIS IS THE FIRST TIME GH'S
// TOKEN IS READ: the yes is what allows it. In the desktop app gh's token is
// held for this launch only, since gh keeps it; the yes outlives the launch.
func KeepGH(ctx context.Context, profileDir string) (string, error) {
	forgetGHToken()
	token := ghTokenNow(ctx)
	if token == "" {
		return "", errors.New("gh answered no token · gh auth login, then try again")
	}
	if err := config.WriteGitHubViaGH(profileDir); err != nil {
		return "", errors.New("codeaf could not keep this GitHub connection")
	}
	if credentials := config.AppCredentialSource(); credentials != nil {
		credentials.Set("github", token)
	}
	return token, nil
}

// Forget disconnects this profile's GitHub. In the desktop app the app is told
// to forget its kept token, and the removal stands for the rest of the launch
// even over a shell token (config's [config.AppCredentials]); in a terminal
// the profile's token and yes are removed.
//
// A TOKEN FROM THE SHELL IS NOT OURS TO FORGET. GH_TOKEN and GITHUB_TOKEN are
// read before the profile, so a terminal with one set stays connected after
// the profile is cleared, and that is said rather than pretended away.
func Forget(profileDir string) error {
	if credentials := config.AppCredentialSource(); credentials != nil {
		if err := config.ForgetGitHubViaGH(profileDir); err != nil {
			return errors.New("codeaf could not disconnect GitHub")
		}
		credentials.Forget("github")
		forgetGHToken()
		return nil
	}
	if err := config.ForgetGitHub(profileDir); err != nil {
		return errors.New("codeaf could not disconnect GitHub")
	}
	forgetGHToken()
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if strings.TrimSpace(env.Value(name)) != "" {
			return errors.New("GitHub is still read from $" + name + " on this computer · unset it to disconnect")
		}
	}
	return nil
}
