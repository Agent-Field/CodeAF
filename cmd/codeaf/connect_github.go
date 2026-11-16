package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/connect"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/guard"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// ── GITHUB ON THE CONNECTORS PAGE IS THE FACTORY'S GITHUB ───────────────────
//
// v3GitHubDoors hangs internal/connect's GitHub row over the one connection
// the factory reads ([factorygithub.TokenAt]), on the profile the factory
// reads it from ([factoryProfile]). There is no second token anywhere: a
// token pasted on the Connectors page is the token the factory's first run
// would have kept, and a factory connected in its first run shows GitHub
// connected on the Connectors page.
func v3GitHubDoors(profileDir string) connect.GitHubDoors {
	profile := factoryProfile(profileDir)
	doors := connect.GitHubDoors{
		Token: func(ctx context.Context) (string, string) { return factorygithub.TokenAt(ctx, profile) },
		Keep: func(_ context.Context, token string) error {
			registerGitHubSecret(token)
			return factorygithub.KeepToken(profile, token)
		},
		KeepLocal: func(ctx context.Context) (string, error) {
			token, err := factorygithub.KeepGH(ctx, profile)
			registerGitHubSecret(token)
			return token, err
		},
		Forget:     func(context.Context) error { return factorygithub.Forget(profile) },
		LocalLogin: ghLocalLogin,
		Login:      githubLogin,
	}
	if factorygithub.DeviceClientID() != "" {
		doors.Device = &connect.GitHubDevice{Start: githubDeviceStart, Poll: githubDevicePoll}
	}
	return doors
}

// registerGitHubSecret keeps a token out of every log line and trace before
// anything else can say it.
func registerGitHubSecret(token string) {
	if token = strings.TrimSpace(token); token != "" {
		redact.Register(token)
		trace.Secret(token)
	}
}

// githubLogin asks GitHub whose token this is. A 401 or 403 is GitHub saying
// no, and is told apart from GitHub not answering.
func githubLogin(ctx context.Context, token string) (string, error) {
	login, err := forge.NewClient(token).Me(ctx)
	if err != nil {
		var apiErr *forge.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
			return "", connect.ErrGitHubRefused
		}
		// The forge's error can quote an address; it never quotes the token,
		// and it is not shown to a person either way.
		return "", errors.New("GitHub could not be reached")
	}
	return login, nil
}

func githubDeviceStart(ctx context.Context) (connect.GitHubCode, error) {
	code, err := factorygithub.DeviceClient{}.StartDevice(ctx)
	if err != nil {
		return connect.GitHubCode{}, &connect.SignInWords{Words: err.Error()}
	}
	redact.Register(code.Code)
	trace.Secret(code.Code)
	return connect.GitHubCode{
		Code: code.Code, UserCode: code.UserCode, Link: code.VerificationURI,
		Interval: time.Duration(code.Interval) * time.Second, ExpiresIn: time.Duration(code.ExpiresIn) * time.Second,
	}, nil
}

func githubDevicePoll(ctx context.Context, code string) (connect.GitHubGrant, error) {
	grant, err := factorygithub.DeviceClient{}.PollDevice(ctx, code)
	if err != nil {
		return connect.GitHubGrant{}, err
	}
	registerGitHubSecret(grant.Token)
	out := connect.GitHubGrant{Token: grant.Token, Interval: time.Duration(grant.Interval) * time.Second}
	switch grant.Error {
	case "":
	case factorygithub.DevicePending:
		out.Error = connect.GitHubPending
	case factorygithub.DeviceSlowDown:
		out.Error = connect.GitHubSlowDown
	case factorygithub.DeviceDenied:
		out.Error = connect.GitHubDenied
	default:
		out.Error = connect.GitHubExpired
	}
	return out, nil
}

// ── who gh is signed in as, without waiting on gh ───────────────────────────

// ghLoginAsk is how gh is asked who it is signed in as; tests replace it so no
// test asks the developer's own gh.
var ghLoginAsk = factorygithub.GHLogin

// ghLoginFresh is how long gh's answer about who it is signed in as stands.
const ghLoginFresh = time.Minute

// ghLoginCache is the one answer `gh auth status` last gave. A LIST NEVER
// WAITS ON GH FOR LONG: a list asks on every draw, gh asks GitHub, and a gh
// stuck on a keychain prompt once held every read. The answer stands for
// [ghLoginFresh] and is then asked again behind the reads, one ask at a time.
var ghLoginCache struct {
	mu    sync.Mutex
	login string
	at    time.Time
	ask   chan struct{}
}

// ghLocalLogin is the login gh is signed in as on this computer, or "". It
// reads `gh auth status`, which asks GitHub about gh's token itself, so a login
// named here is one GitHub accepts — and codeaf never takes gh's token to
// learn it: that waits for the person's yes ([factorygithub.KeepGH]).
func ghLocalLogin(ctx context.Context) string {
	ghLoginCache.mu.Lock()
	login, at, ask := ghLoginCache.login, ghLoginCache.at, ghLoginCache.ask
	if ask == nil && (at.IsZero() || time.Since(at) > ghLoginFresh) {
		ask = make(chan struct{})
		ghLoginCache.ask = ask
		guard.Go("connect/gh login", func() {
			found, _ := ghLoginAsk(context.Background())
			ghLoginCache.mu.Lock()
			ghLoginCache.login, ghLoginCache.at, ghLoginCache.ask = found, time.Now(), nil
			ghLoginCache.mu.Unlock()
			close(ask)
		})
	}
	ghLoginCache.mu.Unlock()
	if !at.IsZero() {
		return login
	}
	select {
	case <-ask:
	case <-ctx.Done():
	}
	ghLoginCache.mu.Lock()
	defer ghLoginCache.mu.Unlock()
	return ghLoginCache.login
}

// githubFactoryNote is what describe says when the factory on this machine
// reads GitHub through this same connection, so a disconnect can say what
// stops working.
const githubFactoryNote = "The Factory reads your repositories and opens pull requests with this same connection."
