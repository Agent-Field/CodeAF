package main

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// ── codeaf app: CONNECT THE FIRST RUN'S GITHUB IN THE BROWSER ───────────────
//
// The engine's half of appwire.FactorySetupGitHub. It runs GitHub's device
// flow for the factory on this machine and, once the person approves, hands
// the token to the SAME connect a pasted token takes ([appBackend.FactorySetupAct]
// with source token). There is no second credential store: the app keeps the
// token in its Keychain exactly as it keeps a pasted one, from the one poll
// answer that carries it.
//
// ONE SIGN-IN PER WINDOW. A second start from the same caller replaces the
// first, and the registry is bounded, so abandoned sign-ins cost nothing past
// their expiry.

// setupGitHubFlows bounds the browser sign-ins waiting at once.
const setupGitHubFlows = 16

type appSetupGitHub struct {
	mu    sync.Mutex
	flows map[string]*appSetupGitHubFlow
	// device replaces GitHub's endpoints and now the clock, in tests; both
	// are set before the first call.
	device *factorygithub.DeviceClient
	now    func() time.Time
}

type appSetupGitHubFlow struct {
	caller   string
	code     string
	interval int
	expires  time.Time
	next     time.Time
	busy     bool
}

func (b *appBackend) setupGitHubNow() time.Time {
	if b.setupGitHub.now != nil {
		return b.setupGitHub.now()
	}
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func (b *appBackend) setupGitHubClient() factorygithub.DeviceClient {
	g := &b.setupGitHub
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.device != nil {
		return *g.device
	}
	return factorygithub.DeviceClient{}
}

// FactorySetupGitHubAvailable is true when this build names a GitHub OAuth
// app, or a test handed in a client.
func (b *appBackend) FactorySetupGitHubAvailable() bool {
	return b.setupGitHubClient().ClientID != "" || factorygithub.DeviceClientID() != ""
}

func (b *appBackend) FactorySetupGitHubStart(ctx context.Context, caller string) (appwire.FactorySignIn, error) {
	id, err := b.localRunning()
	if err != nil {
		return appwire.FactorySignIn{}, err
	}
	dev, err := b.setupGitHubClient().StartDevice(ctx)
	if err != nil {
		return appwire.FactorySignIn{}, appwire.Errorf(appwire.CodeEngine, "%s", err.Error())
	}
	redact.Register(dev.Code)
	trace.Secret(dev.Code)
	flow, err := appFactoryHandle()
	if err != nil {
		return appwire.FactorySignIn{}, err
	}
	now := b.setupGitHubNow()
	g := &b.setupGitHub
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.flows == nil {
		g.flows = map[string]*appSetupGitHubFlow{}
	}
	for key, f := range g.flows {
		if f.caller == caller || !now.Before(f.expires) {
			delete(g.flows, key)
		}
	}
	if len(g.flows) >= setupGitHubFlows {
		return appwire.FactorySignIn{}, appwire.Errorf(appwire.CodeBusy, "too many GitHub sign-ins are waiting; finish or cancel one")
	}
	g.flows[flow] = &appSetupGitHubFlow{
		caller: caller, code: dev.Code, interval: dev.Interval,
		expires: now.Add(time.Duration(dev.ExpiresIn) * time.Second),
		next:    now.Add(time.Duration(dev.Interval) * time.Second),
	}
	return appwire.FactorySignIn{Flow: flow, Factory: id, UserCode: dev.UserCode, VerificationURI: dev.VerificationURI, Interval: dev.Interval, ExpiresIn: dev.ExpiresIn}, nil
}

// claim is the caller's own flow, marked busy for one ask of GitHub, or the
// answer to give without asking: expired, or too soon.
func (b *appBackend) setupGitHubClaim(caller, flow string) (*appSetupGitHubFlow, *appwire.FactorySetupGitHubState, error) {
	g := &b.setupGitHub
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.flows[flow]
	if f == nil || f.caller != caller {
		return nil, nil, errAppFactoryNoFlow()
	}
	if f.busy {
		return nil, nil, appwire.Errorf(appwire.CodeBusy, "this sign-in is being checked")
	}
	now := b.setupGitHubNow()
	if !now.Before(f.expires) {
		delete(g.flows, flow)
		return nil, &appwire.FactorySetupGitHubState{State: appwire.SignInExpired}, nil
	}
	if now.Before(f.next) {
		// TOO SOON TO ASK GITHUB AGAIN: answer without a request.
		return nil, &appwire.FactorySetupGitHubState{State: appwire.SignInPending, Interval: f.interval}, nil
	}
	f.busy = true
	f.next = now.Add(time.Duration(f.interval) * time.Second)
	return f, nil, nil
}

func (b *appBackend) setupGitHubEnd(flow string, f *appSetupGitHubFlow, keep bool, interval int) int {
	g := &b.setupGitHub
	g.mu.Lock()
	defer g.mu.Unlock()
	if !keep {
		delete(g.flows, flow)
		return 0
	}
	if interval > f.interval {
		f.interval = interval
		f.next = b.setupGitHubNow().Add(time.Duration(interval) * time.Second)
	}
	f.busy = false
	return f.interval
}

func (b *appBackend) FactorySetupGitHubPoll(ctx context.Context, caller, flow string) (appwire.FactorySetupGitHubState, error) {
	f, early, err := b.setupGitHubClaim(caller, flow)
	if err != nil {
		return appwire.FactorySetupGitHubState{}, err
	}
	if early != nil {
		return *early, nil
	}
	grant, err := b.setupGitHubClient().PollDevice(ctx, f.code)
	if err != nil {
		// GitHub did not answer: the flow stands, and the next poll asks again.
		b.setupGitHubEnd(flow, f, true, 0)
		return appwire.FactorySetupGitHubState{}, appwire.Errorf(appwire.CodeEngine, "%s", err.Error())
	}
	switch grant.Error {
	case factorygithub.DevicePending:
		return appwire.FactorySetupGitHubState{State: appwire.SignInPending, Interval: b.setupGitHubEnd(flow, f, true, grant.Interval)}, nil
	case factorygithub.DeviceSlowDown:
		slower := grant.Interval
		if slower <= f.interval {
			slower = f.interval + 5
		}
		return appwire.FactorySetupGitHubState{State: appwire.SignInPending, Interval: b.setupGitHubEnd(flow, f, true, slower)}, nil
	case factorygithub.DeviceDenied:
		b.setupGitHubEnd(flow, f, false, 0)
		return appwire.FactorySetupGitHubState{State: appwire.SignInDenied}, nil
	case factorygithub.DeviceExpired:
		b.setupGitHubEnd(flow, f, false, 0)
		return appwire.FactorySetupGitHubState{State: appwire.SignInExpired}, nil
	}
	// GitHub's exchange is single-use: the flow ends here whatever the connect
	// says, and a connect that fails is answered like a pasted token's.
	b.setupGitHubEnd(flow, f, false, 0)
	redact.Register(grant.Token)
	trace.Secret(grant.Token)
	view, err := b.FactorySetupAct(ctx, caller, appwire.FactorySetupAct{Verb: appwire.SetupVerbConnect, Source: appwire.SetupSourceToken, Token: grant.Token})
	if err != nil {
		return appwire.FactorySetupGitHubState{}, err
	}
	return appwire.FactorySetupGitHubState{State: appwire.SignInSignedIn, GitHubToken: grant.Token, View: &view}, nil
}

func (b *appBackend) FactorySetupGitHubCancel(_ context.Context, caller, flow string) error {
	g := &b.setupGitHub
	g.mu.Lock()
	defer g.mu.Unlock()
	f := g.flows[flow]
	if f == nil || f.caller != caller {
		return errAppFactoryNoFlow()
	}
	// GitHub has no way to withdraw a device code; it expires on its own.
	delete(g.flows, flow)
	return nil
}
