package appwire

import (
	"context"
	"encoding/json"
)

// ── factory.setup_github_*: CONNECT GITHUB IN THE BROWSER ───────────────────
//
// The first run's main way to connect GitHub. A person never types a command
// or handles a token: the app shows a one-time code, opens GitHub's device
// page, and the person approves there.
//
//	factory.setup_github_start  {caller}        → FactorySignIn
//	factory.setup_github_poll   {flow, caller}  → FactorySetupGitHubState
//	factory.setup_github_cancel {flow, caller}  → Empty
//
// The shapes are factory.sign_in_*'s, so one client drives both. GitHub's
// device code stays in the engine; the app holds only the opaque flow, bound
// to the caller that started it. A poll is answered at GitHub's pace: one
// that comes too soon is answered pending without asking GitHub.
//
// ONCE THE PERSON APPROVES, THE TOKEN IS CONNECTED EXACTLY AS A PASTED ONE
// IS (factory.setup_connect, source token) before the poll answers. The one
// signed_in answer then carries that token back ONCE, as github_token, for
// the app to keep in its Keychain beside the token a person pastes: in app
// mode the app holds every key and the engine holds it in memory only. It is
// the only answer of this wire that carries a secret, and no other answer,
// event or error does.
//
// ABSENT, NOT BROKEN: an engine built without a GitHub OAuth client ID does
// not announce [FeatureFactorySetupGitHub] and answers these methods as
// unknown_method, and the first run offers the gh login and a pasted token.

// FeatureFactorySetupGitHub is announced in hello when this engine can
// connect GitHub in the browser.
const FeatureFactorySetupGitHub = "factory.setup_github"

// FactorySetupGitHubState is one poll of a browser sign-in. On signed_in,
// GitHubToken is the token now connected (for the app's Keychain) and View is
// the setup after the connect. Message is set only on denied, when there are
// words for why.
type FactorySetupGitHubState struct {
	State       string            `json:"state" enum:"pending,signed_in,expired,denied"`
	Interval    int               `json:"interval,omitempty"`
	Message     string            `json:"message,omitempty"`
	GitHubToken string            `json:"github_token,omitempty"`
	View        *FactorySetupView `json:"view,omitempty"`
}

// FactorySetupBrowser is the engine's half of factory.setup_github_*.
type FactorySetupBrowser interface {
	// FactorySetupGitHubAvailable is false when this build has no OAuth
	// client ID, which leaves the methods off the wire.
	FactorySetupGitHubAvailable() bool
	FactorySetupGitHubStart(ctx context.Context, caller string) (FactorySignIn, error)
	FactorySetupGitHubPoll(ctx context.Context, caller, flow string) (FactorySetupGitHubState, error)
	FactorySetupGitHubCancel(ctx context.Context, caller, flow string) error
}

func withFactorySetupGitHubFeature(hello Hello, e Engine) Hello {
	if _, err := setupGitHubOf(e, ""); err == nil {
		hello = withFeature(hello, FeatureFactorySetupGitHub)
	}
	return hello
}

func setupGitHubOf(e Engine, name string) (FactorySetupBrowser, error) {
	if _, err := factoryLocalOf(e, name); err != nil {
		return nil, err
	}
	g, ok := e.(FactorySetupBrowser)
	if !ok || !g.FactorySetupGitHubAvailable() {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return g, nil
}

func init() {
	methods["factory.setup_github_start"] = shaped[FactoryLocalParams, FactorySignIn](func(e Engine, raw json.RawMessage) (*FactorySignIn, error) {
		const name = "factory.setup_github_start"
		g, err := setupGitHubOf(e, name)
		if err != nil {
			return nil, err
		}
		var p FactoryLocalParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := g.FactorySetupGitHubStart(ctx, caller)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	methods["factory.setup_github_poll"] = typed(func(e Engine, p FactorySignInFlowParams) (*FactorySetupGitHubState, error) {
		g, err := setupGitHubOf(e, "factory.setup_github_poll")
		if err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		flow, err := required("flow", p.Flow)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := g.FactorySetupGitHubPoll(ctx, caller, flow)
		if err != nil {
			return nil, err
		}
		if out.View != nil && out.View.Repos == nil {
			out.View.Repos = []FactorySetupRepo{}
		}
		return &out, nil
	})
	methods["factory.setup_github_cancel"] = typed(func(e Engine, p FactorySignInFlowParams) (*Empty, error) {
		g, err := setupGitHubOf(e, "factory.setup_github_cancel")
		if err != nil {
			return nil, err
		}
		caller, err := factoryCaller(p.Caller)
		if err != nil {
			return nil, err
		}
		flow, err := required("flow", p.Flow)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		return done(g.FactorySetupGitHubCancel(ctx, caller, flow))
	})
}
