package connect

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/int128/oauth2cli"
	"golang.org/x/oauth2"
)

// localServerAddresses are the loopback addresses the browser is sent back to,
// tried in order until one is free.
//
// The first two are fixed because the service's console has to be told, once
// and in advance, exactly which addresses it may return a person to; a port
// picked at random would match nothing that was registered. The last rung is a
// free port, which will not be accepted by a strict console but keeps the
// package working for services that allow any loopback port — and keeps a test
// off a fixed port that another process on the machine may already hold.
var localServerAddresses = []string{"127.0.0.1:8765", "127.0.0.1:18765", "127.0.0.1:0"}

// successPage is what the person sees in the tab they were sent to. It says the
// one thing they need — that they are done and can go back to the terminal —
// and it says it in their own words, not in the vocabulary of the machinery
// that got them here.
const successPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Connected</title>
<style>
  :root { color-scheme: light dark; }
  body {
    margin: 0; min-height: 100vh;
    display: flex; align-items: center; justify-content: center;
    font: 16px/1.6 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
    background: #fafaf9; color: #1c1917;
  }
  main { text-align: center; padding: 2rem; }
  h1 { margin: 0 0 .35rem; font-size: 1.35rem; font-weight: 600; letter-spacing: -.01em; }
  p { margin: 0; color: #78716c; }
  @media (prefers-color-scheme: dark) {
    body { background: #1c1917; color: #fafaf9; }
    p { color: #a8a29e; }
  }
</style>
</head>
<body>
<main>
  <h1>Connected.</h1>
  <p>You can close this tab.</p>
</main>
</body>
</html>
`

// Flow is one connection in progress: a listener waiting on the loopback
// address, and an address for the person to visit.
//
// THE ADDRESS IS READY BEFORE THE WAIT BEGINS. [Manager.BeginAuth] returns only
// once the listener is up and [Flow.URL] can be answered, so a caller can put
// the address on screen, open it, and only then block on [Flow.Wait]. Any other
// arrangement makes the screen wait on the network before it can show anything.
type Flow struct {
	manager *Manager
	plug    Plug
	url     string
	cancel  context.CancelFunc
	done    chan flowResult

	mu      sync.Mutex
	settled bool
	status  Status
	err     error
}

// flowResult is what the browser round-trip eventually produced.
type flowResult struct {
	keys *oauth2.Token
	err  error
}

// BeginAuth starts connecting one service and returns as soon as there is an
// address to send the person to.
//
// THE FLOW OUTLIVES THIS CALL. The listener is deliberately not tied to ctx:
// ctx bounds only the wait for the listener to come up, and the round-trip
// itself lives until [Flow.Wait] returns or [Flow.Cancel] is called. A caller
// that passed a short-lived context to start a connection should not find the
// connection dead a moment later for a reason that has nothing to do with the
// person's browser.
//
// The browser is NOT opened here. Whoever owns the screen decides how the
// address reaches the person.
func (m *Manager) BeginAuth(ctx context.Context, id string) (*Flow, error) {
	plug, err := m.plug(id)
	if err != nil {
		return nil, err
	}
	service := plug.Service()
	// SEAM(mcp): a service that brings its own tools is signed in to by asking
	// it where its sign-in is, rather than from addresses written down here.
	// The trip that follows is the same trip. See mcp_auth.go.
	if server, ok := plug.(*toolServer); ok {
		return m.beginToolServer(ctx, server)
	}
	if service.Auth == AuthKey {
		// There is nothing to open. Saying so here rather than starting a
		// listener nobody will ever be sent to is what keeps a surface from
		// showing a person a page for an account that never wanted one.
		return nil, fmt.Errorf("%s is connected with a key, not in a browser", service.Name)
	}
	credential, err := m.credential(service.ID)
	if err != nil {
		return nil, err
	}

	// The proof key is generated per connection and never leaves this
	// process: the challenge goes out with the browser trip, the verifier
	// goes up with the exchange, and an intercepted round-trip is worth
	// nothing without it.
	verifier := oauth2.GenerateVerifier()

	config := oauth2cli.Config{
		OAuth2Config:           *m.config(plug, credential),
		AuthCodeOptions:        append(plug.AuthCodeOptions(), oauth2.S256ChallengeOption(verifier)),
		TokenRequestOptions:    []oauth2.AuthCodeOption{oauth2.VerifierOption(verifier)},
		LocalServerBindAddress: append([]string(nil), localServerAddresses...),
		LocalServerSuccessHTML: successPage,
		LocalServerReadyChan:   nil,
	}
	ready := make(chan string, 1)
	config.LocalServerReadyChan = ready

	runContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	flow := &Flow{
		manager: m,
		plug:    plug,
		cancel:  cancel,
		done:    make(chan flowResult, 1),
	}
	go func() {
		keys, err := oauth2cli.GetToken(runContext, config)
		if err != nil {
			err = fmt.Errorf("connect %s: %w", service.Name, err)
		}
		flow.done <- flowResult{keys: keys, err: err}
	}()

	select {
	case flow.url = <-ready:
		return flow, nil
	case result := <-flow.done:
		// The listener never came up, so there is nothing to wait on and
		// nothing to cancel beyond the context we just made.
		cancel()
		if result.err != nil {
			return nil, result.err
		}
		return nil, fmt.Errorf("connect %s: the connection ended before it began", service.Name)
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}

// URL is the address to open in a browser. It is a loopback address on this
// machine that immediately sends the browser on to the service's own sign-in
// page — one hop, so that the long address with all its parameters never has to
// be printed in a terminal or retyped by hand.
func (f *Flow) URL() string { return f.url }

// Wait blocks until the person finishes in their browser, ctx ends, or the
// round-trip fails, then saves the connection and answers with its status.
//
// Wait is meant to be called once. Calling it again returns the same answer it
// gave the first time rather than starting anything new.
func (f *Flow) Wait(ctx context.Context) (Status, error) {
	f.mu.Lock()
	settled, status, err := f.settled, f.status, f.err
	f.mu.Unlock()
	if settled {
		return status, err
	}
	select {
	case result := <-f.done:
		return f.settle(ctx, result)
	case <-ctx.Done():
		f.cancel()
		return f.settle(ctx, flowResult{err: fmt.Errorf("connect %s: %w", f.plug.Service().Name, ctx.Err())})
	}
}

// Cancel abandons a connection nobody is going to finish — the person pressed
// escape, or closed the tab and came back. It releases the loopback address so
// the next attempt can have it.
func (f *Flow) Cancel() { f.cancel() }

// settle records the outcome once: it asks whose account this is, writes the
// connection to the store, and fixes the answer every later [Flow.Wait] gets.
func (f *Flow) settle(ctx context.Context, result flowResult) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.settled {
		return f.status, f.err
	}
	f.settled = true
	f.cancel()

	service := f.plug.Service()
	if result.err != nil {
		f.status, f.err = Status{Service: service}, result.err
		return f.status, f.err
	}
	if result.keys == nil {
		f.status = Status{Service: service}
		f.err = fmt.Errorf("connect %s: the service granted nothing", service.Name)
		return f.status, f.err
	}

	entry := stored{Keys: result.keys, Scopes: granted(result.keys, service.Scopes)}
	// THE IDENTITY PROBE MAY FAIL WITHOUT FAILING THE CONNECTION. The keys
	// work; only the label is missing, and a missing label renders as
	// nothing.
	if account, err := f.plug.Account(ctx, staticClient(ctx, result.keys)); err == nil {
		entry.Account = account
	}
	if err := f.manager.store.put(service.ID, entry); err != nil {
		f.status = Status{Service: service}
		f.err = fmt.Errorf("connect %s: %w", service.Name, err)
		return f.status, f.err
	}
	f.status = Status{Service: service, Connected: true, Account: entry.Account}
	return f.status, nil
}

// granted is what the person actually agreed to, for the record kept beside the
// keys.
//
// The service says so itself in the answer to the exchange, and its answer wins:
// a person may untick a box on the permissions screen, and a connection recorded
// as carrying something it does not carry is worse than no record at all.
// A service that says nothing leaves the ASK as the record, which is the closest
// true statement available — it is what the sign-in that just succeeded was for.
func granted(keys *oauth2.Token, asked []string) []string {
	if keys != nil {
		if raw, ok := keys.Extra("scope").(string); ok {
			if given := strings.Fields(raw); len(given) > 0 {
				return given
			}
		}
	}
	return append([]string(nil), asked...)
}
