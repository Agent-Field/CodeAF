package main

import (
	"context"
	"crypto/ed25519"
	"os"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// appClientEmitter schedules fresh snapshots after the engine has published
// its changes. The empty case is one pointer load with no goroutine or timer.
type appClientEmitter struct {
	b    *appBackend
	next appwire.Emitter
}

func (e appClientEmitter) Emit(name string, data any) {
	e.next.Emit(name, data)
	switch name {
	case appwire.EventTurnDone, appwire.EventAskOpened, appwire.EventAskClosed, appwire.EventTaskChanged, appwire.EventAgentChanged:
		if r := e.b.clientRunner.Load(); r != nil {
			r.Changed()
		}
	}
}

type appClientSigner struct {
	id  identity.Identity
	dev identity.Dev
}

func (s appClientSigner) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s appClientSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s appClientSigner) Sign(p []byte) []byte           { return s.dev.Sign(p) }

// startPairedClients preserves the unpaired startup: it does not open sync,
// make keys, or start a timer when no local client record exists.
func (b *appBackend) startPairedClients() {
	if _, err := os.Stat(clientrun.StatePath(home.Dir())); err == nil {
		if _, err := b.clients(); err != nil {
			b.emit.Emit(appwire.EventLog, appwire.Log{Level: "warn", Message: "paired clients could not start"})
		}
	}
}

func (b *appBackend) clients() (*clientrun.Runner, error) {
	b.clientInit.Lock()
	defer b.clientInit.Unlock()
	if r := b.clientRunner.Load(); r != nil {
		return r, nil
	}
	s, err := syncOf()
	if err != nil {
		return nil, err
	}
	r, err := clientrun.Open(clientrun.Config{Home: s.Home, Relay: s.Relay, Signer: appClientSigner{s.Identity, s.Device}, Directory: s.Dir, Engine: b, Now: b.now})
	if err != nil {
		return nil, err
	}
	b.clientRunner.Store(r)
	guard.Go("app/clients", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stop := syncsetup.WatchPublishes(r.Changed)
		defer stop()
		guard.Go("app/clients-stop", func() {
			select {
			case <-b.left:
				cancel()
			case <-ctx.Done():
			}
		})
		r.Run(ctx)
	})
	return r, nil
}
