package main

import (
	"context"
	"crypto/rand"
	"sync"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
)

type appAccountFlow interface {
	URL() string
	Wait(context.Context) (codexauth.Tokens, error)
	Cancel()
}

type appAccountAuth struct {
	attempt string
	mu      sync.Mutex
	flow    appAccountFlow
	cancel  context.CancelFunc
	state   string
	message string
}

// Each transition owns its lock only until the state is decided. Credential
// writes, browser cancellation and provider events run after that lock is gone.
func (a *appAccountAuth) status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// An empty result means cancellation already won. "saving" is the commit
// boundary: no cancellation can claim to undo a write after this transition.
func (a *appAccountAuth) finishWait(err error) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != "pending" {
		return ""
	}
	if err != nil {
		a.state, a.message = "failed", "Sign-in was not completed. Try again."
	} else {
		a.state = "saving"
	}
	return a.state
}

func (a *appAccountAuth) finishSave(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.state, a.message = "failed", "Could not save the account. Try signing in again."
	} else {
		a.state = "signed_in"
	}
}

func (a *appAccountAuth) cancelAttempt(attempt string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.attempt != attempt {
		return false, nil
	}
	if a.state == "saving" {
		return false, appwire.Errorf(appwire.CodeBusy, "account sign-in is being saved")
	}
	if a.state != "pending" {
		return false, nil
	}
	a.state = "cancelled"
	return true, nil
}

func (b *appBackend) accountAuthView(view *appwire.Provider) {
	var auth *appAccountAuth
	b.locked(func() { auth = b.accountAuth })
	if auth == nil {
		return
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	view.SignInState, view.SignInError, view.SignInAttempt = auth.state, auth.message, auth.attempt
}

// accountSignIn is the ChatGPT sign-in, through the same loopback flow the
// terminal's `codeaf connect codex` opens ([connectCodexFlow], or the test's
// [appBackend.codexBegin]) and the same keeping ([config.ConnectCodex]).
func (b *appBackend) accountSignIn() (string, error) {
	begin := b.codexBegin
	if begin == nil {
		begin = connectCodexFlow
	}
	return b.beginAccountSignIn(func(ctx context.Context) (appAccountFlow, error) { return begin(ctx) }, func(ctx context.Context, tokens codexauth.Tokens) error {
		_, err := config.ConnectCodex(ctx, config.ProfileDir(), tokens)
		return err
	})
}

// A single browser operation owns its callback listener until it settles.
// Saving is the commit boundary: cancellation cannot undo an authorized account
// write, and callers receive Busy rather than a false cancellation receipt.
func (b *appBackend) beginAccountSignIn(begin func(context.Context) (appAccountFlow, error), save func(context.Context, codexauth.Tokens) error) (string, error) {
	var auth *appAccountAuth
	var err error
	b.locked(func() {
		if b.closing {
			err = appwire.Errorf(appwire.CodeBusy, "the engine is closing")
			return
		}
		if old := b.accountAuth; old != nil {
			state := old.status()
			if state == "pending" {
				auth = old
				return
			}
			if state == "saving" {
				err = appwire.Errorf(appwire.CodeBusy, "account sign-in is being saved")
				return
			}
			// A cancelled or settled flow may still be closing its listener.
			// Cancel is idempotent; release that listener before binding another.
			old.flow.Cancel()
		}
		ctx, cancel := context.WithTimeout(context.Background(), appSignInWait)
		flow, e := begin(ctx)
		if e != nil {
			cancel()
			err = appEngineError(e)
			return
		}
		auth = &appAccountAuth{attempt: rand.Text(), flow: flow, cancel: cancel, state: "pending"}
		b.accountAuth = auth
		guard.Go("app/account-sign-in", func() {
			defer cancel()
			defer flow.Cancel()
			tokens, e := flow.Wait(ctx)
			state := auth.finishWait(e)
			if state == "" {
				return
			}
			b.providerIDChanged("codex", "")
			if state != "saving" {
				return
			}
			e = save(ctx, tokens)
			auth.finishSave(e)
			if e == nil {
				b.keysMoved("codex")
				// The account's models reach models.list as a pasted key's do.
				b.warmProviderModels()
			}
			b.providerIDChanged("codex", "")
		})
	})
	if err != nil {
		return "", err
	}
	b.providerIDChanged("codex", "")
	return auth.flow.URL(), nil
}

func (b *appBackend) CancelSignIn(provider, attempt string) error {
	if provider != "codex" {
		return appwire.Errorf(appwire.CodeBadParams, "this provider does not expose account sign-in cancellation")
	}
	var auth *appAccountAuth
	b.locked(func() { auth = b.accountAuth })
	if auth == nil {
		return nil
	}
	cancelled, err := auth.cancelAttempt(attempt)
	if err != nil {
		return err
	}
	if !cancelled {
		return nil
	}
	// The cancelled state wins before callbacks run. Neither callback may hold
	// auth.mu: it can wake Wait or re-enter a read of the operation's state.
	auth.cancel()
	auth.flow.Cancel()
	b.providerIDChanged(provider, "")
	return nil
}

// endAccountSignIn settles the ChatGPT sign-in before the account is signed
// out: one still waiting for the browser is cancelled, as providers.sign_in_cancel
// cancels it, one being saved answers busy, because the tokens it is writing
// would outlive the sign-out, and the last one's state is forgotten, so the
// row says signed out and nothing older.
func (b *appBackend) endAccountSignIn() error {
	var auth *appAccountAuth
	b.locked(func() { auth = b.accountAuth })
	if auth == nil {
		return nil
	}
	cancelled, err := auth.cancelAttempt(auth.attempt)
	if err != nil {
		return err
	}
	if cancelled {
		auth.cancel()
	}
	// Cancel is idempotent, and a settled flow may still hold its listener.
	auth.flow.Cancel()
	b.locked(func() {
		if b.accountAuth == auth {
			b.accountAuth = nil
		}
	})
	return nil
}

// StartSignIn returns the non-secret operation identity alongside the browser
// URL. Late cancellation and events cannot be mistaken for a replacement flow.
func (b *appBackend) StartSignIn(provider string) (appwire.ProviderSignInStart, error) {
	if provider != "codex" {
		return appwire.ProviderSignInStart{}, appwire.Errorf(appwire.CodeBadParams, "this provider does not expose account sign-in")
	}
	address, err := b.accountSignIn()
	if err != nil {
		return appwire.ProviderSignInStart{}, err
	}
	var auth *appAccountAuth
	b.locked(func() { auth = b.accountAuth })
	if auth == nil || auth.flow.URL() != address {
		return appwire.ProviderSignInStart{}, appwire.Errorf(appwire.CodeBusy, "sign-in was replaced; try again")
	}
	return appwire.ProviderSignInStart{URL: address, Attempt: auth.attempt}, nil
}
