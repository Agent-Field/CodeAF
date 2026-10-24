package main

import (
	"context"
	"crypto/ed25519"
	"os"
	"strconv"
	"time"

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
	if _, err := os.Stat(clientrun.StatePath(home.Dir())); err != nil {
		return
	}
	local, err := clientrun.Open(clientrun.Config{Home: home.Dir()})
	if err != nil {
		b.emit.Emit(appwire.EventLog, appwire.Log{Level: "warn", Message: "paired client records could not be read"})
		return
	}
	if len(local.Clients()) == 0 {
		b.clientArchive.Store(local)
		return
	}
	if _, err := b.clients(); err != nil {
		b.emit.Emit(appwire.EventLog, appwire.Log{Level: "warn", Message: "paired clients could not start"})
	}
}

func (b *appBackend) clients() (*clientrun.Runner, error) {
	b.clientInit.Lock()
	defer b.clientInit.Unlock()
	select {
	case <-b.left:
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	default:
	}
	if r := b.clientRunner.Load(); r != nil {
		return r, nil
	}
	s, err := syncOf()
	if err != nil {
		return nil, err
	}
	r, err := clientrun.Open(clientrun.Config{Home: s.Home, Relay: s.Relay, Signer: appClientSigner{s.Identity, s.Device}, Directory: s.Dir, Engine: b, Now: b.now, Log: b.logClientCall})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.clientCancel = cancel
	b.clientDone = make(chan struct{})
	b.clientRunner.Store(r)
	guard.Go("app/clients", func() {
		defer close(b.clientDone)
		stop := syncsetup.WatchPublishes(r.Changed)
		defer stop()
		r.Run(ctx)
	})
	return r, nil
}

// logClientCall emits a receipt already durably kept by the runner. Entries
// reads that same history, so reconnecting never loses the who/when/method.
func (b *appBackend) logClientCall(receipt clientrun.Receipt) error {
	if receipt.Agent != "" {
		b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appClientEntry(receipt)})
	}
	return nil
}
func appClientEntry(receipt clientrun.Receipt) appwire.Entry {
	at, _ := time.Parse(time.RFC3339Nano, receipt.At)
	state := "your call"
	switch receipt.Status {
	case "started":
		state = "running"
	case "done":
		state = "done"
	case "failed":
		state = "incomplete"
	}
	suffix := ""
	if receipt.SkippedFrom > 0 {
		suffix = " · skipped missing calls " + strconv.FormatInt(receipt.SkippedFrom, 10) + "–" + strconv.FormatInt(receipt.SkippedThrough, 10)
	}
	return appwire.Entry{ID: "client-" + receipt.Client + "-" + strconv.FormatInt(receipt.Number, 10), Agent: receipt.Agent, Kind: appwire.EntryNotice, At: at, Text: "Paired client " + receipt.Client + " · " + receipt.Method + " · " + state + suffix}
}
func (b *appBackend) clientEntries(agent string) []appwire.Entry {
	r := b.clientRunner.Load()
	if r == nil {
		r = b.clientArchive.Load()
	}
	if r == nil {
		return nil
	}
	var entries []appwire.Entry
	for _, receipt := range r.Receipts() {
		if receipt.Agent == agent {
			entries = append(entries, appClientEntry(receipt))
		}
	}
	return entries
}

func (b *appBackend) ListPairedClients() ([]appwire.PairedClient, error) {
	r := b.clientRunner.Load()
	if r == nil {
		return []appwire.PairedClient{}, nil
	}
	out := make([]appwire.PairedClient, 0)
	for _, c := range r.Clients() {
		out = append(out, appwire.PairedClient{ID: c.ID, Name: c.Name, Methods: c.Methods, Reads: c.Reads, Paused: c.Paused})
	}
	return out, nil
}
func (b *appBackend) PairClient(p appwire.PairClientParams) (appwire.ClientGrant, error) {
	r, err := b.clients()
	if err != nil {
		return appwire.ClientGrant{}, err
	}
	id, err := identity.Load(home.Dir())
	if err != nil {
		return appwire.ClientGrant{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	grant, err := r.Pair(ctx, clientrun.Pairing{Cert: identity.IssueCert(id, p.SigningKey, b.now()), BoxKey: p.BoxKey, Methods: p.Methods, Reads: p.Reads, Name: p.Name})
	return appwire.ClientGrant{Cert: grant.Cert, Identity: grant.Identity, Relay: grant.Relay, Writer: grant.Writer, BoxKey: grant.BoxKey}, err
}
func (b *appBackend) PausePairedClient(id string, paused bool) error {
	r := b.clientRunner.Load()
	if r == nil {
		return appwire.Errorf(appwire.CodeNotFound, "paired client not found")
	}
	return r.Pause(id, paused)
}
func (b *appBackend) UnpairClient(id string) error {
	r := b.clientRunner.Load()
	if r == nil {
		return appwire.Errorf(appwire.CodeNotFound, "paired client not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return r.Unpair(ctx, id)
}

// stopClients joins the executor before any engine it may call is closed.
func (b *appBackend) stopClients() {
	done := func() chan struct{} {
		b.clientInit.Lock()
		defer b.clientInit.Unlock()
		if b.clientCancel != nil {
			b.clientCancel()
		}
		return b.clientDone
	}()
	if done != nil {
		<-done
	}
}
