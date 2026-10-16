package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/codexauth"
)

type accountTestFlow struct {
	result chan error
	ended  chan struct{}
}

func (f *accountTestFlow) URL() string { return "https://example.invalid/account" }
func (f *accountTestFlow) Cancel()     {}
func (f *accountTestFlow) Wait(ctx context.Context) (codexauth.Tokens, error) {
	defer close(f.ended)
	select {
	case err := <-f.result:
		return codexauth.Tokens{}, err
	case <-ctx.Done():
		return codexauth.Tokens{}, ctx.Err()
	}
}
func accountTestState(b *appBackend) string {
	v := appwire.Provider{}
	b.accountAuthView(&v)
	return v.SignInState
}
func awaitAccountState(t *testing.T, b *appBackend, want string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if accountTestState(b) == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("state %q, wanted %q", accountTestState(b), want)
		case <-time.After(time.Millisecond):
		}
	}
}
func TestAppAccountCancelPreventsLateCommitAndAllowsRestart(t *testing.T) {
	b, _, _ := workBackend(t, &fakeConversation{})
	var saves atomic.Int32
	start := func(f *accountTestFlow) error {
		_, err := b.beginAccountSignIn(func(context.Context) (appAccountFlow, error) { return f, nil }, func(context.Context, codexauth.Tokens) error { saves.Add(1); return nil })
		return err
	}
	first := &accountTestFlow{make(chan error, 1), make(chan struct{})}
	if err := start(first); err != nil {
		t.Fatal(err)
	}
	// A second caller joins the one active operation rather than creating a
	// second browser listener or credential write.
	if _, err := b.beginAccountSignIn(func(context.Context) (appAccountFlow, error) { t.Fatal("duplicate browser flow"); return nil, nil }, nil); err != nil {
		t.Fatal(err)
	}
	oldAttempt := b.accountAuth.attempt
	if err := b.CancelSignIn("codex", oldAttempt); err != nil {
		t.Fatal(err)
	}
	first.result <- nil
	<-first.ended
	if saves.Load() != 0 || accountTestState(b) != "cancelled" {
		t.Fatal("cancelled flow saved account")
	}
	second := &accountTestFlow{make(chan error, 1), make(chan struct{})}
	if err := start(second); err != nil {
		t.Fatal(err)
	}
	if err := b.CancelSignIn("codex", oldAttempt); err != nil {
		t.Fatal(err)
	}
	if accountTestState(b) != "pending" {
		t.Fatal("old attempt cancelled the new browser operation")
	}
	second.result <- nil
	awaitAccountState(t, b, "signed_in")
	if err := b.CancelSignIn("codex", "old-operation"); err != nil {
		t.Fatal(err)
	}
	if accountTestState(b) != "signed_in" {
		t.Fatal("stale cancellation changed newer operation")
	}
	if saves.Load() != 1 {
		t.Fatalf("saved %d times", saves.Load())
	}
}
func TestAppAccountSaveIsExplicitCommitBoundary(t *testing.T) {
	b, _, _ := workBackend(t, &fakeConversation{})
	flow := &accountTestFlow{make(chan error, 1), make(chan struct{})}
	entered, release := make(chan struct{}), make(chan struct{})
	_, err := b.beginAccountSignIn(func(context.Context) (appAccountFlow, error) { return flow, nil }, func(context.Context, codexauth.Tokens) error {
		close(entered)
		<-release
		return errors.New("private save failure")
	})
	if err != nil {
		t.Fatal(err)
	}
	flow.result <- nil
	<-entered
	if err := b.CancelSignIn("codex", b.accountAuth.attempt); err == nil {
		t.Fatal("claimed cancellation during authorized save")
	}
	close(release)
	awaitAccountState(t, b, "failed")
	view := appwire.Provider{}
	b.accountAuthView(&view)
	if view.SignInError == "private save failure" {
		t.Fatal("leaked raw credential persistence failure")
	}
}
func TestAppCodexProviderIsAccountNotAPIKey(t *testing.T) {
	b, _, _ := workBackend(t, &fakeConversation{})
	providers, err := b.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range providers {
		if p.ID == "codex" {
			found = true
			if p.AuthKind != "account" || !p.SignIn || p.KeyHint != "" {
				t.Fatalf("wrong account capability: %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("Codex absent alongside other providers")
	}
	if err := b.SetKey("codex", "not-an-account"); err == nil {
		t.Fatal("accepted pasted key for account")
	}
	if _, err := b.CheckKey("codex", "not-an-account"); err == nil {
		t.Fatal("probed pasted key for account")
	}
}
