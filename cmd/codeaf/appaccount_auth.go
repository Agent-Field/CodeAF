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

func (b *appBackend) accountAuthView(view *appwire.Provider) {
	var auth *appAccountAuth
	b.locked(func() { auth = b.accountAuth })
	if auth == nil {
		return
	}
	auth.mu.Lock()
	view.SignInState, view.SignInError, view.SignInAttempt = auth.state, auth.message, auth.attempt
	auth.mu.Unlock()
}

func (b *appBackend) accountSignIn() (string, error) {
	return b.beginAccountSignIn(func(ctx context.Context) (appAccountFlow, error) { return codexauth.Begin(ctx, codexauth.Options{}) }, func(ctx context.Context, tokens codexauth.Tokens) error {
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
			old.mu.Lock()
			state := old.state
			old.mu.Unlock()
			if state == "pending" {
				auth = old
				return
			}
			if state == "saving" {
				err = appwire.Errorf(appwire.CodeBusy, "account sign-in is being saved")
				return
			}
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
			auth.mu.Lock()
			if auth.state != "pending" {
				auth.mu.Unlock()
				return
			}
			if e != nil {
				auth.state = "failed"
				auth.message = "Sign-in was not completed. Try again."
				auth.mu.Unlock()
				b.providerChanged("codex", "")
				return
			}
			auth.state = "saving"
			auth.mu.Unlock()
			b.providerChanged("codex", "")
			e = save(ctx, tokens)
			auth.mu.Lock()
			if e != nil {
				auth.state = "failed"
				auth.message = "Could not save the account. Try signing in again."
			} else {
				auth.state = "signed_in"
			}
			auth.mu.Unlock()
			if e == nil {
				b.keysMoved("codex")
			}
			b.providerChanged("codex", "")
		})
	})
	if err != nil {
		return "", err
	}
	b.providerChanged("codex", "")
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
	auth.mu.Lock()
	if auth.attempt != attempt {
		auth.mu.Unlock()
		return nil
	}
	if auth.state == "saving" {
		auth.mu.Unlock()
		return appwire.Errorf(appwire.CodeBusy, "account sign-in is being saved")
	}
	if auth.state != "pending" {
		auth.mu.Unlock()
		return nil
	}
	auth.state = "cancelled"
	auth.cancel()
	auth.flow.Cancel()
	auth.mu.Unlock()
	b.providerChanged(provider, "")
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
